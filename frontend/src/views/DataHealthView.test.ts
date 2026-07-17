import { flushPromises, mount } from "@vue/test-utils";
import { createMemoryHistory } from "vue-router";
import { nextTick, ref } from "vue";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { createAppI18n } from "@/i18n";
import { createAppRouter } from "@/router";

import DataHealthView from "./DataHealthView.vue";

const harness = vi.hoisted(() => ({ page: {} as Record<string, unknown> }));
vi.mock("@/features/runtime/useDataHealthPage", async (importOriginal) => {
  const original = await importOriginal<typeof import("@/features/runtime/useDataHealthPage")>();
  return { ...original, useDataHealthPage: () => harness.page };
});

const numeric = (value: number, unit = "count") => ({ value, unit, unknownReason: null });
const unknown = (unit = "milliseconds") => ({ value: null, unit, unknownReason: "not_computed" });
const meta = { issues: null, page: null, status: "complete", version: "query-v1" };

function query(data: unknown) {
  return {
    data: ref(data), isError: ref(false), isFetching: ref(false), isPending: ref(false),
    isStale: ref(false), refetch: vi.fn(),
  };
}

function mutation() {
  return { data: ref<unknown>(undefined), isError: ref(false), isPending: ref(false), mutate: vi.fn() };
}

function component(component: string, level = "healthy", recoveryAction = "none") {
  return {
    component, level, evidence: "known", reason: level === "healthy" ? "healthy" : "disk_low",
    impact: level === "healthy" ? "none" : "storage_at_risk",
    protection: level === "healthy" ? "none" : "writes_stopped", recoveryAction,
  };
}

function readyPage() {
  const runtime = [{
    capturedAtMs: numeric(1_784_100_000_000, "milliseconds"), cpuPercent: 8.4,
    rssBytes: numeric(186 * 1024 * 1024, "bytes"), peakRssBytes: numeric(200 * 1024 * 1024, "bytes"),
    dbBytes: numeric(1280 * 1024 * 1024, "bytes"), walBytes: numeric(48 * 1024 * 1024, "bytes"),
    diskFreeBytes: numeric(86 * 1024 * 1024 * 1024, "bytes"), liveQueueDepth: numeric(2), backfillQueueDepth: numeric(1),
    oldestLiveWaitMs: numeric(800, "milliseconds"), oldestBackfillWaitMs: numeric(1200, "milliseconds"), droppedSamples: numeric(0),
  }];
  return {
    metrics: query({
      meta, evaluatedAtMs: numeric(1_784_100_000_000, "milliseconds"),
      window: { fromMs: numeric(1_784_013_600_000, "milliseconds"), untilMs: numeric(1_784_100_000_000, "milliseconds") },
      runtime, latest: runtime[0],
      scheduler: {
        cycleCount: numeric(9), completedCycles: numeric(6), yieldedCycles: numeric(2), failedCycles: numeric(1),
        interruptedCycles: numeric(0), filesScanned: numeric(12), bytesRead: numeric(2048, "bytes"),
        activeMs: numeric(500, "milliseconds"), maxCycleActiveMs: numeric(100, "milliseconds"),
        lastProgressAtMs: numeric(1_784_099_000_000, "milliseconds"), lastBackfillProgressAtMs: numeric(1_784_098_000_000, "milliseconds"),
      },
      jobs: { queued: numeric(1), running: numeric(1), interrupted: numeric(0), succeeded: numeric(4), failed: numeric(1), cancelled: numeric(0) },
      sources: { total: numeric(7), current: numeric(5), stale: numeric(1), unavailable: numeric(1), consecutiveFailures: numeric(2), nextRetryAtMs: numeric(1_784_101_000_000, "milliseconds") },
      currentJobs: [{ jobId: "private-job-id", phase: "history_backfill", state: "running", progress: { current: numeric(82), total: numeric(100) }, nextRetryAtMs: unknown() }],
      recentJobs: [{ jobId: "private-recent-job-id", phase: "live", state: "succeeded", progress: { current: numeric(100), total: numeric(100) }, nextRetryAtMs: unknown() }],
      openEvents: [{ eventId: "private-event-id", active: true, component: "history_backfill", severity: "warning", occurrenceCount: numeric(3), impact: "history_incomplete", protection: "retry_backoff", lastSeenAtMs: numeric(1_784_100_000_000, "milliseconds") }],
      recentEvents: [{ eventId: "private-resolved-id", active: false, component: "storage", severity: "info", occurrenceCount: numeric(1), impact: "none", protection: "none", lastSeenAtMs: numeric(1_784_099_000_000, "milliseconds") }],
    }),
    projection: query({
      hasValue: true, stale: false, failure: "none", evaluatedAtMs: numeric(1_784_100_000_000, "milliseconds"),
      level: "degraded", primary: component("storage", "degraded", "free_space"),
      components: ["local_index", "live_queue", "history_backfill", "online_quota", "storage", "runtime", "updater"].map(
        (name) => name === "storage" ? component(name, "degraded", "free_space")
          : name === "local_index" ? component(name, "degraded", "repair_store")
            : name === "history_backfill" ? component(name, "degraded", "check_source") : component(name),
      ),
    }),
    reconcile: mutation(), repair: mutation(), refresh: vi.fn(),
  };
}

async function mountView() {
  const router = createAppRouter(createMemoryHistory());
  await router.push("/local-status/data-health");
  await router.isReady();
  const wrapper = mount(DataHealthView, {
    attachTo: document.body,
    global: {
      plugins: [createAppI18n(), router],
      stubs: { DataHealthTrendChart: { template: "<div data-testid='data-health-trend' />" } },
    },
  });
  return { router, wrapper };
}

describe("DataHealthView", () => {
  beforeEach(() => { harness.page = readyPage(); });
  afterEach(() => { document.body.replaceChildren(); });

  it("renders authoritative components, work, merged events and resources without opaque identities", async () => {
    const { wrapper } = await mountView();
    expect(wrapper.findAll("[data-testid='data-health-component']")).toHaveLength(7);
    expect(wrapper.get("[data-testid='data-health-component']").text()).toContain("可信证据");
    expect(wrapper.get("[data-testid='data-health-source-metrics']").text()).toContain("7 个来源");
    expect(wrapper.get("[data-testid='data-health-source-metrics']").text()).toContain("1 个不可用");
    expect(wrapper.get("[data-testid='data-health-scheduler-metrics']").text()).toContain("9 个调度周期");
    expect(wrapper.findAll("[data-testid='data-health-job']")).toHaveLength(2);
    expect(wrapper.get("[data-testid='data-health-current-jobs']").text()).toContain("当前任务");
    expect(wrapper.get("[data-testid='data-health-recent-jobs']").text()).toContain("最近完成");
    expect(wrapper.get("[data-testid='data-health-current-jobs']").text()).toContain("历史补齐");
    expect(wrapper.findAll("[data-testid='data-health-event']")).toHaveLength(2);
    expect(wrapper.get("[data-testid='data-health-open-events']").text()).toContain("仍需处理");
    expect(wrapper.get("[data-testid='data-health-recent-events']").text()).toContain("最近恢复");
    expect(wrapper.get("[data-testid='data-health-event']").text()).toContain("3 次");
    expect(wrapper.get("[data-testid='data-health-projection-time']").text()).toContain("健康投影评估于");
    expect(wrapper.get("[data-testid='data-health-resources']").text()).toContain("8.4%");
    expect(wrapper.get("[data-testid='data-health-resources']").text()).toContain("186 MB");
    expect(wrapper.get("[data-testid='data-health-resources']").text()).toContain("1.25 GB / 48 MB");
    expect(wrapper.find("[data-testid='data-health-trend']").exists()).toBe(true);
    expect(wrapper.text()).not.toContain("private-job-id");
    expect(wrapper.text()).not.toContain("private-event-id");
  });

  it("previews reconcile and repair dry-run before dispatch, then announces result", async () => {
    const page = harness.page as ReturnType<typeof readyPage>;
    const { wrapper } = await mountView();
    await wrapper.get("[data-testid='data-health-reconcile']").trigger("click");
    await nextTick();
    expect(page.reconcile.mutate).not.toHaveBeenCalled();
    const dialog = wrapper.get("[data-testid='data-health-action-preview']");
    expect(dialog.attributes("role")).toBe("dialog");
    expect(wrapper.get("[data-testid='data-health-content']").attributes()).toHaveProperty("inert");
    expect(document.activeElement).toBe(wrapper.get("[data-testid='data-health-action-confirm']").element);
    await dialog.trigger("keydown", { key: "Tab" });
    expect(document.activeElement).toBe(wrapper.get("[data-testid='data-health-action-cancel']").element);
    await dialog.trigger("keydown", { key: "Tab", shiftKey: true });
    expect(document.activeElement).toBe(wrapper.get("[data-testid='data-health-action-confirm']").element);
    await dialog.trigger("keydown", { key: "Escape" });
    expect(page.reconcile.mutate).not.toHaveBeenCalled();
    expect(document.activeElement).toBe(wrapper.get("[data-testid='data-health-reconcile']").element);

    await wrapper.get("[data-testid='data-health-reconcile']").trigger("click");
    await nextTick();
    await wrapper.get("[data-testid='data-health-action-confirm']").trigger("click");
    expect(page.reconcile.mutate).toHaveBeenCalledOnce();
    page.reconcile.isPending.value = true;
    await nextTick();
    expect(wrapper.get("[data-testid='data-health-action-progress']").attributes("role")).toBe("status");
    page.reconcile.isPending.value = false;

    page.reconcile.data.value = { action: "reconcile", transition: "steady" };
    await nextTick();
    expect(wrapper.get("[data-testid='data-health-action-result']").attributes("aria-live")).toBe("polite");
    expect(wrapper.get("[data-testid='data-health-action-result']").text()).toContain("状态稳定");

    await wrapper.get("[data-testid='data-health-repair']").trigger("click");
    await wrapper.get("[data-testid='data-health-action-confirm']").trigger("click");
    expect(page.repair.mutate).toHaveBeenCalledOnce();
    page.repair.data.value = { actionCount: 2, conflictCount: 1 };
    await nextTick();
    expect(wrapper.get("[data-testid='data-health-repair-result']").text()).toContain("2 个候选操作，1 个冲突");
  });

  it("routes finite settings recovery and separates unavailable from known empty", async () => {
    const page = harness.page as ReturnType<typeof readyPage>;
    const { router, wrapper } = await mountView();
    await wrapper.get("[data-testid='data-health-settings']").trigger("click");
    await flushPromises();
    expect(router.currentRoute.value.name).toBe("settings");

    page.metrics.data.value = undefined;
    page.metrics.isError.value = true;
    await nextTick();
    expect(wrapper.find("[data-testid='data-health-unavailable']").exists()).toBe(true);
    expect(wrapper.text()).not.toContain("最近 24 小时暂无资源采样");
  });

  it("shows only recovery actions declared by the authoritative projection", async () => {
    const page = harness.page as ReturnType<typeof readyPage>;
    (page.projection.data.value as { components: Array<{ recoveryAction: string }> }).components.forEach((item) => { item.recoveryAction = "none"; });
    const { wrapper } = await mountView();

    expect(wrapper.find("[data-testid='data-health-reconcile']").exists()).toBe(false);
    expect(wrapper.find("[data-testid='data-health-repair']").exists()).toBe(false);
    expect(wrapper.find("[data-testid='data-health-settings']").exists()).toBe(false);
    expect(wrapper.find("[data-testid='data-health-refresh']").exists()).toBe(true);
  });

  it("marks cached projection facts and does not render unavailable projection as known empty", async () => {
    const page = harness.page as ReturnType<typeof readyPage>;
    page.projection.isError.value = true;
    const { wrapper } = await mountView();

    expect(wrapper.get("[data-testid='data-health-last-trusted']").text()).toContain("上次可信数据");
    expect(wrapper.get("[data-testid='data-health-resources']").text()).toContain("8.4%");

    page.projection.data.value = undefined;
    await nextTick();
    expect(wrapper.get("[data-testid='data-health-partial']").attributes("role")).toBe("status");
    expect(wrapper.get("[data-testid='data-health-projection-unavailable']").text()).toContain("健康投影暂不可用");
    expect(wrapper.text()).not.toContain("尚无组件结论");
  });
});
