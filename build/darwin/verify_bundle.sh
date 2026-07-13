#!/usr/bin/env bash

set -euo pipefail

usage() {
    echo "usage: $0 <bundle.app> <bundle-id> <version> <build-number> <minimum-macos> [archive.zip]" >&2
    exit 64
}

fail() {
    echo "verify_bundle: $*" >&2
    exit 1
}

[[ $# -eq 5 || $# -eq 6 ]] || usage

bundle_path=$1
expected_identifier=$2
expected_version=$3
expected_build=$4
expected_min_os=$5
archive_path=${6:-}
plist_path="$bundle_path/Contents/Info.plist"

for tool in codesign ditto file iconutil lipo plutil sips unzip vtool; do
    command -v "$tool" >/dev/null 2>&1 || fail "missing required tool: $tool"
done

[[ -d "$bundle_path" ]] || fail "bundle does not exist: $bundle_path"
[[ -f "$plist_path" ]] || fail "bundle Info.plist does not exist"
plutil -lint "$plist_path" >/dev/null || fail "bundle Info.plist is invalid"

plist_value() {
    plutil -extract "$1" raw -o - "$plist_path"
}

assert_equal() {
    local label=$1
    local actual=$2
    local expected=$3
    [[ "$actual" == "$expected" ]] || fail "$label mismatch: expected '$expected', got '$actual'"
}

bundle_name=$(basename "$bundle_path" .app)
executable_name=$(plist_value CFBundleExecutable)
icon_name=$(plist_value CFBundleIconFile)

assert_equal CFBundlePackageType "$(plist_value CFBundlePackageType)" APPL
assert_equal CFBundleName "$(plist_value CFBundleName)" "$bundle_name"
assert_equal CFBundleDisplayName "$(plist_value CFBundleDisplayName)" "$bundle_name"
assert_equal CFBundleExecutable "$executable_name" "$bundle_name"
assert_equal CFBundleIdentifier "$(plist_value CFBundleIdentifier)" "$expected_identifier"
assert_equal CFBundleShortVersionString "$(plist_value CFBundleShortVersionString)" "$expected_version"
assert_equal CFBundleVersion "$(plist_value CFBundleVersion)" "$expected_build"
assert_equal LSMinimumSystemVersion "$(plist_value LSMinimumSystemVersion)" "$expected_min_os"

executable_path="$bundle_path/Contents/MacOS/$executable_name"
icon_path="$bundle_path/Contents/Resources/${icon_name%.icns}.icns"
[[ -x "$executable_path" ]] || fail "bundle executable is missing or not executable"
[[ -s "$icon_path" ]] || fail "bundle icon is missing or empty"

file "$executable_path" | grep -Eq 'Mach-O .* arm64' \
    || fail "bundle executable is not a Mach-O arm64 binary"
assert_equal "Mach-O architectures" "$(lipo -archs "$executable_path")" arm64

build_info=$(vtool -show-build "$executable_path")
echo "$build_info" | awk '$1 == "platform" && $2 == "MACOS" { found=1 } END { exit !found }' \
    || fail "bundle executable does not declare the macOS platform"
binary_min_os=$(echo "$build_info" | awk '$1 == "minos" { print $2; exit }')
expected_binary_min_os=${expected_min_os%.0}
assert_equal "Mach-O minimum macOS" "$binary_min_os" "$expected_binary_min_os"

codesign --verify --deep --strict --verbose=4 "$bundle_path"
codesign_details=$(codesign -dv --verbose=4 "$bundle_path" 2>&1)
echo "$codesign_details" | grep -Fq 'Signature=adhoc' \
    || fail "bundle signature is not ad-hoc"

tmp_dir=$(mktemp -d "${TMPDIR:-/tmp}/codex-pulse-bundle-verify.XXXXXX")
trap 'rm -rf "$tmp_dir"' EXIT
iconutil -c iconset "$icon_path" -o "$tmp_dir/Verified.iconset"
while read -r icon_file expected_size; do
    [[ -s "$tmp_dir/Verified.iconset/$icon_file" ]] \
        || fail "icns is missing required representation: $icon_file"
    actual_size=$(sips -g pixelWidth -g pixelHeight "$tmp_dir/Verified.iconset/$icon_file" 2>/dev/null \
        | awk '/pixelWidth:/ { width=$2 } /pixelHeight:/ { height=$2 } END { print width "x" height }')
    assert_equal "$icon_file size" "$actual_size" "${expected_size}x${expected_size}"
done <<'EOF'
icon_16x16.png 16
icon_16x16@2x.png 32
icon_32x32.png 32
icon_32x32@2x.png 64
icon_128x128.png 128
icon_128x128@2x.png 256
icon_256x256.png 256
icon_256x256@2x.png 512
icon_512x512.png 512
icon_512x512@2x.png 1024
EOF

if [[ -n "$archive_path" ]]; then
    [[ -s "$archive_path" ]] || fail "archive does not exist or is empty: $archive_path"
    archive_entries=$(unzip -Z1 "$archive_path")
    forbidden_entry=$(printf '%s\n' "$archive_entries" \
        | awk -F/ '$1 == "__MACOSX" || $NF ~ /^\._/ { print; exit }')
    [[ -z "$forbidden_entry" ]] \
        || fail "archive contains forbidden AppleDouble metadata: $forbidden_entry"
    top_levels=$(printf '%s\n' "$archive_entries" | awk -F/ 'NF { print $1 }' | sort -u)
    assert_equal "archive top-level entry" "$top_levels" "$(basename "$bundle_path")"

    extracted_dir="$tmp_dir/extracted"
    mkdir -p "$extracted_dir"
    ditto -x -k "$archive_path" "$extracted_dir"
    extracted_bundle="$extracted_dir/$(basename "$bundle_path")"
    [[ -d "$extracted_bundle" ]] || fail "archive did not extract the expected app bundle"
    "$0" \
        "$extracted_bundle" \
        "$expected_identifier" \
        "$expected_version" \
        "$expected_build" \
        "$expected_min_os"
fi

echo "verified $bundle_path (arm64, minOS=$expected_min_os, ad-hoc)${archive_path:+ and $archive_path}"
