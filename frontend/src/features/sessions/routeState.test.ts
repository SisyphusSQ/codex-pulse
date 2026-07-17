import { describe, expect, it } from "vitest";

import {
  defaultSessionsRouteState,
  parseSessionsRouteState,
  selectSession,
  serializeSessionsRouteState,
  setSessionsListCursor,
  updateSessionsFilters,
} from "./routeState";

describe("Sessions route state", () => {
  it("round-trips every recoverable filter, sort, list cursor, and selection", () => {
    const state = {
      activity: "active",
      cursor: "opaque-list-cursor",
      direction: "asc",
      modelKey: "model-safe-key",
      projectId: "project-safe-id",
      range: "30d",
      sessionId: "opaque-session-id",
      sort: "estimatedCost",
    } as const;

    expect(parseSessionsRouteState(serializeSessionsRouteState(state))).toEqual({
      normalized: false,
      state,
    });
  });

  it("fails closed to finite defaults for arrays, blanks, unknown enums, and oversized identities", () => {
    const parsed = parseSessionsRouteState({
      activity: "busy",
      cursor: ["one", "two"],
      direction: "sideways",
      ignored: "secret-value",
      model: " ",
      project: "p".repeat(257),
      range: "forever",
      session: "s".repeat(1025),
      sort: "databaseColumn",
    });

    expect(parsed).toEqual({ normalized: true, state: defaultSessionsRouteState });
    expect(serializeSessionsRouteState(parsed.state)).toEqual({});
  });

  it("canonicalizes explicit default values out of the URL", () => {
    expect(parseSessionsRouteState({
      activity: "all",
      direction: "desc",
      range: "all",
      sort: "lastActivityAt",
    })).toEqual({
      normalized: true,
      state: defaultSessionsRouteState,
    });
  });

  it("clears stale list and detail identities when filters or pages change", () => {
    const current = {
      ...defaultSessionsRouteState,
      cursor: "page-two",
      sessionId: "selected-session",
    };

    expect(updateSessionsFilters(current, { activity: "idle" })).toEqual({
      ...defaultSessionsRouteState,
      activity: "idle",
    });
    expect(setSessionsListCursor(current, "page-three")).toEqual({
      ...current,
      cursor: "page-three",
      sessionId: null,
    });
    expect(selectSession(current, "another-session")).toEqual({
      ...current,
      sessionId: "another-session",
    });
  });
});
