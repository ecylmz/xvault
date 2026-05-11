INSERT OR IGNORE INTO schema_migrations(version, applied_at)
VALUES(4, strftime('%Y-%m-%dT%H:%M:%fZ','now'));

CREATE TABLE IF NOT EXISTS raw_payloads (
  id TEXT PRIMARY KEY,
  kind TEXT NOT NULL,
  operation_name TEXT,
  sha256 TEXT NOT NULL,
  payload BLOB NOT NULL,
  compressed INTEGER NOT NULL DEFAULT 1,
  captured_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS sync_runs (
  id TEXT PRIMARY KEY,
  collection_type TEXT NOT NULL,
  mode TEXT NOT NULL,
  status TEXT NOT NULL,
  started_at TEXT NOT NULL,
  finished_at TEXT,
  pages_fetched INTEGER NOT NULL DEFAULT 0,
  tweets_seen INTEGER NOT NULL DEFAULT 0,
  tweets_inserted INTEGER NOT NULL DEFAULT 0,
  tweets_updated INTEGER NOT NULL DEFAULT 0,
  tweets_unchanged INTEGER NOT NULL DEFAULT 0,
  errors_count INTEGER NOT NULL DEFAULT 0,
  rate_limit_count INTEGER NOT NULL DEFAULT 0,
  error_code TEXT,
  error_message TEXT
);

CREATE TABLE IF NOT EXISTS sync_checkpoints (
  collection_type TEXT PRIMARY KEY,
  cursor TEXT,
  last_tweet_id TEXT,
  last_sort_index TEXT,
  source_run_id TEXT,
  total_seen INTEGER NOT NULL DEFAULT 0,
  updated_at TEXT NOT NULL,
  status TEXT NOT NULL
);
