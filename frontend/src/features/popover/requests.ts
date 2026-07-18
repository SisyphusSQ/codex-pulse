import { SortDirection, type Request } from "@bindings/github.com/SisyphusSQ/codex-pulse/internal/query/models";
import { TrendGranularity, type UsageCostRequest } from "@bindings/github.com/SisyphusSQ/codex-pulse/internal/query/usagecost/models";
import { createOverviewPresetRange } from "@/features/overview/range";

export function createPopoverRequests(nowMS: number, timeZone: string) {
  const today = createOverviewPresetRange("today", nowMS, timeZone);
  return {
    sessions: {
      filters: null,
      page: { cursor: null, limit: 5 },
      sort: [{ field: "lastActivityAtMs", direction: SortDirection.SortDescending }],
      timeRange: null,
    } satisfies Request,
    usage: {
      range: today,
      granularity: TrendGranularity.TrendDay,
    } satisfies UsageCostRequest,
  };
}
