import { describe, expect, it } from "vitest";

import {
  defaultProjectsRouteState,
  parseProjectsRouteState,
  selectProject,
  serializeProjectsRouteState,
  setProjectsListCursor,
  updateProjectsFilters,
} from "./routeState";

describe("Projects route state", () => {
  it("round-trips every recoverable filter, sort, custom range, list cursor, and selection", () => {
    const state = {
      confidence: "low",
      cursor: "opaque-list-cursor",
      direction: "asc",
      endDateExclusive: "2026-07-17",
      projectKey: "opaque-project-key",
      range: "custom",
      sort: "estimatedCost",
      startDate: "2026-06-01",
    } as const;

    expect(parseProjectsRouteState(serializeProjectsRouteState(state))).toEqual({
      normalized: false,
      state,
    });
  });

  it("fails arrays, blanks, invalid custom ranges, unknown values, and oversized identities closed", () => {
    const parsed = parseProjectsRouteState({
      confidence: "certain",
      cursor: ["one", "two"],
      direction: "sideways",
      end: "2026-06-01",
      ignored: "private-value",
      project: "p".repeat(1025),
      range: "custom",
      sort: "databaseColumn",
      start: "2026-07-01",
    });

    expect(parsed).toEqual({ normalized: true, state: defaultProjectsRouteState });
    expect(serializeProjectsRouteState(parsed.state)).toEqual({});
  });

  it("canonicalizes explicit defaults and irrelevant custom dates out of the URL", () => {
    expect(parseProjectsRouteState({
      confidence: "all",
      direction: "desc",
      end: "2026-07-17",
      range: "7d",
      sort: "lastActivityAt",
      start: "2026-07-10",
    })).toEqual({
      normalized: true,
      state: defaultProjectsRouteState,
    });
  });

  it("rejects impossible calendar dates and custom ranges longer than the backend contract", () => {
    expect(parseProjectsRouteState({
      end: "2026-03-02",
      range: "custom",
      start: "2026-02-30",
    })).toEqual({ normalized: true, state: defaultProjectsRouteState });
    expect(parseProjectsRouteState({
      end: "2027-01-03",
      range: "custom",
      start: "2026-01-01",
    })).toEqual({ normalized: true, state: defaultProjectsRouteState });
  });

  it("clears stale list and detail identities when filters or list pages change", () => {
    const current = {
      ...defaultProjectsRouteState,
      cursor: "page-two",
      projectKey: "selected-project",
    };

    expect(updateProjectsFilters(current, { confidence: "unknown" })).toEqual({
      ...defaultProjectsRouteState,
      confidence: "unknown",
    });
    expect(setProjectsListCursor(current, "page-three")).toEqual({
      ...current,
      cursor: "page-three",
      projectKey: null,
    });
    expect(selectProject(current, "another-project")).toEqual({
      ...current,
      projectKey: "another-project",
    });
  });
});
