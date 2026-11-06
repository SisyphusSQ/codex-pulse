#!/usr/bin/env bash

set -euo pipefail

SCRIPT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
REPO_ROOT=$(CDPATH= cd -- "$SCRIPT_DIR/../.." && pwd)
VERSION=""
BUILD_NUMBER=""
SPARKLE_FEED_URL=""
SPARKLE_PUBLIC_KEY_FILE=""
RUN_APP_TESTS=true
DISTRIBUTION=signed-notarized
SIGNING_IDENTITY=""
NOTARY_PROFILE=""

usage() {
  cat <<'EOF'
usage: build-release-app.sh --version <semver> --build-number <positive-integer>
  --sparkle-feed-url <https-url> --sparkle-public-key-file <path>
  [--distribution signed-notarized|unsigned]
  [--signing-identity <Developer-ID-SHA1> --notary-profile <Keychain-profile>]
  [--skip-app-tests]

Builds Developer ID signed and notarized Apple Silicon assets by default under:
  .artifacts/releases/v<semver>/
Unsigned assets require explicit --distribution unsigned. No automatic fallback.
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
    --skip-app-tests)
      RUN_APP_TESTS=false
      shift
      ;;
    --distribution|--signing-identity|--notary-profile)
      [ "$#" -ge 2 ] || fail "missing value for $1"
      case "$1" in
        --distribution) DISTRIBUTION=$2 ;;
        --signing-identity) SIGNING_IDENTITY=$2 ;;
        --notary-profile) NOTARY_PROFILE=$2 ;;
      esac
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

case "$DISTRIBUTION" in
  signed-notarized)
    [[ "$SIGNING_IDENTITY" =~ ^[0-9A-Fa-f]{40}$ && -n "$NOTARY_PROFILE" ]] ||
      fail "signed-notarized requires --signing-identity and --notary-profile"
    ;;
  unsigned)
    [[ -z "$SIGNING_IDENTITY" && -z "$NOTARY_PROFILE" ]] ||
      fail "unsigned distribution cannot accept signing or notarization arguments"
    ;;
  *) fail "distribution must be signed-notarized or unsigned" ;;
esac

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

for tool in swift xcode-select xcrun go plutil iconutil lipo otool vtool strip codesign ditto hdiutil unzip shasum strings python3; do
  command -v "$tool" >/dev/null 2>&1 || fail "required tool is unavailable: $tool"
done

if [[ "$DISTRIBUTION" == signed-notarized ]]; then
  # Authentication must work unattended before a long build or any submission.
  xcrun notarytool history --keychain-profile "$NOTARY_PROFILE" \
    --output-format json </dev/null >/dev/null ||
    fail "notarization profile is unavailable; configure Keychain credentials before building"
fi

SWIFT_DEVELOPER_DIR=${DEVELOPER_DIR:-$(xcode-select -p 2>/dev/null || true)}
[ -n "$SWIFT_DEVELOPER_DIR" ] || fail "unable to resolve the active Xcode developer directory"
SWIFT_SDK_PATH=$(DEVELOPER_DIR="$SWIFT_DEVELOPER_DIR" xcrun --sdk macosx --show-sdk-path) ||
  fail "unable to resolve the active macOS SDK path"
SWIFT_SDK_VERSION=$(DEVELOPER_DIR="$SWIFT_DEVELOPER_DIR" xcrun --sdk macosx --show-sdk-version) ||
  fail "unable to resolve the active macOS SDK version"
SWIFT_MINIMUM_TARGET=$(plutil -extract LSMinimumSystemVersion raw "$SCRIPT_DIR/Info.plist")

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
  local status=$?
  if [ -n "$BUILD_ROOT" ] && [ -d "$BUILD_ROOT" ]; then
    if [[ "$status" == 0 ]]; then
      rm -rf -- "$BUILD_ROOT"
    else
      printf 'release build retained for recovery: %s\n' "$BUILD_ROOT" >&2
    fi
  fi
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

umask 077
mkdir -p "$RELEASE_DIR"
for output in "$ARCHIVE_PATH" "$DMG_PATH" "$CHECKSUM_PATH"; do
  [[ ! -e "$output" ]] || fail "release output already exists; inspect before continuing: $output"
done
BUILD_ROOT=$(mktemp -d "$RELEASE_DIR/.build.XXXXXX")
APP_DIR="$BUILD_ROOT/Codex Pulse.app"
EXTRACT_DIR="$BUILD_ROOT/extracted"
HELPER_EXECUTABLE="$BUILD_ROOT/codex-pulse"

SWIFT_RELEASE_ARGUMENTS=(
  --package-path "$REPO_ROOT/app/macos"
  --scratch-path "$BUILD_ROOT/swift-build"
  --configuration release
  -Xlinker -platform_version
  -Xlinker macos
  -Xlinker "$SWIFT_MINIMUM_TARGET"
  -Xlinker "$SWIFT_SDK_VERSION"
  -Xswiftc -gline-tables-only
  -Xcc "-ffile-prefix-map=$REPO_ROOT=."
  -Xcc "-fmacro-prefix-map=$REPO_ROOT=."
)

swift_release() {
  DEVELOPER_DIR="$SWIFT_DEVELOPER_DIR" SDKROOT="$SWIFT_SDK_PATH" \
    xcrun --sdk macosx swift "$@"
}

printf 'release SDK: developer_dir=%s sdk=%s version=%s minimum=%s\n' \
  "$SWIFT_DEVELOPER_DIR" "$SWIFT_SDK_PATH" "$SWIFT_SDK_VERSION" "$SWIFT_MINIMUM_TARGET"

if [ "$RUN_APP_TESTS" = true ]; then
  swift_release run \
    "${SWIFT_RELEASE_ARGUMENTS[@]}" \
    codex-pulse-app-tests
fi
swift_release build \
  "${SWIFT_RELEASE_ARGUMENTS[@]}" \
  --product codex-pulse-app

SWIFT_BIN_DIR=$(swift_release build \
  "${SWIFT_RELEASE_ARGUMENTS[@]}" \
  --show-bin-path)
APP_EXECUTABLE="$SWIFT_BIN_DIR/codex-pulse-app"
SPARKLE_FRAMEWORK="$SWIFT_BIN_DIR/Sparkle.framework"
LOCALIZATION_BUNDLE=$(find "$SWIFT_BIN_DIR" -maxdepth 1 -type d -name '*CodexPulseAppSupport*.bundle' -print -quit)
[ -x "$APP_EXECUTABLE" ] || fail "Swift release executable is missing"
[ -d "$SPARKLE_FRAMEWORK" ] || fail "Sparkle framework is missing"
[ -n "$LOCALIZATION_BUNDLE" ] && [ -d "$LOCALIZATION_BUNDLE" ] || fail "App localization resource bundle is missing"
APP_SDK_VERSION=$(vtool -show-build "$APP_EXECUTABLE" | awk '$1 == "sdk" { print $2; exit }')
[ "$APP_SDK_VERSION" = "$SWIFT_SDK_VERSION" ] ||
  fail "Swift release executable SDK $APP_SDK_VERSION does not match selected SDK $SWIFT_SDK_VERSION"
LOCALIZATION_RESOURCES="$LOCALIZATION_BUNDLE"
if [ -d "$LOCALIZATION_BUNDLE/Contents/Resources/en.lproj" ]; then
  LOCALIZATION_RESOURCES="$LOCALIZATION_BUNDLE/Contents/Resources"
fi

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
for localization_directory in en.lproj zh-Hans.lproj; do
  [ -d "$LOCALIZATION_RESOURCES/$localization_directory" ] || \
    fail "App localization directory is missing: $localization_directory"
  ditto "$LOCALIZATION_RESOURCES/$localization_directory" \
    "$APP_DIR/Contents/Resources/$localization_directory"
done

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
APP_BUILD_VERSION=$(vtool -show-build "$APP_DIR/Contents/MacOS/Codex Pulse")
APP_MINIMUM_TARGET=$(awk '$1 == "minos" { print $2; exit }' <<<"$APP_BUILD_VERSION")
APP_SDK_VERSION=$(awk '$1 == "sdk" { print $2; exit }' <<<"$APP_BUILD_VERSION")
[ "$APP_MINIMUM_TARGET" = "$SWIFT_MINIMUM_TARGET" ] ||
  fail "Swift executable minimum macOS version $APP_MINIMUM_TARGET does not match bundle minimum $SWIFT_MINIMUM_TARGET"
[ "$APP_SDK_VERSION" = "$SWIFT_SDK_VERSION" ] ||
  fail "Swift executable SDK $APP_SDK_VERSION does not match selected SDK $SWIFT_SDK_VERSION"
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

if [[ "$DISTRIBUTION" == signed-notarized ]]; then
  bash "$SCRIPT_DIR/sign-release-app.sh" "$APP_DIR" "$SIGNING_IDENTITY"
  ditto -c -k --keepParent "$APP_DIR" "$BUILD_ROOT/notarization.zip"
  bash "$SCRIPT_DIR/notarize-release-archive.sh" \
    "$BUILD_ROOT/notarization.zip" "$NOTARY_PROFILE" "$RELEASE_DIR/notary-app"
  xcrun stapler staple "$APP_DIR"
  xcrun stapler validate "$APP_DIR"
  spctl --assess --type execute --verbose=4 "$APP_DIR"
else
  codesign --force --sign - --timestamp=none \
    "$APP_DIR/Contents/Helpers/codex-pulse"
  codesign --force --sign - --timestamp=none "$APP_DIR"
fi
codesign --verify --deep --strict --verbose=2 "$APP_DIR"
APP_CODESIGN_DETAILS=$(codesign -dv --verbose=2 "$APP_DIR" 2>&1)
if [[ "$DISTRIBUTION" == unsigned ]]; then
  grep -Fq 'Signature=adhoc' <<<"$APP_CODESIGN_DETAILS" ||
    fail "release App does not have a complete ad-hoc signature"
fi

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
EXTRACTED_SDK_VERSION=$(vtool -show-build "$EXTRACT_DIR/Codex Pulse.app/Contents/MacOS/Codex Pulse" | awk '$1 == "sdk" { print $2; exit }')
[ "$EXTRACTED_SDK_VERSION" = "$SWIFT_SDK_VERSION" ] ||
  fail "extracted App SDK $EXTRACTED_SDK_VERSION does not match selected SDK $SWIFT_SDK_VERSION"
[ "$(lipo -archs "$EXTRACT_DIR/Codex Pulse.app/Contents/Helpers/codex-pulse")" = "arm64" ] ||
  fail "extracted Helper architecture readback failed"
[ -x "$EXTRACT_DIR/Codex Pulse.app/Contents/Frameworks/Sparkle.framework/Versions/B/Sparkle" ] ||
  fail "extracted Sparkle framework is missing"
codesign --verify --deep --strict --verbose=2 \
  "$EXTRACT_DIR/Codex Pulse.app"
EXTRACTED_CODESIGN_DETAILS=$(
  codesign -dv --verbose=2 "$EXTRACT_DIR/Codex Pulse.app" 2>&1
)
if [[ "$DISTRIBUTION" == signed-notarized ]]; then
  grep -Fq 'Authority=Developer ID Application:' <<<"$EXTRACTED_CODESIGN_DETAILS" ||
    fail "extracted release App lost its Developer ID signature"
  xcrun stapler validate "$EXTRACT_DIR/Codex Pulse.app"
  spctl --assess --type execute --verbose=4 "$EXTRACT_DIR/Codex Pulse.app"
else
  grep -Fq 'Signature=adhoc' <<<"$EXTRACTED_CODESIGN_DETAILS" ||
    fail "extracted release App lost its ad-hoc signature"
fi

"$SCRIPT_DIR/create-install-dmg.sh" \
  --app "$APP_DIR" \
  --output "$DMG_PATH" \
  --volume-name "Codex Pulse"

if [[ "$DISTRIBUTION" == signed-notarized ]]; then
  codesign --force --sign "$SIGNING_IDENTITY" --timestamp "$DMG_PATH"
  bash "$SCRIPT_DIR/notarize-release-archive.sh" \
    "$DMG_PATH" "$NOTARY_PROFILE" "$RELEASE_DIR/notary-dmg"
  xcrun stapler staple "$DMG_PATH"
  xcrun stapler validate "$DMG_PATH"
  codesign --verify --strict "$DMG_PATH"
  spctl --assess --type open --context context:primary-signature --verbose=4 "$DMG_PATH"
fi

ZIP_DIGEST=$(shasum -a 256 "$ARCHIVE_PATH" | awk '{print $1}')
DMG_DIGEST=$(shasum -a 256 "$DMG_PATH" | awk '{print $1}')
printf '%s  %s\n%s  %s\n' \
  "$ZIP_DIGEST" "$ARCHIVE_NAME" \
  "$DMG_DIGEST" "$DMG_NAME" >"$CHECKSUM_PATH"

printf '%s\n' \
  "release app assembled: tag=$TAG build=$BUILD_NUMBER arch=arm64 minos=15.0 sparkle_updater=yes distribution=$DISTRIBUTION app_tests=$RUN_APP_TESTS" \
  "release SDK readback: selected=$SWIFT_SDK_VERSION app=$APP_SDK_VERSION zip=$EXTRACTED_SDK_VERSION" \
  "release update asset: $ARCHIVE_PATH" \
  "release install asset: $DMG_PATH" \
  "release ZIP sha256: $ZIP_DIGEST" \
  "release DMG sha256: $DMG_DIGEST"
