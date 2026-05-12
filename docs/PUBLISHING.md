# Publishing

Run the local release gates before creating a GitHub repository or tag:

```bash
go test ./...
golangci-lint run
go build -o bin/xvault ./cmd/xvault
make release-check
make verify-archive
for os in linux darwin; do
  for arch in amd64 arm64; do
    GOOS=$os GOARCH=$arch go build -o /tmp/xvault-$os-$arch ./cmd/xvault
  done
done
```

`make verify-archive` is intentionally a local publishing gate, not a CI gate. It uses the configured SQLite database and fails unless the archive has synced bookmarks and likes that are queryable through normal collection search and FTS.

If Docker is available locally, also verify the container image:

```bash
docker build -t xvault:local .
docker run --rm xvault:local version --json
```

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
