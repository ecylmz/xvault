# Installation

## Homebrew

```bash
brew tap ecylmz/tap
brew install xvault
```

## Release Archives

Download the matching archive and checksum from the
[latest release](https://github.com/ecylmz/xvault/releases/latest).

| Platform | Archive |
|---|---|
| macOS Apple Silicon | `xvault-darwin-arm64.tar.gz` |
| macOS Intel | `xvault-darwin-amd64.tar.gz` |
| Linux x86_64 | `xvault-linux-amd64.tar.gz` |
| Linux arm64 | `xvault-linux-arm64.tar.gz` |

macOS Apple Silicon example:

```bash
base=https://github.com/ecylmz/xvault/releases/latest/download
curl -LO "$base/xvault-darwin-arm64.tar.gz"
curl -LO "$base/xvault-darwin-arm64.tar.gz.sha256"
shasum -a 256 -c xvault-darwin-arm64.tar.gz.sha256
tar -xzf xvault-darwin-arm64.tar.gz
xattr -d com.apple.quarantine xvault-darwin-arm64/xvault 2>/dev/null || true
sudo mv xvault-darwin-arm64/xvault /usr/local/bin/xvault
xvault version --json
```

Linux x86_64 example:

```bash
base=https://github.com/ecylmz/xvault/releases/latest/download
curl -LO "$base/xvault-linux-amd64.tar.gz"
curl -LO "$base/xvault-linux-amd64.tar.gz.sha256"
sha256sum -c xvault-linux-amd64.tar.gz.sha256
tar -xzf xvault-linux-amd64.tar.gz
sudo mv xvault-linux-amd64/xvault /usr/local/bin/xvault
xvault version --json
```

Install to another directory on `PATH`, such as `~/.local/bin`, if
`/usr/local/bin` is not writable or not on your `PATH`.

## From Source

```bash
git clone https://github.com/ecylmz/xvault.git
cd xvault
go test ./...
go build -o bin/xvault ./cmd/xvault
sudo install -m 0755 bin/xvault /usr/local/bin/xvault
```

## Docker

```bash
docker build -t xvault .
docker run --rm \
  -v "$HOME/.config/xvault:/home/xvault/.config/xvault:ro" \
  -v "$HOME/.local/share/xvault:/home/xvault/.local/share/xvault" \
  xvault status --json
```

Do not bake `.env` or cookies into an image.

## Authentication

The primary setup path is a private dotenv file:

```bash
mkdir -p ~/.config/xvault
chmod 700 ~/.config/xvault
cat > ~/.config/xvault/.env <<'EOF'
XVAULT_AUTH_TOKEN="..."
XVAULT_CT0="..."
XVAULT_TWID="..."
EOF
chmod 600 ~/.config/xvault/.env
```

`xvault` also attempts best-effort browser extraction from Firefox and
Chrome/Chromium. On macOS, Chrome/Chromium encrypted `v10` cookies require
access to the browser Safe Storage item in Keychain.

Use `--auth-source` to test or run with a specific source without editing
config:

```bash
xvault --auth-source firefox auth test --json
xvault --auth-source macos_keychain auth test --json
xvault --auth-source chrome sync bookmarks --count 100 --max-pages 2 --json
```

If browser extraction succeeds, write those cookies into the dotenv file:

```bash
xvault auth import-browser --source firefox --force --json
xvault auth import-browser --source chrome --force --json
```
