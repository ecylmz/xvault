# Troubleshooting

## Auth Missing

Check:

```bash
xvault auth status --json
```

Then verify `~/.config/xvault/.env` contains `XVAULT_AUTH_TOKEN` and `XVAULT_CT0`.
If `auth status --json` reports `"valid_shape": false`, the values are missing, placeholders, or malformed even if the redacted cookie presence fields say `"present"`.
`auth test --json` and `auth import-env --json` report `AUTH_MALFORMED` for this state without sending malformed cookies to X or writing them to the dotenv file.

## Auth Expired

`AUTH_EXPIRED` means cookies were found, but X rejected them. Restore fresh `XVAULT_AUTH_TOKEN`, `XVAULT_CT0`, and `XVAULT_TWID` values in `~/.config/xvault/.env`, then run:

```bash
xvault auth status --json
xvault auth test --json
```

If browser cookies are available locally, import them without printing secret values:

```bash
xvault auth import-browser --source firefox --force --json
xvault auth import-browser --source chrome --force --json
```

Do not commit, paste, screenshot, or send cookie values to an agent or issue tracker.

## Query ID or 404 Failures

X rotates internal GraphQL IDs. Run:

```bash
xvault refresh-ids --json
```

If sync still fails, capture only redacted error output. Do not share cookies or raw payloads.

## Search Returns Nothing

Run:

```bash
xvault stats --json
xvault db rebuild-fts --json
```

Then retry search.
