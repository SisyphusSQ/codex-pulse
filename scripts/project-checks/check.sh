#!/usr/bin/env bash

set -euo pipefail

SCRIPT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
REPO_ROOT=$(CDPATH= cd -- "$SCRIPT_DIR/../.." && pwd)

fail() {
  local rule=$1
  local source=$2
  shift 2
  printf '[%s] %s\nsource: %s\ncommand: make verify-architecture\n' "$rule" "$*" "$source" >&2
  exit 1
}

require_file() {
  [ -f "$REPO_ROOT/$1" ] || fail "$2" "$3" "missing required file: $1"
}

require_pattern() {
  grep -Eq -- "$2" "$REPO_ROOT/$1" || fail "$3" "$4" "missing contract in $1: $2"
}

reject_pattern() {
  ! grep -Eq -- "$2" "$REPO_ROOT/$1" || fail "$3" "$4" "forbidden contract in $1: $2"
}

require_file go.mod TOOLCHAIN-001 go.mod
require_file api/codexpulse/core/v1/core.proto RPC-001 docs/design/details/architecture/README.md
require_file api/codexpulse/core/v1/core.pb.go RPC-001 api/codexpulse/core/v1/core.proto
require_file api/codexpulse/core/v1/core_grpc.pb.go RPC-001 api/codexpulse/core/v1/core.proto
require_file internal/helper/runtime.go RPC-002 docs/design/details/architecture/README.md
require_file scripts/proto/generate.sh VERIFY-003 Makefile
require_file scripts/proto/generate-swift.sh SWIFT-001 docs/design/details/native-macos-client/README.md
require_file app/macos/Package.swift SWIFT-001 docs/design/details/native-macos-client/README.md
require_file app/macos/Package.resolved SWIFT-001 app/macos/Package.swift
require_file app/macos/Sources/CodexPulseProtocolGenerated/core.pb.swift SWIFT-001 api/codexpulse/core/v1/core.proto
require_file app/macos/Sources/CodexPulseProtocolGenerated/core.grpc.swift SWIFT-001 api/codexpulse/core/v1/core.proto
require_file app/macos/Sources/CodexPulseCoreClient/CoreClient.swift SWIFT-001 docs/design/details/native-macos-client/README.md
require_file app/macos/Sources/CodexPulseCoreClient/HelperSupervisor.swift SWIFT-001 docs/design/details/native-macos-client/README.md
require_file app/macos/Sources/CodexPulseCoreClient/InvalidationStreamController.swift SWIFT-001 docs/design/details/native-macos-client/README.md
require_file app/macos/Sources/CodexPulseCoreClient/ReadRetryPolicy.swift SWIFT-001 docs/design/details/native-macos-client/README.md
require_file scripts/swift-cancel-probe/main.go SWIFT-001 docs/test/swift-transport-spike.md
require_file app/macos/Sources/CodexPulseApp/AppMain.swift SWIFT-002 docs/design/details/native-macos-client/README.md
require_file app/macos/Sources/CodexPulseApp/AppDelegate.swift SWIFT-002 docs/design/details/native-macos-client/README.md
require_file app/macos/Sources/CodexPulseApp/RootView.swift SWIFT-002 docs/design/details/native-macos-client/README.md
require_file app/macos/Sources/CodexPulseApp/StatusItemController.swift SWIFT-002 docs/design/details/native-macos-client/README.md
require_file app/macos/Sources/CodexPulseAppSupport/AppRuntime.swift SWIFT-002 docs/design/details/native-macos-client/README.md
require_file app/macos/Sources/CodexPulseAppSupport/HelperProcessMonitor.swift SWIFT-002 docs/design/details/native-macos-client/README.md
require_file app/macos/Sources/CodexPulseAppSupport/OverviewModels.swift SWIFT-002 docs/design/details/native-macos-client/README.md
require_file app/macos/Sources/CodexPulseAppSupport/FeatureModels.swift SWIFT-003 docs/design/details/native-macos-client/README.md
require_file app/macos/Sources/CodexPulseAppSupport/FeatureRequests.swift SWIFT-003 docs/design/details/native-macos-client/README.md
require_file app/macos/Sources/CodexPulseApp/SessionsProjectsViews.swift SWIFT-003 docs/design/details/native-macos-client/README.md
require_file app/macos/Sources/CodexPulseApp/QuotaHealthViews.swift SWIFT-003 docs/design/details/native-macos-client/README.md
require_file app/macos/Sources/CodexPulseApp/SourcesJobsSettingsViews.swift SWIFT-003 docs/design/details/native-macos-client/README.md
require_file internal/codex/appserver/process.go DATA-001 docs/design/details/native-macos-client/README.md
require_file scripts/macos/build-dev-app.sh SWIFT-002 docs/test/native-app-shell-overview.md
require_file scripts/macos/build-release-app.sh RELEASE-001 .agents/skills/project-version-release/references/codex-pulse-release-policy.md
require_file scripts/macos/run-app-smoke.sh SWIFT-002 docs/test/native-app-shell-overview.md
require_file scripts/macos/run-app-live-smoke.sh SWIFT-004 docs/test/native-primary-pages.md
require_file scripts/macos/smoke-seed/main.go DATA-001 docs/test/native-app-shell-overview.md
require_file scripts/macos/Info.plist SWIFT-002 docs/test/native-app-shell-overview.md
require_file .github/workflows/ci.yml CI-001 docs/test/engineering-baseline/basic-ci-and-verification.md

go_version=$(awk '$1 == "go" { print $2; exit }' "$REPO_ROOT/go.mod")
[ "$go_version" = "1.25.0" ] || fail TOOLCHAIN-001 go.mod "Go directive must be 1.25.0"
grep -Fq 'google.golang.org/grpc v1.82.1' "$REPO_ROOT/go.mod" || fail TOOLCHAIN-001 go.mod "grpc-go must be v1.82.1"
grep -Fq 'google.golang.org/protobuf v1.36.11' "$REPO_ROOT/go.mod" || fail TOOLCHAIN-001 go.mod "protobuf-go must be v1.36.11"

[ ! -e "$REPO_ROOT/frontend/package.json" ] || fail ARCH-001 AGENTS.md "frontend manifest returned"
for removed in internal/updater internal/platform/tray internal/singleinstance scripts/sparkle build cmd/trayprobe cmd/traystatusprobe; do
  if [ -d "$REPO_ROOT/$removed" ] && find "$REPO_ROOT/$removed" -type f \( -name '*.go' -o -name '*.sh' -o -name '*.yml' -o -name '*.yaml' \) -print -quit | grep -q .; then
    fail ARCH-001 AGENTS.md "removed desktop source returned: $removed"
  fi
done

if grep -R -E 'github.com/wailsapp|@wailsio|Sparkle|sparkle|AppKit' \
  "$REPO_ROOT/go.mod" "$REPO_ROOT/main.go" "$REPO_ROOT/internal" \
  --include='*.go' --include='*.sh' >/dev/null 2>&1; then
  fail ARCH-001 AGENTS.md "Wails/AppKit/Sparkle dependency returned to Helper source"
fi

require_pattern Makefile '^verify-architecture:' VERIFY-003 Makefile
require_pattern Makefile '^verify-proto:' VERIFY-003 Makefile
require_pattern Makefile '^verify-helper:' VERIFY-003 Makefile
require_pattern Makefile 'APP_VERSION[[:space:]]*[?]?=' RELEASE-001 .agents/skills/project-version-release/references/codex-pulse-release-policy.md
require_pattern Makefile '^verify-go:' VERIFY-003 Makefile
require_pattern Makefile '^verify-swift-transport:' SWIFT-001 Makefile
require_pattern Makefile '^verify-swift-app:' SWIFT-002 Makefile
require_pattern Makefile '^verify-swift-app-smoke-isolated:' SWIFT-002 Makefile
require_pattern Makefile '^verify-swift-app-live:' SWIFT-004 Makefile
require_pattern Makefile '^verify-swift-app-smoke:' SWIFT-002 Makefile
require_pattern Makefile '^verify-swift-primary-pages:' SWIFT-003 Makefile
require_pattern main.go 'parseRuntimeConfig' RPC-002 main.go
require_pattern main.go 'signal.NotifyContext' RPC-002 main.go
require_pattern internal/helper/runtime.go 'ListenUnix' RPC-002 internal/helper/runtime.go
require_pattern internal/helper/runtime.go 'readAuthPipe' RPC-002 internal/helper/runtime.go
require_pattern internal/helper/server.go 'ChainUnaryInterceptor' RPC-002 internal/helper/server.go
require_pattern internal/helper/server.go 'ChainStreamInterceptor' RPC-002 internal/helper/server.go

grep -Fq 'exact: "2.4.2"' "$REPO_ROOT/app/macos/Package.swift" || fail SWIFT-001 app/macos/Package.swift "grpc-swift-2 must be pinned to 2.4.2"
grep -Fq 'exact: "2.9.0"' "$REPO_ROOT/app/macos/Package.swift" || fail SWIFT-001 app/macos/Package.swift "grpc-swift-nio-transport must be pinned to 2.9.0"
grep -Fq 'exact: "2.4.1"' "$REPO_ROOT/app/macos/Package.swift" || fail SWIFT-001 app/macos/Package.swift "grpc-swift-protobuf must be pinned to 2.4.1"
grep -Fq 'exact: "1.38.1"' "$REPO_ROOT/app/macos/Package.swift" || fail SWIFT-001 app/macos/Package.swift "swift-protobuf must be pinned to 1.38.1"
require_pattern app/macos/Sources/CodexPulseCoreClient/CoreClient.swift 'unixDomainSocket' SWIFT-001 docs/design/details/native-macos-client/README.md
require_pattern app/macos/Sources/CodexPulseCoreClient/HelperSupervisor.swift 'posix_spawn' SWIFT-001 docs/design/details/native-macos-client/README.md
require_pattern app/macos/Sources/CodexPulseCoreClient/HelperSupervisor.swift 'POSIX_SPAWN_CLOEXEC_DEFAULT' SWIFT-001 docs/design/details/native-macos-client/README.md
require_pattern app/macos/Sources/CodexPulseCoreClient/HelperSupervisor.swift 'validatedSocketIdentity' SWIFT-001 docs/design/details/native-macos-client/README.md
require_pattern scripts/macos/build-release-app.sh 'CFBundleShortVersionString' RELEASE-001 .agents/skills/project-version-release/references/codex-pulse-release-policy.md
require_pattern scripts/macos/build-release-app.sh 'main.applicationVersion=' RELEASE-001 .agents/skills/project-version-release/references/codex-pulse-release-policy.md
require_pattern scripts/macos/build-release-app.sh 'Codex-Pulse-.*-macos-arm64.zip' RELEASE-001 .agents/skills/project-version-release/references/codex-pulse-release-policy.md
reject_pattern scripts/macos/build-release-app.sh '(^|[[:space:]])-gnone([[:space:]]|$)' RELEASE-001 .agents/skills/project-version-release/references/codex-pulse-release-policy.md
require_pattern scripts/macos/build-release-app.sh '--scratch-path' RELEASE-001 .agents/skills/project-version-release/references/codex-pulse-release-policy.md
require_pattern scripts/macos/build-release-app.sh 'gline-tables-only' RELEASE-001 .agents/skills/project-version-release/references/codex-pulse-release-policy.md
require_pattern scripts/macos/build-release-app.sh 'ffile-prefix-map' RELEASE-001 .agents/skills/project-version-release/references/codex-pulse-release-policy.md
require_pattern scripts/macos/build-release-app.sh 'fmacro-prefix-map' RELEASE-001 .agents/skills/project-version-release/references/codex-pulse-release-policy.md
require_pattern scripts/macos/build-release-app.sh 'codex-pulse-app-tests' RELEASE-001 .agents/skills/project-version-release/references/codex-pulse-release-policy.md
require_pattern scripts/macos/build-release-app.sh 'strip -S' RELEASE-001 .agents/skills/project-version-release/references/codex-pulse-release-policy.md
require_pattern scripts/macos/build-release-app.sh 'release binaries contain a local absolute path' RELEASE-001 .agents/skills/project-version-release/references/codex-pulse-release-policy.md
require_pattern scripts/macos/build-release-app.sh 'codesign --force --sign -' RELEASE-001 .agents/skills/project-version-release/references/codex-pulse-release-policy.md
require_pattern scripts/macos/build-release-app.sh 'codesign --verify --deep --strict' RELEASE-001 .agents/skills/project-version-release/references/codex-pulse-release-policy.md
require_pattern scripts/macos/build-release-app.sh 'Signature=adhoc' RELEASE-001 .agents/skills/project-version-release/references/codex-pulse-release-policy.md
require_pattern app/macos/Sources/CodexPulseCoreClient/InvalidationStreamController.swift 'streamGeneration' SWIFT-001 docs/design/details/native-macos-client/README.md
require_pattern app/macos/Sources/CodexPulseCoreClient/ReadRetryPolicy.swift 'error.code == .unavailable' SWIFT-001 docs/design/details/native-macos-client/README.md
require_pattern Makefile 'CODEX_PULSE_CANCEL_PROBE' SWIFT-001 docs/test/swift-transport-spike.md
require_pattern app/macos/Sources/CodexPulseApp/AppMain.swift 'NSApplication.shared' SWIFT-002 docs/design/details/native-macos-client/README.md
require_pattern app/macos/Sources/CodexPulseApp/AppDelegate.swift 'NSWindow' SWIFT-002 docs/design/details/native-macos-client/README.md
require_pattern app/macos/Sources/CodexPulseApp/AppDelegate.swift 'applicationWillResignActive' SWIFT-002 docs/design/details/native-macos-client/README.md
require_pattern app/macos/Sources/CodexPulseApp/RootView.swift 'NavigationSplitView' SWIFT-002 docs/design/details/native-macos-client/README.md
require_pattern app/macos/Sources/CodexPulseApp/StatusItemController.swift 'NSStatusBar.system.statusItem' SWIFT-002 docs/design/details/native-macos-client/README.md
require_pattern app/macos/Sources/CodexPulseApp/StatusItemController.swift 'minimumHitTarget: CGFloat = 44' SWIFT-002 docs/design/details/native-macos-client/README.md
require_pattern app/macos/Sources/CodexPulseApp/StatusItemController.swift 'iconVisualSize: CGFloat = 28' SWIFT-002 docs/design/details/native-macos-client/README.md
require_pattern app/macos/Sources/CodexPulseApp/StatusItemController.swift 'compactButtonVisualHeight: CGFloat = 28' SWIFT-002 docs/design/details/native-macos-client/README.md
require_pattern app/macos/Sources/CodexPulseApp/StatusItemController.swift 'padding\(\.vertical, -PopoverInteractionMetrics\.compactButtonHitSlop\)' SWIFT-002 docs/design/details/native-macos-client/README.md
require_pattern app/macos/Sources/CodexPulseApp/StatusItemController.swift 'InteractiveCardButtonStyle' SWIFT-002 docs/design/details/native-macos-client/README.md
require_pattern app/macos/Sources/CodexPulseApp/StatusItemController.swift '\.contentShape\(' SWIFT-002 docs/design/details/native-macos-client/README.md
reject_pattern app/macos/Sources/CodexPulseApp/StatusItemController.swift '\.onHover' SWIFT-002 docs/design/details/native-macos-client/README.md
reject_pattern app/macos/Sources/CodexPulseApp/StatusItemController.swift '\.accentColor\.opacity' SWIFT-002 docs/design/details/native-macos-client/README.md
require_pattern app/macos/Sources/CodexPulseApp/StatusItemController.swift 'controlActiveState' SWIFT-002 docs/design/details/native-macos-client/README.md
require_pattern app/macos/Sources/CodexPulseApp/StatusItemController.swift 'model\.isOverviewRefreshing' SWIFT-002 docs/design/details/native-macos-client/README.md
require_pattern app/macos/Sources/CodexPulseApp/StatusItemController.swift '正在刷新本地数据' SWIFT-002 docs/design/details/native-macos-client/README.md
require_pattern app/macos/Sources/CodexPulseApp/StatusItemController.swift 'private struct RefreshArrowSymbol' SWIFT-002 docs/design/details/native-macos-client/README.md
require_pattern app/macos/Sources/CodexPulseApp/StatusItemController.swift 'TimelineView\(\.animation' SWIFT-002 docs/design/details/native-macos-client/README.md
require_pattern app/macos/Sources/CodexPulseApp/StatusItemController.swift 'private struct PopoverBackButton' SWIFT-002 docs/design/details/native-macos-client/README.md
reject_pattern app/macos/Sources/CodexPulseApp/StatusItemController.swift 'private struct DetailHeader' SWIFT-002 docs/design/details/native-macos-client/README.md
reject_pattern app/macos/Sources/CodexPulseApp/StatusItemController.swift '重置次数详情' SWIFT-002 docs/design/details/native-macos-client/README.md
require_pattern app/macos/Sources/CodexPulseAppSupport/AppRuntime.swift 'OverviewRequestSet.make' SWIFT-002 api/codexpulse/core/v1/core.proto
require_pattern app/macos/Sources/CodexPulseAppSupport/AppModel.swift 'featureGenerations' SWIFT-003 docs/design/details/native-macos-client/README.md
require_pattern app/macos/Sources/CodexPulseAppSupport/FeatureModels.swift '^public enum RuntimeControlAction:' SWIFT-003 api/codexpulse/core/v1/core.proto
require_pattern app/macos/Sources/CodexPulseAppSupport/FeatureModels.swift 'expectedRevision' SWIFT-003 api/codexpulse/core/v1/core.proto
require_pattern app/macos/Sources/CodexPulseAppSupport/FeatureRequests.swift 'min\(max\(limit, 1\), 100\)' SWIFT-003 api/codexpulse/core/v1/core.proto
require_pattern app/macos/Sources/CodexPulseCoreClient/CoreClient.swift 'service.runRuntimeAction' SWIFT-003 api/codexpulse/core/v1/core.proto
require_pattern app/macos/Sources/CodexPulseAppSupport/HelperProcessMonitor.swift 'DispatchSource.makeProcessSource' SWIFT-002 docs/design/details/native-macos-client/README.md
require_pattern internal/codex/appserver/process.go 'command.Env = isolatedCodexEnvironment' DATA-001 docs/design/details/native-macos-client/README.md
require_pattern scripts/macos/build-dev-app.sh 'Contents/Helpers' SWIFT-002 docs/design/details/native-macos-client/README.md
require_pattern scripts/macos/run-app-smoke.sh '--ui-smoke' SWIFT-002 docs/test/native-app-shell-overview.md
require_pattern scripts/macos/run-app-smoke.sh '--skip-live-lifecycle' SWIFT-002 docs/test/native-app-shell-overview.md
require_pattern scripts/macos/run-app-smoke.sh 'app smoke failed: timeout' SWIFT-002 docs/test/native-app-shell-overview.md
require_pattern scripts/macos/run-app-smoke.sh 'isolated empty Home produced unexpected user facts' DATA-001 docs/test/native-app-shell-overview.md
require_pattern scripts/macos/run-app-smoke.sh 'primary_pages=partial' SWIFT-003 docs/test/native-primary-pages.md
require_pattern scripts/macos/run-app-smoke.sh 'ui_pages=7' SWIFT-003 docs/test/native-primary-pages.md
require_pattern scripts/macos/run-app-live-smoke.sh 'CODEX_PULSE_APP_RUNTIME' SWIFT-004 docs/test/native-primary-pages.md
require_pattern scripts/macos/run-app-live-smoke.sh 'confirmed Home is not the real Codex Home' SWIFT-004 docs/test/native-primary-pages.md
require_pattern scripts/macos/run-app-live-smoke.sh 'standard_housekeeping=allowed' SWIFT-004 docs/test/native-primary-pages.md
require_pattern scripts/macos/run-app-live-smoke.sh 'primary_pages=loaded' SWIFT-004 docs/test/native-primary-pages.md
require_pattern scripts/macos/run-app-live-smoke.sh 'unavailable=none ui_pages=7' SWIFT-004 docs/test/native-primary-pages.md
if grep -Eq 'mktemp -d' "$REPO_ROOT/scripts/macos/run-app-live-smoke.sh"; then
  fail SWIFT-004 AGENTS.md "real Home live smoke must reuse an existing runtime"
fi

if grep -R -E 'sqlite3_open|SQLite\.open|\.jsonl(["'"'"']|$)|127\.0\.0\.1|localhost|NWListener|ServerBootstrap' \
  "$REPO_ROOT/app/macos/Sources/CodexPulseApp" \
  "$REPO_ROOT/app/macos/Sources/CodexPulseAppSupport" \
  --include='*.swift' >/dev/null 2>&1; then
  fail SWIFT-002 AGENTS.md "Swift App introduced direct data access or TCP listener"
fi

WORKFLOW="$REPO_ROOT/.github/workflows/ci.yml"
grep -Eq '^  contents: read$' "$WORKFLOW" || fail CI-001 "$WORKFLOW" "workflow must be read-only"
grep -Eq '^    runs-on: macos-15$' "$WORKFLOW" || fail CI-001 "$WORKFLOW" "workflow runner must be macos-15"
grep -Eq '^        run: make verify$' "$WORKFLOW" || fail CI-001 "$WORKFLOW" "workflow must run make verify"
if grep -Ein 'setup-node|npm |wails|sparkle|notarytool|gh release|git tag|contents: write|write-all|secrets\.|github\.token|GITHUB_TOKEN' "$WORKFLOW" >/dev/null; then
  fail CI-001 "$WORKFLOW" "workflow contains removed UI tooling or privileged/publishing behavior"
fi

printf 'architecture checks passed (ARCH-001, RPC-001, RPC-002, DATA-001, SWIFT-001, SWIFT-002, SWIFT-003, SWIFT-004, TOOLCHAIN-001, VERIFY-003, CI-001)\n'
