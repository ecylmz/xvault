# Publishing

Run the local release gates before creating a GitHub repository or tag:

```bash
make publish-check
```

`make publish-check` runs the CI-safe checks, lint, local build, Linux/macOS amd64/arm64 cross-builds, and `make verify-archive`. `make verify-archive` is intentionally a local publishing gate, not a GitHub Actions gate. It uses the configured SQLite database and fails unless the archive has synced bookmarks and likes that are queryable through normal collection search and FTS.

If Docker is available locally, also verify the container image:

```bash
make docker-check
```

After fresh X cookies are configured, Docker is running, and the GitHub remote exists, run the strict readiness check:

```bash
xvault doctor --online --strict --json
```

This check fails if live X auth is rejected, local database integrity is broken, unresolved sync failures remain, `origin` is missing, or Docker is unavailable.

Create the GitHub repository only when you are ready for the project to become visible:

```bash
gh repo create ecylmz/xvault --private --source=. --remote=origin --push
```

Switch `--private` to `--public` only when docs, credentials, local database files, and generated artifacts have been checked. The repository `.gitignore` excludes dotenv files, binaries, SQLite databases, and build outputs.

To publish a release after the remote exists:

```bash
git tag v0.1.0
git push origin main v0.1.0
```

The release workflow builds Linux and macOS binaries for amd64 and arm64.
