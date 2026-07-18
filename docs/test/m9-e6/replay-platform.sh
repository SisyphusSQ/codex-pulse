#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "$0")/../../.." && pwd)"
run_dir="$repo_root/.agents/runs/too-290-platform-$(date +%s)"
probe_dir="$run_dir/probe"
app_binary="$repo_root/bin/Codex Pulse.app/Contents/MacOS/Codex Pulse"
export PATH="$repo_root/.task/bin:$PATH"

command -v wails3 >/dev/null
command -v osascript >/dev/null
mkdir -p "$probe_dir" "$run_dir/home" "$run_dir/tmp"
cd "$repo_root"

go run ./cmd/traystatusprobe --output "$probe_dir"
for change in display space wake appearance; do rg -q "\"$change\"" "$probe_dir/platform-events.json"; done

swiftc docs/test/m9-e6/observe-title.swift -o "$run_dir/observe-title"
ax_ready="$run_dir/ax-ready"
ax_continue="$run_dir/ax-continue"
go run ./cmd/traystatusprobe --output "$run_dir/ax-probe" \
  --accessibility-ready-file "$ax_ready" --accessibility-continue-file "$ax_continue" \
  >"$run_dir/ax-probe.log" 2>&1 &
ax_probe_job=$!
for _ in $(seq 1 100); do [[ -s "$ax_ready" ]] && break; sleep 0.05; done
[[ -s "$ax_ready" ]] || { echo "accessibility probe did not become ready" >&2; exit 1; }
"$run_dir/observe-title" "$(tr -d '\n' <"$ax_ready")" "$ax_continue" "$run_dir/ax-title-notification.txt"
wait "$ax_probe_job"
rg -qx 'AXTitleChanged|AXAnnouncementRequested' "$run_dir/ax-title-notification.txt"

swift -e '
import AppKit
for (index, screen) in NSScreen.screens.enumerated() {
  let frame = screen.frame
  let visible = screen.visibleFrame
  print("screen=\(index) frame=\(Int(frame.origin.x)),\(Int(frame.origin.y)),\(Int(frame.width))x\(Int(frame.height)) visible=\(Int(visible.origin.x)),\(Int(visible.origin.y)),\(Int(visible.width))x\(Int(visible.height)) scale=\(screen.backingScaleFactor)")
}
' >"$run_dir/screens.txt"
test -s "$run_dir/screens.txt"

wails3 package GOOS=darwin
HOME="$run_dir/home" TMPDIR="$run_dir/tmp" "$app_binary" >"$run_dir/app.log" 2>&1 &
app_pid=$!
cleanup() { kill "$app_pid" 2>/dev/null || true; wait "$app_pid" 2>/dev/null || true; }
trap cleanup EXIT INT TERM

ready=false
for _ in $(seq 1 100); do
  if ! kill -0 "$app_pid" 2>/dev/null; then echo "Codex Pulse exited before platform replay" >&2; exit 1; fi
  if osascript - "$app_pid" <<'APPLESCRIPT' >/dev/null 2>&1
on run argv
  set targetPID to (item 1 of argv) as integer
  tell application "System Events" to tell first application process whose unix id is targetPID
    if (count of menu bars) < 2 then error "status item unavailable"
    if (count of windows) < 1 then error "main window unavailable"
  end tell
end run
APPLESCRIPT
  then ready=true; break; fi
  sleep 0.1
done
[[ "$ready" == true ]] || { echo "platform replay did not become ready" >&2; exit 1; }

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

osascript - "$app_pid" <<'APPLESCRIPT' >"$run_dir/status-accessibility.txt"
on run argv
  set targetPID to (item 1 of argv) as integer
  tell application "System Events" to tell first application process whose unix id is targetPID
    set statusElement to UI element 1 of menu bar 2
    return (role of statusElement as text) & "|" & (name of statusElement as text) & "|" & (value of attribute "AXHelp" of statusElement as text) & "|" & (name of every action of statusElement as text)
  end tell
end run
APPLESCRIPT
rg -q 'AXMenuBarItem' "$run_dir/status-accessibility.txt"
rg -q 'Codex Pulse|数据不可用|剩余' "$run_dir/status-accessibility.txt"
rg -q '左键打开额度概览，右键打开应用菜单' "$run_dir/status-accessibility.txt"
rg -q 'AXPress' "$run_dir/status-accessibility.txt"

osascript - "$app_pid" <<'APPLESCRIPT' >/dev/null
on run argv
  set targetPID to (item 1 of argv) as integer
  tell application "System Events" to tell first application process whose unix id is targetPID
    set frontmost to true
    keystroke "w" using command down
    perform action "AXPress" of UI element 1 of menu bar 2
  end tell
end run
APPLESCRIPT

popover_visible=false
for _ in $(seq 1 50); do
  inventory="$(on_screen_window_inventory)"
  if grep -qx 'count=1' <<<"$inventory" && rg -q '^size=419x(759|760|761)$' <<<"$inventory"; then popover_visible=true; break; fi
  sleep 0.1
done
[[ "$popover_visible" == true ]] || { echo "Popover window did not reach expected Retina geometry" >&2; exit 1; }

osascript - "$app_pid" <<'APPLESCRIPT' >/dev/null
on run argv
  set targetPID to (item 1 of argv) as integer
  tell application "System Events" to tell first application process whose unix id is targetPID
    set frontmost to true
    key code 53
  end tell
end run
APPLESCRIPT

popover_hidden=false
for _ in $(seq 1 50); do
  count="$(osascript - "$app_pid" <<'APPLESCRIPT'
on run argv
  set targetPID to (item 1 of argv) as integer
  tell application "System Events" to tell first application process whose unix id is targetPID
    return count of windows
  end tell
end run
APPLESCRIPT
)"
  if [[ "$count" == 0 ]]; then popover_hidden=true; break; fi
  sleep 0.1
done
[[ "$popover_hidden" == true ]] || { echo "Escape did not hide Popover" >&2; exit 1; }

osascript - "$app_pid" <<'APPLESCRIPT' >/dev/null
on run argv
  set targetPID to (item 1 of argv) as integer
  tell application "System Events" to tell first application process whose unix id is targetPID
    set frontmost to true
    keystroke "q" using command down
  end tell
end run
APPLESCRIPT

exited=false
for _ in $(seq 1 150); do
  if ! kill -0 "$app_pid" 2>/dev/null; then exited=true; break; fi
  sleep 0.1
done
[[ "$exited" == true ]] || { echo "graceful Cmd-Q shutdown did not complete" >&2; exit 1; }
wait "$app_pid"
trap - EXIT INT TERM
if rg -n 'SIGTRAP|panic|fatal error' "$run_dir/app.log"; then exit 1; fi
echo "AppKit platform/accessibility replay passed"
