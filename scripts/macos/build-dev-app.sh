#!/usr/bin/env bash

set -euo pipefail

SCRIPT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
REPO_ROOT=$(CDPATH= cd -- "$SCRIPT_DIR/../.." && pwd)
APP_DIR="$REPO_ROOT/build/dev/Codex Pulse.app"
REQUIRE_LAYERED_ICON=0
BUILD_CONFIGURATION=debug

while [ "$#" -gt 0 ]; do
  case "$1" in
    --output)
      [ "$#" -ge 2 ] || { echo "missing value for --output" >&2; exit 2; }
      APP_DIR=$2
      shift 2
      ;;
    --require-layered-icon)
      REQUIRE_LAYERED_ICON=1
      shift
      ;;
    --configuration)
      [ "$#" -ge 2 ] || { echo "missing value for --configuration" >&2; exit 2; }
      case "$2" in
        debug|release) BUILD_CONFIGURATION=$2 ;;
        *) echo "configuration must be debug or release" >&2; exit 2 ;;
      esac
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

SWIFT_DEVELOPER_DIR=${DEVELOPER_DIR:-$(xcode-select -p 2>/dev/null || true)}
[ -n "$SWIFT_DEVELOPER_DIR" ] || {
  echo "unable to resolve the active Xcode developer directory" >&2
  exit 1
}
SWIFT_SDK_PATH=$(DEVELOPER_DIR="$SWIFT_DEVELOPER_DIR" xcrun --sdk macosx --show-sdk-path)
SWIFT_SDK_VERSION=$(DEVELOPER_DIR="$SWIFT_DEVELOPER_DIR" xcrun --sdk macosx --show-sdk-version)
SWIFT_MINIMUM_TARGET=$(plutil -extract LSMinimumSystemVersion raw "$SCRIPT_DIR/Info.plist")
SWIFT_SCRATCH_PATH="$REPO_ROOT/build/dev/swift-build-macos-$SWIFT_SDK_VERSION"
SWIFT_BUILD_ARGUMENTS=(
  --package-path "$REPO_ROOT/app/macos"
  --scratch-path "$SWIFT_SCRATCH_PATH"
  --configuration "$BUILD_CONFIGURATION"
  -Xlinker -platform_version
  -Xlinker macos
  -Xlinker "$SWIFT_MINIMUM_TARGET"
  -Xlinker "$SWIFT_SDK_VERSION"
)
DEVELOPER_DIR="$SWIFT_DEVELOPER_DIR" SDKROOT="$SWIFT_SDK_PATH" \
  xcrun --sdk macosx swift build \
  "${SWIFT_BUILD_ARGUMENTS[@]}" \
  --product codex-pulse-app
make -C "$REPO_ROOT" verify-helper

SWIFT_BIN_DIR=$(DEVELOPER_DIR="$SWIFT_DEVELOPER_DIR" SDKROOT="$SWIFT_SDK_PATH" \
  xcrun --sdk macosx swift build \
  "${SWIFT_BUILD_ARGUMENTS[@]}" \
  --show-bin-path)
APP_EXECUTABLE="$SWIFT_BIN_DIR/codex-pulse-app"
SPARKLE_FRAMEWORK="$SWIFT_BIN_DIR/Sparkle.framework"
LOCALIZATION_BUNDLE=$(find "$SWIFT_BIN_DIR" -maxdepth 1 -type d -name '*CodexPulseAppSupport*.bundle' -print -quit)
HELPER_EXECUTABLE="$REPO_ROOT/bin/codex-pulse"
APP_ICON_SOURCE="$REPO_ROOT/app/macos/Resources/AppIcon/CodexPulse.icon"
APP_ICONSET="$REPO_ROOT/app/macos/Resources/AppIcon/CodexPulse.iconset"
[ -x "$APP_EXECUTABLE" ] || { echo "Swift app executable is missing" >&2; exit 1; }
[ "$(DEVELOPER_DIR="$SWIFT_DEVELOPER_DIR" xcrun vtool -show-build "$APP_EXECUTABLE" | awk '$1 == "sdk" { print $2; exit }')" = "$SWIFT_SDK_VERSION" ] || {
  echo "Swift app executable was not linked against the active macOS SDK $SWIFT_SDK_VERSION" >&2
  exit 1
}
[ -d "$SPARKLE_FRAMEWORK" ] || { echo "Sparkle framework is missing" >&2; exit 1; }
[ -n "$LOCALIZATION_BUNDLE" ] && [ -d "$LOCALIZATION_BUNDLE" ] || {
  echo "App localization resource bundle is missing" >&2
  exit 1
}
LOCALIZATION_ROOT="$LOCALIZATION_BUNDLE"
if [ -d "$LOCALIZATION_BUNDLE/Contents/Resources" ]; then
  LOCALIZATION_ROOT="$LOCALIZATION_BUNDLE/Contents/Resources"
fi
[ -x "$HELPER_EXECUTABLE" ] || { echo "Go Helper executable is missing" >&2; exit 1; }
[ -f "$APP_ICON_SOURCE/icon.json" ] || { echo "Codex Pulse Icon Composer source is missing" >&2; exit 1; }
[ -d "$APP_ICONSET" ] || { echo "Codex Pulse AppIcon iconset is missing" >&2; exit 1; }
command -v iconutil >/dev/null 2>&1 || { echo "iconutil is unavailable" >&2; exit 1; }

rm -rf -- "$APP_DIR"
mkdir -p \
  "$APP_DIR/Contents/MacOS" \
  "$APP_DIR/Contents/Helpers" \
  "$APP_DIR/Contents/Resources" \
  "$APP_DIR/Contents/Frameworks"
cp "$SCRIPT_DIR/Info.plist" "$APP_DIR/Contents/Info.plist"
plutil -replace CFBundleDisplayName -string "Codex Pulse Development" "$APP_DIR/Contents/Info.plist"
plutil -replace CFBundleIdentifier -string "com.sisyphussq.codex-pulse.development" "$APP_DIR/Contents/Info.plist"
plutil -replace CFBundleShortVersionString -string "0.0.0" "$APP_DIR/Contents/Info.plist"
plutil -replace CFBundleVersion -string "1" "$APP_DIR/Contents/Info.plist"
plutil -replace CodexPulseProductVersion -string "0.0.0-dev" "$APP_DIR/Contents/Info.plist"
cp "$APP_EXECUTABLE" "$APP_DIR/Contents/MacOS/Codex Pulse"
cp "$HELPER_EXECUTABLE" "$APP_DIR/Contents/Helpers/codex-pulse"
ditto "$SPARKLE_FRAMEWORK" "$APP_DIR/Contents/Frameworks/Sparkle.framework"
iconutil -c icns "$APP_ICONSET" -o "$APP_DIR/Contents/Resources/CodexPulse.icns"
for localization_mapping in en.lproj:en.lproj zh-Hans.lproj:zh-hans.lproj; do
  localization_source=${localization_mapping%%:*}
  localization_destination=${localization_mapping#*:}
  [ -d "$LOCALIZATION_ROOT/$localization_source" ] || {
    echo "App localization directory is missing: $localization_source" >&2
    exit 1
  }
  ditto "$LOCALIZATION_ROOT/$localization_source" \
    "$APP_DIR/Contents/Resources/$localization_destination"
done

ICON_PIPELINE=icns-fallback
XCODE_DEVELOPER_DIR=""
if [ -n "${CODEX_PULSE_XCODE_DEVELOPER_DIR:-}" ]; then
  XCODE_CANDIDATES=("$CODEX_PULSE_XCODE_DEVELOPER_DIR")
else
  XCODE_CANDIDATES=(
    "$SWIFT_DEVELOPER_DIR"
    "/Applications/Xcode.app/Contents/Developer"
  )
fi
for candidate in "${XCODE_CANDIDATES[@]}"
do
  [ -n "$candidate" ] || continue
  if [ -x "$candidate/usr/bin/actool" ]; then
    XCODE_DEVELOPER_DIR=$candidate
    break
  fi
done

if [ -n "$XCODE_DEVELOPER_DIR" ] && \
   DEVELOPER_DIR="$XCODE_DEVELOPER_DIR" xcrun actool --version >/dev/null 2>&1
then
  ASSET_INFO_PLIST="$APP_DIR/Contents/IconComposerAssetInfo.plist"
  MINIMUM_TARGET=$(plutil -extract LSMinimumSystemVersion raw "$APP_DIR/Contents/Info.plist")
  DEVELOPER_DIR="$XCODE_DEVELOPER_DIR" xcrun actool \
    --compile "$APP_DIR/Contents/Resources" \
    --output-format human-readable-text \
    --warnings \
    --notices \
    --errors \
    --output-partial-info-plist "$ASSET_INFO_PLIST" \
    --app-icon CodexPulse \
    --include-all-app-icons \
    --platform macosx \
    --minimum-deployment-target "$MINIMUM_TARGET" \
    --target-device mac \
    --standalone-icon-behavior all \
    --skip-app-store-deployment \
    "$APP_ICON_SOURCE"
  [ "$(plutil -extract CFBundleIconFile raw "$ASSET_INFO_PLIST")" = "CodexPulse" ] || {
    echo "Icon Composer emitted an unexpected CFBundleIconFile" >&2
    exit 1
  }
  [ "$(plutil -extract CFBundleIconName raw "$ASSET_INFO_PLIST")" = "CodexPulse" ] || {
    echo "Icon Composer emitted an unexpected CFBundleIconName" >&2
    exit 1
  }
  rm -f -- "$ASSET_INFO_PLIST"
  [ -s "$APP_DIR/Contents/Resources/Assets.car" ] || {
    echo "Icon Composer Assets.car is missing" >&2
    exit 1
  }
  ICON_PIPELINE=icon-composer
elif [ "$REQUIRE_LAYERED_ICON" -eq 1 ]; then
  echo "layered icon required, but a ready full Xcode actool is unavailable" >&2
  echo "initialize Xcode with: DEVELOPER_DIR=/Applications/Xcode.app/Contents/Developer xcodebuild -runFirstLaunch" >&2
  exit 1
fi

chmod 0755 "$APP_DIR/Contents/MacOS/Codex Pulse" "$APP_DIR/Contents/Helpers/codex-pulse"
codesign --verify --deep --strict \
  "$APP_DIR/Contents/Frameworks/Sparkle.framework"
otool -l "$APP_DIR/Contents/MacOS/Codex Pulse" |
  grep -Fq '@executable_path/../Frameworks' || {
    echo "Swift app executable is missing the standard Frameworks rpath" >&2
    exit 1
  }
plutil -lint "$APP_DIR/Contents/Info.plist" >/dev/null
[ -s "$APP_DIR/Contents/Resources/CodexPulse.icns" ] || { echo "Codex Pulse AppIcon ICNS is missing" >&2; exit 1; }
[ "$(plutil -extract CFBundleIconName raw "$APP_DIR/Contents/Info.plist")" = "CodexPulse" ] || {
  echo "Codex Pulse CFBundleIconName is invalid" >&2
  exit 1
}

printf 'development app assembled: executable=present helper=present sparkle=embedded icon_pipeline=%s configuration=%s signed=no distribution=no\n' "$ICON_PIPELINE" "$BUILD_CONFIGURATION"
