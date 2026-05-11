package exporter

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ecylmz/xvault/internal/model"
	"github.com/ecylmz/xvault/internal/store"
)

func TestExportsWriteExpectedFiles(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(filepath.Join(t.TempDir(), "xvault.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	page := model.ParsedPage{
		Users:       []model.User{{ID: "u1", Username: "alice", DisplayName: "Alice"}},
		Tweets:      []model.Tweet{{ID: "10001", Text: "export fixture tweet", AuthorID: "u1", AuthorUsername: "alice", AuthorDisplayName: "Alice", CreatedAt: "2026-01-01T00:00:00Z"}},
		Collections: []model.CollectionItem{{TweetID: "10001", CollectionType: "bookmark"}},
	}
	if err := st.UpsertPage(ctx, page); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	jsonPath := filepath.Join(dir, "archive.json")
	csvPath := filepath.Join(dir, "archive.csv")
	htmlPath := filepath.Join(dir, "archive.html")
	mdDir := filepath.Join(dir, "hermes")
	if data, err := JSON(ctx, st, "all", jsonPath, true); err != nil || data["count"] != 1 {
		t.Fatalf("json data=%#v err=%v", data, err)
	}
	if data, err := CSV(ctx, st, "all", csvPath); err != nil || data["count"] != 1 {
		t.Fatalf("csv data=%#v err=%v", data, err)
	}
	if data, err := HTML(ctx, st, "all", htmlPath); err != nil || data["count"] != 1 {
		t.Fatalf("html data=%#v err=%v", data, err)
	}
	if data, err := Markdown(ctx, st, "all", mdDir, true); err != nil || data["count"] != 1 {
		t.Fatalf("markdown data=%#v err=%v", data, err)
	}
	for _, path := range []string{jsonPath, csvPath, htmlPath, filepath.Join(mdDir, "index.jsonl")} {
		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(b), "10001") {
			t.Fatalf("%s does not contain tweet id", path)
		}
	}
}

func TestBackupCreatesIntegrityCheckedSQLiteCopy(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(filepath.Join(t.TempDir(), "xvault.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	page := model.ParsedPage{
		Users:       []model.User{{ID: "u1", Username: "alice"}},
		Tweets:      []model.Tweet{{ID: "10001", Text: "backup fixture tweet", AuthorID: "u1", AuthorUsername: "alice"}},
		Collections: []model.CollectionItem{{TweetID: "10001", CollectionType: "like"}},
	}
	if err := st.UpsertPage(ctx, page); err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(t.TempDir(), "backup.sqlite")
	data, err := Backup(ctx, st, output)
	if err != nil {
		t.Fatal(err)
	}
	if data["output"] != output {
		t.Fatalf("backup output = %#v", data)
	}
	db, err := sql.Open("sqlite", output)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var integrity string
	if err := db.QueryRowContext(ctx, "PRAGMA integrity_check").Scan(&integrity); err != nil {
		t.Fatal(err)
	}
	if integrity != "ok" {
		t.Fatalf("integrity = %q", integrity)
	}
	var count int
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM tweets WHERE id='10001'").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("backup tweet count = %d", count)
	}
	if _, err := Backup(ctx, st, output); err == nil {
		t.Fatal("expected existing backup path to be rejected")
	}
}
