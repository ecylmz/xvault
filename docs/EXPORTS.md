# Exports

Supported export commands:

```bash
xvault export json --collection all --output archive.json --json
xvault export json --collection bookmarks --folder Research --output research-bookmarks.json --json
xvault export csv --collection all --output archive.csv --json
xvault export markdown --collection all --output exports/markdown --json
xvault export hermes --output exports/hermes --json
xvault export obsidian --output exports/obsidian --json
xvault export html --collection all --output archive.html --json
```

HTML export is a single offline file with local search and filters.

Hermes export writes Markdown plus `index.jsonl`.
