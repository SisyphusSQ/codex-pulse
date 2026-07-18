import { mount } from "@vue/test-utils";
import { nextTick, ref } from "vue";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { createAppI18n } from "@/i18n";
import UpdatePanel from "./UpdatePanel.vue";

const harness = vi.hoisted(() => ({ panel: {} as Record<string, unknown> }));
vi.mock("./useUpdatePanel", () => ({ useUpdatePanel: () => harness.panel }));

function mutation() {
  return { isError: ref(false), isPending: ref(false), mutate: vi.fn() };
}

function updateState(overrides: Record<string, unknown> = {}) {
  return {
    phase: "available", currentVersion: "0.1.0", version: "42", displayVersion: "0.2.0", architecture: "arm64",
    releaseNotes: "安全更新", contentLength: "4096", signatureStatus: "succeeded",
    progressStage: "", progressReceived: "0", progressTotal: "0", progressFraction: 0,
    faultCode: "", canCancel: false, readyToInstall: false, autoCheckEnabled: true,
    shutdownPhase: "running", shutdownStage: "", shutdownFailedStage: "",
    checkIntervalSeconds: 3600, skippedVersion: null, snoozeUntilMs: null, lastCheckAtMs: null,
    promptVisible: true, ...overrides,
  };
}

function readyPanel(overrides: Record<string, unknown> = {}) {
  return {
    state: { data: ref(updateState(overrides)), isPending: ref(false), isError: ref(false), refetch: vi.fn() },
    check: mutation(), download: mutation(), install: mutation(), cancel: mutation(), skip: mutation(), snooze: mutation(),
  };
}

describe("UpdatePanel", () => {
  beforeEach(() => { harness.panel = readyPanel(); });
  afterEach(() => { document.body.replaceChildren(); });

  it("requires a keyboard-safe confirmation before download", async () => {
    const panel = harness.panel as ReturnType<typeof readyPanel>;
    const wrapper = mount(UpdatePanel, { attachTo: document.body, global: { plugins: [createAppI18n()] } });
    await wrapper.get("[data-testid='update-download']").trigger("click");
    await nextTick();
    const dialog = wrapper.get("[data-testid='update-download-dialog']");
    expect(dialog.attributes("role")).toBe("dialog");
    expect(document.activeElement).toBe(wrapper.get("[data-testid='update-download-confirm']").element);

    await dialog.trigger("keydown", { key: "Tab" });
    expect(document.activeElement).toBe(wrapper.get("[data-testid='update-download-cancel']").element);
    await dialog.trigger("keydown", { key: "Tab", shiftKey: true });
    expect(document.activeElement).toBe(wrapper.get("[data-testid='update-download-confirm']").element);
    await wrapper.get("[data-testid='update-download-confirm']").trigger("click");
    expect(panel.download.mutate).toHaveBeenCalledWith(undefined, expect.any(Object));
    const options = vi.mocked(panel.download.mutate).mock.calls[0]?.[1] as { onSettled?: () => void };
    panel.state.data.value = updateState({ phase: "downloading", promptVisible: false, canCancel: true });
    options.onSettled?.();
    await nextTick();
    expect(document.activeElement).toBe(wrapper.get("[data-testid='update-status']").element);
  });

  it("exposes skip and snooze before an update is ready", async () => {
    const panel = harness.panel as ReturnType<typeof readyPanel>;
    const wrapper = mount(UpdatePanel, { global: { plugins: [createAppI18n()] } });
    await wrapper.get("[data-testid='update-snooze']").trigger("click");
    await wrapper.get("[data-testid='update-skip']").trigger("click");
    expect(panel.snooze.mutate).toHaveBeenCalledWith(3600);
    expect(panel.skip.mutate).toHaveBeenCalledWith("42");
    expect(wrapper.text()).toContain("安全更新");
    expect(wrapper.find("[data-testid='update-install']").exists()).toBe(false);
  });

  it("requires confirmation before safe install and reports draining", async () => {
    harness.panel = readyPanel({ readyToInstall: true });
    const panel = harness.panel as ReturnType<typeof readyPanel>;
    const wrapper = mount(UpdatePanel, { attachTo: document.body, global: { plugins: [createAppI18n()] } });
    expect(wrapper.get("[data-testid='update-ready']").attributes("role")).toBe("status");
    expect(wrapper.text()).toContain("安全结束后台任务后安装并重启");
    expect(wrapper.find("[data-testid='update-download']").exists()).toBe(false);
    await wrapper.get("[data-testid='update-install']").trigger("click");
    expect(wrapper.get("[data-testid='update-install-dialog']").attributes("role")).toBe("dialog");
    await wrapper.get("[data-testid='update-install-confirm']").trigger("click");
    expect(panel.install.mutate).toHaveBeenCalledWith(undefined, expect.any(Object));
    panel.state.data.value = updateState({ readyToInstall: true, shutdownPhase: "draining", shutdownStage: "sqlite" });
    await nextTick();
    expect(wrapper.text()).toContain("正在安全结束后台索引");
    expect(wrapper.text()).toContain("sqlite");
    expect(wrapper.find("[data-testid='update-install']").exists()).toBe(false);

    panel.state.data.value = updateState({
      readyToInstall: true,
      shutdownPhase: "closed",
      shutdownStage: "closed",
      shutdownFailedStage: "sqlite",
    });
    await nextTick();
    expect(wrapper.text()).toContain("安全关闭在“sqlite”阶段失败");
    expect(wrapper.find("[data-testid='update-install']").exists()).toBe(false);
  });
});
