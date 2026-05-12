#!/usr/bin/env sh
set -eu

version="${VERSION:-${1:-}}"

if [ -z "$version" ]; then
  printf '%s\n' "usage: VERSION=v0.1.0 make release" >&2
  exit 2
fi

case "$version" in
  v[0-9]*.[0-9]*.[0-9]*) ;;
  *)
    printf '%s\n' "release version must look like vMAJOR.MINOR.PATCH" >&2
    exit 2
    ;;
esac

if ! command -v gh >/dev/null 2>&1; then
  printf '%s\n' "gh is required to create and verify GitHub releases" >&2
  exit 1
fi

if ! git diff --quiet || ! git diff --cached --quiet; then
  printf '%s\n' "working tree has uncommitted changes; commit before release" >&2
  exit 1
fi

if git rev-parse -q --verify "refs/tags/$version" >/dev/null; then
  printf '%s\n' "tag already exists locally: $version" >&2
  exit 1
fi

if git ls-remote --exit-code --tags origin "$version" >/dev/null 2>&1; then
  printf '%s\n' "tag already exists on origin: $version" >&2
  exit 1
fi

notes="$(mktemp)"
trap 'rm -f "$notes"' EXIT
sh tools/changelog_notes.sh "$version" > "$notes"

make publish-check
make dist
(cd dist && shasum -a 256 -c *.sha256)

git tag -a "$version" -m "Release $version"
git push origin main "$version"

run_id=""
for _ in 1 2 3 4 5 6; do
  run_id="$(gh run list --workflow Release --branch "$version" --limit 1 --json databaseId --jq '.[0].databaseId // ""' 2>/dev/null || true)"
  if [ -n "$run_id" ]; then
    break
  fi
  sleep 5
done

if [ -n "$run_id" ]; then
  gh run watch "$run_id" --exit-status
fi

for _ in 1 2 3 4 5 6 7 8 9 10 11 12; do
  if gh release view "$version" --json tagName,url,publishedAt; then
    exit 0
  fi
  sleep 5
done

printf '%s\n' "release was not visible on GitHub after waiting: $version" >&2
exit 1
