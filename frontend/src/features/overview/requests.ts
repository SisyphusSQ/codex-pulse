import type {
  LocalDateRange,
  Request,
} from "@bindings/github.com/SisyphusSQ/codex-pulse/internal/query/models";
import {
  TrendGranularity,
  type UsageCostRequest,
} from "@bindings/github.com/SisyphusSQ/codex-pulse/internal/query/usagecost/models";

function listRequest(limit: number, timeRange: LocalDateRange | null): Request {
  return {
    filters: null,
    page: { cursor: null, limit },
    sort: null,
    timeRange,
  };
}

export function createOverviewRequests(range: LocalDateRange) {
  return {
    health: listRequest(5, null),
    projects: listRequest(5, range),
    sessions: listRequest(5, range),
    sources: listRequest(3, null),
    usage: {
      granularity: TrendGranularity.TrendDay,
      range,
    } satisfies UsageCostRequest,
  };
}
