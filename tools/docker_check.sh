#!/usr/bin/env sh
set -eu

timeout_seconds="${XVAULT_DOCKER_TIMEOUT:-180}"

run_with_timeout() {
  label="$1"
  shift
  if command -v timeout >/dev/null 2>&1; then
    if timeout -k 5s "${timeout_seconds}s" "$@"; then
      return 0
    fi
    status="$?"
    if [ "$status" -ge 124 ]; then
      printf '%s\n' "docker check failed: ${label} timed out after ${timeout_seconds}s" >&2
    fi
    return "$status"
  fi
  "$@" &
  pid="$!"
  (
    sleep "$timeout_seconds"
    kill "$pid" 2>/dev/null || true
  ) &
  timer="$!"
  if wait "$pid"; then
    kill "$timer" 2>/dev/null || true
    wait "$timer" 2>/dev/null || true
    return 0
  fi
  status="$?"
  kill "$timer" 2>/dev/null || true
  wait "$timer" 2>/dev/null || true
  if [ "$status" -ge 128 ]; then
    printf '%s\n' "docker check failed: ${label} timed out after ${timeout_seconds}s" >&2
  fi
  return "$status"
}

docker version >/dev/null
if ! run_with_timeout "docker build" docker build -t xvault:local .; then
  exit 1
fi
if ! docker image inspect xvault:local >/dev/null 2>&1; then
  printf '%s\n' "docker check failed: image xvault:local was not built" >&2
  exit 1
fi
if ! run_with_timeout "docker run" docker run --rm xvault:local version --json; then
  exit 1
fi
