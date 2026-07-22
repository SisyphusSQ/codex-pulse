#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "$0")/../../.." && pwd)"
run_dir="$repo_root/.artifacts/runs/too-289-native-$(date +%s)"
app_binary="$repo_root/bin/Codex Pulse.app/Contents/MacOS/Codex Pulse"
export PATH="$repo_root/.task/bin:$PATH"

command -v wails3 >/dev/null
command -v osascript >/dev/null
mkdir -p "$run_dir/home" "$run_dir/tmp"
cd "$repo_root"
wails3 package GOOS=darwin

HOME="$run_dir/home" TMPDIR="$run_dir/tmp" "$app_binary" >"$run_dir/app.log" 2>&1 &
app_pid=$!
cleanup() { kill "$app_pid" 2>/dev/null || true; wait "$app_pid" 2>/dev/null || true; }
trap cleanup EXIT INT TERM

application_ready() {
  osascript - "$app_pid" <<'APPLESCRIPT' >/dev/null 2>&1
on run argv
  set targetPID to (item 1 of argv) as integer
  tell application "System Events" to tell first application process whose unix id is targetPID
    if (count of menu bars) < 2 then error "status menu is not ready"
    if (count of windows) < 1 then error "main window is not ready"
  end tell
end run
APPLESCRIPT
}

ready=false
for _ in $(seq 1 100); do
  if ! kill -0 "$app_pid" 2>/dev/null; then echo "Codex Pulse exited before native UI became ready" >&2; exit 1; fi
  if application_ready; then ready=true; break; fi
  sleep 0.1
done
[[ "$ready" == true ]] || { echo "Codex Pulse native UI did not become ready" >&2; exit 1; }

on_screen_window_inventory() {
  swift -e '
import CoreGraphics
import Foundation
let pid = Int32(CommandLine.arguments[1])!
let windows = (CGWindowListCopyWindowInfo([.optionOnScreenOnly, .excludeDesktopElements], kCGNullWindowID)! as! [[String: Any]])
  .filter { ($0[kCGWindowOwnerPID as String] as? Int32) == pid }
print("count=\(windows.count)")
for window in windows {
  let bounds = window[kCGWindowBounds as String] as! [String: Any]
  print("size=\((bounds["Width"] as! NSNumber).intValue)x\((bounds["Height"] as! NSNumber).intValue)")
}
' "$app_pid"
}

press_status_item() {
  osascript - "$app_pid" <<'APPLESCRIPT' >/dev/null
on run argv
  set targetPID to (item 1 of argv) as integer
  tell application "System Events" to tell first application process whose unix id is targetPID
    perform action "AXPress" of UI element 1 of menu bar 2
  end tell
end run
APPLESCRIPT
}

actions="$(osascript - "$app_pid" <<'APPLESCRIPT'
on run argv
  set targetPID to (item 1 of argv) as integer
  tell application "System Events" to tell first application process whose unix id is targetPID
    return name of every action of UI element 1 of menu bar 2
  end tell
end run
APPLESCRIPT
)"
grep -q 'AXPress' <<<"$actions"

# This host's crowded menu bar places the status item behind the camera notch,
# so a coordinate right-click is not a truthful replay. Verify the finite native
# menu construction statically and keep the packaged primary action executable.
for expected in '打开概览' '刷新' '设置…' '关于 Codex Pulse' '退出 Codex Pulse'; do
  rg -Fq "@\"$expected\"" internal/platform/tray/native_darwin.m
done

osascript - "$app_pid" <<'APPLESCRIPT' >/dev/null
on run argv
  set targetPID to (item 1 of argv) as integer
  tell application "System Events" to tell first application process whose unix id is targetPID
    set frontmost to true
    keystroke "w" using command down
  end tell
end run
APPLESCRIPT

hidden=false
for _ in $(seq 1 50); do
  if grep -qx 'count=0' <<<"$(on_screen_window_inventory)"; then hidden=true; break; fi
  sleep 0.1
done
[[ "$hidden" == true ]] || { echo "Cmd-W did not hide the main window" >&2; exit 1; }

press_status_item
shown=false
for _ in $(seq 1 50); do
  inventory="$(on_screen_window_inventory)"
  if grep -qx 'count=1' <<<"$inventory" && grep -qx 'size=419x759' <<<"$inventory"; then shown=true; break; fi
  sleep 0.1
done
[[ "$shown" == true ]] || { echo "Popover primary action was replaced by the native menu" >&2; exit 1; }

press_status_item
hidden=false
for _ in $(seq 1 50); do
  if grep -qx 'count=0' <<<"$(on_screen_window_inventory)"; then hidden=true; break; fi
  sleep 0.1
done
[[ "$hidden" == true ]] || { echo "Popover did not hide after the second primary action" >&2; exit 1; }

kill -0 "$app_pid"
if rg -n 'SIGTRAP|panic|fatal error' "$run_dir/app.log"; then exit 1; fi
echo "native menu structure and preserved Popover action replay passed"
