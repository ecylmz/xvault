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
xvault db integrity --json
```
