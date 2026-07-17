import { QueryClient, VueQueryPlugin } from "@tanstack/vue-query";
import { flushPromises, mount } from "@vue/test-utils";
import type { CancellablePromise } from "@wailsio/runtime";
import { defineComponent, h } from "vue";
import { afterEach, describe, expect, it, vi } from "vitest";

import type { HealthProjectionResponse } from "@bindings/github.com/SisyphusSQ/codex-pulse/internal/app/models";
import { businessQueryRoots } from "@/queries/business";

import { useAppStatus } from "./useAppStatus";

const binding = vi.hoisted(() => ({ healthProjection: vi.fn() }));
vi.mock("@bindings/github.com/SisyphusSQ/codex-pulse/internal/app/service", async (importOriginal) => ({
  ...await importOriginal<typeof import("@bindings/github.com/SisyphusSQ/codex-pulse/internal/app/service")>(),
  HealthProjection: binding.healthProjection,
}));

function cancellable<T>(promise: Promise<T>): CancellablePromise<T> {
  return Object.assign(promise, { cancel: vi.fn(), cancelOn() { return this; } }) as unknown as CancellablePromise<T>;
}

function healthy(): HealthProjectionResponse {
  return {
    hasValue: true, stale: false, failure: "none", level: "healthy", primary: null,
    evaluatedAtMs: { value: 1_784_100_000_000, unit: "milliseconds", unknownReason: null },
    components: Array.from({ length: 7 }, (_, index) => ({
      component: ["local_index", "live_queue", "history_backfill", "online_quota", "storage", "runtime", "updater"][index],
      level: "healthy", evidence: "known", reason: "healthy", impact: "none", protection: "none", recoveryAction: "none",
    })),
  } as HealthProjectionResponse;
}

function harness(client: QueryClient, capture?: (value: ReturnType<typeof useAppStatus>) => void) {
  return mount(defineComponent({
    setup() {
      const status = useAppStatus();
      capture?.(status);
      return () => h("p", status.status.value?.kind ?? "healthy");
    },
  }), { global: { plugins: [[VueQueryPlugin, { queryClient: client }]] } });
}

describe("useAppStatus with real Vue Query semantics", () => {
  afterEach(() => binding.healthProjection.mockReset());

  it("does not turn cache invalidation into an authoritative stale banner", async () => {
    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    binding.healthProjection.mockReturnValueOnce(cancellable(Promise.resolve(healthy())));
    const wrapper = harness(client);
    await flushPromises();
    expect(wrapper.text()).toBe("healthy");

    binding.healthProjection.mockReturnValueOnce(cancellable(new Promise(() => undefined)));
    void client.invalidateQueries({ queryKey: businessQueryRoots.health, refetchType: "active" });
    await flushPromises();
    expect(client.getQueryState([...businessQueryRoots.health, "projection"])?.isInvalidated).toBe(true);
    expect(wrapper.text()).toBe("healthy");
    wrapper.unmount();
    await client.cancelQueries();
    client.clear();
  });

  it("rejects a failed explicit refetch so the Banner can report it inline", async () => {
    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    binding.healthProjection.mockReturnValue(cancellable(Promise.reject(new Error("synthetic"))));
    let appStatus: ReturnType<typeof useAppStatus> | undefined;
    const wrapper = harness(client, (value) => { appStatus = value; });
    await flushPromises();
    expect(wrapper.text()).toBe("unavailable");
    await expect(appStatus?.retry()).rejects.toThrow("synthetic");
    wrapper.unmount();
    client.clear();
  });
});
