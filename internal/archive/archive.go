package archive

import (
	archivezip "archive/zip"
	"bytes"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/ecylmz/xvault/internal/model"
)

type ParsedArchive struct {
	Page    model.ParsedPage
	Summary Summary
}

type Summary struct {
	ArchivePath       string        `json:"archive_path"`
	AccountID         string        `json:"account_id,omitempty"`
	AccountUsername   string        `json:"account_username,omitempty"`
	Files             []FileSummary `json:"files"`
	TweetsSeen        int           `json:"tweets_seen"`
	DeletedTweetsSeen int           `json:"deleted_tweets_seen"`
	NoteTweetsSeen    int           `json:"note_tweets_seen"`
	LikesSeen         int           `json:"likes_seen"`
	BookmarksSeen     int           `json:"bookmarks_seen"`
	TotalTweetsSeen   int           `json:"total_tweets_seen"`
	CollectionsSeen   int           `json:"collections_seen"`
}

type FileSummary struct {
	Path  string `json:"path"`
	Kind  string `json:"kind"`
	Count int    `json:"count"`
}

var (
	accountFileRe        = regexp.MustCompile(`(?i)(^|/)data/account\.js$`)
	tweetFileRe          = regexp.MustCompile(`(?i)(^|/)data/(tweets|community-tweet)(-part\d+)?\.js$`)
	deletedTweetFileRe   = regexp.MustCompile(`(?i)(^|/)data/deleted-(tweets|tweet-headers)(-part\d+)?\.js$`)
	noteTweetFileRe      = regexp.MustCompile(`(?i)(^|/)data/note-tweet(-part\d+)?\.js$`)
	deletedNoteTweetRe   = regexp.MustCompile(`(?i)(^|/)data/deleted-note-tweet(-part\d+)?\.js$`)
	likeFileRe           = regexp.MustCompile(`(?i)(^|/)data/(like|likes)(-part\d+)?\.js$`)
	bookmarkFileRe       = regexp.MustCompile(`(?i)(^|/)data/(bookmark|bookmarks)(-part\d+)?\.js$`)
	statusURLRe          = regexp.MustCompile(`(?i)^/(.+?)/status/([0-9]+)`)
	twitterArchiveLayout = "Mon Jan 02 15:04:05 -0700 2006"
)

func ParseZip(path string) (ParsedArchive, error) {
	zr, err := archivezip.OpenReader(path)
	if err != nil {
		return ParsedArchive{}, err
	}
	defer func() { _ = zr.Close() }()

	files := map[string][]sourceFile{}
	for _, f := range zr.File {
		if f.FileInfo().IsDir() {
			continue
		}
		name := normalizeArchivePath(f.Name)
		kind := archiveFileKind(name)
		if kind == "" {
			continue
		}
		payload, err := readZipFile(f)
		if err != nil {
			return ParsedArchive{}, fmt.Errorf("read %s: %w", name, err)
		}
		files[kind] = append(files[kind], sourceFile{name: name, payload: payload})
	}

	for kind := range files {
		sort.Slice(files[kind], func(i, j int) bool { return files[kind][i].name < files[kind][j].name })
	}

	account := model.User{}
	if entries := files["account"]; len(entries) > 0 {
		records, err := parseArchiveArray(entries[0].payload)
		if err != nil {
			return ParsedArchive{}, fmt.Errorf("parse %s: %w", entries[0].name, err)
		}
		account = parseAccount(records)
	}

	b := newBuilder()
	if account.ID != "" {
		b.addUser(account)
	}

	summary := Summary{
		ArchivePath:     path,
		AccountID:       account.ID,
		AccountUsername: account.Username,
	}

	parseGroup := func(kind string, fn func(sourceFile, []map[string]any) int) error {
		for _, file := range files[kind] {
			records, err := parseArchiveArray(file.payload)
			if err != nil {
				return fmt.Errorf("parse %s: %w", file.name, err)
			}
			count := fn(file, records)
			summary.Files = append(summary.Files, FileSummary{Path: file.name, Kind: kind, Count: count})
		}
		return nil
	}

	if err := parseGroup("tweets", func(file sourceFile, records []map[string]any) int {
		count := parseTweets(records, account, false, b)
		summary.TweetsSeen += count
		return count
	}); err != nil {
		return ParsedArchive{}, err
	}
	if err := parseGroup("deleted_tweets", func(file sourceFile, records []map[string]any) int {
		count := parseTweets(records, account, true, b)
		summary.DeletedTweetsSeen += count
		return count
	}); err != nil {
		return ParsedArchive{}, err
	}
	if err := parseGroup("note_tweets", func(file sourceFile, records []map[string]any) int {
		count := parseNoteTweets(records, account, false, b)
		summary.NoteTweetsSeen += count
		return count
	}); err != nil {
		return ParsedArchive{}, err
	}
	if err := parseGroup("deleted_note_tweets", func(file sourceFile, records []map[string]any) int {
		count := parseNoteTweets(records, account, true, b)
		summary.NoteTweetsSeen += count
		summary.DeletedTweetsSeen += count
		return count
	}); err != nil {
		return ParsedArchive{}, err
	}
	if err := parseGroup("likes", func(file sourceFile, records []map[string]any) int {
		count := parseCollectionTweets(records, "like", b)
		summary.LikesSeen += count
		return count
	}); err != nil {
		return ParsedArchive{}, err
	}
	if err := parseGroup("bookmarks", func(file sourceFile, records []map[string]any) int {
		count := parseCollectionTweets(records, "bookmark", b)
		summary.BookmarksSeen += count
		return count
	}); err != nil {
		return ParsedArchive{}, err
	}

	summary.TotalTweetsSeen = len(b.page.Tweets)
	summary.CollectionsSeen = len(b.page.Collections)
	if summary.TotalTweetsSeen == 0 && summary.CollectionsSeen == 0 {
		return ParsedArchive{}, fmt.Errorf("no supported Twitter archive records found")
	}
	return ParsedArchive{Page: b.page, Summary: summary}, nil
}

type sourceFile struct {
	name    string
	payload []byte
}

type builder struct {
	page            model.ParsedPage
	tweetIndex      map[string]int
	users           map[string]bool
	collections     map[string]bool
	urls            map[string]bool
	media           map[string]bool
	mentions        map[string]bool
	hashtags        map[string]bool
	pseudoUserIndex map[string]bool
}

func newBuilder() *builder {
	return &builder{
		tweetIndex:      map[string]int{},
		users:           map[string]bool{},
		collections:     map[string]bool{},
		urls:            map[string]bool{},
		media:           map[string]bool{},
		mentions:        map[string]bool{},
		hashtags:        map[string]bool{},
		pseudoUserIndex: map[string]bool{},
	}
}

func (b *builder) addUser(u model.User) {
	if u.ID == "" || b.users[u.ID] {
		return
	}
	b.users[u.ID] = true
	b.page.Users = append(b.page.Users, u)
}

func (b *builder) addTweet(tw model.Tweet) {
	if tw.ID == "" {
		return
	}
	if tw.ConversationID == "" {
		tw.ConversationID = tw.ID
	}
	if tw.AuthorID == "" {
		tw.AuthorID = "unknown"
	}
	if idx, ok := b.tweetIndex[tw.ID]; ok {
		existing := b.page.Tweets[idx]
		if existing.IsTombstone && !tw.IsTombstone {
			b.page.Tweets[idx] = tw
		}
		return
	}
	b.tweetIndex[tw.ID] = len(b.page.Tweets)
	b.page.Tweets = append(b.page.Tweets, tw)
}

func (b *builder) addCollection(c model.CollectionItem) {
	if c.TweetID == "" || c.CollectionType == "" {
		return
	}
	key := c.TweetID + "\x00" + c.CollectionType + "\x00" + c.BookmarkFolderID
	if b.collections[key] {
		return
	}
	b.collections[key] = true
	b.page.Collections = append(b.page.Collections, c)
}

func (b *builder) addURL(u model.URL) {
	if u.TweetID == "" {
		return
	}
	if u.URL == "" {
		u.URL = u.ExpandedURL
	}
	if u.URL == "" {
		return
	}
	key := strings.Join([]string{u.TweetID, u.URL, u.ExpandedURL, u.DisplayURL}, "\x00")
	if b.urls[key] {
		return
	}
	b.urls[key] = true
	b.page.URLs = append(b.page.URLs, u)
}

func (b *builder) addMedia(m model.Media) {
	if m.TweetID == "" {
		return
	}
	if m.ID == "" {
		m.ID = m.TweetID + ":" + first(m.URL, m.PreviewURL, m.ExpandedURL)
	}
	if m.ID == "" || b.media[m.ID] {
		return
	}
	b.media[m.ID] = true
	b.page.Media = append(b.page.Media, m)
}

func (b *builder) addMention(m model.Mention) {
	if m.TweetID == "" || m.Username == "" {
		return
	}
	key := m.TweetID + "\x00" + strings.ToLower(m.Username)
	if b.mentions[key] {
		return
	}
	b.mentions[key] = true
	b.page.Mentions = append(b.page.Mentions, m)
	if m.UserID != "" {
		b.addUser(model.User{ID: m.UserID, Username: m.Username, DisplayName: first(m.DisplayName, m.Username)})
	}
}

func (b *builder) addHashtag(h model.Hashtag) {
	if h.TweetID == "" || h.Tag == "" {
		return
	}
	key := h.TweetID + "\x00" + strings.ToLower(h.Tag)
	if b.hashtags[key] {
		return
	}
	b.hashtags[key] = true
	b.page.Hashtags = append(b.page.Hashtags, h)
}

func (b *builder) addPseudoUser(username string) string {
	username = strings.TrimPrefix(strings.TrimSpace(username), "@")
	if username == "" {
		return "archive:unknown"
	}
	id := "archive:user:" + strings.ToLower(username)
	if !b.pseudoUserIndex[id] {
		b.pseudoUserIndex[id] = true
		b.addUser(model.User{ID: id, Username: username, DisplayName: username})
	}
	return id
}

func archiveFileKind(name string) string {
	name = normalizeArchivePath(name)
	switch {
	case accountFileRe.MatchString(name):
		return "account"
	case tweetFileRe.MatchString(name):
		return "tweets"
	case deletedTweetFileRe.MatchString(name):
		return "deleted_tweets"
	case noteTweetFileRe.MatchString(name):
		return "note_tweets"
	case deletedNoteTweetRe.MatchString(name):
		return "deleted_note_tweets"
	case likeFileRe.MatchString(name):
		return "likes"
	case bookmarkFileRe.MatchString(name):
		return "bookmarks"
	default:
		return ""
	}
}

func readZipFile(f *archivezip.File) ([]byte, error) {
	rc, err := f.Open()
	if err != nil {
		return nil, err
	}
	defer func() { _ = rc.Close() }()
	return io.ReadAll(rc)
}

func normalizeArchivePath(value string) string {
	return strings.ReplaceAll(value, "\\", "/")
}

func parseArchiveArray(raw []byte) ([]map[string]any, error) {
	payload := extractArchiveJSON(raw)
	if len(payload) == 0 {
		return nil, nil
	}
	var records []map[string]any
	if err := json.Unmarshal(payload, &records); err != nil {
		return nil, err
	}
	return records, nil
}

func extractArchiveJSON(raw []byte) []byte {
	b := bytes.TrimSpace(raw)
	if len(b) == 0 {
		return nil
	}
	if idx := bytes.IndexByte(b, '='); idx >= 0 {
		b = b[idx+1:]
	}
	b = bytes.TrimSpace(b)
	b = bytes.TrimSuffix(b, []byte(";"))
	return bytes.TrimSpace(b)
}

func parseAccount(records []map[string]any) model.User {
	for _, wrapper := range records {
		account := obj(wrapper, "account")
		if account == nil {
			continue
		}
		username := strings.TrimPrefix(str(account, "username", "screenName", "screen_name"), "@")
		id := str(account, "accountId", "account_id", "id")
		if id == "" && username != "" {
			id = "archive:account:" + strings.ToLower(username)
		}
		return model.User{
			ID:          id,
			Username:    username,
			DisplayName: first(str(account, "accountDisplayName", "displayName", "name"), username),
		}
	}
	return model.User{}
}

func parseTweets(records []map[string]any, account model.User, deleted bool, b *builder) int {
	count := 0
	for _, wrapper := range records {
		tweet := obj(wrapper, "tweet")
		if tweet == nil {
			continue
		}
		id := first(str(tweet, "id_str", "id"), str(tweet, "tweet_id"))
		if id == "" {
			continue
		}
		text := html.UnescapeString(first(str(tweet, "full_text", "text"), ""))
		isTombstone := false
		reason := ""
		if text == "" && deleted {
			text = "[Tweet unavailable]"
			isTombstone = true
			reason = "archive_deleted_tweet"
		}
		authorID := first(account.ID, str(tweet, "user_id_str", "user_id"), "unknown")
		authorUsername := account.Username
		authorDisplayName := account.DisplayName
		replyUserID := str(tweet, "in_reply_to_user_id_str", "in_reply_to_user_id")
		replyScreenName := strings.TrimPrefix(str(tweet, "in_reply_to_screen_name"), "@")
		if replyUserID != "" && replyScreenName != "" {
			b.addUser(model.User{ID: replyUserID, Username: replyScreenName, DisplayName: replyScreenName})
		}
		quotedID := str(tweet, "quoted_status_id_str", "quoted_status_id")
		retweeted := boolv(tweet, "retweeted")
		tw := model.Tweet{
			ID:                 id,
			Text:               text,
			Lang:               str(tweet, "lang"),
			AuthorID:           authorID,
			AuthorUsername:     authorUsername,
			AuthorDisplayName:  authorDisplayName,
			CreatedAt:          parseArchiveTime(str(tweet, "created_at")),
			ConversationID:     first(str(tweet, "conversation_id_str", "conversation_id"), id),
			InReplyToTweetID:   str(tweet, "in_reply_to_status_id_str", "in_reply_to_status_id"),
			InReplyToUserID:    replyUserID,
			QuotedTweetID:      quotedID,
			IsQuote:            quotedID != "",
			IsRetweet:          retweeted,
			IsReply:            str(tweet, "in_reply_to_status_id_str", "in_reply_to_status_id") != "",
			IsTombstone:        isTombstone,
			TombstoneReason:    reason,
			ReplyCount:         intv(tweet, "reply_count"),
			RetweetCount:       intv(tweet, "retweet_count"),
			LikeCount:          intv(tweet, "favorite_count"),
			QuoteCount:         intv(tweet, "quote_count"),
			BookmarkFolderID:   "",
			BookmarkFolderName: "",
		}
		b.addTweet(tw)
		b.addCollection(model.CollectionItem{TweetID: id, CollectionType: "tweet", SortIndex: first(tw.CreatedAt, id)})
		addTweetEntities(b, id, tweet)
		count++
	}
	return count
}

func parseNoteTweets(records []map[string]any, account model.User, deleted bool, b *builder) int {
	count := 0
	for _, wrapper := range records {
		note := obj(wrapper, "noteTweet")
		if note == nil {
			continue
		}
		id := first(str(note, "noteTweetId", "id", "tweetId"), str(note, "tweet_id"))
		if id == "" {
			continue
		}
		core := obj(note, "core")
		text := html.UnescapeString(first(str(core, "text"), str(note, "text")))
		isTombstone := false
		reason := ""
		if text == "" && deleted {
			text = "[Tweet unavailable]"
			isTombstone = true
			reason = "archive_deleted_note_tweet"
		}
		tw := model.Tweet{
			ID:                id,
			Text:              text,
			AuthorID:          first(account.ID, "unknown"),
			AuthorUsername:    account.Username,
			AuthorDisplayName: account.DisplayName,
			CreatedAt:         parseArchiveTime(first(str(note, "createdAt"), str(note, "created_at"))),
			ConversationID:    id,
			IsTombstone:       isTombstone,
			TombstoneReason:   reason,
		}
		b.addTweet(tw)
		b.addCollection(model.CollectionItem{TweetID: id, CollectionType: "tweet", SortIndex: first(tw.CreatedAt, id)})
		count++
	}
	return count
}

func parseCollectionTweets(records []map[string]any, collectionType string, b *builder) int {
	count := 0
	for _, wrapper := range records {
		rec := obj(wrapper, collectionType)
		if rec == nil {
			rec = obj(wrapper, collectionType+"s")
		}
		if rec == nil {
			continue
		}
		id := first(str(rec, "tweetId", "tweet_id", "id_str", "id"), str(rec, "tweetID"))
		if id == "" {
			continue
		}
		expandedURL := str(rec, "expandedUrl", "expanded_url", "url")
		username := usernameFromStatusURL(expandedURL)
		authorID := b.addPseudoUser(username)
		text := html.UnescapeString(first(str(rec, "fullText", "full_text", "text"), ""))
		tw := model.Tweet{
			ID:                id,
			Text:              text,
			AuthorID:          authorID,
			AuthorUsername:    username,
			AuthorDisplayName: username,
			ConversationID:    id,
		}
		b.addTweet(tw)
		addedAt := parseArchiveTime(first(str(rec, "likedAt"), str(rec, "bookmarkedAt"), str(rec, "createdAt"), str(rec, "created_at")))
		b.addCollection(model.CollectionItem{TweetID: id, CollectionType: collectionType, AddedAt: addedAt, SortIndex: first(addedAt, id)})
		count++
	}
	return count
}

func addTweetEntities(b *builder, tweetID string, tweet map[string]any) {
	entities := obj(tweet, "entities")
	for _, item := range arrKey(entities, "urls") {
		u, ok := item.(map[string]any)
		if !ok {
			continue
		}
		b.addURL(model.URL{
			TweetID:     tweetID,
			URL:         str(u, "url"),
			ExpandedURL: str(u, "expanded_url", "expandedUrl"),
			DisplayURL:  str(u, "display_url", "displayUrl"),
		})
	}
	for _, item := range arrKey(entities, "user_mentions") {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		username := strings.TrimPrefix(str(m, "screen_name", "username"), "@")
		if username == "" {
			continue
		}
		b.addMention(model.Mention{
			TweetID:     tweetID,
			UserID:      str(m, "id_str", "id"),
			Username:    username,
			DisplayName: str(m, "name"),
		})
	}
	for _, item := range arrKey(entities, "hashtags") {
		h, ok := item.(map[string]any)
		if !ok {
			continue
		}
		tag := strings.TrimPrefix(first(str(h, "text"), str(h, "tag")), "#")
		if tag != "" {
			b.addHashtag(model.Hashtag{TweetID: tweetID, Tag: tag})
		}
	}
	for _, item := range append(arrKey(entities, "media"), arrKey(obj(tweet, "extended_entities"), "media")...) {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		mediaURL := str(m, "media_url_https", "media_url")
		b.addMedia(model.Media{
			ID:          str(m, "id_str", "media_key", "id"),
			TweetID:     tweetID,
			MediaType:   str(m, "type"),
			URL:         mediaURL,
			ExpandedURL: str(m, "expanded_url", "expandedUrl"),
			PreviewURL:  mediaURL,
			AltText:     str(m, "ext_alt_text", "alt_text", "altText"),
		})
	}
}

func usernameFromStatusURL(value string) string {
	if value == "" {
		return ""
	}
	parsed, err := url.Parse(value)
	if err != nil {
		return ""
	}
	host := strings.ToLower(strings.TrimPrefix(parsed.Hostname(), "www."))
	if host != "x.com" && host != "twitter.com" && host != "mobile.twitter.com" {
		return ""
	}
	m := statusURLRe.FindStringSubmatch(parsed.EscapedPath())
	if len(m) < 2 {
		return ""
	}
	username, err := url.PathUnescape(m[1])
	if err != nil {
		return ""
	}
	username = strings.Trim(username, "/")
	if username == "" || username == "i" {
		return ""
	}
	return username
}

func parseArchiveTime(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339, twitterArchiveLayout} {
		if parsed, err := time.Parse(layout, value); err == nil {
			return parsed.UTC().Format(time.RFC3339)
		}
	}
	return ""
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
	if arr, ok := m[key].([]any); ok {
		return arr
	}
	return nil
}

func str(m map[string]any, keys ...string) string {
	for _, key := range keys {
		if m == nil {
			continue
		}
		switch v := m[key].(type) {
		case string:
			return v
		case float64:
			return strconv.FormatInt(int64(v), 10)
		case json.Number:
			return v.String()
		}
	}
	return ""
}

func intv(m map[string]any, key string) int64 {
	if m == nil {
		return 0
	}
	switch v := m[key].(type) {
	case float64:
		return int64(v)
	case string:
		n, _ := strconv.ParseInt(v, 10, 64)
		return n
	case json.Number:
		n, _ := v.Int64()
		return n
	default:
		return 0
	}
}

func boolv(m map[string]any, key string) bool {
	if m == nil {
		return false
	}
	v, _ := m[key].(bool)
	return v
}

func first(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
