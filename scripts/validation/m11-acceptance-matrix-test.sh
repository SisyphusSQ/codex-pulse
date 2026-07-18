#!/usr/bin/env bash

set -euo pipefail

repo_root=$(CDPATH= cd -- "$(dirname -- "$0")/../.." && pwd)
cd "$repo_root"

tmp_dir=$(mktemp -d "${TMPDIR:-/tmp}/codex-pulse-m11-matrix.XXXXXX")
trap 'rm -rf "$tmp_dir"' EXIT

canonical=docs/test/m11-e1.md
gate=scripts/validation/m11-acceptance-matrix.sh

expect_failure() {
  local name=$1
  local document=$2
  if M11_MATRIX_TEST_MODE=1 M11_MATRIX_DOC="$document" bash "$gate" >/dev/null 2>&1; then
    echo "M11-TEST-001: negative fixture unexpectedly passed: $name" >&2
    exit 1
  fi
  echo "negative fixture rejected: $name"
}

sed '/^| UPD-03 |/d' "$canonical" >"$tmp_dir/missing-row.md"
expect_failure missing-row "$tmp_dir/missing-row.md"

sed '/^| UPD-03 |/s/| TOO-302 | blocking |/| TOO-299 | blocking |/' "$canonical" >"$tmp_dir/wrong-owner.md"
expect_failure wrong-owner "$tmp_dir/wrong-owner.md"

sed '/^| UPD-01 |/s/| 关闭 updater\/loopback，清 synthetic state |/|  |/' "$canonical" >"$tmp_dir/empty-cleanup.md"
expect_failure empty-cleanup "$tmp_dir/empty-cleanup.md"

sed '/^| UPD-03 |/s/| 未执行 |$/| 通过 |/' "$canonical" >"$tmp_dir/false-pass.md"
expect_failure false-pass "$tmp_dir/false-pass.md"

sed '/^| ONB-01 |/s/| 真实数据 + 自动化 |/| 自动化 |/' "$canonical" >"$tmp_dir/downgraded-live.md"
expect_failure downgraded-live "$tmp_dir/downgraded-live.md"

if M11_MATRIX_DOC="$canonical" bash "$gate" >/dev/null 2>&1; then
  echo "M11-TEST-002: canonical gate accepted an unguarded document override" >&2
  exit 1
fi
echo "negative fixture rejected: unguarded-override"

echo "M11 acceptance matrix negative tests: PASS"
