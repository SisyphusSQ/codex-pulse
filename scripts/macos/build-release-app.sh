#!/usr/bin/env bash

set -euo pipefail

SCRIPT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
REPO_ROOT=$(CDPATH= cd -- "$SCRIPT_DIR/../.." && pwd)
VERSION=""
BUILD_NUMBER=""
SPARKLE_FEED_URL=""
SPARKLE_PUBLIC_KEY_FILE=""

usage() {
  cat <<'EOF'
usage: build-release-app.sh --version <semver> --build-number <positive-integer>
  --sparkle-feed-url <https-url> --sparkle-public-key-file <path>

Builds the ad-hoc-signed, unnotarized Apple Silicon preview asset under:
  .artifacts/releases/v<semver>/
EOF
}

fail() {
  printf 'release app build failed: %s\n' "$1" >&2
  exit 1
}

while [ "$#" -gt 0 ]; do
  case "$1" in
    --version)
      [ "$#" -ge 2 ] || fail "missing value for --version"
      VERSION=${2#v}
      shift 2
      ;;
    --build-number)
      [ "$#" -ge 2 ] || fail "missing value for --build-number"
      BUILD_NUMBER=$2
      shift 2
      ;;
    --sparkle-feed-url)
      [ "$#" -ge 2 ] || fail "missing value for --sparkle-feed-url"
      SPARKLE_FEED_URL=$2
      shift 2
      ;;
    --sparkle-public-key-file)
      [ "$#" -ge 2 ] || fail "missing value for --sparkle-public-key-file"
      SPARKLE_PUBLIC_KEY_FILE=$2
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

[[ "$VERSION" =~ ^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-[0-9A-Za-z-]+(\.[0-9A-Za-z-]+)*)?(\+[0-9A-Za-z-]+(\.[0-9A-Za-z-]+)*)?$ ]] ||
  fail "version must be SemVer"
[[ "$BUILD_NUMBER" =~ ^[1-9][0-9]*$ ]] ||
  fail "build number must be a positive integer"
[[ "$SPARKLE_FEED_URL" =~ ^https://[^[:space:]]+$ ]] ||
  fail "Sparkle feed URL must use HTTPS"
[ -f "$SPARKLE_PUBLIC_KEY_FILE" ] && [ ! -L "$SPARKLE_PUBLIC_KEY_FILE" ] ||
  fail "Sparkle public key must be a regular non-symlink file"
[ "$(uname -s)" = "Darwin" ] || fail "release build requires macOS"
[ "$(uname -m)" = "arm64" ] || fail "release build requires an Apple Silicon host"

for tool in swift go plutil iconutil lipo otool vtool strip codesign ditto hdiutil unzip shasum strings python3; do
  command -v "$tool" >/dev/null 2>&1 || fail "required tool is unavailable: $tool"
done

SPARKLE_PUBLIC_ED_KEY=$(tr -d '\r\n' <"$SPARKLE_PUBLIC_KEY_FILE")
python3 - "$SPARKLE_PUBLIC_ED_KEY" "$SPARKLE_FEED_URL" <<'PY' ||
import base64
import sys
from urllib.parse import urlparse

try:
    public_key = base64.b64decode(sys.argv[1], validate=True)
except ValueError:
    raise SystemExit(1)
feed_url = urlparse(sys.argv[2])
if (
    len(public_key) != 32
    or feed_url.scheme != "https"
    or not feed_url.hostname
    or feed_url.username is not None
    or feed_url.password is not None
    or feed_url.port is not None
    or bool(feed_url.query)
    or bool(feed_url.fragment)
):
    raise SystemExit(1)
PY
  fail "Sparkle public key or feed URL is invalid"

TAG="v$VERSION"
BUNDLE_SHORT_VERSION=${VERSION%%[-+]*}
RELEASE_DIR="$REPO_ROOT/.artifacts/releases/$TAG"
ARCHIVE_NAME="Codex-Pulse-$TAG-macos-arm64.zip"
ARCHIVE_PATH="$RELEASE_DIR/$ARCHIVE_NAME"
DMG_NAME="Codex-Pulse-$TAG-macos-arm64.dmg"
DMG_PATH="$RELEASE_DIR/$DMG_NAME"
CHECKSUM_PATH="$RELEASE_DIR/SHA256SUMS"
BUILD_ROOT=""

cleanup() {
  if [ -n "$BUILD_ROOT" ] && [ -d "$BUILD_ROOT" ]; then
    rm -rf -- "$BUILD_ROOT"
  fi
}
trap cleanup EXIT INT TERM

mkdir -p "$RELEASE_DIR"
BUILD_ROOT=$(mktemp -d "$RELEASE_DIR/.build.XXXXXX")
APP_DIR="$BUILD_ROOT/Codex Pulse.app"
EXTRACT_DIR="$BUILD_ROOT/extracted"
HELPER_EXECUTABLE="$BUILD_ROOT/codex-pulse"

SWIFT_RELEASE_ARGUMENTS=(
  --package-path "$REPO_ROOT/app/macos"
  --scratch-path "$BUILD_ROOT/swift-build"
  --configuration release
  -Xswiftc -gline-tables-only
  -Xcc "-ffile-prefix-map=$REPO_ROOT=."
  -Xcc "-fmacro-prefix-map=$REPO_ROOT=."
)

swift run \
  "${SWIFT_RELEASE_ARGUMENTS[@]}" \
  codex-pulse-app-tests
swift build \
  "${SWIFT_RELEASE_ARGUMENTS[@]}" \
  --product codex-pulse-app

SWIFT_BIN_DIR=$(swift build \
  "${SWIFT_RELEASE_ARGUMENTS[@]}" \
  --show-bin-path)
APP_EXECUTABLE="$SWIFT_BIN_DIR/codex-pulse-app"
SPARKLE_FRAMEWORK="$SWIFT_BIN_DIR/Sparkle.framework"
[ -x "$APP_EXECUTABLE" ] || fail "Swift release executable is missing"
[ -d "$SPARKLE_FRAMEWORK" ] || fail "Sparkle framework is missing"

(
  cd "$REPO_ROOT"
  GOOS=darwin GOARCH=arm64 CGO_ENABLED=0 go build \
    -trimpath \
    -ldflags="-s -w -X main.applicationVersion=$VERSION" \
    -o "$HELPER_EXECUTABLE" \
    .
)
[ -x "$HELPER_EXECUTABLE" ] || fail "Go Helper release executable is missing"

mkdir -p \
  "$APP_DIR/Contents/MacOS" \
  "$APP_DIR/Contents/Helpers" \
  "$APP_DIR/Contents/Resources" \
  "$APP_DIR/Contents/Frameworks"
cp "$SCRIPT_DIR/Info.plist" "$APP_DIR/Contents/Info.plist"
plutil -replace CFBundleShortVersionString \
  -string "$BUNDLE_SHORT_VERSION" "$APP_DIR/Contents/Info.plist"
plutil -replace CFBundleVersion \
  -string "$BUILD_NUMBER" "$APP_DIR/Contents/Info.plist"
plutil -replace CodexPulseProductVersion \
  -string "$VERSION" "$APP_DIR/Contents/Info.plist"
plutil -insert SUFeedURL \
  -string "$SPARKLE_FEED_URL" "$APP_DIR/Contents/Info.plist"
plutil -insert SUPublicEDKey \
  -string "$SPARKLE_PUBLIC_ED_KEY" "$APP_DIR/Contents/Info.plist"
plutil -insert SUEnableAutomaticChecks \
  -bool YES "$APP_DIR/Contents/Info.plist"
cp "$APP_EXECUTABLE" "$APP_DIR/Contents/MacOS/Codex Pulse"
cp "$HELPER_EXECUTABLE" "$APP_DIR/Contents/Helpers/codex-pulse"
ditto "$SPARKLE_FRAMEWORK" "$APP_DIR/Contents/Frameworks/Sparkle.framework"
strip -S "$APP_DIR/Contents/MacOS/Codex Pulse"
iconutil \
  -c icns \
  "$REPO_ROOT/app/macos/Resources/AppIcon/CodexPulse.iconset" \
  -o "$APP_DIR/Contents/Resources/CodexPulse.icns"
chmod 0755 \
  "$APP_DIR/Contents/MacOS/Codex Pulse" \
  "$APP_DIR/Contents/Helpers/codex-pulse"

PLIST="$APP_DIR/Contents/Info.plist"
plutil -lint "$PLIST" >/dev/null
[ "$(plutil -extract CFBundleIdentifier raw "$PLIST")" = "com.sisyphussq.codexpulse" ] ||
  fail "production bundle identifier readback failed"
[ "$(plutil -extract CFBundleDisplayName raw "$PLIST")" = "Codex Pulse" ] ||
  fail "production display name readback failed"
[ "$(plutil -extract CFBundleShortVersionString raw "$PLIST")" = "$BUNDLE_SHORT_VERSION" ] ||
  fail "bundle short version readback failed"
[ "$(plutil -extract CFBundleVersion raw "$PLIST")" = "$BUILD_NUMBER" ] ||
  fail "bundle build number readback failed"
[ "$(plutil -extract CodexPulseProductVersion raw "$PLIST")" = "$VERSION" ] ||
  fail "product version readback failed"
[ "$(plutil -extract SUFeedURL raw "$PLIST")" = "$SPARKLE_FEED_URL" ] ||
  fail "Sparkle feed URL readback failed"
[ "$(plutil -extract SUPublicEDKey raw "$PLIST")" = "$SPARKLE_PUBLIC_ED_KEY" ] ||
  fail "Sparkle public key readback failed"
[ "$(plutil -extract SUEnableAutomaticChecks raw "$PLIST")" = "true" ] ||
  fail "Sparkle automatic-check policy readback failed"
[ "$(plutil -extract LSMinimumSystemVersion raw "$PLIST")" = "15.0" ] ||
  fail "bundle minimum system version readback failed"

for executable in \
  "$APP_DIR/Contents/MacOS/Codex Pulse" \
  "$APP_DIR/Contents/Helpers/codex-pulse"; do
  [ "$(lipo -archs "$executable")" = "arm64" ] ||
    fail "non-arm64 executable in release bundle: $executable"
done
SPARKLE_EXECUTABLE="$APP_DIR/Contents/Frameworks/Sparkle.framework/Versions/B/Sparkle"
lipo -archs "$SPARKLE_EXECUTABLE" | tr ' ' '\n' | grep -Fxq arm64 ||
  fail "Sparkle framework does not contain arm64"
codesign --verify --deep --strict --verbose=2 \
  "$APP_DIR/Contents/Frameworks/Sparkle.framework"
otool -l "$APP_DIR/Contents/MacOS/Codex Pulse" |
  grep -Fq '@executable_path/../Frameworks' ||
  fail "Swift executable is missing the standard Frameworks rpath"
vtool -show-build "$APP_DIR/Contents/MacOS/Codex Pulse" |
  grep -Eq 'minos[[:space:]]+15(\.0)*' ||
  fail "Swift executable minimum macOS version is not 15"
awk -v expected="$VERSION" '
  $0 == expected { found = 1 }
  END { exit !found }
' < <(strings "$APP_DIR/Contents/Helpers/codex-pulse") ||
  fail "Helper product version readback failed"
for local_path in "$REPO_ROOT" "$HOME/"; do
  if grep -Fq "$local_path" < <(
    strings \
      "$APP_DIR/Contents/MacOS/Codex Pulse" \
      "$APP_DIR/Contents/Helpers/codex-pulse"
  ); then
    fail "release binaries contain a local absolute path"
  fi
done

codesign --force --sign - --timestamp=none \
  "$APP_DIR/Contents/Helpers/codex-pulse"
codesign --force --sign - --timestamp=none \
  "$APP_DIR"
codesign --verify --deep --strict --verbose=2 "$APP_DIR"
APP_CODESIGN_DETAILS=$(codesign -dv --verbose=2 "$APP_DIR" 2>&1)
grep -Fq 'Signature=adhoc' <<<"$APP_CODESIGN_DETAILS" ||
  fail "release App does not have a complete ad-hoc signature"

rm -f -- "$ARCHIVE_PATH" "$DMG_PATH" "$CHECKSUM_PATH"
COPYFILE_DISABLE=1 ditto \
  -c -k --norsrc --noextattr --keepParent \
  "$APP_DIR" "$ARCHIVE_PATH"
[ -s "$ARCHIVE_PATH" ] || fail "release ZIP is missing"
ARCHIVE_ENTRIES=$(unzip -Z1 "$ARCHIVE_PATH")
[ "$(printf '%s\n' "$ARCHIVE_ENTRIES" | awk -F/ 'NF {print $1}' | sort -u)" = "Codex Pulse.app" ] ||
  fail "release ZIP must contain one top-level App bundle"
if printf '%s\n' "$ARCHIVE_ENTRIES" |
  awk -F/ '$1 == "__MACOSX" || $NF ~ /^\._/ {found=1} END {exit !found}'; then
  fail "release ZIP contains AppleDouble metadata"
fi
mkdir -p "$EXTRACT_DIR"
ditto -x -k "$ARCHIVE_PATH" "$EXTRACT_DIR"
[ ! -L "$EXTRACT_DIR/Codex Pulse.app" ] ||
  fail "release ZIP contains a symlinked App root"
cmp \
  "$APP_DIR/Contents/Info.plist" \
  "$EXTRACT_DIR/Codex Pulse.app/Contents/Info.plist" >/dev/null ||
  fail "extracted bundle metadata changed"
[ "$(lipo -archs "$EXTRACT_DIR/Codex Pulse.app/Contents/MacOS/Codex Pulse")" = "arm64" ] ||
  fail "extracted App architecture readback failed"
[ "$(lipo -archs "$EXTRACT_DIR/Codex Pulse.app/Contents/Helpers/codex-pulse")" = "arm64" ] ||
  fail "extracted Helper architecture readback failed"
[ -x "$EXTRACT_DIR/Codex Pulse.app/Contents/Frameworks/Sparkle.framework/Versions/B/Sparkle" ] ||
  fail "extracted Sparkle framework is missing"
codesign --verify --deep --strict --verbose=2 \
  "$EXTRACT_DIR/Codex Pulse.app"
EXTRACTED_CODESIGN_DETAILS=$(
  codesign -dv --verbose=2 "$EXTRACT_DIR/Codex Pulse.app" 2>&1
)
grep -Fq 'Signature=adhoc' <<<"$EXTRACTED_CODESIGN_DETAILS" ||
  fail "extracted release App lost its ad-hoc signature"

"$SCRIPT_DIR/create-install-dmg.sh" \
  --app "$APP_DIR" \
  --output "$DMG_PATH" \
  --volume-name "Codex Pulse"

ZIP_DIGEST=$(shasum -a 256 "$ARCHIVE_PATH" | awk '{print $1}')
DMG_DIGEST=$(shasum -a 256 "$DMG_PATH" | awk '{print $1}')
printf '%s  %s\n%s  %s\n' \
  "$ZIP_DIGEST" "$ARCHIVE_NAME" \
  "$DMG_DIGEST" "$DMG_NAME" >"$CHECKSUM_PATH"

printf '%s\n' \
  "release app assembled: tag=$TAG build=$BUILD_NUMBER arch=arm64 minos=15.0 sparkle_updater=yes adhoc_signed=yes developer_id=no notarized=no" \
  "release update asset: $ARCHIVE_PATH" \
  "release install asset: $DMG_PATH" \
  "release ZIP sha256: $ZIP_DIGEST" \
  "release DMG sha256: $DMG_DIGEST"
