import { flushPromises } from "@vue/test-utils";
import type { CancellablePromise } from "@wailsio/runtime";
import type { App as VueApp } from "vue";
import { createMemoryHistory } from "vue-router";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

// The real Wails runtime starts a DOM polling interval as a module side effect.
// App tests inject or observe only its public seams, so use deterministic stubs
// instead of carrying the desktop runtime lifecycle into jsdom.
vi.mock("@wailsio/runtime", () => ({
  Call: { ByID: vi.fn() },
  Events: {
    On: vi.fn(() => () => undefined),
    Types: {
      Common: {
        SystemDidWake: "common:system-did-wake",
        WindowRuntimeReady: "common:window-runtime-ready",
      },
      Mac: { ApplicationDidBecomeActive: "mac:application-did-become-active" },
    },
  },
}));

import { HealthProjection, Settings } from "@bindings/github.com/SisyphusSQ/codex-pulse/internal/app/service";
import { Bootstrap } from "@bindings/github.com/SisyphusSQ/codex-pulse/internal/app/startupservice";
import { ApplicationMode } from "@bindings/github.com/SisyphusSQ/codex-pulse/internal/app/models";

import { createAppDependencies, createCodexPulseApp } from "./app";
import type { QueryInvalidationEventSource } from "./events/queryInvalidation";

vi.mock("@bindings/github.com/SisyphusSQ/codex-pulse/internal/app/service", async (importOriginal) => ({
  ...await importOriginal<typeof import("@bindings/github.com/SisyphusSQ/codex-pulse/internal/app/service")>(),
  HealthProjection: vi.fn(),
  Settings: vi.fn(),
}));
vi.mock("@bindings/github.com/SisyphusSQ/codex-pulse/internal/app/startupservice", () => ({ Bootstrap: vi.fn() }));

const bootstrapMock = vi.mocked(Bootstrap);
const healthMock = vi.mocked(HealthProjection);
const settingsMock = vi.mocked(Settings);
const mountedApps: VueApp[] = [];

function cancellable<T>(promise: Promise<T>): CancellablePromise<T> {
  return Object.assign(promise, {
    cancel: vi.fn(),
    cancelOn() { return this; },
  }) as unknown as CancellablePromise<T>;
}

function healthResponse(level = "healthy") {
  const components = ["local_index", "live_queue", "history_backfill", "online_quota", "storage", "runtime", "updater"].map((component) => ({
    component, level: "healthy", evidence: "known", reason: "healthy", impact: "none", protection: "none", recoveryAction: "none",
  }));
  const primary = level === "blocked" ? {
    component: "storage", level: "blocked", evidence: "known", reason: "store_disk_full",
    impact: "storage_at_risk", protection: "writes_stopped", recoveryAction: "free_space",
  } : null;
  return {
    hasValue: true, stale: false, failure: "none", level, primary,
    evaluatedAtMs: { value: 1_784_100_000_000, unit: "milliseconds", unknownReason: null },
    components: primary === null ? components : components.map((item) => item.component === "storage" ? primary : item),
  } as Awaited<ReturnType<typeof HealthProjection>>;
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
  await flushPromises();
  mountedApps.push(app);

  return { dependencies, host };
}

describe("Codex Pulse application shell", () => {
  beforeEach(() => {
    bootstrapMock.mockReset();
    bootstrapMock.mockResolvedValue({
      name: "Codex Pulse", locale: "zh-CN", platform: "darwin", mode: ApplicationMode.ApplicationModeNormal, recovery: null,
    });
    healthMock.mockReset();
    healthMock.mockReturnValue(cancellable(Promise.resolve(healthResponse())));
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
    await flushPromises();

    expect(host.querySelector("[data-testid='settings-loading']")?.textContent).toContain("正在读取设置");
    expect(host.querySelector("[data-testid='app-status-banner']")).toBeNull();
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

  it("shows only one global high-priority banner across a business route", async () => {
    healthMock.mockReturnValue(cancellable(Promise.resolve(healthResponse("blocked"))));
    settingsMock.mockReturnValue(cancellable(Promise.resolve(settingsResponse())));

    const { host } = await renderApp();
    await flushPromises();

    expect(host.querySelectorAll("[data-testid='app-status-banner']")).toHaveLength(1);
    expect(host.querySelector("[data-testid='app-status-banner']")?.textContent).toContain("阻塞");
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
