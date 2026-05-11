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
