import { flushPromises, mount } from "@vue/test-utils";
import { ref } from "vue";
import { createMemoryHistory, createRouter } from "vue-router";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { createAppI18n } from "@/i18n";

import AppStatusBanner from "./AppStatusBanner.vue";

const harness = vi.hoisted(() => ({ value: {} as Record<string, unknown> }));
vi.mock("./useAppStatus", () => ({ useAppStatus: () => harness.value }));

function router() {
  return createRouter({ history: createMemoryHistory(), routes: [
    { path: "/overview", name: "overview", component: { template: "<p />" } },
    { path: "/local-status", name: "local-status", component: { template: "<p />" } },
    { path: "/local-status/data-health", name: "data-health", component: { template: "<p />" } },
  ] });
}

describe("AppStatusBanner", () => {
  beforeEach(() => {
    harness.value = {
      retry: vi.fn().mockResolvedValue(undefined),
      status: ref({ kind: "blocked", retryable: true, evaluatedAtMs: 1_784_100_000_000, failure: "none", primary: {
        component: "storage", level: "blocked", evidence: "known", reason: "store_disk_full",
        impact: "storage_at_risk", protection: "writes_stopped", recoveryAction: "free_space",
      } }),
    };
  });

  it("renders exactly one primary with impact, reason, time and registered recovery", async () => {
    const appRouter = router(); await appRouter.push("/overview");
    const wrapper = mount(AppStatusBanner, { global: { plugins: [appRouter, createAppI18n()] } });
    expect(wrapper.findAll("[data-testid='app-status-banner']")).toHaveLength(1);
    expect(wrapper.get("[data-testid='app-status-banner']").attributes("role")).toBe("alert");
    expect(wrapper.get("[data-testid='app-status-detail']").text()).toContain("本机存储存在风险");
    expect(wrapper.get("[data-testid='app-status-detail']").text()).toContain("本机磁盘已满");
    expect(wrapper.text()).toContain("释放空间");
    await wrapper.get("[data-testid='app-status-recovery']").trigger("click");
    await flushPromises();
    expect(appRouter.currentRoute.value.fullPath).toBe("/local-status/data-health");
  });

  it("uses retry for stale query recovery and keeps detail navigation", async () => {
    harness.value.status = ref({ kind: "stale", retryable: true, evaluatedAtMs: 1_784_100_000_000, primary: null, failure: "persist", lastTrusted: true });
    const appRouter = router(); await appRouter.push("/overview");
    const wrapper = mount(AppStatusBanner, { global: { plugins: [appRouter, createAppI18n()] } });
    expect(wrapper.get("[data-testid='app-status-detail']").text()).toContain("本机健康结果暂未保存");
    expect(wrapper.get("[data-testid='app-status-detail']").text()).toContain("上次可信状态");
    await wrapper.get("[data-testid='app-status-recovery']").trigger("click");
    expect(harness.value.retry).toHaveBeenCalledOnce();
    await wrapper.get("[data-testid='app-status-details']").trigger("click");
    await flushPromises();
    expect(appRouter.currentRoute.value.fullPath).toBe("/local-status/data-health");
  });

  it("reports a failed recovery inline without removing detail navigation", async () => {
    harness.value.retry = vi.fn().mockRejectedValue(new Error("synthetic"));
    harness.value.status = ref({ kind: "unavailable", retryable: true, evaluatedAtMs: null, primary: null, failure: "none", lastTrusted: false });
    const appRouter = router(); await appRouter.push("/overview");
    const wrapper = mount(AppStatusBanner, { global: { plugins: [appRouter, createAppI18n()] } });
    await wrapper.get("[data-testid='app-status-recovery']").trigger("click");
    await flushPromises();
    expect(wrapper.get("[role='alert']").text()).toContain("操作未完成");
    expect(wrapper.find("[data-testid='app-status-details']").exists()).toBe(true);
  });

  it("does not reserve a live region when health is authoritative and healthy", () => {
    harness.value.status = ref(null);
    const wrapper = mount(AppStatusBanner, { global: { plugins: [router(), createAppI18n()] } });
    expect(wrapper.find("[data-testid='app-status-banner']").exists()).toBe(false);
  });
});
