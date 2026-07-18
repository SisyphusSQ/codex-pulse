#!/usr/bin/env bash

set -euo pipefail

cache_root=${1:-.cache/sparkle}
version=2.9.4
archive="$cache_root/$version/Sparkle-$version.tar.xz"
tools_root="$cache_root/$version/tools"

fail() {
    printf 'prepare_release_tools: %s\n' "$*" >&2
    exit 1
}

[[ $# -le 1 ]] || fail "usage: $0 [cache-root]"
[[ -z "${SPARKLE_RELEASE_TOOLS+x}" ]] || fail "tool path overrides are not supported"

scripts/sparkle/prepare_framework.sh "$cache_root" >/dev/null
[[ -f "$archive" ]] || fail "trusted Sparkle archive is missing"

staging=$(mktemp -d "$cache_root/$version/.tools.XXXXXX")
trap 'rm -rf "$staging"' EXIT
tar -xJf "$archive" -C "$staging" \
    ./bin/generate_appcast ./bin/generate_keys ./bin/sign_update

for tool in generate_appcast generate_keys sign_update; do
    path="$staging/bin/$tool"
    [[ -x "$path" ]] || fail "archive tool is missing or not executable: $tool"
    file "$path" | grep -Fq 'Mach-O' || fail "$tool is not a Mach-O executable"
    lipo -archs "$path" | tr ' ' '\n' | grep -Fxq arm64 \
        || fail "$tool does not contain arm64"
done

previous="${tools_root}.previous.$$"
if [[ -e "$tools_root" ]]; then
    mv "$tools_root" "$previous"
fi
if ! mv "$staging/bin" "$tools_root"; then
    [[ ! -e "$tools_root" && -e "$previous" ]] && mv "$previous" "$tools_root"
    fail "cannot publish verified tools"
fi
rm -rf "$previous"
trap - EXIT
rm -rf "$staging"
printf '%s\n' "$tools_root"
