PRAGMA journal_mode=WAL;
PRAGMA foreign_keys=ON;

CREATE TABLE IF NOT EXISTS schema_migrations (
  version INTEGER PRIMARY KEY,
  applied_at TEXT NOT NULL
);

INSERT OR IGNORE INTO schema_migrations(version, applied_at)
VALUES(1, strftime('%Y-%m-%dT%H:%M:%fZ','now'));

CREATE TABLE IF NOT EXISTS users (
  id TEXT PRIMARY KEY,
  username TEXT,
  display_name TEXT,
  avatar_url TEXT,
  verified INTEGER NOT NULL DEFAULT 0,
  protected INTEGER NOT NULL DEFAULT 0,
  raw_json_id TEXT,
  first_seen_at TEXT NOT NULL,
  last_seen_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS tweets (
  id TEXT PRIMARY KEY,
  text TEXT NOT NULL,
  lang TEXT,
  author_id TEXT NOT NULL,
  created_at TEXT,
  conversation_id TEXT,
  in_reply_to_tweet_id TEXT,
  in_reply_to_user_id TEXT,
  quoted_tweet_id TEXT,
  retweeted_tweet_id TEXT,
  is_quote INTEGER NOT NULL DEFAULT 0,
  is_retweet INTEGER NOT NULL DEFAULT 0,
  is_reply INTEGER NOT NULL DEFAULT 0,
  is_tombstone INTEGER NOT NULL DEFAULT 0,
  tombstone_reason TEXT,
  reply_count INTEGER NOT NULL DEFAULT 0,
  retweet_count INTEGER NOT NULL DEFAULT 0,
  like_count INTEGER NOT NULL DEFAULT 0,
  quote_count INTEGER NOT NULL DEFAULT 0,
  bookmark_count INTEGER,
  view_count INTEGER,
  raw_json_id TEXT,
  first_seen_at TEXT NOT NULL,
  last_seen_at TEXT NOT NULL,
  FOREIGN KEY(author_id) REFERENCES users(id)
);

CREATE TABLE IF NOT EXISTS collections (
  tweet_id TEXT NOT NULL,
  collection_type TEXT NOT NULL,
  bookmark_folder_id TEXT,
  bookmark_folder_id_key TEXT NOT NULL DEFAULT '',
  bookmark_folder_name TEXT,
  added_at TEXT,
  synced_at TEXT NOT NULL,
  sort_index TEXT,
  source_run_id TEXT,
  thread_id TEXT,
  PRIMARY KEY(tweet_id, collection_type, bookmark_folder_id_key),
  FOREIGN KEY(tweet_id) REFERENCES tweets(id),
  CHECK(bookmark_folder_id_key = COALESCE(bookmark_folder_id, ''))
);

CREATE TABLE IF NOT EXISTS bookmark_folders (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL,
  sort_order INTEGER,
  first_seen_at TEXT NOT NULL,
  last_seen_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS media (
  id TEXT PRIMARY KEY,
  tweet_id TEXT NOT NULL,
  media_type TEXT NOT NULL,
  url TEXT,
  expanded_url TEXT,
  preview_url TEXT,
  local_path TEXT,
  width INTEGER,
  height INTEGER,
  duration_ms INTEGER,
  alt_text TEXT,
  raw_json_id TEXT,
  FOREIGN KEY(tweet_id) REFERENCES tweets(id)
);

CREATE TABLE IF NOT EXISTS urls (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  tweet_id TEXT NOT NULL,
  url TEXT NOT NULL,
  expanded_url TEXT,
  display_url TEXT,
  title TEXT,
  description TEXT,
  FOREIGN KEY(tweet_id) REFERENCES tweets(id)
);

CREATE INDEX IF NOT EXISTS idx_tweets_author ON tweets(author_id);
CREATE INDEX IF NOT EXISTS idx_tweets_created_at ON tweets(created_at);
CREATE INDEX IF NOT EXISTS idx_tweets_conversation ON tweets(conversation_id);
CREATE INDEX IF NOT EXISTS idx_collections_type ON collections(collection_type);
CREATE INDEX IF NOT EXISTS idx_collections_synced ON collections(synced_at);
CREATE INDEX IF NOT EXISTS idx_collections_folder ON collections(bookmark_folder_name);
