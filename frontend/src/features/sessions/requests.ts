import {
  FilterOperator,
  SortDirection,
  type FilterTerm,
  type Request,
} from "@bindings/github.com/SisyphusSQ/codex-pulse/internal/query/models";
import type { SessionDetailRequest } from "@bindings/github.com/SisyphusSQ/codex-pulse/internal/query/usagecost/models";
import { createOverviewPresetRange } from "@/features/overview/range";

import type { SessionsRouteState } from "./routeState";

export function createSessionListRequest(
  state: SessionsRouteState,
  nowMS: number,
  timeZone: string,
): Request {
  const filters: FilterTerm[] = [];
  if (state.projectId !== null) {
    filters.push({
      field: "projectId",
      operator: FilterOperator.FilterEqual,
      values: [state.projectId],
    });
  }
  if (state.modelKey !== null) {
    filters.push({
      field: "modelKey",
      operator: FilterOperator.FilterEqual,
      values: [state.modelKey],
    });
  }
  if (state.activity !== "all") {
    filters.push({
      field: "activity",
      operator: FilterOperator.FilterEqual,
      values: [state.activity],
    });
  }

  const timeRange = state.range === "all"
    ? null
    : createOverviewPresetRange(state.range, nowMS, timeZone);

  return {
    filters: filters.length === 0 ? null : filters,
    page: { cursor: state.cursor, limit: 50 },
    sort: [{
      direction: state.direction === "asc"
        ? SortDirection.SortAscending
        : SortDirection.SortDescending,
      field: state.sort,
    }],
    timeRange,
  };
}

export function createSessionDetailRequest(
  sessionId: string,
  turnCursor: string | null,
  reportingTimezone: string,
): SessionDetailRequest {
  return {
    reportingTimezone,
    sessionId,
    turnPage: { cursor: turnCursor, limit: 20 },
  };
}
