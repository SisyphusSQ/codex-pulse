#!/usr/bin/env bash

set -euo pipefail

repo_root=$(CDPATH= cd -- "$(dirname -- "$0")/../.." && pwd)
cd "$repo_root"

tool_root=$(mktemp -d "${TMPDIR:-/tmp}/codex-pulse-m11-privacy-tools.XXXXXX")
package_lock="bin/.m11-privacy-audit.lock"
package_token="$(basename "$tool_root")"
package_started="$tool_root/package-started"

export GOTOOLCHAIN=local
export GOPROXY=off
export MACOSX_DEPLOYMENT_TARGET="${MACOSX_DEPLOYMENT_TARGET:-15.0}"

case "$MACOSX_DEPLOYMENT_TARGET" in
  15|15.*) ;;
  *)
    printf '%s\n' 'M11-PRIV-002: deployment target must be 15.x' >&2
    exit 1
    ;;
esac

cleanup() {
	if [[ -f "$package_lock/owner" ]] && [[ "$(cat "$package_lock/owner")" == "$package_token" ]]; then
	  if [[ -x "$tool_root/wails3" && -f "$package_started" ]]; then
	    "$tool_root/wails3" task darwin:package:clean >/dev/null 2>&1 || true
	    rm -f "bin/codex-pulse"
	  fi
	  rm -f "$package_lock/owner"
	  rmdir "$package_lock" >/dev/null 2>&1 || true
	fi
	rm -rf "$tool_root"
}
trap cleanup EXIT INT TERM

if [[ -e "bin/codex-pulse" || -e "bin/Codex Pulse.app" || -e "bin/Codex Pulse.app.zip" || -e "bin/.packaging" || -e "$package_lock" ]]; then
  printf '%s\n' 'M11-PRIV-004: package output is not empty or is already leased' >&2
  exit 1
fi
mkdir -p bin
if ! mkdir "$package_lock"; then
  printf '%s\n' 'M11-PRIV-004: package output is already leased' >&2
  exit 1
fi
if ! printf '%s' "$package_token" >"$package_lock/owner"; then
  rm -f "$package_lock/owner"
  rmdir "$package_lock" >/dev/null 2>&1 || true
  exit 1
fi

wails_source="$(go env GOMODCACHE)/github.com/wailsapp/wails/v3@v3.0.0-alpha2.117"
if [[ ! -d "$wails_source/cmd/wails3" ]]; then
  printf '%s\n' 'M11-PRIV-003: pinned Wails CLI source is not available in the module cache' >&2
  exit 1
fi
GOPROXY=off go -C "$wails_source" build -o "$tool_root/wails3" ./cmd/wails3
export PATH="$tool_root:$PATH"

CGO_ENABLED=0 go test ./internal/privacy ./internal/codex/logs ./internal/indexer ./internal/store ./internal/store/sqlite ./internal/codex/quota ./internal/query/... ./scripts/m11privacy -count=1
CGO_ENABLED=1 go test ./internal/app -count=1
printf '%s' "$package_token" >"$package_started"
make verify
go run ./scripts/m11privacy --repo "$repo_root" --artifact "bin/Codex Pulse.app"
