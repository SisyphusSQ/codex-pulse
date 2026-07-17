import type { Request } from "@bindings/github.com/SisyphusSQ/codex-pulse/internal/query/models";

function runtimeListRequest(limit: number): Request {
  return {
    filters: null,
    page: { cursor: null, limit },
    sort: null,
    timeRange: null,
  };
}

export function createRuntimeRequests() {
  return {
    health: runtimeListRequest(12),
    jobs: runtimeListRequest(12),
    sources: runtimeListRequest(12),
  };
}
