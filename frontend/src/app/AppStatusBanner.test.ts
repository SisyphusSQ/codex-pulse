import { mount } from "@vue/test-utils";
import { ref } from "vue";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { createAppI18n } from "@/i18n";

import AppStatusBanner from "./AppStatusBanner.vue";

const harness = vi.hoisted(() => ({ value: {} as Record<string, unknown> }));
vi.mock("./useAppStatus", () => ({ useAppStatus: () => harness.value }));

describe("AppStatusBanner", () => {
  beforeEach(() => {
    harness.value = {
      retry: vi.fn(),
      status: ref({ kind: "blocked", retryable: true }),
    };
  });

  it("renders exactly one global high-priority status and its recovery action", async () => {
    const wrapper = mount(AppStatusBanner, { global: { plugins: [createAppI18n()] } });

    expect(wrapper.findAll("[data-testid='app-status-banner']")).toHaveLength(1);
    expect(wrapper.get("[data-testid='app-status-banner']").attributes("role")).toBe("alert");
    expect(wrapper.get("[data-testid='app-status-banner']").classes()).toContain("border-red-300");
    expect(wrapper.text()).toContain("本机数据处理已阻塞");
    await wrapper.get("[data-testid='app-status-retry']").trigger("click");
    expect(harness.value.retry).toHaveBeenCalledOnce();
  });

  it("uses distinct finite severity styles for degraded and loading states", () => {
    harness.value.status = ref({ kind: "degraded", retryable: true });
    const degraded = mount(AppStatusBanner, { global: { plugins: [createAppI18n()] } });
    expect(degraded.get("[data-testid='app-status-banner']").classes()).toContain("border-amber-200");
    expect(degraded.get("[data-testid='app-status-banner']").attributes("role")).toBe("status");
    degraded.unmount();

    harness.value.status = ref({ kind: "loading", retryable: false });
    const loading = mount(AppStatusBanner, { global: { plugins: [createAppI18n()] } });
    expect(loading.get("[data-testid='app-status-banner']").classes()).toContain("border-blue-200");
  });

  it("does not reserve a live region when the global status is healthy", () => {
    harness.value.status = ref(null);
    const wrapper = mount(AppStatusBanner, { global: { plugins: [createAppI18n()] } });
    expect(wrapper.find("[data-testid='app-status-banner']").exists()).toBe(false);
  });
});
