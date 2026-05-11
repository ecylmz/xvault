package exporter

import (
	"context"
	"database/sql"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"html"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ecylmz/xvault/internal/model"
	"github.com/ecylmz/xvault/internal/store"
)

func JSON(ctx context.Context, st *store.Store, collection, output string, pretty bool) (map[string]any, error) {
	results, err := st.Search(ctx, "", collection, "", "", 100000, 0)
	if err != nil {
		return nil, err
	}
	doc := map[string]any{"schema_version": 1, "exported_at": time.Now().UTC().Format(time.RFC3339), "collection": collection, "count": len(results), "tweets": results}
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
	results, err := st.Search(ctx, "", collection, "", "", 100000, 0)
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
	results, err := st.Search(ctx, "", collection, "", "", 100000, 0)
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
		dir := filepath.Join(output, collectionDir(r.Collections, collection))
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, err
		}
		name := safeName(r.CreatedAt, r.TweetID, r.AuthorUsername) + ".md"
		rel := filepath.Join(collectionDir(r.Collections, collection), name)
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

func HTML(ctx context.Context, st *store.Store, collection, output string) (map[string]any, error) {
	results, err := st.Search(ctx, "", collection, "", "", 100000, 0)
	if err != nil {
		return nil, err
	}
	data, _ := json.Marshal(results)
	doc := `<!doctype html><html><head><meta charset="utf-8"><title>xvault archive</title><style>body{font-family:-apple-system,BlinkMacSystemFont,Segoe UI,sans-serif;margin:0;background:#fafafa;color:#111}.bar{position:sticky;top:0;background:#fff;border-bottom:1px solid #ddd;padding:12px 18px;display:flex;gap:12px;align-items:center}input,select{font:inherit;padding:7px 9px;border:1px solid #bbb;border-radius:6px}.wrap{max-width:980px;margin:20px auto;padding:0 14px}.tweet{background:#fff;border:1px solid #ddd;border-radius:8px;padding:14px;margin:10px 0}.meta{color:#555;font-size:13px}.cols{font-size:12px;color:#555}</style></head><body><div class="bar"><strong>xvault archive</strong><input id="q" placeholder="Search archive"><select id="c"><option value="">All</option><option>bookmark</option><option>like</option><option>tweet</option><option>repost</option><option>reply</option></select><span id="n"></span></div><main class="wrap" id="list"></main><script>const data=` + string(data) + `;const q=document.getElementById('q'),c=document.getElementById('c'),list=document.getElementById('list'),n=document.getElementById('n');function render(){const term=q.value.toLowerCase(),col=c.value;const rows=data.filter(r=>(!term||r.text_preview.toLowerCase().includes(term)||r.author_username.toLowerCase().includes(term))&&(!col||r.collections.includes(col))).slice(0,1000);n.textContent=rows.length+' shown';list.innerHTML=rows.map(r=>'<article class=tweet><div class=meta>@'+esc(r.author_username)+' · '+esc(r.created_at)+' · <a href="'+r.url+'">open</a></div><p>'+esc(r.text_preview)+'</p><div class=cols>'+r.collections.map(esc).join(', ')+'</div></article>').join('')}function esc(s){return String(s||'').replace(/[&<>"]/g,m=>({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;'}[m]))}q.oninput=render;c.onchange=render;render()</script></body></html>`
	if output != "" {
		if err := writeFile(output, []byte(doc)); err != nil {
			return nil, err
		}
	}
	return map[string]any{"output": output, "count": len(results)}, nil
}

func Backup(ctx context.Context, st *store.Store, output string) (map[string]any, error) {
	if output == "" {
		output = filepath.Join(os.Getenv("HOME"), ".local/state/xvault/backups/xvault-"+time.Now().UTC().Format("20060102-150405")+".sqlite")
	}
	if err := os.MkdirAll(filepath.Dir(output), 0o700); err != nil {
		return nil, err
	}
	dst, err := sql.Open("sqlite", output)
	if err != nil {
		return nil, err
	}
	defer dst.Close()
	srcConn, err := st.DB().Conn(ctx)
	if err != nil {
		return nil, err
	}
	defer srcConn.Close()
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
	return fmt.Sprintf("---\ntweet_id: %q\nurl: %q\nauthor_username: %q\nauthor_display_name: %q\ncreated_at: %q\ncollections: [%q]\nbookmark_folder: %q\nhas_media: %v\nhas_links: %v\nquoted_tweet_id: %q\nconversation_id: %q\n---\n\n%s\n\n## Source\n\n%s\n",
		r.TweetID, r.URL, r.AuthorUsername, r.AuthorDisplayName, r.CreatedAt, strings.Join(r.Collections, `","`), r.BookmarkFolderName, r.HasMedia, r.HasLinks, r.QuotedTweetID, r.ConversationID, r.TextPreview, r.URL)
}

func safeName(created, id, author string) string {
	date := "undated"
	if len(created) >= 10 {
		date = created[:10]
	}
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

func Escape(s string) string { return html.EscapeString(s) }
