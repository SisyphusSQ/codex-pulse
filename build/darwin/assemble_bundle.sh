#!/usr/bin/env bash

set -euo pipefail

usage() {
    echo "usage: $0 <binary> <icon.icns> <prepared-tray-dir> <Info.plist> <bundle.app> <version> <build-number>" >&2
    exit 64
}

fail() {
    echo "assemble_bundle: $*" >&2
    exit 1
}

[[ $# -eq 7 ]] || usage

binary_path=$1
icon_path=$2
prepared_tray_dir=$3
plist_template=$4
bundle_path=$5
app_version=$6
build_number=$7
tray_icon="$prepared_tray_dir/codex-pulse-tray-template.png"
tray_icon_2x="$prepared_tray_dir/codex-pulse-tray-template@2x.png"

[[ "$bundle_path" == "bin/Codex Pulse.app" ]] \
    || fail "bundle output is fixed to bin/Codex Pulse.app"
[[ "$app_version" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]] \
    || fail "APP_VERSION must contain exactly three numeric components"
[[ "$build_number" =~ ^[0-9]+$ ]] \
    || fail "BUILD_NUMBER must be a non-negative integer"
[[ "$(uname -s)" == Darwin ]] || fail "bundle assembly requires macOS"

for path in "$binary_path" "$icon_path" "$tray_icon" "$tray_icon_2x" "$plist_template"; do
    [[ -f "$path" ]] || fail "required input does not exist: $path"
done
[[ -x "$binary_path" ]] || fail "application binary is not executable: $binary_path"
plutil -lint "$plist_template" >/dev/null || fail "invalid Info.plist template"

bundle_parent=$(dirname "$bundle_path")
staging_path="${bundle_path}.staging.$$"
trap 'rm -rf "$staging_path"' EXIT
rm -rf "$staging_path"
mkdir -p "$staging_path/Contents/MacOS" "$staging_path/Contents/Resources" "$bundle_parent"

cp "$plist_template" "$staging_path/Contents/Info.plist"
plutil -replace CFBundleShortVersionString -string "$app_version" "$staging_path/Contents/Info.plist"
plutil -replace CFBundleVersion -string "$build_number" "$staging_path/Contents/Info.plist"

executable_name=$(plutil -extract CFBundleExecutable raw -o - "$staging_path/Contents/Info.plist")
icon_name=$(plutil -extract CFBundleIconFile raw -o - "$staging_path/Contents/Info.plist")
[[ -n "$executable_name" && "$executable_name" != */* ]] \
    || fail "CFBundleExecutable must be a single file name"
[[ -n "$icon_name" && "$icon_name" != */* ]] \
    || fail "CFBundleIconFile must be a single resource name"
icon_name=${icon_name%.icns}.icns

cp "$binary_path" "$staging_path/Contents/MacOS/$executable_name"
chmod 0755 "$staging_path/Contents/MacOS/$executable_name"
cp "$icon_path" "$staging_path/Contents/Resources/$icon_name"
cp "$tray_icon" "$staging_path/Contents/Resources/codex-pulse-tray-template.png"
cp "$tray_icon_2x" "$staging_path/Contents/Resources/codex-pulse-tray-template@2x.png"

if command -v xattr >/dev/null 2>&1; then
    xattr -cr "$staging_path"
fi
plutil -lint "$staging_path/Contents/Info.plist" >/dev/null

rm -rf "$bundle_path"
mv "$staging_path" "$bundle_path"
trap - EXIT

echo "assembled $bundle_path (version=$app_version, build=$build_number)"
