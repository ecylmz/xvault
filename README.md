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

## Install

Download a release binary for your OS and CPU architecture from the
[latest GitHub release](https://github.com/ecylmz/xvault/releases/latest).

macOS Apple Silicon:

```bash
curl -LO https://github.com/ecylmz/xvault/releases/latest/download/xvault-darwin-arm64
curl -LO https://github.com/ecylmz/xvault/releases/latest/download/xvault-darwin-arm64.sha256
shasum -a 256 -c xvault-darwin-arm64.sha256
chmod +x xvault-darwin-arm64
xattr -d com.apple.quarantine xvault-darwin-arm64 2>/dev/null || true
sudo mv xvault-darwin-arm64 /usr/local/bin/xvault
xvault version --json
```

macOS Intel:

```bash
curl -LO https://github.com/ecylmz/xvault/releases/latest/download/xvault-darwin-amd64
curl -LO https://github.com/ecylmz/xvault/releases/latest/download/xvault-darwin-amd64.sha256
shasum -a 256 -c xvault-darwin-amd64.sha256
chmod +x xvault-darwin-amd64
xattr -d com.apple.quarantine xvault-darwin-amd64 2>/dev/null || true
sudo mv xvault-darwin-amd64 /usr/local/bin/xvault
xvault version --json
```

Linux x86_64:

```bash
curl -LO https://github.com/ecylmz/xvault/releases/latest/download/xvault-linux-amd64
curl -LO https://github.com/ecylmz/xvault/releases/latest/download/xvault-linux-amd64.sha256
sha256sum -c xvault-linux-amd64.sha256
chmod +x xvault-linux-amd64
sudo mv xvault-linux-amd64 /usr/local/bin/xvault
xvault version --json
```

Linux arm64:

```bash
curl -LO https://github.com/ecylmz/xvault/releases/latest/download/xvault-linux-arm64
curl -LO https://github.com/ecylmz/xvault/releases/latest/download/xvault-linux-arm64.sha256
sha256sum -c xvault-linux-arm64.sha256
chmod +x xvault-linux-arm64
sudo mv xvault-linux-arm64 /usr/local/bin/xvault
xvault version --json
```

If `/usr/local/bin` is not writable or not on your `PATH`, install to another
directory already on `PATH`, such as `~/.local/bin`.

From a local checkout:

```bash
go install ./cmd/xvault
```

Or build a pinned local binary:

```bash
go build -trimpath -o bin/xvault ./cmd/xvault
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

## Local Data

By default, `xvault` stores its SQLite archive at:

```text
~/.local/share/xvault/xvault.sqlite
```

The default config file is `~/.config/xvault/config.toml`, and the default dotenv auth file is `~/.config/xvault/.env`. Query and maintain the archive through `xvault` commands such as `search`, `show`, `count`, `stats`, `db integrity`, and `db rebuild-fts`; do not edit the SQLite database directly unless you are developing a migration.

## Full Likes And Bookmarks Archive

After `auth test` succeeds, sync the complete local archive for the two primary saved collections:

```bash
xvault sync bookmarks --all --json
xvault sync likes --all --json
xvault verify-archive --json
```

Query the SQLite archive through `xvault`:

```bash
xvault count bookmarks --json
xvault count likes --json
xvault search --recent --source bookmarks --limit 10 --json
xvault search --recent --source likes --limit 10 --json
xvault search "retrieval" --source all --limit 10 --json
```

## Main Commands

```bash
xvault version --json
xvault status --json
xvault doctor --json
xvault --auth-source chrome auth test --json
xvault sync likes --count 100 --max-pages 2 --json
xvault sync bookmarks --count 100 --max-pages 2 --json
xvault sync feed --hours 24 --count 100 --max-pages 2 --json
xvault sync runs --limit 10 --json
xvault sync summary --json
xvault sync checkpoints --json
xvault bookmarks folders --json
xvault search "query" --source bookmarks --limit 10 --json
xvault search --recent --source bookmarks --limit 10 --json
xvault count bookmarks --json
xvault count likes --json
xvault verify-archive --json
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

## Releases

Release notes live in `CHANGELOG.md`. To publish a version, add a matching changelog section and run:

```bash
make release VERSION=v0.1.0
```

The release workflow builds static binaries for Linux and macOS on `amd64` and `arm64`, publishes `.sha256` checksum files, and uses the matching `CHANGELOG.md` section as the GitHub release notes.

## Limitations

X internal GraphQL operation IDs and response layouts change over time. `xvault` ships static fallback IDs and a parser for bundle-discovered query IDs, but real sync may require refreshing operation IDs when X changes web traffic.

Current X web builds expose bookmarks through `BookmarkSearchTimeline`; `xvault` uses a broad bookmark search query and cursor pagination to archive bookmark records. Run `xvault refresh-ids --json` if X rotates operation IDs.

No command posts, likes, deletes, follows, or otherwise mutates X.
