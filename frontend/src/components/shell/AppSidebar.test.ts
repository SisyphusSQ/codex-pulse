import { mount } from "@vue/test-utils";
import { ref } from "vue";
import { createMemoryHistory, createRouter } from "vue-router";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { createAppI18n } from "@/i18n";

import AppSidebar from "./AppSidebar.vue";

const harness = vi.hoisted(() => ({ health: {} as Record<string, unknown> }));
vi.mock("@/features/runtime/useHealthProjection", () => ({ useHealthProjection: () => harness.health }));

function appRouter() {
  return createRouter({ history: createMemoryHistory(), routes: [
    { path: "/:pathMatch(.*)*", component: { template: "<p />" } },
  ] });
}

describe("AppSidebar health entry", () => {
  beforeEach(() => {
    harness.health = {
      data: ref({ hasValue: true, stale: false, level: "degraded", components: [
        { level: "healthy" }, { level: "degraded" }, { level: "busy" }, { level: "healthy" },
        { level: "healthy" }, { level: "healthy" }, { level: "healthy" },
      ] }),
      isPending: ref(false), isError: ref(false),
    };
  });

  it("shows one overall dot and component attention summary on local status", () => {
    const wrapper = mount(AppSidebar, { global: { plugins: [appRouter(), createAppI18n()] } });
    expect(wrapper.findAll("[data-testid='local-health-entry']")).toHaveLength(1);
    expect(wrapper.get("[data-testid='local-health-summary']").text()).toBe("2 项需关注");
    expect(wrapper.find("[data-testid='local-health-entry'] .bg-amber-500").exists()).toBe(true);
  });

  it("does not disguise unavailable or unknown projection as healthy", async () => {
    harness.health.data = ref(undefined);
    harness.health.isError = ref(true);
    const unavailable = mount(AppSidebar, { global: { plugins: [appRouter(), createAppI18n()] } });
    expect(unavailable.get("[data-testid='local-health-summary']").text()).toBe("不可用");
    unavailable.unmount();

    harness.health.data = ref({ hasValue: false, components: [] });
    harness.health.isError = ref(false);
    const unknown = mount(AppSidebar, { global: { plugins: [appRouter(), createAppI18n()] } });
    expect(unknown.get("[data-testid='local-health-summary']").text()).toBe("状态未知");
  });

  it("keeps cached data as last trusted when refetch fails", () => {
    harness.health.data = ref({
      hasValue: true, stale: false, level: "healthy",
      components: Array.from({ length: 7 }, () => ({ level: "healthy" })),
    });
    harness.health.isError = ref(true);
    const wrapper = mount(AppSidebar, { global: { plugins: [appRouter(), createAppI18n()] } });
    expect(wrapper.get("[data-testid='local-health-summary']").text()).toBe("上次可信 · 7 项正常");
    expect(wrapper.find("[data-testid='local-health-entry'] .bg-amber-500").exists()).toBe(true);
  });
});
