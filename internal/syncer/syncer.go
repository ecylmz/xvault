package syncer

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/ecylmz/xvault/internal/client"
	"github.com/ecylmz/xvault/internal/model"
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
	FolderID   string
	FeedHours  int
}

type Result struct {
	RunID                string `json:"run_id,omitempty"`
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

func (s *Syncer) Sync(ctx context.Context, req Request) (res Result, err error) {
	ops, err := s.operations(req)
	if err != nil {
		return Result{}, err
	}
	res = Result{Collection: req.Collection, Mode: "incremental", DBPath: s.dbPath}
	if req.Full {
		res.Mode = "full"
	}
	runCollection := collectionDBValue(req.Collection)
	runID, err := s.store.StartSyncRun(ctx, runCollection, res.Mode)
	if err != nil {
		return Result{}, err
	}
	res.RunID = runID
	defer func() {
		status := "success"
		errorsCount := 0
		errorCode := ""
		errorMessage := ""
		if err != nil {
			status = "failed"
			errorsCount = 1
			errorCode = classifySyncError(err)
			errorMessage = summarizeSyncError(errorCode)
		} else if res.NextCursor != "" && !res.CheckpointCleared {
			status = "partial"
		}
		_ = s.store.FinishSyncRun(ctx, store.SyncRun{
			ID:              runID,
			Status:          status,
			PagesFetched:    res.PagesFetched,
			TweetsSeen:      res.TweetsSeen,
			TweetsInserted:  res.TweetsInserted,
			TweetsUpdated:   res.TweetsUpdated,
			TweetsUnchanged: res.TweetsUnchanged,
			ErrorsCount:     errorsCount,
			RateLimitCount:  res.RateLimitEvents,
			ErrorCode:       errorCode,
			ErrorMessage:    errorMessage,
		})
	}()
	if req.Full {
		if err := s.store.ClearCheckpoint(ctx, collectionDBValue(req.Collection)); err != nil {
			return Result{}, err
		}
	}
	limit := req.Count
	if req.All {
		limit = -1
	}
	var feedBoundary time.Time
	if req.Collection == "feed" && !req.All && req.FeedHours > 0 {
		feedBoundary = time.Now().UTC().Add(-time.Duration(req.FeedHours) * time.Hour)
	}
	for _, op := range ops {
		cursor := ""
		checkpointType := runCollection
		if !req.Full {
			cp, ok, err := s.store.LoadCheckpoint(ctx, checkpointType)
			if err != nil {
				return res, err
			}
			if ok && cp.Status == "in_progress" && cp.Cursor != "" {
				cursor = cp.Cursor
				res.NextCursor = cursor
			}
		}
		seenCursors := map[string]bool{}
		seenTweets := map[string]bool{}
		unchangedPages := 0
		oldFeedPages := 0
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
			folderID, folderName := folderMetadata(req)
			page, err := parser.Timeline(raw, collectionDBValue(req.Collection), folderID, folderName, rawID)
			if err != nil {
				return res, err
			}
			page = normalizePageForCollection(page, req.Collection)
			for i := range page.Collections {
				page.Collections[i].SourceRunID = runID
			}
			res.FoldersSeen += countPageFolders(page)
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
			res.NextCursor = cursor
			if !feedBoundary.IsZero() {
				if pageHasTweetAtOrAfter(page, feedBoundary) {
					oldFeedPages = 0
				} else {
					oldFeedPages++
				}
				if oldFeedPages >= 2 {
					if err := s.store.ClearCheckpoint(ctx, checkpointType); err != nil {
						return res, err
					}
					res.CheckpointCleared = true
					res.NextCursor = ""
					return res, nil
				}
			}
			if limit > 0 && res.TweetsSeen >= limit {
				if cursor != "" {
					if err := s.saveCheckpoint(ctx, checkpointType, runID, cursor, res.TweetsSeen, page); err != nil {
						return res, err
					}
				}
				return res, nil
			}
			if cursor == "" || unchangedPages >= 2 {
				if err := s.store.ClearCheckpoint(ctx, checkpointType); err != nil {
					return res, err
				}
				res.CheckpointCleared = true
				break
			}
			if err := s.saveCheckpoint(ctx, checkpointType, runID, cursor, res.TweetsSeen, page); err != nil {
				return res, err
			}
			if req.MaxPages > 0 && res.PagesFetched >= req.MaxPages {
				return res, nil
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

func folderMetadata(req Request) (string, string) {
	if req.Collection != "bookmarks" || req.Folder == "" {
		return "", ""
	}
	if req.FolderID != "" {
		return req.FolderID, req.Folder
	}
	return req.Folder, req.Folder
}

func countPageFolders(page model.ParsedPage) int {
	seen := map[string]bool{}
	for _, item := range page.Collections {
		if item.CollectionType == "bookmark" && item.BookmarkFolderID != "" && !seen[item.BookmarkFolderID] {
			seen[item.BookmarkFolderID] = true
		}
	}
	return len(seen)
}

func pageHasTweetAtOrAfter(page model.ParsedPage, boundary time.Time) bool {
	for _, tw := range page.Tweets {
		created, ok := parseTweetTime(tw.CreatedAt)
		if ok && !created.Before(boundary) {
			return true
		}
	}
	return false
}

func parseTweetTime(value string) (time.Time, bool) {
	for _, layout := range []string{time.RFC3339, "Mon Jan 02 15:04:05 -0700 2006"} {
		if t, err := time.Parse(layout, value); err == nil {
			return t.UTC(), true
		}
	}
	return time.Time{}, false
}

func (s *Syncer) saveCheckpoint(ctx context.Context, collectionType, runID, cursor string, totalSeen int, page model.ParsedPage) error {
	cp := store.Checkpoint{CollectionType: collectionType, Cursor: cursor, SourceRunID: runID, TotalSeen: totalSeen, Status: "in_progress"}
	for i := len(page.Tweets) - 1; i >= 0; i-- {
		if page.Tweets[i].ID != "" {
			cp.LastTweetID = page.Tweets[i].ID
			break
		}
	}
	for i := len(page.Collections) - 1; i >= 0; i-- {
		if page.Collections[i].SortIndex != "" {
			cp.LastSortIndex = page.Collections[i].SortIndex
			break
		}
	}
	return s.store.SaveCheckpoint(ctx, cp)
}

func summarizeSyncError(code string) string {
	switch code {
	case "RATE_LIMITED":
		return "rate limited by X"
	case "AUTH_EXPIRED":
		return "authentication cookies were rejected by X"
	case "QUERY_ID_STALE":
		return "X GraphQL query ID appears stale"
	default:
		return "sync failed"
	}
}

func classifySyncError(err error) string {
	if err == nil {
		return ""
	}
	msg := strings.ToLower(err.Error())
	switch {
	case strings.Contains(msg, "rate limited"):
		return "RATE_LIMITED"
	case strings.Contains(msg, "http 401"), strings.Contains(msg, "authenticate"):
		return "AUTH_EXPIRED"
	case strings.Contains(msg, "http 404"), strings.Contains(msg, "query not found"):
		return "QUERY_ID_STALE"
	default:
		return "SYNC_FAILED"
	}
}

func normalizePageForCollection(page model.ParsedPage, collection string) model.ParsedPage {
	keep := map[string]bool{}
	filtered := page.Tweets[:0]
	for _, tw := range page.Tweets {
		include := true
		switch collection {
		case "tweets":
			include = !tw.IsRetweet && !tw.IsReply
		case "replies":
			include = tw.IsReply
		case "reposts":
			include = tw.IsRetweet || tw.RetweetedTweetID != ""
		}
		if include {
			filtered = append(filtered, tw)
			keep[tw.ID] = true
		}
	}
	if collection == "posts" {
		for _, tw := range page.Tweets {
			keep[tw.ID] = true
		}
		filtered = page.Tweets
	}
	page.Tweets = filtered
	collections := []model.CollectionItem{}
	for _, item := range page.Collections {
		if keep[item.TweetID] {
			collections = append(collections, item)
		}
	}
	if collection == "posts" {
		for _, tw := range page.Tweets {
			collections = append(collections, model.CollectionItem{TweetID: tw.ID, CollectionType: "post"})
			if tw.IsRetweet || tw.RetweetedTweetID != "" {
				collections = append(collections, model.CollectionItem{TweetID: tw.ID, CollectionType: "repost"})
			} else if !tw.IsReply {
				collections = append(collections, model.CollectionItem{TweetID: tw.ID, CollectionType: "tweet"})
			}
		}
	}
	page.Collections = collections
	page.URLs = filterURLs(page.URLs, keep)
	page.Media = filterMedia(page.Media, keep)
	return page
}

func filterURLs(in []model.URL, keep map[string]bool) []model.URL {
	out := in[:0]
	for _, u := range in {
		if keep[u.TweetID] {
			out = append(out, u)
		}
	}
	return out
}

func filterMedia(in []model.Media, keep map[string]bool) []model.Media {
	out := in[:0]
	for _, m := range in {
		if keep[m.TweetID] {
			out = append(out, m)
		}
	}
	return out
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
		if req.Folder != "" {
			folderID := req.FolderID
			if folderID == "" {
				folderID = req.Folder
			}
			v["folderId"] = folderID
			return []client.Operation{{Name: "BookmarkFolderTimeline", QueryID: s.qids.QueryID("BookmarkFolderTimeline"), Variables: v}}, nil
		}
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
