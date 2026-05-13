# Archive Import

`xvault import archive` imports a downloaded Twitter/X archive ZIP into the
same SQLite tables used by live sync.

It is safe to re-run on the same archive. Tweets and collection edges are
upserted by tweet ID, URL/entity rows are de-duplicated, and FTS is refreshed
through the normal store path.

## Usage

Pass a ZIP path explicitly:

```bash
xvault import archive ~/Downloads/twitter-archive.zip --json
```

If the current directory contains exactly one likely archive ZIP, the path is
optional:

```bash
xvault import archive --json
```

The command writes to the configured database, or to `--db` when supplied:

```bash
xvault --db ./scratch.sqlite import archive ./twitter-archive.zip --json
```

The JSON response includes:

- `archive`: parsed account and source-file counts
- `before`: database counts before the import
- `after`: database counts after the import
- `added`: count deltas for total tweets, own tweets, likes, and bookmarks

On a second run against the same database and archive, the `added` values should
be zero unless the database was changed in between.

## Imported Data

Current archive import supports:

- `data/account.js` for the account ID, username, and display name
- `data/tweets.js` and split `tweets-partN.js` files as own tweets
- `data/note-tweet.js` as own long-form note tweets
- `data/deleted-tweets.js` and `data/deleted-tweet-headers.js` as deleted own
  tweet records when present
- `data/like.js` and split `like` or `likes` part files as liked tweets
- `data/bookmark.js` or `data/bookmarks.js` files when an archive/export
  contains bookmark records

Tweet entities from the archive are normalized into the existing `urls`,
`media`, `mentions`, and `hashtags` tables when the source file contains them.
Likes and bookmarks in Twitter archives usually contain only tweet ID, text, and
status URL, so author IDs may be archive-local placeholders until a later live
sync sees the canonical X profile ID.

Direct messages and ad/personalization files are intentionally not imported
because `xvault` does not currently have DM or ad archive tables.

## Verification

After an import, use the normal local commands:

```bash
xvault stats --json
xvault search --recent --source tweets --limit 10 --json
xvault search --recent --source likes --limit 10 --json
xvault db integrity --json
```

Use `--db` with a scratch database when testing an archive before importing into
your default `~/.local/share/xvault/xvault.sqlite` database.
