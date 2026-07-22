#!/usr/bin/env bash
set -euo pipefail

usage() {
  echo "usage: $0 --write|--check" >&2
}

if [[ $# -ne 1 || ( "$1" != "--write" && "$1" != "--check" ) ]]; then
  usage
  exit 2
fi

mode="$1"
repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
package_root="$repo_root/app/macos"
generated_root="$package_root/Sources/CodexPulseProtocolGenerated"
proto_file="$repo_root/api/codexpulse/core/v1/core.proto"
protoc_path="$(command -v protoc || true)"

if [[ -z "$protoc_path" ]]; then
  echo "protoc is required" >&2
  exit 1
fi
if [[ "$("$protoc_path" --version)" != "libprotoc 34.1" ]]; then
  echo "protoc 34.1 is required" >&2
  exit 1
fi

generate_into() {
  local output_path="$1"
  swift package \
    --package-path "$package_root" \
    --allow-writing-to-package-directory \
    --allow-writing-to-directory "$output_path" \
    generate-grpc-code-from-protos \
    --access-level public \
    --no-servers \
    --file-naming dropPath \
    --protoc-path "$protoc_path" \
    --import-path "$repo_root/api" \
    --output-path "$output_path" \
    -- "$proto_file"
}

if [[ "$mode" == "--write" ]]; then
  generate_into "$generated_root"
  exit 0
fi

temporary_root="$(mktemp -d "${TMPDIR:-/tmp}/codex-pulse-swift-proto.XXXXXX")"
trap 'rm -rf "$temporary_root"' EXIT
generate_into "$temporary_root"

diff -u "$generated_root/core.pb.swift" "$temporary_root/core.pb.swift"
diff -u "$generated_root/core.grpc.swift" "$temporary_root/core.grpc.swift"
