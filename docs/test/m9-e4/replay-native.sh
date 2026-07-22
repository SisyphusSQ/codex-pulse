#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "$0")/../../.." && pwd)"
run_dir="$repo_root/.artifacts/runs/too-288-popover-native"
app_binary="$repo_root/bin/Codex Pulse.app/Contents/MacOS/Codex Pulse"
export PATH="$repo_root/.task/bin:$PATH"

command -v wails3 >/dev/null
command -v osascript >/dev/null
command -v swift >/dev/null
mkdir -p "$run_dir/home" "$run_dir/tmp"

cd "$repo_root"
wails3 package GOOS=darwin

HOME="$run_dir/home" TMPDIR="$run_dir/tmp" "$app_binary" &
app_pid=$!
cleanup() {
  kill "$app_pid" 2>/dev/null || true
  wait "$app_pid" 2>/dev/null || true
}
trap cleanup EXIT INT TERM

application_ready() {
  osascript - "$app_pid" <<'APPLESCRIPT' >/dev/null 2>&1
on run argv
  set targetPID to (item 1 of argv) as integer
  tell application "System Events"
    tell first application process whose unix id is targetPID
      if (count of menu bars) < 2 then error "status menu is not ready"
      if (count of windows) < 1 then error "main window is not ready"
    end tell
  end tell
end run
APPLESCRIPT
}

ready=false
for _ in $(seq 1 100); do
  if ! kill -0 "$app_pid" 2>/dev/null; then
    echo "Codex Pulse exited before native UI became ready" >&2
    exit 1
  fi
  if application_ready; then
    ready=true
    break
  fi
  sleep 0.1
done
if [[ "$ready" != true ]]; then
  echo "Codex Pulse native UI did not become ready" >&2
  exit 1
fi

window_inventory() {
  swift -e '
import CoreGraphics
import Foundation
let pid = Int32(CommandLine.arguments[1])!
let windows = (CGWindowListCopyWindowInfo([.optionOnScreenOnly, .excludeDesktopElements], kCGNullWindowID)! as! [[String: Any]])
  .filter { ($0[kCGWindowOwnerPID as String] as? Int32) == pid }
print("count=\(windows.count)")
for window in windows {
  let bounds = window[kCGWindowBounds as String] as! [String: Any]
  let width = (bounds["Width"] as! NSNumber).intValue
  let height = (bounds["Height"] as! NSNumber).intValue
  print("size=\(width)x\(height)")
}
' "$app_pid"
}

press_status_item() {
  osascript - "$app_pid" <<'APPLESCRIPT' >/dev/null
on run argv
  set targetPID to (item 1 of argv) as integer
  tell application "System Events"
    tell first application process whose unix id is targetPID
      perform action "AXPress" of UI element 1 of menu bar 2
    end tell
  end tell
end run
APPLESCRIPT
}

# The close button is hidden; Cmd-W is the supported close-to-hide gesture.
osascript - "$app_pid" <<'APPLESCRIPT' >/dev/null
on run argv
  set targetPID to (item 1 of argv) as integer
  tell application "System Events"
    tell first application process whose unix id is targetPID
      set frontmost to true
      keystroke "w" using command down
    end tell
  end tell
end run
APPLESCRIPT
sleep 0.2
kill -0 "$app_pid"
hidden_inventory="$(window_inventory)"
grep -qx 'count=0' <<<"$hidden_inventory"

press_status_item
shown=false
for _ in $(seq 1 50); do
  shown_inventory="$(window_inventory)"
  if grep -qx 'count=1' <<<"$shown_inventory" && grep -qx 'size=419x759' <<<"$shown_inventory"; then
    shown=true
    break
  fi
  sleep 0.1
done
if [[ "$shown" != true ]]; then
  echo "Popover did not reach the expected on-screen geometry" >&2
  echo "$shown_inventory" >&2
  exit 1
fi

press_status_item
hidden=false
for _ in $(seq 1 50); do
  final_inventory="$(window_inventory)"
  if grep -qx 'count=0' <<<"$final_inventory"; then
    hidden=true
    break
  fi
  sleep 0.1
done
if [[ "$hidden" != true ]]; then
  echo "Popover did not hide after the second status-item press" >&2
  echo "$final_inventory" >&2
  exit 1
fi

kill -0 "$app_pid"
echo "native popover replay passed (pid=$app_pid, show=419x759, hidden=0)"
