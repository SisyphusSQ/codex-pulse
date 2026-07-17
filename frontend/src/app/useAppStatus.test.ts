import { mount } from "@vue/test-utils";
import { defineComponent, h, nextTick, ref } from "vue";
import { afterEach, describe, expect, it, vi } from "vitest";

import { selectAppStatus, type AppStatusInput, useAppStatus } from "./useAppStatus";

const harness = vi.hoisted(() => ({ health: {} as Record<string, unknown> }));
vi.mock("@tanstack/vue-query", () => ({ useQuery: () => harness.health }));
vi.mock("@/features/runtime/requests", () => ({ createRuntimeRequests: () => ({ health: vi.fn() }) }));
vi.mock("@/queries/business", () => ({ healthListQueryOptions: vi.fn(() => ({})) }));

function input(overrides: Partial<AppStatusInput> = {}): AppStatusInput {
  return {
    online: true,
    health: {
      data: {
        metaStatus: "complete",
        level: "healthy",
      },
      isError: false,
      isPending: false,
      isStale: false,
    },
    ...overrides,
  };
}

describe("global application status", () => {
  afterEach(() => vi.restoreAllMocks());

  it("returns no banner for a healthy authoritative snapshot", () => {
    expect(selectAppStatus(input())).toBeNull();
  });

  it("uses one deterministic priority for concurrent conditions", () => {
    expect(selectAppStatus(input({
      online: false,
      health: {
        data: { metaStatus: "partial", level: "blocked" },
        isError: true,
        isPending: false,
        isStale: true,
      },
    }))).toEqual({ kind: "blocked", retryable: true });

    expect(selectAppStatus(input({ online: false }))).toEqual({ kind: "offline", retryable: false });
  });

  it("separates unavailable, partial, stale, loading, paused and busy", () => {
    expect(selectAppStatus(input({ health: { data: undefined, isError: true, isPending: false, isStale: false } }))).toEqual({ kind: "unavailable", retryable: true });
    expect(selectAppStatus(input({ health: { data: { metaStatus: "partial", level: "healthy" }, isError: false, isPending: false, isStale: false } }))).toEqual({ kind: "partial", retryable: true });
    expect(selectAppStatus(input({ health: { data: { metaStatus: "complete", level: "healthy" }, isError: true, isPending: false, isStale: true } }))).toEqual({ kind: "stale", retryable: true });
    expect(selectAppStatus(input({ health: { data: undefined, isError: false, isPending: true, isStale: false } }))).toEqual({ kind: "loading", retryable: false });
    expect(selectAppStatus(input({ health: { data: { metaStatus: "complete", level: "paused" }, isError: false, isPending: false, isStale: false } }))).toEqual({ kind: "paused", retryable: false });
    expect(selectAppStatus(input({ health: { data: { metaStatus: "complete", level: "busy" }, isError: false, isPending: false, isStale: false } }))).toEqual({ kind: "busy", retryable: false });
  });

  it("subscribes to online state only while mounted and removes both listeners", async () => {
    harness.health = {
      data: ref({ meta: { status: "complete" }, summary: { level: "healthy" } }),
      isError: ref(false),
      isPending: ref(false),
      isStale: ref(false),
      refetch: vi.fn(),
    };
    Object.defineProperty(window.navigator, "onLine", { configurable: true, value: true });
    const add = vi.spyOn(window, "addEventListener");
    const remove = vi.spyOn(window, "removeEventListener");
    const component = defineComponent({
      setup() {
        const { status } = useAppStatus();
        return () => h("p", status.value?.kind ?? "healthy");
      },
    });

    const wrapper = mount(component);
    expect(add).toHaveBeenCalledWith("online", expect.any(Function));
    expect(add).toHaveBeenCalledWith("offline", expect.any(Function));
    Object.defineProperty(window.navigator, "onLine", { configurable: true, value: false });
    window.dispatchEvent(new Event("offline"));
    await nextTick();
    expect(wrapper.text()).toBe("offline");

    wrapper.unmount();
    expect(remove).toHaveBeenCalledWith("online", expect.any(Function));
    expect(remove).toHaveBeenCalledWith("offline", expect.any(Function));
  });
});
