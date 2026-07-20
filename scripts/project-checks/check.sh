#!/usr/bin/env bash

set -euo pipefail

SCRIPT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
REPO_ROOT=$(CDPATH= cd -- "$SCRIPT_DIR/../.." && pwd)

fail() {
  local rule=$1
  local source=$2
  shift 2
  printf '[%s] %s\nsource: %s\ncommand: make project-check\n' "$rule" "$*" "$source" >&2
  exit 1
}

require_file() {
  [ -f "$REPO_ROOT/$1" ] || fail "$2" "$3" "missing required file: $1"
}

require_pattern() {
  grep -Eq -- "$2" "$REPO_ROOT/$1" || fail "$3" "$4" "missing contract in $1: $2"
}

require_file go.mod TOOLCHAIN-001 go.mod
require_file api/codexpulse/core/v1/core.proto RPC-001 docs/design/details/architecture/README.md
require_file api/codexpulse/core/v1/core.pb.go RPC-001 api/codexpulse/core/v1/core.proto
require_file api/codexpulse/core/v1/core_grpc.pb.go RPC-001 api/codexpulse/core/v1/core.proto
require_file internal/helper/runtime.go RPC-002 docs/design/details/architecture/README.md
require_file scripts/proto/generate.sh VERIFY-003 Makefile
require_file .github/workflows/ci.yml CI-001 docs/test/engineering-baseline/basic-ci-and-verification.md

go_version=$(awk '$1 == "go" { print $2; exit }' "$REPO_ROOT/go.mod")
[ "$go_version" = "1.25.0" ] || fail TOOLCHAIN-001 go.mod "Go directive must be 1.25.0"
grep -Fq 'google.golang.org/grpc v1.82.1' "$REPO_ROOT/go.mod" || fail TOOLCHAIN-001 go.mod "grpc-go must be v1.82.1"
grep -Fq 'google.golang.org/protobuf v1.36.11' "$REPO_ROOT/go.mod" || fail TOOLCHAIN-001 go.mod "protobuf-go must be v1.36.11"

[ ! -e "$REPO_ROOT/frontend/package.json" ] || fail ARCH-001 docs/harness/project-constraints.md "frontend manifest returned"
for removed in internal/updater internal/platform/tray internal/singleinstance scripts/sparkle build cmd/trayprobe cmd/traystatusprobe; do
  if [ -d "$REPO_ROOT/$removed" ] && find "$REPO_ROOT/$removed" -type f \( -name '*.go' -o -name '*.sh' -o -name '*.yml' -o -name '*.yaml' \) -print -quit | grep -q .; then
    fail ARCH-001 docs/harness/project-constraints.md "removed desktop source returned: $removed"
  fi
done

if grep -R -E 'github.com/wailsapp|@wailsio|Sparkle|sparkle|AppKit' \
  "$REPO_ROOT/go.mod" "$REPO_ROOT/main.go" "$REPO_ROOT/internal" \
  --include='*.go' --include='*.sh' >/dev/null 2>&1; then
  fail ARCH-001 docs/harness/project-constraints.md "Wails/AppKit/Sparkle dependency returned to Helper source"
fi

require_pattern Makefile '^verify-proto:' VERIFY-003 Makefile
require_pattern Makefile '^verify-helper:' VERIFY-003 Makefile
require_pattern Makefile '^verify-go:' VERIFY-003 Makefile
require_pattern main.go 'parseRuntimeConfig' RPC-002 main.go
require_pattern main.go 'signal.NotifyContext' RPC-002 main.go
require_pattern internal/helper/runtime.go 'ListenUnix' RPC-002 internal/helper/runtime.go
require_pattern internal/helper/runtime.go 'readAuthPipe' RPC-002 internal/helper/runtime.go
require_pattern internal/helper/server.go 'ChainUnaryInterceptor' RPC-002 internal/helper/server.go
require_pattern internal/helper/server.go 'ChainStreamInterceptor' RPC-002 internal/helper/server.go

WORKFLOW="$REPO_ROOT/.github/workflows/ci.yml"
grep -Eq '^  contents: read$' "$WORKFLOW" || fail CI-001 "$WORKFLOW" "workflow must be read-only"
grep -Eq '^    runs-on: macos-15$' "$WORKFLOW" || fail CI-001 "$WORKFLOW" "workflow runner must be macos-15"
grep -Eq '^        run: make verify$' "$WORKFLOW" || fail CI-001 "$WORKFLOW" "workflow must run make verify"
if grep -Ein 'setup-node|npm |wails|sparkle|notarytool|gh release|git tag|contents: write|write-all|secrets\.|github\.token|GITHUB_TOKEN' "$WORKFLOW" >/dev/null; then
  fail CI-001 "$WORKFLOW" "workflow contains removed UI tooling or privileged/publishing behavior"
fi

printf 'project checks passed (ARCH-001, RPC-001, RPC-002, TOOLCHAIN-001, VERIFY-003, CI-001)\n'
