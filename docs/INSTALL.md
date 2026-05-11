# Installation

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

`xvault` also attempts best-effort browser extraction from Firefox and Chrome/Chromium. On macOS, Chrome/Chromium encrypted `v10` cookies require access to the browser Safe Storage item in Keychain.

Use `--auth-source` to test or run with a specific source without editing config:

```bash
xvault --auth-source firefox auth test --json
xvault --auth-source chrome sync bookmarks --count 100 --max-pages 2 --json
```

If browser extraction succeeds, write those cookies into the dotenv file:

```bash
xvault auth import-browser --source firefox --force --json
xvault auth import-browser --source chrome --force --json
```
