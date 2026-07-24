#!/usr/bin/env bash

set -euo pipefail

SCRIPT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
REPO_ROOT=$(CDPATH= cd -- "$SCRIPT_DIR/../.." && pwd)
APP_ICON_ROOT="$REPO_ROOT/app/macos/Resources/AppIcon"
STATUS_ITEM_ROOT="$REPO_ROOT/app/macos/Resources/StatusItem"
ICONSET_DIR="$APP_ICON_ROOT/CodexPulse.iconset"
WORK_DIR="$REPO_ROOT/build/icon-assets"

for tool in sips iconutil xmllint swift; do
  command -v "$tool" >/dev/null 2>&1 || {
    echo "required icon tool is unavailable: $tool" >&2
    exit 1
  }
done

xmllint --noout \
  "$APP_ICON_ROOT/CodexPulseDefault.svg" \
  "$APP_ICON_ROOT/CodexPulseDark.svg" \
  "$APP_ICON_ROOT/CodexPulseMono.svg" \
  "$STATUS_ITEM_ROOT/CodexPulseStatusTemplate.svg"

mkdir -p "$ICONSET_DIR" "$STATUS_ITEM_ROOT" "$WORK_DIR"

sips -s format png \
  "$APP_ICON_ROOT/CodexPulseDefault.svg" \
  --out "$ICONSET_DIR/icon_512x512@2x.png" >/dev/null

render_icon() {
  local pixels=$1
  local output=$2
  cp "$ICONSET_DIR/icon_512x512@2x.png" "$ICONSET_DIR/$output"
  sips -z "$pixels" "$pixels" "$ICONSET_DIR/$output" >/dev/null
}

render_icon 16 icon_16x16.png
render_icon 32 icon_16x16@2x.png
render_icon 32 icon_32x32.png
render_icon 64 icon_32x32@2x.png
render_icon 128 icon_128x128.png
render_icon 256 icon_128x128@2x.png
render_icon 256 icon_256x256.png
render_icon 512 icon_256x256@2x.png
render_icon 512 icon_512x512.png

sips -s format png \
  "$STATUS_ITEM_ROOT/CodexPulseStatusTemplate.svg" \
  --out "$WORK_DIR/CodexPulseStatusTemplate-1024.png" >/dev/null
cp "$WORK_DIR/CodexPulseStatusTemplate-1024.png" "$STATUS_ITEM_ROOT/CodexPulseStatusTemplate@2x.png"
sips -z 38 38 "$STATUS_ITEM_ROOT/CodexPulseStatusTemplate@2x.png" >/dev/null
cp "$WORK_DIR/CodexPulseStatusTemplate-1024.png" "$STATUS_ITEM_ROOT/CodexPulseStatusTemplate.png"
sips -z 19 19 "$STATUS_ITEM_ROOT/CodexPulseStatusTemplate.png" >/dev/null

iconutil -c icns "$ICONSET_DIR" -o "$WORK_DIR/CodexPulse.icns"
swift "$SCRIPT_DIR/validate-icon-assets.swift" "$REPO_ROOT"

printf 'icon assets rendered: appicon=10 status-item=2 icns-validation=passed\n'
