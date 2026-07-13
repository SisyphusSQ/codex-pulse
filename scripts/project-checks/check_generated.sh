#!/usr/bin/env bash

set -euo pipefail

SCRIPT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
REPO_ROOT=$(CDPATH= cd -- "$SCRIPT_DIR/../.." && pwd)
BEFORE=$(mktemp "${TMPDIR:-/tmp}/codex-pulse-generated-before.XXXXXX")
AFTER=$(mktemp "${TMPDIR:-/tmp}/codex-pulse-generated-after.XXXXXX")
trap 'rm -f "$BEFORE" "$AFTER"' EXIT

snapshot() {
  local output=$1
  (
    cd "$REPO_ROOT"
    {
      git ls-files -- go.mod go.sum frontend/bindings
      git ls-files --others --exclude-standard -- frontend/bindings
    } |
      LC_ALL=C sort -u |
      while IFS= read -r path; do
        if [ -f "$path" ]; then
          hash=$(shasum -a 256 "$path" | awk '{print $1}')
        else
          hash=MISSING
        fi
        printf '%s  %s\n' "$hash" "$path"
      done
  ) >"$output"
}

snapshot "$BEFORE"
(
  cd "$REPO_ROOT"
  go mod tidy
  wails3 generate bindings -clean=true -ts -i
)
snapshot "$AFTER"

if ! cmp -s "$BEFORE" "$AFTER"; then
  printf '[VERIFY-003] generated files changed after regeneration\n' >&2
  diff -u "$BEFORE" "$AFTER" >&2 || true
  printf 'command: make verify-generated\n' >&2
  exit 1
fi

(
  cd "$REPO_ROOT"
  git diff --check
)
printf 'generated bindings and Go module files are stable\n'
