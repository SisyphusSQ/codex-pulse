#!/usr/bin/env bash

set -euo pipefail

usage() {
    echo "usage: $0 <arch> <version> <build-number> [feed-url public-key]" >&2
    exit 64
}

fail() {
    echo "validate_package_inputs: $*" >&2
    exit 1
}

[[ $# -eq 3 || $# -eq 5 ]] || usage

arch=$1
app_version=$2
build_number=$3
feed_url=${4:-}
public_key=${5:-}

[[ "$(uname -s)" == Darwin ]] || fail "package requires macOS"
[[ "$arch" == arm64 ]] || fail "Codex Pulse v0.1 package only supports arm64"
[[ "$app_version" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]] \
    || fail "APP_VERSION must contain exactly three numeric components"
[[ "$build_number" =~ ^[0-9]+$ ]] \
    || fail "BUILD_NUMBER must be a non-negative integer"
if [[ -n "$feed_url" || -n "$public_key" ]]; then
    [[ "$feed_url" =~ ^https://[^[:space:]@]+$ ]] || fail "FEED_URL must be HTTPS without userinfo"
    [[ "$public_key" != *[[:space:]]* ]] || fail "PUBLIC_KEY must not contain whitespace"
    [[ $(printf '%s' "$public_key" | base64 -D 2>/dev/null | wc -c | tr -d ' ') -eq 32 ]] \
        || fail "PUBLIC_KEY must decode to 32 bytes"
fi

echo "validated package inputs (arch=$arch, version=$app_version, build=$build_number)"
