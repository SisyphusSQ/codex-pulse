import { flushPromises, mount } from "@vue/test-utils";
import { nextTick, ref } from "vue";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { RuntimeAction } from "@bindings/github.com/SisyphusSQ/codex-pulse/internal/app/models";
import { createAppI18n } from "@/i18n";

import LocalStatusView from "./LocalStatusView.vue";

const harness = vi.hoisted(() => ({ page: {} as Record<string, unknown> }));
vi.mock("@/features/runtime/useLocalStatusPage", () => ({ useLocalStatusPage: () => harness.page }));

const numeric = (value: number, unit = "count") => ({ value, unit, unknownReason: null });
const unknown = (unit = "milliseconds") => ({ value: null, unit, unknownReason: "not_computed" });
const meta = { issues: null, page: null, status: "complete", version: "query-v1" };

function query(data: unknown) {
  return {
    data: ref(data), isError: ref(false), isFetching: ref(false), isPending: ref(false),
    isStale: ref(false), refetch: vi.fn(),
  };
}

function readyPage() {
  return {
    sources: query({ meta, summary: { total: numeric(1), attention: numeric(0) }, items: [{
      sourceKey: "private-source-id", kind: "local_file", sourceType: "session", state: "active",
      sizeBytes: numeric(2048, "bytes"), parsedBytes: numeric(1024, "bytes"),
      lastAttemptAtMs: numeric(1_784_100_000_000, "milliseconds"), lastSuccessAtMs: numeric(1_784_099_000_000, "milliseconds"),
    }] }),
    jobs: query({ meta, summary: { total: numeric(1) }, items: [{
      jobId: "private-job-id", phase: "backfill", state: "running",
      progress: { current: numeric(4), total: numeric(10) },
      startedAtMs: numeric(1_784_100_000_000, "milliseconds"), nextRetryAtMs: unknown(),
    }] }),
    health: query({ meta, summary: { level: "blocked", active: numeric(1) }, items: [{ eventId: "private-event-id", severity: "warning", active: true }] }),
    action: { data: ref<unknown>(undefined), isError: ref(false), isPending: ref(false) },
    repair: { data: ref<unknown>(undefined), isError: ref(false), isPending: ref(false) },
    analyzeRepair: vi.fn(),
    runAction: vi.fn(),
  };
}

describe("LocalStatusView", () => {
  beforeEach(() => { harness.page = readyPage(); });
  afterEach(() => { document.body.replaceChildren(); });

  it("renders finite summaries without private source, job or event identities", () => {
    const wrapper = mount(LocalStatusView, { attachTo: document.body, global: { plugins: [createAppI18n()] } });
    expect(wrapper.findAll("[data-testid='runtime-source']")).toHaveLength(1);
    expect(wrapper.findAll("[data-testid='runtime-job']")).toHaveLength(1);
    expect(wrapper.findAll("[data-testid='runtime-health']")).toHaveLength(1);
    expect(wrapper.text()).toContain("本机会话日志");
    expect(wrapper.text()).toContain("历史回填");
    expect(wrapper.get("[data-testid='local-status-banner']").text()).toContain("阻塞");
    expect(wrapper.get("[data-testid='runtime-source']").text()).toContain("1 KB / 2 KB");
    expect(wrapper.get("[data-testid='runtime-job']").text()).toContain("4 / 10");
    expect(wrapper.get("[data-testid='runtime-source']").text()).toContain("最近成功");
    expect(wrapper.get("[data-testid='runtime-job']").text()).toContain("开始时间");
    expect(wrapper.text()).not.toContain("private-source-id");
    expect(wrapper.text()).not.toContain("private-job-id");
    expect(wrapper.text()).not.toContain("private-event-id");
  });

  it("requires keyboard-safe confirmation for pause_all and dispatches only finite actions plus Analyze", async () => {
    const page = harness.page as ReturnType<typeof readyPage>;
    const wrapper = mount(LocalStatusView, { attachTo: document.body, global: { plugins: [createAppI18n()] } });
    await wrapper.get("[data-testid='runtime-pause-backfill']").trigger("click");
    await wrapper.get("[data-testid='runtime-pause-all']").trigger("click");
    expect(page.runAction).toHaveBeenCalledTimes(1);
    const dialog = wrapper.get("[data-testid='pause-all-dialog']");
    expect(dialog.attributes("role")).toBe("dialog");
    await nextTick();
    expect(document.activeElement).toBe(wrapper.get("[data-testid='pause-all-confirm']").element);
    await dialog.trigger("keydown", { key: "Escape" });
    expect(page.runAction).toHaveBeenCalledTimes(1);
    await wrapper.get("[data-testid='runtime-pause-all']").trigger("click");
    await wrapper.get("[data-testid='pause-all-confirm']").trigger("click");
    await wrapper.get("[data-testid='runtime-resume']").trigger("click");
    await wrapper.get("[data-testid='runtime-reconcile']").trigger("click");
    expect(page.runAction.mock.calls.map(([action]) => action)).toEqual([
      RuntimeAction.RuntimeActionPauseBackfill,
      RuntimeAction.RuntimeActionPauseAll,
      RuntimeAction.RuntimeActionResume,
      RuntimeAction.RuntimeActionReconcile,
    ]);
    await wrapper.get("[data-testid='runtime-repair-analyze']").trigger("click");
    expect(page.analyzeRepair).toHaveBeenCalledOnce();
  });

  it("separates unavailable from empty and marks cached stale facts as last trusted data", async () => {
    const page = readyPage();
    page.sources.data.value = undefined;
    page.sources.isError.value = true;
    harness.page = page;
    const wrapper = mount(LocalStatusView, { global: { plugins: [createAppI18n()] } });
    expect(wrapper.find("[data-testid='source-unavailable']").exists()).toBe(true);
    expect(wrapper.text()).not.toContain("暂无来源");

    page.sources.data.value = readyPage().sources.data.value;
    page.sources.isStale.value = true;
    await nextTick();
    expect(wrapper.findAll("[data-testid='runtime-source']")).toHaveLength(1);
    expect(wrapper.get("[data-testid='source-stale']").text()).toContain("上次可信数据");
  });

  it("announces durable action and Analyze results through polite live regions", async () => {
    const page = readyPage();
    page.action.data.value = { action: "resume" };
    page.repair.data.value = { actionCount: 2, conflictCount: 1 };
    harness.page = page;
    const wrapper = mount(LocalStatusView, { global: { plugins: [createAppI18n()] } });
    await flushPromises();
    expect(wrapper.get("[data-testid='runtime-action-result']").attributes("role")).toBe("status");
    expect(wrapper.get("[data-testid='repair-result']").attributes("aria-live")).toBe("polite");
  });
});
