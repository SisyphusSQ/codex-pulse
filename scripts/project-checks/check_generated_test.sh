#!/usr/bin/env bash

set -euo pipefail

SCRIPT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
CHECK_SCRIPT="$SCRIPT_DIR/check_generated.sh"
FAILURE_SCRIPT="$SCRIPT_DIR/check_binding_generation_failure.sh"

if [ ! -x "$CHECK_SCRIPT" ] || [ ! -x "$FAILURE_SCRIPT" ]; then
  printf 'expected executable generated checks: %s %s\n' "$CHECK_SCRIPT" "$FAILURE_SCRIPT" >&2
  exit 1
fi

TMP_ROOT=$(mktemp -d "${TMPDIR:-/tmp}/codex-pulse-generated-check-test.XXXXXX")
trap 'rm -rf "$TMP_ROOT"' EXIT

FIXTURE="$TMP_ROOT/repo"
mkdir -p "$FIXTURE/scripts/project-checks" "$FIXTURE/frontend/bindings" "$TMP_ROOT/bin"
cp "$CHECK_SCRIPT" "$FIXTURE/scripts/project-checks/check_generated.sh"
cp "$FAILURE_SCRIPT" "$FIXTURE/scripts/project-checks/check_binding_generation_failure.sh"
chmod +x "$FIXTURE/scripts/project-checks/check_generated.sh" \
  "$FIXTURE/scripts/project-checks/check_binding_generation_failure.sh"

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
if [ "${1:-}" = "version" ]; then
  printf '%s\n' "${CODEX_PULSE_TEST_WAILS_VERSION:-v3.0.0-alpha2.117}"
  exit 0
fi
if [ "${CODEX_PULSE_TEST_GENERIC_GENERATE_FAILURE:-0}" = "1" ]; then
  printf 'synthetic generic generator failure\n' >&2
  exit 1
fi
if printf '%s\n' "$*" | grep -Fq -- '-models INVALID'; then
  if [ "${CODEX_PULSE_TEST_MUTATE_BINDINGS:-0}" = "1" ]; then
    printf 'export const changed = true;\n' >frontend/bindings/existing.ts
  fi
  printf 'models filename must not contain uppercase characters\n' >&2
  exit 1
fi
mkdir -p frontend/bindings
printf 'export const generated = true;\n' >frontend/bindings/new-generated.ts
EOF
chmod +x "$TMP_ROOT/bin/go" "$TMP_ROOT/bin/wails3"

PATH="$TMP_ROOT/bin:$PATH" \
  "$FIXTURE/scripts/project-checks/check_binding_generation_failure.sh"

set +e
generic_output=$(CODEX_PULSE_TEST_GENERIC_GENERATE_FAILURE=1 PATH="$TMP_ROOT/bin:$PATH" \
  "$FIXTURE/scripts/project-checks/check_binding_generation_failure.sh" 2>&1)
generic_rc=$?
set -e
if [ "$generic_rc" -eq 0 ] || ! printf '%s\n' "$generic_output" | grep -Fq '[BINDING-001]'; then
  printf 'expected generic generation failure to be rejected, got:\n%s\n' \
    "$generic_output" >&2
  exit 1
fi

set +e
version_output=$(CODEX_PULSE_TEST_WAILS_VERSION=v0.0.0 PATH="$TMP_ROOT/bin:$PATH" \
  "$FIXTURE/scripts/project-checks/check_binding_generation_failure.sh" 2>&1)
version_rc=$?
set -e
if [ "$version_rc" -eq 0 ] || ! printf '%s\n' "$version_output" | grep -Fq '[BINDING-001]'; then
  printf 'expected Wails version mismatch to fail verification, got:\n%s\n' \
    "$version_output" >&2
  exit 1
fi

set +e
failure_output=$(CODEX_PULSE_TEST_MUTATE_BINDINGS=1 PATH="$TMP_ROOT/bin:$PATH" \
  "$FIXTURE/scripts/project-checks/check_binding_generation_failure.sh" 2>&1)
failure_rc=$?
set -e
if [ "$failure_rc" -eq 0 ]; then
  printf 'expected mutation during failed generation to fail verification\n' >&2
  exit 1
fi
if ! printf '%s\n' "$failure_output" | grep -Fq '[BINDING-001]'; then
  printf 'expected [BINDING-001] failure, got:\n%s\n' "$failure_output" >&2
  exit 1
fi
printf 'export const existing = true;\n' >"$FIXTURE/frontend/bindings/existing.ts"

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
