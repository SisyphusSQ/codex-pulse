#!/usr/bin/env bash

set -euo pipefail

SCRIPT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
CHECK_SCRIPT="$SCRIPT_DIR/check_generated.sh"

if [ ! -x "$CHECK_SCRIPT" ]; then
  printf 'expected executable generated check: %s\n' "$CHECK_SCRIPT" >&2
  exit 1
fi

TMP_ROOT=$(mktemp -d "${TMPDIR:-/tmp}/codex-pulse-generated-check-test.XXXXXX")
trap 'rm -rf "$TMP_ROOT"' EXIT

FIXTURE="$TMP_ROOT/repo"
mkdir -p "$FIXTURE/scripts/project-checks" "$FIXTURE/frontend/bindings" "$TMP_ROOT/bin"
cp "$CHECK_SCRIPT" "$FIXTURE/scripts/project-checks/check_generated.sh"
chmod +x "$FIXTURE/scripts/project-checks/check_generated.sh"

printf 'module example.com/generated-check\n\ngo 1.25.0\n' >"$FIXTURE/go.mod"
: >"$FIXTURE/go.sum"
printf 'export const existing = true;\n' >"$FIXTURE/frontend/bindings/existing.ts"

(
  cd "$FIXTURE"
  git init -q
  git add go.mod go.sum frontend/bindings/existing.ts
)

cat >"$TMP_ROOT/bin/go" <<'EOF'
#!/usr/bin/env bash
exit 0
EOF
cat >"$TMP_ROOT/bin/wails3" <<'EOF'
#!/usr/bin/env bash
mkdir -p frontend/bindings
printf 'export const generated = true;\n' >frontend/bindings/new-generated.ts
EOF
chmod +x "$TMP_ROOT/bin/go" "$TMP_ROOT/bin/wails3"

set +e
output=$(PATH="$TMP_ROOT/bin:$PATH" "$FIXTURE/scripts/project-checks/check_generated.sh" 2>&1)
rc=$?
set -e

if [ "$rc" -eq 0 ]; then
  printf 'expected untracked generated binding to fail verification\n' >&2
  exit 1
fi
if ! printf '%s\n' "$output" | grep -Fq '[VERIFY-003]'; then
  printf 'expected [VERIFY-003] failure, got:\n%s\n' "$output" >&2
  exit 1
fi
if ! printf '%s\n' "$output" | grep -Fq 'frontend/bindings/new-generated.ts'; then
  printf 'expected failure to identify new generated binding, got:\n%s\n' "$output" >&2
  exit 1
fi

printf 'generated-check contract tests passed\n'
