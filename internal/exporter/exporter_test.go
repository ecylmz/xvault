package exporter

import (
	"context"
	"database/sql"
	"fmt"
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
	obsidianDir := filepath.Join(dir, "obsidian")
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
	singlePath := filepath.Join(dir, "archive.md")
	if data, err := MarkdownSingleWithFolder(ctx, st, "all", "", singlePath); err != nil || data["count"] != 1 || data["mode"] != "single" {
		t.Fatalf("single markdown data=%#v err=%v", data, err)
	}
	if data, err := ObsidianWithFolder(ctx, st, "all", "", obsidianDir, true); err != nil || data["count"] != 1 || data["with_index_jsonl"] != true {
		t.Fatalf("obsidian data=%#v err=%v", data, err)
	}
	mdPath := filepath.Join(mdDir, "bookmarks", "2026", "2026-01-01-10001-alice.md")
	for _, path := range []string{jsonPath, csvPath, htmlPath, filepath.Join(mdDir, "index.jsonl"), mdPath, singlePath, filepath.Join(obsidianDir, "Bookmarks.md"), filepath.Join(obsidianDir, "Bookmarks", "2026", "2026-01-01-10001-alice.md"), filepath.Join(obsidianDir, "Authors", "alice.md"), filepath.Join(obsidianDir, "index.jsonl")} {
		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(b), "10001") {
			t.Fatalf("%s does not contain tweet id", path)
		}
	}
	htmlDoc, err := os.ReadFile(htmlPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(htmlDoc), "data.flatMap") || strings.Contains(string(htmlDoc), "<option>bookmark</option>") {
		t.Fatalf("html collection filter is not data-driven")
	}
	mdDoc, err := os.ReadFile(mdPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(mdDoc), "collections:\n  - \"bookmark\"") {
		t.Fatalf("markdown front matter collections invalid: %s", mdDoc)
	}
	indexDoc, err := os.ReadFile(filepath.Join(mdDir, "index.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(indexDoc), filepath.ToSlash("bookmarks/2026/2026-01-01-10001-alice.md")) {
		t.Fatalf("index path is not stable year layout: %s", indexDoc)
	}
	emptyIndex, err := os.ReadFile(filepath.Join(obsidianDir, "Likes.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(emptyIndex), "No records exported.") {
		t.Fatalf("empty obsidian collection index = %s", emptyIndex)
	}
	if _, err := os.Stat(filepath.Join(obsidianDir, "Likes")); !os.IsNotExist(err) {
		t.Fatalf("empty obsidian collection directory should not exist, err=%v", err)
	}
}

func TestMarkdownPathUsesTwitterDateYearLayout(t *testing.T) {
	if got := safeName("Wed Sep 18 12:56:31 +0000 2024", "1836388833698680949", "JinaAI_"); got != "2024-09-18-1836388833698680949-JinaAI_" {
		t.Fatalf("safe name = %q", got)
	}
	if got := exportYear("Wed Sep 18 12:56:31 +0000 2024"); got != "2024" {
		t.Fatalf("year = %q", got)
	}
}

func TestExportFolderFilter(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(filepath.Join(t.TempDir(), "xvault.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	page := model.ParsedPage{
		Users: []model.User{{ID: "u1", Username: "alice"}},
		Tweets: []model.Tweet{
			{ID: "10001", Text: "folder alpha", AuthorID: "u1", AuthorUsername: "alice"},
			{ID: "10002", Text: "folder beta", AuthorID: "u1", AuthorUsername: "alice"},
		},
		Collections: []model.CollectionItem{
			{TweetID: "10001", CollectionType: "bookmark", BookmarkFolderID: "f1", BookmarkFolderName: "Research"},
			{TweetID: "10002", CollectionType: "bookmark", BookmarkFolderID: "f2", BookmarkFolderName: "Later"},
		},
	}
	if err := st.UpsertPage(ctx, page); err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(t.TempDir(), "research.json")
	data, err := JSONWithFolder(ctx, st, "bookmarks", "Research", output, true)
	if err != nil {
		t.Fatal(err)
	}
	if data["count"] != 1 {
		t.Fatalf("export count = %#v", data)
	}
	b, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "10001") || strings.Contains(string(b), "10002") {
		t.Fatalf("folder export content = %s", b)
	}
}

func TestExportsRenderQuotedTweetContent(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(filepath.Join(t.TempDir(), "xvault.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	page := model.ParsedPage{
		Users: []model.User{
			{ID: "u1", Username: "alice", DisplayName: "Alice"},
			{ID: "u2", Username: "bob", DisplayName: "Bob"},
		},
		Tweets: []model.Tweet{
			{ID: "10001", Text: "outer quote tweet", AuthorID: "u1", AuthorUsername: "alice", QuotedTweetID: "20002", IsQuote: true, CreatedAt: "2026-01-01T00:00:00Z"},
			{ID: "20002", Text: "quoted inner text", AuthorID: "u2", AuthorUsername: "bob", AuthorDisplayName: "Bob", CreatedAt: "2026-01-01T00:00:00Z"},
		},
		Collections: []model.CollectionItem{{TweetID: "10001", CollectionType: "bookmark"}},
	}
	if err := st.UpsertPage(ctx, page); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	mdDir := filepath.Join(dir, "markdown")
	htmlPath := filepath.Join(dir, "archive.html")
	if _, err := Markdown(ctx, st, "bookmarks", mdDir, false); err != nil {
		t.Fatal(err)
	}
	if _, err := HTML(ctx, st, "bookmarks", htmlPath); err != nil {
		t.Fatal(err)
	}
	mdDoc, err := os.ReadFile(filepath.Join(mdDir, "bookmarks", "2026", "2026-01-01-10001-alice.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(mdDoc), "## Quoted Tweet") || !strings.Contains(string(mdDoc), "quoted inner text") || !strings.Contains(string(mdDoc), "@bob") {
		t.Fatalf("markdown quoted content missing: %s", mdDoc)
	}
	htmlDoc, err := os.ReadFile(htmlPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(htmlDoc), "quoted_text_preview") || !strings.Contains(string(htmlDoc), "quoted inner text") {
		t.Fatalf("html quoted content missing: %s", htmlDoc)
	}
}

func TestHTMLFailOnLarge(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(filepath.Join(t.TempDir(), "xvault.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	page := model.ParsedPage{
		Users:       []model.User{{ID: "u1", Username: "alice"}},
		Tweets:      []model.Tweet{{ID: "10001", Text: "large html fixture", AuthorID: "u1", AuthorUsername: "alice"}},
		Collections: []model.CollectionItem{{TweetID: "10001", CollectionType: "bookmark"}},
	}
	if err := st.UpsertPage(ctx, page); err != nil {
		t.Fatal(err)
	}
	data, err := HTMLWithFolderOptions(ctx, st, "all", "", "", 1, false)
	if err != nil {
		t.Fatal(err)
	}
	if data["html_size_bytes"] == nil || data["html_warn_size_mb"] != 1 {
		t.Fatalf("html metadata = %#v", data)
	}
	if _, err := HTMLWithFolderOptions(ctx, st, "all", "", "", 1, true); err != nil {
		t.Fatalf("1 MiB threshold should not fail: %v", err)
	}
	if _, err := HTMLWithFolderOptions(ctx, st, "all", "", "", -1, true); err != nil {
		t.Fatalf("negative threshold disables limit: %v", err)
	}
	if _, err := HTMLWithFolderOptions(ctx, st, "all", "", "", 0, true); err != nil {
		t.Fatalf("zero threshold disables limit: %v", err)
	}
	large := model.ParsedPage{
		Users: []model.User{{ID: "u1", Username: "alice"}},
	}
	for i := 0; i < 4000; i++ {
		id := fmt.Sprintf("2%04d", i)
		large.Tweets = append(large.Tweets, model.Tweet{ID: id, Text: strings.Repeat("large html row ", 30), AuthorID: "u1", AuthorUsername: "alice"})
		large.Collections = append(large.Collections, model.CollectionItem{TweetID: id, CollectionType: "bookmark"})
	}
	if err := st.UpsertPage(ctx, large); err != nil {
		t.Fatal(err)
	}
	if data, err := HTMLWithFolderOptions(ctx, st, "all", "", "", 1, false); err != nil || data["large_file_warning"] != true {
		t.Fatalf("large html warning data=%#v err=%v", data, err)
	}
	if _, err := HTMLWithFolderOptions(ctx, st, "all", "", "", 1, true); err == nil {
		t.Fatal("expected fail-on-large to reject oversized HTML")
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
