import { QueryClient, VueQueryPlugin } from "@tanstack/vue-query";
import { flushPromises, mount } from "@vue/test-utils";
import { Events, Window } from "@wailsio/runtime";
import { nextTick, ref } from "vue";
import type { Ref } from "vue";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { QuotaWindowKind } from "@bindings/github.com/SisyphusSQ/codex-pulse/internal/store/models";
import { createAppI18n } from "@/i18n";

import PopoverView from "./PopoverView.vue";

const popoverHarness = vi.hoisted(() => ({
  events: new Map<string, () => void>(),
  mainFocus: vi.fn(),
  mainShow: vi.fn(),
  popoverHide: vi.fn(),
  queries: {} as Record<string, unknown>,
  requestClock: undefined as Ref<number> | undefined,
}));

vi.mock("@/features/popover/usePopoverQueries", () => ({
  usePopoverQueries: (clock: Ref<number>) => {
    popoverHarness.requestClock = clock;
    return popoverHarness.queries;
  },
}));

vi.mock("@wailsio/runtime", () => ({
  Events: {
    Emit: vi.fn(),
    On: vi.fn((name: string, callback: () => void) => {
      popoverHarness.events.set(name, callback);
      return () => popoverHarness.events.delete(name);
    }),
    Types: { Common: { WindowHide: "common:WindowHide", WindowShow: "common:WindowShow" } },
  },
  Window: {
    Get: vi.fn(() => ({ Focus: popoverHarness.mainFocus, Show: popoverHarness.mainShow })),
    Hide: popoverHarness.popoverHide,
  },
}));

function query(data: unknown) {
  return {
    data: ref(data),
    dataUpdatedAt: ref(Date.parse("2026-07-18T02:00:00Z")),
    isError: ref(false),
    isFetching: ref(false),
    isPending: ref(false),
    isPlaceholderData: ref(false),
    refetch: vi.fn(),
  };
}

function quotaWindow(windowKind: QuotaWindowKind, remainingPercent: number) {
  return {
    windowKind,
    remainingPercent,
    resetRemainingMs: 3_600_000,
  };
}

function quotaResponse(windows: ReturnType<typeof quotaWindow>[]) {
  return {
    current: {
      windows,
      resetCredits: {
        availableCount: 2,
        cumulativeRemainingMs: 3_600_000,
        nextExpiresAtMs: Date.parse("2026-07-19T02:00:00Z"),
        totalCount: 3,
      },
    },
  };
}

function readyQueries(windows: ReturnType<typeof quotaWindow>[]) {
  return {
    quota: query(quotaResponse(windows)),
    usage: query(undefined),
    sessions: query({ items: [] }),
    timeZone: "Asia/Shanghai",
  };
}

function renderPopover() {
  const queryClient = new QueryClient();
  const wrapper = mount(PopoverView, {
    global: {
      plugins: [[VueQueryPlugin, { queryClient }], createAppI18n()],
    },
  });
  return { queryClient, wrapper };
}

describe("PopoverView", () => {
  beforeEach(() => {
    popoverHarness.events.clear();
    popoverHarness.mainFocus.mockClear();
    popoverHarness.mainShow.mockClear();
    popoverHarness.popoverHide.mockClear();
    vi.mocked(Events.Emit).mockClear();
    vi.mocked(Window.Get).mockClear();
    popoverHarness.requestClock = undefined;
    popoverHarness.queries = readyQueries([
      quotaWindow(QuotaWindowKind.QuotaWindowSecondary, 64),
    ]);
  });

  it("hides the complete 5-hour row while primary quota is absent and restores it when data returns", async () => {
    const { wrapper } = renderPopover();

    expect(wrapper.text()).toContain("本周");
    expect(wrapper.text()).not.toContain("5 小时");
    expect(wrapper.text()).not.toContain("5 小时 --");

    const quota = (popoverHarness.queries as ReturnType<typeof readyQueries>).quota;
    quota.data.value = quotaResponse([
      quotaWindow(QuotaWindowKind.QuotaWindowPrimary, 42),
      quotaWindow(QuotaWindowKind.QuotaWindowSecondary, 64),
    ]);
    await nextTick();

    expect(wrapper.text()).toContain("5 小时");
    expect(wrapper.text()).not.toContain("5 小时 --");
  });

  it("keeps authoritative quota visible after a refresh error", async () => {
    const { wrapper } = renderPopover();
    const quota = (popoverHarness.queries as ReturnType<typeof readyQueries>).quota;

    quota.isError.value = true;
    await nextTick();

    expect(wrapper.text()).toContain("本周");
    expect(wrapper.text()).toContain("刷新失败，保留上次可信数据");
    expect(wrapper.text()).not.toContain("重新读取额度");
  });

  it("cancels all popover query regions when the window hides", async () => {
    const { queryClient } = renderPopover();
    const cancel = vi.spyOn(queryClient, "cancelQueries");

    popoverHarness.events.get("common:WindowHide")?.();
    await nextTick();

    expect(cancel).toHaveBeenCalledTimes(3);
    expect(cancel).toHaveBeenCalledWith({ queryKey: ["business", "quota"] });
    expect(cancel).toHaveBeenCalledWith({ queryKey: ["business", "usage"] });
    expect(cancel).toHaveBeenCalledWith({ queryKey: ["business", "sessions"] });
  });

  it("updates the local-day request clock whenever the window shows", async () => {
    const now = vi.spyOn(Date, "now").mockReturnValue(100);
    renderPopover();
    expect(popoverHarness.requestClock?.value).toBe(100);

    now.mockReturnValue(200);
    popoverHarness.events.get("common:WindowShow")?.();
    await nextTick();

    expect(popoverHarness.requestClock?.value).toBe(200);
    now.mockRestore();
  });

  it("navigates through the durable named main window and then hides the popover", async () => {
    const { wrapper } = renderPopover();

    await wrapper.get(".popover__open").trigger("click");
    await flushPromises();

    expect(Events.Emit).toHaveBeenCalledWith("codex-pulse:navigate", { path: "/overview" });
    expect(Window.Get).toHaveBeenCalledWith("main");
    expect(popoverHarness.mainShow).toHaveBeenCalledOnce();
    expect(popoverHarness.mainFocus).toHaveBeenCalledOnce();
    expect(popoverHarness.popoverHide).toHaveBeenCalledOnce();
  });
});
