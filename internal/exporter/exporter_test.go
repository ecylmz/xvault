package exporter

import (
	"context"
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
