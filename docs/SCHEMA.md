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

FTS is contentless. Search previews come from `tweets.text`, not from SQLite `snippet()` or `highlight()`.

Use:

```bash
xvault db rebuild-fts --json
```

to rebuild the FTS index from canonical tweet rows.
