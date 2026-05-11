# SQLite Schema

The database contains:

- `users`
- `tweets`
- `collections`
- `bookmark_folders`
- `media`
- `urls`
- `raw_payloads`
- `sync_runs`
- `sync_checkpoints`
- `tweets_fts`
- `tweets_fts_map`

The checked-in SQL files under `migrations/` mirror the schema embedded in the Go binary and can be applied to an empty SQLite database for inspection or external tooling.

FTS is contentless. Search previews come from `tweets.text`, not from SQLite `snippet()` or `highlight()`.

Use:

```bash
xvault db rebuild-fts --json
```

to rebuild the FTS index from canonical tweet rows.
