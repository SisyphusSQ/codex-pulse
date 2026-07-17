import { describe, expect, it } from "vitest";

import {
  FilterOperator,
  SortDirection,
} from "@bindings/github.com/SisyphusSQ/codex-pulse/internal/query/models";

import {
  defaultProjectsRouteState,
  parseProjectsRouteState,
} from "./routeState";
import {
  createProjectDetailRequest,
  createProjectListRequest,
} from "./requests";

describe("Projects generated requests", () => {
  it("maps the default route to a bounded seven-day server-owned list request", () => {
    expect(createProjectListRequest(
      defaultProjectsRouteState,
      Date.parse("2026-07-16T08:00:00Z"),
      "Asia/Shanghai",
    )).toEqual({
      filters: null,
      page: { cursor: null, limit: 50 },
      sort: [{ direction: SortDirection.SortDescending, field: "lastActivityAt" }],
      timeRange: {
        endDateExclusive: "2026-07-17",
        startDate: "2026-07-10",
        timeZone: "Asia/Shanghai",
      },
    });
  });

  it("maps confidence, sort, direction, cursor, and custom range to exact allowlist fields", () => {
    expect(createProjectListRequest({
      confidence: "medium",
      cursor: "opaque-list-cursor",
      direction: "asc",
      endDateExclusive: "2026-07-17",
      projectKey: "selection-is-not-a-filter",
      range: "custom",
      sort: "displayName",
      startDate: "2026-06-01",
    }, Date.parse("2026-07-16T08:00:00Z"), "Asia/Shanghai")).toEqual({
      filters: [{
        field: "confidence",
        operator: FilterOperator.FilterEqual,
        values: ["medium"],
      }],
      page: { cursor: "opaque-list-cursor", limit: 50 },
      sort: [{ direction: SortDirection.SortAscending, field: "displayName" }],
      timeRange: {
        endDateExclusive: "2026-07-17",
        startDate: "2026-06-01",
        timeZone: "Asia/Shanghai",
      },
    });
  });

  it("keeps every accepted four-digit custom date executable by the shared range builder", () => {
    const parsed = parseProjectsRouteState({
      end: "0099-01-02",
      range: "custom",
      start: "0099-01-01",
    });

    expect(parsed.normalized).toBe(false);
    expect(createProjectListRequest(
      parsed.state,
      Date.parse("2026-07-16T08:00:00Z"),
      "Asia/Shanghai",
    ).timeRange).toEqual({
      endDateExclusive: "0099-01-02",
      startDate: "0099-01-01",
      timeZone: "Asia/Shanghai",
    });
  });

  it("builds one range-bound detail request with independent ephemeral pages", () => {
    const range = {
      endDateExclusive: "2026-07-17",
      startDate: "2026-07-10",
      timeZone: "Asia/Shanghai",
    };
    expect(createProjectDetailRequest(
      "opaque-project-key",
      range,
      "opaque-session-cursor",
      "opaque-model-cursor",
    )).toEqual({
      dimensionKey: "opaque-project-key",
      modelPage: { cursor: "opaque-model-cursor", limit: 20 },
      range,
      sessionPage: { cursor: "opaque-session-cursor", limit: 20 },
    });
  });
});
