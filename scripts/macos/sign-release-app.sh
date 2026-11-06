#!/usr/bin/env bash

set -euo pipefail

APP_PATH=${1:-}
IDENTITY=${2:-}

fail() {
  printf 'release App signing failed: %s\n' "$*" >&2
  exit 1
}

[[ "$#" == 2 && -d "$APP_PATH" && ! -L "$APP_PATH" && "$APP_PATH" == *.app ]] ||
  fail "usage: sign-release-app.sh <bundle.app> <Developer-ID-SHA1>"
[[ "$IDENTITY" =~ ^[0-9A-Fa-f]{40}$ ]] || fail "identity must be a SHA-1 fingerprint"
security find-identity -v -p codesigning |
  grep -Ei "${IDENTITY} \"Developer ID Application:" >/dev/null ||
  fail "identity is not a valid Developer ID Application identity"

sign_code() {
  codesign --force --sign "$IDENTITY" --options runtime --timestamp \
    --preserve-metadata=entitlements "$1"
}

# Sparkle's nested services retain their own entitlements and are signed before containers.
while IFS= read -r -d '' executable; do
  if file -b "$executable" | grep -Fq 'Mach-O'; then
    sign_code "$executable"
  fi
done < <(find "$APP_PATH" -type f -print0)

while IFS= read -r -d '' bundle; do
  sign_code "$bundle"
done < <(find "$APP_PATH" -depth -type d \
  \( -name '*.app' -o -name '*.xpc' -o -name '*.framework' \) -print0)

codesign --verify --deep --strict --verbose=2 "$APP_PATH"
while IFS= read -r -d '' executable; do
  if file -b "$executable" | grep -Fq 'Mach-O'; then
    details=$(codesign -dv --verbose=4 "$executable" 2>&1)
    grep -Fq 'Authority=Developer ID Application:' <<<"$details" ||
      fail "nested code is missing Developer ID signing: $executable"
    grep -Fq '(runtime)' <<<"$details" ||
      fail "nested code is missing Hardened Runtime: $executable"
    grep -q '^Timestamp=' <<<"$details" ||
      fail "nested code is missing a secure timestamp: $executable"
  fi
done < <(find "$APP_PATH" -type f -print0)
