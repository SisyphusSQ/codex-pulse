#!/usr/bin/env bash

set -euo pipefail

SCRIPT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
REPO_ROOT=$(CDPATH= cd -- "$SCRIPT_DIR/../.." && pwd)
SMOKE_ROOT=$(mktemp -d "/private/tmp/cp-app-smoke.XXXXXX")
APP_DIR="$SMOKE_ROOT/Codex Pulse.app"
RUNTIME_DIR="$SMOKE_ROOT/runtime"
APP_OUTPUT="$SMOKE_ROOT/app-smoke.out"
APP_PID=""

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
  rm -rf -- "$SMOKE_ROOT"
}
trap cleanup EXIT INT TERM

chmod 0700 "$SMOKE_ROOT"
bash "$SCRIPT_DIR/build-dev-app.sh" --output "$APP_DIR"
mkdir -p "$RUNTIME_DIR"
chmod 0700 "$RUNTIME_DIR"
go run "$SCRIPT_DIR/smoke-seed" \
  --preferences "$RUNTIME_DIR/preferences.json" \
  --home "$RUNTIME_DIR/codex-home"

"$APP_DIR/Contents/MacOS/Codex Pulse" \
  --ui-smoke \
  --runtime-directory "$RUNTIME_DIR" \
  --skip-live-lifecycle >"$APP_OUTPUT" 2>&1 &
APP_PID=$!

deadline=$((SECONDS + 30))
while app_is_running; do
  if [ "$SECONDS" -ge "$deadline" ]; then
    kill "$APP_PID" 2>/dev/null || true
    grace_deadline=$((SECONDS + 5))
    while app_is_running && [ "$SECONDS" -lt "$grace_deadline" ]; do sleep 0.1; done
    if app_is_running; then kill -KILL "$APP_PID" 2>/dev/null || true; fi
    wait "$APP_PID" 2>/dev/null || true
    APP_PID=""
    echo "app smoke failed: timeout" >&2
    exit 1
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
[ "$app_status" -eq 0 ] || {
  if grep -Fq 'codex-pulse-app: startup configuration unavailable' "$APP_OUTPUT"; then
    startup_code=$(grep -Eo 'code=[a-z_]+' "$APP_OUTPUT" | tail -n 1 || true)
    echo "app smoke failed: startup_configuration_unavailable ${startup_code:-code=unknown}" >&2
  fi
  echo "app smoke failed: app_exit_status=$app_status" >&2
  exit 1
}
[ -n "$smoke_summary" ] || {
  echo "app smoke failed: stable summary missing" >&2
  exit 1
}
case "$smoke_summary" in
  "app smoke passed: "*) ;;
  *)
    echo "app smoke failed: application reported a failed smoke summary" >&2
    exit 1
    ;;
esac
printf '%s\n' "$smoke_summary" | grep -Eq \
  'overview=loaded quota_windows=0 sessions=0 trend_points=0 .*primary_pages=partial sessions=0 projects=0 sources=0 jobs=0 health_events=0 .*unavailable=projects_unavailable ui_pages=7 ' || {
  echo "app smoke failed: isolated empty Home produced unexpected user facts" >&2
  exit 1
}
if find "$RUNTIME_DIR/codex-home" -type f -name '*.jsonl' -print -quit | grep -q .; then
  echo "app smoke failed: isolated empty Home produced unexpected rollout data" >&2
  exit 1
fi

[ ! -S "$RUNTIME_DIR/core.sock" ] || {
  echo "app smoke failed: helper socket remained after shutdown" >&2
  exit 1
}

printf 'app smoke cleanup passed: isolated_runtime=yes user_codex_home=no rollout_data=no\n'
