#!/usr/bin/env bash

set -euo pipefail

fail() {
    printf 'verify_release: %s\n' "$*" >&2
    exit 1
}

[[ $# -eq 6 ]] || fail "usage: $0 <dir> <version> <build> <feed-url> <download-prefix> <public-key>"
release_dir=$1
version=$2
build=$3
feed_url=$4
download_prefix=$5
public_key=$6
archive="$release_dir/Codex-Pulse-$version-arm64.zip"

[[ -d "$release_dir" && ! -L "$release_dir" ]] || fail "release directory is invalid"
[[ "$version" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]] || fail "version must contain exactly three numeric components"
[[ "$build" =~ ^[0-9]+$ ]] || fail "build must be a non-negative integer"
[[ -s "$archive" ]] || fail "release archive is missing"

go run ./scripts/sparkle/releaseverify \
    -dir "$release_dir" \
    -version "$version" \
    -build "$build" \
    -feed-url "$feed_url" \
    -download-prefix "${download_prefix%/}" \
    -public-key "$public_key"

tmp_dir=$(mktemp -d "${TMPDIR:-/tmp}/codex-pulse-release-verify.XXXXXX")
trap 'rm -rf "$tmp_dir"' EXIT
ditto -x -k "$archive" "$tmp_dir"
build/darwin/verify_bundle.sh \
    "$tmp_dir/Codex Pulse.app" \
    com.sisyphussq.codexpulse \
    "$version" "$build" 15.0.0
[[ "$(plutil -extract SUFeedURL raw -o - "$tmp_dir/Codex Pulse.app/Contents/Info.plist")" == "$feed_url" ]] \
    || fail "bundle feed URL mismatch"
[[ "$(plutil -extract SUPublicEDKey raw -o - "$tmp_dir/Codex Pulse.app/Contents/Info.plist")" == "$public_key" ]] \
    || fail "bundle public key mismatch"
