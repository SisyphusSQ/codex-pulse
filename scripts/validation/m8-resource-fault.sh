#!/usr/bin/env bash

set -euo pipefail

repo_root="$(CDPATH= cd -- "$(dirname -- "$0")/../.." && pwd)"
run_id="${RUN_ID:-m8-resource-fault-$(date -u +%Y%m%dT%H%M%SZ)}"
mode="run"
export MACOSX_DEPLOYMENT_TARGET="${MACOSX_DEPLOYMENT_TARGET:-15.0}"
export GOTOOLCHAIN=local
export GOPROXY=off

usage() {
  cat <<'EOF'
usage: scripts/validation/m8-resource-fault.sh [--check] [--run-id SAFE_ID]

Runs the macOS arm64 M8 resource and synthetic fault validation matrix.
Raw, machine-local evidence is written below .artifacts/runs/ and is not committed.
EOF
}

while [ "$#" -gt 0 ]; do
  case "$1" in
    --check)
      mode="check"
      shift
      ;;
    --run-id)
      if [ "$#" -lt 2 ]; then
        printf '%s\n' '--run-id requires a value' >&2
        exit 2
      fi
      run_id="$2"
      shift 2
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      printf 'unknown argument: %s\n' "$1" >&2
      usage >&2
      exit 2
      ;;
  esac
done

case "$run_id" in
  ''|*[!A-Za-z0-9._-]*)
    printf 'unsafe run id: %s\n' "$run_id" >&2
    exit 2
    ;;
esac

required_paths=(
  internal/metrics/collector_test.go
  internal/app/metrics_resource_benchmark_test.go
  internal/health/evaluator_test.go
  internal/query/runtimeinfo/data_health_test.go
  internal/store/retention_test.go
  internal/store/sqlite/store_test.go
  internal/store/store_integration_test.go
  internal/codex/logs/discovery_test.go
  internal/codex/index/parser_test.go
  internal/codex/quota/client_test.go
  internal/lifecycle/coordinator_test.go
  internal/bootstrap/runtime_test.go
)

cd "$repo_root"

for path in "${required_paths[@]}"; do
  if [ ! -f "$path" ]; then
    printf 'missing validation dependency: %s\n' "$path" >&2
    exit 1
  fi
done

for command_name in go git uname sw_vers; do
  if ! command -v "$command_name" >/dev/null 2>&1; then
    printf 'missing required command: %s\n' "$command_name" >&2
    exit 1
  fi
done

if [ ! -x /usr/bin/time ]; then
  printf '%s\n' 'missing executable /usr/bin/time' >&2
  exit 1
fi

if [ "$(uname -s)" != "Darwin" ] || [ "$(uname -m)" != "arm64" ]; then
  printf 'TOO-284 requires macOS arm64; got %s %s\n' "$(uname -s)" "$(uname -m)" >&2
  exit 1
fi

case "$MACOSX_DEPLOYMENT_TARGET" in
  15|15.*) ;;
  *)
    printf 'TOO-284 requires MACOSX_DEPLOYMENT_TARGET 15.x; got %s\n' \
      "$MACOSX_DEPLOYMENT_TARGET" >&2
    exit 1
    ;;
esac

if [ "$mode" = "check" ]; then
  printf '%s\n' 'M8 resource/fault matrix preflight passed'
  exit 0
fi

run_dir="$repo_root/.artifacts/runs/$run_id"
if [ -e "$run_dir" ]; then
  printf 'run directory already exists: .artifacts/runs/%s\n' "$run_id" >&2
  exit 1
fi
mkdir -p "$run_dir"

{
  printf 'run_id=%s\n' "$run_id"
  printf 'started_at_utc=%s\n' "$(date -u +%Y-%m-%dT%H:%M:%SZ)"
  printf 'os_version=%s\n' "$(sw_vers -productVersion)"
  printf 'architecture=%s\n' "$(uname -m)"
  printf 'macos_deployment_target=%s\n' "$MACOSX_DEPLOYMENT_TARGET"
  printf 'go_version=%s\n' "$(go version | sed 's/^go version //')"
  printf 'commit=%s\n' "$(git rev-parse HEAD)"
  if [ -n "$(git status --porcelain=v1)" ]; then
    printf 'worktree_dirty=true\n'
  else
    printf 'worktree_dirty=false\n'
  fi
  printf 'tracked_diff_sha256=%s\n' "$(git diff --binary HEAD | shasum -a 256 | awk '{print $1}')"
  printf 'untracked_tree_sha256=%s\n' "$({
    git ls-files --others --exclude-standard | LC_ALL=C sort | while IFS= read -r path; do
      shasum -a 256 "$path"
    done
  } | shasum -a 256 | awk '{print $1}')"
} >"$run_dir/environment.txt"

printf 'category\tphase\tstatus\n' >"$run_dir/manifest.tsv"

run_timed() {
  category="$1"
  phase="$2"
  expected_symbols="$3"
  shift 3

  command_file="$run_dir/$phase.command"
  stdout_file="$run_dir/$phase.stdout"
  stderr_file="$run_dir/$phase.stderr"
  time_file="$run_dir/$phase.time"

  {
    printf 'cd %q\n' "$repo_root"
    printf '%q ' "$@"
    printf '\n'
  } >"$command_file"

  printf '[%s] %s\n' "$category" "$phase"
  if /usr/bin/time -l -o "$time_file" "$@" >"$stdout_file" 2>"$stderr_file"; then
    previous_ifs="$IFS"
    IFS=','
    for expected_spec in $expected_symbols; do
      expected_symbol="${expected_spec%@*}"
      expected_count="${expected_spec##*@}"
      if [ "$expected_symbol" = "$expected_spec" ] ||
        ! printf '%s\n' "$expected_count" | grep -Eq '^[1-9][0-9]*$'; then
        IFS="$previous_ifs"
        printf '%s\t%s\tFAIL\n' "$category" "$phase" >>"$run_dir/manifest.tsv"
        printf 'invalid expected symbol contract %s: %s\n' "$expected_spec" "$phase" >&2
        return 1
      fi
      actual_count="$(grep -F -c -- "$expected_symbol" "$stdout_file" || true)"
      if [ "$actual_count" -lt "$expected_count" ]; then
        IFS="$previous_ifs"
        printf '%s\t%s\tFAIL\n' "$category" "$phase" >>"$run_dir/manifest.tsv"
        printf 'phase matched %s %s times, need at least %s: %s\n' \
          "$expected_symbol" "$actual_count" "$expected_count" "$phase" >&2
        return 1
      fi
    done
    IFS="$previous_ifs"
    printf '%s\t%s\tPASS\n' "$category" "$phase" >>"$run_dir/manifest.tsv"
    return 0
  fi

  printf '%s\t%s\tFAIL\n' "$category" "$phase" >>"$run_dir/manifest.tsv"
  printf 'phase failed: %s; inspect .artifacts/runs/%s/%s.stderr\n' "$phase" "$run_id" "$phase" >&2
  return 1
}

require_benchmark_below() {
  stdout_file="$1"
  benchmark_name="$2"
  unit="$3"
  limit="$4"

  threshold_phase="${benchmark_name}-${unit}"
  if ! awk -v benchmark="$benchmark_name" -v unit="$unit" -v limit="$limit" '
    $1 ~ ("^" benchmark "-") {
      found = 1
      for (field_number = 2; field_number <= NF; field_number++) {
        if ($field_number == unit) {
          unit_found = 1
          value_text = $(field_number - 1)
          if (value_text !~ /^[0-9]+([.][0-9]+)?$/) {
            printf "%s %s has non-numeric value %s\n", benchmark, unit, value_text > "/dev/stderr"
            failed = 1
          }
          value = value_text + 0
          if (value >= limit) {
            printf "%s %s %.6f exceeds %.6f\n", benchmark, unit, value, limit > "/dev/stderr"
            failed = 1
          }
        }
      }
    }
    END {
      if (!found) {
        printf "missing benchmark result: %s\n", benchmark > "/dev/stderr"
        exit 2
      }
      if (!unit_found) {
        printf "missing benchmark unit: %s %s\n", benchmark, unit > "/dev/stderr"
        exit 2
      }
      if (failed) {
        exit 1
      }
    }
  ' "$stdout_file"; then
    printf 'threshold\t%s\tFAIL\n' "$threshold_phase" >>"$run_dir/manifest.tsv"
    printf 'resource threshold failed: %s %s < %s\n' "$benchmark_name" "$unit" "$limit" >&2
    exit 1
  fi
  printf 'threshold\t%s\tPASS\n' "$threshold_phase" >>"$run_dir/manifest.tsv"
}

require_benchmark_contract() {
  stdout_file="$1"
  benchmark_name="$2"
  expected_rows="$3"
  required_units="$4"
  contract_phase="${benchmark_name}-metric-contract"

  if ! awk -v benchmark="$benchmark_name" -v expected_rows="$expected_rows" \
    -v units="$required_units" '
    BEGIN {
      required_count = split(units, required, ",")
    }
    $1 ~ ("^" benchmark "-") {
      row_count++
      delete seen
      for (field_number = 2; field_number <= NF; field_number++) {
        for (unit_number = 1; unit_number <= required_count; unit_number++) {
          if ($field_number == required[unit_number]) {
            seen[unit_number]++
            value_text = $(field_number - 1)
            if (value_text !~ /^[0-9]+([.][0-9]+)?$/) {
              printf "%s row %d unit %s has non-numeric value %s\n", \
                benchmark, row_count, required[unit_number], value_text > "/dev/stderr"
              failed = 1
            }
          }
        }
      }
      for (unit_number = 1; unit_number <= required_count; unit_number++) {
        if (seen[unit_number] != 1) {
          printf "%s row %d requires unit %s exactly once, got %d\n", \
            benchmark, row_count, required[unit_number], seen[unit_number] > "/dev/stderr"
          failed = 1
        }
      }
    }
    END {
      if (row_count != expected_rows) {
        printf "%s requires %d rows, got %d\n", benchmark, expected_rows, row_count > "/dev/stderr"
        failed = 1
      }
      if (failed) {
        exit 1
      }
    }
  ' "$stdout_file"; then
    printf 'threshold\t%s\tFAIL\n' "$contract_phase" >>"$run_dir/manifest.tsv"
    printf 'benchmark metric contract failed: %s\n' "$benchmark_name" >&2
    exit 1
  fi
  printf 'threshold\t%s\tPASS\n' "$contract_phase" >>"$run_dir/manifest.tsv"
}

# Resource phases combine process-level time/RSS/IO from BSD time with typed
# assertions inside the selected tests. They never inspect a real Codex Home or
# application database.
run_timed resource idle-collector 'BenchmarkCollector-@5' \
  go test ./internal/metrics -run '^$' -bench '^BenchmarkCollector$' -benchmem -count=5 -timeout=5m
run_timed resource application-collector 'BenchmarkApplicationMetricsCollector-@5' \
  go test ./internal/app -run '^$' -bench '^BenchmarkApplicationMetricsCollector$' -benchmem -benchtime=100x -count=5 -timeout=10m
require_benchmark_contract "$run_dir/application-collector.stdout" \
  BenchmarkApplicationMetricsCollector 5 \
  duty_pct,query_ms,rss_mib,db_mib,wal_mib,live_depth,backfill_depth,probe_retries
require_benchmark_below "$run_dir/application-collector.stdout" \
  BenchmarkApplicationMetricsCollector duty_pct 0.5
require_benchmark_below "$run_dir/application-collector.stdout" \
  BenchmarkApplicationMetricsCollector query_ms 50
require_benchmark_below "$run_dir/application-collector.stdout" \
  BenchmarkApplicationMetricsCollector rss_mib 512
require_benchmark_below "$run_dir/application-collector.stdout" \
  BenchmarkApplicationMetricsCollector wal_mib 256
run_timed resource health-evaluator 'BenchmarkEvaluator-@5' \
  go test ./internal/health -run '^$' -bench '^BenchmarkEvaluator$' -benchmem -count=5 -timeout=5m
run_timed resource live-index '=== RUN   TestRuntimeInterruptsAndRecoversLiveJobFromAuthoritativeCheckpoint@20' \
  go test -v ./internal/liveindex -run '^TestRuntimeInterruptsAndRecoversLiveJobFromAuthoritativeCheckpoint$' -count=20 -timeout=10m
run_timed resource history-backfill '=== RUN   TestRuntimeRunSliceTimeBudgetStillMarksSnapshotDrift@10,=== RUN   TestRuntimeInterruptsAndResumesFromDurableSourceCheckpointAfterRestart@10' \
  go test -v ./internal/bootstrap -run '^(TestRuntimeRunSliceTimeBudgetStillMarksSnapshotDrift|TestRuntimeInterruptsAndResumesFromDurableSourceCheckpointAfterRestart)$' -count=10 -timeout=10m
run_timed resource data-health-query '=== RUN   TestDataHealthMapsBoundedTwentyFourHourMetrics@20,=== RUN   TestDataHealthRejectsCancelledInvalidAndInconsistentSnapshots@20' \
  go test -v ./internal/query/runtimeinfo -run '^TestDataHealth' -count=20 -timeout=10m
run_timed resource retention-cleanup '=== RUN   TestCleanupRetentionAppliesFixedWindowAndReferenceRules@10,=== RUN   TestCleanupRetentionRollsBackFailedBatchAndCanRetry@10' \
  go test -v ./internal/store -run '^TestCleanupRetention' -count=10 -timeout=10m

# Fault phases use only temporary directories, fake adapters, httptest, or a
# dedicated child process. No real permission, disk, network, sleep, or process
# state is changed.
run_timed fault permission '=== RUN   TestDiscovererReportsProbeFailuresWithoutPartialSnapshot@20' \
  go test -v ./internal/codex/logs -run '^TestDiscovererReportsProbeFailuresWithoutPartialSnapshot$' -count=20 -timeout=10m
run_timed fault disk-full-read-only '=== RUN   TestClassifyRuntimeErrorReturnsOnlyStableClasses@20,=== RUN   TestMigrationRunnerStopsBeforeApplyWhenSpaceOrBackupFails/space@20,=== RUN   TestMigrationRunnerStopsBeforeApplyWhenSpaceOrBackupFails/read-only@20,=== RUN   TestMigrationRunnerStopsBeforeApplyWhenSpaceOrBackupFails/backup@20' \
  go test -v ./internal/store -run '^(TestClassifyRuntimeErrorReturnsOnlyStableClasses|TestMigrationRunnerStopsBeforeApplyWhenSpaceOrBackupFails)$' -count=20 -timeout=10m
run_timed fault sqlite-lock '=== RUN   TestStoreClassifiesExternalWriterContentionAsBusy@20,=== RUN   TestCheckpointWALDoesNotBlockActiveReadSnapshot@20' \
  go test -v ./internal/store/sqlite -run '^(TestStoreClassifiesExternalWriterContentionAsBusy|TestCheckpointWALDoesNotBlockActiveReadSnapshot)$' -count=20 -timeout=10m
run_timed fault malformed-row '=== RUN   TestParseUsesLatestValidAppendEntryAndKeepsContentFreeDiagnostics@20,=== RUN   TestParseRejectsDuplicateJSONKeys@20,=== RUN   TestParseSkipsRowsMissingRequiredUpstreamStringFields@20' \
  go test -v ./internal/codex/index -run '^(TestParseUsesLatestValidAppendEntryAndKeepsContentFreeDiagnostics|TestParseRejectsDuplicateJSONKeys|TestParseSkipsRowsMissingRequiredUpstreamStringFields)$' -count=20 -timeout=10m
run_timed fault network '=== RUN   TestClientRetriesOnlyNetworkTimeoutAndServerFailuresWithBoundedPolicy@20' \
  go test -v ./internal/codex/quota -run '^TestClientRetriesOnlyNetworkTimeoutAndServerFailuresWithBoundedPolicy$' -count=20 -timeout=10m
run_timed fault sleep-wake '=== RUN   TestCoordinatorKeepsUserPauseAcrossSleepWakeAndExactReplay@20,=== RUN   TestCoordinatorDefersSourceRecoveryWhileSleeping@20,=== RUN   TestCoordinatorHomeChangedPreservesSleepAndPauseUntilWake@20' \
  go test -v ./internal/lifecycle -run '^(TestCoordinatorKeepsUserPauseAcrossSleepWakeAndExactReplay|TestCoordinatorDefersSourceRecoveryWhileSleeping|TestCoordinatorHomeChangedPreservesSleepAndPauseUntilWake)$' -count=20 -timeout=10m
run_timed fault process-interruption '=== RUN   TestStoreIntegrationReopensAfterAbnormalExit@10,=== RUN   TestRuntimeInterruptsAndResumesFromDurableSourceCheckpointAfterRestart@10,=== RUN   TestRuntimeDrainInterruptsAdmissionAndResumeCreatesOneNewAttempt@10' \
  go test -v ./internal/store ./internal/bootstrap -run '^(TestStoreIntegrationReopensAfterAbnormalExit|TestRuntimeInterruptsAndResumesFromDurableSourceCheckpointAfterRestart|TestRuntimeDrainInterruptsAdmissionAndResumeCreatesOneNewAttempt)$' -count=10 -timeout=10m

printf 'completed_at_utc=%s\n' "$(date -u +%Y-%m-%dT%H:%M:%SZ)" >>"$run_dir/environment.txt"
printf 'M8 resource/fault matrix passed; evidence: .artifacts/runs/%s\n' "$run_id"
