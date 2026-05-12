package exporter

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"html"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/ecylmz/xvault/internal/model"
	"github.com/ecylmz/xvault/internal/store"
)

func JSON(ctx context.Context, st *store.Store, collection, output string, pretty bool) (map[string]any, error) {
	return JSONWithFolder(ctx, st, collection, "", output, pretty)
}

func JSONWithFolder(ctx context.Context, st *store.Store, collection, folder, output string, pretty bool) (map[string]any, error) {
	results, err := exportRows(ctx, st, collection, folder)
	if err != nil {
		return nil, err
	}
	doc := map[string]any{"schema_version": 1, "exported_at": time.Now().UTC().Format(time.RFC3339), "collection": collection, "folder": folder, "count": len(results), "tweets": results}
	var b []byte
	if pretty {
		b, err = json.MarshalIndent(doc, "", "  ")
	} else {
		b, err = json.Marshal(doc)
	}
	if err != nil {
		return nil, err
	}
	if output != "" {
		if err := writeFile(output, b); err != nil {
			return nil, err
		}
	}
	return map[string]any{"output": output, "count": len(results)}, nil
}

func CSV(ctx context.Context, st *store.Store, collection, output string) (map[string]any, error) {
	return CSVWithFolder(ctx, st, collection, "", output)
}

func CSVWithFolder(ctx context.Context, st *store.Store, collection, folder, output string) (map[string]any, error) {
	results, err := exportRows(ctx, st, collection, folder)
	if err != nil {
		return nil, err
	}
	var sb strings.Builder
	w := csv.NewWriter(&sb)
	_ = w.Write([]string{"tweet_id", "url", "text", "author_username", "author_display_name", "created_at", "collections", "bookmark_folder", "has_media", "has_links", "quoted_tweet_id", "conversation_id"})
	for _, r := range results {
		_ = w.Write([]string{r.TweetID, r.URL, r.TextPreview, r.AuthorUsername, r.AuthorDisplayName, r.CreatedAt, strings.Join(r.Collections, ";"), r.BookmarkFolderName, fmt.Sprint(r.HasMedia), fmt.Sprint(r.HasLinks), r.QuotedTweetID, r.ConversationID})
	}
	w.Flush()
	if output != "" {
		if err := writeFile(output, []byte(sb.String())); err != nil {
			return nil, err
		}
	}
	return map[string]any{"output": output, "count": len(results)}, nil
}

func Markdown(ctx context.Context, st *store.Store, collection, output string, hermesIndex bool) (map[string]any, error) {
	return MarkdownWithFolder(ctx, st, collection, "", output, hermesIndex)
}

func MarkdownWithFolder(ctx context.Context, st *store.Store, collection, folder, output string, hermesIndex bool) (map[string]any, error) {
	results, err := exportRows(ctx, st, collection, folder)
	if err != nil {
		return nil, err
	}
	if output == "" {
		output = filepath.Join(os.Getenv("HOME"), ".local/share/xvault/exports/markdown")
	}
	if err := os.MkdirAll(output, 0o755); err != nil {
		return nil, err
	}
	var index strings.Builder
	for _, r := range results {
		relDir := filepath.Join(collectionDir(r.Collections, collection), exportYear(r.CreatedAt))
		dir := filepath.Join(output, relDir)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, err
		}
		name := safeName(r.CreatedAt, r.TweetID, r.AuthorUsername) + ".md"
		rel := filepath.Join(relDir, name)
		body := markdownDoc(r)
		if err := os.WriteFile(filepath.Join(output, rel), []byte(body), 0o644); err != nil {
			return nil, err
		}
		if hermesIndex {
			line, _ := json.Marshal(map[string]any{"tweet_id": r.TweetID, "path": rel, "text": r.TextPreview, "author": r.AuthorUsername, "collections": r.Collections})
			index.Write(line)
			index.WriteByte('\n')
		}
	}
	if hermesIndex {
		if err := os.WriteFile(filepath.Join(output, "index.jsonl"), []byte(index.String()), 0o644); err != nil {
			return nil, err
		}
	}
	return map[string]any{"output": output, "count": len(results)}, nil
}

func MarkdownSingleWithFolder(ctx context.Context, st *store.Store, collection, folder, output string) (map[string]any, error) {
	results, err := exportRows(ctx, st, collection, folder)
	if err != nil {
		return nil, err
	}
	if output == "" {
		output = filepath.Join(os.Getenv("HOME"), ".local/share/xvault/exports/markdown/archive.md")
	}
	var body strings.Builder
	body.WriteString("# xvault archive\n\n")
	body.WriteString(fmt.Sprintf("- collection: %s\n- folder: %s\n- count: %d\n- exported_at: %s\n\n", collection, folder, len(results), time.Now().UTC().Format(time.RFC3339)))
	for i, r := range results {
		if i > 0 {
			body.WriteString("\n---\n\n")
		}
		body.WriteString(markdownDoc(r))
	}
	if err := writeFile(output, []byte(body.String())); err != nil {
		return nil, err
	}
	return map[string]any{"output": output, "count": len(results), "mode": "single"}, nil
}

func ObsidianWithFolder(ctx context.Context, st *store.Store, collection, folder, output string, withIndexJSONL bool) (map[string]any, error) {
	results, err := exportRows(ctx, st, collection, folder)
	if err != nil {
		return nil, err
	}
	if output == "" {
		output = filepath.Join(os.Getenv("HOME"), ".local/share/xvault/exports/obsidian")
	}
	if err := os.MkdirAll(output, 0o755); err != nil {
		return nil, err
	}
	collectionNotes := map[string][]model.SearchResult{}
	authorNotes := map[string][]model.SearchResult{}
	var index strings.Builder
	for _, r := range results {
		dirName := obsidianCollectionDir(r.Collections, collection)
		collectionNotes[dirName] = append(collectionNotes[dirName], r)
		author := r.AuthorUsername
		if author == "" {
			author = "unknown"
		}
		authorNotes[author] = append(authorNotes[author], r)
		relDir := filepath.Join(dirName, exportYear(r.CreatedAt))
		if err := os.MkdirAll(filepath.Join(output, relDir), 0o755); err != nil {
			return nil, err
		}
		name := safeName(r.CreatedAt, r.TweetID, r.AuthorUsername) + ".md"
		rel := filepath.Join(relDir, name)
		if err := os.WriteFile(filepath.Join(output, rel), []byte(markdownDoc(r)), 0o644); err != nil {
			return nil, err
		}
		if withIndexJSONL {
			line, _ := json.Marshal(map[string]any{"tweet_id": r.TweetID, "path": filepath.ToSlash(rel), "text": r.TextPreview, "author": r.AuthorUsername, "collections": r.Collections})
			index.Write(line)
			index.WriteByte('\n')
		}
	}
	for _, dirName := range obsidianCollectionOrder() {
		if err := writeObsidianCollectionIndex(output, dirName, collectionNotes[dirName]); err != nil {
			return nil, err
		}
	}
	if len(authorNotes) > 0 {
		authorsDir := filepath.Join(output, "Authors")
		if err := os.MkdirAll(authorsDir, 0o755); err != nil {
			return nil, err
		}
		for _, author := range sortedKeys(authorNotes) {
			if err := writeObsidianAuthorIndex(authorsDir, author, authorNotes[author]); err != nil {
				return nil, err
			}
		}
	}
	if withIndexJSONL {
		if err := os.WriteFile(filepath.Join(output, "index.jsonl"), []byte(index.String()), 0o644); err != nil {
			return nil, err
		}
	}
	return map[string]any{"output": output, "count": len(results), "with_index_jsonl": withIndexJSONL}, nil
}

func HTML(ctx context.Context, st *store.Store, collection, output string) (map[string]any, error) {
	return HTMLWithFolder(ctx, st, collection, "", output)
}

func HTMLWithFolder(ctx context.Context, st *store.Store, collection, folder, output string) (map[string]any, error) {
	results, err := exportRows(ctx, st, collection, folder)
	if err != nil {
		return nil, err
	}
	data, _ := json.Marshal(results)
	doc := `<!doctype html><html><head><meta charset="utf-8"><title>xvault archive</title><style>body{font-family:-apple-system,BlinkMacSystemFont,Segoe UI,sans-serif;margin:0;background:#fafafa;color:#111}.bar{position:sticky;top:0;background:#fff;border-bottom:1px solid #ddd;padding:12px 18px;display:flex;gap:12px;align-items:center}input,select{font:inherit;padding:7px 9px;border:1px solid #bbb;border-radius:6px}.wrap{max-width:980px;margin:20px auto;padding:0 14px}.tweet{background:#fff;border:1px solid #ddd;border-radius:8px;padding:14px;margin:10px 0}.meta{color:#555;font-size:13px}.cols{font-size:12px;color:#555}</style></head><body><div class="bar"><strong>xvault archive</strong><input id="q" placeholder="Search archive"><select id="c"><option value="">All</option></select><span id="n"></span></div><main class="wrap" id="list"></main><script>const data=` + string(data) + `;const q=document.getElementById('q'),c=document.getElementById('c'),list=document.getElementById('list'),n=document.getElementById('n');function setup(){[...new Set(data.flatMap(r=>r.collections||[]))].sort().forEach(v=>{const o=document.createElement('option');o.value=v;o.textContent=v;c.appendChild(o)})}function render(){const term=q.value.toLowerCase(),col=c.value;const rows=data.filter(r=>(!term||r.text_preview.toLowerCase().includes(term)||r.author_username.toLowerCase().includes(term))&&(!col||(r.collections||[]).includes(col))).slice(0,1000);n.textContent=rows.length+' shown';list.innerHTML=rows.map(r=>'<article class=tweet><div class=meta>@'+esc(r.author_username)+' · '+esc(r.created_at)+' · <a href="'+r.url+'">open</a></div><p>'+esc(r.text_preview)+'</p><div class=cols>'+(r.collections||[]).map(esc).join(', ')+'</div></article>').join('')}function esc(s){return String(s||'').replace(/[&<>"]/g,m=>({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;'}[m]))}q.oninput=render;c.onchange=render;setup();render()</script></body></html>`
	if output != "" {
		if err := writeFile(output, []byte(doc)); err != nil {
			return nil, err
		}
	}
	return map[string]any{"output": output, "count": len(results)}, nil
}

func exportRows(ctx context.Context, st *store.Store, collection, folder string) ([]model.SearchResult, error) {
	if collection == "" {
		collection = "all"
	}
	return st.SearchWithFilters(ctx, "", collection, "", folder, "", "", false, false, 100000, 0)
}

func Backup(ctx context.Context, st *store.Store, output string) (map[string]any, error) {
	if output == "" {
		output = filepath.Join(os.Getenv("HOME"), ".local/state/xvault/backups/xvault-"+time.Now().UTC().Format("20060102-150405")+".sqlite")
	}
	if err := os.MkdirAll(filepath.Dir(output), 0o700); err != nil {
		return nil, err
	}
	if _, err := os.Stat(output); err == nil {
		return nil, fmt.Errorf("backup output already exists: %s", output)
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	if _, err := st.DB().ExecContext(ctx, "PRAGMA wal_checkpoint(PASSIVE)"); err != nil {
		return nil, err
	}
	if _, err := st.DB().ExecContext(ctx, "VACUUM INTO ?", output); err != nil {
		return nil, err
	}
	return map[string]any{"output": output}, nil
}

func writeFile(path string, b []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o644)
}

func markdownDoc(r model.SearchResult) string {
	var collections strings.Builder
	for _, c := range r.Collections {
		collections.WriteString("\n  - ")
		collections.WriteString(strconvQuote(c))
	}
	if collections.Len() == 0 {
		collections.WriteString(" []")
	}
	return fmt.Sprintf("---\ntweet_id: %q\nurl: %q\nauthor_username: %q\nauthor_display_name: %q\ncreated_at: %q\ncollections:%s\nbookmark_folder: %q\nhas_media: %v\nhas_links: %v\nquoted_tweet_id: %q\nconversation_id: %q\n---\n\n%s\n\n## Source\n\n%s\n",
		r.TweetID, r.URL, r.AuthorUsername, r.AuthorDisplayName, r.CreatedAt, collections.String(), r.BookmarkFolderName, r.HasMedia, r.HasLinks, r.QuotedTweetID, r.ConversationID, r.TextPreview, r.URL)
}

func safeName(created, id, author string) string {
	date, _ := exportDateParts(created)
	base := date + "-" + id
	if author != "" {
		base += "-" + author
	}
	return strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_' {
			return r
		}
		return '-'
	}, base)
}

func collectionDir(cols []string, fallback string) string {
	if fallback != "" && fallback != "all" {
		return fallback
	}
	for _, c := range cols {
		if c == "bookmark" {
			return "bookmarks"
		}
	}
	if len(cols) > 0 {
		return cols[0] + "s"
	}
	return "all"
}

func obsidianCollectionDir(cols []string, fallback string) string {
	switch collectionDir(cols, fallback) {
	case "bookmarks":
		return "Bookmarks"
	case "likes":
		return "Likes"
	case "tweets":
		return "Tweets"
	case "posts":
		return "Tweets"
	case "reposts":
		return "Reposts"
	case "replies":
		return "Replies"
	case "feed":
		return "Feed"
	default:
		return "All"
	}
}

func obsidianCollectionOrder() []string {
	return []string{"Bookmarks", "Likes", "Tweets", "Reposts", "Replies", "Feed"}
}

func writeObsidianCollectionIndex(output, dirName string, rows []model.SearchResult) error {
	var b strings.Builder
	b.WriteString("# " + dirName + "\n\n")
	if len(rows) == 0 {
		b.WriteString("No records exported.\n")
		return os.WriteFile(filepath.Join(output, dirName+".md"), []byte(b.String()), 0o644)
	}
	b.WriteString(fmt.Sprintf("%d records exported.\n\n", len(rows)))
	for _, r := range rows {
		rel := filepath.ToSlash(filepath.Join(dirName, exportYear(r.CreatedAt), safeName(r.CreatedAt, r.TweetID, r.AuthorUsername)+".md"))
		b.WriteString(fmt.Sprintf("- [%s](%s) @%s\n", r.TweetID, rel, r.AuthorUsername))
	}
	return os.WriteFile(filepath.Join(output, dirName+".md"), []byte(b.String()), 0o644)
}

func writeObsidianAuthorIndex(authorsDir, author string, rows []model.SearchResult) error {
	var b strings.Builder
	b.WriteString("# @" + author + "\n\n")
	b.WriteString(fmt.Sprintf("%d records exported.\n\n", len(rows)))
	for _, r := range rows {
		dirName := obsidianCollectionDir(r.Collections, "all")
		rel := filepath.ToSlash(filepath.Join("..", dirName, exportYear(r.CreatedAt), safeName(r.CreatedAt, r.TweetID, r.AuthorUsername)+".md"))
		b.WriteString(fmt.Sprintf("- [%s](%s) %s\n", r.TweetID, rel, r.TextPreview))
	}
	return os.WriteFile(filepath.Join(authorsDir, safeSegment(author)+".md"), []byte(b.String()), 0o644)
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func exportYear(created string) string {
	_, year := exportDateParts(created)
	return year
}

func exportDateParts(created string) (string, string) {
	if t, err := time.Parse(time.RFC3339, created); err == nil {
		return t.UTC().Format("2006-01-02"), t.UTC().Format("2006")
	}
	if t, err := time.Parse("Mon Jan 02 15:04:05 -0700 2006", created); err == nil {
		return t.UTC().Format("2006-01-02"), t.UTC().Format("2006")
	}
	return "undated", "undated"
}

func strconvQuote(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

func safeSegment(s string) string {
	if s == "" {
		return "unknown"
	}
	return strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_' {
			return r
		}
		return '-'
	}, s)
}

func Escape(s string) string { return html.EscapeString(s) }
