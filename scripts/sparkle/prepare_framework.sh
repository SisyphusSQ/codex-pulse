#!/usr/bin/env bash

set -euo pipefail

cache_root=${1:-.cache/sparkle}
version=2.9.4
url=https://github.com/sparkle-project/Sparkle/releases/download/2.9.4/Sparkle-2.9.4.tar.xz
expected_sha256=ce89daf967db1e1893ed3ebd67575ed82d3902563e3191ca92aaec9164fbdef9

fail() {
  printf 'prepare_framework: %s\n' "$*" >&2
  exit 1
}

if [[ -n "${SPARKLE_VERSION+x}" || -n "${SPARKLE_URL+x}" || -n "${SPARKLE_SHA256+x}" ]]; then
  fail "environment pin overrides are not supported; update the reviewed repository constants"
fi
[[ "$#" -le 1 ]] || fail "usage: $0 [cache-root]"

version_root="$cache_root/$version"
archive="$version_root/Sparkle-$version.tar.xz"
framework_root="$version_root/framework"
framework="$framework_root/Sparkle.framework"

sha256() {
  shasum -a 256 "$1" | awk '{print $1}'
}

framework_version() {
  plutil -extract CFBundleShortVersionString raw \
    -o - "$1/Versions/B/Resources/Info.plist" 2>/dev/null
}

mkdir -p "$version_root"
temporary_archive=""
staging=""
cleanup() {
  if [[ -n "$temporary_archive" && -e "$temporary_archive" ]]; then
    rm -f -- "$temporary_archive"
  fi
  if [[ -n "$staging" && -d "$staging" ]]; then
    rm -rf -- "$staging"
  fi
}
trap cleanup EXIT

if [[ ! -f "$archive" ]]; then
  temporary_archive=$(mktemp "$version_root/.Sparkle-$version.XXXXXX.tar.xz")
  curl --fail --location --silent --show-error --retry 3 \
    --output "$temporary_archive" "$url"
  [[ "$(sha256 "$temporary_archive")" == "$expected_sha256" ]] ||
    fail "download checksum mismatch for Sparkle $version"
  mv "$temporary_archive" "$archive"
  temporary_archive=""
else
  [[ "$(sha256 "$archive")" == "$expected_sha256" ]] ||
    fail "cached archive checksum mismatch: $archive"
fi

staging=$(mktemp -d "$version_root/.framework.XXXXXX")
tar -xJf "$archive" -C "$staging" ./Sparkle.framework
[[ -x "$staging/Sparkle.framework/Versions/B/Sparkle" ]] ||
  fail "archive does not contain an executable Sparkle framework"
[[ "$(framework_version "$staging/Sparkle.framework")" == "$version" ]] ||
  fail "archive framework version does not match $version"

rm -rf -- "$framework_root"
mv "$staging" "$framework_root"
staging=""
printf '%s\n' "$framework"
