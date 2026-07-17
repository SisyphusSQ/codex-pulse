import type { LocationQuery } from "vue-router";

export type SessionsActivity = "all" | "active" | "idle";
export type SessionsRange = "all" | "today" | "7d" | "30d";
export type SessionsSort = "lastActivityAt" | "totalTokens" | "estimatedCost";
export type SessionsSortDirection = "asc" | "desc";

export interface SessionsRouteState {
  activity: SessionsActivity;
  cursor: string | null;
  direction: SessionsSortDirection;
  modelKey: string | null;
  projectId: string | null;
  range: SessionsRange;
  sessionId: string | null;
  sort: SessionsSort;
}

export const defaultSessionsRouteState: SessionsRouteState = Object.freeze({
  activity: "all",
  cursor: null,
  direction: "desc",
  modelKey: null,
  projectId: null,
  range: "all",
  sessionId: null,
  sort: "lastActivityAt",
});

const queryKeys = new Set([
  "activity",
  "cursor",
  "direction",
  "model",
  "project",
  "range",
  "session",
  "sort",
]);

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

export function parseSessionsRouteState(query: LocationQuery) {
  const activity = finiteValue(query.activity, ["all", "active", "idle"], "all");
  const range = finiteValue(query.range, ["all", "today", "7d", "30d"], "all");
  const sort = finiteValue(
    query.sort,
    ["lastActivityAt", "totalTokens", "estimatedCost"],
    "lastActivityAt",
  );
  const direction = finiteValue(query.direction, ["asc", "desc"], "desc");
  const projectId = opaqueValue(query.project, 256);
  const modelKey = opaqueValue(query.model, 256);
  const cursor = opaqueValue(query.cursor, 8192);
  const sessionId = opaqueValue(query.session, 1024);
  const unknownKey = Object.keys(query).some((key) => !queryKeys.has(key));

  return {
    normalized: unknownKey || [
      activity,
      range,
      sort,
      direction,
      projectId,
      modelKey,
      cursor,
      sessionId,
    ].some((result) => result.normalized),
    state: {
      activity: activity.value,
      cursor: cursor.value,
      direction: direction.value,
      modelKey: modelKey.value,
      projectId: projectId.value,
      range: range.value,
      sessionId: sessionId.value,
      sort: sort.value,
    } satisfies SessionsRouteState,
  };
}

export function serializeSessionsRouteState(state: SessionsRouteState): Record<string, string> {
  const query: Record<string, string> = {};
  if (state.activity !== defaultSessionsRouteState.activity) query.activity = state.activity;
  if (state.range !== defaultSessionsRouteState.range) query.range = state.range;
  if (state.sort !== defaultSessionsRouteState.sort) query.sort = state.sort;
  if (state.direction !== defaultSessionsRouteState.direction) query.direction = state.direction;
  if (state.projectId !== null) query.project = state.projectId;
  if (state.modelKey !== null) query.model = state.modelKey;
  if (state.cursor !== null) query.cursor = state.cursor;
  if (state.sessionId !== null) query.session = state.sessionId;
  return query;
}

export type SessionsFilterPatch = Partial<Pick<
  SessionsRouteState,
  "activity" | "direction" | "modelKey" | "projectId" | "range" | "sort"
>>;

export function updateSessionsFilters(
  state: SessionsRouteState,
  patch: SessionsFilterPatch,
): SessionsRouteState {
  return { ...state, ...patch, cursor: null, sessionId: null };
}

export function setSessionsListCursor(
  state: SessionsRouteState,
  cursor: string | null,
): SessionsRouteState {
  return { ...state, cursor, sessionId: null };
}

export function selectSession(
  state: SessionsRouteState,
  sessionId: string | null,
): SessionsRouteState {
  return { ...state, sessionId };
}
