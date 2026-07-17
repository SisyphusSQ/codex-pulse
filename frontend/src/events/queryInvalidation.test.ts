import { QueryClient } from "@tanstack/vue-query";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

// The real Wails runtime starts a DOM polling interval as a module side effect.
// Unit tests exercise the injected event source, so keep the desktop runtime
// outside jsdom and make accidental calls fail through an inert subscription.
vi.mock("@wailsio/runtime", () => ({
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

import {
  QueryInvalidationDomain,
  QueryInvalidationVersion,
  type QueryInvalidationEvent,
} from "@bindings/github.com/SisyphusSQ/codex-pulse/internal/app/models";
import { businessQueryRootList, businessQueryRoots } from "@/queries/business";

import {
  QUERY_INVALIDATION_CONTRACT_VERSION,
  installQueryInvalidationBridge,
  type QueryInvalidationEventSource,
} from "./queryInvalidation";

class FakeEventSource implements QueryInvalidationEventSource {
  invalidation?: (event: unknown) => void;
  wake?: () => void;
  ready?: () => void;
  foreground?: () => void;
  unsubscribeCalls = 0;

  onInvalidation(callback: (event: unknown) => void) {
    this.invalidation = callback;
    return () => {
      this.invalidation = undefined;
      this.unsubscribeCalls++;
    };
  }

  onWake(callback: () => void) {
    this.wake = callback;
    return () => {
      this.wake = undefined;
      this.unsubscribeCalls++;
    };
  }

  onRuntimeReady(callback: () => void) {
    this.ready = callback;
    return () => {
      this.ready = undefined;
      this.unsubscribeCalls++;
    };
  }

  onForeground(callback: () => void) {
    this.foreground = callback;
    return () => {
      this.foreground = undefined;
      this.unsubscribeCalls++;
    };
  }
}

function payload(domain: QueryInvalidationDomain): QueryInvalidationEvent {
  return { version: QUERY_INVALIDATION_CONTRACT_VERSION, domain };
}

describe("Wails query invalidation bridge", () => {
  beforeEach(() => vi.useFakeTimers());
  afterEach(() => vi.useRealTimers());

  it("coalesces duplicate event storms and refetches only active queries", async () => {
    const client = new QueryClient();
    const invalidate = vi.spyOn(client, "invalidateQueries").mockResolvedValue();
    const setData = vi.spyOn(client, "setQueryData");
    const source = new FakeEventSource();
    const cleanup = installQueryInvalidationBridge(client, source);

    for (let index = 0; index < 100; index++) {
      source.invalidation?.(payload(QueryInvalidationDomain.QueryInvalidationIndex));
      source.invalidation?.(payload(QueryInvalidationDomain.QueryInvalidationQuota));
    }
    await vi.runAllTimersAsync();

    const roots = invalidate.mock.calls.map(([filters]) =>
      (filters as { queryKey?: readonly unknown[] }).queryKey,
    );
    expect(roots).toEqual([
      businessQueryRoots.usage,
      businessQueryRoots.sessions,
      businessQueryRoots.projects,
      businessQueryRoots.sources,
      businessQueryRoots.jobs,
      businessQueryRoots.health,
      businessQueryRoots.quota,
    ]);
    for (const [filters] of invalidate.mock.calls) {
      expect(filters).toMatchObject({ exact: false, refetchType: "active" });
    }
    expect(setData).not.toHaveBeenCalled();
    cleanup();
  });

  it("fails closed to full business invalidation on unknown payload and recovery events", async () => {
    const client = new QueryClient();
    const invalidate = vi.spyOn(client, "invalidateQueries").mockResolvedValue();
    const source = new FakeEventSource();
    const cleanup = installQueryInvalidationBridge(client, source);

    source.invalidation?.({
      version: "future-version" as QueryInvalidationVersion,
      domain: QueryInvalidationDomain.QueryInvalidationHealth,
    });
    source.wake?.();
    source.ready?.();
    source.foreground?.();
    await vi.runAllTimersAsync();

    expect(invalidate.mock.calls.map(([filters]) =>
      (filters as { queryKey?: readonly unknown[] }).queryKey,
    )).toEqual(businessQueryRootList);
    expect(invalidate.mock.calls.some(([filters]) =>
      (filters as { queryKey?: readonly unknown[] }).queryKey?.includes("bootstrap"),
    )).toBe(false);
    cleanup();
  });

  it("fails closed without throwing for malformed runtime payloads", async () => {
    const client = new QueryClient();
    const invalidate = vi.spyOn(client, "invalidateQueries").mockResolvedValue();
    const source = new FakeEventSource();
    const cleanup = installQueryInvalidationBridge(client, source);

    for (const malformed of [
      null,
      undefined,
      "query-invalidation-v1",
      1,
      [],
      {},
      { version: QUERY_INVALIDATION_CONTRACT_VERSION },
      { version: QUERY_INVALIDATION_CONTRACT_VERSION, domain: "future-domain" },
    ]) {
      expect(() => source.invalidation?.(malformed)).not.toThrow();
    }
    await vi.runAllTimersAsync();

    expect(invalidate.mock.calls.map(([filters]) =>
      (filters as { queryKey?: readonly unknown[] }).queryKey,
    )).toEqual(businessQueryRootList);
    cleanup();
  });

  it("unsubscribes and cancels pending invalidation on cleanup", async () => {
    const client = new QueryClient();
    const invalidate = vi.spyOn(client, "invalidateQueries").mockResolvedValue();
    const source = new FakeEventSource();
    const cleanup = installQueryInvalidationBridge(client, source);

    source.invalidation?.(payload(QueryInvalidationDomain.QueryInvalidationSettings));
    cleanup();
    await vi.runAllTimersAsync();

    expect(source.unsubscribeCalls).toBe(4);
    expect(invalidate).not.toHaveBeenCalled();
    expect(source.invalidation).toBeUndefined();
    expect(source.wake).toBeUndefined();
    expect(source.ready).toBeUndefined();
    expect(source.foreground).toBeUndefined();
  });
});
