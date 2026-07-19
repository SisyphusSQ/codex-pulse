#!/usr/bin/env bash

set -euo pipefail

repo_root=$(CDPATH= cd -- "$(dirname -- "$0")/../.." && pwd)
run_id="${RUN_ID:-m11-performance-support-$(date -u +%Y%m%dT%H%M%SZ)}"
app_exec="$repo_root/bin/Codex Pulse.app/Contents/MacOS/Codex Pulse"
export MACOSX_DEPLOYMENT_TARGET="${MACOSX_DEPLOYMENT_TARGET:-15.0}"
export GOTOOLCHAIN=local
export GOPROXY=off

case "$run_id" in
  ''|*[!A-Za-z0-9._-]*)
    printf 'unsafe run id: %s\n' "$run_id" >&2
    exit 2
    ;;
esac
if [[ ! -x "$app_exec" ]] || [[ "$(uname -s)" != "Darwin" ]] || [[ "$(uname -m)" != "arm64" ]]; then
  printf '%s\n' 'M11-SUPPORT-001: packaged macOS arm64 app executable is required' >&2
  exit 1
fi
case "$MACOSX_DEPLOYMENT_TARGET" in
  15|15.*) ;;
  *)
    printf 'M11-SUPPORT-004: deployment target must be 15.x; got %s\n' "$MACOSX_DEPLOYMENT_TARGET" >&2
    exit 1
    ;;
esac

cd "$repo_root"
for command_name in go git uname sw_vers sysctl awk sed grep shasum; do
  command -v "$command_name" >/dev/null 2>&1 || {
    printf 'missing required command: %s\n' "$command_name" >&2
    exit 1
  }
done
run_dir="$repo_root/.agents/runs/$run_id"
if [[ -e "$run_dir" ]]; then
  printf 'run directory already exists: .agents/runs/%s\n' "$run_id" >&2
  exit 1
fi
mkdir -p "$run_dir"
printf 'phase\tstatus\n' >"$run_dir/manifest.tsv"
support_source_files=(
  Makefile
  internal/liveindex/runtime_test.go
  internal/store/retention_test.go
  internal/app/query_invalidation_test.go
  scripts/m11idle/main.go
  scripts/m11idle/main_test.go
  scripts/validation/m11-performance-support.sh
)
{
  printf 'started_at_utc=%s\n' "$(date -u +%Y-%m-%dT%H:%M:%SZ)"
  printf 'os_version=%s\n' "$(sw_vers -productVersion)"
  printf 'architecture=%s\n' "$(uname -m)"
  printf 'cpu_model=%s\n' "$(sysctl -n machdep.cpu.brand_string)"
  printf 'memory_bytes=%s\n' "$(sysctl -n hw.memsize)"
  printf 'macos_deployment_target=%s\n' "$MACOSX_DEPLOYMENT_TARGET"
  printf 'go_version=%s\n' "$(go version | sed 's/^go version //')"
  printf 'commit=%s\n' "$(git rev-parse HEAD)"
  if [[ -n "$(git status --porcelain=v1)" ]]; then
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
  printf 'support_source_sha256=%s\n' "$({
    for path in "${support_source_files[@]}"; do
      printf '%s\t' "$path"
      shasum -a 256 "$path" | awk '{print $1}'
    done
  } | shasum -a 256 | awk '{print $1}')"
  printf 'app_executable=bin/Codex Pulse.app/Contents/MacOS/Codex Pulse\n'
  printf 'app_executable_sha256=%s\n' "$(shasum -a 256 "$app_exec" | awk '{print $1}')"
} >"$run_dir/environment.txt"

go test ./internal/liveindex ./internal/store ./internal/app -run '^$' \
  -bench '^(BenchmarkRuntimeIncrementalAppend|BenchmarkCleanupRetentionBatch1000|BenchmarkQueryInvalidationPublisherNotify)$' \
  -benchmem -benchtime=20x -count=5 >"$run_dir/synthetic-benchmarks.txt"

awk '
  /^BenchmarkRuntimeIncrementalAppend-/ {
    append_count++; if ($3 > append_max) append_max=$3
    for (field=4; field<=NF; field++) if ($field == "MB/s" && (append_min==0 || $(field-1) < append_min)) append_min=$(field-1)
  }
  /^BenchmarkCleanupRetentionBatch1000-/ {
    retention_count++; if ($3 > retention_max) retention_max=$3
    for (field=4; field<=NF; field++) if ($field == "rows/s" && (retention_min==0 || $(field-1) < retention_min)) retention_min=$(field-1)
  }
  /^BenchmarkQueryInvalidationPublisherNotify-/ {
    event_count++; if ($3 > event_max) event_max=$3
  }
  END {
    if (append_count != 5 || retention_count != 5 || event_count != 5 ||
        append_max > 50000000 || append_min < 0.01 ||
        retention_max > 100000000 || retention_min < 1000 || event_max > 50000) exit 1
    printf "append_max_ns=%d\nappend_min_mb_s=%.6f\nretention_max_ns=%d\nretention_min_rows_s=%.6f\nevent_max_ns=%d\n",
      append_max, append_min, retention_max, retention_min, event_max
  }
' "$run_dir/synthetic-benchmarks.txt" >"$run_dir/synthetic-summary.txt" || {
  printf '%s\n' 'M11-SUPPORT-002: synthetic append/retention/event threshold failed' >&2
  exit 1
}
printf 'synthetic\tPASS\n' >>"$run_dir/manifest.tsv"

printf 'round\taverage_cpu_percent\tpeak_rss_bytes\tdatabase_bytes\twal_bytes\n' >"$run_dir/idle.tsv"
for round in 1 2 3; do
  go run ./scripts/m11idle --app "$app_exec" >"$run_dir/idle-$round.json"
  line=$(sed -n '1p' "$run_dir/idle-$round.json")
  if ! printf '%s\n' "$line" | grep -Eq '"version":"m11-tray-idle-v1".*"result":"passed".*"bootstrapComplete":true.*"activeTasks":0.*"isolatedCleanup":true'; then
    printf '%s\n' 'M11-SUPPORT-003: invalid tray idle contract' >&2
    exit 1
  fi
  cpu=$(printf '%s\n' "$line" | sed -E 's/.*"averageCpuPercent":([0-9.]+).*/\1/')
  rss=$(printf '%s\n' "$line" | sed -E 's/.*"peakRssBytes":([0-9]+).*/\1/')
  database=$(printf '%s\n' "$line" | sed -E 's/.*"databaseBytes":([0-9]+).*/\1/')
  wal=$(printf '%s\n' "$line" | sed -E 's/.*"walBytes":([0-9]+).*/\1/')
  if [[ "$cpu" == "$line" || "$rss" == "$line" || "$database" == "$line" || "$wal" == "$line" ]]; then
    printf '%s\n' 'M11-SUPPORT-003: invalid tray idle summary' >&2
    exit 1
  fi
  printf '%s\t%s\t%s\t%s\t%s\n' "$round" "$cpu" "$rss" "$database" "$wal" >>"$run_dir/idle.tsv"
done
awk 'NR > 1 { if ($2 > cpu) cpu=$2; if ($3 > rss) rss=$3; if ($4 > db) db=$4; if ($5 > wal) wal=$5 }
  END { printf "idle_cpu_max_percent=%.6f\nidle_rss_max_bytes=%d\nidle_db_max_bytes=%d\nidle_wal_max_bytes=%d\n", cpu, rss, db, wal }' \
  "$run_dir/idle.tsv" >"$run_dir/idle-summary.txt"
printf 'tray-idle\tPASS\ncompleted\tPASS\n' >>"$run_dir/manifest.tsv"
printf 'completed_at_utc=%s\n' "$(date -u +%Y-%m-%dT%H:%M:%SZ)" >>"$run_dir/environment.txt"
printf 'M11 support performance matrix passed; evidence: .agents/runs/%s\n' "$run_id"
cat "$run_dir/synthetic-summary.txt" "$run_dir/idle-summary.txt"
