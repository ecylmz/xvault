package syncer

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/ecylmz/xvault/internal/auth"
	"github.com/ecylmz/xvault/internal/client"
	"github.com/ecylmz/xvault/internal/model"
	"github.com/ecylmz/xvault/internal/queryids"
	"github.com/ecylmz/xvault/internal/store"
)

func TestSyncLikesWithReplayServer(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "xvault.sqlite")
	st, err := store.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = st.Close() }()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/i/api/graphql/test-likes/Likes" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("content-type", "application/json")
		_, _ = w.Write([]byte(`[{"__typename":"Tweet","rest_id":"10001","core":{"user_results":{"result":{"rest_id":"20001","core":{"screen_name":"alice","name":"Alice"}}}},"legacy":{"full_text":"fixture replay tweet","created_at":"2026-01-01T00:00:00Z","user_id_str":"20001","conversation_id_str":"10001"}}]`))
	}))
	defer server.Close()
	qids := queryids.Cache{Operations: map[string]queryids.Entry{"Likes": {QueryID: "test-likes"}}}
	x := client.New(client.Options{BaseURL: server.URL, Auth: auth.Cookies{AuthToken: "a", CT0: "c", TWID: "u=1"}})
	result, err := New(x, st, qids, dbPath, "u=1", 0).Sync(ctx, Request{Collection: "likes", Count: 10})
	if err != nil {
		t.Fatal(err)
	}
	if result.PagesFetched != 1 || result.TweetsSeen != 1 {
		t.Fatalf("result = %#v", result)
	}
	if result.RunID == "" {
		t.Fatalf("missing run id: %#v", result)
	}
	run, err := st.GetSyncRun(ctx, result.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if run.Status != "success" || run.PagesFetched != 1 || run.TweetsSeen != 1 {
		t.Fatalf("sync run = %#v", run)
	}
	results, err := st.Search(ctx, "fixture", "likes", "", "", 10, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].AuthorUsername != "alice" {
		t.Fatalf("search results = %#v", results)
	}
	var sourceRunID string
	if err := st.DB().QueryRowContext(ctx, `SELECT source_run_id FROM collections WHERE tweet_id='10001' AND collection_type='like'`).Scan(&sourceRunID); err != nil {
		t.Fatal(err)
	}
	if sourceRunID != result.RunID {
		t.Fatalf("source_run_id = %q, want %q", sourceRunID, result.RunID)
	}
}

func TestSyncTweetsUsesAuthenticatedUserTimelineAndFiltersOwnTweets(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "xvault.sqlite")
	st, err := store.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = st.Close() }()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/i/api/graphql/test-user/UserTweets" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		var vars map[string]any
		if err := json.Unmarshal([]byte(r.URL.Query().Get("variables")), &vars); err != nil {
			t.Fatal(err)
		}
		if vars["userId"] != "789" || vars["withV2Timeline"] != true || vars["includePromotedContent"] != true {
			t.Fatalf("variables = %#v", vars)
		}
		var toggles map[string]any
		if err := json.Unmarshal([]byte(r.URL.Query().Get("fieldToggles")), &toggles); err != nil {
			t.Fatal(err)
		}
		if toggles["withArticleRichContentState"] != true {
			t.Fatalf("field toggles = %#v", toggles)
		}
		w.Header().Set("content-type", "application/json")
		_, _ = w.Write([]byte(`[{"__typename":"Tweet","rest_id":"10001","core":{"user_results":{"result":{"rest_id":"789","core":{"screen_name":"me","name":"Me"}}}},"legacy":{"full_text":"own tweet","created_at":"2026-01-01T00:00:00Z","user_id_str":"789","conversation_id_str":"10001"}},{"__typename":"Tweet","rest_id":"10002","core":{"user_results":{"result":{"rest_id":"789","core":{"screen_name":"me","name":"Me"}}}},"legacy":{"full_text":"reply tweet","created_at":"2026-01-01T00:01:00Z","user_id_str":"789","conversation_id_str":"10001","in_reply_to_status_id_str":"10001"}},{"__typename":"Tweet","rest_id":"10003","core":{"user_results":{"result":{"rest_id":"789","core":{"screen_name":"me","name":"Me"}}}},"legacy":{"full_text":"RT @alice: reposted","created_at":"2026-01-01T00:02:00Z","user_id_str":"789","conversation_id_str":"10003","retweeted_status_result":{"result":{"rest_id":"20001","core":{"user_results":{"result":{"rest_id":"30001","core":{"screen_name":"alice","name":"Alice"}}}},"legacy":{"full_text":"reposted original","user_id_str":"30001","conversation_id_str":"20001"}}}}}]`))
	}))
	defer server.Close()
	qids := queryids.Cache{Operations: map[string]queryids.Entry{"UserTweets": {QueryID: "test-user"}}}
	x := client.New(client.Options{BaseURL: server.URL, Auth: auth.Cookies{AuthToken: "a", CT0: "c", TWID: "u=789"}})
	result, err := New(x, st, qids, dbPath, "u%3D789", 0).Sync(ctx, Request{Collection: "tweets", Count: 10})
	if err != nil {
		t.Fatal(err)
	}
	if result.PagesFetched != 1 || result.TweetsSeen != 1 {
		t.Fatalf("result = %#v", result)
	}
	if count, err := st.CollectionCount(ctx, "tweets"); err != nil || count != 1 {
		t.Fatalf("tweets count=%d err=%v", count, err)
	}
	if count, err := st.CollectionCount(ctx, "reposts"); err != nil || count != 0 {
		t.Fatalf("reposts count=%d err=%v", count, err)
	}
}

func TestSyncRepostsStoresOriginalContentInRepostCollection(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "xvault.sqlite")
	st, err := store.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = st.Close() }()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/i/api/graphql/test-user/UserTweets" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("content-type", "application/json")
		_, _ = w.Write([]byte(`[{"__typename":"Tweet","rest_id":"10001","core":{"user_results":{"result":{"rest_id":"789","core":{"screen_name":"me","name":"Me"}}}},"legacy":{"full_text":"own tweet","created_at":"2026-01-01T00:00:00Z","user_id_str":"789","conversation_id_str":"10001"}},{"__typename":"Tweet","rest_id":"10002","core":{"user_results":{"result":{"rest_id":"789","core":{"screen_name":"me","name":"Me"}}}},"legacy":{"full_text":"RT @alice: searchable original","created_at":"2026-01-01T00:02:00Z","user_id_str":"789","conversation_id_str":"10002","retweeted_status_result":{"result":{"rest_id":"20001","core":{"user_results":{"result":{"rest_id":"30001","core":{"screen_name":"alice","name":"Alice"}}}},"legacy":{"full_text":"searchable original","user_id_str":"30001","conversation_id_str":"20001","favorite_count":7,"entities":{"urls":[{"url":"https://t.co/o","expanded_url":"https://example.com/original","display_url":"example.com/original"}]}}}}}}]`))
	}))
	defer server.Close()
	qids := queryids.Cache{Operations: map[string]queryids.Entry{"UserTweets": {QueryID: "test-user"}}}
	x := client.New(client.Options{BaseURL: server.URL, Auth: auth.Cookies{AuthToken: "a", CT0: "c", TWID: "u=789"}})
	result, err := New(x, st, qids, dbPath, "u=789", 0).Sync(ctx, Request{Collection: "reposts", Count: 10})
	if err != nil {
		t.Fatal(err)
	}
	if result.PagesFetched != 1 || result.TweetsSeen != 1 {
		t.Fatalf("result = %#v", result)
	}
	rows, err := st.Search(ctx, "searchable", "reposts", "", "", 10, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].TweetID != "10002" || rows[0].AuthorUsername != "alice" || rows[0].LikeCount != 7 {
		t.Fatalf("search rows = %#v", rows)
	}
	var reposted string
	if err := st.DB().QueryRowContext(ctx, `SELECT COALESCE(retweeted_tweet_id,'') FROM tweets WHERE id='10002'`).Scan(&reposted); err != nil {
		t.Fatal(err)
	}
	if reposted != "20001" {
		t.Fatalf("retweeted_tweet_id = %q", reposted)
	}
}

func TestSyncBookmarkFolderUsesFolderTimelineAndMetadata(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "xvault.sqlite")
	st, err := store.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = st.Close() }()
	var folderID string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/i/api/graphql/test-folder/BookmarkFolderTimeline" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		var vars map[string]any
		if err := json.Unmarshal([]byte(r.URL.Query().Get("variables")), &vars); err != nil {
			t.Fatal(err)
		}
		folderID, _ = vars["folderId"].(string)
		w.Header().Set("content-type", "application/json")
		_, _ = w.Write([]byte(`[{"__typename":"Tweet","rest_id":"10001","core":{"user_results":{"result":{"rest_id":"20001","core":{"screen_name":"alice","name":"Alice"}}}},"legacy":{"full_text":"folder bookmark","created_at":"2026-01-01T00:00:00Z","user_id_str":"20001","conversation_id_str":"10001"}}]`))
	}))
	defer server.Close()
	qids := queryids.Cache{Operations: map[string]queryids.Entry{"BookmarkFolderTimeline": {QueryID: "test-folder"}}}
	x := client.New(client.Options{BaseURL: server.URL, Auth: auth.Cookies{AuthToken: "a", CT0: "c", TWID: "u=1"}})
	result, err := New(x, st, qids, dbPath, "u=1", 0).Sync(ctx, Request{Collection: "bookmarks", Count: 10, Folder: "Research", FolderID: "folder-1"})
	if err != nil {
		t.Fatal(err)
	}
	if folderID != "folder-1" || result.FoldersSeen != 1 {
		t.Fatalf("folderID=%q result=%#v", folderID, result)
	}
	folders, err := st.BookmarkFolders(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(folders) != 1 || folders[0].ID != "folder-1" || folders[0].Name != "Research" || folders[0].Count != 1 {
		t.Fatalf("folders = %#v", folders)
	}
}

func TestSyncStopsOnRepeatedCursor(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "xvault.sqlite")
	st, err := store.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = st.Close() }()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("content-type", "application/json")
		_, _ = w.Write([]byte(`[{"__typename":"Tweet","rest_id":"10001","core":{"user_results":{"result":{"rest_id":"20001","core":{"screen_name":"alice","name":"Alice"}}}},"legacy":{"full_text":"same cursor tweet","user_id_str":"20001","conversation_id_str":"10001"}},{"entryType":"TimelineTimelineCursor","cursorType":"Bottom","value":"CURSOR"}]`))
	}))
	defer server.Close()
	qids := queryids.Cache{Operations: map[string]queryids.Entry{"Likes": {QueryID: "test-likes"}}}
	x := client.New(client.Options{BaseURL: server.URL, Auth: auth.Cookies{AuthToken: "a", CT0: "c", TWID: "u=1"}})
	result, err := New(x, st, qids, dbPath, "u=1", time.Millisecond).Sync(ctx, Request{Collection: "likes", All: true})
	if err != nil {
		t.Fatal(err)
	}
	if result.PagesFetched != 2 {
		t.Fatalf("expected duplicate cursor stop after 2 pages, got %#v", result)
	}
}

func TestSyncPersistsAndResumesCheckpoint(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "xvault.sqlite")
	st, err := store.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = st.Close() }()
	var seenCursorOnResume bool
	requestCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		var vars map[string]any
		if err := json.Unmarshal([]byte(r.URL.Query().Get("variables")), &vars); err != nil {
			t.Fatal(err)
		}
		if requestCount == 2 && vars["cursor"] == "CURSOR-1" {
			seenCursorOnResume = true
		}
		w.Header().Set("content-type", "application/json")
		switch requestCount {
		case 1:
			_, _ = w.Write([]byte(`[{"__typename":"Tweet","rest_id":"10001","core":{"user_results":{"result":{"rest_id":"20001","core":{"screen_name":"alice","name":"Alice"}}}},"legacy":{"full_text":"first page","user_id_str":"20001","conversation_id_str":"10001"}},{"entryType":"TimelineTimelineCursor","cursorType":"Bottom","value":"CURSOR-1"}]`))
		default:
			_, _ = w.Write([]byte(`[{"__typename":"Tweet","rest_id":"10002","core":{"user_results":{"result":{"rest_id":"20001","core":{"screen_name":"alice","name":"Alice"}}}},"legacy":{"full_text":"second page","user_id_str":"20001","conversation_id_str":"10002"}}]`))
		}
	}))
	defer server.Close()
	qids := queryids.Cache{Operations: map[string]queryids.Entry{"Likes": {QueryID: "test-likes"}}}
	x := client.New(client.Options{BaseURL: server.URL, Auth: auth.Cookies{AuthToken: "a", CT0: "c", TWID: "u=1"}})
	sy := New(x, st, qids, dbPath, "u=1", 0)
	first, err := sy.Sync(ctx, Request{Collection: "likes", All: true, MaxPages: 1})
	if err != nil {
		t.Fatal(err)
	}
	if first.NextCursor != "CURSOR-1" || first.CheckpointCleared {
		t.Fatalf("first result = %#v", first)
	}
	run, err := st.GetSyncRun(ctx, first.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if run.Status != "partial" || run.PagesFetched != 1 {
		t.Fatalf("first run = %#v", run)
	}
	if cp, ok, err := st.LoadCheckpoint(ctx, "like"); err != nil || !ok || cp.Cursor != "CURSOR-1" {
		t.Fatalf("checkpoint = %#v ok=%v err=%v", cp, ok, err)
	}
	second, err := sy.Sync(ctx, Request{Collection: "likes", All: true})
	if err != nil {
		t.Fatal(err)
	}
	if !seenCursorOnResume || !second.CheckpointCleared {
		t.Fatalf("resume seen=%v second=%#v", seenCursorOnResume, second)
	}
	if _, ok, err := st.LoadCheckpoint(ctx, "like"); err != nil || ok {
		t.Fatalf("expected checkpoint cleared, ok=%v err=%v", ok, err)
	}
}

func TestSyncFeedStopsAfterTwoPagesOlderThanBoundary(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "xvault.sqlite")
	st, err := store.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = st.Close() }()
	requestCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		if r.URL.Path != "/i/api/graphql/test-feed/HomeLatestTimeline" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("content-type", "application/json")
		switch requestCount {
		case 1:
			_, _ = w.Write([]byte(`[{"__typename":"Tweet","rest_id":"20001","core":{"user_results":{"result":{"rest_id":"30001","core":{"screen_name":"alice","name":"Alice"}}}},"legacy":{"full_text":"old feed page one","created_at":"Mon Jan 02 15:04:05 +0000 2020","user_id_str":"30001","conversation_id_str":"20001"}},{"entryType":"TimelineTimelineCursor","cursorType":"Bottom","value":"CURSOR-1"}]`))
		default:
			_, _ = w.Write([]byte(`[{"__typename":"Tweet","rest_id":"20002","core":{"user_results":{"result":{"rest_id":"30001","core":{"screen_name":"alice","name":"Alice"}}}},"legacy":{"full_text":"old feed page two","created_at":"2020-01-03T15:04:05Z","user_id_str":"30001","conversation_id_str":"20002"}},{"entryType":"TimelineTimelineCursor","cursorType":"Bottom","value":"CURSOR-2"}]`))
		}
	}))
	defer server.Close()
	qids := queryids.Cache{Operations: map[string]queryids.Entry{"HomeLatestTimeline": {QueryID: "test-feed"}}}
	x := client.New(client.Options{BaseURL: server.URL, Auth: auth.Cookies{AuthToken: "a", CT0: "c", TWID: "u=1"}})
	result, err := New(x, st, qids, dbPath, "u=1", 0).Sync(ctx, Request{Collection: "feed", Count: 100, FeedHours: 24})
	if err != nil {
		t.Fatal(err)
	}
	if requestCount != 2 || result.PagesFetched != 2 || !result.CheckpointCleared || result.NextCursor != "" {
		t.Fatalf("feed boundary result=%#v requests=%d", result, requestCount)
	}
	if count, err := st.CollectionCount(ctx, "feed"); err != nil || count != 2 {
		t.Fatalf("feed count=%d err=%v", count, err)
	}
}

func TestSyncRunStoresSanitizedFailureMessage(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "xvault.sqlite")
	st, err := store.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = st.Close() }()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"errors":[{"message":"Could not authenticate you","code":32}]}`, http.StatusUnauthorized)
	}))
	defer server.Close()
	qids := queryids.Cache{Operations: map[string]queryids.Entry{"Likes": {QueryID: "test-likes"}}}
	x := client.New(client.Options{BaseURL: server.URL, Auth: auth.Cookies{AuthToken: "a", CT0: "c", TWID: "u=1"}, MaxRetries: 0, RetryBaseDelay: time.Nanosecond})
	result, err := New(x, st, qids, dbPath, "u=1", 0).Sync(ctx, Request{Collection: "likes", Count: 10})
	if err == nil {
		t.Fatal("expected auth error")
	}
	run, err := st.GetSyncRun(ctx, result.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if run.ErrorCode != "AUTH_EXPIRED" || run.ErrorMessage != "authentication cookies were rejected by X" {
		t.Fatalf("sync run = %#v", run)
	}
}

func TestEntityFiltersFollowKeptTweets(t *testing.T) {
	keep := map[string]bool{"10001": true}
	mentions := filterMentions([]model.Mention{
		{TweetID: "10001", Username: "alice"},
		{TweetID: "10002", Username: "bob"},
	}, keep)
	if len(mentions) != 1 || mentions[0].Username != "alice" {
		t.Fatalf("mentions = %#v", mentions)
	}
	hashtags := filterHashtags([]model.Hashtag{
		{TweetID: "10001", Tag: "Keep"},
		{TweetID: "10002", Tag: "Drop"},
	}, keep)
	if len(hashtags) != 1 || hashtags[0].Tag != "Keep" {
		t.Fatalf("hashtags = %#v", hashtags)
	}
}
