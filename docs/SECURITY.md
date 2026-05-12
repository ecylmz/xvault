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

## Agent Isolation

The supported agent/Hermes workflow is to call `xvault` commands and consume redacted JSON envelopes. Agents should not read dotenv files, browser profiles, Keychain items, shell history, raw GraphQL payloads, or the SQLite database directly.

For read-only agent access, prefer exported Markdown/JSONL or narrow commands such as `search`, `show`, `count`, `stats`, and `verify-archive`.

## Redaction Guarantee

CLI status and diagnostic commands report only whether credentials are present. Error envelopes are sanitized before output and tested not to print cookie header names or known credential markers such as `auth_token`, `ct0`, `Cookie`, `Authorization`, or `x-csrf-token`.

If a command fails in a way that could include upstream response details, share only the redacted JSON envelope. Do not share raw payloads or local database rows.

## Responsible Use

Use `xvault` only with X/Twitter accounts and local browser profiles you control. The tool is read-only: it does not post, like, delete, follow, unfollow, or otherwise mutate account state. Sync conservatively, respect rate limits, and keep exported archives private unless you intentionally review and publish their contents.
