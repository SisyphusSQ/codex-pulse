import { flushPromises } from "@vue/test-utils";
import type { App as VueApp } from "vue";
import { createMemoryHistory } from "vue-router";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { Settings } from "@bindings/github.com/SisyphusSQ/codex-pulse/internal/app/service";

import { createAppDependencies, createCodexPulseApp } from "./app";
import type { QueryInvalidationEventSource } from "./events/queryInvalidation";

vi.mock("@bindings/github.com/SisyphusSQ/codex-pulse/internal/app/service", async (importOriginal) => ({
  ...await importOriginal<typeof import("@bindings/github.com/SisyphusSQ/codex-pulse/internal/app/service")>(),
  Settings: vi.fn(),
}));

const settingsMock = vi.mocked(Settings);
const mountedApps: VueApp[] = [];

function cancellable<T>(promise: Promise<T>): ReturnType<typeof Settings> {
  return Object.assign(promise, {
    cancel: vi.fn(),
    cancelOn() { return this; },
  }) as unknown as ReturnType<typeof Settings>;
}

function settingsResponse() {
  return {
    meta: { issues: null, page: null, status: "complete", version: "query-v1" },
    snapshot: {
      schemaVersion: 2, revision: "7", onboardingCompleted: true,
      home: { configured: true, generation: "3", switchStatus: "stable", lastSwitchOutcome: null },
      online: { quotaEnabled: false, resetCreditsEnabled: false },
      refresh: { quotaIntervalSeconds: 300, resetCreditsIntervalSeconds: 1800, reconcileIntervalSeconds: 1800, jsonlDebounceMilliseconds: 4000 },
      updates: {
        autoCheckEnabled: true, autoDownloadEnabled: false, channel: "stable",
        checkIntervalSeconds: 3600, skippedVersion: null,
        snoozeUntilMs: { value: null, unit: "milliseconds", unknownReason: "not_applicable" },
        lastCheckAtMs: { value: null, unit: "milliseconds", unknownReason: "never_loaded" },
      },
      ui: { locale: "zh-CN", launchBehavior: "tray", overviewRange: "seven_days" },
    },
    editableFields: [],
  } as Awaited<ReturnType<typeof Settings>>;
}

async function renderApp(initialPath = "/settings") {
  const dependencies = createAppDependencies({ history: createMemoryHistory() });
  await dependencies.router.push(initialPath);

  const app = createCodexPulseApp(dependencies);
  const host = document.createElement("div");
  document.body.append(host);
  app.mount(host);
  await dependencies.router.isReady();
  mountedApps.push(app);

  return { dependencies, host };
}

describe("Codex Pulse application shell", () => {
  beforeEach(() => {
    settingsMock.mockReset();
  });

  afterEach(() => {
    for (const app of mountedApps.splice(0)) {
      app.unmount();
    }
    document.body.replaceChildren();
  });

  it("shows a loading state while the owned Settings query is pending", async () => {
    settingsMock.mockReturnValue(cancellable(new Promise(() => undefined)));

    const { host } = await renderApp();

    expect(host.querySelector("[data-testid='settings-loading']")?.textContent).toContain("正在读取设置");
    expect(host.querySelectorAll("[aria-live], [role='status'], [role='alert']")).toHaveLength(1);
  });

  it("renders the typed Settings snapshot returned by the Go binding", async () => {
    settingsMock.mockReturnValue(cancellable(Promise.resolve(settingsResponse())));

    const { host } = await renderApp();
    await flushPromises();

    expect(host.querySelector("[data-testid='settings-view']")?.textContent).toContain("只读配置");
    expect(host.textContent).toContain("Codex Pulse");
    expect(host.textContent).toContain("zh-CN");
  });

  it("keeps binding failures explicit and allows a user retry", async () => {
    settingsMock
      .mockReturnValueOnce(cancellable(Promise.reject(new Error("binding unavailable"))))
      .mockReturnValueOnce(cancellable(Promise.resolve(settingsResponse())));

    const { host } = await renderApp();
    await flushPromises();

    expect(host.querySelector("[data-testid='settings-error']")?.textContent).toContain("暂不可用");
    expect(host.querySelectorAll("[aria-live], [role='status'], [role='alert']")).toHaveLength(1);
    (host.querySelector("[data-testid='settings-retry']") as HTMLButtonElement).click();
    await flushPromises();

    expect(settingsMock).toHaveBeenCalledTimes(2);
    expect(host.querySelector("[data-testid='settings-view']")).not.toBeNull();
  });

  it("renders the six-route application shell and follows router selection", async () => {
    settingsMock.mockReturnValue(cancellable(Promise.resolve(settingsResponse())));

    const { dependencies, host } = await renderApp();
    await flushPromises();

    expect(host.querySelector("[data-testid='app-shell']")).not.toBeNull();
    const navigation = host.querySelector("[data-testid='primary-navigation']");
    expect(navigation?.querySelectorAll("a")).toHaveLength(6);
    expect(navigation?.querySelector("a[aria-current='page']")?.getAttribute("href")).toBe("/settings");
    expect(host.querySelector("img")?.getAttribute("alt")).toBe("");
    expect(host.querySelector("img")?.getAttribute("aria-hidden")).toBe("true");

    await dependencies.router.push("/sessions");
    await flushPromises();

    expect(navigation?.querySelector("a[aria-current='page']")?.getAttribute("href")).toBe("/sessions");
  });

  it("releases every Wails event subscription when the app unmounts", async () => {
    settingsMock.mockReturnValue(cancellable(Promise.resolve(settingsResponse())));
    let unsubscribeCalls = 0;
    const subscribe = () => () => { unsubscribeCalls++; };
    const eventSource: QueryInvalidationEventSource = {
      onInvalidation: subscribe,
      onWake: subscribe,
      onRuntimeReady: subscribe,
      onForeground: subscribe,
    };
    const dependencies = createAppDependencies({
      history: createMemoryHistory(),
      eventSource,
    });
    await dependencies.router.push("/");
    const app = createCodexPulseApp(dependencies);
    const host = document.createElement("div");
    document.body.append(host);
    app.mount(host);

    app.unmount();

    expect(unsubscribeCalls).toBe(4);
  });
});
