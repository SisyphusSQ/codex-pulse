import { describe, expect, it } from "vitest";
import { SortDirection } from "@bindings/github.com/SisyphusSQ/codex-pulse/internal/query/models";
import { createPopoverRequests } from "./requests";

describe("createPopoverRequests", () => {
  it("locks today usage and the latest five safe sessions", () => {
    const requests = createPopoverRequests(Date.UTC(2026, 6, 18, 12), "Asia/Shanghai");
    expect(requests.usage.range).toEqual({
      startDate: "2026-07-18", endDateExclusive: "2026-07-19", timeZone: "Asia/Shanghai",
    });
    expect(requests.sessions.page).toEqual({ cursor: null, limit: 5 });
    expect(requests.sessions.sort).toEqual([{ field: "lastActivityAtMs", direction: SortDirection.SortDescending }]);
    expect(requests.sessions.filters).toBeNull();
  });
});
