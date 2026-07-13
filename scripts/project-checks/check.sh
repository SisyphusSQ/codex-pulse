#!/usr/bin/env bash

set -euo pipefail

SCRIPT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
REPO_ROOT=$(CDPATH= cd -- "$SCRIPT_DIR/../.." && pwd)

EXPECTED_WAILS="v3.0.0-alpha2.117"
EXPECTED_RUNTIME="3.0.0-alpha.97"
EXPECTED_GO="1.25.0"
EXPECTED_NODE_VERSION="22.13.0"
EXPECTED_NODE_ENGINE="^22.13.0 || >=24.0.0"
EXPECTED_NPM_ENGINE=">=10.0.0"
EXPECTED_JSDOM_NODE_ENGINE="^20.19.0 || ^22.13.0 || >=24.0.0"

fail() {
  local rule=$1
  local source=$2
  shift 2
  printf '[%s] %s\nsource: %s\ncommand: make project-check\n' "$rule" "$*" "$source" >&2
  exit 1
}

require_file() {
  local path=$1
  local rule=$2
  local source=$3
  [ -f "$path" ] || fail "$rule" "$source" "missing required file: $path"
}

require_text() {
  local path=$1
  local text=$2
  local rule=$3
  local source=$4
  grep -Fq -- "$text" "$path" || fail "$rule" "$source" "missing required contract in $path: $text"
}

require_pattern() {
  local path=$1
  local pattern=$2
  local rule=$3
  local source=$4
  grep -Eq -- "$pattern" "$path" || fail "$rule" "$source" "missing required pattern in $path: $pattern"
}

GO_MOD="$REPO_ROOT/go.mod"
PACKAGE_JSON="$REPO_ROOT/frontend/package.json"
PACKAGE_LOCK="$REPO_ROOT/frontend/package-lock.json"
MAKEFILE="$REPO_ROOT/Makefile"
ROOT_TASKFILE="$REPO_ROOT/Taskfile.yml"
DARWIN_TASKFILE="$REPO_ROOT/build/darwin/Taskfile.yml"
HARNESS_CHECK="$REPO_ROOT/scripts/harness/check.sh"
WORKFLOW="$REPO_ROOT/.github/workflows/ci.yml"
WORKFLOW_PERMISSIONS_CHECK="$SCRIPT_DIR/check_workflow_permissions.rb"

require_file "$GO_MOD" TOOLCHAIN-001 "docs/test/engineering-baseline/wails3-toolchain-capability-probe.md"
require_file "$PACKAGE_JSON" TOOLCHAIN-001 "docs/test/engineering-baseline/wails3-toolchain-capability-probe.md"
require_file "$PACKAGE_LOCK" TOOLCHAIN-001 "docs/test/engineering-baseline/wails3-toolchain-capability-probe.md"
require_file "$MAKEFILE" VERIFY-003 "docs/test/engineering-baseline/basic-ci-and-verification.md"
require_file "$ROOT_TASKFILE" RUNTIME-002 "docs/test/packaging/macos-arm64-bundle-signing.md"
require_file "$DARWIN_TASKFILE" RUNTIME-002 "docs/test/packaging/macos-arm64-bundle-signing.md"
require_file "$HARNESS_CHECK" CI-001 "docs/test/engineering-baseline/basic-ci-and-verification.md"
require_file "$WORKFLOW" CI-001 "docs/test/engineering-baseline/basic-ci-and-verification.md"
require_file "$WORKFLOW_PERMISSIONS_CHECK" CI-001 "docs/test/engineering-baseline/basic-ci-and-verification.md"

actual_os=$(uname -s)
actual_arch=$(uname -m)
[ "$actual_os" = Darwin ] || fail RUNTIME-002 "README.md" "verification requires macOS, got $actual_os"
[ "$actual_arch" = arm64 ] || fail RUNTIME-002 "README.md" "verification requires arm64, got $actual_arch"

go_version=$(awk '$1 == "go" { print $2; exit }' "$GO_MOD")
[ "$go_version" = "$EXPECTED_GO" ] || fail TOOLCHAIN-001 "$GO_MOD" "Go directive must be $EXPECTED_GO, got ${go_version:-missing}"

wails_module=$(awk '
  $1 == "require" && $2 == "github.com/wailsapp/wails/v3" { print $3; exit }
  $1 == "github.com/wailsapp/wails/v3" { print $2; exit }
' "$GO_MOD")
[ "$wails_module" = "$EXPECTED_WAILS" ] || fail TOOLCHAIN-001 "$GO_MOD" "Wails module must be $EXPECTED_WAILS, got ${wails_module:-missing}"

package_runtime=$(node -e 'const p=require(process.argv[1]); process.stdout.write(p.dependencies?.["@wailsio/runtime"] || "")' "$PACKAGE_JSON")
lock_root_runtime=$(node -e 'const p=require(process.argv[1]); process.stdout.write(p.packages?.[""]?.dependencies?.["@wailsio/runtime"] || "")' "$PACKAGE_LOCK")
lock_runtime=$(node -e 'const p=require(process.argv[1]); process.stdout.write(p.packages?.["node_modules/@wailsio/runtime"]?.version || "")' "$PACKAGE_LOCK")
node_engine=$(node -e 'const p=require(process.argv[1]); process.stdout.write(p.engines?.node || "")' "$PACKAGE_JSON")
npm_engine=$(node -e 'const p=require(process.argv[1]); process.stdout.write(p.engines?.npm || "")' "$PACKAGE_JSON")
lock_node_engine=$(node -e 'const p=require(process.argv[1]); process.stdout.write(p.packages?.[""]?.engines?.node || "")' "$PACKAGE_LOCK")
lock_npm_engine=$(node -e 'const p=require(process.argv[1]); process.stdout.write(p.packages?.[""]?.engines?.npm || "")' "$PACKAGE_LOCK")
jsdom_node_engine=$(node -e 'const p=require(process.argv[1]); process.stdout.write(p.packages?.["node_modules/jsdom"]?.engines?.node || "")' "$PACKAGE_LOCK")

[ "$package_runtime" = "$EXPECTED_RUNTIME" ] || fail TOOLCHAIN-001 "$PACKAGE_JSON" "@wailsio/runtime must be $EXPECTED_RUNTIME, got ${package_runtime:-missing}"
[ "$lock_root_runtime" = "$EXPECTED_RUNTIME" ] || fail TOOLCHAIN-001 "$PACKAGE_LOCK" "lockfile root runtime must be $EXPECTED_RUNTIME, got ${lock_root_runtime:-missing}"
[ "$lock_runtime" = "$EXPECTED_RUNTIME" ] || fail TOOLCHAIN-001 "$PACKAGE_LOCK" "locked runtime package must be $EXPECTED_RUNTIME, got ${lock_runtime:-missing}"
[ "$node_engine" = "$EXPECTED_NODE_ENGINE" ] || fail TOOLCHAIN-001 "$PACKAGE_JSON" "Node engine must be $EXPECTED_NODE_ENGINE, got ${node_engine:-missing}"
[ "$npm_engine" = "$EXPECTED_NPM_ENGINE" ] || fail TOOLCHAIN-001 "$PACKAGE_JSON" "npm engine must be $EXPECTED_NPM_ENGINE, got ${npm_engine:-missing}"
[ "$lock_node_engine" = "$EXPECTED_NODE_ENGINE" ] || fail TOOLCHAIN-001 "$PACKAGE_LOCK" "lockfile root Node engine must be $EXPECTED_NODE_ENGINE, got ${lock_node_engine:-missing}"
[ "$lock_npm_engine" = "$EXPECTED_NPM_ENGINE" ] || fail TOOLCHAIN-001 "$PACKAGE_LOCK" "lockfile root npm engine must be $EXPECTED_NPM_ENGINE, got ${lock_npm_engine:-missing}"
[ "$jsdom_node_engine" = "$EXPECTED_JSDOM_NODE_ENGINE" ] || fail TOOLCHAIN-001 "$PACKAGE_LOCK" "locked jsdom Node engine must be $EXPECTED_JSDOM_NODE_ENGINE, got ${jsdom_node_engine:-missing}"

if ! wails_output=$(wails3 version 2>&1); then
  fail TOOLCHAIN-001 "README.md" "cannot execute Wails CLI from PATH"
fi
if ! printf '%s\n' "$wails_output" | tr -d '\r' | grep -Fxq "$EXPECTED_WAILS"; then
  fail TOOLCHAIN-001 "README.md" "Wails CLI must report $EXPECTED_WAILS, got: $wails_output"
fi

require_text "$DARWIN_TASKFILE" 'MACOSX_DEPLOYMENT_TARGET: "15.0"' RUNTIME-002 "docs/test/packaging/macos-arm64-bundle-signing.md"
require_text "$DARWIN_TASKFILE" 'CODEX_PULSE_ARCH:' RUNTIME-002 "docs/test/packaging/macos-arm64-bundle-signing.md"
require_text "$DARWIN_TASKFILE" 'test "$CODEX_PULSE_ARCH" = arm64' RUNTIME-002 "docs/test/packaging/macos-arm64-bundle-signing.md"
require_text "$ROOT_TASKFILE" 'CODEX_PULSE_GOOS:' RUNTIME-002 "docs/test/packaging/macos-arm64-bundle-signing.md"

require_pattern "$MAKEFILE" '^verify:' VERIFY-003 "docs/test/engineering-baseline/basic-ci-and-verification.md"
require_pattern "$MAKEFILE" '^project-check:' VERIFY-003 "docs/test/engineering-baseline/basic-ci-and-verification.md"
require_pattern "$MAKEFILE" '^verify-go:' VERIFY-003 "docs/test/engineering-baseline/basic-ci-and-verification.md"
require_pattern "$MAKEFILE" '^verify-frontend:' VERIFY-003 "docs/test/engineering-baseline/basic-ci-and-verification.md"
require_pattern "$MAKEFILE" '^verify-package:' VERIFY-003 "docs/test/engineering-baseline/basic-ci-and-verification.md"
require_pattern "$MAKEFILE" '^verify-generated:' VERIFY-003 "docs/test/engineering-baseline/basic-ci-and-verification.md"

if ! awk '
  /^verify:/ { in_verify = 1; next }
  in_verify && /^[^\t]/ { exit }
  in_verify && /\$\(MAKE\) verify-generated/ { generated = NR }
  in_verify && /\$\(MAKE\) verify-package/ { package = NR }
  END { exit !(generated > 0 && package > 0 && generated < package) }
' "$MAKEFILE"; then
  fail VERIFY-003 "$MAKEFILE" "verify-generated must run before verify-package"
fi

require_pattern "$WORKFLOW" '^  pull_request:' CI-001 "docs/test/engineering-baseline/basic-ci-and-verification.md"
require_pattern "$WORKFLOW" '^      - main$' CI-001 "docs/test/engineering-baseline/basic-ci-and-verification.md"
require_pattern "$WORKFLOW" '^  contents: read$' CI-001 "docs/test/engineering-baseline/basic-ci-and-verification.md"
require_pattern "$WORKFLOW" '^    runs-on: macos-15$' CI-001 "docs/test/engineering-baseline/basic-ci-and-verification.md"
require_pattern "$WORKFLOW" '^[[:space:]]+uses: actions/checkout@df4cb1c069e1874edd31b4311f1884172cec0e10 # v6$' CI-001 "docs/test/engineering-baseline/basic-ci-and-verification.md"
require_pattern "$WORKFLOW" '^          persist-credentials: false$' CI-001 "docs/test/engineering-baseline/basic-ci-and-verification.md"
require_pattern "$WORKFLOW" '^[[:space:]]+uses: actions/setup-go@924ae3a1cded613372ab5595356fb5720e22ba16 # v6$' CI-001 "docs/test/engineering-baseline/basic-ci-and-verification.md"
require_pattern "$WORKFLOW" '^          go-version-file: go.mod$' CI-001 "docs/test/engineering-baseline/basic-ci-and-verification.md"
require_pattern "$WORKFLOW" '^          cache: false$' CI-001 "docs/test/engineering-baseline/basic-ci-and-verification.md"
require_pattern "$WORKFLOW" '^[[:space:]]+uses: actions/setup-node@48b55a011bda9f5d6aeb4c2d9c7362e8dae4041e # v6$' CI-001 "docs/test/engineering-baseline/basic-ci-and-verification.md"
require_pattern "$WORKFLOW" "^          node-version: '$EXPECTED_NODE_VERSION'$" CI-001 "docs/test/engineering-baseline/basic-ci-and-verification.md"
require_pattern "$WORKFLOW" '^          package-manager-cache: false$' CI-001 "docs/test/engineering-baseline/basic-ci-and-verification.md"
require_pattern "$WORKFLOW" '^[[:space:]]+GOBIN="\$RUNNER_TEMP/bin" go install github.com/wailsapp/wails/v3/cmd/wails3@v3.0.0-alpha2.117$' CI-001 "docs/test/engineering-baseline/basic-ci-and-verification.md"
require_pattern "$WORKFLOW" '^        working-directory: frontend$' CI-001 "docs/test/engineering-baseline/basic-ci-and-verification.md"
require_pattern "$WORKFLOW" '^        run: npm ci$' CI-001 "docs/test/engineering-baseline/basic-ci-and-verification.md"
require_pattern "$WORKFLOW" '^        run: make verify$' CI-001 "docs/test/engineering-baseline/basic-ci-and-verification.md"
require_pattern "$WORKFLOW" '^[[:space:]]+git diff --exit-code$' CI-001 "docs/test/engineering-baseline/basic-ci-and-verification.md"
require_pattern "$WORKFLOW" '^[[:space:]]+git diff --cached --exit-code$' CI-001 "docs/test/engineering-baseline/basic-ci-and-verification.md"
require_pattern "$WORKFLOW" '^[[:space:]]+status=\$\(git status --porcelain --untracked-files=all\)$' CI-001 "docs/test/engineering-baseline/basic-ci-and-verification.md"

if grep -Eq '(^|[^[:alnum:]_])rg([^[:alnum:]_]|$)' "$HARNESS_CHECK"; then
  fail CI-001 "$HARNESS_CHECK" "base harness source must not contain the standalone ripgrep command token"
fi

if ! permissions_output=$(ruby "$WORKFLOW_PERMISSIONS_CHECK" "$WORKFLOW" 2>&1); then
  fail CI-001 "$WORKFLOW" "${permissions_output:-workflow permissions structure check failed}"
fi

if grep -Ein '(^|[[:space:]])(write-all|read-all)([[:space:]#]|$)|^[[:space:]]*[[:alnum:]_-]+:[[:space:]]*write([[:space:]#]|$)' "$WORKFLOW" >/dev/null; then
  fail CI-001 "$WORKFLOW" "workflow must not request broad or write permissions"
fi
if grep -Ein 'secrets[[:space:]]*(\.|\[)|github[[:space:]]*(\.token|\[[^]]*token[^]]*\])|GITHUB_TOKEN|ACTIONS_ID_TOKEN_REQUEST|id-token:|toJSON[[:space:]]*\([[:space:]]*github[[:space:]]*\)' "$WORKFLOW" >/dev/null; then
  fail CI-001 "$WORKFLOW" "workflow must not reference secrets, tokens, or OIDC permissions"
fi
if grep -Ein 'gh[[:space:]]+release|git[[:space:]]+tag|notarytool|appcast|sparkle|(^|[[:space:]/_-])deploy([[:space:]/_-]|$)|uses:[[:space:]].*release' "$WORKFLOW" >/dev/null; then
  fail CI-001 "$WORKFLOW" "verification workflow must not publish, sign, notarize, or deploy releases"
fi
if grep -En 'macos-latest|uses:[[:space:]]+actions/[^@]+@(v[0-9]+|master|main|latest|nightly)([[:space:]#]|$)' "$WORKFLOW" >/dev/null; then
  fail CI-001 "$WORKFLOW" "workflow runner and actions must not use floating labels"
fi

printf 'project checks passed (RUNTIME-002, TOOLCHAIN-001, VERIFY-003, CI-001)\n'
