#!/usr/bin/env bash

set -euo pipefail

SCRIPT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
REPO_ROOT=$(CDPATH= cd -- "$SCRIPT_DIR/../.." && pwd)
RUNTIME_DIR=${CODEX_PULSE_APP_RUNTIME:-}
APP_DIR=${CODEX_PULSE_APP_BUNDLE:-}
REAL_HOME_INPUT=${CODEX_HOME:-$HOME/.codex}
APP_PID=""
APP_OUTPUT=""

fail() {
  printf 'app live smoke failed: %s\n' "$1" >&2
  exit 1
}

app_is_running() {
  local state
  state=$(/bin/ps -p "$APP_PID" -o state= 2>/dev/null || true)
  state=${state//[[:space:]]/}
  case "$state" in
    ""|Z*) return 1 ;;
    *) return 0 ;;
  esac
}

cleanup() {
  if [ -n "$APP_PID" ] && app_is_running; then
    kill "$APP_PID" 2>/dev/null || true
    wait "$APP_PID" 2>/dev/null || true
  fi
  if [ -n "$APP_OUTPUT" ] && [ -f "$APP_OUTPUT" ]; then
    rm -- "$APP_OUTPUT"
  fi
}
trap cleanup EXIT INT TERM

[ -n "$RUNTIME_DIR" ] || fail "CODEX_PULSE_APP_RUNTIME is required"
[ "$(id -u)" = "$(stat -f '%u' "$RUNTIME_DIR" 2>/dev/null || true)" ] || fail "runtime owner mismatch"
[ "$(stat -f '%Lp' "$RUNTIME_DIR" 2>/dev/null || true)" = "700" ] || fail "runtime must use mode 0700"
case "$RUNTIME_DIR" in
  /private/tmp/cp-*|/tmp/cp-*) ;;
  *) fail "runtime must be an existing private cp-* directory" ;;
esac
case "$RUNTIME_DIR" in
  *"/../"*|*"/./"*|*"//"*) fail "runtime contains an unsafe path component" ;;
esac
[ -f "$RUNTIME_DIR/preferences.json" ] || fail "configured preferences are required"
[ -d "$RUNTIME_DIR/data" ] || fail "configured data directory is required"
[ ! -S "$RUNTIME_DIR/core.sock" ] || fail "runtime is already active"
command -v jq >/dev/null 2>&1 || fail "jq is required for content-free preferences preflight"

[ -d "$REAL_HOME_INPUT" ] || fail "real Codex Home is unavailable"
REAL_HOME=$(CDPATH= cd -- "$REAL_HOME_INPUT" && pwd -P)
STORED_HOME=$(jq -er '.codex_home.source.path' "$RUNTIME_DIR/preferences.json") || fail "confirmed Home is unavailable"
STORED_DEVICE=$(jq -er '.codex_home.source.device_id' "$RUNTIME_DIR/preferences.json") || fail "confirmed Home device is unavailable"
STORED_INODE=$(jq -er '.codex_home.source.inode' "$RUNTIME_DIR/preferences.json") || fail "confirmed Home inode is unavailable"
[ "$STORED_HOME" = "$REAL_HOME" ] || fail "confirmed Home is not the real Codex Home"
[ "$STORED_DEVICE" = "$(stat -f '%d' "$REAL_HOME")" ] || fail "confirmed Home device changed"
[ "$STORED_INODE" = "$(stat -f '%i' "$REAL_HOME")" ] || fail "confirmed Home inode changed"

if [ -z "$APP_DIR" ]; then
  APP_DIR="$REPO_ROOT/build/dev/Codex Pulse.app"
  bash "$SCRIPT_DIR/build-dev-app.sh" --output "$APP_DIR"
fi
case "$APP_DIR" in
  "$REPO_ROOT"/build/dev/*.app|/private/tmp/cp-app-smoke.*/*.app) ;;
  *) fail "development App must use an approved local bundle path" ;;
esac
[ -x "$APP_DIR/Contents/MacOS/Codex Pulse" ] || fail "development App executable is unavailable"
[ -x "$APP_DIR/Contents/Helpers/codex-pulse" ] || fail "bundled Helper is unavailable"

APP_OUTPUT=$(mktemp "$RUNTIME_DIR/.app-live-smoke.XXXXXX")
chmod 0600 "$APP_OUTPUT"
"$APP_DIR/Contents/MacOS/Codex Pulse" \
  --ui-smoke \
  --runtime-directory "$RUNTIME_DIR" \
  --skip-live-lifecycle >"$APP_OUTPUT" 2>&1 &
APP_PID=$!

deadline=$((SECONDS + 90))
while app_is_running; do
  if [ "$SECONDS" -ge "$deadline" ]; then
    kill "$APP_PID" 2>/dev/null || true
    grace_deadline=$((SECONDS + 5))
    while app_is_running && [ "$SECONDS" -lt "$grace_deadline" ]; do sleep 0.1; done
    if app_is_running; then kill -KILL "$APP_PID" 2>/dev/null || true; fi
    wait "$APP_PID" 2>/dev/null || true
    APP_PID=""
    fail "timeout"
  fi
  sleep 0.1
done

set +e
wait "$APP_PID"
app_status=$?
set -e
APP_PID=""
smoke_summary=$(grep -E '^app smoke (passed|failed): ' "$APP_OUTPUT" | tail -n 1 || true)
[ -n "$smoke_summary" ] && printf '%s\n' "$smoke_summary"
[ "$app_status" -eq 0 ] || fail "app_exit_status=$app_status"
case "$smoke_summary" in
  "app smoke passed: "*) ;;
  *) fail "application did not report a passing smoke summary" ;;
esac
printf '%s\n' "$smoke_summary" | grep -Eq \
  'overview=loaded .*sessions=[1-9][0-9]* trend_points=[1-9][0-9]* .*primary_pages=loaded sessions=[1-9][0-9]* projects=[1-9][0-9]* .*usage_trend=[1-9][0-9]* usage_models=[1-9][0-9]* usage_model_trend=[1-9][0-9]* usage_model_reconciled=[1-9][0-9]* usage_cost=known .*project_detail_cost=known project_detail_models=[1-9][0-9]* details_read=[1-9][0-9]* .*unavailable=none ui_pages=7 .*shutdown=clean' || \
  fail "real Home did not produce the required non-zero page contract"
[ ! -S "$RUNTIME_DIR/core.sock" ] || fail "Helper socket remained after shutdown"

printf 'app live smoke cleanup passed: runtime=reused confirmed_home=real standard_housekeeping=allowed raw_output=removed\n'
