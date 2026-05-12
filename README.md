# xvault

`xvault` is a local, read-only CLI tool for archiving your own X/Twitter data.

It syncs likes, bookmarks, tweets, posts, replies, reposts, feed items,
threads, and conversations into a local SQLite database. You can then search
the archive and export it as JSON, Markdown, CSV, HTML, Hermes, or
Obsidian-friendly files.

No paid X API key is required. `xvault` uses your own authenticated web session
cookies and does not post, like, delete, follow, unfollow, or otherwise modify
your X account.

> [!WARNING]
> `xvault` uses your own X/Twitter session cookies. Treat `auth_token` and
> `ct0` like passwords. Do not paste them into chat tools, issue trackers,
> logs, screenshots, or commits. `xvault` is read-only with respect to X, but
> leaked cookies may allow account access. See
> [`docs/SECURITY.md`](docs/SECURITY.md).

## Why xvault?

- Archive personal X/Twitter data locally without a paid X API key.
- Keep normalized data, raw payloads, and search indexes in SQLite.
- Search, inspect, and export the archive with deterministic JSON output.
- Support agent and Hermes-style workflows without exposing secrets.
- Run as a single Go binary on macOS, Linux, local machines, or servers.

## Features

| Area | Supported |
|---|---|
| Authentication | dotenv, env vars, config, browser cookies |
| Sync | likes, bookmarks, tweets, posts, replies, reposts, feed |
| Storage | local SQLite, migrations, WAL, compressed raw payloads, FTS5 |
| Search | full-text search, collection filters, recent results, author/date/media/link filters |
| Export | JSON, Markdown, CSV, HTML, Hermes, Obsidian |
| Maintenance | integrity checks, FTS rebuilds, backups, vacuum, query-id refresh |
| Safety | read-only X behavior, redacted diagnostics, JSON error envelopes |

## Status

`xvault` is an early `v0.1.x` release. It is usable for local personal
archiving, but X/Twitter internal web GraphQL endpoints may change and break
sync behavior. Run `xvault refresh-ids --json` when query IDs rotate.

## Install

Download a release binary for your OS and CPU architecture from the
[latest GitHub release](https://github.com/ecylmz/xvault/releases/latest).

| Platform | Binary |
|---|---|
| macOS Apple Silicon | `xvault-darwin-arm64` |
| macOS Intel | `xvault-darwin-amd64` |
| Linux x86_64 | `xvault-linux-amd64` |
| Linux arm64 | `xvault-linux-arm64` |

Example for macOS Apple Silicon:

```bash
base=https://github.com/ecylmz/xvault/releases/latest/download
curl -LO "$base/xvault-darwin-arm64"
curl -LO "$base/xvault-darwin-arm64.sha256"
shasum -a 256 -c xvault-darwin-arm64.sha256
chmod +x xvault-darwin-arm64
xattr -d com.apple.quarantine xvault-darwin-arm64 2>/dev/null || true
sudo mv xvault-darwin-arm64 /usr/local/bin/xvault
xvault version --json
```

Use the matching binary name from the table for other platforms. See
[`docs/INSTALL.md`](docs/INSTALL.md) for source builds, Docker usage, and
authentication setup details.

## Quick Start

Initialize the local configuration:

```bash
xvault init
```

Create a private dotenv file for your X/Twitter session cookies:

```bash
mkdir -p ~/.config/xvault
chmod 700 ~/.config/xvault

cat > ~/.config/xvault/.env <<'EOF'
XVAULT_AUTH_TOKEN="..."
XVAULT_CT0="..."
XVAULT_TWID="..."
EOF

chmod 600 ~/.config/xvault/.env
```

Test authentication:

```bash
xvault auth status --json
xvault auth test --json
```

Run a bounded first sync:

```bash
xvault sync bookmarks --count 100 --max-pages 2 --json
xvault sync likes --count 100 --max-pages 2 --json
```

Search locally:

```bash
xvault search "llm agents" --source all --limit 10 --json
```

## Common Commands

### Health and diagnostics

```bash
xvault status --json
xvault auth sources --json
xvault auth status --json
xvault auth test --json
xvault doctor --json
xvault doctor --online --strict --json
xvault stats --json
```

### Sync

```bash
xvault sync bookmarks --count 100 --max-pages 2 --json
xvault sync likes --count 100 --max-pages 2 --json
xvault sync bookmarks --all --json
xvault sync likes --all --json
xvault sync tweets --count 100 --max-pages 2 --json
xvault sync posts --count 100 --max-pages 2 --json
xvault sync replies --count 100 --max-pages 2 --json
xvault sync reposts --count 100 --max-pages 2 --json
xvault sync feed --hours 24 --count 100 --max-pages 2 --json
xvault sync runs --limit 10 --json
xvault sync checkpoints --json
xvault sync summary --json
```

### Search and inspect

```bash
xvault search "query" --source all --limit 10 --json
xvault search --recent --source bookmarks --limit 10 --json
xvault count bookmarks --json
xvault count likes --json
xvault show TWEET_ID --json
xvault show-url https://x.com/USER/status/TWEET_ID --json
xvault thread TWEET_ID --json
xvault conversation TWEET_ID --json
```

### Export

```bash
xvault export json --collection all --output archive.json --json
xvault export markdown --collection all --output exports/markdown --json
xvault export csv --collection all --output archive.csv --json
xvault export html --collection all --output archive.html --json
xvault export hermes --output exports/hermes --json
xvault export obsidian --output exports/obsidian --json
```

### Database maintenance

```bash
xvault verify-archive --json
xvault db integrity --json
xvault db rebuild-fts --json
xvault vacuum --json
xvault refresh-ids --json
```

### Backup

```bash
xvault backup create --json
xvault backup list --json
xvault backup verify PATH --json
```

## Local Data and Privacy

Default local paths:

| Data | Path |
|---|---|
| Config | `~/.config/xvault/config.toml` |
| Auth dotenv | `~/.config/xvault/.env` |
| SQLite database | `~/.local/share/xvault/xvault.sqlite` |
| Exports | `~/.local/share/xvault/exports` |
| Backups | `~/.local/state/xvault/backups` |

The SQLite database may contain personal tweet data, raw GraphQL payloads,
collection membership, user metadata, and search indexes. Export files may
contain private bookmarks, likes, conversations, media URLs, links, and user
metadata.

Do not share local databases, exports, raw payloads, screenshots containing
secrets, or unredacted diagnostics in public issues, logs, chat tools, or
gists. Query and maintain the archive through `xvault` commands; do not edit
the SQLite database directly unless you are developing a migration.

## Exports

`xvault` can export the local archive into several formats:

- JSON: machine-readable archive data
- Markdown: note-oriented tweet exports
- CSV: spreadsheet-friendly tabular exports
- HTML: single-file offline archive viewer
- Hermes: Markdown plus `index.jsonl` for agent workflows
- Obsidian: vault-friendly notes and optional index files

See [`docs/EXPORTS.md`](docs/EXPORTS.md) for format-specific commands and
output layouts.

## Automation

`xvault` can print cron and systemd examples for scheduled sync jobs:

```bash
xvault service cron print
xvault service systemd print --user
```

Keep automation bounded with `--count` and `--max-pages`, and avoid aggressive
polling. See [`docs/OPERATIONS.md`](docs/OPERATIONS.md) for scheduling,
diagnostics, and operational workflows.

## Troubleshooting

- Auth missing: run `xvault auth status --json` and check
  `~/.config/xvault/.env`.
- Auth expired: refresh `XVAULT_AUTH_TOKEN`, `XVAULT_CT0`, and optionally
  `XVAULT_TWID`, then run `xvault auth test --json`.
- Query ID or endpoint failures: run `xvault refresh-ids --json`.
- Search returns nothing: run `xvault stats --json` and
  `xvault db rebuild-fts --json`.
- Database concerns: run `xvault db integrity --json`.

See [`docs/TROUBLESHOOTING.md`](docs/TROUBLESHOOTING.md) for more detail.

## Documentation

| Document | Purpose |
|---|---|
| [`docs/INSTALL.md`](docs/INSTALL.md) | Installation, Docker, source builds, and auth setup |
| [`docs/CONFIG.md`](docs/CONFIG.md) | Configuration files, paths, and defaults |
| [`docs/SECURITY.md`](docs/SECURITY.md) | Cookie handling, redaction, and agent isolation |
| [`docs/OPERATIONS.md`](docs/OPERATIONS.md) | Automation, diagnostics, and operational workflows |
| [`docs/EXPORTS.md`](docs/EXPORTS.md) | Export formats and output layouts |
| [`docs/SCHEMA.md`](docs/SCHEMA.md) | SQLite schema and FTS notes |
| [`docs/TROUBLESHOOTING.md`](docs/TROUBLESHOOTING.md) | Common problems and fixes |
| [`docs/SKILL.md`](docs/SKILL.md) | Agent/Hermes usage contract |
| [`docs/PUBLISHING.md`](docs/PUBLISHING.md) | Release and publishing workflow |

## Limitations

X/Twitter internal GraphQL operation IDs and response layouts change over time.
`xvault` ships static fallback IDs and a parser for bundle-discovered query
IDs, but real sync may require refreshing operation IDs when X changes web
traffic.

Browser cookie extraction is best-effort and depends on local browser profile
layout and OS permissions. The primary auth path is the private dotenv file at
`~/.config/xvault/.env`.

## Development

The module declares Go `1.25.0` in `go.mod`; CI currently tests with Go
`1.26.x` on Linux and macOS.

```bash
go test ./...
go build -o bin/xvault ./cmd/xvault
```

Common make targets:

```bash
make test
make lint
make build
make publish-check
```

Release notes live in `CHANGELOG.md`. Create releases with:

```bash
make release VERSION=v0.1.0
```

## License

License information has not been added yet.
