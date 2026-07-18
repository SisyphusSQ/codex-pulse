#!/usr/bin/env bash

set -euo pipefail

fail() {
    printf 'local_release_pipeline: %s\n' "$*" >&2
    exit 1
}

[[ $# -eq 0 ]] || fail "usage: CODEX_PULSE_RUN_RELEASE_PIPELINE=1 $0"
[[ "${CODEX_PULSE_RUN_RELEASE_PIPELINE:-}" == 1 ]] \
    || fail "set CODEX_PULSE_RUN_RELEASE_PIPELINE=1 to run the local release and upgrade matrix"
[[ "$(uname -s)" == Darwin && "$(uname -m)" == arm64 ]] \
    || fail "local release pipeline requires macOS arm64"

stage() {
    printf '==> local-release-stage:%s\n' "$1"
}

stage build-test-package
make verify

stage synthetic-sign-appcast-verify
CODEX_PULSE_RUN_RELEASE_E2E=1 go test -run TestSyntheticReleaseEndToEnd -count=1 -v ./scripts/sparkle

stage n-minus-one-upgrade
CODEX_PULSE_RUN_UPGRADE_E2E=1 go run ./scripts/sparkle/upgradee2e -scenario all

stage complete
