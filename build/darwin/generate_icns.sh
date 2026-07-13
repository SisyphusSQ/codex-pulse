#!/usr/bin/env bash

set -euo pipefail

usage() {
    echo "usage: $0 <frozen-icons-dir> <output.icns>" >&2
    exit 64
}

fail() {
    echo "generate_icns: $*" >&2
    exit 1
}

[[ $# -eq 2 ]] || usage

source_dir=$1
output_path=$2
[[ "$output_path" == "bin/.packaging/icons.icns" ]] \
    || fail "output path is fixed to bin/.packaging/icons.icns"
source_1024="$source_dir/codex-pulse-app-icon-1024.png"
source_64="$source_dir/codex-pulse-app-icon-64.png"
source_32="$source_dir/codex-pulse-app-icon-32.png"
source_16="$source_dir/codex-pulse-app-icon-16.png"

for tool in iconutil sips; do
    command -v "$tool" >/dev/null 2>&1 || fail "missing required tool: $tool"
done

pixel_size() {
    sips -g pixelWidth -g pixelHeight "$1" 2>/dev/null \
        | awk '/pixelWidth:/ { width=$2 } /pixelHeight:/ { height=$2 } END { print width "x" height }'
}

png_alpha() {
    sips -g format -g hasAlpha "$1" 2>/dev/null \
        | awk '/format:/ { format=$2 } /hasAlpha:/ { alpha=$2 } END { print format ":" alpha }'
}

require_source() {
    local path=$1
    local expected=$2
    [[ -f "$path" ]] || fail "missing frozen icon: $path"
    [[ "$(pixel_size "$path")" == "${expected}x${expected}" ]] \
        || fail "unexpected frozen icon size for $path: expected ${expected}x${expected}"
    [[ "$(png_alpha "$path")" == "png:yes" ]] \
        || fail "frozen icon must be a PNG with alpha: $path"
}

require_source "$source_1024" 1024
require_source "$source_64" 64
require_source "$source_32" 32
require_source "$source_16" 16

tmp_dir=$(mktemp -d "${TMPDIR:-/tmp}/codex-pulse-iconset.XXXXXX")
trap 'rm -rf "$tmp_dir"' EXIT
iconset="$tmp_dir/CodexPulse.iconset"
mkdir -p "$iconset" "$(dirname "$output_path")"

cp "$source_16" "$iconset/icon_16x16.png"
cp "$source_32" "$iconset/icon_16x16@2x.png"
cp "$source_32" "$iconset/icon_32x32.png"
cp "$source_64" "$iconset/icon_32x32@2x.png"

resize_from_master() {
    local size=$1
    local output=$2
    sips -z "$size" "$size" "$source_1024" --out "$output" >/dev/null
    [[ "$(pixel_size "$output")" == "${size}x${size}" ]] \
        || fail "generated icon has unexpected size: $output"
    [[ "$(png_alpha "$output")" == "png:yes" ]] \
        || fail "generated icon must retain PNG alpha: $output"
}

resize_from_master 128 "$iconset/icon_128x128.png"
resize_from_master 256 "$iconset/icon_128x128@2x.png"
resize_from_master 256 "$iconset/icon_256x256.png"
resize_from_master 512 "$iconset/icon_256x256@2x.png"
resize_from_master 512 "$iconset/icon_512x512.png"
cp "$source_1024" "$iconset/icon_512x512@2x.png"

rm -f "$output_path"
iconutil -c icns "$iconset" -o "$output_path"
[[ -s "$output_path" ]] || fail "iconutil did not create $output_path"

echo "generated $output_path from frozen icon assets"
