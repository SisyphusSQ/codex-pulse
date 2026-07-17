import { describe, expect, it } from "vitest";

import {
  FilterOperator,
  SortDirection,
} from "@bindings/github.com/SisyphusSQ/codex-pulse/internal/query/models";

import { defaultSessionsRouteState } from "./routeState";
import {
  createSessionDetailRequest,
  createSessionListRequest,
} from "./requests";

describe("Sessions generated requests", () => {
  it("keeps the default list server-owned and bounded", () => {
    expect(createSessionListRequest(
      defaultSessionsRouteState,
      Date.parse("2026-07-16T08:00:00Z"),
      "Asia/Shanghai",
    )).toEqual({
      filters: null,
      page: { cursor: null, limit: 50 },
      sort: [{ direction: SortDirection.SortDescending, field: "lastActivityAt" }],
      timeRange: null,
    });
  });

  it("maps finite URL state to exact backend allowlist fields without local filtering", () => {
    const request = createSessionListRequest({
      activity: "idle",
      cursor: "opaque-cursor",
      direction: "asc",
      modelKey: "model-safe-key",
      projectId: "project-safe-id",
      range: "7d",
      sessionId: "selection-is-not-a-list-filter",
      sort: "totalTokens",
    }, Date.parse("2026-07-16T08:00:00Z"), "Asia/Shanghai");

    expect(request).toEqual({
      filters: [
        { field: "projectId", operator: FilterOperator.FilterEqual, values: ["project-safe-id"] },
        { field: "modelKey", operator: FilterOperator.FilterEqual, values: ["model-safe-key"] },
        { field: "activity", operator: FilterOperator.FilterEqual, values: ["idle"] },
      ],
      page: { cursor: "opaque-cursor", limit: 50 },
      sort: [{ direction: SortDirection.SortAscending, field: "totalTokens" }],
      timeRange: {
        endDateExclusive: "2026-07-17",
        startDate: "2026-07-10",
        timeZone: "Asia/Shanghai",
      },
    });
  });

  it("builds a timezone-bound detail request and never persists the turn cursor elsewhere", () => {
    expect(createSessionDetailRequest(
      "opaque-session-id",
      "opaque-turn-cursor",
      "Asia/Shanghai",
    )).toEqual({
      reportingTimezone: "Asia/Shanghai",
      sessionId: "opaque-session-id",
      turnPage: { cursor: "opaque-turn-cursor", limit: 20 },
    });
  });
});
