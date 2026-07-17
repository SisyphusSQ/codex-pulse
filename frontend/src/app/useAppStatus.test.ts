import { mount } from "@vue/test-utils";
import { defineComponent, h, nextTick, ref } from "vue";
import { afterEach, describe, expect, it, vi } from "vitest";

import type { HealthProjectionResponse } from "@bindings/github.com/SisyphusSQ/codex-pulse/internal/app/models";

import { selectAppStatus, type AppStatusInput, useAppStatus } from "./useAppStatus";

const harness = vi.hoisted(() => ({ health: {} as Record<string, unknown> }));
vi.mock("@/features/runtime/useHealthProjection", () => ({ useHealthProjection: () => harness.health }));

function projection(overrides: Partial<HealthProjectionResponse> = {}): HealthProjectionResponse {
  return {
    hasValue: true, stale: false, failure: "none", level: "healthy", primary: null,
    evaluatedAtMs: { value: 1_784_100_000_000, unit: "milliseconds", unknownReason: null },
    components: [], ...overrides,
  } as HealthProjectionResponse;
}

function input(overrides: Partial<AppStatusInput> = {}): AppStatusInput {
  return {
    online: true,
    health: { data: projection(), isError: false, isPending: false },
    ...overrides,
  };
}

describe("global application status", () => {
  afterEach(() => vi.restoreAllMocks());

  it("returns no banner for a healthy authoritative snapshot", () => {
    expect(selectAppStatus(input())).toBeNull();
  });

  it("uses the authoritative primary and one deterministic priority", () => {
    const primary = {
      component: "storage", level: "blocked", evidence: "known", reason: "store_disk_full",
      impact: "storage_at_risk", protection: "writes_stopped", recoveryAction: "free_space",
    };
    expect(selectAppStatus(input({
      online: false,
      health: { data: projection({ level: "blocked", primary } as never), isError: true, isPending: false },
    }))).toMatchObject({ kind: "blocked", primary, retryable: true });
  });

  it("separates error, unknown, stale, loading, offline, paused and busy", () => {
    expect(selectAppStatus(input({ health: { data: undefined, isError: true, isPending: false } }))).toMatchObject({ kind: "unavailable" });
    expect(selectAppStatus(input({ health: { data: projection({ hasValue: false, level: null }), isError: false, isPending: false } }))).toMatchObject({ kind: "unknown" });
    expect(selectAppStatus(input({ health: { data: projection({ stale: true, failure: "persist" } as never), isError: false, isPending: false } }))).toMatchObject({ kind: "stale" });
    expect(selectAppStatus(input({ health: { data: undefined, isError: false, isPending: true } }))).toMatchObject({ kind: "loading" });
    expect(selectAppStatus(input({ online: false }))).toMatchObject({ kind: "offline" });
    expect(selectAppStatus(input({ health: { data: projection({ level: "paused" } as never), isError: false, isPending: false } }))).toMatchObject({ kind: "paused" });
    expect(selectAppStatus(input({ health: { data: projection({ level: "busy" } as never), isError: false, isPending: false } }))).toMatchObject({ kind: "busy" });
  });

  it("subscribes to online state only while mounted", async () => {
    harness.health = {
      data: ref(projection()), isError: ref(false), isPending: ref(false), isStale: ref(false), refetch: vi.fn(),
    };
    Object.defineProperty(window.navigator, "onLine", { configurable: true, value: true });
    const add = vi.spyOn(window, "addEventListener");
    const remove = vi.spyOn(window, "removeEventListener");
    const component = defineComponent({
      setup() {
        const { status } = useAppStatus();
        return () => h("p", status.value?.kind ?? "healthy");
      },
    });
    const wrapper = mount(component);
    Object.defineProperty(window.navigator, "onLine", { configurable: true, value: false });
    window.dispatchEvent(new Event("offline"));
    await nextTick();
    expect(wrapper.text()).toBe("offline");
    wrapper.unmount();
    expect(add).toHaveBeenCalledWith("online", expect.any(Function));
    expect(remove).toHaveBeenCalledWith("offline", expect.any(Function));
  });
});
