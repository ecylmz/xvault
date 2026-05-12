#!/usr/bin/env sh
set -eu

version="${1:-}"
file="${2:-CHANGELOG.md}"

if [ -z "$version" ]; then
  printf '%s\n' "usage: $0 VERSION [CHANGELOG]" >&2
  exit 2
fi

if [ ! -f "$file" ]; then
  printf '%s\n' "changelog file not found: $file" >&2
  exit 1
fi

awk -v version="$version" '
  BEGIN { found=0; emitted=0 }
  /^##[[:space:]]+/ {
    if (found) {
      exit
    }
    line=$0
    if (line ~ "^##[[:space:]]+\\[" version "\\]" || line ~ "^##[[:space:]]+" version "([[:space:]]|-|$)") {
      found=1
      next
    }
  }
  found {
    print
    emitted=1
  }
  END {
    if (!found || !emitted) {
      exit 1
    }
  }
' "$file"
