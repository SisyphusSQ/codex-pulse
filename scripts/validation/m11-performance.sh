#!/usr/bin/env bash

set -euo pipefail

repo_root=$(CDPATH= cd -- "$(dirname -- "$0")/../.." && pwd)
run_id="${RUN_ID:-m11-performance-$(date -u +%Y%m%dT%H%M%SZ)}"
rounds="${CODEX_PULSE_M11_PERF_ROUNDS:-3}"
export MACOSX_DEPLOYMENT_TARGET="${MACOSX_DEPLOYMENT_TARGET:-15.0}"
export GOTOOLCHAIN=local
export GOPROXY=off

case "$run_id" in
  ''|*[!A-Za-z0-9._-]*)
    printf 'unsafe run id: %s\n' "$run_id" >&2
    exit 2
    ;;
esac
if ! printf '%s\n' "$rounds" | grep -Eq '^[3-5]$'; then
  printf 'CODEX_PULSE_M11_PERF_ROUNDS must be between 3 and 5; got %s\n' "$rounds" >&2
  exit 2
fi
if [[ "${CODEX_PULSE_M11_REAL_HOME_CONFIRM:-}" != "READ_ONLY_CONFIRMED" ]] ||
  [[ -z "${CODEX_PULSE_M11_REAL_HOME:-}" ]]; then
  printf '%s\n' 'M11-PERF-002: explicit read-only Home confirmation is required' >&2
  exit 1
fi

cd "$repo_root"
for command_name in go git uname sw_vers sysctl shasum awk sed seq grep; do
  command -v "$command_name" >/dev/null 2>&1 || {
    printf 'missing required command: %s\n' "$command_name" >&2
    exit 1
  }
done
if [[ ! -x /usr/bin/time ]] || [[ "$(uname -s)" != "Darwin" ]] || [[ "$(uname -m)" != "arm64" ]]; then
  printf '%s\n' 'M11-PERF-003: macOS arm64 and BSD time are required' >&2
  exit 1
fi
case "$MACOSX_DEPLOYMENT_TARGET" in
  15|15.*) ;;
  *)
    printf 'M11-PERF-004: deployment target must be 15.x; got %s\n' "$MACOSX_DEPLOYMENT_TARGET" >&2
    exit 1
    ;;
esac

run_dir="$repo_root/.artifacts/runs/$run_id"
if [[ -e "$run_dir" ]]; then
  printf 'run directory already exists: .artifacts/runs/%s\n' "$run_id" >&2
  exit 1
fi
mkdir -p "$run_dir"
printf 'round\tstatus\n' >"$run_dir/manifest.tsv"
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
} >"$run_dir/environment.txt"
summary_paths=()
for round in $(seq 1 "$rounds"); do
  summary="$run_dir/round-$round.json"
  summary_paths+=("$summary")
  printf '[cold-round] %s/%s\n' "$round" "$rounds"
  if /usr/bin/time -l -o "$run_dir/round-$round.time" \
    bash scripts/validation/m11-real-home.sh >"$summary" 2>"$run_dir/round-$round.progress"; then
    printf '%s\tPASS\n' "$round" >>"$run_dir/manifest.tsv"
  else
    printf '%s\tFAIL\n' "$round" >>"$run_dir/manifest.tsv"
    printf 'cold round %s failed; inspect ignored local evidence\n' "$round" >&2
    exit 1
  fi
done

joined=$(IFS=,; printf '%s' "${summary_paths[*]}")
go run ./scripts/m11perf --summaries "$joined" >"$run_dir/aggregate.json"
printf 'completed_at_utc=%s\n' "$(date -u +%Y-%m-%dT%H:%M:%SZ)" >>"$run_dir/environment.txt"
printf 'completed\tPASS\n' >>"$run_dir/manifest.tsv"
printf 'M11 performance matrix passed; evidence: .artifacts/runs/%s\n' "$run_id"
sed -n '1p' "$run_dir/aggregate.json"
