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
