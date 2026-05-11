INSERT OR IGNORE INTO schema_migrations(version, applied_at)
VALUES(3, strftime('%Y-%m-%dT%H:%M:%fZ','now'));

CREATE TABLE IF NOT EXISTS threads (
  id TEXT PRIMARY KEY,
  conversation_id TEXT NOT NULL,
  root_tweet_id TEXT NOT NULL,
  focal_tweet_id TEXT NOT NULL,
  focal_tweet_id_key TEXT NOT NULL,
  author_id TEXT NOT NULL,
  thread_type TEXT NOT NULL,
  mode TEXT NOT NULL,
  expansion_limit INTEGER NOT NULL,
  tweet_count INTEGER NOT NULL,
  is_complete INTEGER NOT NULL DEFAULT 0,
  fetched_at TEXT NOT NULL,
  source_run_id TEXT,
  UNIQUE(thread_type, focal_tweet_id_key, mode),
  CHECK(focal_tweet_id_key = focal_tweet_id)
);

CREATE TABLE IF NOT EXISTS thread_tweets (
  thread_id TEXT NOT NULL,
  tweet_id TEXT NOT NULL,
  depth INTEGER NOT NULL DEFAULT 0,
  position INTEGER NOT NULL DEFAULT 0,
  role TEXT NOT NULL DEFAULT 'member',
  PRIMARY KEY(thread_id, tweet_id)
);
