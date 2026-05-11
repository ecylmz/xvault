package syncer

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/ecylmz/xvault/internal/auth"
	"github.com/ecylmz/xvault/internal/client"
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
	defer st.Close()
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
	results, err := st.Search(ctx, "fixture", "likes", "", "", 10, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].AuthorUsername != "alice" {
		t.Fatalf("search results = %#v", results)
	}
}

func TestSyncStopsOnRepeatedCursor(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "xvault.sqlite")
	st, err := store.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
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
