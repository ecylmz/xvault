# xvault Skill

Use `xvault` to search and retrieve the user's local X/Twitter archive.

Rules:

- Always use `--json`.
- Search local data before syncing.
- Do not read cookies, config secrets, environment variables, shell history, browser profiles, or SQLite directly.
- Do not run aggressive sync loops.
- Stop on rate limits.
- Do not use `show --include-raw`.
- Report concise results with author, date, source collection, short summary, and URL.

Common commands:

```bash
xvault status --json
xvault stats --json
xvault search "query" --source all --limit 10 --json
xvault show TWEET_ID --json
xvault sync bookmarks --count 100 --max-pages 2 --json
xvault sync likes --count 100 --max-pages 2 --json
xvault export hermes --json
```

Prefer bounded syncs. Use `--max-pages` when an agent needs predictable network behavior.
