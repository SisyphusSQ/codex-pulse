#!/usr/bin/env bash

set -euo pipefail

SCRIPT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
REPO_ROOT=$(CDPATH= cd -- "$SCRIPT_DIR/../.." && pwd)
APP_DIR="$REPO_ROOT/build/dev/Codex Pulse.app"

while [ "$#" -gt 0 ]; do
  case "$1" in
    --output)
      [ "$#" -ge 2 ] || { echo "missing value for --output" >&2; exit 2; }
      APP_DIR=$2
      shift 2
      ;;
    *)
      echo "unknown argument: $1" >&2
      exit 2
      ;;
  esac
done

case "$APP_DIR" in
  *"/../"*|*"/./"*|*"//"*)
    echo "development app output contains an unsafe path component" >&2
    exit 2
    ;;
esac
case "$APP_DIR" in
  "$REPO_ROOT"/build/dev/*.app|/private/tmp/cp-app-smoke.*/*.app) ;;
  *)
    echo "development app output must stay under repo build/dev or an isolated cp-app-smoke root" >&2
    exit 2
    ;;
esac

swift build --package-path "$REPO_ROOT/app/macos" --product codex-pulse-app
make -C "$REPO_ROOT" verify-helper

SWIFT_BIN_DIR=$(swift build --package-path "$REPO_ROOT/app/macos" --show-bin-path)
APP_EXECUTABLE="$SWIFT_BIN_DIR/codex-pulse-app"
HELPER_EXECUTABLE="$REPO_ROOT/bin/codex-pulse"
[ -x "$APP_EXECUTABLE" ] || { echo "Swift app executable is missing" >&2; exit 1; }
[ -x "$HELPER_EXECUTABLE" ] || { echo "Go Helper executable is missing" >&2; exit 1; }

rm -rf -- "$APP_DIR"
mkdir -p "$APP_DIR/Contents/MacOS" "$APP_DIR/Contents/Helpers" "$APP_DIR/Contents/Resources"
cp "$SCRIPT_DIR/Info.plist" "$APP_DIR/Contents/Info.plist"
cp "$APP_EXECUTABLE" "$APP_DIR/Contents/MacOS/Codex Pulse"
cp "$HELPER_EXECUTABLE" "$APP_DIR/Contents/Helpers/codex-pulse"
chmod 0755 "$APP_DIR/Contents/MacOS/Codex Pulse" "$APP_DIR/Contents/Helpers/codex-pulse"
plutil -lint "$APP_DIR/Contents/Info.plist" >/dev/null

printf 'development app assembled: executable=present helper=present signed=no distribution=no\n'
