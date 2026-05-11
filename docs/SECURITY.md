# Security

`auth_token` and `ct0` are live session credentials. Anyone who can read them may be able to act as the account in a browser session.

Rules:

- keep `~/.config/xvault` mode `0700`
- keep `~/.config/xvault/.env` mode `0600`
- do not put cookies in shell history, logs, screenshots, fixtures, or commits
- use `--json` for agent workflows
- do not let agents inspect config files, browser profiles, keychains, or the raw SQLite database
- prefer separate users or containers for sync and read-only export access

`show --include-raw` is blocked by default when agent safe mode is enabled.
