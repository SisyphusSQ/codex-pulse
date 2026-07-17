import { computed, ref, type ComputedRef } from "vue";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { defaultProjectsRouteState } from "./routeState";
import { useProjectQueries } from "./useProjectQueries";

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
  projectDetailQueryOptions: (request: unknown) => {
    businessHarness.detail(request);
    return { queryKey: ["detail", request] };
  },
  projectListQueryOptions: (request: unknown) => {
    businessHarness.list(request);
    return { queryKey: ["list", request] };
  },
}));

describe("useProjectQueries", () => {
  beforeEach(() => {
    queryHarness.calls = [];
    businessHarness.detail.mockReset();
    businessHarness.list.mockReset();
  });

  it("keeps full list/detail requests reactive and enables detail only for a selected Project", () => {
    const state = ref({ ...defaultProjectsRouteState });
    const sessionCursor = ref<string | null>(null);
    const modelCursor = ref<string | null>(null);
    const nowMS = ref(Date.parse("2026-07-16T08:00:00Z"));

    const result = useProjectQueries(
      computed(() => state.value),
      sessionCursor,
      modelCursor,
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

    state.value = {
      ...state.value,
      cursor: "opaque-list-cursor",
      projectKey: "opaque-project",
    };
    sessionCursor.value = "opaque-session-cursor";
    modelCursor.value = "opaque-model-cursor";

    expect(detailOptions.value.enabled).toBe(true);
    expect(businessHarness.detail).toHaveBeenLastCalledWith({
      dimensionKey: "opaque-project",
      modelPage: { cursor: "opaque-model-cursor", limit: 20 },
      range: {
        endDateExclusive: "2026-07-17",
        startDate: "2026-07-10",
        timeZone: "Asia/Shanghai",
      },
      sessionPage: { cursor: "opaque-session-cursor", limit: 20 },
    });
    expect(result.requests.value.list.page.cursor).toBe("opaque-list-cursor");

    nowMS.value = Date.parse("2026-07-16T16:00:00Z");
    expect(result.requests.value.list.timeRange).toEqual({
      endDateExclusive: "2026-07-18",
      startDate: "2026-07-11",
      timeZone: "Asia/Shanghai",
    });
    expect(result.requests.value.list.page.cursor).toBeNull();
    expect(result.requests.value.detail).toBeNull();
    expect(detailOptions.value.enabled).toBe(false);
  });
});
