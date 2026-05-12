#!/usr/bin/env sh
set -eu

bad="$(git ls-files | grep -E '(^|/)(\.env(\..*)?|bin/|dist/|xvault$)|\.(env|env\..*|sqlite|sqlite-[^/]*|db|db-[^/]*)$|(^|/)coverage\.out$|\.test$' || true)"

if [ -n "$bad" ]; then
  printf '%s\n' "release safety check failed: tracked generated or secret-like files:" >&2
  printf '%s\n' "$bad" >&2
  exit 1
fi

check_ignore() {
  file="$1"
  pattern="$2"
  label="$3"
  if ! grep -Eq "$pattern" "$file"; then
    printf '%s\n' "release safety check failed: $file does not ignore $label" >&2
    exit 1
  fi
}

for ignore_file in .gitignore .dockerignore; do
  if [ ! -f "$ignore_file" ]; then
    printf '%s\n' "release safety check failed: $ignore_file is missing" >&2
    exit 1
  fi
  check_ignore "$ignore_file" '(^|/)\.env$|(^|/)\.env\.\*$|^\*\.env$|^\*\.env\.\*$' "dotenv files"
  check_ignore "$ignore_file" '(^|/|^\*)\*?\.sqlite' "SQLite databases"
  check_ignore "$ignore_file" '(^|/)bin/' "local binaries"
  check_ignore "$ignore_file" '(^|/)dist/' "release artifacts"
done

git diff --check
