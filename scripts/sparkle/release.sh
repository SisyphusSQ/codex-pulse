#!/usr/bin/env bash

set -euo pipefail

fail() {
    printf 'release: %s\n' "$*" >&2
    exit 1
}

[[ $# -eq 6 ]] || fail "usage: $0 <version> <build-number> <feed-url> <download-url-prefix> <public-key> <release-notes-file>"
version=$1
build_number=$2
feed_url=$3
download_prefix=${4%/}
public_key=$5
release_notes=$6
output_dir=dist/update
lock_file=dist/.update.lock

[[ ! -t 0 ]] || fail "private EdDSA key must be provided on stdin"
IFS= read -r private_key || true
[[ -n "$private_key" && ${#private_key} -le 4096 ]] || fail "private EdDSA key stdin is empty or invalid"
[[ $(printf '%s' "$private_key" | base64 -D 2>/dev/null | wc -c | tr -d ' ') -eq 32 ]] \
    || fail "private EdDSA key stdin must decode to a 32-byte seed"
[[ -f "$release_notes" && ! -L "$release_notes" ]] || fail "release notes must be a regular file"
[[ $(wc -c <"$release_notes" | tr -d ' ') -le 65536 ]] || fail "release notes exceed 64 KiB"
[[ "$feed_url" =~ ^https://[^[:space:]@]+$ ]] || fail "feed URL must be HTTPS without userinfo"
[[ "$download_prefix" =~ ^https://[^[:space:]@?#]+$ ]] || fail "download URL prefix must be HTTPS without userinfo, query, or fragment"
[[ ! -L dist && ! -L "$output_dir" ]] || fail "release output paths must not be symlinks"
[[ ! -L "$lock_file" ]] || fail "release lock must not be a symlink"

mkdir -p dist
staging=""

cleanup() {
    status=$?
    unset private_key
    [[ -z "$staging" ]] || rm -rf "$staging" || true
    return "$status"
}
trap cleanup EXIT

exec 9>"$lock_file"
chmod 600 "$lock_file"
/usr/bin/lockf -s -t 0 9 || fail "another release process holds $lock_file"
staging=$(mktemp -d dist/.update.staging.XXXXXX)

tools_root=$(scripts/sparkle/prepare_release_tools.sh .cache/sparkle)
wails3 task package \
    APP_VERSION="$version" BUILD_NUMBER="$build_number" \
    FEED_URL="$feed_url" PUBLIC_KEY="$public_key"

archive_name="Codex-Pulse-$version-arm64.zip"
notes_name="Codex-Pulse-$version-arm64.txt"
cp "bin/Codex Pulse.app.zip" "$staging/$archive_name"
cp "$release_notes" "$staging/$notes_name"

printf '%s' "$private_key" | "$tools_root/generate_appcast" \
    --ed-key-file - \
    --download-url-prefix "$download_prefix/" \
    --embed-release-notes \
    --maximum-versions 1 \
    -o "$staging/appcast.xml" \
    "$staging"
unset private_key

go run ./scripts/sparkle/releaseverify \
    -write-manifest \
    -dir "$staging" \
    -version "$version" \
    -build "$build_number" \
    -feed-url "$feed_url" \
    -download-prefix "$download_prefix" \
    -public-key "$public_key"
scripts/sparkle/verify_release.sh "$staging" "$version" "$build_number" "$feed_url" "$download_prefix" "$public_key"

go run ./scripts/sparkle/atomicreplace "$staging" "$output_dir"
rm -rf "$staging" || true
staging=""
printf '%s\n' "$output_dir"
