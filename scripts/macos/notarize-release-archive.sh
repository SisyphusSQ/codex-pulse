#!/usr/bin/env bash

set -euo pipefail
umask 077

ARCHIVE=${1:-}
PROFILE=${2:-}
RECORD=${3:-}

fail() {
  printf 'release notarization failed: %s\n' "$*" >&2
  exit 1
}

[[ "$#" == 3 && -f "$ARCHIVE" && ! -L "$ARCHIVE" && -n "$PROFILE" && -n "$RECORD" ]] ||
  fail "usage: notarize-release-archive.sh <archive> <Keychain-profile> <record-prefix>"
[[ -d "$(dirname -- "$RECORD")" ]] || fail "record directory must already exist"
DIGEST=$(shasum -a 256 "$ARCHIVE" | awk '{print $1}')

if [[ -e "$RECORD.sha256" ]]; then
  [[ "$(cat "$RECORD.sha256")" == "$DIGEST" ]] ||
    fail "archive differs from retained submission; inspect the original submission"
else
  [[ ! -e "$RECORD.submit.json" ]] || fail "submission exists without its archive digest"
  printf '%s\n' "$DIGEST" >"$RECORD.sha256"
  # Keep the request marker on failure: an unknown result must never trigger resubmission.
  xcrun notarytool submit "$ARCHIVE" --keychain-profile "$PROFILE" \
    --no-wait --output-format json >"$RECORD.submit.json"
fi

SUBMISSION_ID=$(python3 - "$RECORD.submit.json" <<'PY'
import json
import sys
import uuid

with open(sys.argv[1], encoding="utf-8") as source:
    submission = json.load(source)
print(uuid.UUID(submission["id"]))
PY
) || fail "submission result is unknown; inspect the retained request before continuing"

printf 'notarization submission: %s\n' "$SUBMISSION_ID"
xcrun notarytool wait "$SUBMISSION_ID" --keychain-profile "$PROFILE" \
  --timeout 20m --output-format json >"$RECORD.wait.json"
STATUS=$(python3 - "$RECORD.wait.json" <<'PY'
import json
import sys

with open(sys.argv[1], encoding="utf-8") as source:
    print(json.load(source)["status"])
PY
)
if [[ "$STATUS" != Accepted ]]; then
  xcrun notarytool log "$SUBMISSION_ID" --keychain-profile "$PROFILE" \
    "$RECORD.log.json" || true
  fail "submission $SUBMISSION_ID status is $STATUS; see retained records"
fi
