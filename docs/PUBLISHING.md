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

Set `XVAULT_DOCKER_TIMEOUT=300` if Docker Hub image pulls are slow. The check fails instead of hanging when Docker build or run steps do not finish within the timeout.
By default, `make docker-check` uses an offline scratch-image smoke test built from the locally compiled Linux binary, so it can verify container execution without pulling base images. Run `XVAULT_DOCKER_CHECK_MODE=online make docker-check` when you specifically want to verify the checked-in multi-stage Dockerfile and Docker Hub base image pulls.

To dry-run release artifacts and checksums locally:

```bash
make dist
ls dist/*.sha256
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

To publish a release after the remote exists, add release notes to `CHANGELOG.md` under a matching version heading and run:

```bash
make release VERSION=v0.1.0
```

`make release` requires a clean working tree, extracts the release notes from `CHANGELOG.md`, runs the local publish gates, builds and verifies local release artifacts, creates an annotated tag, pushes `main` and the tag, waits for the GitHub Release workflow when available, and verifies the created release.

The release workflow builds Linux and macOS binaries for amd64 and arm64, publishes matching `.sha256` checksum files, and uses the matching `CHANGELOG.md` section as the GitHub release notes.
