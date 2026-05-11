# xvault SPEC.md

> SPEC version: v3
>
> Working name: `xvault`
>
> Purpose: A Go-based, single-binary personal X/Twitter archival tool that can sync, search, inspect, and export a user's own X data locally. It is designed for both human CLI usage and agent usage through tools such as Hermes.
>
> Scope target: feature parity with TweetHoarder, implemented in Go, with additional emphasis on agent-safe command contracts, SQLite/FTS search, deterministic JSON output, backups, and operational isolation.

## v3 Revision Notes


This version additionally incorporates a diff-focused v2 review and closes the remaining consistency issues:

- The architecture diagram now names Cobra only.
- macOS browser cookie extraction implementation files are represented in the repository layout.
- Thread IDs no longer include expansion limits; limit is stored as metadata to avoid duplicate thread records for the same focal tweet and mode.
- Obsidian export layout now includes all collection directories and index notes consistently.
- `--include-raw` and `--with-index-jsonl` are included in CLI summaries.
- Agent safe-mode enforcement for raw output is explicitly defined.

This version incorporates the following corrections and scope clarifications:

- macOS is a v1.0 target, not a future platform.
- Cookie-based auth through `auth_token` and `ct0` in `~/.config/xvault/.env` is the primary intended auth path.
- Cobra is the required CLI framework.
- SQLite `collections` primary key no longer uses invalid `COALESCE(...)` syntax.
- Raw payload storage is canonicalized as compressed SQLite BLOBs for v1.0.
- Contentless FTS5 is retained, but search previews must be generated from `tweets.text`, not `snippet()`/`highlight()`.
- `--max-pages` is now a documented sync flag.
- `export obsidian` is defined.
- `posts` collection membership semantics are made explicit.
- `UserArticlesTweets` is removed from v1.0 and left for a future extension.
- `db rebuild-fts` is required.
- Dockerfile and container deployment examples are required.

---

## 1. Executive Summary

`xvault` archives a user's X/Twitter data locally without requiring a paid X API key. It uses the authenticated web session model used by the X web application: session cookies are supplied by the user through environment variables or a dotenv file, or extracted from local browser profiles where feasible. The primary supported authentication method is cookie-based auth using `auth_token` and `ct0`; the default dotenv path is `~/.config/xvault/.env`. The tool must run on Linux and macOS. It accesses X's internal GraphQL endpoints read-only, paginates through supported timelines, normalizes tweets into a local SQLite database, preserves raw responses for future reparsing, supports resumable sync, and exports the archive to JSON, Markdown, CSV, and a self-contained searchable HTML viewer.

The binary must support at least the following collection types:

- likes
- bookmarks
- bookmark folders
- own tweets
- reposts/retweets
- replies
- posts, meaning own tweets + reposts in one combined operation where the X endpoint supports it
- home/following feed
- thread expansion
- conversation expansion
- quoted tweet capture
- tombstone records for unavailable/deleted/suspended content

The project must be built as a single Go binary and must include a comprehensive Go test suite.

---

## 2. Design Goals

### 2.1 Primary Goals

1. Provide a reliable personal archive of saved X/Twitter information.
2. Avoid dependence on unstable abandoned third-party CLI tools.
3. Use a single Go binary for simple deployment on a VPS, MacBook, Mac mini, or local workstation.
4. Store all data locally in SQLite.
5. Preserve enough raw API data to survive future parser changes.
6. Support safe agent workflows through JSON CLI output.
7. Support Hermes-style skill usage:
   - sync
   - search
   - show
   - export
   - stats
   - doctor
8. Never require a paid X API key for the primary workflow.
9. Treat cookie-based auth through `~/.config/xvault/.env` as a first-class supported workflow.
10. Support Linux and macOS from v1.0.
11. Keep the tool read-only with respect to X.
12. Make sync idempotent and resumable.

### 2.2 Secondary Goals

1. Allow optional systemd/cron automation.
2. Support full-text search with SQLite FTS5.
3. Export Obsidian/Hermes-friendly Markdown.
4. Produce a single-file offline HTML archive viewer.
5. Support query-id refresh when X rotates GraphQL operation IDs.
6. Provide deterministic output suitable for tests and agent parsing.
7. Provide strong diagnostics without leaking secrets.

### 2.3 Non-Goals

The tool must not:

- post tweets
- like tweets
- unlike tweets
- delete tweets
- bookmark or unbookmark tweets
- follow/unfollow users
- send direct messages
- bypass account protections
- automate engagement
- scrape arbitrary users at scale
- act as a general Twitter client
- run aggressive polling loops by default
- expose auth cookies or tokens in logs, JSON output, or diagnostics

The project is a personal archival tool, not a social automation tool.

---

## 3. Feature Parity Matrix

| Capability | Required | Notes |
|---|---:|---|
| Cookie-based auth | Yes | Env, dotenv file, config, and browser extraction |
| No paid API key | Yes | Primary mode |
| Linux support | Yes | amd64 and arm64 |
| macOS support | Yes | amd64 and arm64; dotenv auth required, Keychain browser extraction best-effort |
| Dockerfile | Yes | Optional deployment artifact, not required at runtime |
| Likes sync | Yes | Incremental + full |
| Bookmarks sync | Yes | Incremental + full |
| Bookmark folders | Yes | Folder metadata and folder-filtered export |
| Own tweets sync | Yes | UserTweets endpoint |
| Reposts sync | Yes | Retweets/reposts collection |
| Replies sync | Yes | UserTweetsAndReplies with reply classification |
| Posts sync | Yes | Own tweets + reposts in one command where feasible |
| Home/following feed sync | Yes | Time-bounded by default |
| Thread expansion | Yes | Same-author chain |
| Conversation expansion | Yes | Multi-author discussion |
| Quoted tweets | Yes | Store as first-class tweet records |
| Tombstones | Yes | Deleted/unavailable/suspended placeholders |
| Raw JSON storage | Yes | Default on |
| Checkpoint/resume | Yes | Cursor-level resume |
| Query ID refresh | Yes | Static fallback + JS bundle discovery |
| Rate limiting | Yes | Conservative defaults + adaptive backoff |
| JSON export | Yes | Machine-readable |
| Markdown export | Yes | Hermes/Obsidian-friendly |
| CSV export | Yes | Spreadsheet-friendly |
| HTML export | Yes | Single-file viewer |
| Search | Yes | FTS5 + filters |
| Stats | Yes | Collection/folder/database stats |
| Config show/set | Yes | TOML config |
| Doctor command | Yes | Redacted diagnostics |
| Agent skill support | Yes | `--json` everywhere |
| Go tests | Yes | Unit, integration, fixture, golden, CLI |

---

## 4. High-Level Architecture

```text
             ┌────────────────────────┐
             │        User / Hermes    │
             └───────────┬────────────┘
                         │
                         ▼
             ┌────────────────────────┐
             │      xvault CLI         │
             │   Cobra subcommands      │
             └───────────┬────────────┘
                         │
        ┌────────────────┼─────────────────┐
        ▼                ▼                 ▼
┌──────────────┐  ┌──────────────┐  ┌──────────────┐
│ Auth Resolver│  │ Config Loader│  │ JSON Renderer│
└──────┬───────┘  └──────┬───────┘  └──────────────┘
       │                 │
       ▼                 ▼
┌────────────────────────────────────────────┐
│              X GraphQL Client              │
│ headers, query ids, feature flags, retries │
└───────────────────┬────────────────────────┘
                    │
                    ▼
┌────────────────────────────────────────────┐
│              Sync Orchestrator             │
│ pagination, checkpoint, dedupe, rate limit │
└───────────────────┬────────────────────────┘
                    │
                    ▼
┌────────────────────────────────────────────┐
│                SQLite Store                │
│ tweets, users, collections, raw, FTS, runs │
└───────────────────┬────────────────────────┘
                    │
        ┌───────────┼────────────┐
        ▼           ▼            ▼
┌────────────┐ ┌────────────┐ ┌────────────┐
│   Search   │ │   Export   │ │   Stats    │
└────────────┘ └────────────┘ └────────────┘
```

---

## 5. Repository Layout

```text
xvault/
├── cmd/
│   └── xvault/
│       └── main.go
├── internal/
│   ├── app/
│   │   ├── root.go
│   │   ├── output.go
│   │   └── errors.go
│   ├── auth/
│   │   ├── resolver.go
│   │   ├── env.go
│   │   ├── config.go
│   │   ├── firefox.go
│   │   ├── chrome_linux.go
│   │   ├── chrome_macos.go
│   │   ├── macos_keychain.go
│   │   └── redaction.go
│   ├── config/
│   │   ├── config.go
│   │   ├── paths.go
│   │   └── defaults.go
│   ├── client/
│   │   ├── client.go
│   │   ├── headers.go
│   │   ├── graphql.go
│   │   ├── operations.go
│   │   ├── feature_flags.go
│   │   ├── retry.go
│   │   └── errors.go
│   ├── queryids/
│   │   ├── store.go
│   │   ├── fallback.go
│   │   ├── scraper.go
│   │   └── parser.go
│   ├── parser/
│   │   ├── tweet.go
│   │   ├── timeline.go
│   │   ├── bookmark.go
│   │   ├── thread.go
│   │   ├── entities.go
│   │   └── tombstone.go
│   ├── model/
│   │   ├── tweet.go
│   │   ├── user.go
│   │   ├── media.go
│   │   ├── collection.go
│   │   ├── thread.go
│   │   └── run.go
│   ├── store/
│   │   ├── db.go
│   │   ├── migrations.go
│   │   ├── tweets.go
│   │   ├── collections.go
│   │   ├── checkpoints.go
│   │   ├── raw.go
│   │   ├── search.go
│   │   ├── backups.go
│   │   └── testutil.go
│   ├── syncer/
│   │   ├── syncer.go
│   │   ├── likes.go
│   │   ├── bookmarks.go
│   │   ├── tweets.go
│   │   ├── reposts.go
│   │   ├── replies.go
│   │   ├── posts.go
│   │   ├── feed.go
│   │   ├── threads.go
│   │   ├── conversations.go
│   │   ├── checkpoint.go
│   │   └── rate_limiter.go
│   ├── export/
│   │   ├── json.go
│   │   ├── markdown.go
│   │   ├── csv.go
│   │   ├── html.go
│   │   ├── html_template.go
│   │   └── frontmatter.go
│   ├── search/
│   │   ├── query.go
│   │   ├── filters.go
│   │   └── rank.go
│   ├── service/
│   │   ├── lock.go
│   │   ├── systemd.go
│   │   └── cron.go
│   └── testdata/
│       ├── fixtures/
│       ├── graphql/
│       ├── bundles/
│       └── golden/
├── migrations/
│   ├── 0001_initial.sql
│   ├── 0002_fts.sql
│   ├── 0003_threads.sql
│   └── 0004_runs_backups.sql
├── docs/
│   ├── SKILL.md
│   ├── INSTALL.md
│   ├── SECURITY.md
│   └── OPERATIONS.md
├── SPEC.md
├── README.md
├── go.mod
├── go.sum
├── Makefile
├── Dockerfile
├── docker-compose.example.yml
└── .github/
    └── workflows/
        └── ci.yml
```

---

## 6. Technology Decisions

| Area | Decision |
|---|---|
| Language | Go 1.24+ |
| Binary | Single static-ish binary where feasible |
| CLI | `spf13/cobra`; use Cobra consistently for all commands and tests |
| Config | TOML via `pelletier/go-toml/v2` |
| SQLite | Prefer `modernc.org/sqlite` for CGO-free builds; allow build tag for `mattn/go-sqlite3` if needed |
| HTTP | Standard `net/http` with custom retry middleware |
| HTML parsing | `goquery` or stdlib tokenizer |
| Logging | Structured JSON logs internally; human output only at CLI boundary |
| Tests | Native `go test` |
| Fixtures | JSON fixtures + HTTP replay server |
| CI | GitHub Actions: Linux and macOS; optional Windows compile check |
| Lint | `golangci-lint` |
| Formatting | `gofmt`, `go vet` |
| Release | GitHub Releases with checksums |

---

## 7. Command-Line Interface

All commands must support:

```bash
--json
--config PATH
--db PATH
--profile NAME
--quiet
--verbose
--no-color
```

Human-readable output is default when stdout is a terminal. JSON output is required for Hermes and scripts.

### 7.1 Global Commands

```bash
xvault version [--json]
xvault init [--json]
xvault status [--json]
xvault doctor [--json]
xvault stats [--json]
xvault config show [--json]
xvault config get KEY [--json]
xvault config set KEY VALUE [--json]
xvault refresh-ids [--json]
```

### 7.2 Sync Commands

```bash
xvault sync [--json]
xvault sync likes [--count N] [--max-pages N] [--all] [--full] [--with-threads] [--thread-mode thread|conversation] [--thread-limit N] [--json]
xvault sync bookmarks [--count N] [--max-pages N] [--all] [--full] [--folder NAME] [--with-threads] [--thread-mode thread|conversation] [--thread-limit N] [--json]
xvault sync tweets [--count N] [--max-pages N] [--all] [--full] [--with-threads] [--json]
xvault sync reposts [--count N] [--max-pages N] [--all] [--full] [--with-threads] [--json]
xvault sync replies [--count N] [--max-pages N] [--all] [--full] [--with-threads] [--json]
xvault sync posts [--count N] [--max-pages N] [--all] [--full] [--with-threads] [--json]
xvault sync feed [--hours N] [--count N] [--max-pages N] [--all] [--json]
```

Default `xvault sync` must sync:

- likes
- bookmarks
- tweets
- reposts
- replies

It must not sync feed by default because feed can be large and more volatile.

`--count` limits the number of tweets/items processed. `--max-pages` limits the number of paginated GraphQL pages fetched. When both are set, the earlier limit wins. Agent/wrapper workflows should prefer `--max-pages` because it bounds network calls even when pages contain unexpectedly many items.


### 7.3 Thread Commands

```bash
xvault thread TWEET_ID [--mode thread|conversation] [--limit N] [--json]
xvault conversation TWEET_ID [--limit N] [--json]
```

`xvault conversation` is an alias for:

```bash
xvault thread TWEET_ID --mode conversation
```

### 7.4 Search and Show Commands

```bash
xvault search "QUERY" [--source all|likes|bookmarks|tweets|reposts|replies|posts|feed|threads] [--limit N] [--offset N] [--author USERNAME] [--from YYYY-MM-DD] [--to YYYY-MM-DD] [--has-media] [--has-link] [--folder NAME] [--json]

xvault show TWEET_ID [--include-raw] [--json]
xvault show-url URL [--json]
xvault open TWEET_ID
```

`open` opens the canonical URL in a local browser where available. It must not be used automatically by Hermes unless the user explicitly requests it.

### 7.5 Export Commands

```bash
xvault export json [--collection TYPE] [--output PATH] [--folder NAME] [--pretty] [--json]
xvault export markdown [--collection TYPE] [--output PATH] [--folder NAME] [--mode single|files] [--json]
xvault export csv [--collection TYPE] [--output PATH] [--folder NAME] [--json]
xvault export html [--collection TYPE] [--output PATH] [--folder NAME] [--json]
xvault export hermes [--output PATH] [--json]
xvault export obsidian [--output PATH] [--with-index-jsonl] [--json]
```

### 7.6 Backup Commands

```bash
xvault backup create [--output PATH] [--json]
xvault backup list [--json]
xvault backup verify PATH [--json]
xvault vacuum [--json]
```

### 7.7 Service Commands

```bash
xvault service systemd print --user
xvault service cron print
```

These commands only print sample unit/timer or cron definitions. They must not install services unless an explicit install flag is later added.

---

## 8. JSON Output Contract

Every command with `--json` must return a single JSON object. No progress bars, color codes, warnings outside JSON, or mixed output may appear on stdout.

### 8.1 Success Envelope

```json
{
  "ok": true,
  "command": "sync bookmarks",
  "started_at": "2026-05-11T16:20:00Z",
  "finished_at": "2026-05-11T16:20:31Z",
  "duration_ms": 31000,
  "data": {}
}
```

### 8.2 Error Envelope

```json
{
  "ok": false,
  "command": "sync bookmarks",
  "error": {
    "code": "AUTH_EXPIRED",
    "message": "Authentication cookies appear to be expired. Refresh the X session cookies.",
    "retryable": false,
    "safe_for_agent": true
  }
}
```

### 8.3 Exit Codes

| Code | Meaning |
|---:|---|
| 0 | Success |
| 1 | Generic error |
| 2 | Invalid CLI arguments |
| 3 | Config error |
| 4 | Auth missing/expired |
| 5 | Network error |
| 6 | Rate limited |
| 7 | Query ID refresh failed |
| 8 | Database locked/corrupt |
| 9 | Partial success |
| 10 | User-aborted operation |

### 8.4 Sync JSON Example

```json
{
  "ok": true,
  "command": "sync bookmarks",
  "data": {
    "collection": "bookmarks",
    "mode": "incremental",
    "pages_fetched": 4,
    "tweets_seen": 176,
    "tweets_inserted": 42,
    "tweets_updated": 91,
    "tweets_unchanged": 43,
    "quoted_tweets_inserted": 8,
    "folders_seen": 3,
    "checkpoint_cleared": true,
    "rate_limit_events": 0,
    "db_path": "/home/xarchiver/.local/share/xvault/xvault.sqlite"
  }
}
```

### 8.5 Search JSON Example

```json
{
  "ok": true,
  "command": "search",
  "data": {
    "query": "defect prediction",
    "source": "all",
    "limit": 10,
    "offset": 0,
    "total_estimate": 3,
    "results": [
      {
        "tweet_id": "1234567890",
        "url": "https://x.com/example/status/1234567890",
        "author_username": "example",
        "author_display_name": "Example",
        "created_at": "2026-01-10T12:00:00Z",
        "collections": ["bookmark", "like"],
        "bookmark_folder_name": "Research",
        "score": 12.34,
        "text_preview": "A useful thread about defect prediction datasets...",
        "has_media": false,
        "has_links": true,
        "local_markdown_path": "/data/xvault/exports/markdown/bookmarks/2026/1234567890.md"
      }
    ]
  }
}
```

---

## 9. Configuration

### 9.1 XDG Locations

Default paths:

```text
Config:       ~/.config/xvault/config.toml
Dotenv auth:  ~/.config/xvault/.env
Query IDs:    ~/.config/xvault/query-ids-cache.json
Data:         ~/.local/share/xvault/xvault.sqlite
Raw cache:    not used in v1; raw payloads are stored in SQLite BLOBs by default
Exports:      ~/.local/share/xvault/exports/
Logs:         ~/.local/state/xvault/logs/
Backups:      ~/.local/state/xvault/backups/
Locks:        ~/.local/state/xvault/locks/
```

### 9.2 Config File

```toml
[auth]
# Priority order: env, dotenv, config, firefox, chrome, macos_keychain
sources = ["env", "dotenv", "config", "firefox", "chrome", "macos_keychain"]

dotenv_path = "~/.config/xvault/.env"

# Optional manual fallback. These must never be printed.
# auth_token = ""
# ct0 = ""
# twid = ""

[auth.firefox]
enabled = true
paths = [
  "~/.mozilla/firefox/*/cookies.sqlite",
  "~/snap/firefox/common/.mozilla/firefox/*/cookies.sqlite"
]

[auth.chrome]
enabled = true
paths = [
  "~/.config/google-chrome/*/Network/Cookies",
  "~/.config/chromium/*/Network/Cookies",
  "~/Library/Application Support/Google/Chrome/*/Cookies",
  "~/Library/Application Support/Google/Chrome/*/Network/Cookies",
  "~/Library/Application Support/Chromium/*/Cookies",
  "~/Library/Application Support/Chromium/*/Network/Cookies"
]

[auth.macos_keychain]
enabled = true
# Browser cookie extraction on macOS is best-effort because Chrome/Chromium cookie values
# may require Keychain access. Dotenv auth remains the primary macOS workflow.
# Implementation files: internal/auth/chrome_macos.go and internal/auth/macos_keychain.go.

[sync]
default_count = 100
default_like_count = -1
default_bookmark_count = -1
request_delay_ms = 750
max_retries = 5
stop_after_consecutive_rate_limits = 3
store_raw = true
checkpoint_every_items = 25
checkpoint_every_pages = 1
default_thread_mode = "thread"
default_thread_limit = 200
default_conversation_limit = 500
feed_default_hours = 24

[query_ids]
ttl_hours = 24
auto_refresh_on_404 = true
refresh_pages = [
  "https://x.com/?lang=en",
  "https://x.com/explore",
  "https://x.com/notifications",
  "https://x.com/settings/profile"
]

[database]
path = "~/.local/share/xvault/xvault.sqlite"
wal = true
busy_timeout_ms = 5000
foreign_keys = true
auto_vacuum = "incremental"
backup_before_migration = true

[export]
dir = "~/.local/share/xvault/exports"
markdown_layout = "collection/year"
overwrite = false
html_warn_size_mb = 40

[display]
progress = true
color = true
verbose = false

[agent]
safe_mode = true
json_default = false
allow_direct_db = false
allow_raw_output = false
```

---

## 10. Authentication

### 10.1 Cookie Resolution Priority

1. Environment variables:
   - `XVAULT_AUTH_TOKEN`
   - `XVAULT_CT0`
   - `XVAULT_TWID`
   - also accept TweetHoarder-compatible aliases:
     - `TWITTER_AUTH_TOKEN`
     - `TWITTER_CT0`
     - `TWITTER_TWID`
2. Dotenv file:
   - default path: `~/.config/xvault/.env`
   - keys:
     - `XVAULT_AUTH_TOKEN`
     - `XVAULT_CT0`
     - `XVAULT_TWID`
   - aliases are also accepted:
     - `TWITTER_AUTH_TOKEN`
     - `TWITTER_CT0`
     - `TWITTER_TWID`
3. Config file:
   - `[auth].auth_token`
   - `[auth].ct0`
   - `[auth].twid`
4. Firefox cookie database:
   - Linux normal profile
   - Linux snap Firefox profile
   - macOS Firefox profile where accessible
5. Chrome/Chromium cookie database:
   - Linux Secret Service/keyring support where available
   - macOS Keychain support where available
   - best-effort only; dotenv auth is still the reliable workflow
6. Explicit future extension:
   - `xvault auth import-cookies PATH`

### 10.2 Required Cookies

| Cookie | Required | Purpose |
|---|---:|---|
| `auth_token` | Yes | Session auth |
| `ct0` | Yes | CSRF token |
| `twid` | Preferred | User ID; useful for own profile operations |

### 10.3 Dotenv Auth File

The primary intended workflow for the user's own deployment is a dotenv file at:

```text
~/.config/xvault/.env
```

Example:

```dotenv
XVAULT_AUTH_TOKEN="..."
XVAULT_CT0="..."
XVAULT_TWID="..."
```

File permission guidance:

```bash
mkdir -p ~/.config/xvault
chmod 700 ~/.config/xvault
chmod 600 ~/.config/xvault/.env
```

The dotenv parser must:

- support quoted and unquoted values
- ignore blank lines and comments
- not expand shell commands
- not support command substitution
- reject multiline values
- never print values in diagnostics

### 10.4 Auth Commands

```bash
xvault auth status [--json]
xvault auth test [--json]
xvault auth sources [--json]
xvault auth import-env --json
```

The project must not implement an interactive login flow in v1. Users must authenticate through their browser and provide cookies.

### 10.5 Redaction Rules

The following strings must never appear in stdout, stderr, logs, snapshots, test fixtures, HTML exports, Markdown exports, or error messages:

- `auth_token`
- `ct0`
- bearer token value
- `Authorization` header
- `Cookie` header
- `x-csrf-token` value
- browser cookie database contents

Redaction function:

```go
func RedactSecret(s string) string {
    if len(s) <= 8 {
        return "[REDACTED]"
    }
    return s[:4] + "..." + s[len(s)-4:]
}
```

Diagnostics may show redacted presence, not values:

```json
{
  "auth_token": "present",
  "ct0": "present",
  "twid": "missing"
}
```

---

## 11. X GraphQL Client

### 11.1 Client Requirements

The client must:

- use authenticated cookie headers
- send `x-csrf-token` derived from `ct0`
- use a known web bearer token if required by X web endpoints
- support feature flags per operation
- support GraphQL variables per operation
- support GET/POST as required by the specific endpoint
- decode timeline instructions
- distinguish retryable and non-retryable errors
- detect expired authentication
- detect query ID errors
- trigger query ID refresh on 404/missing operation
- support request tracing without secrets

### 11.2 Operation Names

The following operation names must be supported, with static fallback query IDs shipped in the binary and runtime refresh support:

- `Bookmarks`
- `BookmarkFolderTimeline`
- `Likes`
- `TweetDetail`
- `SearchTimeline`
- `UserTweets`
- `UserTweetsAndReplies`
- `Following`
- `Followers`
- `HomeLatestTimeline`

Add operation names as necessary when actual X web traffic shows different names.

### 11.3 Feature Flags

Feature flags must be centralized in `internal/client/feature_flags.go`.

The implementation must avoid scattering arbitrary booleans across syncers. Each operation should declare:

```go
type OperationConfig struct {
    OperationName string
    QueryID       string
    Variables     map[string]any
    Features      map[string]bool
}
```

### 11.4 Request Headers

Minimum header builder:

```go
type AuthCookies struct {
    AuthToken string
    CT0       string
    TWID      string
}

func BuildHeaders(c AuthCookies) http.Header
```

Headers must include required web-client fields but must never be printed unredacted.

---

## 12. Query ID Management

X rotates internal GraphQL operation identifiers. `xvault` must not hard-code only one set of values.

### 12.1 Query ID Sources

Priority:

1. Runtime cache
2. Static fallback compiled into binary
3. Bundle scraping refresh
4. Fail with clear error

### 12.2 Cache File

```json
{
  "version": 1,
  "updated_at": "2026-05-11T12:00:00Z",
  "ttl_hours": 24,
  "operations": {
    "Bookmarks": {
      "query_id": "RV1g3b8n_SGOHwkqKYSCFw",
      "source": "bundle",
      "discovered_at": "2026-05-11T12:00:00Z"
    }
  }
}
```

### 12.3 Bundle Discovery

Discovery pages:

```text
https://x.com/?lang=en
https://x.com/explore
https://x.com/notifications
https://x.com/settings/profile
```

Bundle URL pattern:

```regex
https://abs\.twimg\.com/responsive-web/client-web(?:-legacy)?/[A-Za-z0-9._-]+\.js
```

Query ID validation:

```regex
^[A-Za-z0-9_-]{8,128}$
```

Operation parser must use multiple patterns because bundled JS changes format.

### 12.4 Refresh Behavior

On operation failure caused by stale query ID:

1. refresh query IDs
2. retry the failed request once
3. if it still fails, stop the current sync
4. persist checkpoint
5. report `QUERY_ID_REFRESH_FAILED`

---

## 13. Data Model

Use a normalized schema rather than one large denormalized table. Keep raw JSON separately to allow reparsing.

### 13.1 Core Tables

```sql
CREATE TABLE IF NOT EXISTS schema_migrations (
  version INTEGER PRIMARY KEY,
  applied_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS users (
  id TEXT PRIMARY KEY,
  username TEXT,
  display_name TEXT,
  avatar_url TEXT,
  verified INTEGER NOT NULL DEFAULT 0,
  protected INTEGER NOT NULL DEFAULT 0,
  raw_json_id TEXT,
  first_seen_at TEXT NOT NULL,
  last_seen_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS tweets (
  id TEXT PRIMARY KEY,
  text TEXT NOT NULL,
  lang TEXT,
  author_id TEXT NOT NULL,
  created_at TEXT,
  conversation_id TEXT,
  in_reply_to_tweet_id TEXT,
  in_reply_to_user_id TEXT,
  quoted_tweet_id TEXT,
  retweeted_tweet_id TEXT,
  is_quote INTEGER NOT NULL DEFAULT 0,
  is_retweet INTEGER NOT NULL DEFAULT 0,
  is_reply INTEGER NOT NULL DEFAULT 0,
  is_tombstone INTEGER NOT NULL DEFAULT 0,
  tombstone_reason TEXT,
  reply_count INTEGER NOT NULL DEFAULT 0,
  retweet_count INTEGER NOT NULL DEFAULT 0,
  like_count INTEGER NOT NULL DEFAULT 0,
  quote_count INTEGER NOT NULL DEFAULT 0,
  bookmark_count INTEGER,
  view_count INTEGER,
  raw_json_id TEXT,
  first_seen_at TEXT NOT NULL,
  last_seen_at TEXT NOT NULL,
  FOREIGN KEY(author_id) REFERENCES users(id),
  FOREIGN KEY(quoted_tweet_id) REFERENCES tweets(id),
  FOREIGN KEY(retweeted_tweet_id) REFERENCES tweets(id)
);

CREATE TABLE IF NOT EXISTS collections (
  tweet_id TEXT NOT NULL,
  collection_type TEXT NOT NULL,
  bookmark_folder_id TEXT,
  bookmark_folder_id_key TEXT NOT NULL DEFAULT '',
  bookmark_folder_name TEXT,
  added_at TEXT,
  synced_at TEXT NOT NULL,
  sort_index TEXT,
  source_run_id TEXT,
  thread_id TEXT,
  PRIMARY KEY(tweet_id, collection_type, bookmark_folder_id_key),
  FOREIGN KEY(tweet_id) REFERENCES tweets(id),
  CHECK(bookmark_folder_id_key = COALESCE(bookmark_folder_id, ''))
);

`bookmark_folder_id_key` must be set by store code to `COALESCE(bookmark_folder_id, '')`. SQLite does not permit expressions such as `COALESCE(...)` directly inside a `PRIMARY KEY`, so the normalized key column is required.

CREATE TABLE IF NOT EXISTS bookmark_folders (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL,
  sort_order INTEGER,
  first_seen_at TEXT NOT NULL,
  last_seen_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS media (
  id TEXT PRIMARY KEY,
  tweet_id TEXT NOT NULL,
  media_type TEXT NOT NULL,
  url TEXT,
  expanded_url TEXT,
  preview_url TEXT,
  local_path TEXT,
  width INTEGER,
  height INTEGER,
  duration_ms INTEGER,
  alt_text TEXT,
  raw_json_id TEXT,
  FOREIGN KEY(tweet_id) REFERENCES tweets(id)
);

CREATE TABLE IF NOT EXISTS urls (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  tweet_id TEXT NOT NULL,
  url TEXT NOT NULL,
  expanded_url TEXT,
  display_url TEXT,
  title TEXT,
  description TEXT,
  FOREIGN KEY(tweet_id) REFERENCES tweets(id)
);

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
```

### 13.2 Thread Tables

`threads.id` must be deterministic, not a random UUID. It must not include the requested expansion limit, because different limits for the same focal tweet and mode would otherwise create duplicate logical thread records. Use:

```text
{thread_type}:{focal_tweet_id}:{mode}
```

Examples:

```text
thread:1234567890:thread
conversation:1234567890:conversation
```

The requested limit is stored as metadata in `expansion_limit`. This makes repeated expansion idempotent for the same focal tweet/mode while still preserving the largest or latest expansion boundary.

```sql
CREATE TABLE IF NOT EXISTS threads (
  id TEXT PRIMARY KEY,
  conversation_id TEXT NOT NULL,
  root_tweet_id TEXT NOT NULL,
  focal_tweet_id TEXT NOT NULL,
  focal_tweet_id_key TEXT NOT NULL,
  author_id TEXT NOT NULL,
  thread_type TEXT NOT NULL,
  mode TEXT NOT NULL,
  expansion_limit INTEGER NOT NULL,
  tweet_count INTEGER NOT NULL,
  is_complete INTEGER NOT NULL DEFAULT 0,
  fetched_at TEXT NOT NULL,
  source_run_id TEXT,
  UNIQUE(thread_type, focal_tweet_id_key, mode),
  CHECK(focal_tweet_id_key = focal_tweet_id),
  FOREIGN KEY(root_tweet_id) REFERENCES tweets(id),
  FOREIGN KEY(focal_tweet_id) REFERENCES tweets(id),
  FOREIGN KEY(author_id) REFERENCES users(id)
);

CREATE TABLE IF NOT EXISTS thread_tweets (
  thread_id TEXT NOT NULL,
  tweet_id TEXT NOT NULL,
  depth INTEGER NOT NULL DEFAULT 0,
  position INTEGER NOT NULL DEFAULT 0,
  role TEXT NOT NULL DEFAULT 'member',
  PRIMARY KEY(thread_id, tweet_id),
  FOREIGN KEY(thread_id) REFERENCES threads(id),
  FOREIGN KEY(tweet_id) REFERENCES tweets(id)
);
```

Thread role values:

- `root`
- `focal`
- `author_reply`
- `direct_parent`
- `high_engagement_reply`
- `member`
- `tombstone`

### 13.3 Raw JSON Tables

Raw payload storage decision for v1.0:

- The canonical raw payload store is SQLite table `raw_payloads`.
- Payloads are gzip-compressed BLOBs.
- The filesystem raw cache directory is intentionally not used in v1.0 to avoid split-brain behavior between DB and disk.
- A future `raw.storage = "filesystem"` mode may be added later, but it is out of v1.0 scope.
- Backups must include raw payloads because they are inside the SQLite database.

```sql
CREATE TABLE IF NOT EXISTS raw_payloads (
  id TEXT PRIMARY KEY,
  kind TEXT NOT NULL,
  operation_name TEXT,
  sha256 TEXT NOT NULL,
  payload BLOB NOT NULL,
  compressed INTEGER NOT NULL DEFAULT 1,
  captured_at TEXT NOT NULL
);
```

Raw payloads should be gzip-compressed by default.

### 13.4 Sync Tables

```sql
CREATE TABLE IF NOT EXISTS sync_runs (
  id TEXT PRIMARY KEY,
  collection_type TEXT NOT NULL,
  mode TEXT NOT NULL,
  status TEXT NOT NULL,
  started_at TEXT NOT NULL,
  finished_at TEXT,
  pages_fetched INTEGER NOT NULL DEFAULT 0,
  tweets_seen INTEGER NOT NULL DEFAULT 0,
  tweets_inserted INTEGER NOT NULL DEFAULT 0,
  tweets_updated INTEGER NOT NULL DEFAULT 0,
  tweets_unchanged INTEGER NOT NULL DEFAULT 0,
  errors_count INTEGER NOT NULL DEFAULT 0,
  rate_limit_count INTEGER NOT NULL DEFAULT 0,
  error_code TEXT,
  error_message TEXT
);

CREATE TABLE IF NOT EXISTS sync_checkpoints (
  collection_type TEXT PRIMARY KEY,
  cursor TEXT,
  last_tweet_id TEXT,
  last_sort_index TEXT,
  source_run_id TEXT,
  total_seen INTEGER NOT NULL DEFAULT 0,
  updated_at TEXT NOT NULL,
  status TEXT NOT NULL
);
```

### 13.5 FTS5 Search

```sql
CREATE VIRTUAL TABLE IF NOT EXISTS tweets_fts USING fts5(
  text,
  author_username,
  author_display_name,
  content='',
  tokenize='unicode61'
);

CREATE TABLE IF NOT EXISTS tweets_fts_map (
  rowid INTEGER PRIMARY KEY,
  tweet_id TEXT NOT NULL UNIQUE,
  FOREIGN KEY(tweet_id) REFERENCES tweets(id)
);
```

Because `content=''` contentless FTS avoids fragile triggers, update FTS explicitly in store methods. Every tweet insert/update must call the FTS updater in the same transaction.

Important consequence:

- SQLite `snippet()` and `highlight()` must not be used as the source of search previews in v1.0.
- `text_preview` must be generated from the canonical `tweets.text` column after retrieving matched tweet IDs.
- A future version may switch to an external-content FTS table (`content='tweets'`) if snippet/highlight support becomes more important than operational simplicity.

A maintenance command is required:

```bash
xvault db rebuild-fts --json
```

It must delete and rebuild the FTS index from `tweets` and `users`. It must be tested with a corrupted or intentionally incomplete FTS index.

### 13.6 Indexes

```sql
CREATE INDEX IF NOT EXISTS idx_tweets_author ON tweets(author_id);
CREATE INDEX IF NOT EXISTS idx_tweets_created_at ON tweets(created_at);
CREATE INDEX IF NOT EXISTS idx_tweets_conversation ON tweets(conversation_id);
CREATE INDEX IF NOT EXISTS idx_tweets_quote ON tweets(quoted_tweet_id);
CREATE INDEX IF NOT EXISTS idx_collections_type ON collections(collection_type);
CREATE INDEX IF NOT EXISTS idx_collections_synced ON collections(synced_at);
CREATE INDEX IF NOT EXISTS idx_collections_folder ON collections(bookmark_folder_name);
CREATE INDEX IF NOT EXISTS idx_threads_conversation ON threads(conversation_id);
CREATE INDEX IF NOT EXISTS idx_thread_tweets_tweet ON thread_tweets(tweet_id);
```

---

## 14. Sync Semantics

### 14.1 Idempotency

A sync operation must be safe to run repeatedly.

Rules:

- `tweets.id` is primary key.
- Existing tweet metadata may be updated.
- `first_seen_at` must never change after initial insert.
- `last_seen_at` updates when the tweet is seen again.
- `collections` uses composite primary keys to avoid duplicate membership.
- A tweet may belong to multiple collections.
- Deleted/unavailable content must not remove existing local content.
- Local records must not be deleted because remote content disappeared.

### 14.2 Incremental Sync

Default sync is incremental.

Stop conditions:

- reached `--count`
- no next cursor
- old known tweet boundary reached
- rate limit stop threshold reached
- auth failure
- fatal parser error
- user interruption

### 14.3 Full Sync

`--full` ignores existing boundary checks and paginates until no cursor or limit.

`--all` means no item limit but still respects rate limiting and stop thresholds.

### 14.4 Checkpointing

Checkpoint must be written after every page and optionally after every N items.

Interrupted sync behavior:

- On next run, if checkpoint exists and `status = in_progress`, resume from cursor.
- If user passes `--full`, clear existing checkpoint for that collection before starting.
- If sync completes successfully, clear checkpoint.
- If sync partially succeeds, keep checkpoint and return exit code 9.

### 14.5 Collection Types

Internal collection type values:

| User-facing | DB value |
|---|---|
| likes | `like` |
| bookmarks | `bookmark` |
| tweets | `tweet` |
| reposts | `repost` |
| replies | `reply` |
| posts | `post` |
| feed | `feed` |
| thread | `thread` |
| conversation | `conversation` |

---

## 15. Collection-Specific Requirements

### 15.1 Likes

Command:

```bash
xvault sync likes
```

Requirements:

- Fetch liked tweets for authenticated user.
- Capture quoted tweets.
- Capture media, URLs, hashtags, mentions.
- Preserve remote order via `sort_index`.
- Store collection membership as `like`.
- Support `--count`, `--all`, `--full`, `--with-threads`.

### 15.2 Bookmarks

Command:

```bash
xvault sync bookmarks
```

Requirements:

- Fetch default bookmarks.
- Fetch all bookmark folders when available.
- Store folder ID/name in `bookmark_folders` and `collections`.
- If folder metadata is missing, store `bookmark_folder_name = NULL`.
- Support folder-specific export and search.
- Support `--folder NAME` for sync where endpoint allows targeted folder timeline.
- Support `--with-threads`.

### 15.3 Own Tweets

Command:

```bash
xvault sync tweets
```

Requirements:

- Fetch authenticated user's original tweets.
- Avoid classifying replies as original tweets unless endpoint includes them and parser identifies them.
- Store collection membership as `tweet`.

### 15.4 Reposts

Command:

```bash
xvault sync reposts
```

Requirements:

- Fetch retweets/reposts by authenticated user.
- Store the repost wrapper if available.
- Store the original tweet as a tweet record.
- Store collection membership as `repost`.
- Link via `retweeted_tweet_id`.

### 15.5 Replies

Command:

```bash
xvault sync replies
```

Requirements:

- Fetch authenticated user's replies.
- Store reply metadata:
  - `is_reply = 1`
  - `in_reply_to_tweet_id`
  - `in_reply_to_user_id`
  - `conversation_id`
- Store collection membership as `reply`.
- Where parent tweet is present, store it too.
- Where parent tweet is missing/deleted, create tombstone if referenced.

### 15.6 Posts

Command:

```bash
xvault sync posts
```

Requirements:

- Efficiently sync own tweets and reposts together where the endpoint supports it.
- `post` is a convenience collection representing items returned by the combined posts endpoint.
- If an item is an original tweet, store collection memberships `post` and `tweet`.
- If an item is a repost/retweet, store collection memberships `post` and `repost`, and link the original tweet via `retweeted_tweet_id`.
- Search and export must deduplicate by `tweet_id` by default while preserving all collection memberships in the output.
- Avoid duplicate records.

### 15.7 Feed

Command:

```bash
xvault sync feed --hours 24
```

Requirements:

- Fetch authenticated user's home/following timeline.
- Default to last 24 hours.
- Must not be included in default `xvault sync`.
- Store membership as `feed`.
- Use conservative pagination.
- Home/following timelines may be algorithmically mixed rather than strictly chronological. Therefore `created_at < now - hours` is a soft stop condition: stop only after either no cursor remains, `--count`/`--max-pages` is reached, or at least two consecutive pages contain no tweets newer than the boundary.

---

## 16. Thread and Conversation Expansion

### 16.1 Definitions

Thread:

- Same author as the root or focal chain author.
- Same `conversation_id`.
- Represents a self-reply chain.

Conversation:

- All tweets sharing the same `conversation_id`.
- Includes multiple participants.
- Superset of thread.

Focal tweet:

- The tweet for which expansion was requested.

Tombstone:

- Placeholder record for unavailable content.

### 16.2 Commands

```bash
xvault thread 1234567890
xvault thread 1234567890 --mode conversation --limit 200
xvault sync likes --with-threads --thread-mode thread
xvault sync bookmarks --with-threads --thread-mode conversation --thread-limit 200
```

### 16.3 Thread Mode

Thread mode must:

- fetch `TweetDetail`
- identify focal tweet
- identify conversation root
- identify original/root author
- keep only same-author tweets in the conversation
- include direct missing parents as tombstones where necessary
- store `threads.thread_type = 'thread'`

### 16.4 Conversation Mode

Conversation mode must:

- fetch `TweetDetail`
- collect all returned tweets sharing `conversation_id`
- paginate where the response provides cursors
- apply a hard limit
- prioritize tweets when conversation exceeds limit:
  1. original author tweets
  2. direct parents for context
  3. focal tweet
  4. high engagement replies
  5. chronological remainder

### 16.5 Limits

Defaults:

| Parameter | Default |
|---|---:|
| `--thread-limit` | 200 |
| `--conversation-limit` | 500 |
| max consecutive thread rate limits | 3 |

### 16.6 Deduplication

During `--with-threads`:

- Expand only newly inserted or newly collected tweets by default.
- Thread existence checks must use the logical key `(thread_type, focal_tweet_id, mode)`, not the raw `threads.id` string alone.
- Skip expansion when an existing thread record has `is_complete = 1` or `expansion_limit >= requested_limit`, unless `--refresh-threads` is provided.
- If a larger limit is requested later, reuse the same thread record, update `expansion_limit`, and refresh the `thread_tweets` membership for that thread inside a transaction.
- Store thread tweets as normal tweet records and add `thread_tweets` links.

---

## 17. Quoted Tweets

Quoted tweets must be first-class tweet records.

When a tweet quotes another tweet:

1. parse the outer tweet
2. parse the quoted tweet if present
3. insert/update quoted tweet in `tweets`
4. set `tweets.quoted_tweet_id` on the outer tweet
5. preserve quoted tweet raw JSON if available
6. render quoted tweet in Markdown and HTML exports

If quoted content is unavailable, create tombstone:

```text
[Tweet unavailable]
```

with `tombstone_reason = 'quoted_unavailable'`.

---

## 18. Tombstone Handling

Create tombstones for:

- deleted tweets
- unavailable tweets
- suspended account content
- protected account content
- missing parent tweets
- missing quoted tweets
- parser-recognized unavailable placeholders

Tombstone requirements:

- Must have stable tweet ID where known.
- Must not overwrite previously captured real content.
- If a previously tombstoned tweet later becomes available, replace tombstone fields with real content but keep `first_seen_at`.
- Must be searchable only when user requests tombstones or in diagnostics.

---

## 19. Search

### 19.1 Search Command

```bash
xvault search "query" --source all --limit 10 --json
```

### 19.2 Search Requirements

Search must support:

- FTS5 full-text search over tweet text and author fields
- source filter
- author username filter
- date range filter
- bookmark folder filter
- has-media filter
- has-link filter
- collection intersection display
- result ranking
- pagination
- deterministic order for equal scores

### 19.3 Search Ranking

Default rank:

1. FTS rank
2. collection priority:
   - bookmark > like > tweet > repost > reply > feed
3. recency
4. engagement
5. tweet ID deterministic tie-breaker

### 19.4 Search Result Format

Each result must include:

- tweet ID
- canonical URL
- text preview
- author username
- author display name
- created_at
- collections
- bookmark folder
- score
- media/link flags
- local Markdown path if exported

---

## 20. Show Command

Command:

```bash
xvault show 1234567890 --json
```

Must return:

- normalized tweet
- author
- metrics
- collections
- links
- media
- mentions
- hashtags
- quoted tweet
- reposted tweet
- thread metadata
- canonical URL
- raw JSON availability flag
- local export paths if available

Must not return raw JSON by default. Raw JSON requires:

```bash
xvault show 1234567890 --include-raw --json
```

`--include-raw` must be blocked in agent safe mode unless explicitly allowed. Enforcement is performed in the Cobra pre-run validation layer before command execution:

- If `[agent].safe_mode = true` and `[agent].allow_raw_output = false`, `show --include-raw` must fail with `RAW_OUTPUT_BLOCKED`.
- The command must not read raw payloads before this validation passes.
- The JSON error must be redacted and safe for agent display.
- Human users may disable the block explicitly through config or a future dedicated flag, but agent wrapper binaries should keep it enabled.

---

## 21. Export Formats

### 21.1 JSON Export

Command:

```bash
xvault export json --collection all --output archive.json --json
```

Requirements:

- stable schema version
- exported_at timestamp
- collection filter metadata
- tweet list
- users
- media
- URLs
- threads if included
- quoted tweets embedded and/or referenced
- deterministic ordering

Schema:

```json
{
  "schema_version": 1,
  "exported_at": "2026-05-11T12:00:00Z",
  "collection": "all",
  "count": 1000,
  "tweets": []
}
```

### 21.2 Markdown Export

Two modes:

```bash
xvault export markdown --mode single
xvault export markdown --mode files
```

Default for Hermes/Obsidian should be `files`.

Directory layout:

```text
exports/markdown/
├── bookmarks/
│   └── 2026/
│       └── 2026-05-11-1234567890.md
├── likes/
├── tweets/
├── reposts/
├── replies/
├── feed/
└── index.md
```

Each file:

```markdown
---
tweet_id: "1234567890"
url: "https://x.com/example/status/1234567890"
author_username: "example"
author_display_name: "Example User"
created_at: "2026-05-11T12:00:00Z"
collections:
  - bookmark
  - like
bookmark_folder: "Research"
has_media: false
has_links: true
quoted_tweet_id: "9876543210"
conversation_id: "1234567890"
---

Tweet text here.

## Links

- https://example.com

## Quoted Tweet

> Quoted tweet text...

## Source

https://x.com/example/status/1234567890
```

Markdown requirements:

- valid YAML front matter
- no raw HTML unless needed for media
- safe filenames
- deterministic output
- update only changed files when possible
- include quoted tweets
- include thread context when available

### 21.3 CSV Export

One row per tweet.

Columns:

- tweet_id
- url
- text
- author_username
- author_display_name
- created_at
- collections
- bookmark_folder
- reply_count
- repost_count
- like_count
- quote_count
- has_media
- has_links
- quoted_tweet_id
- conversation_id

### 21.4 HTML Export

Single self-contained file.

Features:

- light/dark theme
- virtual scrolling for 10,000+ tweets
- full-text search
- author filters
- collection type filters
- media filters
- date range filters
- bookmark folder filters
- quoted tweet rendering
- media preview
- clickable links
- copy as Markdown
- deduplication across collections
- localStorage for UI preferences
- no external JS/CSS/CDN dependencies

Large file warning:

- If estimated HTML size > `export.html_warn_size_mb`, warn in human mode.
- In JSON/non-interactive mode, continue unless `--fail-on-large` is provided.

### 21.5 Hermes Export

Command:

```bash
xvault export hermes --output /data/xvault/hermes --json
```

Requirements:

- Markdown files optimized for agent retrieval.
- `index.jsonl` containing one record per tweet.
- No secrets.
- Stable relative paths.
- Optional summary fields are allowed later but no LLM calls in v1.

`index.jsonl` example:

```json
{"tweet_id":"123","path":"bookmarks/2026/2026-05-11-123.md","text":"...","author":"example","collections":["bookmark"]}
```


### 21.6 Obsidian Export

Command:

```bash
xvault export obsidian --output ~/ObsidianVault/XVault --json
```

Obsidian export is similar to Hermes Markdown export but optimized for human note-taking and vault navigation.

Differences from `export hermes`:

- Creates one Markdown note per tweet.
- Adds YAML front matter compatible with Obsidian properties.
- Uses wiki-link-friendly filenames where possible.
- Creates collection index notes:
  - `Bookmarks.md`
  - `Likes.md`
  - `Tweets.md`
  - `Reposts.md`
  - `Replies.md`
  - `Feed.md`
- Creates author index notes under `Authors/`.
- Optionally creates hashtag index notes under `Tags/`.
- Does not include `index.jsonl` unless `--with-index-jsonl` is passed.

Default layout:

```text
XVault/
├── Bookmarks.md
├── Likes.md
├── Tweets.md
├── Reposts.md
├── Replies.md
├── Feed.md
├── Bookmarks/
├── Likes/
├── Tweets/
├── Reposts/
├── Replies/
├── Feed/
├── Authors/
└── Tags/
```

Each collection with exported notes must have both an index note and a matching directory. Empty collections may produce an index note stating that no records were exported, but should not create empty per-record directories unless `--create-empty-dirs` is introduced later.

Obsidian export must remain deterministic and must not require Obsidian-specific plugins.

---

## 22. HTML Viewer Design

The HTML viewer must be static and offline.

### 22.1 Layout

```text
┌──────────────────────────────────────────────────────────────┐
│ xvault archive                                  theme/search │
├───────────────┬──────────────────────────────────────────────┤
│ filters       │ tweet list                                   │
│ - source      │ ┌──────────────────────────────────────────┐ │
│ - authors     │ │ @author · date · collections             │ │
│ - folders     │ │ tweet text                               │ │
│ - media       │ │ quoted tweet card                        │ │
│ - dates       │ │ links/media/actions                      │ │
│               │ └──────────────────────────────────────────┘ │
└───────────────┴──────────────────────────────────────────────┘
```

### 22.2 Performance Requirements

- Render only visible tweet cards.
- Debounce search input at 150 ms.
- Precompute facets at export time.
- Avoid huge DOM nodes.
- Support at least 25,000 tweets in a modern browser.

### 22.3 Copy as Markdown

Copy format:

```markdown
> Tweet text
>
> — @author, 2026-05-11
> https://x.com/author/status/123
```

If quoted tweet exists:

```markdown
> Tweet text
>
> Quoted:
> > Quoted tweet text
>
> — @author
```

---

## 23. Stats

Command:

```bash
xvault stats --json
```

Human output should include:

- total unique tweets
- likes
- bookmarks
- bookmark folder breakdown
- own tweets
- reposts
- replies
- feed
- threads
- conversations
- tombstones
- quoted tweets
- database size
- raw payload size
- last sync per collection
- last successful sync per collection
- incomplete syncs
- incomplete threads

JSON output must contain the same data.

---

## 24. Doctor

Command:

```bash
xvault doctor --json
```

Doctor checks:

- config file readable
- data directory writable
- database opens
- migrations applied
- WAL mode enabled if configured
- auth source exists
- auth cookies present
- auth test succeeds
- query ID cache present/fresh
- static fallback query IDs available
- network access to X base domain
- export directory writable
- active lock status
- recent failed syncs
- database integrity check

Doctor must not:

- print cookies
- print environment variables
- dump headers
- dump browser cookie files
- run a long sync

---

## 25. Rate Limiting

### 25.1 Default Policy

- Base delay: 750 ms between GraphQL calls.
- On HTTP 429:
  - exponential backoff
  - jitter
  - persist checkpoint
  - stop after 3 consecutive rate limits by default
- On HTTP 403:
  - stop immediately
  - report possible auth/protection/risk state
- On network error:
  - retry up to `max_retries`
- On 404:
  - attempt query-id refresh once
- On parser error:
  - save raw payload
  - return partial error if possible

### 25.2 Adaptive Limiter

```go
type AdaptiveLimiter struct {
    MinDelay time.Duration
    MaxDelay time.Duration
    CurrentDelay time.Duration
    ConsecutiveSuccesses int
    ConsecutiveRateLimits int
}
```

Behavior:

- speed up after 5 successful requests
- slow down on 429
- stop when threshold reached
- log rate limit events without secrets

---

## 26. Error Handling

Error categories:

| Code | Behavior |
|---|---|
| `AUTH_MISSING` | Stop; tell user to configure cookies |
| `AUTH_EXPIRED` | Stop; tell user to refresh cookies |
| `RATE_LIMITED` | Stop after threshold; checkpoint retained |
| `QUERY_ID_STALE` | Refresh and retry |
| `QUERY_ID_REFRESH_FAILED` | Stop; checkpoint retained |
| `NETWORK_TEMPORARY` | Retry |
| `NETWORK_FATAL` | Stop |
| `PARSER_PARTIAL` | Store raw payload; continue if safe |
| `DB_LOCKED` | Stop; no destructive action |
| `DB_CORRUPT` | Stop; suggest restore/backup |
| `EXPORT_FAILED` | Leave temp files; report path |
| `INTERRUPTED` | Save checkpoint; exit partial |

---

## 27. Database Migrations

Requirements:

- Apply migrations at startup before commands needing DB.
- Backup DB before migration when configured.
- Store migration versions.
- Migrations must be idempotent.
- Migration tests must create old schemas and upgrade them.
- Migration failure must not destroy existing DB.

Command:

```bash
xvault db migrate --json
xvault db integrity --json
xvault db rebuild-fts --json
```

`db migrate`, `db integrity`, and `db rebuild-fts` are required in v1.0 because they are operational safety tools, not optional developer utilities.

---

## 28. Backups

### 28.1 Automatic Backup

Before schema migration:

```text
~/.local/state/xvault/backups/xvault-before-migration-YYYYMMDD-HHMMSS.sqlite
```

### 28.2 Manual Backup

```bash
xvault backup create --json
```

Must use SQLite online backup API where possible. Do not copy a live WAL DB naively unless first checkpointed safely.

### 28.3 Retention

Config:

```toml
[backup]
enabled = true
retention = 10
```

---

## 29. Locking and Concurrency

Only one sync/export modifying operation may run at a time.

Requirements:

- File lock under `~/.local/state/xvault/locks/xvault.lock`.
- Read-only commands may run concurrently if SQLite permits.
- Sync must use transaction per page, not one giant transaction.
- Search/show must not wait indefinitely.
- Lock JSON should include owner PID and started_at.

If locked:

```json
{
  "ok": false,
  "error": {
    "code": "LOCKED",
    "message": "Another xvault operation is running.",
    "retryable": true
  }
}
```

---

## 30. Security Model for Hermes

`xvault` must assume that an agent can misuse shell access if not isolated. Therefore the project must support a safe operational model, but it cannot guarantee security if the agent has root access.

Recommended deployment:

```text
xarchiver user:
  owns config, cookies, primary SQLite DB
  runs sync service

hermes user:
  cannot read xarchiver config/cookies
  can call selected xvault commands through wrapper
  can read exported Markdown/JSON
```

Better deployment:

```text
xvault-sync container:
  has cookies
  writes DB/export

hermes container:
  no cookies
  read-only mount of export directory
```

### 30.1 Agent Restrictions

Hermes SKILL must instruct:

- Use `xvault` CLI only.
- Prefer `search` before `sync`.
- Use `--json`.
- Never inspect config files, env, browser cookies, or DB directly.
- Never print secrets.
- Do not run repeated sync loops.
- Stop on rate limits.

### 30.2 Safe Wrapper

Optional wrapper:

```bash
/usr/local/bin/xvault-agent
```

Allowed commands:

- `status`
- `stats`
- `search`
- `show`
- `export hermes`
- bounded `sync bookmarks --max-pages N`
- bounded `sync likes --max-pages N`

The wrapper may reject:

- `auth`
- `show --include-raw`
- direct DB commands
- full unbounded syncs unless explicitly allowed

---

## 31. Hermes SKILL.md

A file should be generated at:

```text
docs/SKILL.md
```

Content summary:

```markdown
# xvault Skill

Use `xvault` to search and retrieve the user's local X/Twitter archive.

Rules:
- Always use `--json`.
- Search local data before syncing.
- Do not read cookies, config secrets, environment variables, shell history, browser profiles, or SQLite directly.
- Do not run aggressive syncs.
- Stop on rate limits.
- Report concise results with author, date, source collection, short summary, and URL.

Common commands:
- `xvault status --json`
- `xvault search "query" --source all --limit 10 --json`
- `xvault show TWEET_ID --json`
- `xvault sync bookmarks --count 100 --json`
- `xvault sync likes --count 100 --json`
- `xvault export hermes --json`
```

The final SKILL must be more detailed but follow this operational model.

---

## 32. Service and Automation

### 32.1 systemd User Timer Example

`xvault service systemd print --user` should output something like:

```ini
[Unit]
Description=xvault bookmark sync

[Service]
Type=oneshot
ExecStart=/usr/local/bin/xvault sync bookmarks --count 300 --json
```

Timer:

```ini
[Unit]
Description=Run xvault bookmark sync every 3 hours

[Timer]
OnBootSec=10min
OnUnitActiveSec=3h
Persistent=true

[Install]
WantedBy=timers.target
```

### 32.2 Cron Example

```cron
0 */3 * * * /usr/local/bin/xvault sync bookmarks --count 300 --json >> ~/.local/state/xvault/logs/bookmarks.log 2>&1
```

### 32.3 Automation Defaults

- Do not recommend less than hourly sync.
- Default suggested interval: every 3 to 6 hours.
- Feed sync should be manual or low-frequency.

---

## 33. Installation

### 33.1 Build from Source

```bash
git clone https://github.com/OWNER/xvault.git
cd xvault
go test ./...
go build -o xvault ./cmd/xvault
sudo install -m 0755 xvault /usr/local/bin/xvault
```

### 33.2 Release Binary

```bash
curl -L -o xvault https://github.com/OWNER/xvault/releases/latest/download/xvault-linux-arm64
chmod +x xvault
sudo mv xvault /usr/local/bin/
```

### 33.3 Docker

A Dockerfile must be provided for VPS/container deployments.

Requirements:

- multi-stage build
- non-root runtime user
- mounted config directory
- mounted data directory
- no secrets baked into image
- works on Linux amd64 and arm64

Example:

```bash
docker run --rm \
  -v $HOME/.config/xvault:/home/xvault/.config/xvault:ro \
  -v $HOME/.local/share/xvault:/home/xvault/.local/share/xvault \
  ghcr.io/OWNER/xvault:latest \
  xvault status --json
```

`docker-compose.example.yml` should include separate examples for:

- one-shot sync
- scheduled sync through host cron/systemd
- Hermes read-only export mount

### 33.4 First Run

```bash
xvault init
xvault auth status
xvault doctor
xvault sync bookmarks --count 100
xvault search "software defect prediction"
```

---

## 34. Testing Strategy

The project must include a strong Go test suite from the beginning.

### 34.1 Test Categories

| Category | Required | Purpose |
|---|---:|---|
| Unit tests | Yes | Fast logic tests |
| Parser fixture tests | Yes | GraphQL response parsing |
| Query ID parser tests | Yes | JS bundle parsing |
| Store tests | Yes | SQLite insert/update/search |
| Migration tests | Yes | Schema upgrade safety |
| CLI tests | Yes | Command behavior and JSON output |
| Golden export tests | Yes | Markdown/CSV/JSON/HTML output stability |
| HTTP replay tests | Yes | Simulate X API responses |
| Error tests | Yes | Rate limit/auth/query-id/parser errors |
| Race tests | Yes | Concurrency/locking where meaningful |
| Integration tests | Optional by default | Real X calls only manually |

### 34.2 Unit Tests

Required test files:

```text
internal/auth/resolver_test.go
internal/auth/dotenv_test.go
internal/auth/redaction_test.go
internal/auth/chrome_macos_test.go
internal/queryids/parser_test.go
internal/client/retry_test.go
internal/parser/tweet_test.go
internal/parser/timeline_test.go
internal/parser/thread_test.go
internal/store/tweets_test.go
internal/store/collections_test.go
internal/store/search_test.go
internal/store/migrations_test.go
internal/syncer/checkpoint_test.go
internal/syncer/rate_limiter_test.go
internal/export/markdown_test.go
internal/export/json_test.go
internal/export/csv_test.go
internal/export/html_test.go
internal/app/output_test.go
```

### 34.3 Parser Fixture Tests

Fixtures:

```text
internal/testdata/fixtures/likes_page_1.json
internal/testdata/fixtures/bookmarks_default.json
internal/testdata/fixtures/bookmark_folder.json
internal/testdata/fixtures/user_tweets.json
internal/testdata/fixtures/user_replies.json
internal/testdata/fixtures/reposts.json
internal/testdata/fixtures/home_timeline.json
internal/testdata/fixtures/tweet_detail_thread.json
internal/testdata/fixtures/tweet_detail_conversation.json
internal/testdata/fixtures/quote_tweet.json
internal/testdata/fixtures/tombstone.json
```

Each fixture test must assert:

- tweet count
- user count
- media parsing
- URL parsing
- quoted tweet linking
- tombstone handling
- cursor extraction
- sort index extraction

### 34.4 Query ID Tests

Use JS bundle fixture files:

```text
internal/testdata/bundles/bundle_format_1.js
internal/testdata/bundles/bundle_format_2.js
internal/testdata/bundles/bundle_minified.js
```

Test cases:

- extract operation/queryId pairs
- ignore invalid query IDs
- handle queryId before operationName
- handle operationName before queryId
- handle duplicate operation IDs
- prefer most recent discovered ID
- fail clearly when missing

### 34.5 Store Tests

Use temporary SQLite DB for each test.

Tests:

- insert tweet
- upsert tweet preserves `first_seen_at`
- upsert tweet updates `last_seen_at`
- insert same collection twice is idempotent
- tweet may belong to multiple collections
- bookmark folder metadata stored
- raw payload compressed and retrievable
- FTS updated on tweet insert
- FTS updated on tweet update
- tombstone does not overwrite real tweet
- real tweet replaces tombstone
- checkpoint save/load/clear
- sync run lifecycle
- migration idempotency
- DB integrity check

### 34.6 CLI Tests

Use `exec.Command` or direct Cobra command invocation.

Tests:

- `xvault version --json` valid JSON
- invalid args return exit code 2
- `status --json` has no progress text
- auth error returns redacted JSON
- `search --json` deterministic
- `sync` with fixture HTTP server succeeds
- `doctor --json` never leaks env values

### 34.7 Golden Export Tests

Golden files:

```text
internal/testdata/golden/export_likes.json
internal/testdata/golden/export_bookmarks.md
internal/testdata/golden/export_bookmarks.csv
internal/testdata/golden/export_all.html
```

Rules:

- normalize timestamps in tests
- deterministic ordering
- no local absolute paths except expected temp placeholders
- compare exact output
- provide `UPDATE_GOLDEN=1 go test ./...` workflow

### 34.8 HTTP Replay Tests

Do not use real X in CI.

Build a local `httptest.Server` that simulates:

- likes pagination
- bookmarks pagination
- bookmark folders
- user tweets
- replies
- reposts
- feed
- TweetDetail
- 429 then success
- 404 query ID stale
- auth expired
- malformed payload

The client must allow base URL override in tests:

```go
client := NewClient(ClientOptions{
    BaseURL: server.URL,
    Auth: fakeAuth,
})
```

### 34.9 Integration Tests

Real X calls are opt-in only:

```bash
XVAULT_RUN_INTEGRATION=1 go test ./internal/integration -v
```

Requirements:

- never run in CI by default
- require explicit env variables
- redact all secrets
- limit to small counts
- do not mutate remote account

### 34.10 Coverage Targets

Minimums:

| Area | Minimum |
|---|---:|
| Parser | 85% |
| Store | 85% |
| Query IDs | 90% |
| Export | 80% |
| Sync orchestration | 75% |
| Overall | 75% |

Coverage alone is not sufficient; fixture and golden tests are required.

---

## 35. Development Commands

Makefile:

```makefile
.PHONY: test
test:
	go test ./...

.PHONY: race
race:
	go test -race ./...

.PHONY: lint
lint:
	golangci-lint run

.PHONY: fmt
fmt:
	gofmt -w .

.PHONY: ci
ci: fmt test lint

.PHONY: build
build:
	go build -o bin/xvault ./cmd/xvault

.PHONY: update-golden
update-golden:
	UPDATE_GOLDEN=1 go test ./...
```

---

## 36. Implementation Phases

### Phase 1: Project Skeleton

Deliverables:

- Go module
- CLI skeleton
- config loader
- XDG path resolver
- JSON output envelope
- version command
- initial tests

Acceptance:

```bash
go test ./...
go build ./cmd/xvault
xvault version --json
```

### Phase 2: SQLite Store

Deliverables:

- migrations
- tweet/user/collection tables
- raw payload table
- FTS5 search
- store tests

Acceptance:

- insert/update/search works
- migrations idempotent
- FTS returns expected results

### Phase 3: Auth

Deliverables:

- env auth
- dotenv auth from `~/.config/xvault/.env`
- config auth
- Firefox extraction
- Chrome Linux best-effort extraction
- Chrome/Chromium macOS best-effort extraction through Keychain where feasible
- redaction
- auth status/test commands

Acceptance:

- no secret leakage in tests
- auth resolver priority tested

### Phase 4: Query IDs

Deliverables:

- static fallback IDs
- runtime cache
- JS bundle scraper
- query ID parser tests

Acceptance:

- parse fixture bundles
- refresh cache
- handle stale operation

### Phase 5: GraphQL Client

Deliverables:

- base client
- headers
- operation executor
- retry/backoff
- fake server tests

Acceptance:

- can call fixture server
- handles 429, 403, 404, network failures

### Phase 6: Parser

Deliverables:

- timeline parser
- tweet parser
- entities parser
- media/url/mention/hashtag parser
- tombstone parser
- quoted tweet parser

Acceptance:

- all fixture tests pass

### Phase 7: Sync Likes and Bookmarks

Deliverables:

- likes sync
- bookmarks sync
- bookmark folders
- checkpointing
- sync runs

Acceptance:

- fixture sync inserts expected records
- repeated sync is idempotent
- checkpoint resume works

### Phase 8: Tweets/Reposts/Replies/Posts/Feed

Deliverables:

- own tweets
- reposts
- replies
- posts
- feed with hours filter

Acceptance:

- fixture syncs work
- collection types correct
- feed stops by date boundary

### Phase 9: Threads and Conversations

Deliverables:

- TweetDetail parser
- thread expansion
- conversation expansion
- prioritization
- tombstones

Acceptance:

- thread fixture produces expected chain
- conversation limit/prioritization works
- `--with-threads` only expands new tweets

### Phase 10: Search/Show/Stats/Doctor

Deliverables:

- search command
- show command
- stats command
- doctor command

Acceptance:

- JSON stable and useful for Hermes
- no secrets

### Phase 11: Exports

Deliverables:

- JSON export
- Markdown export
- CSV export
- HTML export
- Hermes export
- Obsidian export

Acceptance:

- golden tests pass
- HTML is self-contained
- Markdown has valid front matter

### Phase 12: Operations and Release

Deliverables:

- systemd print
- cron print
- backup command
- installation docs
- security docs
- GitHub Actions
- release workflow

Acceptance:

- install from binary
- service examples generated
- checksums published

---

## 37. Acceptance Criteria for v1.0

`xvault` v1.0 is complete when:

1. Single Go binary builds on Linux amd64/arm64 and macOS amd64/arm64.
2. `go test ./...` passes.
3. Likes sync works against replay fixtures and real opt-in integration.
4. Bookmarks sync works, including bookmark folders.
5. Tweets/reposts/replies/posts/feed sync work.
6. Query ID refresh works with bundle fixtures.
7. Thread and conversation expansion work.
8. Quoted tweets are stored and exported.
9. Tombstones are handled safely.
10. SQLite schema supports all required collections.
11. `db migrate`, `db integrity`, and `db rebuild-fts` work and are tested.
12. FTS search works.
12. JSON output is available for all important commands.
13. Markdown/JSON/CSV/HTML exports work.
14. HTML viewer is offline and searchable.
15. Checkpoint/resume is tested.
16. Rate limit behavior is tested.
17. Secrets are redacted in all tested outputs.
18. Hermes SKILL.md exists.
19. README, INSTALL, SECURITY, OPERATIONS docs exist.
20. GitHub Actions runs tests and lint.

---

## 38. Open Technical Risks

### 38.1 X Web API Instability

Risk:

- operation names, query IDs, variables, and response structures can change.

Mitigation:

- static fallback query IDs
- bundle scraping
- raw payload preservation
- parser fixture expansion
- clear `doctor` output
- minimal client surface

### 38.2 Cookie Expiration

Risk:

- cookie auth will expire or trigger verification.

Mitigation:

- detect auth failure
- no repeated retries on auth failure
- clear user-facing refresh instructions
- support multiple auth sources

### 38.3 Chrome Cookie Decryption

Risk:

- Linux keyring/decryption is environment-specific.

Mitigation:

- env/config manual cookies are primary reliable mode
- Firefox extraction is simpler
- Chrome extraction best-effort
- clear doctor messages

### 38.4 Rate Limiting

Risk:

- aggressive sync can produce rate limits or account risk.

Mitigation:

- conservative defaults
- checkpointing
- adaptive limiter
- stop on repeated 429
- feed excluded from default sync

### 38.5 Agent Overreach

Risk:

- Hermes can read secrets if given full server access.

Mitigation:

- document separate user/container model
- safe wrapper
- SKILL restrictions
- do not rely on SKILL as security

---

## 39. Future Extensions After v1.0

These are not required for feature parity but are useful:

- X Articles / `UserArticlesTweets` archival
- X archive ZIP import
- media download with dedupe and size limits
- URL enrichment/title extraction
- RSS export
- MCP server mode
- read-only HTTP API on localhost
- embedding index for semantic search
- automatic topic tagging
- duplicate detection across links
- Obsidian backlinks
- remote backup to S3-compatible storage
- multi-account support
- encrypted secret storage
- Tailscale/private network UI

---

## 40. Coding Rules for Agents

When an AI coding agent implements this project:

1. Do not remove tests to make code pass.
2. Do not weaken redaction rules.
3. Do not print cookies in debug output.
4. Do not add write actions against X.
5. Do not introduce external services for core functionality.
6. Keep the binary self-contained.
7. Prefer small packages with clear interfaces.
8. Use fixtures for parser behavior.
9. Add tests before or with each feature.
10. Keep JSON schemas stable.
11. Keep CLI backwards-compatible once documented.
12. Never store secrets in testdata.
13. Never commit real GraphQL responses containing user data.
14. Use synthetic or redacted fixtures only.
15. Treat all archive data as private.

---

## 41. Minimal Interfaces

### 41.1 Auth

```go
type AuthResolver interface {
    Resolve(ctx context.Context) (AuthCookies, AuthSource, error)
}

type AuthCookies struct {
    AuthToken string
    CT0       string
    TWID      string
}
```

### 41.2 Client

```go
type TwitterClient interface {
    FetchTimeline(ctx context.Context, op Operation, cursor string) (*TimelinePage, error)
    FetchTweetDetail(ctx context.Context, tweetID string) (*TweetDetailResult, error)
}
```

### 41.3 Store

```go
type Store interface {
    UpsertTweet(ctx context.Context, tx Tx, tweet model.Tweet) error
    UpsertUser(ctx context.Context, tx Tx, user model.User) error
    AddToCollection(ctx context.Context, tx Tx, item model.CollectionItem) error
    SaveRawPayload(ctx context.Context, tx Tx, payload model.RawPayload) (string, error)
    SaveCheckpoint(ctx context.Context, checkpoint model.Checkpoint) error
    LoadCheckpoint(ctx context.Context, collection string) (*model.Checkpoint, error)
    ClearCheckpoint(ctx context.Context, collection string) error
    Search(ctx context.Context, query search.Query) ([]search.Result, error)
}
```

### 41.4 Syncer

```go
type Syncer interface {
    Sync(ctx context.Context, req SyncRequest) (*SyncResult, error)
}
```

---

## 42. Example Human Workflows

### 42.1 First Bookmark Sync

```bash
xvault init
xvault auth status
xvault doctor
xvault sync bookmarks --count 300
xvault search "llm agents"
```

### 42.2 Full Personal Archive

```bash
xvault sync likes --all
xvault sync bookmarks --all
xvault sync tweets --all
xvault sync reposts --all
xvault sync replies --all
xvault export html --collection all --output ~/x-archive.html
```

### 42.3 Hermes-Friendly Export

```bash
xvault sync bookmarks --count 300 --json
xvault export hermes --output /data/xvault/hermes --json
```

---

## 43. Example Hermes Workflows

### 43.1 User asks: "Bunu daha önce bookmarklamış mıydım?"

Hermes should run:

```bash
xvault search "USER_TOPIC" --source bookmarks --limit 10 --json
```

If archive is stale:

```bash
xvault status --json
xvault sync bookmarks --count 100 --json
xvault search "USER_TOPIC" --source bookmarks --limit 10 --json
```

### 43.2 User asks: "Son beğendiğim LLM agent tweetlerini bul"

```bash
xvault search "LLM agent" --source likes --limit 10 --json
```

### 43.3 User asks: "Arşivi Obsidian/Hermes için güncelle"

```bash
xvault export hermes --json
```

---

## 44. Documentation Requirements

Required docs:

```text
README.md
SPEC.md
docs/INSTALL.md
docs/SECURITY.md
docs/OPERATIONS.md
docs/SKILL.md
docs/CONFIG.md
docs/SCHEMA.md
docs/EXPORTS.md
docs/TROUBLESHOOTING.md
```

README must include:

- what the tool does
- security warning about cookies
- installation
- first run
- commands
- examples
- limitations

SECURITY must include:

- cookie risk
- Hermes isolation model
- file permissions
- container/user separation
- redaction guarantee
- responsible use statement

---

## 45. Definition of Done

A feature is done only when:

1. Code is implemented.
2. Unit tests exist.
3. Fixture or golden tests exist where applicable.
4. CLI help is updated.
5. JSON output is documented.
6. Errors are typed and redacted.
7. No secret leakage tests fail.
8. `go test ./...` passes.
9. `golangci-lint run` passes.
10. README/SPEC impact is updated when public behavior changes.
