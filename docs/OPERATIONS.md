# Operations

Recommended automation interval is every 3 to 6 hours for bookmarks or likes.

Print examples:

```bash
xvault service systemd print --user
xvault service cron print
```

Run bounded syncs in automation:

```bash
xvault sync bookmarks --count 300 --max-pages 5 --json
xvault sync likes --count 300 --max-pages 5 --json
```

Before troubleshooting sync, run:

```bash
xvault doctor --json
xvault auth test --json
xvault sync runs --limit 10 --json
xvault db integrity --json
```

`sync runs` reports recent success, partial, and failed sync attempts with run IDs, counters, error codes, and timestamps. Filter it with `--collection bookmarks`, `--collection likes`, or `--status failed` when diagnosing automation.
