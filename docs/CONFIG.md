# Configuration

Default paths:

- config: `~/.config/xvault/config.toml`
- dotenv auth: `~/.config/xvault/.env`
- database: `~/.local/share/xvault/xvault.sqlite`
- exports: `~/.local/share/xvault/exports`
- backups: `~/.local/state/xvault/backups`

Use `xvault init` to create a starter config.

Secrets may be supplied by environment variables, dotenv, or config. Environment variables and dotenv are preferred.

Feed sync uses `[sync].feed_default_hours` as the default lookback for `xvault sync feed --hours N`. The default is `24`.
