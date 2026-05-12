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
xvault sync checkpoints --json
xvault sync sanitize-runs --json
xvault db integrity --json
```

`doctor` also reports publish-readiness checks for the Git `origin` remote and Docker daemon availability.

Use strict mode when a script should fail on any reported doctor check:

```bash
xvault doctor --strict --json
```

Use online mode when the diagnostic should also validate the active X session with the lightweight Viewer request used by `auth test`:

```bash
xvault doctor --online --strict --json
```

`doctor` treats failed sync runs as unresolved only until a later successful run exists for the same collection.

`sync runs` reports recent success, partial, and failed sync attempts with run IDs, counters, error codes, and timestamps. Filter it with `--collection bookmarks`, `--collection likes`, or `--status failed` when diagnosing automation.

`sync checkpoints` reports any retained resumable cursors after a bounded, interrupted, partial, or rate-limited sync.

`sync sanitize-runs` rewrites stored historical sync error messages to safe categorized summaries.
