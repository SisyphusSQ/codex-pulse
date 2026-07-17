import { computed, ref, type ComputedRef } from "vue";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { defaultSessionsRouteState } from "./routeState";
import { useSessionQueries } from "./useSessionQueries";

const queryHarness = vi.hoisted(() => ({ calls: [] as unknown[] }));
const businessHarness = vi.hoisted(() => ({ detail: vi.fn(), list: vi.fn() }));

vi.mock("@tanstack/vue-query", () => ({
  keepPreviousData: "keep-previous-data",
  useQuery: (options: unknown) => {
    queryHarness.calls.push(options);
    return { options };
  },
}));

vi.mock("@/queries/business", () => ({
  sessionDetailQueryOptions: (request: unknown) => {
    businessHarness.detail(request);
    return { queryKey: ["detail", request] };
  },
  sessionListQueryOptions: (request: unknown) => {
    businessHarness.list(request);
    return { queryKey: ["list", request] };
  },
}));

describe("useSessionQueries", () => {
  beforeEach(() => {
    queryHarness.calls = [];
    businessHarness.detail.mockReset();
    businessHarness.list.mockReset();
  });

  it("keeps the full route request reactive and enables detail only for a selected Session", () => {
    const state = ref({ ...defaultSessionsRouteState });
    const turnCursor = ref<string | null>(null);
    const nowMS = ref(Date.parse("2026-07-16T08:00:00Z"));

    const result = useSessionQueries(
      computed(() => state.value),
      turnCursor,
      "Asia/Shanghai",
      () => nowMS.value,
    );

    expect(queryHarness.calls).toHaveLength(2);
    const listOptions = (queryHarness.calls[0] as ComputedRef<Record<string, unknown>>).value;
    const detailOptions = queryHarness.calls[1] as ComputedRef<Record<string, unknown>>;
    expect(listOptions.placeholderData).toBe("keep-previous-data");
    expect(businessHarness.list).toHaveBeenLastCalledWith(expect.objectContaining({
      page: { cursor: null, limit: 50 },
    }));
    expect(detailOptions.value.enabled).toBe(false);

    state.value = { ...state.value, sessionId: "opaque-session", cursor: "opaque-list-cursor" };
    turnCursor.value = "opaque-turn-cursor";

    expect(detailOptions.value.enabled).toBe(true);
    expect(businessHarness.detail).toHaveBeenLastCalledWith({
      reportingTimezone: "Asia/Shanghai",
      sessionId: "opaque-session",
      turnPage: { cursor: "opaque-turn-cursor", limit: 20 },
    });
    expect(result.requests.value.list.page.cursor).toBe("opaque-list-cursor");

    state.value = { ...state.value, range: "today" };
    expect(result.requests.value.list.timeRange).toEqual({
      endDateExclusive: "2026-07-17",
      startDate: "2026-07-16",
      timeZone: "Asia/Shanghai",
    });
    nowMS.value = Date.parse("2026-07-16T16:00:00Z");
    expect(result.requests.value.list.timeRange).toEqual({
      endDateExclusive: "2026-07-18",
      startDate: "2026-07-17",
      timeZone: "Asia/Shanghai",
    });
  });
});
