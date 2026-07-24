#!/usr/bin/env bash

set -euo pipefail

SCRIPT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
REPO_ROOT=$(CDPATH= cd -- "$SCRIPT_DIR/../.." && pwd)
MODE=${1:-}
PROTO="api/codexpulse/core/v1/core.proto"
GENERATED=(
  "api/codexpulse/core/v1/core.pb.go"
  "api/codexpulse/core/v1/core_grpc.pb.go"
)

case "$MODE" in
  --check|--write) ;;
  *) printf 'usage: %s --check|--write\n' "$0" >&2; exit 2 ;;
esac

TMP_ROOT=$(mktemp -d "${TMPDIR:-/tmp}/codex-pulse-proto.XXXXXX")
trap 'rm -rf "$TMP_ROOT"' EXIT

command -v protoc >/dev/null 2>&1 || { printf 'protoc 34.1 is required\n' >&2; exit 1; }
[ "$(protoc --version)" = "libprotoc 34.1" ] || { printf 'protoc must be 34.1\n' >&2; exit 1; }
mkdir -p "$TMP_ROOT/bin"
GOBIN="$TMP_ROOT/bin" go install google.golang.org/protobuf/cmd/protoc-gen-go@v1.36.11
GOBIN="$TMP_ROOT/bin" go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@v1.6.2
PATH="$TMP_ROOT/bin:$PATH"
export PATH

cd "$REPO_ROOT"
protoc -I . \
  --go_out="$TMP_ROOT" --go_opt=paths=source_relative \
  --go-grpc_out="$TMP_ROOT" --go-grpc_opt=paths=source_relative \
  "$PROTO"

for path in "${GENERATED[@]}"; do
  if [ "$MODE" = "--write" ]; then
    cp "$TMP_ROOT/$path" "$REPO_ROOT/$path"
  elif ! cmp -s "$REPO_ROOT/$path" "$TMP_ROOT/$path"; then
    printf 'generated protobuf drift: %s (run make generate-proto)\n' "$path" >&2
    exit 1
  fi
done

printf 'protobuf generation %s passed\n' "$MODE"
