# Troubleshooting

## Auth Missing

Check:

```bash
xvault auth status --json
```

Then verify `~/.config/xvault/.env` contains `XVAULT_AUTH_TOKEN` and `XVAULT_CT0`.

## Query ID or 404 Failures

X rotates internal GraphQL IDs. Run:

```bash
xvault refresh-ids --json
```

If sync still fails, capture only redacted error output. Do not share cookies or raw payloads.

If the dotenv file is stale but browser cookies are available locally, try:

```bash
xvault auth import-browser --source firefox --force --json
xvault auth import-browser --source chrome --force --json
```

## Search Returns Nothing

Run:

```bash
xvault stats --json
xvault db rebuild-fts --json
```

Then retry search.
