#!/usr/bin/env bash

set -euo pipefail

SCRIPT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
REPO_ROOT=$(CDPATH= cd -- "$SCRIPT_DIR/../.." && pwd)
VERSION=""
BUILD_NUMBER=""
CHANNEL=""
ARCHIVE=""
ARCHIVE_URL=""
EXISTING=""
OUTPUT=""
PUBLICATION_DATE=""

fail() {
  printf 'generate_appcast: %s\n' "$*" >&2
  exit 1
}

usage() {
  printf '%s\n' \
    "usage: generate_appcast.sh --version <semver> --build-number <integer>" \
    "  --channel <stable|prerelease> --archive <zip> --archive-url <https-url>" \
    "  --output <appcast.xml> [--existing <appcast.xml>] [--publication-date <RFC-822>]" \
    "" \
    "The Sparkle Ed25519 private key is read only from standard input."
}

while [[ "$#" -gt 0 ]]; do
  case "$1" in
    --version) VERSION=${2:-}; shift 2 ;;
    --build-number) BUILD_NUMBER=${2:-}; shift 2 ;;
    --channel) CHANNEL=${2:-}; shift 2 ;;
    --archive) ARCHIVE=${2:-}; shift 2 ;;
    --archive-url) ARCHIVE_URL=${2:-}; shift 2 ;;
    --existing) EXISTING=${2:-}; shift 2 ;;
    --output) OUTPUT=${2:-}; shift 2 ;;
    --publication-date) PUBLICATION_DATE=${2:-}; shift 2 ;;
    --help|-h) usage; exit 0 ;;
    *) fail "unknown argument: $1" ;;
  esac
done

[[ -n "$VERSION" && -n "$BUILD_NUMBER" && -n "$CHANNEL" ]] ||
  fail "version, build number, and channel are required"
[[ -f "$ARCHIVE" && ! -L "$ARCHIVE" ]] ||
  fail "archive must be a regular non-symlink file"
[[ -n "$ARCHIVE_URL" && -n "$OUTPUT" ]] ||
  fail "archive URL and output are required"
if [[ -n "$EXISTING" ]]; then
  [[ -f "$EXISTING" && ! -L "$EXISTING" ]] ||
    fail "existing appcast must be a regular non-symlink file"
fi
[[ ! -t 0 ]] || fail "Ed25519 private key must be provided on standard input"

if [[ -z "$PUBLICATION_DATE" ]]; then
  PUBLICATION_DATE=$(LC_ALL=C date -R)
fi

TOOLS_ROOT=$("$SCRIPT_DIR/prepare_release_tools.sh" "$REPO_ROOT/.cache/sparkle")
SIGN_UPDATE="$TOOLS_ROOT/sign_update"
SIGNATURE_ATTRIBUTES=$("$SIGN_UPDATE" "$ARCHIVE" --ed-key-file -)

read -r ED_SIGNATURE ARCHIVE_LENGTH < <(
  python3 - "$SIGNATURE_ATTRIBUTES" <<'PY'
import re
import sys

attributes = sys.argv[1]
signature = re.search(r'sparkle:edSignature="([^"]+)"', attributes)
length = re.search(r'length="([1-9][0-9]*)"', attributes)
if signature is None or length is None:
    raise SystemExit("sign_update returned unexpected attributes")
print(signature.group(1), length.group(1))
PY
)

python3 "$SCRIPT_DIR/verify_archive_signature.py" \
  --archive "$ARCHIVE" \
  --ed-signature "$ED_SIGNATURE"

RENDER_ARGUMENTS=(
  "$SCRIPT_DIR/render_appcast.py"
  --output "$OUTPUT"
  --version "${VERSION#v}"
  --build-number "$BUILD_NUMBER"
  --channel "$CHANNEL"
  --archive-url "$ARCHIVE_URL"
  --archive-length "$ARCHIVE_LENGTH"
  --ed-signature "$ED_SIGNATURE"
  --publication-date "$PUBLICATION_DATE"
)
if [[ -n "$EXISTING" ]]; then
  RENDER_ARGUMENTS+=(--existing "$EXISTING")
fi
python3 "${RENDER_ARGUMENTS[@]}"
printf 'appcast generated: %s\n' "$OUTPUT"
