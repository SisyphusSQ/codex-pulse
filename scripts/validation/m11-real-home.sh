#!/usr/bin/env bash

set -euo pipefail

repo_root=$(CDPATH= cd -- "$(dirname -- "$0")/../.." && pwd)
cd "$repo_root"

if [[ "${CODEX_PULSE_M11_REAL_HOME_CONFIRM:-}" != "READ_ONLY_CONFIRMED" ]]; then
  echo "M11-LIVE-001: set CODEX_PULSE_M11_REAL_HOME_CONFIRM=READ_ONLY_CONFIRMED" >&2
  exit 1
fi
if [[ -z "${CODEX_PULSE_M11_REAL_HOME:-}" ]]; then
  echo "M11-LIVE-002: set CODEX_PULSE_M11_REAL_HOME to an explicit absolute path" >&2
  exit 1
fi

exec go run ./scripts/m11realhome \
  --home "$CODEX_PULSE_M11_REAL_HOME" \
  --confirm READ_ONLY_CONFIRMED \
  --observe-append "${CODEX_PULSE_M11_OBSERVE_APPEND:-20s}"
