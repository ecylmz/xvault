#!/usr/bin/env sh
set -eu

bad="$(git ls-files | grep -E '(^|/)(\.env|bin/|dist/|xvault$)|\.(sqlite|sqlite-[^/]*|db|db-[^/]*)$|(^|/)coverage\.out$|\.test$' || true)"

if [ -n "$bad" ]; then
  printf '%s\n' "release safety check failed: tracked generated or secret-like files:" >&2
  printf '%s\n' "$bad" >&2
  exit 1
fi

git diff --check
