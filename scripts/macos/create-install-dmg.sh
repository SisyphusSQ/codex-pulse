#!/usr/bin/env bash

set -euo pipefail

APP_PATH=""
OUTPUT_PATH=""
VOLUME_NAME=""

fail() {
  printf 'install DMG build failed: %s\n' "$*" >&2
  exit 1
}

usage() {
  printf '%s\n' \
    "usage: create-install-dmg.sh --app <bundle.app> --output <installer.dmg>" \
    "  --volume-name <name>"
}

while [[ "$#" -gt 0 ]]; do
  case "$1" in
    --app)
      APP_PATH=${2:-}
      shift 2
      ;;
    --output)
      OUTPUT_PATH=${2:-}
      shift 2
      ;;
    --volume-name)
      VOLUME_NAME=${2:-}
      shift 2
      ;;
    --help|-h)
      usage
      exit 0
      ;;
    *)
      fail "unknown argument: $1"
      ;;
  esac
done

[[ -n "$APP_PATH" && -n "$OUTPUT_PATH" && -n "$VOLUME_NAME" ]] ||
  fail "app, output, and volume name are required"
[[ -d "$APP_PATH" && ! -L "$APP_PATH" ]] ||
  fail "app must be a non-symlink bundle directory"
[[ "$(basename -- "$APP_PATH")" == *.app ]] ||
  fail "app must use the .app extension"
[[ "$OUTPUT_PATH" == *.dmg && ! -L "$OUTPUT_PATH" ]] ||
  fail "output must be a non-symlink .dmg path"
[[ "$VOLUME_NAME" != *"/"* && "$VOLUME_NAME" != "." && "$VOLUME_NAME" != ".." ]] ||
  fail "volume name is invalid"

for tool in codesign ditto find hdiutil; do
  command -v "$tool" >/dev/null 2>&1 ||
    fail "required tool is unavailable: $tool"
done

OUTPUT_PARENT=$(dirname -- "$OUTPUT_PATH")
[[ -d "$OUTPUT_PARENT" && ! -L "$OUTPUT_PARENT" ]] ||
  fail "output parent must be a non-symlink directory"

codesign --verify --deep --strict "$APP_PATH" ||
  fail "source App signature verification failed"

WORK_ROOT=$(mktemp -d "$OUTPUT_PARENT/.dmg-build.XXXXXX")
STAGING_ROOT="$WORK_ROOT/root"
MOUNT_ROOT="$WORK_ROOT/mounted"
TEMP_DMG="$WORK_ROOT/$(basename -- "$OUTPUT_PATH")"
APP_NAME=$(basename -- "$APP_PATH")
MOUNTED=0

cleanup() {
  if [[ "$MOUNTED" -eq 1 ]]; then
    hdiutil detach -quiet "$MOUNT_ROOT" >/dev/null 2>&1 || true
  fi
  rm -rf -- "$WORK_ROOT"
}
trap cleanup EXIT INT TERM

mkdir -p "$STAGING_ROOT" "$MOUNT_ROOT"
ditto "$APP_PATH" "$STAGING_ROOT/$APP_NAME"
ln -s /Applications "$STAGING_ROOT/Applications"

hdiutil create \
  -quiet \
  -ov \
  -fs APFS \
  -format ULFO \
  -volname "$VOLUME_NAME" \
  -srcfolder "$STAGING_ROOT" \
  "$TEMP_DMG"
hdiutil verify -quiet "$TEMP_DMG"

hdiutil attach \
  -quiet \
  -readonly \
  -nobrowse \
  -mountpoint "$MOUNT_ROOT" \
  "$TEMP_DMG"
MOUNTED=1

ROOT_ENTRY_COUNT=$(
  find "$MOUNT_ROOT" -mindepth 1 -maxdepth 1 -print |
    wc -l |
    tr -d '[:space:]'
)
[[ "$ROOT_ENTRY_COUNT" == "2" ]] ||
  fail "DMG root must contain only the App and Applications link"
[[ -d "$MOUNT_ROOT/$APP_NAME" && ! -L "$MOUNT_ROOT/$APP_NAME" ]] ||
  fail "DMG App bundle is missing"
[[ -L "$MOUNT_ROOT/Applications" ]] ||
  fail "DMG Applications link is missing"
[[ "$(readlink "$MOUNT_ROOT/Applications")" == "/Applications" ]] ||
  fail "DMG Applications link has an unexpected target"
cmp \
  "$APP_PATH/Contents/Info.plist" \
  "$MOUNT_ROOT/$APP_NAME/Contents/Info.plist" >/dev/null ||
  fail "DMG App metadata changed"
codesign --verify --deep --strict "$MOUNT_ROOT/$APP_NAME" ||
  fail "mounted DMG App signature verification failed"

hdiutil detach -quiet "$MOUNT_ROOT"
MOUNTED=0
mv -f -- "$TEMP_DMG" "$OUTPUT_PATH"
printf 'install DMG created: %s\n' "$OUTPUT_PATH"
