package store

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ecylmz/xvault/internal/model"
	_ "modernc.org/sqlite"
)

type Store struct {
	db   *sql.DB
	path string
}

type Checkpoint struct {
	CollectionType string `json:"collection_type"`
	Cursor         string `json:"cursor,omitempty"`
	LastTweetID    string `json:"last_tweet_id,omitempty"`
	LastSortIndex  string `json:"last_sort_index,omitempty"`
	SourceRunID    string `json:"source_run_id,omitempty"`
	TotalSeen      int    `json:"total_seen"`
	Status         string `json:"status"`
	UpdatedAt      string `json:"updated_at,omitempty"`
}

type SyncRun struct {
	ID              string `json:"id"`
	CollectionType  string `json:"collection_type"`
	Mode            string `json:"mode"`
	Status          string `json:"status"`
	PagesFetched    int    `json:"pages_fetched"`
	TweetsSeen      int    `json:"tweets_seen"`
	TweetsInserted  int    `json:"tweets_inserted"`
	TweetsUpdated   int    `json:"tweets_updated"`
	TweetsUnchanged int    `json:"tweets_unchanged"`
	ErrorsCount     int    `json:"errors_count"`
	RateLimitCount  int    `json:"rate_limit_count"`
	ErrorCode       string `json:"error_code,omitempty"`
	ErrorMessage    string `json:"error_message,omitempty"`
	StartedAt       string `json:"started_at"`
	FinishedAt      string `json:"finished_at,omitempty"`
}

func Open(path string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", path+"?_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)")
	if err != nil {
		return nil, err
	}
	s := &Store{db: db, path: path}
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
	for _, m := range page.Mentions {
		if m.TweetID == "" || m.Username == "" {
			continue
		}
		if _, err := tx.ExecContext(ctx, `INSERT OR REPLACE INTO mentions(tweet_id, user_id, username, display_name) VALUES(?,?,?,?)`, m.TweetID, nullString(m.UserID), m.Username, nullString(m.DisplayName)); err != nil {
			return err
		}
	}
	for _, h := range page.Hashtags {
		if h.TweetID == "" || h.Tag == "" {
			continue
		}
		if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO hashtags(tweet_id, tag) VALUES(?,?)`, h.TweetID, h.Tag); err != nil {
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
	return s.SearchWithFilters(ctx, query, source, author, folder, "", "", false, false, limit, offset)
}

func (s *Store) SearchWithFilters(ctx context.Context, query, source, author, folder, fromDate, toDate string, hasMedia, hasLink bool, limit, offset int) ([]model.SearchResult, error) {
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
	if fromDate != "" {
		where = append(where, "COALESCE(t.created_at,'') >= ?")
		args = append(args, fromDate)
	}
	if toDate != "" {
		where = append(where, "COALESCE(t.created_at,'') <= ?")
		args = append(args, toDate)
	}
	if hasMedia {
		where = append(where, "EXISTS(SELECT 1 FROM media m WHERE m.tweet_id=t.id)")
	}
	if hasLink {
		where = append(where, "EXISTS(SELECT 1 FROM urls ur WHERE ur.tweet_id=t.id)")
	}
	sqlText := `SELECT t.id, t.text, COALESCE(u.username,''), COALESCE(u.display_name,''), COALESCE(t.created_at,''), COALESCE(t.quoted_tweet_id,''), COALESCE(qt.text,''), COALESCE(qu.username,''), COALESCE(qu.display_name,''), COALESCE(t.conversation_id,''),
EXISTS(SELECT 1 FROM media m WHERE m.tweet_id=t.id), EXISTS(SELECT 1 FROM urls ur WHERE ur.tweet_id=t.id)
FROM tweets t LEFT JOIN users u ON u.id=t.author_id LEFT JOIN tweets qt ON qt.id=t.quoted_tweet_id AND qt.is_tombstone=0 LEFT JOIN users qu ON qu.id=qt.author_id `
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
	out := []model.SearchResult{}
	for rows.Next() {
		var r model.SearchResult
		if err := rows.Scan(&r.TweetID, &r.TextPreview, &r.AuthorUsername, &r.AuthorDisplayName, &r.CreatedAt, &r.QuotedTweetID, &r.QuotedTextPreview, &r.QuotedAuthorUsername, &r.QuotedAuthorDisplayName, &r.ConversationID, &r.HasMedia, &r.HasLinks); err != nil {
			return nil, err
		}
		r.URL = CanonicalURL(r.AuthorUsername, r.TweetID)
		r.TextPreview = preview(r.TextPreview, 260)
		r.QuotedTextPreview = preview(r.QuotedTextPreview, 260)
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
	urls, err := s.tweetURLs(ctx, id)
	if err != nil {
		return nil, err
	}
	media, err := s.tweetMedia(ctx, id)
	if err != nil {
		return nil, err
	}
	mentions, err := s.tweetMentions(ctx, id)
	if err != nil {
		return nil, err
	}
	hashtags, err := s.tweetHashtags(ctx, id)
	if err != nil {
		return nil, err
	}
	threads, err := s.tweetThreadMetadata(ctx, id)
	if err != nil {
		return nil, err
	}
	paths := s.localExportPaths(id)
	quotedSummary, err := s.tweetSummary(ctx, quoted)
	if err != nil {
		return nil, err
	}
	repostedID, repostedSummary, err := s.repostedTweetSummary(ctx, id)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"tweet_id": id, "text": text, "lang": lang, "author_id": authorID, "author_username": username,
		"author_display_name": display, "created_at": created, "conversation_id": conv, "quoted_tweet_id": quoted,
		"collections": cols, "bookmark_folder_name": folder, "url": CanonicalURL(username, id),
		"metrics":            map[string]int64{"reply_count": reply, "repost_count": retweet, "like_count": like, "quote_count": quote},
		"links":              urls,
		"media":              media,
		"mentions":           mentions,
		"hashtags":           hashtags,
		"quoted_tweet":       quotedSummary,
		"reposted_tweet_id":  repostedID,
		"reposted_tweet":     repostedSummary,
		"threads":            threads,
		"local_export_paths": paths,
		"raw_json_available": raw != "",
		"raw_json_id":        raw,
	}, nil
}

func (s *Store) tweetURLs(ctx context.Context, tweetID string) ([]model.URL, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT url, COALESCE(expanded_url,''), COALESCE(display_url,'') FROM urls WHERE tweet_id=? ORDER BY id`, tweetID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.URL{}
	for rows.Next() {
		u := model.URL{TweetID: tweetID}
		if err := rows.Scan(&u.URL, &u.ExpandedURL, &u.DisplayURL); err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

func (s *Store) tweetMedia(ctx context.Context, tweetID string) ([]model.Media, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, media_type, COALESCE(url,''), COALESCE(expanded_url,''), COALESCE(preview_url,''), COALESCE(alt_text,'') FROM media WHERE tweet_id=? ORDER BY id`, tweetID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.Media{}
	for rows.Next() {
		m := model.Media{TweetID: tweetID}
		if err := rows.Scan(&m.ID, &m.MediaType, &m.URL, &m.ExpandedURL, &m.PreviewURL, &m.AltText); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

func (s *Store) tweetMentions(ctx context.Context, tweetID string) ([]model.Mention, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT COALESCE(user_id,''), username, COALESCE(display_name,'') FROM mentions WHERE tweet_id=? ORDER BY username`, tweetID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.Mention{}
	for rows.Next() {
		m := model.Mention{TweetID: tweetID}
		if err := rows.Scan(&m.UserID, &m.Username, &m.DisplayName); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

func (s *Store) tweetHashtags(ctx context.Context, tweetID string) ([]model.Hashtag, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT tag FROM hashtags WHERE tweet_id=? ORDER BY LOWER(tag), tag`, tweetID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.Hashtag{}
	for rows.Next() {
		h := model.Hashtag{TweetID: tweetID}
		if err := rows.Scan(&h.Tag); err != nil {
			return nil, err
		}
		out = append(out, h)
	}
	return out, rows.Err()
}

func (s *Store) tweetThreadMetadata(ctx context.Context, tweetID string) ([]map[string]any, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT th.id, th.thread_type, th.mode, th.focal_tweet_id, th.conversation_id, th.expansion_limit, th.tweet_count, th.is_complete, tt.role, tt.position
FROM threads th JOIN thread_tweets tt ON tt.thread_id=th.id WHERE tt.tweet_id=? ORDER BY th.thread_type, th.mode, tt.position`, tweetID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var id, threadType, mode, focalID, conversationID, role string
		var limit, count, complete, position int
		if err := rows.Scan(&id, &threadType, &mode, &focalID, &conversationID, &limit, &count, &complete, &role, &position); err != nil {
			return nil, err
		}
		out = append(out, map[string]any{
			"thread_id": id, "thread_type": threadType, "mode": mode, "focal_tweet_id": focalID,
			"conversation_id": conversationID, "expansion_limit": limit, "tweet_count": count,
			"is_complete": complete != 0, "role": role, "position": position,
		})
	}
	return out, rows.Err()
}

func (s *Store) localExportPaths(tweetID string) map[string]string {
	base := filepath.Join(os.Getenv("HOME"), ".local/share/xvault/exports")
	candidates := map[string]string{}
	for name, dir := range map[string]string{
		"markdown": filepath.Join(base, "markdown"),
		"hermes":   filepath.Join(base, "hermes"),
		"obsidian": filepath.Join(base, "obsidian"),
	} {
		if path := findExportedTweetPath(dir, tweetID); path != "" {
			candidates[name] = path
		}
	}
	return candidates
}

func findExportedTweetPath(root, tweetID string) string {
	info, err := os.Stat(root)
	if err != nil || !info.IsDir() {
		return ""
	}
	var found string
	_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil || found != "" {
			return nil
		}
		if d.IsDir() {
			return nil
		}
		if strings.HasSuffix(d.Name(), ".md") && strings.Contains(d.Name(), tweetID) {
			found = path
			return filepath.SkipAll
		}
		return nil
	})
	return found
}

func (s *Store) tweetSummary(ctx context.Context, tweetID string) (map[string]any, error) {
	if tweetID == "" {
		return nil, nil
	}
	row := s.db.QueryRowContext(ctx, `SELECT t.id, t.text, COALESCE(u.username,''), COALESCE(u.display_name,''), COALESCE(t.created_at,''), t.is_tombstone, COALESCE(t.tombstone_reason,'') FROM tweets t LEFT JOIN users u ON u.id=t.author_id WHERE t.id=?`, tweetID)
	var id, text, username, display, created, reason string
	var tombstone int
	if err := row.Scan(&id, &text, &username, &display, &created, &tombstone, &reason); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return map[string]any{"tweet_id": id, "text": text, "author_username": username, "author_display_name": display, "created_at": created, "url": CanonicalURL(username, id), "is_tombstone": tombstone != 0, "tombstone_reason": reason}, nil
}

func (s *Store) repostedTweetSummary(ctx context.Context, tweetID string) (string, map[string]any, error) {
	var repostedID string
	err := s.db.QueryRowContext(ctx, `SELECT COALESCE(retweeted_tweet_id,'') FROM tweets WHERE id=?`, tweetID).Scan(&repostedID)
	if err != nil {
		return "", nil, err
	}
	summary, err := s.tweetSummary(ctx, repostedID)
	return repostedID, summary, err
}

func (s *Store) RawPayload(ctx context.Context, id string) (json.RawMessage, error) {
	var payload []byte
	var compressed int
	err := s.db.QueryRowContext(ctx, `SELECT payload, compressed FROM raw_payloads WHERE id=?`, id).Scan(&payload, &compressed)
	if err != nil {
		return nil, err
	}
	if compressed != 0 {
		gr, err := gzip.NewReader(bytes.NewReader(payload))
		if err != nil {
			return nil, err
		}
		defer gr.Close()
		payload, err = io.ReadAll(gr)
		if err != nil {
			return nil, err
		}
	}
	if !json.Valid(payload) {
		return nil, fmt.Errorf("raw payload %s is not valid JSON", id)
	}
	return json.RawMessage(payload), nil
}

func (s *Store) ShowByURL(ctx context.Context, url string) (map[string]any, error) {
	parts := strings.Split(strings.TrimRight(url, "/"), "/")
	if len(parts) == 0 {
		return nil, sql.ErrNoRows
	}
	return s.Show(ctx, parts[len(parts)-1])
}

func (s *Store) CollectionTweetIDs(ctx context.Context, collection string, limit int) ([]string, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := s.db.QueryContext(ctx, `SELECT DISTINCT tweet_id FROM collections WHERE collection_type=? ORDER BY synced_at DESC, tweet_id DESC LIMIT ?`, normalizeCollection(collection), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
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
	isComplete := len(items) < limit
	if err := s.UpsertThread(ctx, ThreadRecord{
		ID:             threadID,
		ConversationID: conversationID,
		RootTweetID:    conversationID,
		FocalTweetID:   focalID,
		AuthorID:       authorID,
		ThreadType:     threadType,
		Mode:           mode,
		ExpansionLimit: limit,
		TweetCount:     len(items),
		IsComplete:     isComplete,
		Tweets:         threadRecordTweets(items),
	}); err != nil {
		return nil, err
	}
	return map[string]any{
		"thread_id": threadID, "thread_type": threadType, "mode": mode, "focal_tweet_id": focalID,
		"conversation_id": conversationID, "expansion_limit": limit, "tweet_count": len(items),
		"is_complete": isComplete, "tweets": items,
	}, nil
}

type ThreadRecord struct {
	ID             string
	ConversationID string
	RootTweetID    string
	FocalTweetID   string
	AuthorID       string
	ThreadType     string
	Mode           string
	ExpansionLimit int
	TweetCount     int
	IsComplete     bool
	SourceRunID    string
	Tweets         []ThreadTweet
}

type ThreadTweet struct {
	TweetID  string
	Depth    int
	Position int
	Role     string
}

type BookmarkFolder struct {
	ID        string `json:"id,omitempty"`
	Name      string `json:"name"`
	Count     int64  `json:"count"`
	FirstSeen string `json:"first_seen_at,omitempty"`
	LastSeen  string `json:"last_seen_at,omitempty"`
}

func (s *Store) UpsertThread(ctx context.Context, rec ThreadRecord) error {
	if rec.ID == "" || rec.ConversationID == "" || rec.FocalTweetID == "" || rec.ThreadType == "" || rec.Mode == "" {
		return fmt.Errorf("thread record is missing required fields")
	}
	if rec.RootTweetID == "" {
		rec.RootTweetID = rec.ConversationID
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	_, err = tx.ExecContext(ctx, `INSERT INTO threads(id, conversation_id, root_tweet_id, focal_tweet_id, focal_tweet_id_key, author_id, thread_type, mode, expansion_limit, tweet_count, is_complete, fetched_at, source_run_id)
VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)
ON CONFLICT(thread_type, focal_tweet_id_key, mode) DO UPDATE SET conversation_id=excluded.conversation_id, root_tweet_id=excluded.root_tweet_id, author_id=excluded.author_id, expansion_limit=excluded.expansion_limit, tweet_count=excluded.tweet_count, is_complete=excluded.is_complete, fetched_at=excluded.fetched_at, source_run_id=excluded.source_run_id`,
		rec.ID, rec.ConversationID, rec.RootTweetID, rec.FocalTweetID, rec.FocalTweetID, rec.AuthorID, rec.ThreadType, rec.Mode, rec.ExpansionLimit, rec.TweetCount, boolInt(rec.IsComplete), now(), nullString(rec.SourceRunID))
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM thread_tweets WHERE thread_id=?`, rec.ID); err != nil {
		return err
	}
	for i, tw := range rec.Tweets {
		if tw.TweetID == "" {
			continue
		}
		position := tw.Position
		if position == 0 {
			position = i + 1
		}
		role := tw.Role
		if role == "" {
			role = "member"
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO thread_tweets(thread_id, tweet_id, depth, position, role) VALUES(?,?,?,?,?)`, rec.ID, tw.TweetID, tw.Depth, position, role); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func threadRecordTweets(items []map[string]any) []ThreadTweet {
	out := make([]ThreadTweet, 0, len(items))
	for i, item := range items {
		id, _ := item["tweet_id"].(string)
		role, _ := item["role"].(string)
		out = append(out, ThreadTweet{TweetID: id, Position: i + 1, Role: role})
	}
	return out
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
	folders := map[string]int64{}
	folderRows, err := s.db.QueryContext(ctx, `SELECT COALESCE(bookmark_folder_name,''), COUNT(DISTINCT tweet_id) FROM collections WHERE collection_type='bookmark' GROUP BY COALESCE(bookmark_folder_name,'') ORDER BY 1`)
	if err != nil {
		return nil, err
	}
	defer folderRows.Close()
	for folderRows.Next() {
		var name string
		var count int64
		if err := folderRows.Scan(&name, &count); err != nil {
			return nil, err
		}
		if name == "" {
			name = "(none)"
		}
		folders[name] = count
	}
	if err := folderRows.Err(); err != nil {
		return nil, err
	}
	out["bookmark_folders"] = folders
	var rawBytes sql.NullInt64
	_ = s.db.QueryRowContext(ctx, `SELECT SUM(length(payload)) FROM raw_payloads`).Scan(&rawBytes)
	if rawBytes.Valid {
		out["raw_payload_size_bytes"] = rawBytes.Int64
	} else {
		out["raw_payload_size_bytes"] = int64(0)
	}
	if info, err := os.Stat(s.path); err == nil {
		out["database_size_bytes"] = info.Size()
	}
	return out, nil
}

func (s *Store) BookmarkFolders(ctx context.Context) ([]BookmarkFolder, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT COALESCE(bf.id,''), COALESCE(NULLIF(bf.name,''), COALESCE(c.bookmark_folder_name,'')), COUNT(DISTINCT c.tweet_id), COALESCE(bf.first_seen_at,''), COALESCE(bf.last_seen_at,'')
FROM collections c
LEFT JOIN bookmark_folders bf ON bf.id = c.bookmark_folder_id
WHERE c.collection_type='bookmark'
GROUP BY COALESCE(bf.id,''), COALESCE(NULLIF(bf.name,''), COALESCE(c.bookmark_folder_name,''))
ORDER BY 2`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []BookmarkFolder{}
	for rows.Next() {
		var f BookmarkFolder
		if err := rows.Scan(&f.ID, &f.Name, &f.Count, &f.FirstSeen, &f.LastSeen); err != nil {
			return nil, err
		}
		if f.Name == "" {
			f.Name = "(none)"
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

func (s *Store) BookmarkFolderIDByName(ctx context.Context, name string) (string, bool, error) {
	if strings.TrimSpace(name) == "" {
		return "", false, nil
	}
	var id string
	err := s.db.QueryRowContext(ctx, `SELECT id FROM bookmark_folders WHERE name=? OR id=? ORDER BY CASE WHEN name=? THEN 0 ELSE 1 END, id LIMIT 1`, name, name, name).Scan(&id)
	if err == nil {
		return id, true, nil
	}
	if err != sql.ErrNoRows {
		return "", false, err
	}
	err = s.db.QueryRowContext(ctx, `SELECT bookmark_folder_id FROM collections WHERE bookmark_folder_name=? AND bookmark_folder_id IS NOT NULL AND bookmark_folder_id <> '' ORDER BY synced_at DESC, bookmark_folder_id LIMIT 1`, name).Scan(&id)
	if err == nil {
		return id, true, nil
	}
	if err == sql.ErrNoRows {
		return "", false, nil
	}
	return "", false, err
}

func (s *Store) CollectionCount(ctx context.Context, collection string) (int64, error) {
	var count int64
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(DISTINCT tweet_id) FROM collections WHERE collection_type=?`, normalizeCollection(collection)).Scan(&count)
	return count, err
}

func (s *Store) LastSuccessfulSync(ctx context.Context, collection string) (SyncRun, bool, error) {
	var run SyncRun
	var errorCode, errorMessage, finishedAt sql.NullString
	err := s.db.QueryRowContext(ctx, `SELECT id, collection_type, mode, status, pages_fetched, tweets_seen, tweets_inserted, tweets_updated, tweets_unchanged, errors_count, rate_limit_count, error_code, error_message, started_at, finished_at
FROM sync_runs WHERE collection_type=? AND status='success' ORDER BY finished_at DESC, started_at DESC LIMIT 1`, normalizeCollection(collection)).
		Scan(&run.ID, &run.CollectionType, &run.Mode, &run.Status, &run.PagesFetched, &run.TweetsSeen, &run.TweetsInserted, &run.TweetsUpdated, &run.TweetsUnchanged, &run.ErrorsCount, &run.RateLimitCount, &errorCode, &errorMessage, &run.StartedAt, &finishedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return SyncRun{}, false, nil
	}
	if err != nil {
		return SyncRun{}, false, err
	}
	run.ErrorCode = errorCode.String
	run.ErrorMessage = errorMessage.String
	run.FinishedAt = finishedAt.String
	return run, true, nil
}

func (s *Store) Vacuum(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, "PRAGMA wal_checkpoint(TRUNCATE); VACUUM")
	return err
}

func (s *Store) SaveCheckpoint(ctx context.Context, cp Checkpoint) error {
	if cp.CollectionType == "" {
		return fmt.Errorf("checkpoint collection type is required")
	}
	if cp.Status == "" {
		cp.Status = "in_progress"
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO sync_checkpoints(collection_type, cursor, last_tweet_id, last_sort_index, source_run_id, total_seen, updated_at, status)
VALUES(?,?,?,?,?,?,?,?)
ON CONFLICT(collection_type) DO UPDATE SET cursor=excluded.cursor, last_tweet_id=excluded.last_tweet_id, last_sort_index=excluded.last_sort_index, source_run_id=excluded.source_run_id, total_seen=excluded.total_seen, updated_at=excluded.updated_at, status=excluded.status`,
		cp.CollectionType, nullString(cp.Cursor), nullString(cp.LastTweetID), nullString(cp.LastSortIndex), nullString(cp.SourceRunID), cp.TotalSeen, now(), cp.Status)
	return err
}

func (s *Store) LoadCheckpoint(ctx context.Context, collectionType string) (Checkpoint, bool, error) {
	var cp Checkpoint
	var cursor, lastTweetID, lastSortIndex, sourceRunID sql.NullString
	err := s.db.QueryRowContext(ctx, `SELECT collection_type, cursor, last_tweet_id, last_sort_index, source_run_id, total_seen, status, updated_at FROM sync_checkpoints WHERE collection_type=?`, collectionType).
		Scan(&cp.CollectionType, &cursor, &lastTweetID, &lastSortIndex, &sourceRunID, &cp.TotalSeen, &cp.Status, &cp.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Checkpoint{}, false, nil
	}
	if err != nil {
		return Checkpoint{}, false, err
	}
	cp.Cursor = cursor.String
	cp.LastTweetID = lastTweetID.String
	cp.LastSortIndex = lastSortIndex.String
	cp.SourceRunID = sourceRunID.String
	return cp, true, nil
}

func (s *Store) ListCheckpoints(ctx context.Context) ([]Checkpoint, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT collection_type, cursor, last_tweet_id, last_sort_index, source_run_id, total_seen, status, updated_at FROM sync_checkpoints ORDER BY collection_type`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Checkpoint{}
	for rows.Next() {
		var cp Checkpoint
		var cursor, lastTweetID, lastSortIndex, sourceRunID sql.NullString
		if err := rows.Scan(&cp.CollectionType, &cursor, &lastTweetID, &lastSortIndex, &sourceRunID, &cp.TotalSeen, &cp.Status, &cp.UpdatedAt); err != nil {
			return nil, err
		}
		cp.Cursor = cursor.String
		cp.LastTweetID = lastTweetID.String
		cp.LastSortIndex = lastSortIndex.String
		cp.SourceRunID = sourceRunID.String
		out = append(out, cp)
	}
	return out, rows.Err()
}

func (s *Store) ClearCheckpoint(ctx context.Context, collectionType string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM sync_checkpoints WHERE collection_type=?`, collectionType)
	return err
}

func (s *Store) StartSyncRun(ctx context.Context, collectionType, mode string) (string, error) {
	if collectionType == "" {
		return "", fmt.Errorf("sync run collection type is required")
	}
	if mode == "" {
		mode = "incremental"
	}
	id := newRunID()
	_, err := s.db.ExecContext(ctx, `INSERT INTO sync_runs(id, collection_type, mode, status, started_at) VALUES(?,?,?,?,?)`,
		id, collectionType, mode, "in_progress", now())
	return id, err
}

func (s *Store) FinishSyncRun(ctx context.Context, run SyncRun) error {
	if run.ID == "" {
		return fmt.Errorf("sync run id is required")
	}
	_, err := s.db.ExecContext(ctx, `UPDATE sync_runs SET status=?, finished_at=?, pages_fetched=?, tweets_seen=?, tweets_inserted=?, tweets_updated=?, tweets_unchanged=?, errors_count=?, rate_limit_count=?, error_code=?, error_message=? WHERE id=?`,
		run.Status, now(), run.PagesFetched, run.TweetsSeen, run.TweetsInserted, run.TweetsUpdated, run.TweetsUnchanged, run.ErrorsCount, run.RateLimitCount, nullString(run.ErrorCode), nullString(run.ErrorMessage), run.ID)
	return err
}

func (s *Store) GetSyncRun(ctx context.Context, id string) (SyncRun, error) {
	var run SyncRun
	var errorCode, errorMessage, finishedAt sql.NullString
	err := s.db.QueryRowContext(ctx, `SELECT id, collection_type, mode, status, pages_fetched, tweets_seen, tweets_inserted, tweets_updated, tweets_unchanged, errors_count, rate_limit_count, error_code, error_message, started_at, finished_at FROM sync_runs WHERE id=?`, id).
		Scan(&run.ID, &run.CollectionType, &run.Mode, &run.Status, &run.PagesFetched, &run.TweetsSeen, &run.TweetsInserted, &run.TweetsUpdated, &run.TweetsUnchanged, &run.ErrorsCount, &run.RateLimitCount, &errorCode, &errorMessage, &run.StartedAt, &finishedAt)
	if err != nil {
		return SyncRun{}, err
	}
	run.ErrorCode = errorCode.String
	run.ErrorMessage = errorMessage.String
	run.FinishedAt = finishedAt.String
	return run, nil
}

func (s *Store) ListSyncRuns(ctx context.Context, collection, status string, limit int) ([]SyncRun, error) {
	if limit <= 0 {
		limit = 20
	} else if limit > 500 {
		limit = 500
	}
	where := []string{"1=1"}
	args := []any{}
	if collection != "" && collection != "all" {
		where = append(where, "collection_type=?")
		args = append(args, normalizeCollection(collection))
	}
	if status != "" && status != "all" {
		where = append(where, "status=?")
		args = append(args, status)
	}
	args = append(args, limit)
	rows, err := s.db.QueryContext(ctx, `SELECT id, collection_type, mode, status, pages_fetched, tweets_seen, tweets_inserted, tweets_updated, tweets_unchanged, errors_count, rate_limit_count, COALESCE(error_code,''), COALESCE(error_message,''), started_at, COALESCE(finished_at,'')
FROM sync_runs WHERE `+strings.Join(where, " AND ")+` ORDER BY started_at DESC, id DESC LIMIT ?`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SyncRun
	for rows.Next() {
		var run SyncRun
		if err := rows.Scan(&run.ID, &run.CollectionType, &run.Mode, &run.Status, &run.PagesFetched, &run.TweetsSeen, &run.TweetsInserted, &run.TweetsUpdated, &run.TweetsUnchanged, &run.ErrorsCount, &run.RateLimitCount, &run.ErrorCode, &run.ErrorMessage, &run.StartedAt, &run.FinishedAt); err != nil {
			return nil, err
		}
		out = append(out, run)
	}
	return out, rows.Err()
}

func (s *Store) UnresolvedFailedSyncRuns(ctx context.Context, limit int) ([]SyncRun, error) {
	if limit <= 0 {
		limit = 20
	} else if limit > 500 {
		limit = 500
	}
	rows, err := s.db.QueryContext(ctx, `SELECT f.id, f.collection_type, f.mode, f.status, f.pages_fetched, f.tweets_seen, f.tweets_inserted, f.tweets_updated, f.tweets_unchanged, f.errors_count, f.rate_limit_count, COALESCE(f.error_code,''), COALESCE(f.error_message,''), f.started_at, COALESCE(f.finished_at,'')
FROM sync_runs f
WHERE f.status='failed'
  AND NOT EXISTS (
    SELECT 1 FROM sync_runs s
    WHERE s.collection_type=f.collection_type
      AND s.status='success'
      AND COALESCE(s.finished_at, s.started_at) >= COALESCE(f.finished_at, f.started_at)
  )
ORDER BY f.started_at DESC, f.id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SyncRun
	for rows.Next() {
		var run SyncRun
		if err := rows.Scan(&run.ID, &run.CollectionType, &run.Mode, &run.Status, &run.PagesFetched, &run.TweetsSeen, &run.TweetsInserted, &run.TweetsUpdated, &run.TweetsUnchanged, &run.ErrorsCount, &run.RateLimitCount, &run.ErrorCode, &run.ErrorMessage, &run.StartedAt, &run.FinishedAt); err != nil {
			return nil, err
		}
		out = append(out, run)
	}
	return out, rows.Err()
}

func (s *Store) SanitizeSyncRunErrors(ctx context.Context) (int64, error) {
	res, err := s.db.ExecContext(ctx, `UPDATE sync_runs
SET error_message = CASE error_code
  WHEN 'AUTH_EXPIRED' THEN 'authentication cookies were rejected by X'
  WHEN 'RATE_LIMITED' THEN 'rate limited by X'
  WHEN 'QUERY_ID_STALE' THEN 'X GraphQL query ID appears stale'
  ELSE 'sync failed'
END
WHERE error_message IS NOT NULL AND error_message <> '' AND (
  instr(error_message, '{') > 0 OR instr(error_message, 'auth_token') > 0 OR instr(error_message, 'ct0') > 0 OR instr(error_message, 'Cookie') > 0 OR length(error_message) > 160
)`)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

func newRunID() string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprintf("run-%d", time.Now().UTC().UnixNano())
	}
	return fmt.Sprintf("run-%d-%016x", time.Now().UTC().UnixNano(), binary.BigEndian.Uint64(b[:]))
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
	case "posts":
		return "post"
	case "threads":
		return "thread"
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
CREATE TABLE IF NOT EXISTS mentions (tweet_id TEXT NOT NULL, user_id TEXT, username TEXT NOT NULL, display_name TEXT, PRIMARY KEY(tweet_id, username), FOREIGN KEY(tweet_id) REFERENCES tweets(id));
CREATE TABLE IF NOT EXISTS hashtags (tweet_id TEXT NOT NULL, tag TEXT NOT NULL, PRIMARY KEY(tweet_id, tag), FOREIGN KEY(tweet_id) REFERENCES tweets(id));
INSERT OR IGNORE INTO schema_migrations(version, applied_at) VALUES(5, strftime('%Y-%m-%dT%H:%M:%fZ','now'));
CREATE TABLE IF NOT EXISTS raw_payloads (id TEXT PRIMARY KEY, kind TEXT NOT NULL, operation_name TEXT, sha256 TEXT NOT NULL, payload BLOB NOT NULL, compressed INTEGER NOT NULL DEFAULT 1, captured_at TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS sync_runs (id TEXT PRIMARY KEY, collection_type TEXT NOT NULL, mode TEXT NOT NULL, status TEXT NOT NULL, started_at TEXT NOT NULL, finished_at TEXT, pages_fetched INTEGER NOT NULL DEFAULT 0, tweets_seen INTEGER NOT NULL DEFAULT 0, tweets_inserted INTEGER NOT NULL DEFAULT 0, tweets_updated INTEGER NOT NULL DEFAULT 0, tweets_unchanged INTEGER NOT NULL DEFAULT 0, errors_count INTEGER NOT NULL DEFAULT 0, rate_limit_count INTEGER NOT NULL DEFAULT 0, error_code TEXT, error_message TEXT);
CREATE TABLE IF NOT EXISTS sync_checkpoints (collection_type TEXT PRIMARY KEY, cursor TEXT, last_tweet_id TEXT, last_sort_index TEXT, source_run_id TEXT, total_seen INTEGER NOT NULL DEFAULT 0, updated_at TEXT NOT NULL, status TEXT NOT NULL);
INSERT OR IGNORE INTO schema_migrations(version, applied_at) VALUES(4, strftime('%Y-%m-%dT%H:%M:%fZ','now'));
CREATE TABLE IF NOT EXISTS threads (id TEXT PRIMARY KEY, conversation_id TEXT NOT NULL, root_tweet_id TEXT NOT NULL, focal_tweet_id TEXT NOT NULL, focal_tweet_id_key TEXT NOT NULL, author_id TEXT NOT NULL, thread_type TEXT NOT NULL, mode TEXT NOT NULL, expansion_limit INTEGER NOT NULL, tweet_count INTEGER NOT NULL, is_complete INTEGER NOT NULL DEFAULT 0, fetched_at TEXT NOT NULL, source_run_id TEXT, UNIQUE(thread_type, focal_tweet_id_key, mode), CHECK(focal_tweet_id_key = focal_tweet_id));
CREATE TABLE IF NOT EXISTS thread_tweets (thread_id TEXT NOT NULL, tweet_id TEXT NOT NULL, depth INTEGER NOT NULL DEFAULT 0, position INTEGER NOT NULL DEFAULT 0, role TEXT NOT NULL DEFAULT 'member', PRIMARY KEY(thread_id, tweet_id));
INSERT OR IGNORE INTO schema_migrations(version, applied_at) VALUES(3, strftime('%Y-%m-%dT%H:%M:%fZ','now'));
CREATE VIRTUAL TABLE IF NOT EXISTS tweets_fts USING fts5(text, author_username, author_display_name, content='', contentless_delete=1, tokenize='unicode61');
CREATE TABLE IF NOT EXISTS tweets_fts_map (rowid INTEGER PRIMARY KEY, tweet_id TEXT NOT NULL UNIQUE, FOREIGN KEY(tweet_id) REFERENCES tweets(id));
INSERT OR IGNORE INTO schema_migrations(version, applied_at) VALUES(2, strftime('%Y-%m-%dT%H:%M:%fZ','now'));
CREATE INDEX IF NOT EXISTS idx_tweets_author ON tweets(author_id);
CREATE INDEX IF NOT EXISTS idx_tweets_created_at ON tweets(created_at);
CREATE INDEX IF NOT EXISTS idx_tweets_conversation ON tweets(conversation_id);
CREATE INDEX IF NOT EXISTS idx_collections_type ON collections(collection_type);
CREATE INDEX IF NOT EXISTS idx_collections_synced ON collections(synced_at);
CREATE INDEX IF NOT EXISTS idx_collections_folder ON collections(bookmark_folder_name);
`
