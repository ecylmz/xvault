INSERT OR IGNORE INTO schema_migrations(version, applied_at)
VALUES(2, strftime('%Y-%m-%dT%H:%M:%fZ','now'));

CREATE VIRTUAL TABLE IF NOT EXISTS tweets_fts
USING fts5(
  text,
  author_username,
  author_display_name,
  content='',
  contentless_delete=1,
  tokenize='unicode61'
);

CREATE TABLE IF NOT EXISTS tweets_fts_map (
  rowid INTEGER PRIMARY KEY,
  tweet_id TEXT NOT NULL UNIQUE,
  FOREIGN KEY(tweet_id) REFERENCES tweets(id)
);
