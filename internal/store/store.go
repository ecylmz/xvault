package store

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ecylmz/xvault/internal/model"
	_ "modernc.org/sqlite"
)

type Store struct {
	db *sql.DB
}

func Open(path string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", path+"?_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)")
	if err != nil {
		return nil, err
	}
	s := &Store{db: db}
	if err := s.Migrate(context.Background()); err != nil {
		_ = db.Close()
		return nil, err
	}
	return s, nil
}

func (s *Store) Close() error { return s.db.Close() }
func (s *Store) DB() *sql.DB  { return s.db }

func (s *Store) Migrate(ctx context.Context) error {
	for _, stmt := range strings.Split(schemaSQL, ";\n") {
		if strings.TrimSpace(stmt) == "" {
			continue
		}
		if _, err := s.db.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("migration failed: %w\nstatement: %s", err, stmt)
		}
	}
	return nil
}

func (s *Store) Integrity(ctx context.Context) (string, error) {
	var result string
	return result, s.db.QueryRowContext(ctx, "PRAGMA integrity_check").Scan(&result)
}

func (s *Store) SaveRaw(ctx context.Context, kind, op string, payload []byte) (string, error) {
	sum := sha256.Sum256(payload)
	id := hex.EncodeToString(sum[:])
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	if _, err := gz.Write(payload); err != nil {
		return "", err
	}
	if err := gz.Close(); err != nil {
		return "", err
	}
	_, err := s.db.ExecContext(ctx, `INSERT OR IGNORE INTO raw_payloads(id, kind, operation_name, sha256, payload, compressed, captured_at) VALUES(?,?,?,?,?,1,?)`,
		id, kind, op, id, buf.Bytes(), time.Now().UTC().Format(time.RFC3339))
	return id, err
}

func (s *Store) UpsertPage(ctx context.Context, page model.ParsedPage) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	for _, u := range page.Users {
		if u.ID == "" {
			continue
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO users(id, username, display_name, avatar_url, verified, protected, first_seen_at, last_seen_at)
VALUES(?,?,?,?,?,?,?,?)
ON CONFLICT(id) DO UPDATE SET username=excluded.username, display_name=excluded.display_name, avatar_url=excluded.avatar_url, verified=excluded.verified, protected=excluded.protected, last_seen_at=excluded.last_seen_at`,
			u.ID, u.Username, u.DisplayName, u.AvatarURL, boolInt(u.Verified), boolInt(u.Protected), now(), now()); err != nil {
			return err
		}
	}
	for _, tw := range page.Tweets {
		if tw.ID == "" {
			continue
		}
		if tw.AuthorID == "" {
			tw.AuthorID = "unknown"
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO users(id, username, display_name, first_seen_at, last_seen_at) VALUES(?,?,?,?,?)
ON CONFLICT(id) DO UPDATE SET username=CASE WHEN users.username IS NULL OR users.username='' THEN excluded.username ELSE users.username END, display_name=CASE WHEN users.display_name IS NULL OR users.display_name='' THEN excluded.display_name ELSE users.display_name END, last_seen_at=excluded.last_seen_at`, tw.AuthorID, tw.AuthorUsername, tw.AuthorDisplayName, now(), now()); err != nil {
			return err
		}
		for _, ref := range []struct {
			id     string
			reason string
		}{
			{tw.QuotedTweetID, "quoted_unavailable"},
			{tw.RetweetedTweetID, "retweeted_unavailable"},
		} {
			if ref.id != "" {
				if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO users(id, username, display_name, first_seen_at, last_seen_at) VALUES('unknown','unknown','Unknown',?,?)`, now(), now()); err != nil {
					return err
				}
				if err := upsertTweet(ctx, tx, model.Tweet{ID: ref.id, Text: "[Tweet unavailable]", AuthorID: "unknown", IsTombstone: true, TombstoneReason: ref.reason}); err != nil {
					return err
				}
			}
		}
		if err := upsertTweet(ctx, tx, tw); err != nil {
			return err
		}
		if err := updateFTS(ctx, tx, tw); err != nil {
			return err
		}
	}
	for _, c := range page.Collections {
		if c.TweetID == "" || c.CollectionType == "" {
			continue
		}
		key := c.BookmarkFolderID
		if _, err := tx.ExecContext(ctx, `INSERT INTO collections(tweet_id, collection_type, bookmark_folder_id, bookmark_folder_id_key, bookmark_folder_name, added_at, synced_at, sort_index, source_run_id, thread_id)
VALUES(?,?,?,?,?,?,?,?,?,?)
ON CONFLICT(tweet_id, collection_type, bookmark_folder_id_key) DO UPDATE SET bookmark_folder_name=excluded.bookmark_folder_name, synced_at=excluded.synced_at, sort_index=excluded.sort_index, source_run_id=excluded.source_run_id, thread_id=excluded.thread_id`,
			c.TweetID, c.CollectionType, nullString(c.BookmarkFolderID), key, nullString(c.BookmarkFolderName), nullString(c.AddedAt), now(), nullString(c.SortIndex), nullString(c.SourceRunID), nullString(c.ThreadID)); err != nil {
			return err
		}
		if c.CollectionType == "bookmark" && c.BookmarkFolderID != "" {
			if _, err := tx.ExecContext(ctx, `INSERT INTO bookmark_folders(id, name, first_seen_at, last_seen_at) VALUES(?,?,?,?)
ON CONFLICT(id) DO UPDATE SET name=excluded.name, last_seen_at=excluded.last_seen_at`, c.BookmarkFolderID, c.BookmarkFolderName, now(), now()); err != nil {
				return err
			}
		}
	}
	for _, u := range page.URLs {
		if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO urls(tweet_id, url, expanded_url, display_url) VALUES(?,?,?,?)`, u.TweetID, u.URL, u.ExpandedURL, u.DisplayURL); err != nil {
			return err
		}
	}
	for _, m := range page.Media {
		if m.ID == "" {
			m.ID = m.TweetID + ":" + m.URL
		}
		if _, err := tx.ExecContext(ctx, `INSERT OR REPLACE INTO media(id, tweet_id, media_type, url, expanded_url, preview_url, alt_text) VALUES(?,?,?,?,?,?,?)`, m.ID, m.TweetID, m.MediaType, m.URL, m.ExpandedURL, m.PreviewURL, m.AltText); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func upsertTweet(ctx context.Context, tx *sql.Tx, tw model.Tweet) error {
	if tw.Text == "" && !tw.IsTombstone {
		tw.Text = " "
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO tweets(id, text, lang, author_id, created_at, conversation_id, in_reply_to_tweet_id, in_reply_to_user_id, quoted_tweet_id, retweeted_tweet_id, is_quote, is_retweet, is_reply, is_tombstone, tombstone_reason, reply_count, retweet_count, like_count, quote_count, bookmark_count, view_count, raw_json_id, first_seen_at, last_seen_at)
VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
ON CONFLICT(id) DO UPDATE SET
text=CASE WHEN tweets.is_tombstone=1 OR excluded.is_tombstone=0 THEN excluded.text ELSE tweets.text END,
lang=excluded.lang, author_id=excluded.author_id, created_at=excluded.created_at, conversation_id=excluded.conversation_id,
in_reply_to_tweet_id=excluded.in_reply_to_tweet_id, in_reply_to_user_id=excluded.in_reply_to_user_id,
quoted_tweet_id=excluded.quoted_tweet_id, retweeted_tweet_id=excluded.retweeted_tweet_id,
is_quote=excluded.is_quote, is_retweet=excluded.is_retweet, is_reply=excluded.is_reply,
is_tombstone=CASE WHEN excluded.is_tombstone=0 THEN 0 ELSE tweets.is_tombstone END,
tombstone_reason=CASE WHEN excluded.is_tombstone=0 THEN NULL ELSE tweets.tombstone_reason END,
reply_count=excluded.reply_count, retweet_count=excluded.retweet_count, like_count=excluded.like_count, quote_count=excluded.quote_count,
bookmark_count=excluded.bookmark_count, view_count=excluded.view_count, raw_json_id=excluded.raw_json_id, last_seen_at=excluded.last_seen_at`,
		tw.ID, tw.Text, nullString(tw.Lang), tw.AuthorID, nullString(tw.CreatedAt), nullString(tw.ConversationID), nullString(tw.InReplyToTweetID), nullString(tw.InReplyToUserID),
		nullString(tw.QuotedTweetID), nullString(tw.RetweetedTweetID), boolInt(tw.IsQuote), boolInt(tw.IsRetweet), boolInt(tw.IsReply), boolInt(tw.IsTombstone), nullString(tw.TombstoneReason),
		tw.ReplyCount, tw.RetweetCount, tw.LikeCount, tw.QuoteCount, tw.BookmarkCount, tw.ViewCount, nullString(tw.RawJSONID), now(), now())
	return err
}

func updateFTS(ctx context.Context, tx *sql.Tx, tw model.Tweet) error {
	var rowid int64
	err := tx.QueryRowContext(ctx, `SELECT rowid FROM tweets_fts_map WHERE tweet_id=?`, tw.ID).Scan(&rowid)
	if errors.Is(err, sql.ErrNoRows) {
		res, err := tx.ExecContext(ctx, `INSERT INTO tweets_fts(text, author_username, author_display_name) VALUES(?,?,?)`, tw.Text, tw.AuthorUsername, tw.AuthorDisplayName)
		if err != nil {
			return err
		}
		rowid, err = res.LastInsertId()
		if err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO tweets_fts_map(rowid, tweet_id) VALUES(?,?)`, rowid, tw.ID)
		return err
	}
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM tweets_fts WHERE rowid=?`, rowid); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM tweets_fts_map WHERE rowid=?`, rowid); err != nil {
		return err
	}
	res, err := tx.ExecContext(ctx, `INSERT INTO tweets_fts(text, author_username, author_display_name) VALUES(?,?,?)`, tw.Text, tw.AuthorUsername, tw.AuthorDisplayName)
	if err != nil {
		return err
	}
	rowid, err = res.LastInsertId()
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO tweets_fts_map(rowid, tweet_id) VALUES(?,?)`, rowid, tw.ID)
	return err
}

func (s *Store) RebuildFTS(ctx context.Context) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `DELETE FROM tweets_fts_map; DELETE FROM tweets_fts`); err != nil {
		return err
	}
	rows, err := tx.QueryContext(ctx, `SELECT t.id, t.text, COALESCE(u.username,''), COALESCE(u.display_name,'') FROM tweets t LEFT JOIN users u ON u.id=t.author_id WHERE t.is_tombstone=0 ORDER BY t.id`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var tw model.Tweet
		if err := rows.Scan(&tw.ID, &tw.Text, &tw.AuthorUsername, &tw.AuthorDisplayName); err != nil {
			return err
		}
		if err := updateFTS(ctx, tx, tw); err != nil {
			return err
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) Search(ctx context.Context, query, source, author, folder string, limit, offset int) ([]model.SearchResult, error) {
	if limit <= 0 {
		limit = 10
	} else if limit > 100000 {
		limit = 100000
	}
	where := []string{"t.is_tombstone=0"}
	args := []any{}
	joinFTS := false
	if strings.TrimSpace(query) != "" {
		joinFTS = true
		where = append(where, "f.text MATCH ?")
		args = append(args, query)
	}
	if source != "" && source != "all" {
		where = append(where, "EXISTS (SELECT 1 FROM collections c WHERE c.tweet_id=t.id AND c.collection_type=?)")
		args = append(args, normalizeCollection(source))
	}
	if author != "" {
		where = append(where, "LOWER(u.username)=LOWER(?)")
		args = append(args, strings.TrimPrefix(author, "@"))
	}
	if folder != "" {
		where = append(where, "EXISTS (SELECT 1 FROM collections c WHERE c.tweet_id=t.id AND c.collection_type='bookmark' AND c.bookmark_folder_name=?)")
		args = append(args, folder)
	}
	sqlText := `SELECT t.id, t.text, COALESCE(u.username,''), COALESCE(u.display_name,''), COALESCE(t.created_at,''), COALESCE(t.quoted_tweet_id,''), COALESCE(t.conversation_id,''),
EXISTS(SELECT 1 FROM media m WHERE m.tweet_id=t.id), EXISTS(SELECT 1 FROM urls ur WHERE ur.tweet_id=t.id)
FROM tweets t LEFT JOIN users u ON u.id=t.author_id `
	if joinFTS {
		sqlText += `JOIN tweets_fts_map fm ON fm.tweet_id=t.id JOIN tweets_fts f ON f.rowid=fm.rowid `
	}
	sqlText += `WHERE ` + strings.Join(where, " AND ") + ` ORDER BY COALESCE(t.created_at,'') DESC, t.id DESC LIMIT ? OFFSET ?`
	args = append(args, limit, offset)
	rows, err := s.db.QueryContext(ctx, sqlText, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.SearchResult
	for rows.Next() {
		var r model.SearchResult
		if err := rows.Scan(&r.TweetID, &r.TextPreview, &r.AuthorUsername, &r.AuthorDisplayName, &r.CreatedAt, &r.QuotedTweetID, &r.ConversationID, &r.HasMedia, &r.HasLinks); err != nil {
			return nil, err
		}
		r.URL = CanonicalURL(r.AuthorUsername, r.TweetID)
		r.TextPreview = preview(r.TextPreview, 260)
		r.Collections, r.BookmarkFolderName = s.collections(ctx, r.TweetID)
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *Store) Show(ctx context.Context, id string) (map[string]any, error) {
	row := s.db.QueryRowContext(ctx, `SELECT t.id,t.text,COALESCE(t.lang,''),t.author_id,COALESCE(u.username,''),COALESCE(u.display_name,''),COALESCE(t.created_at,''),COALESCE(t.conversation_id,''),COALESCE(t.quoted_tweet_id,''),t.reply_count,t.retweet_count,t.like_count,t.quote_count,COALESCE(t.raw_json_id,'') FROM tweets t LEFT JOIN users u ON u.id=t.author_id WHERE t.id=?`, id)
	var lang, authorID, username, display, created, conv, quoted, raw string
	var reply, retweet, like, quote int64
	var text string
	if err := row.Scan(&id, &text, &lang, &authorID, &username, &display, &created, &conv, &quoted, &reply, &retweet, &like, &quote, &raw); err != nil {
		return nil, err
	}
	cols, folder := s.collections(ctx, id)
	return map[string]any{
		"tweet_id": id, "text": text, "lang": lang, "author_id": authorID, "author_username": username,
		"author_display_name": display, "created_at": created, "conversation_id": conv, "quoted_tweet_id": quoted,
		"collections": cols, "bookmark_folder_name": folder, "url": CanonicalURL(username, id),
		"metrics":            map[string]int64{"reply_count": reply, "repost_count": retweet, "like_count": like, "quote_count": quote},
		"raw_json_available": raw != "",
	}, nil
}

func (s *Store) ShowByURL(ctx context.Context, url string) (map[string]any, error) {
	parts := strings.Split(strings.TrimRight(url, "/"), "/")
	if len(parts) == 0 {
		return nil, sql.ErrNoRows
	}
	return s.Show(ctx, parts[len(parts)-1])
}

func (s *Store) Thread(ctx context.Context, focalID, mode string, limit int) (map[string]any, error) {
	if limit <= 0 {
		limit = 200
	}
	base, err := s.Show(ctx, focalID)
	if err != nil {
		return nil, err
	}
	conversationID, _ := base["conversation_id"].(string)
	authorID, _ := base["author_id"].(string)
	if conversationID == "" {
		conversationID = focalID
	}
	where := "t.conversation_id=?"
	args := []any{conversationID}
	threadType := "conversation"
	if mode == "thread" {
		where += " AND t.author_id=?"
		args = append(args, authorID)
		threadType = "thread"
	}
	args = append(args, limit)
	rows, err := s.db.QueryContext(ctx, `SELECT t.id,t.text,COALESCE(u.username,''),COALESCE(u.display_name,''),COALESCE(t.created_at,''),COALESCE(t.conversation_id,'')
FROM tweets t LEFT JOIN users u ON u.id=t.author_id WHERE `+where+` ORDER BY COALESCE(t.created_at,''), t.id LIMIT ?`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []map[string]any{}
	for rows.Next() {
		var id, text, username, display, created, conv string
		if err := rows.Scan(&id, &text, &username, &display, &created, &conv); err != nil {
			return nil, err
		}
		items = append(items, map[string]any{
			"tweet_id": id, "text": preview(text, 500), "author_username": username,
			"author_display_name": display, "created_at": created, "conversation_id": conv,
			"url": CanonicalURL(username, id), "role": threadRole(id, focalID),
		})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	threadID := threadType + ":" + focalID + ":" + mode
	return map[string]any{
		"thread_id": threadID, "thread_type": threadType, "mode": mode, "focal_tweet_id": focalID,
		"conversation_id": conversationID, "expansion_limit": limit, "tweet_count": len(items),
		"is_complete": len(items) < limit, "tweets": items,
	}, nil
}

func (s *Store) collections(ctx context.Context, tweetID string) ([]string, string) {
	rows, err := s.db.QueryContext(ctx, `SELECT collection_type, COALESCE(bookmark_folder_name,'') FROM collections WHERE tweet_id=? ORDER BY collection_type, bookmark_folder_name`, tweetID)
	if err != nil {
		return nil, ""
	}
	defer rows.Close()
	var cols []string
	var folder string
	for rows.Next() {
		var c, f string
		_ = rows.Scan(&c, &f)
		cols = append(cols, c)
		if folder == "" && f != "" {
			folder = f
		}
	}
	return cols, folder
}

func (s *Store) Stats(ctx context.Context) (map[string]any, error) {
	out := map[string]any{}
	var total, tomb, quoted int64
	_ = s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM tweets`).Scan(&total)
	_ = s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM tweets WHERE is_tombstone=1`).Scan(&tomb)
	_ = s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM tweets WHERE quoted_tweet_id IS NOT NULL AND quoted_tweet_id <> ''`).Scan(&quoted)
	out["total_unique_tweets"] = total
	out["tombstones"] = tomb
	out["quoted_tweets"] = quoted
	rows, err := s.db.QueryContext(ctx, `SELECT collection_type, COUNT(DISTINCT tweet_id) FROM collections GROUP BY collection_type ORDER BY collection_type`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	collections := map[string]int64{}
	for rows.Next() {
		var k string
		var v int64
		_ = rows.Scan(&k, &v)
		collections[k] = v
	}
	out["collections"] = collections
	return out, nil
}

func (s *Store) Vacuum(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, "PRAGMA wal_checkpoint(TRUNCATE); VACUUM")
	return err
}

func CanonicalURL(username, id string) string {
	if username == "" {
		username = "i"
	}
	return "https://x.com/" + username + "/status/" + id
}

func normalizeCollection(s string) string {
	switch s {
	case "likes":
		return "like"
	case "bookmarks":
		return "bookmark"
	case "tweets":
		return "tweet"
	case "reposts":
		return "repost"
	case "replies":
		return "reply"
	default:
		return s
	}
}

func threadRole(id, focalID string) string {
	if id == focalID {
		return "focal"
	}
	return "member"
}

func boolInt(v bool) int {
	if v {
		return 1
	}
	return 0
}

func now() string { return time.Now().UTC().Format(time.RFC3339) }

func nullString(v string) any {
	if v == "" {
		return nil
	}
	return v
}

func preview(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

func JSON(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

const schemaSQL = `
PRAGMA journal_mode=WAL;
CREATE TABLE IF NOT EXISTS schema_migrations (version INTEGER PRIMARY KEY, applied_at TEXT NOT NULL);
INSERT OR IGNORE INTO schema_migrations(version, applied_at) VALUES(1, strftime('%Y-%m-%dT%H:%M:%fZ','now'));
CREATE TABLE IF NOT EXISTS users (
  id TEXT PRIMARY KEY, username TEXT, display_name TEXT, avatar_url TEXT, verified INTEGER NOT NULL DEFAULT 0,
  protected INTEGER NOT NULL DEFAULT 0, raw_json_id TEXT, first_seen_at TEXT NOT NULL, last_seen_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS tweets (
  id TEXT PRIMARY KEY, text TEXT NOT NULL, lang TEXT, author_id TEXT NOT NULL, created_at TEXT, conversation_id TEXT,
  in_reply_to_tweet_id TEXT, in_reply_to_user_id TEXT, quoted_tweet_id TEXT, retweeted_tweet_id TEXT,
  is_quote INTEGER NOT NULL DEFAULT 0, is_retweet INTEGER NOT NULL DEFAULT 0, is_reply INTEGER NOT NULL DEFAULT 0,
  is_tombstone INTEGER NOT NULL DEFAULT 0, tombstone_reason TEXT, reply_count INTEGER NOT NULL DEFAULT 0,
  retweet_count INTEGER NOT NULL DEFAULT 0, like_count INTEGER NOT NULL DEFAULT 0, quote_count INTEGER NOT NULL DEFAULT 0,
  bookmark_count INTEGER, view_count INTEGER, raw_json_id TEXT, first_seen_at TEXT NOT NULL, last_seen_at TEXT NOT NULL,
  FOREIGN KEY(author_id) REFERENCES users(id)
);
CREATE TABLE IF NOT EXISTS collections (
  tweet_id TEXT NOT NULL, collection_type TEXT NOT NULL, bookmark_folder_id TEXT, bookmark_folder_id_key TEXT NOT NULL DEFAULT '',
  bookmark_folder_name TEXT, added_at TEXT, synced_at TEXT NOT NULL, sort_index TEXT, source_run_id TEXT, thread_id TEXT,
  PRIMARY KEY(tweet_id, collection_type, bookmark_folder_id_key), FOREIGN KEY(tweet_id) REFERENCES tweets(id),
  CHECK(bookmark_folder_id_key = COALESCE(bookmark_folder_id, ''))
);
CREATE TABLE IF NOT EXISTS bookmark_folders (id TEXT PRIMARY KEY, name TEXT NOT NULL, sort_order INTEGER, first_seen_at TEXT NOT NULL, last_seen_at TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS media (id TEXT PRIMARY KEY, tweet_id TEXT NOT NULL, media_type TEXT NOT NULL, url TEXT, expanded_url TEXT, preview_url TEXT, local_path TEXT, width INTEGER, height INTEGER, duration_ms INTEGER, alt_text TEXT, raw_json_id TEXT, FOREIGN KEY(tweet_id) REFERENCES tweets(id));
CREATE TABLE IF NOT EXISTS urls (id INTEGER PRIMARY KEY AUTOINCREMENT, tweet_id TEXT NOT NULL, url TEXT NOT NULL, expanded_url TEXT, display_url TEXT, title TEXT, description TEXT, FOREIGN KEY(tweet_id) REFERENCES tweets(id));
CREATE TABLE IF NOT EXISTS raw_payloads (id TEXT PRIMARY KEY, kind TEXT NOT NULL, operation_name TEXT, sha256 TEXT NOT NULL, payload BLOB NOT NULL, compressed INTEGER NOT NULL DEFAULT 1, captured_at TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS sync_runs (id TEXT PRIMARY KEY, collection_type TEXT NOT NULL, mode TEXT NOT NULL, status TEXT NOT NULL, started_at TEXT NOT NULL, finished_at TEXT, pages_fetched INTEGER NOT NULL DEFAULT 0, tweets_seen INTEGER NOT NULL DEFAULT 0, tweets_inserted INTEGER NOT NULL DEFAULT 0, tweets_updated INTEGER NOT NULL DEFAULT 0, tweets_unchanged INTEGER NOT NULL DEFAULT 0, errors_count INTEGER NOT NULL DEFAULT 0, rate_limit_count INTEGER NOT NULL DEFAULT 0, error_code TEXT, error_message TEXT);
CREATE TABLE IF NOT EXISTS sync_checkpoints (collection_type TEXT PRIMARY KEY, cursor TEXT, last_tweet_id TEXT, last_sort_index TEXT, source_run_id TEXT, total_seen INTEGER NOT NULL DEFAULT 0, updated_at TEXT NOT NULL, status TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS threads (id TEXT PRIMARY KEY, conversation_id TEXT NOT NULL, root_tweet_id TEXT NOT NULL, focal_tweet_id TEXT NOT NULL, focal_tweet_id_key TEXT NOT NULL, author_id TEXT NOT NULL, thread_type TEXT NOT NULL, mode TEXT NOT NULL, expansion_limit INTEGER NOT NULL, tweet_count INTEGER NOT NULL, is_complete INTEGER NOT NULL DEFAULT 0, fetched_at TEXT NOT NULL, source_run_id TEXT, UNIQUE(thread_type, focal_tweet_id_key, mode), CHECK(focal_tweet_id_key = focal_tweet_id));
CREATE TABLE IF NOT EXISTS thread_tweets (thread_id TEXT NOT NULL, tweet_id TEXT NOT NULL, depth INTEGER NOT NULL DEFAULT 0, position INTEGER NOT NULL DEFAULT 0, role TEXT NOT NULL DEFAULT 'member', PRIMARY KEY(thread_id, tweet_id));
CREATE VIRTUAL TABLE IF NOT EXISTS tweets_fts USING fts5(text, author_username, author_display_name, content='', contentless_delete=1, tokenize='unicode61');
CREATE TABLE IF NOT EXISTS tweets_fts_map (rowid INTEGER PRIMARY KEY, tweet_id TEXT NOT NULL UNIQUE, FOREIGN KEY(tweet_id) REFERENCES tweets(id));
CREATE INDEX IF NOT EXISTS idx_tweets_author ON tweets(author_id);
CREATE INDEX IF NOT EXISTS idx_tweets_created_at ON tweets(created_at);
CREATE INDEX IF NOT EXISTS idx_tweets_conversation ON tweets(conversation_id);
CREATE INDEX IF NOT EXISTS idx_collections_type ON collections(collection_type);
CREATE INDEX IF NOT EXISTS idx_collections_synced ON collections(synced_at);
CREATE INDEX IF NOT EXISTS idx_collections_folder ON collections(bookmark_folder_name);
`
