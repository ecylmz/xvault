package store

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/ecylmz/xvault/internal/model"
	_ "modernc.org/sqlite"
)

func TestStoreUpsertSearchAndCollections(t *testing.T) {
	ctx := context.Background()
	s, err := Open(filepath.Join(t.TempDir(), "xvault.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Close() }()
	page := model.ParsedPage{
		Users:       []model.User{{ID: "u1", Username: "alice", DisplayName: "Alice"}},
		Tweets:      []model.Tweet{{ID: "10001", Text: "defect prediction archive", AuthorID: "u1", AuthorUsername: "alice", AuthorDisplayName: "Alice", CreatedAt: "2026-05-11T12:00:00Z"}},
		Collections: []model.CollectionItem{{TweetID: "10001", CollectionType: "bookmark", BookmarkFolderID: "f1", BookmarkFolderName: "Research"}},
		URLs:        []model.URL{{TweetID: "10001", URL: "https://t.co/a", ExpandedURL: "https://example.com"}},
	}
	if err := s.UpsertPage(ctx, page); err != nil {
		t.Fatal(err)
	}
	if err := s.UpsertPage(ctx, page); err != nil {
		t.Fatal(err)
	}
	results, err := s.Search(ctx, "defect", "bookmarks", "", "Research", 10, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Fatalf("search results = %d", len(results))
	}
	if results[0].BookmarkFolderName != "Research" || len(results[0].Collections) != 1 {
		t.Fatalf("unexpected result: %#v", results[0])
	}
	stats, err := s.Stats(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if stats["database_size_bytes"] == nil || stats["raw_payload_size_bytes"] == nil {
		t.Fatalf("stats missing size fields: %#v", stats)
	}
	if stats["bookmarks"] != int64(1) {
		t.Fatalf("stats bookmarks = %#v", stats["bookmarks"])
	}
	folders, err := s.BookmarkFolders(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(folders) != 1 || folders[0].Name != "Research" || folders[0].Count != 1 || folders[0].ID != "f1" {
		t.Fatalf("bookmark folders = %#v", folders)
	}
	if id, ok, err := s.BookmarkFolderIDByName(ctx, "Research"); err != nil || !ok || id != "f1" {
		t.Fatalf("folder lookup id=%q ok=%v err=%v", id, ok, err)
	}
	count, err := s.CollectionCount(ctx, "bookmarks")
	if err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("collection count = %d", count)
	}
	page.Collections = []model.CollectionItem{{TweetID: "10001", CollectionType: "post"}, {TweetID: "10001", CollectionType: "thread"}}
	if err := s.UpsertPage(ctx, page); err != nil {
		t.Fatal(err)
	}
	if count, err := s.CollectionCount(ctx, "posts"); err != nil || count != 1 {
		t.Fatalf("posts count = %d err=%v", count, err)
	}
	if results, err := s.Search(ctx, "defect", "threads", "", "", 10, 0); err != nil || len(results) != 1 {
		t.Fatalf("threads search results=%d err=%v", len(results), err)
	}
	if err := s.RebuildFTS(ctx); err != nil {
		t.Fatal(err)
	}
	results, err = s.Search(ctx, "archive", "all", "", "", 10, 0)
	if err != nil || len(results) != 1 {
		t.Fatalf("search after rebuild results=%d err=%v", len(results), err)
	}
}

func TestMigrateUpgradesInitialSchema(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "old.sqlite")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	oldSchema := `
CREATE TABLE schema_migrations (version INTEGER PRIMARY KEY, applied_at TEXT NOT NULL);
INSERT INTO schema_migrations(version, applied_at) VALUES(1, '2026-01-01T00:00:00Z');
CREATE TABLE users (id TEXT PRIMARY KEY, username TEXT, display_name TEXT, avatar_url TEXT, verified INTEGER NOT NULL DEFAULT 0, protected INTEGER NOT NULL DEFAULT 0, raw_json_id TEXT, first_seen_at TEXT NOT NULL, last_seen_at TEXT NOT NULL);
CREATE TABLE tweets (id TEXT PRIMARY KEY, text TEXT NOT NULL, lang TEXT, author_id TEXT NOT NULL, created_at TEXT, conversation_id TEXT, in_reply_to_tweet_id TEXT, in_reply_to_user_id TEXT, quoted_tweet_id TEXT, retweeted_tweet_id TEXT, is_quote INTEGER NOT NULL DEFAULT 0, is_retweet INTEGER NOT NULL DEFAULT 0, is_reply INTEGER NOT NULL DEFAULT 0, is_tombstone INTEGER NOT NULL DEFAULT 0, tombstone_reason TEXT, reply_count INTEGER NOT NULL DEFAULT 0, retweet_count INTEGER NOT NULL DEFAULT 0, like_count INTEGER NOT NULL DEFAULT 0, quote_count INTEGER NOT NULL DEFAULT 0, bookmark_count INTEGER, view_count INTEGER, raw_json_id TEXT, first_seen_at TEXT NOT NULL, last_seen_at TEXT NOT NULL);
CREATE TABLE collections (tweet_id TEXT NOT NULL, collection_type TEXT NOT NULL, bookmark_folder_id TEXT, bookmark_folder_id_key TEXT NOT NULL DEFAULT '', bookmark_folder_name TEXT, added_at TEXT, synced_at TEXT NOT NULL, sort_index TEXT, source_run_id TEXT, thread_id TEXT, PRIMARY KEY(tweet_id, collection_type, bookmark_folder_id_key));
CREATE TABLE bookmark_folders (id TEXT PRIMARY KEY, name TEXT NOT NULL, sort_order INTEGER, first_seen_at TEXT NOT NULL, last_seen_at TEXT NOT NULL);
CREATE TABLE media (id TEXT PRIMARY KEY, tweet_id TEXT NOT NULL, media_type TEXT NOT NULL, url TEXT, expanded_url TEXT, preview_url TEXT, local_path TEXT, width INTEGER, height INTEGER, duration_ms INTEGER, alt_text TEXT, raw_json_id TEXT);
CREATE TABLE urls (id INTEGER PRIMARY KEY AUTOINCREMENT, tweet_id TEXT NOT NULL, url TEXT NOT NULL, expanded_url TEXT, display_url TEXT, title TEXT, description TEXT);
`
	if _, err := db.ExecContext(ctx, oldSchema); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	s, err := Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Close() }()
	for _, table := range []string{"tweets_fts", "tweets_fts_map", "threads", "thread_tweets", "raw_payloads", "sync_runs", "sync_checkpoints", "mentions", "hashtags"} {
		var name string
		if err := s.DB().QueryRowContext(ctx, `SELECT name FROM sqlite_master WHERE name=?`, table).Scan(&name); err != nil {
			t.Fatalf("missing migrated table %s: %v", table, err)
		}
	}
	var versions int
	if err := s.DB().QueryRowContext(ctx, `SELECT COUNT(*) FROM schema_migrations WHERE version IN (1,2,3,4,5)`).Scan(&versions); err != nil {
		t.Fatal(err)
	}
	if versions != 5 {
		t.Fatalf("migration versions = %d", versions)
	}
}

func TestTombstoneDoesNotOverwriteRealTweet(t *testing.T) {
	ctx := context.Background()
	s, err := Open(filepath.Join(t.TempDir(), "xvault.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Close() }()
	real := model.ParsedPage{Users: []model.User{{ID: "u1"}}, Tweets: []model.Tweet{{ID: "10001", Text: "real text", AuthorID: "u1"}}}
	tomb := model.ParsedPage{Tweets: []model.Tweet{{ID: "10001", Text: "[Tweet unavailable]", AuthorID: "u1", IsTombstone: true, TombstoneReason: "deleted"}}}
	if err := s.UpsertPage(ctx, real); err != nil {
		t.Fatal(err)
	}
	if err := s.UpsertPage(ctx, tomb); err != nil {
		t.Fatal(err)
	}
	got, err := s.Show(ctx, "10001")
	if err != nil {
		t.Fatal(err)
	}
	if got["text"] != "real text" {
		t.Fatalf("tombstone overwrote real text: %#v", got)
	}
}

func TestRealTweetReplacesTombstone(t *testing.T) {
	ctx := context.Background()
	s, err := Open(filepath.Join(t.TempDir(), "xvault.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Close() }()
	tomb := model.ParsedPage{Tweets: []model.Tweet{{ID: "10001", Text: "[Tweet unavailable]", AuthorID: "u1", IsTombstone: true, TombstoneReason: "deleted"}}}
	real := model.ParsedPage{Users: []model.User{{ID: "u1", Username: "alice"}}, Tweets: []model.Tweet{{ID: "10001", Text: "real text", AuthorID: "u1", AuthorUsername: "alice"}}}
	if err := s.UpsertPage(ctx, tomb); err != nil {
		t.Fatal(err)
	}
	if err := s.UpsertPage(ctx, real); err != nil {
		t.Fatal(err)
	}
	got, err := s.Show(ctx, "10001")
	if err != nil {
		t.Fatal(err)
	}
	if got["text"] != "real text" {
		t.Fatalf("real tweet did not replace tombstone: %#v", got)
	}
	var tombstone int
	if err := s.DB().QueryRowContext(ctx, `SELECT is_tombstone FROM tweets WHERE id='10001'`).Scan(&tombstone); err != nil {
		t.Fatal(err)
	}
	if tombstone != 0 {
		t.Fatalf("is_tombstone = %d", tombstone)
	}
}

func TestShowByURLThreadAndVacuum(t *testing.T) {
	ctx := context.Background()
	s, err := Open(filepath.Join(t.TempDir(), "xvault.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Close() }()
	page := model.ParsedPage{
		Users: []model.User{{ID: "u1", Username: "alice", DisplayName: "Alice"}},
		Tweets: []model.Tweet{
			{ID: "10001", Text: "root", AuthorID: "u1", AuthorUsername: "alice", ConversationID: "10001", CreatedAt: "2026-01-01T00:00:00Z"},
			{ID: "10002", Text: "reply", AuthorID: "u1", AuthorUsername: "alice", ConversationID: "10001", InReplyToTweetID: "10001", IsReply: true, CreatedAt: "2026-01-01T00:01:00Z"},
		},
		Collections: []model.CollectionItem{{TweetID: "10001", CollectionType: "bookmark"}, {TweetID: "10002", CollectionType: "bookmark"}},
	}
	if err := s.UpsertPage(ctx, page); err != nil {
		t.Fatal(err)
	}
	shown, err := s.ShowByURL(ctx, "https://x.com/alice/status/10001")
	if err != nil {
		t.Fatal(err)
	}
	if shown["tweet_id"] != "10001" {
		t.Fatalf("show-url tweet_id = %#v", shown["tweet_id"])
	}
	thread, err := s.Thread(ctx, "10001", "thread", 10)
	if err != nil {
		t.Fatal(err)
	}
	if thread["tweet_count"] != 2 {
		t.Fatalf("thread tweet_count = %#v", thread["tweet_count"])
	}
	var threadCount, memberCount int
	if err := s.DB().QueryRowContext(ctx, `SELECT COUNT(*) FROM threads WHERE id='thread:10001:thread' AND tweet_count=2 AND is_complete=1`).Scan(&threadCount); err != nil {
		t.Fatal(err)
	}
	if threadCount != 1 {
		t.Fatalf("persisted thread count = %d", threadCount)
	}
	if err := s.DB().QueryRowContext(ctx, `SELECT COUNT(*) FROM thread_tweets WHERE thread_id='thread:10001:thread'`).Scan(&memberCount); err != nil {
		t.Fatal(err)
	}
	if memberCount != 2 {
		t.Fatalf("persisted thread members = %d", memberCount)
	}
	shouldExpand, err := s.ShouldExpandThread(ctx, "10001", "thread", 10, false)
	if err != nil {
		t.Fatal(err)
	}
	if shouldExpand {
		t.Fatal("complete thread with same limit should be skipped")
	}
	shouldExpand, err = s.ShouldExpandThread(ctx, "10001", "thread", 10, true)
	if err != nil {
		t.Fatal(err)
	}
	if !shouldExpand {
		t.Fatal("refresh should force thread expansion")
	}
	shouldExpand, err = s.ShouldExpandThread(ctx, "10002", "thread", 10, false)
	if err != nil {
		t.Fatal(err)
	}
	if !shouldExpand {
		t.Fatal("missing thread should expand")
	}
	ids, err := s.CollectionTweetIDs(ctx, "bookmarks", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 2 {
		t.Fatalf("collection tweet ids = %#v", ids)
	}
	if err := s.Vacuum(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestShowIncludesLinksMediaAndQuotedSummary(t *testing.T) {
	ctx := context.Background()
	s, err := Open(filepath.Join(t.TempDir(), "xvault.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Close() }()
	page := model.ParsedPage{
		Users: []model.User{{ID: "u1", Username: "alice", DisplayName: "Alice"}, {ID: "u2", Username: "bob", DisplayName: "Bob"}},
		Tweets: []model.Tweet{
			{ID: "10001", Text: "outer tweet", AuthorID: "u1", AuthorUsername: "alice", ConversationID: "10001", QuotedTweetID: "10002", IsQuote: true, RawJSONID: "raw1"},
			{ID: "10002", Text: "quoted tweet", AuthorID: "u2", AuthorUsername: "bob"},
		},
		Collections: []model.CollectionItem{{TweetID: "10001", CollectionType: "bookmark"}},
		URLs:        []model.URL{{TweetID: "10001", URL: "https://t.co/a", ExpandedURL: "https://example.com/a", DisplayURL: "example.com/a"}},
		Media:       []model.Media{{ID: "m1", TweetID: "10001", MediaType: "photo", URL: "https://pbs.twimg.com/a.jpg", PreviewURL: "https://pbs.twimg.com/a.jpg", AltText: "Alt text"}},
		Mentions:    []model.Mention{{TweetID: "10001", UserID: "u2", Username: "bob", DisplayName: "Bob"}},
		Hashtags:    []model.Hashtag{{TweetID: "10001", Tag: "XVault"}},
	}
	if err := s.UpsertPage(ctx, page); err != nil {
		t.Fatal(err)
	}
	thread, err := s.Thread(ctx, "10001", "thread", 10)
	if err != nil {
		t.Fatal(err)
	}
	if thread["tweet_count"] != 1 {
		t.Fatalf("thread = %#v", thread)
	}
	got, err := s.Show(ctx, "10001")
	if err != nil {
		t.Fatal(err)
	}
	links, ok := got["links"].([]model.URL)
	if !ok || len(links) != 1 || links[0].ExpandedURL != "https://example.com/a" {
		t.Fatalf("links = %#v", got["links"])
	}
	media, ok := got["media"].([]model.Media)
	if !ok || len(media) != 1 || media[0].AltText != "Alt text" {
		t.Fatalf("media = %#v", got["media"])
	}
	mentions, ok := got["mentions"].([]model.Mention)
	if !ok || len(mentions) != 1 || mentions[0].Username != "bob" {
		t.Fatalf("mentions = %#v", got["mentions"])
	}
	hashtags, ok := got["hashtags"].([]model.Hashtag)
	if !ok || len(hashtags) != 1 || hashtags[0].Tag != "XVault" {
		t.Fatalf("hashtags = %#v", got["hashtags"])
	}
	quoted, ok := got["quoted_tweet"].(map[string]any)
	if !ok || quoted["tweet_id"] != "10002" || quoted["author_username"] != "bob" {
		t.Fatalf("quoted = %#v", got["quoted_tweet"])
	}
	threads, ok := got["threads"].([]map[string]any)
	if !ok || len(threads) != 1 || threads[0]["thread_id"] != "thread:10001:thread" {
		t.Fatalf("threads = %#v", got["threads"])
	}
	if paths, ok := got["local_export_paths"].(map[string]string); !ok || len(paths) != 0 {
		t.Fatalf("local_export_paths = %#v", got["local_export_paths"])
	}
	if got["raw_json_available"] != true {
		t.Fatalf("raw flag = %#v", got["raw_json_available"])
	}
}

func TestSearchWithMediaAndLinkFilters(t *testing.T) {
	ctx := context.Background()
	s, err := Open(filepath.Join(t.TempDir(), "xvault.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Close() }()
	page := model.ParsedPage{
		Users:       []model.User{{ID: "u1", Username: "alice"}},
		Tweets:      []model.Tweet{{ID: "10001", Text: "filter fixture", AuthorID: "u1", CreatedAt: "2026-01-01T00:00:00Z"}},
		Collections: []model.CollectionItem{{TweetID: "10001", CollectionType: "bookmark"}},
		URLs:        []model.URL{{TweetID: "10001", URL: "https://t.co/a", ExpandedURL: "https://example.com"}},
		Media:       []model.Media{{ID: "m1", TweetID: "10001", MediaType: "photo", URL: "https://pbs.twimg.com/a.jpg"}},
	}
	if err := s.UpsertPage(ctx, page); err != nil {
		t.Fatal(err)
	}
	results, err := s.SearchWithFilters(ctx, "filter", "bookmarks", "", "", "2026-01-01", "2026-12-31", true, true, 10, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || !results[0].HasMedia || !results[0].HasLinks {
		t.Fatalf("filtered results = %#v", results)
	}
	if results[0].Score <= 0 {
		t.Fatalf("score = %v", results[0].Score)
	}
}

func TestSearchRankingUsesCollectionPriority(t *testing.T) {
	ctx := context.Background()
	s, err := Open(filepath.Join(t.TempDir(), "xvault.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Close() }()
	page := model.ParsedPage{
		Users: []model.User{{ID: "u1", Username: "alice"}},
		Tweets: []model.Tweet{
			{ID: "10001", Text: "ranking fixture", AuthorID: "u1", AuthorUsername: "alice", CreatedAt: "2026-01-01T00:00:00Z"},
			{ID: "10002", Text: "ranking fixture", AuthorID: "u1", AuthorUsername: "alice", CreatedAt: "2026-01-02T00:00:00Z"},
		},
		Collections: []model.CollectionItem{
			{TweetID: "10001", CollectionType: "bookmark"},
			{TweetID: "10002", CollectionType: "like"},
		},
	}
	if err := s.UpsertPage(ctx, page); err != nil {
		t.Fatal(err)
	}
	results, err := s.Search(ctx, "ranking", "all", "", "", 10, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 2 {
		t.Fatalf("results = %#v", results)
	}
	if results[0].TweetID != "10001" || results[0].Score <= results[1].Score {
		t.Fatalf("ranked results = %#v", results)
	}
}

func TestRawPayloadRoundTrip(t *testing.T) {
	ctx := context.Background()
	s, err := Open(filepath.Join(t.TempDir(), "xvault.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Close() }()
	id, err := s.SaveRaw(ctx, "graphql", "Test", []byte(`{"ok":true}`))
	if err != nil {
		t.Fatal(err)
	}
	raw, err := s.RawPayload(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != `{"ok":true}` {
		t.Fatalf("raw = %s", raw)
	}
}

func TestCheckpointRoundTrip(t *testing.T) {
	ctx := context.Background()
	st, err := Open(filepath.Join(t.TempDir(), "xvault.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = st.Close() }()
	cp := Checkpoint{
		CollectionType: "bookmark",
		Cursor:         "CURSOR-1",
		LastTweetID:    "100",
		LastSortIndex:  "900",
		TotalSeen:      25,
	}
	if err := st.SaveCheckpoint(ctx, cp); err != nil {
		t.Fatal(err)
	}
	got, ok, err := st.LoadCheckpoint(ctx, "bookmark")
	if err != nil {
		t.Fatal(err)
	}
	if !ok || got.Cursor != "CURSOR-1" || got.LastTweetID != "100" || got.LastSortIndex != "900" || got.TotalSeen != 25 || got.Status != "in_progress" || got.UpdatedAt == "" {
		t.Fatalf("checkpoint = %#v, ok=%v", got, ok)
	}
	checkpoints, err := st.ListCheckpoints(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(checkpoints) != 1 || checkpoints[0].CollectionType != "bookmark" {
		t.Fatalf("checkpoints = %#v", checkpoints)
	}
	if err := st.ClearCheckpoint(ctx, "bookmark"); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := st.LoadCheckpoint(ctx, "bookmark"); err != nil || ok {
		t.Fatalf("expected cleared checkpoint, ok=%v err=%v", ok, err)
	}
}

func TestSyncRunLifecycle(t *testing.T) {
	ctx := context.Background()
	st, err := Open(filepath.Join(t.TempDir(), "xvault.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = st.Close() }()
	id, err := st.StartSyncRun(ctx, "like", "incremental")
	if err != nil {
		t.Fatal(err)
	}
	if id == "" {
		t.Fatal("empty sync run id")
	}
	if err := st.FinishSyncRun(ctx, SyncRun{
		ID:             id,
		Status:         "partial",
		PagesFetched:   2,
		TweetsSeen:     10,
		TweetsInserted: 9,
		RateLimitCount: 1,
		ErrorCode:      "RATE_LIMITED",
		ErrorMessage:   "rate limited",
	}); err != nil {
		t.Fatal(err)
	}
	got, err := st.GetSyncRun(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != "partial" || got.PagesFetched != 2 || got.TweetsSeen != 10 || got.ErrorCode != "RATE_LIMITED" {
		t.Fatalf("sync run = %#v", got)
	}
	runs, err := st.ListSyncRuns(ctx, "likes", "partial", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(runs) != 1 || runs[0].ID != id || runs[0].FinishedAt == "" || runs[0].StartedAt == "" {
		t.Fatalf("sync runs = %#v", runs)
	}
	if err := st.FinishSyncRun(ctx, SyncRun{ID: id, Status: "failed", ErrorCode: "AUTH_EXPIRED", ErrorMessage: `{"errors":[{"message":"Could not authenticate you"}]}`}); err != nil {
		t.Fatal(err)
	}
	updated, err := st.SanitizeSyncRunErrors(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if updated != 1 {
		t.Fatalf("updated = %d", updated)
	}
	got, err = st.GetSyncRun(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if got.ErrorMessage != "authentication cookies were rejected by X" {
		t.Fatalf("sanitized run = %#v", got)
	}
	successID, err := st.StartSyncRun(ctx, "like", "incremental")
	if err != nil {
		t.Fatal(err)
	}
	if err := st.FinishSyncRun(ctx, SyncRun{ID: successID, Status: "success", PagesFetched: 1, TweetsSeen: 2}); err != nil {
		t.Fatal(err)
	}
	success, ok, err := st.LastSuccessfulSync(ctx, "likes")
	if err != nil {
		t.Fatal(err)
	}
	if !ok || success.ID != successID || success.Status != "success" {
		t.Fatalf("last success = %#v ok=%v", success, ok)
	}
	unresolved, err := st.UnresolvedFailedSyncRuns(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(unresolved) != 0 {
		t.Fatalf("expected later success to resolve failed run, got %#v", unresolved)
	}
	bookmarkFailure, err := st.StartSyncRun(ctx, "bookmark", "incremental")
	if err != nil {
		t.Fatal(err)
	}
	if err := st.FinishSyncRun(ctx, SyncRun{ID: bookmarkFailure, Status: "failed", ErrorCode: "AUTH_EXPIRED"}); err != nil {
		t.Fatal(err)
	}
	unresolved, err = st.UnresolvedFailedSyncRuns(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(unresolved) != 1 || unresolved[0].ID != bookmarkFailure {
		t.Fatalf("unresolved failures = %#v", unresolved)
	}
	stats, err := st.Stats(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if stats["incomplete_syncs"] != int64(2) {
		t.Fatalf("incomplete_syncs = %#v", stats["incomplete_syncs"])
	}
	lastSync, ok := stats["last_sync"].(map[string]string)
	if !ok || lastSync["like"] == "" || lastSync["bookmark"] == "" {
		t.Fatalf("last_sync = %#v", stats["last_sync"])
	}
	lastSuccess, ok := stats["last_successful_sync"].(map[string]string)
	if !ok || lastSuccess["like"] == "" {
		t.Fatalf("last_successful_sync = %#v", stats["last_successful_sync"])
	}
}
