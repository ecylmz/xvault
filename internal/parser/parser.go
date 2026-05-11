package parser

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/ecylmz/xvault/internal/model"
)

var idRe = regexp.MustCompile(`^\d{5,}$`)

func Timeline(raw []byte, collectionType, folderID, folderName, rawID string) (model.ParsedPage, error) {
	var root any
	if err := json.Unmarshal(raw, &root); err != nil {
		return model.ParsedPage{}, err
	}
	p := model.ParsedPage{RawID: rawID}
	seenTweets := map[string]bool{}
	seenUsers := map[string]bool{}
	walk(root, func(m map[string]any) {
		if cursor := cursorValue(m); cursor != "" {
			p.NextCursor = cursor
		}
		if u, ok := parseUser(m); ok && !seenUsers[u.ID] {
			seenUsers[u.ID] = true
			p.Users = append(p.Users, u)
		}
		if tw, urls, media, ok := parseTweet(m, rawID); ok && !seenTweets[tw.ID] {
			seenTweets[tw.ID] = true
			p.Tweets = append(p.Tweets, tw)
			p.URLs = append(p.URLs, urls...)
			p.Media = append(p.Media, media...)
			if collectionType != "" {
				p.Collections = append(p.Collections, model.CollectionItem{
					TweetID: tw.ID, CollectionType: collectionType, BookmarkFolderID: folderID,
					BookmarkFolderName: folderName, SortIndex: tw.ID,
				})
			}
		}
	})
	return p, nil
}

func parseTweet(m map[string]any, rawID string) (model.Tweet, []model.URL, []model.Media, bool) {
	id := str(m, "rest_id", "id_str", "id")
	legacy := obj(m, "legacy")
	if id == "" {
		id = str(legacy, "id_str", "id")
	}
	if !idRe.MatchString(id) {
		return model.Tweet{}, nil, nil, false
	}
	text := first(str(legacy, "full_text", "text"), str(m, "full_text", "text"))
	if text == "" && !looksTweet(m) {
		return model.Tweet{}, nil, nil, false
	}
	authorID := str(legacy, "user_id_str", "user_id")
	if authorID == "" {
		core := obj(m, "core")
		userResults := obj(core, "user_results")
		result := obj(userResults, "result")
		authorID = str(result, "rest_id", "id")
	}
	created := str(legacy, "created_at")
	tw := model.Tweet{
		ID: id, Text: htmlUnescape(text), Lang: str(legacy, "lang"), AuthorID: authorID,
		CreatedAt: twitterTime(created), ConversationID: str(legacy, "conversation_id_str"),
		InReplyToTweetID: str(legacy, "in_reply_to_status_id_str"), InReplyToUserID: str(legacy, "in_reply_to_user_id_str"),
		IsReply: str(legacy, "in_reply_to_status_id_str") != "", RawJSONID: rawID,
		ReplyCount: intv(legacy, "reply_count"), RetweetCount: intv(legacy, "retweet_count"),
		LikeCount: intv(legacy, "favorite_count"), QuoteCount: intv(legacy, "quote_count"),
	}
	if tw.ConversationID == "" {
		tw.ConversationID = id
	}
	if tw.AuthorID == "" {
		tw.AuthorID = "unknown"
	}
	if core := obj(m, "core"); core != nil {
		if result := obj(obj(core, "user_results"), "result"); result != nil {
			legacyUser := obj(result, "legacy")
			coreUser := obj(result, "core")
			tw.AuthorUsername = first(str(coreUser, "screen_name"), str(legacyUser, "screen_name"))
			tw.AuthorDisplayName = first(str(coreUser, "name"), str(legacyUser, "name"))
		}
	}
	if quoted := obj(m, "quoted_status_result"); quoted != nil {
		if result := obj(quoted, "result"); result != nil {
			tw.QuotedTweetID = str(result, "rest_id")
			tw.IsQuote = tw.QuotedTweetID != ""
		}
	}
	if retweeted := obj(legacy, "retweeted_status_result"); retweeted != nil {
		if result := obj(retweeted, "result"); result != nil {
			tw.RetweetedTweetID = str(result, "rest_id")
			tw.IsRetweet = tw.RetweetedTweetID != ""
		}
	}
	if views := obj(m, "views"); views != nil {
		if v := str(views, "count"); v != "" {
			if n, err := strconv.ParseInt(v, 10, 64); err == nil {
				tw.ViewCount = &n
			}
		}
	}
	var urls []model.URL
	var media []model.Media
	entities := obj(legacy, "entities")
	for _, u := range arrKey(entities, "urls") {
		if um, ok := u.(map[string]any); ok {
			urls = append(urls, model.URL{TweetID: id, URL: str(um, "url"), ExpandedURL: str(um, "expanded_url"), DisplayURL: str(um, "display_url")})
		}
	}
	for _, mm := range append(arrKey(entities, "media"), arrKey(obj(legacy, "extended_entities"), "media")...) {
		if med, ok := mm.(map[string]any); ok {
			media = append(media, model.Media{ID: str(med, "id_str", "media_key"), TweetID: id, MediaType: str(med, "type"), URL: str(med, "media_url_https", "media_url"), ExpandedURL: str(med, "expanded_url"), PreviewURL: str(med, "media_url_https", "media_url"), AltText: str(med, "ext_alt_text")})
		}
	}
	return tw, urls, media, true
}

func parseUser(m map[string]any) (model.User, bool) {
	id := str(m, "rest_id", "id_str", "id")
	legacy := obj(m, "legacy")
	if id == "" {
		id = str(legacy, "id_str", "id")
	}
	username := first(str(legacy, "screen_name"), str(m, "screen_name"))
	name := first(str(legacy, "name"), str(m, "name"))
	if core := obj(m, "core"); core != nil {
		username = first(str(core, "screen_name"), username)
		name = first(str(core, "name"), name)
	}
	if !idRe.MatchString(id) || (username == "" && name == "") {
		return model.User{}, false
	}
	return model.User{ID: id, Username: username, DisplayName: name, AvatarURL: str(legacy, "profile_image_url_https"), Verified: boolv(legacy, "verified"), Protected: boolv(legacy, "protected")}, true
}

func cursorValue(m map[string]any) string {
	if strings.Contains(strings.ToLower(str(m, "cursorType", "cursor_type")), "bottom") {
		return str(m, "value")
	}
	if strings.EqualFold(str(m, "entryType", "__typename"), "TimelineTimelineCursor") && strings.Contains(strings.ToLower(str(m, "cursorType")), "bottom") {
		return str(m, "value")
	}
	return ""
}

func walk(v any, fn func(map[string]any)) {
	switch x := v.(type) {
	case map[string]any:
		fn(x)
		for _, child := range x {
			walk(child, fn)
		}
	case []any:
		for _, child := range x {
			walk(child, fn)
		}
	}
}

func obj(m map[string]any, key string) map[string]any {
	if m == nil {
		return nil
	}
	v, _ := m[key].(map[string]any)
	return v
}

func arrKey(m map[string]any, key string) []any {
	if m == nil {
		return nil
	}
	if a, ok := m[key].([]any); ok {
		return a
	}
	return nil
}

func str(m map[string]any, keys ...string) string {
	for _, k := range keys {
		if m == nil {
			continue
		}
		switch v := m[k].(type) {
		case string:
			return v
		case float64:
			return strconv.FormatInt(int64(v), 10)
		}
	}
	return ""
}

func intv(m map[string]any, key string) int64 {
	switch v := m[key].(type) {
	case float64:
		return int64(v)
	case string:
		n, _ := strconv.ParseInt(v, 10, 64)
		return n
	default:
		return 0
	}
}

func boolv(m map[string]any, key string) bool {
	v, _ := m[key].(bool)
	return v
}

func first(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

func looksTweet(m map[string]any) bool {
	typ := str(m, "__typename", "typename")
	return strings.Contains(typ, "Tweet")
}

func htmlUnescape(s string) string {
	repl := strings.NewReplacer("&amp;", "&", "&lt;", "<", "&gt;", ">", "&quot;", `"`, "&#39;", "'")
	return repl.Replace(s)
}

func twitterTime(s string) string {
	if s == "" {
		return ""
	}
	// X legacy dates are usually "Mon Jan 02 15:04:05 -0700 2006".
	return s
}

func DebugJSON(v any) string {
	b, _ := json.MarshalIndent(v, "", "  ")
	return fmt.Sprint(string(b))
}
