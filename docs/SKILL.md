# xvault Skill

Use `xvault` to search, inspect, sync, and export the user's local X/Twitter archive.

## Rules

- Always use `--json` for command output.
- Search local data before syncing.
- Do not read cookies, config secrets, environment variables, shell history, browser profiles, or Keychain entries.
- Do not query or modify SQLite directly. The default archive path is `~/.local/share/xvault/xvault.sqlite`, but agents must use `xvault` commands to read or maintain it.
- Never change the database by writing SQL against SQLite. Use `xvault sync`, `xvault db rebuild-fts`, migrations, or other first-party commands only.
- Do not run aggressive sync loops.
- Stop on rate limits or repeated auth failures.
- Do not use `show --include-raw`.
- Report concise results with author, date, source collection, short summary, and URL.

## Discovery

If a needed command, flag, or output shape is not documented here, run help instead of guessing:

```bash
xvault --help
xvault COMMAND --help
xvault COMMAND SUBCOMMAND --help
```

## Main Commands

```bash
xvault version --json
xvault status --json
xvault doctor --json
xvault doctor --online --strict --json
xvault auth status --json
xvault auth sources --json
xvault --auth-source chrome auth test --json
xvault --auth-source firefox auth test --json
xvault sync likes --count 100 --max-pages 2 --json
xvault sync bookmarks --count 100 --max-pages 2 --json
xvault sync bookmarks --all --json
xvault sync likes --all --json
xvault sync feed --hours 24 --count 100 --max-pages 2 --json
xvault sync runs --limit 10 --json
xvault sync summary --json
xvault sync checkpoints --json
xvault bookmarks folders --json
xvault search "query" --source all --limit 10 --json
xvault search "query" --source bookmarks --limit 10 --json
xvault search --recent --source bookmarks --limit 10 --json
xvault search --recent --source likes --limit 10 --json
xvault count bookmarks --json
xvault count likes --json
xvault verify-archive --json
xvault show TWEET_ID --json
xvault show-url https://x.com/USER/status/TWEET_ID --json
xvault stats --json
xvault export json --collection all --output archive.json --json
xvault export json --collection bookmarks --folder Research --output research-bookmarks.json --json
xvault export markdown --collection all --output exports/markdown --json
xvault export csv --collection all --output archive.csv --json
xvault export html --collection all --output archive.html --json
xvault export hermes --json
xvault export obsidian --collection all --output exports/obsidian --json
xvault db integrity --json
xvault db rebuild-fts --json
xvault backup create --json
xvault backup list --json
xvault thread TWEET_ID --json
xvault conversation TWEET_ID --json
```

## Sync Guidance

Prefer bounded syncs for routine agent work:

```bash
xvault sync bookmarks --count 100 --max-pages 2 --json
xvault sync likes --count 100 --max-pages 2 --json
```

Use full syncs only when explicitly asked to refresh the complete saved archive:

```bash
xvault sync bookmarks --all --json
xvault sync likes --all --json
xvault verify-archive --json
```

Use `--max-pages` whenever predictable network behavior matters.
