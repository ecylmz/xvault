package client

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ecylmz/xvault/internal/auth"
)

func TestFetchGraphQLRetriesTransientServerError(t *testing.T) {
	ctx := context.Background()
	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if attempts == 1 {
			http.Error(w, "temporary", http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("content-type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()

	x := New(Options{
		BaseURL:        server.URL,
		Auth:           auth.Cookies{AuthToken: "a", CT0: "c"},
		MaxRetries:     2,
		RetryBaseDelay: time.Nanosecond,
	})
	body, status, err := x.FetchGraphQL(ctx, Operation{Name: "TestOperation", QueryID: "query-id", Variables: map[string]any{"count": 1}})
	if err != nil {
		t.Fatal(err)
	}
	if status != http.StatusOK || string(body) != `{"ok":true}` || attempts != 2 {
		t.Fatalf("status=%d body=%s attempts=%d", status, body, attempts)
	}
}

func TestFetchGraphQLDoesNotRetryRateLimit(t *testing.T) {
	ctx := context.Background()
	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		http.Error(w, "rate limited", http.StatusTooManyRequests)
	}))
	defer server.Close()

	x := New(Options{
		BaseURL:        server.URL,
		Auth:           auth.Cookies{AuthToken: "a", CT0: "c"},
		MaxRetries:     2,
		RetryBaseDelay: time.Nanosecond,
	})
	_, status, err := x.FetchGraphQL(ctx, Operation{Name: "TestOperation", QueryID: "query-id", Variables: map[string]any{"count": 1}})
	if err == nil {
		t.Fatal("expected rate-limit error")
	}
	if status != http.StatusTooManyRequests || attempts != 1 {
		t.Fatalf("status=%d attempts=%d", status, attempts)
	}
}
