INSERT OR IGNORE INTO schema_migrations(version, applied_at)
VALUES(5, strftime('%Y-%m-%dT%H:%M:%fZ','now'));

CREATE TABLE IF NOT EXISTS mentions (
  tweet_id TEXT NOT NULL,
  user_id TEXT,
  username TEXT NOT NULL,
  display_name TEXT,
  PRIMARY KEY(tweet_id, username),
  FOREIGN KEY(tweet_id) REFERENCES tweets(id)
);

CREATE TABLE IF NOT EXISTS hashtags (
  tweet_id TEXT NOT NULL,
  tag TEXT NOT NULL,
  PRIMARY KEY(tweet_id, tag),
  FOREIGN KEY(tweet_id) REFERENCES tweets(id)
);
