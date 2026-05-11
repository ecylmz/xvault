package app

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/ecylmz/xvault/internal/config"
)

func TestVersionJSON(t *testing.T) {
	code := Execute([]string{"version", "--json"})
	if code != 0 {
		t.Fatalf("exit code = %d", code)
	}
}

func TestShowIncludeRawBlocked(t *testing.T) {
	dir := t.TempDir()
	db := filepath.Join(dir, "x.sqlite")
	code := Execute([]string{"--db", db, "show", "123", "--include-raw", "--json"})
	if code == 0 {
		t.Fatal("expected RAW_OUTPUT_BLOCKED or not found failure")
	}
}

func TestErrorEnvelopeDoesNotLeakKnownSecretWords(t *testing.T) {
	var buf bytes.Buffer
	writeJSONError(&buf, "test", "AUTH_MISSING", "Authentication cookies appear to be expired.", false)
	var env Envelope
	if err := json.Unmarshal(buf.Bytes(), &env); err != nil {
		t.Fatal(err)
	}
	if env.OK || env.Error == nil || env.Error.Code != "AUTH_MISSING" {
		t.Fatalf("bad envelope: %#v", env)
	}
	if bytes.Contains(buf.Bytes(), []byte("auth_token=")) {
		t.Fatalf("secret-like output leaked: %s", buf.String())
	}
	_ = os.Stdout
}

func TestSyncCountForCollectionUsesConfigDefaults(t *testing.T) {
	cfg := config.Default()
	count, all := syncCountForCollection(cfg, "likes", 100, false, false)
	if count != 0 || !all {
		t.Fatalf("likes defaults count=%d all=%v", count, all)
	}
	count, all = syncCountForCollection(cfg, "tweets", 100, false, false)
	if count != cfg.Sync.DefaultCount || all {
		t.Fatalf("tweets defaults count=%d all=%v", count, all)
	}
	count, all = syncCountForCollection(cfg, "likes", 25, false, true)
	if count != 25 || all {
		t.Fatalf("flag count=%d all=%v", count, all)
	}
	count, all = syncCountForCollection(cfg, "likes", 25, true, true)
	if count != 25 || !all {
		t.Fatalf("flag all count=%d all=%v", count, all)
	}
}
