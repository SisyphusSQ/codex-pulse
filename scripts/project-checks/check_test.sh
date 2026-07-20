#!/usr/bin/env bash

set -euo pipefail

SCRIPT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
REPO_ROOT=$(CDPATH= cd -- "$SCRIPT_DIR/../.." && pwd)
CHECK_SCRIPT="$SCRIPT_DIR/check.sh"
TMP_ROOT=$(mktemp -d "${TMPDIR:-/tmp}/codex-pulse-project-check.XXXXXX")
trap 'rm -rf "$TMP_ROOT"' EXIT

copy_fixture() {
  rm -rf "$TMP_ROOT/repo"
  mkdir -p "$TMP_ROOT/repo"
  while IFS= read -r -d '' path; do
    [ -f "$REPO_ROOT/$path" ] || continue
    mkdir -p "$TMP_ROOT/repo/$(dirname -- "$path")"
    cp "$REPO_ROOT/$path" "$TMP_ROOT/repo/$path"
  done < <(git -C "$REPO_ROOT" ls-files -co --exclude-standard -z)
}

assert_failure() {
  local rule=$1
  local output
  set +e
  output=$(cd "$TMP_ROOT/repo" && bash scripts/project-checks/check.sh 2>&1)
  local rc=$?
  set -e
  if [ "$rc" -eq 0 ] || ! printf '%s\n' "$output" | grep -Fq "[$rule]"; then
    printf 'expected [%s] failure, got:\n%s\n' "$rule" "$output" >&2
    exit 1
  fi
}

copy_fixture
mkdir -p "$TMP_ROOT/repo/frontend"
: >"$TMP_ROOT/repo/frontend/package.json"
assert_failure ARCH-001

copy_fixture
printf '\n// github.com/wailsapp must not return\n' >>"$TMP_ROOT/repo/main.go"
assert_failure ARCH-001

copy_fixture
sed 's/google.golang.org\/grpc v1.82.1/google.golang.org\/grpc v1.82.0/' \
  "$TMP_ROOT/repo/go.mod" >"$TMP_ROOT/go.mod"
mv "$TMP_ROOT/go.mod" "$TMP_ROOT/repo/go.mod"
assert_failure TOOLCHAIN-001

copy_fixture
sed 's/contents: read/contents: write/' "$TMP_ROOT/repo/.github/workflows/ci.yml" >"$TMP_ROOT/ci.yml"
mv "$TMP_ROOT/ci.yml" "$TMP_ROOT/repo/.github/workflows/ci.yml"
assert_failure CI-001

printf 'project-check contract tests passed\n'
