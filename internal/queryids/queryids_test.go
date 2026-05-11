package queryids

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
)

func TestParseBundleFormats(t *testing.T) {
	js := `
	  {operationName:"Bookmarks",queryId:"RV1g3b8n_SGOHwkqKYSCFw"};
	  {queryId:"ABCD1234_efgh",operationName:"Likes"};
	  ["UserTweets"],queryId:"USERtweets123";
	  {operationName:"Bad",queryId:"no"};
	`
	got := ParseBundle(js)
	if got["Bookmarks"] != "RV1g3b8n_SGOHwkqKYSCFw" {
		t.Fatalf("Bookmarks query id not parsed: %#v", got)
	}
	if got["Likes"] != "ABCD1234_efgh" {
		t.Fatalf("Likes query id not parsed: %#v", got)
	}
	if _, ok := got["Bad"]; ok {
		t.Fatalf("invalid query id should be ignored: %#v", got)
	}
}

func TestRefreshFromPagesWritesDiscoveredIDs(t *testing.T) {
	var serverURL string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/page":
			_, _ = w.Write([]byte(`<script src="` + serverURL + `/responsive-web/client-web/main.test.js"></script>`))
		case "/responsive-web/client-web/main.test.js":
			_, _ = w.Write([]byte(`e.exports={queryId:"NEWlikes123",operationName:"Likes",operationType:"query"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	serverURL = server.URL
	path := filepath.Join(t.TempDir(), "query-ids-cache.json")
	cache := Cache{Version: 1, TTLHours: 24, Operations: map[string]Entry{}}
	got, err := RefreshFromPages(context.Background(), path, cache, []string{server.URL + "/page"}, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	if got.QueryID("Likes") != "NEWlikes123" {
		t.Fatalf("Likes query id = %q", got.QueryID("Likes"))
	}
	loaded := Load(path)
	if loaded.QueryID("Likes") != "NEWlikes123" {
		t.Fatalf("persisted Likes query id = %q", loaded.QueryID("Likes"))
	}
}
