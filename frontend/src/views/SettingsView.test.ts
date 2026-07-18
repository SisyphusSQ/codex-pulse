import { flushPromises, mount } from "@vue/test-utils";
import { nextTick, ref } from "vue";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { HomeSwitchStrategy } from "@bindings/github.com/SisyphusSQ/codex-pulse/internal/app/models";
import { createAppI18n } from "@/i18n";

import SettingsView from "./SettingsView.vue";

const harness = vi.hoisted(() => ({ page: {} as Record<string, unknown> }));
vi.mock("@/features/settings/useSettingsPage", () => ({ useSettingsPage: () => harness.page }));
vi.mock("@/features/updates/UpdatePanel.vue", () => ({ default: { template: "<div data-testid='update-panel-stub' />" } }));

function mutation() {
  return { data: ref<unknown>(undefined), isError: ref(false), isPending: ref(false), mutate: vi.fn(), reset: vi.fn() };
}

function settingsResponse(switchStatus = "stable") {
  const editableKeys = [
    "online.quotaEnabled", "online.resetCreditsEnabled", "refresh.quotaIntervalSeconds",
    "refresh.resetCreditsIntervalSeconds", "refresh.reconcileIntervalSeconds",
    "refresh.jsonlDebounceMilliseconds", "updates.autoCheckEnabled", "updates.checkIntervalSeconds",
    "ui.launchBehavior", "ui.overviewRange",
  ];
  return {
    meta: { issues: null, page: null, status: "complete", version: "query-v1" },
    snapshot: {
      revision: "7", online: { quotaEnabled: false, resetCreditsEnabled: true },
      refresh: { quotaIntervalSeconds: 300, resetCreditsIntervalSeconds: 1800, reconcileIntervalSeconds: 1800, jsonlDebounceMilliseconds: 4000 },
      updates: { autoCheckEnabled: true, autoDownloadEnabled: false, channel: "stable", checkIntervalSeconds: 3600, skippedVersion: "private-version", snoozeUntilMs: {}, lastCheckAtMs: {} },
      ui: { locale: "zh-CN", launchBehavior: "tray", overviewRange: "seven_days" },
      home: { configured: true, generation: "3", switchStatus, lastSwitchOutcome: null },
    },
    editableFields: editableKeys.map((key) => ({ key, editable: true, type: "boolean", minimum: null, maximum: null, options: null })),
    privatePath: "/Users/private/codex-home",
  };
}

function readyPage(switchStatus = "stable") {
  return {
    settings: {
      data: ref(settingsResponse(switchStatus)), isError: ref(false), isFetching: ref(false),
      isPending: ref(false), isStale: ref(false), refetch: vi.fn(),
    },
    update: mutation(), plan: mutation(), confirm: mutation(), recover: mutation(),
  };
}

describe("SettingsView", () => {
  beforeEach(() => { harness.page = readyPage(); });
  afterEach(() => { document.body.replaceChildren(); });

  it("saves only editable fields with the current revision and never renders private Home data", async () => {
    const page = harness.page as ReturnType<typeof readyPage>;
    const wrapper = mount(SettingsView, { attachTo: document.body, global: { plugins: [createAppI18n()] } });
    await wrapper.get("[data-testid='setting-quota-enabled']").setValue(true);
    await wrapper.get("form").trigger("submit");
    const request = page.update.mutate.mock.calls[0]?.[0];
    expect(request).toMatchObject({ expectedRevision: "7", online: { quotaEnabled: true, resetCreditsEnabled: true } });
    expect(request.updates).not.toHaveProperty("autoDownloadEnabled");
    expect(request.updates).not.toHaveProperty("skippedVersion");
    expect(request.ui).not.toHaveProperty("locale");
    expect(wrapper.text()).not.toContain("/Users/private/codex-home");
    expect(wrapper.text()).not.toContain("private-version");
  });

  it("clears a private target path after planning and confirms only the redacted impact", async () => {
    const page = harness.page as ReturnType<typeof readyPage>;
    const wrapper = mount(SettingsView, { attachTo: document.body, global: { plugins: [createAppI18n()] } });
    const input = wrapper.get("[data-testid='home-target-path']");
    await input.setValue("/Users/private/new-home");
    await wrapper.get("[data-testid='home-plan']").trigger("click");
    expect(page.plan.mutate).toHaveBeenCalledWith({
      targetPath: "/Users/private/new-home",
      strategy: HomeSwitchStrategy.HomeSwitchClearAndRebuild,
    }, expect.any(Object));
    page.plan.mutate.mock.calls[0]?.[1].onSuccess();
    await nextTick();
    expect((input.element as HTMLInputElement).value).toBe("");

    page.plan.data.value = { targetGeneration: "4", strategy: "clear_and_rebuild", clearsDerivedFacts: true, preservesOldFacts: false };
    await nextTick();
    page.plan.mutate.mock.calls[0]?.[1].onSuccess();
    await nextTick();
    expect(document.activeElement).toBe(wrapper.get("[data-testid='home-impact']").element);
    expect(wrapper.get("[data-testid='home-impact']").text()).not.toContain("/Users/private/new-home");
    await wrapper.get("[data-testid='home-confirm']").trigger("click");
    await flushPromises();
    expect(page.confirm.mutate).toHaveBeenCalledOnce();
    page.confirm.mutate.mock.calls[0]?.[1].onSettled();
    expect(document.activeElement).toBe(input.element);
    page.confirm.data.value = { result: "completed" };
    await nextTick();
    expect(wrapper.get("[data-testid='home-operation-result']").attributes("role")).toBe("status");
  });

  it("offers explicit recovery only when the authority reports recovery_required", () => {
    harness.page = readyPage("recovery_required");
    const wrapper = mount(SettingsView, { global: { plugins: [createAppI18n()] } });
    expect(wrapper.find("[data-testid='home-recover']").exists()).toBe(true);
  });

  it("restores Home input focus after recovery settles", async () => {
    harness.page = readyPage("recovery_required");
    const page = harness.page as ReturnType<typeof readyPage>;
    const wrapper = mount(SettingsView, { attachTo: document.body, global: { plugins: [createAppI18n()] } });
    await wrapper.get("[data-testid='home-recover']").trigger("click");
    page.recover.mutate.mock.calls[0]?.[1].onSettled();
    expect(document.activeElement).toBe(wrapper.get("[data-testid='home-target-path']").element);
  });

  it("keeps cached settings visible while announcing stale or unavailable authority", async () => {
    const page = harness.page as ReturnType<typeof readyPage>;
    page.settings.isError.value = true;
    page.settings.isStale.value = true;
    const wrapper = mount(SettingsView, { global: { plugins: [createAppI18n()] } });
    expect(wrapper.find("[data-testid='settings-stale']").exists()).toBe(true);
    expect(wrapper.find("[data-testid='settings-save']").exists()).toBe(true);
    page.settings.data.value.meta.status = "unavailable";
    await nextTick();
    expect(wrapper.get("[data-testid='settings-stale']").text()).toContain("上次可信设置");
  });

  it("uses polite live status for committed settings results", () => {
    const page = harness.page as ReturnType<typeof readyPage>;
    page.update.data.value = { result: "applied" };
    const wrapper = mount(SettingsView, { global: { plugins: [createAppI18n()] } });
    expect(wrapper.get("[data-testid='settings-save-result']").attributes("role")).toBe("status");
  });

  it("gives the Home target and strategy explicit accessible labels", () => {
    const wrapper = mount(SettingsView, { global: { plugins: [createAppI18n()] } });
    expect(wrapper.get("[data-testid='home-target-path']").attributes("aria-label")).toBe("新的 Codex Home 路径");
    expect(wrapper.get("[data-testid='home-strategy']").attributes("aria-label")).toBe("切换策略");
  });
});
