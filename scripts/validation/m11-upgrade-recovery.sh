#!/usr/bin/env bash

set -euo pipefail

fail() {
  printf 'm11_upgrade_recovery: %s\n' "$*" >&2
  exit 1
}

[[ $# -eq 0 ]] || fail "usage: CODEX_PULSE_RUN_M11_UPGRADE_E2E=1 $0"
[[ "${CODEX_PULSE_RUN_M11_UPGRADE_E2E:-}" == 1 ]] \
  || fail "set CODEX_PULSE_RUN_M11_UPGRADE_E2E=1 to run the isolated M11 update matrix"
[[ "$(uname -s)" == Darwin && "$(uname -m)" == arm64 ]] \
  || fail "M11 update matrix requires macOS arm64"

repo_root=$(CDPATH= cd -- "$(dirname -- "$0")/../.." && pwd)
cd "$repo_root"

stage() {
  printf '==> m11-upgrade-stage:%s\n' "$1"
}

stage update-choice-and-drain
go test -count=1 -run 'Test(CoordinatorSkipSnoozeAndExplicitDownload|CoordinatorSnoozeChoiceFailureRetainsIntentAndPreservesSkippedVersion|CoordinatorRunUsesCronAndCloseDrains|SparklePublishesUpdateOnlyAfterRetainingDownloadReply)$' ./internal/updater
go test -count=1 -run 'Test(ApplicationUpdaterRuntimeInstallsOnlyAfterSharedShutdown|ApplicationUpdaterRuntimeDoesNotInstallAfterShutdownFailure|ApplicationUpdaterRuntimeTimeoutDoesNotInstallUntilReawaitSucceeds|ApplicationUpdaterRuntimeArbitratesQuitUntilNativeInstallDispatch|ApplicationShutdownCoordinatorTimeoutKeepsDrainingAndCanBeReawaited|DesktopCommandCoordinatorDrainsBeforeQuitAndRejectsLaterCommands)$' ./internal/app
go test -count=1 -run 'Test(AcquireWakesOwnerAndAllowsTakeoverAfterClose|ContenderTakesOverWhenOwnerStopsAcknowledgingWakes|LeaseReclaimsAfterOwnerProcessCrash)$' ./internal/singleinstance

stage migration-preflight-and-recovery
go test -count=1 -run 'Test(MigrationRunnerStopsBeforeApplyWhenSpaceOrBackupFails|ApplicationMigrationCreatesRestorablePreMigrationBackupForLegacyDatabase|ApplicationSchemaV15IndexMigrationRollsBackAtomically)$' ./internal/store
go test -count=1 -run 'Test(OpenApplicationStartupReturnsRecoveryGraphForMigrationFailure|MigrationRecoveryRestorePreservesFailedDatabaseAndConsumesConfirmation|MigrationRecoveryAtomicSwitchRollsBackBeforeCommit|MigrationRecoveryReadyVerifyFailurePropagatesTypedFailure)$' ./internal/app

stage focused-race
go test -race -count=1 ./internal/updater ./internal/app ./internal/store ./internal/singleinstance ./scripts/sparkle/...

stage n-minus-one-to-current
CODEX_PULSE_RUN_UPGRADE_E2E=1 go run ./scripts/sparkle/upgradee2e -scenario all

stage complete
