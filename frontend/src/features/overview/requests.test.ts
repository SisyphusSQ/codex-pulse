import { TrendGranularity } from "@bindings/github.com/SisyphusSQ/codex-pulse/internal/query/usagecost/models";
import { describe, expect, it } from "vitest";

import { createOverviewRequests } from "./requests";

describe("overview generated query requests", () => {
  it("shares one bounded local range without inventing cursor or filters", () => {
    const range = {
      startDate: "2026-07-10",
      endDateExclusive: "2026-07-17",
      timeZone: "Asia/Shanghai",
    };
    const requests = createOverviewRequests(range);

    expect(requests.usage).toEqual({ range, granularity: TrendGranularity.TrendDay });
    expect(requests.sessions).toEqual({
      page: { cursor: null, limit: 5 },
      sort: null,
      filters: null,
      timeRange: range,
    });
    expect(requests.projects).toEqual(requests.sessions);
    expect(requests.sources.page).toEqual({ cursor: null, limit: 3 });
    expect(requests.sources.timeRange).toBeNull();
    expect(requests.health.page).toEqual({ cursor: null, limit: 5 });
    expect(requests.health.timeRange).toBeNull();
  });
});
