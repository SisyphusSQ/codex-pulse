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

copy_fixture
sed 's/exact: "2.4.2"/exact: "2.4.0"/' \
  "$TMP_ROOT/repo/app/macos/Package.swift" >"$TMP_ROOT/Package.swift"
mv "$TMP_ROOT/Package.swift" "$TMP_ROOT/repo/app/macos/Package.swift"
assert_failure SWIFT-001

copy_fixture
sed 's/POSIX_SPAWN_CLOEXEC_DEFAULT/POSIX_SPAWN_USEVFORK/' \
  "$TMP_ROOT/repo/app/macos/Sources/CodexPulseCoreClient/HelperSupervisor.swift" >"$TMP_ROOT/HelperSupervisor.swift"
mv "$TMP_ROOT/HelperSupervisor.swift" \
  "$TMP_ROOT/repo/app/macos/Sources/CodexPulseCoreClient/HelperSupervisor.swift"
assert_failure SWIFT-001

copy_fixture
sed 's/streamGeneration/staleGeneration/g' \
  "$TMP_ROOT/repo/app/macos/Sources/CodexPulseCoreClient/InvalidationStreamController.swift" >"$TMP_ROOT/InvalidationStreamController.swift"
mv "$TMP_ROOT/InvalidationStreamController.swift" \
  "$TMP_ROOT/repo/app/macos/Sources/CodexPulseCoreClient/InvalidationStreamController.swift"
assert_failure SWIFT-001

copy_fixture
sed 's/error.code == \.unavailable/error.code == .invalidArgument/' \
  "$TMP_ROOT/repo/app/macos/Sources/CodexPulseCoreClient/ReadRetryPolicy.swift" >"$TMP_ROOT/ReadRetryPolicy.swift"
mv "$TMP_ROOT/ReadRetryPolicy.swift" \
  "$TMP_ROOT/repo/app/macos/Sources/CodexPulseCoreClient/ReadRetryPolicy.swift"
assert_failure SWIFT-001

copy_fixture
sed 's/NSApplication\.shared/NSApplication()/' \
  "$TMP_ROOT/repo/app/macos/Sources/CodexPulseApp/AppMain.swift" >"$TMP_ROOT/AppMain.swift"
mv "$TMP_ROOT/AppMain.swift" "$TMP_ROOT/repo/app/macos/Sources/CodexPulseApp/AppMain.swift"
assert_failure SWIFT-002

copy_fixture
printf '\n// sqlite3_open must never enter Swift App source\n' \
  >>"$TMP_ROOT/repo/app/macos/Sources/CodexPulseAppSupport/AppRuntime.swift"
assert_failure SWIFT-002

copy_fixture
sed 's/RuntimeControlAction/UnsafeRuntimeControlAction/g' \
  "$TMP_ROOT/repo/app/macos/Sources/CodexPulseAppSupport/FeatureModels.swift" >"$TMP_ROOT/FeatureModels.swift"
mv "$TMP_ROOT/FeatureModels.swift" \
  "$TMP_ROOT/repo/app/macos/Sources/CodexPulseAppSupport/FeatureModels.swift"
assert_failure SWIFT-003

copy_fixture
sed 's/standard_housekeeping=allowed/standard_housekeeping=blocked/' \
  "$TMP_ROOT/repo/scripts/macos/run-app-live-smoke.sh" >"$TMP_ROOT/run-app-live-smoke.sh"
mv "$TMP_ROOT/run-app-live-smoke.sh" "$TMP_ROOT/repo/scripts/macos/run-app-live-smoke.sh"
assert_failure SWIFT-004

copy_fixture
sed 's/command.Env = isolatedCodexEnvironment(os.Environ(), canonicalHome)/command.Env = os.Environ()/' \
  "$TMP_ROOT/repo/internal/codex/appserver/process.go" >"$TMP_ROOT/process.go"
mv "$TMP_ROOT/process.go" "$TMP_ROOT/repo/internal/codex/appserver/process.go"
assert_failure DATA-001

printf 'project-check contract tests passed\n'
