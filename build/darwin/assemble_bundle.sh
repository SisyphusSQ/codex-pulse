#!/usr/bin/env bash

set -euo pipefail

usage() {
    echo "usage: $0 <binary> <icon.icns> <prepared-tray-dir> <Info.plist> <Sparkle.framework> <bundle.app> <version> <build-number> [feed-url public-key]" >&2
    exit 64
}

fail() {
    echo "assemble_bundle: $*" >&2
    exit 1
}

[[ $# -eq 8 || $# -eq 10 ]] || usage

binary_path=$1
icon_path=$2
prepared_tray_dir=$3
plist_template=$4
sparkle_framework=$5
bundle_path=$6
app_version=$7
build_number=$8
feed_url=${9:-}
public_key=${10:-}
tray_icon="$prepared_tray_dir/codex-pulse-tray-template.png"
tray_icon_2x="$prepared_tray_dir/codex-pulse-tray-template@2x.png"

[[ "$bundle_path" == "bin/Codex Pulse.app" ]] \
    || fail "bundle output is fixed to bin/Codex Pulse.app"
[[ "$app_version" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]] \
    || fail "APP_VERSION must contain exactly three numeric components"
[[ "$build_number" =~ ^[0-9]+$ ]] \
    || fail "BUILD_NUMBER must be a non-negative integer"
if [[ -n "$feed_url" || -n "$public_key" ]]; then
    [[ "$feed_url" =~ ^https://[^[:space:]@]+$ ]] || fail "SUFeedURL must be an HTTPS URL without userinfo"
    [[ "$public_key" != *[[:space:]]* ]] || fail "SUPublicEDKey must not contain whitespace"
    [[ $(printf '%s' "$public_key" | base64 -D 2>/dev/null | wc -c | tr -d ' ') -eq 32 ]] \
        || fail "SUPublicEDKey must decode to 32 bytes"
fi
[[ "$(uname -s)" == Darwin ]] || fail "bundle assembly requires macOS"

for path in "$binary_path" "$icon_path" "$tray_icon" "$tray_icon_2x" "$plist_template"; do
    [[ -f "$path" ]] || fail "required input does not exist: $path"
done
[[ -d "$sparkle_framework" ]] || fail "Sparkle.framework does not exist: $sparkle_framework"
[[ -x "$sparkle_framework/Versions/B/Sparkle" ]] || fail "Sparkle.framework binary is missing"
[[ -x "$binary_path" ]] || fail "application binary is not executable: $binary_path"
plutil -lint "$plist_template" >/dev/null || fail "invalid Info.plist template"

bundle_parent=$(dirname "$bundle_path")
staging_path="${bundle_path}.staging.$$"
trap 'rm -rf "$staging_path"' EXIT
rm -rf "$staging_path"
mkdir -p "$staging_path/Contents/MacOS" "$staging_path/Contents/Resources" "$staging_path/Contents/Frameworks" "$bundle_parent"

cp "$plist_template" "$staging_path/Contents/Info.plist"
plutil -replace CFBundleShortVersionString -string "$app_version" "$staging_path/Contents/Info.plist"
plutil -replace CFBundleVersion -string "$build_number" "$staging_path/Contents/Info.plist"
if [[ -n "$feed_url" ]]; then
    plutil -insert SUFeedURL -string "$feed_url" "$staging_path/Contents/Info.plist"
    plutil -insert SUPublicEDKey -string "$public_key" "$staging_path/Contents/Info.plist"
fi

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
ditto "$sparkle_framework" "$staging_path/Contents/Frameworks/Sparkle.framework"

if command -v xattr >/dev/null 2>&1; then
    xattr -cr "$staging_path"
fi
plutil -lint "$staging_path/Contents/Info.plist" >/dev/null

rm -rf "$bundle_path"
mv "$staging_path" "$bundle_path"
trap - EXIT

echo "assembled $bundle_path (version=$app_version, build=$build_number)"
