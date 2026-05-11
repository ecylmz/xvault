# xvault

`xvault` is a Go single-binary personal X/Twitter archive tool. It stores normalized tweets, users, collection membership, raw GraphQL payloads, and a contentless SQLite FTS index locally.

The current implementation is an early v1 track focused on:

- cookie-based auth from environment, `~/.config/xvault/.env`, Firefox, or Chrome/Chromium profiles
- `sync likes` and `sync bookmarks` against X web GraphQL endpoints
- local SQLite storage and FTS search
- `show`, `stats`, `doctor`, `db`, `backup`, and export commands
- deterministic `--json` envelopes for agent usage
- `thread` and `conversation` commands that expand via X `TweetDetail` and then read from SQLite

## Security

X cookies are equivalent to account session secrets. Keep `~/.config/xvault/.env` private:

```bash
mkdir -p ~/.config/xvault
chmod 700 ~/.config/xvault
chmod 600 ~/.config/xvault/.env
```

Supported dotenv keys:

```dotenv
XVAULT_AUTH_TOKEN="..."
XVAULT_CT0="..."
XVAULT_TWID="..."
```

TweetHoarder-compatible `TWITTER_AUTH_TOKEN`, `TWITTER_CT0`, and `TWITTER_TWID` aliases are also accepted.

Browser extraction is best-effort. Firefox cookies can be read from the profile database. Chrome/Chromium plaintext cookies are supported, and macOS encrypted `v10` cookies are decrypted through the local Safe Storage Keychain item when macOS permits access.

## Build

```bash
go test ./...
go build -o bin/xvault ./cmd/xvault
```

## First Run

```bash
bin/xvault init
bin/xvault auth status --json
bin/xvault --auth-source firefox auth test --json
bin/xvault doctor --json
bin/xvault sync bookmarks --count 100 --max-pages 2 --json
bin/xvault sync likes --count 100 --max-pages 2 --json
bin/xvault search "llm agents" --source all --limit 10 --json
```

## Main Commands

```bash
xvault version --json
xvault status --json
xvault doctor --json
xvault --auth-source chrome auth test --json
xvault sync likes --count 100 --max-pages 2 --json
xvault sync bookmarks --count 100 --max-pages 2 --json
xvault sync runs --limit 10 --json
xvault sync checkpoints --json
xvault search "query" --source bookmarks --limit 10 --json
xvault show TWEET_ID --json
xvault export json --collection all --output archive.json --json
xvault export json --collection bookmarks --folder Research --output research-bookmarks.json --json
xvault export markdown --collection all --output exports/markdown --json
xvault export csv --collection all --output archive.csv --json
xvault export html --collection all --output archive.html --json
xvault db integrity --json
xvault db rebuild-fts --json
xvault thread TWEET_ID --json
xvault conversation TWEET_ID --json
```

See `docs/PUBLISHING.md` for GitHub release and container verification steps.

## Limitations

X internal GraphQL operation IDs and response layouts change over time. `xvault` ships static fallback IDs and a parser for bundle-discovered query IDs, but real sync may require refreshing operation IDs when X changes web traffic.

Current X web builds expose bookmarks through `BookmarkSearchTimeline`; `xvault` uses a broad bookmark search query and cursor pagination to archive bookmark records. Run `xvault refresh-ids --json` if X rotates operation IDs.

No command posts, likes, deletes, follows, or otherwise mutates X.
