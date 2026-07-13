#!/usr/bin/env bash

set -euo pipefail

usage() {
    echo "usage: $0 <arch> <version> <build-number>" >&2
    exit 64
}

fail() {
    echo "validate_package_inputs: $*" >&2
    exit 1
}

[[ $# -eq 3 ]] || usage

arch=$1
app_version=$2
build_number=$3

[[ "$(uname -s)" == Darwin ]] || fail "package requires macOS"
[[ "$arch" == arm64 ]] || fail "Codex Pulse v0.1 package only supports arm64"
[[ "$app_version" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]] \
    || fail "APP_VERSION must contain exactly three numeric components"
[[ "$build_number" =~ ^[0-9]+$ ]] \
    || fail "BUILD_NUMBER must be a non-negative integer"

echo "validated package inputs (arch=$arch, version=$app_version, build=$build_number)"
