package syncer

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/ecylmz/xvault/internal/client"
	"github.com/ecylmz/xvault/internal/parser"
	"github.com/ecylmz/xvault/internal/queryids"
	"github.com/ecylmz/xvault/internal/store"
)

type Request struct {
	Collection string
	Count      int
	MaxPages   int
	All        bool
	Full       bool
	Folder     string
}

type Result struct {
	Collection           string `json:"collection"`
	Mode                 string `json:"mode"`
	PagesFetched         int    `json:"pages_fetched"`
	TweetsSeen           int    `json:"tweets_seen"`
	TweetsInserted       int    `json:"tweets_inserted"`
	TweetsUpdated        int    `json:"tweets_updated"`
	TweetsUnchanged      int    `json:"tweets_unchanged"`
	QuotedTweetsInserted int    `json:"quoted_tweets_inserted"`
	FoldersSeen          int    `json:"folders_seen"`
	CheckpointCleared    bool   `json:"checkpoint_cleared"`
	RateLimitEvents      int    `json:"rate_limit_events"`
	DBPath               string `json:"db_path"`
	NextCursor           string `json:"next_cursor,omitempty"`
}

type Syncer struct {
	client *client.Client
	store  *store.Store
	qids   queryids.Cache
	dbPath string
	userID string
	delay  time.Duration
}

func New(c *client.Client, st *store.Store, qids queryids.Cache, dbPath, twid string, delay time.Duration) *Syncer {
	return &Syncer{client: c, store: st, qids: qids, dbPath: dbPath, userID: cleanTWID(twid), delay: delay}
}

func (s *Syncer) Sync(ctx context.Context, req Request) (Result, error) {
	ops, err := s.operations(req)
	if err != nil {
		return Result{}, err
	}
	res := Result{Collection: req.Collection, Mode: "incremental", DBPath: s.dbPath}
	limit := req.Count
	if req.All {
		limit = -1
	}
	for _, op := range ops {
		cursor := ""
		seenCursors := map[string]bool{}
		seenTweets := map[string]bool{}
		unchangedPages := 0
		for {
			if req.MaxPages > 0 && res.PagesFetched >= req.MaxPages {
				return res, nil
			}
			if cursor != "" {
				if seenCursors[cursor] {
					res.CheckpointCleared = true
					return res, nil
				}
				seenCursors[cursor] = true
			}
			if cursor != "" {
				op.Variables["cursor"] = cursor
			}
			raw, status, err := s.client.FetchGraphQL(ctx, op)
			if status == 429 {
				res.RateLimitEvents++
				return res, fmt.Errorf("rate limited by X while syncing %s", req.Collection)
			}
			if err != nil {
				return res, err
			}
			rawID, _ := s.store.SaveRaw(ctx, "graphql", op.Name, raw)
			page, err := parser.Timeline(raw, collectionDBValue(req.Collection), "", "", rawID)
			if err != nil {
				return res, err
			}
			for i := range page.Tweets {
				if page.Tweets[i].RawJSONID == "" {
					page.Tweets[i].RawJSONID = rawID
				}
			}
			if err := s.store.UpsertPage(ctx, page); err != nil {
				return res, err
			}
			newInPage := 0
			for _, tw := range page.Tweets {
				if !seenTweets[tw.ID] {
					seenTweets[tw.ID] = true
					newInPage++
				}
			}
			if newInPage == 0 {
				unchangedPages++
			} else {
				unchangedPages = 0
			}
			res.PagesFetched++
			res.TweetsSeen += len(page.Tweets)
			res.TweetsInserted += newInPage
			cursor = page.NextCursor
			if limit > 0 && res.TweetsSeen >= limit {
				return res, nil
			}
			if cursor == "" || unchangedPages >= 2 {
				res.CheckpointCleared = true
				break
			}
			if s.delay > 0 {
				select {
				case <-ctx.Done():
					return res, ctx.Err()
				case <-time.After(s.delay):
				}
			}
		}
	}
	return res, nil
}

func (s *Syncer) operations(req Request) ([]client.Operation, error) {
	count := req.Count
	if count <= 0 || req.All {
		count = 100
	}
	baseVars := func() map[string]any {
		v := map[string]any{"count": count, "includePromotedContent": false, "withQuickPromoteEligibilityTweetFields": false, "withVoice": true}
		if s.userID != "" {
			v["userId"] = s.userID
		}
		return v
	}
	switch req.Collection {
	case "likes":
		return []client.Operation{{Name: "Likes", QueryID: s.qids.QueryID("Likes"), Variables: baseVars()}}, nil
	case "bookmarks":
		v := baseVars()
		// X currently exposes bookmarks through BookmarkSearchTimeline. This
		// tautological query returns both link/media and non-link bookmarks while
		// still satisfying the non-empty search query requirement.
		v["rawQuery"] = "filter:links OR filter:media OR -filter:links"
		return []client.Operation{{Name: "BookmarkSearchTimeline", QueryID: s.qids.QueryID("BookmarkSearchTimeline"), Variables: v}}, nil
	case "tweets":
		return []client.Operation{{Name: "UserTweets", QueryID: s.qids.QueryID("UserTweets"), Variables: baseVars()}}, nil
	case "replies":
		v := baseVars()
		v["includePromotedContent"] = true
		v["withCommunity"] = false
		return []client.Operation{{Name: "UserTweetsAndReplies", QueryID: s.qids.QueryID("UserTweetsAndReplies"), Method: "POST", Variables: v, FieldToggles: defaultFieldToggles()}}, nil
	case "posts":
		return []client.Operation{{Name: "UserTweets", QueryID: s.qids.QueryID("UserTweets"), Variables: baseVars()}}, nil
	case "reposts":
		return []client.Operation{{Name: "UserTweets", QueryID: s.qids.QueryID("UserTweets"), Variables: baseVars()}}, nil
	case "feed":
		return []client.Operation{{Name: "HomeLatestTimeline", QueryID: s.qids.QueryID("HomeLatestTimeline"), Variables: map[string]any{"count": count, "includePromotedContent": false, "latestControlAvailable": true}}}, nil
	default:
		return nil, fmt.Errorf("unsupported sync collection %q", req.Collection)
	}
}

func defaultFieldToggles() map[string]any {
	return map[string]any{
		"withPayments":                false,
		"withAuxiliaryUserLabels":     false,
		"withArticleRichContentState": false,
		"withArticlePlainText":        false,
		"withArticleSummaryText":      false,
		"withArticleVoiceOver":        false,
		"withGrokAnalyze":             false,
		"withDisallowedReplyControls": false,
	}
}

func collectionDBValue(v string) string {
	switch v {
	case "likes":
		return "like"
	case "bookmarks":
		return "bookmark"
	case "tweets":
		return "tweet"
	case "reposts":
		return "repost"
	case "replies":
		return "reply"
	default:
		return strings.TrimSuffix(v, "s")
	}
}

func cleanTWID(twid string) string {
	twid = strings.Trim(twid, `"`)
	if decoded, err := url.QueryUnescape(twid); err == nil {
		twid = decoded
	}
	twid = strings.TrimPrefix(twid, "u=")
	if _, err := strconv.ParseInt(twid, 10, 64); err == nil {
		return twid
	}
	return ""
}
