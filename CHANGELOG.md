# Changelog

All notable changes to xvault are documented here. Release notes are generated
from the matching version section in this file.

## [Unreleased]

### Added

- Add the MIT license.

### Documentation

- Document release binary installation with checksum verification for macOS and Linux.
- Use installed `xvault` commands in the README first-run examples.
- Restructure the README as a concise open-source entry point with links to
  detailed docs.

### Fixed

- Generate release checksum files with portable basenames instead of `dist/`
  paths.

## [v0.1.0] - 2026-05-12

### Added

- Initial Go single-binary CLI for local X/Twitter archival.
- Cookie-based auth from environment, `~/.config/xvault/.env`, config,
  Firefox, Chrome/Chromium, and best-effort macOS Keychain browser extraction.
- Full and incremental sync for likes, bookmarks, bookmark folders, own tweets,
  reposts, replies, posts, feed, threads, conversations, quoted tweets, and
  tombstones.
- SQLite storage with migrations, WAL, compressed raw payloads, collection
  tables, sync runs, checkpoints, and contentless FTS5 search.
- JSON, Markdown, CSV, HTML, Hermes, and Obsidian exports.
- Agent-safe `--json` command envelopes, redaction, raw-output blocking,
  operation locks, diagnostics, and strict `doctor` checks.
- Backup, integrity, FTS rebuild, vacuum, stats, query-id refresh, and archive
  verification commands.
- Dockerfile, docker-compose example, release workflows, and Linux/macOS
  amd64/arm64 release builds.

### Changed

- Accept URL-encoded `twid` cookie values such as `u%3D...` during auth shape
  validation.
- Keep doctor tests isolated from the developer machine's real auth and Git remote state.

### Security

- Dotenv files, SQLite databases, local binaries, and generated release
  artifacts are excluded from Git and Docker contexts.
- Malformed or placeholder auth imports are rejected before writing dotenv files
  or contacting X.
