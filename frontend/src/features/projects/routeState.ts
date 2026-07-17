import type { LocationQuery } from "vue-router";

export type ProjectsConfidence = "all" | "high" | "low" | "medium" | "unknown";
export type ProjectsRange = "today" | "7d" | "30d" | "custom";
export type ProjectsSort = "displayName" | "estimatedCost" | "lastActivityAt" | "totalTokens";
export type ProjectsSortDirection = "asc" | "desc";

export interface ProjectsRouteState {
  confidence: ProjectsConfidence;
  cursor: string | null;
  direction: ProjectsSortDirection;
  endDateExclusive: string | null;
  projectKey: string | null;
  range: ProjectsRange;
  sort: ProjectsSort;
  startDate: string | null;
}

export const defaultProjectsRouteState: ProjectsRouteState = Object.freeze({
  confidence: "all",
  cursor: null,
  direction: "desc",
  endDateExclusive: null,
  projectKey: null,
  range: "7d",
  sort: "lastActivityAt",
  startDate: null,
});

const queryKeys = new Set([
  "confidence",
  "cursor",
  "direction",
  "end",
  "project",
  "range",
  "sort",
  "start",
]);
const localDatePattern = /^\d{4}-\d{2}-\d{2}$/u;

function finiteValue<T extends string>(
  value: LocationQuery[string],
  allowed: readonly T[],
  fallback: T,
) {
  if (value === undefined) return { normalized: false, value: fallback };
  if (typeof value !== "string" || !allowed.includes(value as T)) {
    return { normalized: true, value: fallback };
  }
  return { normalized: value === fallback, value: value as T };
}

function opaqueValue(value: LocationQuery[string], maximumLength: number) {
  if (value === undefined) return { normalized: false, value: null };
  if (
    typeof value !== "string"
    || value.length === 0
    || value.length > maximumLength
    || value.trim() !== value
  ) {
    return { normalized: true, value: null };
  }
  return { normalized: false, value };
}

function localDateValue(value: LocationQuery[string]) {
  if (value === undefined) return { normalized: false, value: null };
  const timestamp = typeof value === "string"
    ? Date.parse(`${value}T00:00:00Z`)
    : Number.NaN;
  if (
    typeof value !== "string"
    || !localDatePattern.test(value)
    || !Number.isFinite(timestamp)
    || new Date(timestamp).toISOString().slice(0, 10) !== value
  ) {
    return { normalized: true, value: null };
  }
  return { normalized: false, value };
}

export function parseProjectsRouteState(query: LocationQuery) {
  const confidence = finiteValue(
    query.confidence,
    ["all", "high", "medium", "low", "unknown"],
    "all",
  );
  const range = finiteValue(query.range, ["today", "7d", "30d", "custom"], "7d");
  const sort = finiteValue(
    query.sort,
    ["lastActivityAt", "totalTokens", "estimatedCost", "displayName"],
    "lastActivityAt",
  );
  const direction = finiteValue(query.direction, ["asc", "desc"], "desc");
  const cursor = opaqueValue(query.cursor, 8192);
  const projectKey = opaqueValue(query.project, 1024);
  const startDate = localDateValue(query.start);
  const endDateExclusive = localDateValue(query.end);
  const customRangeValid = range.value !== "custom" || (
    startDate.value !== null
    && endDateExclusive.value !== null
    && startDate.value < endDateExclusive.value
    && (
      Date.parse(`${endDateExclusive.value}T00:00:00Z`)
      - Date.parse(`${startDate.value}T00:00:00Z`)
    ) / 86_400_000 <= 366
  );
  const irrelevantDates = range.value !== "custom"
    && (startDate.value !== null || endDateExclusive.value !== null);
  const unknownKey = Object.keys(query).some((key) => !queryKeys.has(key));

  if (!customRangeValid) {
    return { normalized: true, state: defaultProjectsRouteState };
  }

  return {
    normalized: unknownKey || irrelevantDates || [
      confidence,
      range,
      sort,
      direction,
      cursor,
      projectKey,
      startDate,
      endDateExclusive,
    ].some((result) => result.normalized),
    state: {
      confidence: confidence.value,
      cursor: cursor.value,
      direction: direction.value,
      endDateExclusive: range.value === "custom" ? endDateExclusive.value : null,
      projectKey: projectKey.value,
      range: range.value,
      sort: sort.value,
      startDate: range.value === "custom" ? startDate.value : null,
    } satisfies ProjectsRouteState,
  };
}

export function serializeProjectsRouteState(state: ProjectsRouteState): Record<string, string> {
  const query: Record<string, string> = {};
  if (state.confidence !== defaultProjectsRouteState.confidence) query.confidence = state.confidence;
  if (state.range !== defaultProjectsRouteState.range) query.range = state.range;
  if (state.sort !== defaultProjectsRouteState.sort) query.sort = state.sort;
  if (state.direction !== defaultProjectsRouteState.direction) query.direction = state.direction;
  if (state.range === "custom" && state.startDate !== null && state.endDateExclusive !== null) {
    query.start = state.startDate;
    query.end = state.endDateExclusive;
  }
  if (state.cursor !== null) query.cursor = state.cursor;
  if (state.projectKey !== null) query.project = state.projectKey;
  return query;
}

export type ProjectsFilterPatch = Partial<Pick<
  ProjectsRouteState,
  "confidence" | "direction" | "endDateExclusive" | "range" | "sort" | "startDate"
>>;

export function updateProjectsFilters(
  state: ProjectsRouteState,
  patch: ProjectsFilterPatch,
): ProjectsRouteState {
  return { ...state, ...patch, cursor: null, projectKey: null };
}

export function setProjectsListCursor(
  state: ProjectsRouteState,
  cursor: string | null,
): ProjectsRouteState {
  return { ...state, cursor, projectKey: null };
}

export function selectProject(
  state: ProjectsRouteState,
  projectKey: string | null,
): ProjectsRouteState {
  return { ...state, projectKey };
}
