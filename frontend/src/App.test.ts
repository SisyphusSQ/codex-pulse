import { flushPromises } from "@vue/test-utils";
import type { App as VueApp } from "vue";
import { createMemoryHistory } from "vue-router";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { Bootstrap } from "@bindings/github.com/SisyphusSQ/codex-pulse/internal/app/service";

import { createAppDependencies, createCodexPulseApp } from "./app";
import type { QueryInvalidationEventSource } from "./events/queryInvalidation";

vi.mock("@bindings/github.com/SisyphusSQ/codex-pulse/internal/app/service", () => ({
  Bootstrap: vi.fn(),
}));

const bootstrapMock = vi.mocked(Bootstrap);
const mountedApps: VueApp[] = [];

async function renderApp() {
  const dependencies = createAppDependencies({ history: createMemoryHistory() });
  await dependencies.router.push("/");

  const app = createCodexPulseApp(dependencies);
  const host = document.createElement("div");
  document.body.append(host);
  app.mount(host);
  await dependencies.router.isReady();
  mountedApps.push(app);

  return host;
}

describe("Codex Pulse application shell", () => {
  beforeEach(() => {
    bootstrapMock.mockReset();
  });

  afterEach(() => {
    for (const app of mountedApps.splice(0)) {
      app.unmount();
    }
    document.body.replaceChildren();
  });

  it("shows a loading state while the Go binding is pending", async () => {
    bootstrapMock.mockReturnValue(new Promise(() => undefined) as ReturnType<typeof Bootstrap>);

    const host = await renderApp();

    expect(host.querySelector("[data-testid='service-loading']")?.textContent).toContain("正在连接");
  });

  it("renders the typed metadata returned by the Go binding", async () => {
    bootstrapMock.mockResolvedValue({
      name: "Codex Pulse",
      locale: "zh-CN",
      platform: "darwin",
    });

    const host = await renderApp();
    await flushPromises();

    expect(host.querySelector("[data-testid='service-ready']")?.textContent).toContain("本机服务已连接");
    expect(host.textContent).toContain("Codex Pulse");
    expect(host.textContent).toContain("darwin");
  });

  it("keeps binding failures explicit and allows a user retry", async () => {
    bootstrapMock
      .mockRejectedValueOnce(new Error("binding unavailable"))
      .mockResolvedValueOnce({ name: "Codex Pulse", locale: "zh-CN", platform: "darwin" });

    const host = await renderApp();
    await flushPromises();

    expect(host.querySelector("[data-testid='service-error']")?.textContent).toContain("暂不可用");
    (host.querySelector("[data-testid='retry-binding']") as HTMLButtonElement).click();
    await flushPromises();

    expect(bootstrapMock).toHaveBeenCalledTimes(2);
    expect(host.querySelector("[data-testid='service-ready']")).not.toBeNull();
  });

  it("releases every Wails event subscription when the app unmounts", async () => {
    bootstrapMock.mockResolvedValue({ name: "Codex Pulse", locale: "zh-CN", platform: "darwin" });
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
