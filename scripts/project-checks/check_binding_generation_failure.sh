#!/usr/bin/env bash

set -euo pipefail

SCRIPT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
REPO_ROOT=$(CDPATH= cd -- "$SCRIPT_DIR/../.." && pwd)
WAILS_VERSION=v3.0.0-alpha2.117

if ! WAILS_BIN=$(command -v wails3); then
  printf '[BINDING-001] wails3 is required for failure-preservation verification\n' >&2
  exit 1
fi
actual_version=$("$WAILS_BIN" version 2>&1) || {
  printf '[BINDING-001] unable to read wails3 version\n' >&2
  exit 1
}
if [ "$actual_version" != "$WAILS_VERSION" ]; then
  printf '[BINDING-001] wails3 version mismatch: expected %s, got %s\n' \
    "$WAILS_VERSION" "$actual_version" >&2
  exit 1
fi

snapshot() {
  (
    cd "$REPO_ROOT"
    {
      git ls-files -- frontend/bindings
      git ls-files --others --exclude-standard -- frontend/bindings
    } |
      LC_ALL=C sort -u |
      while IFS= read -r file_path; do
        if [ -f "$file_path" ]; then
          hash=$(/usr/bin/shasum -a 256 "$file_path" | awk '{print $1}')
          printf '%s  %s\n' "$hash" "$file_path"
        fi
      done
  )
}

before=$(snapshot)
set +e
output=$(
  cd "$REPO_ROOT"
  "$WAILS_BIN" generate bindings -clean=true -ts -i -models INVALID 2>&1
)
rc=$?
set -e

if [ "$rc" -eq 0 ]; then
  printf '[BINDING-001] expected injected binding generation failure\n' >&2
  exit 1
fi
if ! printf '%s\n' "$output" |
  grep -Fq 'models filename must not contain uppercase characters'; then
  printf '[BINDING-001] generation failed before the injected models validation\n' >&2
  exit 1
fi

after=$(snapshot)
if [ "$before" != "$after" ]; then
  printf '[BINDING-001] failed generation changed frontend/bindings\n' >&2
  diff -u <(printf '%s\n' "$before") <(printf '%s\n' "$after") >&2 || true
  exit 1
fi

file_count=$(printf '%s\n' "$before" | awk 'NF { count++ } END { print count + 0 }')
printf 'failed binding generation preserved %s generated files\n' "$file_count"
