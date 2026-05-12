# Agent Instructions

This repository contains `xvault`, a Go single-binary personal X/Twitter archival tool. Work from the real repo state, keep changes small and verifiable, and prefer existing package patterns over new abstractions.

## Safety

- Never print, commit, copy, or transform real cookie values from `~/.config/xvault/.env`, shell environments, browser profiles, Keychain, logs, or config files.
- Never add dotenv files, SQLite databases, `bin/`, `dist/`, local backups, or generated archives to Git.
- Do not modify the local SQLite archive with direct SQL. Use `xvault` commands and migrations only.
- Keep all X interactions read-only. Do not add commands that post, like, unlike, delete, bookmark, unbookmark, follow, unfollow, DM, or automate engagement.

## Development

- Format Go code with `gofmt`.
- Run focused tests while editing, then run `make publish-check` before publishing changes.
- Use `make docker-check` when Docker-related behavior or release readiness is in scope.
- Use `bin/xvault --help` or subcommand `--help` when command behavior is unclear instead of guessing.
- Use `bin/xvault doctor --online --strict --json` only when valid local cookies are intentionally configured.

## Commits

- Use Conventional Commits for all new commits, for example `fix: accept encoded twid cookies` or `docs: expand agent skill guidance`.
- Keep commit messages in English.
- Commit related code, tests, and documentation together when they describe one behavior change.

## Changelog And Releases

- Update `CHANGELOG.md` for user-facing changes, release process changes, security changes, and compatibility changes.
- Add release notes under a `## [vX.Y.Z] - YYYY-MM-DD` section before running a release.
- Create releases with `make release VERSION=vX.Y.Z`. The release workflow builds Linux and macOS binaries for `amd64` and `arm64` and publishes checksum files.
- Do not create release notes only in GitHub; the source of truth is `CHANGELOG.md`.
