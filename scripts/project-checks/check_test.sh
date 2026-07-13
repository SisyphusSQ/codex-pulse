#!/usr/bin/env bash

set -euo pipefail

SCRIPT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
REPO_ROOT=$(CDPATH= cd -- "$SCRIPT_DIR/../.." && pwd)
CHECK_SCRIPT="$SCRIPT_DIR/check.sh"

if [ ! -x "$CHECK_SCRIPT" ]; then
  printf 'expected executable project check: %s\n' "$CHECK_SCRIPT" >&2
  exit 1
fi

TMP_ROOT=$(mktemp -d "${TMPDIR:-/tmp}/codex-pulse-project-check-test.XXXXXX")
trap 'rm -rf "$TMP_ROOT"' EXIT

FIXTURE="$TMP_ROOT/repo"
mkdir -p "$FIXTURE/frontend" "$FIXTURE/.github/workflows" "$FIXTURE/build/darwin" "$FIXTURE/scripts/harness" "$FIXTURE/scripts/project-checks"
cp "$REPO_ROOT/go.mod" "$FIXTURE/go.mod"
cp "$REPO_ROOT/Makefile" "$FIXTURE/Makefile"
cp "$REPO_ROOT/Taskfile.yml" "$FIXTURE/Taskfile.yml"
cp "$REPO_ROOT/build/darwin/Taskfile.yml" "$FIXTURE/build/darwin/Taskfile.yml"
cp "$REPO_ROOT/scripts/harness/check.sh" "$FIXTURE/scripts/harness/check.sh"
cp "$REPO_ROOT/frontend/package.json" "$FIXTURE/frontend/package.json"
cp "$REPO_ROOT/frontend/package-lock.json" "$FIXTURE/frontend/package-lock.json"
cp "$REPO_ROOT/.github/workflows/ci.yml" "$FIXTURE/.github/workflows/ci.yml"
cp "$CHECK_SCRIPT" "$FIXTURE/scripts/project-checks/check.sh"
cp "$SCRIPT_DIR/check_workflow_permissions.rb" "$FIXTURE/scripts/project-checks/check_workflow_permissions.rb"
chmod +x "$FIXTURE/scripts/project-checks/check.sh"
FIXTURE_CHECK="$FIXTURE/scripts/project-checks/check.sh"

ORIGINAL_PACKAGE="$TMP_ROOT/package.json"
ORIGINAL_LOCK="$TMP_ROOT/package-lock.json"
ORIGINAL_WORKFLOW="$TMP_ROOT/ci.yml"
ORIGINAL_HARNESS_CHECK="$TMP_ROOT/harness-check.sh"
ORIGINAL_GO_MOD="$TMP_ROOT/go.mod"
cp "$FIXTURE/frontend/package.json" "$ORIGINAL_PACKAGE"
cp "$FIXTURE/frontend/package-lock.json" "$ORIGINAL_LOCK"
cp "$FIXTURE/.github/workflows/ci.yml" "$ORIGINAL_WORKFLOW"
cp "$FIXTURE/scripts/harness/check.sh" "$ORIGINAL_HARNESS_CHECK"
cp "$FIXTURE/go.mod" "$ORIGINAL_GO_MOD"

FAKE_WAILS="$TMP_ROOT/wails3"
write_fake_wails() {
  local version=$1
  printf '#!/usr/bin/env bash\nprintf "%%s\\n" "%s"\n' "$version" >"$FAKE_WAILS"
  chmod +x "$FAKE_WAILS"
}

FAILURES=0

run_check() {
  PATH="$TMP_ROOT:$PATH" "$FIXTURE_CHECK"
}

assert_failure() {
  local expected_rule=$1
  shift
  local output
  local rc
  set +e
  output=$("$@" 2>&1)
  rc=$?
  set -e
  if [ "$rc" -eq 0 ]; then
    printf 'expected failure containing %s, command passed\n' "$expected_rule" >&2
    FAILURES=$((FAILURES + 1))
    return 0
  fi
  if ! printf '%s\n' "$output" | grep -Fq "[$expected_rule]"; then
    printf 'expected failure containing [%s], got:\n%s\n' "$expected_rule" "$output" >&2
    FAILURES=$((FAILURES + 1))
    return 0
  fi
}

assert_command_failure() {
  local label=$1
  shift
  local rc
  set +e
  "$@" >/dev/null 2>&1
  rc=$?
  set -e
  if [ "$rc" -eq 0 ]; then
    printf 'expected %s to fail, command passed\n' "$label" >&2
    FAILURES=$((FAILURES + 1))
  fi
}

replace_text() {
  local path=$1
  local from=$2
  local to=$3
  node -e '
    const fs = require("node:fs");
    const [path, from, to] = process.argv.slice(1);
    const current = fs.readFileSync(path, "utf8");
    if (!current.includes(from)) {
      console.error(`missing fixture text in ${path}: ${from}`);
      process.exit(1);
    }
    fs.writeFileSync(path, current.replace(from, to));
  ' "$path" "$from" "$to"
}

write_fake_wails "v3.0.0-alpha2.117"
run_check >/dev/null

replace_text "$FIXTURE/go.mod" "v3.0.0-alpha2.117" "v3.0.0-alpha2.118"
assert_failure TOOLCHAIN-001 run_check
cp "$ORIGINAL_GO_MOD" "$FIXTURE/go.mod"

replace_text "$FIXTURE/frontend/package.json" "3.0.0-alpha.97" "3.0.0-alpha.98"
assert_failure TOOLCHAIN-001 run_check
cp "$ORIGINAL_PACKAGE" "$FIXTURE/frontend/package.json"

replace_text "$FIXTURE/frontend/package.json" '"node": "^22.13.0 || >=24.0.0"' '"node": ">=22.12.0"'
assert_failure TOOLCHAIN-001 run_check
cp "$ORIGINAL_PACKAGE" "$FIXTURE/frontend/package.json"

replace_text "$FIXTURE/frontend/package.json" '"node": "^22.13.0 || >=24.0.0"' '"node": ">=22.13.0"'
assert_failure TOOLCHAIN-001 run_check
cp "$ORIGINAL_PACKAGE" "$FIXTURE/frontend/package.json"

replace_text "$FIXTURE/frontend/package-lock.json" '"node": "^22.13.0 || >=24.0.0"' '"node": ">=22.12.0"'
assert_failure TOOLCHAIN-001 run_check
cp "$ORIGINAL_LOCK" "$FIXTURE/frontend/package-lock.json"

replace_text "$FIXTURE/.github/workflows/ci.yml" "runs-on: macos-15" "runs-on: macos-latest"
assert_failure CI-001 run_check
cp "$ORIGINAL_WORKFLOW" "$FIXTURE/.github/workflows/ci.yml"

replace_text "$FIXTURE/.github/workflows/ci.yml" "node-version: '22.13.0'" "node-version: '22.12.0'"
assert_failure CI-001 run_check
cp "$ORIGINAL_WORKFLOW" "$FIXTURE/.github/workflows/ci.yml"

ripgrep_source_tokens=(
  'rg --version'
  'x=$(rg -q foo file)'
  '(rg -q foo file)'
  '`rg -q foo file`'
  '/opt/homebrew/bin/rg foo'
  '# rg --version is unavailable'
  'message="use rg command"'
)
for token in "${ripgrep_source_tokens[@]}"; do
  replace_text "$FIXTURE/scripts/harness/check.sh" "set -euo pipefail" $'set -euo pipefail\n'"$token"
  assert_failure CI-001 run_check
  cp "$ORIGINAL_HARNESS_CHECK" "$FIXTURE/scripts/harness/check.sh"
done

replace_text "$FIXTURE/.github/workflows/ci.yml" "contents: read" "contents: write"
assert_failure CI-001 run_check
cp "$ORIGINAL_WORKFLOW" "$FIXTURE/.github/workflows/ci.yml"

replace_text "$FIXTURE/.github/workflows/ci.yml" "  verify:
    name:" "  verify:
    permissions: write-all
    name:"
assert_failure CI-001 run_check
cp "$ORIGINAL_WORKFLOW" "$FIXTURE/.github/workflows/ci.yml"

replace_text "$FIXTURE/.github/workflows/ci.yml" "  verify:
    name:" "  verify:
    permissions : {contents: write}
    name:"
assert_failure CI-001 run_check
cp "$ORIGINAL_WORKFLOW" "$FIXTURE/.github/workflows/ci.yml"

replace_text "$FIXTURE/.github/workflows/ci.yml" "  verify:
    name:" "  verify:
    <<: {permissions: write-all}
    name:"
assert_failure CI-001 run_check
cp "$ORIGINAL_WORKFLOW" "$FIXTURE/.github/workflows/ci.yml"

replace_text "$FIXTURE/.github/workflows/ci.yml" "  contents: read" "  contents: write
  contents: read"
assert_command_failure "duplicate permissions child key" ruby "$FIXTURE/scripts/project-checks/check_workflow_permissions.rb" "$FIXTURE/.github/workflows/ci.yml"
cp "$ORIGINAL_WORKFLOW" "$FIXTURE/.github/workflows/ci.yml"

replace_text "$FIXTURE/.github/workflows/ci.yml" "          cache: false" "          cache: true"
assert_failure CI-001 run_check
cp "$ORIGINAL_WORKFLOW" "$FIXTURE/.github/workflows/ci.yml"

replace_text "$FIXTURE/.github/workflows/ci.yml" '      - name: Run unified verification
        run: make verify' '      - name: Run unified verification
        run: make verify

      - name: Access implicit token
        env:
          GH_TOKEN: ${{ github.token }}
        run: gh api /user'
assert_failure CI-001 run_check
cp "$ORIGINAL_WORKFLOW" "$FIXTURE/.github/workflows/ci.yml"

replace_text "$FIXTURE/.github/workflows/ci.yml" '      - name: Run unified verification
        run: make verify' $'      - name: Run unified verification
        run: make verify

      - name: Access token with dynamic index
        env:
          GH_TOKEN: ${{ github[format(\'to{0}\', \'ken\')] }}
        run: gh api /user'
assert_failure CI-001 run_check
cp "$ORIGINAL_WORKFLOW" "$FIXTURE/.github/workflows/ci.yml"

replace_text "$FIXTURE/.github/workflows/ci.yml" '      - name: Run unified verification
        run: make verify' $'      - name: Run unified verification
        run: make verify

      - name: Access token after YAML decoding
        env:
          GH_TOKEN: "\\u0024{{ github[format(\'to{0}\', \'ken\')] }}"
        run: gh api /user'
assert_failure CI-001 run_check
cp "$ORIGINAL_WORKFLOW" "$FIXTURE/.github/workflows/ci.yml"

replace_text "$FIXTURE/.github/workflows/ci.yml" '      - name: Run unified verification
        run: make verify' '      - name: Run unified verification
        run: make verify

      - name: Access implicit token with index syntax
        env:
          GH_TOKEN: ${{ github["token"] }}
        run: gh api /user'
assert_failure CI-001 run_check
cp "$ORIGINAL_WORKFLOW" "$FIXTURE/.github/workflows/ci.yml"

replace_text "$FIXTURE/.github/workflows/ci.yml" '      - name: Run unified verification
        run: make verify' '      - name: Run unified verification
        run: make verify

      - name: Publish release
        run: gh release create v0.1.0'
assert_failure CI-001 run_check
cp "$ORIGINAL_WORKFLOW" "$FIXTURE/.github/workflows/ci.yml"

replace_text "$FIXTURE/.github/workflows/ci.yml" 'status=$(git status --porcelain --untracked-files=all)' 'status=$(git status --porcelain)'
assert_failure CI-001 run_check
cp "$ORIGINAL_WORKFLOW" "$FIXTURE/.github/workflows/ci.yml"

write_fake_wails "v3.0.0-alpha2.118"
assert_failure TOOLCHAIN-001 run_check

if [ "$FAILURES" -ne 0 ]; then
  printf 'project-check contract tests failed: %s unexpected pass(es)\n' "$FAILURES" >&2
  exit 1
fi

printf 'project-check contract tests passed\n'
