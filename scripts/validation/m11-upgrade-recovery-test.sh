#!/usr/bin/env bash

set -euo pipefail

repo_root=$(CDPATH= cd -- "$(dirname -- "$0")/../.." && pwd)
cd "$repo_root"

runner=scripts/validation/m11-upgrade-recovery.sh

if CODEX_PULSE_RUN_M11_UPGRADE_E2E= bash "$runner" >/dev/null 2>&1; then
  echo "M11-UPGRADE-TEST-001: runner accepted missing opt-in" >&2
  exit 1
fi

required=(
  'stage update-choice-and-drain'
  'stage migration-preflight-and-recovery'
  'Test(MigrationRunnerStopsBeforeApplyWhenSpaceOrBackupFails'
  'Test(AcquireWakesOwnerAndAllowsTakeoverAfterClose|ContenderTakesOverWhenOwnerStopsAcknowledgingWakes|LeaseReclaimsAfterOwnerProcessCrash)'
  'go test -race -count=1 ./internal/updater ./internal/app ./internal/store ./internal/singleinstance ./scripts/sparkle/...'
  'CODEX_PULSE_RUN_UPGRADE_E2E=1 go run ./scripts/sparkle/upgradee2e -scenario all'
)
for marker in "${required[@]}"; do
  grep -Fq -- "$marker" "$runner" || {
    echo "M11-UPGRADE-TEST-002: missing runner contract: $marker" >&2
    exit 1
  }
done

for forbidden in 'workflow_dispatch' 'generate_keys' 'git tag' 'gh release' 'curl ' 'upload'; do
  if grep -Fq -- "$forbidden" "$runner"; then
    echo "M11-UPGRADE-TEST-003: forbidden release/remote operation: $forbidden" >&2
    exit 1
  fi
done

go test -count=1 -run 'Test(UpgradeFixtureTracksCurrentSchemaAndImmediatePredecessor|UpgradeE2ERequiresExplicitOptIn)$' ./scripts/sparkle/upgradee2e
go test -tags upgradee2e -count=1 -run TestUpgradeE2EMigrationCatalogIsFailClosed ./internal/store

grep -Fq 'target 只读回 schema 15、v15 前 backup' docs/design/details/updates-and-release/README.md || {
  echo "M11-UPGRADE-TEST-004: update design does not describe current schema 15" >&2
  exit 1
}
grep -Fq '重新执行 v1-v15 migration' docs/design/details/data-model/README.md || {
  echo "M11-UPGRADE-TEST-005: recovery design does not target current schema 15" >&2
  exit 1
}

echo "M11 upgrade/recovery contract tests: PASS"
