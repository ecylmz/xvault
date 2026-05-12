package store

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/ecylmz/xvault/internal/model"
)

func TestStoreUpsertSearchAndCollections(t *testing.T) {
	ctx := context.Background()
	s, err := Open(filepath.Join(t.TempDir(), "xvault.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
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

func TestTombstoneDoesNotOverwriteRealTweet(t *testing.T) {
	ctx := context.Background()
	s, err := Open(filepath.Join(t.TempDir(), "xvault.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
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
	defer s.Close()
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
	defer s.Close()
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

func TestSearchWithMediaAndLinkFilters(t *testing.T) {
	ctx := context.Background()
	s, err := Open(filepath.Join(t.TempDir(), "xvault.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
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
}

func TestRawPayloadRoundTrip(t *testing.T) {
	ctx := context.Background()
	s, err := Open(filepath.Join(t.TempDir(), "xvault.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
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
	defer st.Close()
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
	defer st.Close()
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
}
