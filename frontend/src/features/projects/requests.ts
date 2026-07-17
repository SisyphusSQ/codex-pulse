import {
  FilterOperator,
  SortDirection,
  type FilterTerm,
  type LocalDateRange,
  type Request,
} from "@bindings/github.com/SisyphusSQ/codex-pulse/internal/query/models";
import type { ProjectDetailRequest } from "@bindings/github.com/SisyphusSQ/codex-pulse/internal/query/usagecost/models";
import {
  createCustomOverviewRange,
  createOverviewPresetRange,
} from "@/features/overview/range";

import type { ProjectsRouteState } from "./routeState";

export function createProjectsRange(
  state: ProjectsRouteState,
  nowMS: number,
  timeZone: string,
): LocalDateRange {
  if (state.range === "custom" && state.startDate !== null && state.endDateExclusive !== null) {
    return createCustomOverviewRange(state.startDate, state.endDateExclusive, timeZone);
  }
  if (state.range === "custom") {
    return createOverviewPresetRange("7d", nowMS, timeZone);
  }
  return createOverviewPresetRange(state.range, nowMS, timeZone);
}

export function createProjectListRequest(
  state: ProjectsRouteState,
  nowMS: number,
  timeZone: string,
): Request {
  const filters: FilterTerm[] = [];
  if (state.confidence !== "all") {
    filters.push({
      field: "confidence",
      operator: FilterOperator.FilterEqual,
      values: [state.confidence],
    });
  }
  return {
    filters: filters.length === 0 ? null : filters,
    page: { cursor: state.cursor, limit: 50 },
    sort: [{
      direction: state.direction === "asc"
        ? SortDirection.SortAscending
        : SortDirection.SortDescending,
      field: state.sort,
    }],
    timeRange: createProjectsRange(state, nowMS, timeZone),
  };
}

export function createProjectDetailRequest(
  dimensionKey: string,
  range: LocalDateRange,
  sessionCursor: string | null,
  modelCursor: string | null,
): ProjectDetailRequest {
  return {
    dimensionKey,
    modelPage: { cursor: modelCursor, limit: 20 },
    range,
    sessionPage: { cursor: sessionCursor, limit: 20 },
  };
}
