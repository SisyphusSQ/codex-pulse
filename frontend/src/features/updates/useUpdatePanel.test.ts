import { effectScope } from "vue";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { CancelUpdate, CheckForUpdates, DownloadUpdate, InstallUpdate, SkipUpdate, SnoozeUpdate } from "@bindings/github.com/SisyphusSQ/codex-pulse/internal/app/service";
import { useUpdatePanel } from "./useUpdatePanel";

const harness = vi.hoisted(() => ({
  event: undefined as (() => void) | undefined,
  off: vi.fn(),
  invalidates: vi.fn(),
  mutations: [] as Array<{ mutationFn: (value?: never) => Promise<unknown>; onMutate?: () => unknown; onSettled?: () => unknown }>,
}));

vi.mock("@wailsio/runtime", () => ({
  Events: { On: vi.fn((_name: string, callback: () => void) => { harness.event = callback; return harness.off; }) },
}));
vi.mock("@tanstack/vue-query", () => ({
  useMutation: (options: { mutationFn: (value?: never) => Promise<unknown>; onMutate?: () => unknown; onSettled?: () => unknown }) => {
    harness.mutations.push(options);
    return { mutate: vi.fn(), isPending: { value: false }, isError: { value: false } };
  },
  useQuery: () => ({ data: { value: undefined }, isPending: { value: true }, isError: { value: false } }),
  useQueryClient: () => ({ invalidateQueries: harness.invalidates }),
}));
vi.mock("@/queries/business", () => ({
  businessQueryRoots: { updates: ["updates"] }, updateStateQueryOptions: () => ({}),
}));
vi.mock("@bindings/github.com/SisyphusSQ/codex-pulse/internal/app/service", () => ({
  CancelUpdate: vi.fn(), CheckForUpdates: vi.fn(), DownloadUpdate: vi.fn(), InstallUpdate: vi.fn(), SkipUpdate: vi.fn(), SnoozeUpdate: vi.fn(),
}));

function pending(cancel: ReturnType<typeof vi.fn>) {
  return Object.assign(new Promise(() => undefined), { cancel });
}

describe("useUpdatePanel", () => {
  beforeEach(() => {
    harness.event = undefined; harness.off.mockReset(); harness.invalidates.mockReset(); harness.mutations = [];
    for (const mock of [CancelUpdate, CheckForUpdates, DownloadUpdate, InstallUpdate, SkipUpdate, SnoozeUpdate]) vi.mocked(mock).mockReset();
  });

  it("invalidates update state for native events and every action", async () => {
    vi.useFakeTimers();
    try {
      for (const mock of [CancelUpdate, CheckForUpdates, DownloadUpdate, InstallUpdate, SkipUpdate, SnoozeUpdate]) vi.mocked(mock).mockResolvedValue({} as never);
      const scope = effectScope();
      scope.run(() => useUpdatePanel());
      for (let index = 0; index < 100; index++) harness.event?.();
      await vi.runAllTimersAsync();
      expect(harness.invalidates).toHaveBeenCalledWith({ queryKey: ["updates"] });
      for (const mutation of harness.mutations) await mutation.onSettled?.();
    expect(harness.invalidates).toHaveBeenCalledTimes(7);
      scope.stop();
      expect(harness.off).toHaveBeenCalledOnce();
    } finally {
      vi.useRealTimers();
    }
  });

  it("cancels every overlapping update action on disposal", async () => {
    const cancels = Array.from({ length: 6 }, () => vi.fn());
    const bindings = [CheckForUpdates, DownloadUpdate, InstallUpdate, CancelUpdate, SkipUpdate, SnoozeUpdate];
    bindings.forEach((binding, index) => vi.mocked(binding).mockReturnValue(pending(cancels[index]!) as never));
    const scope = effectScope();
    scope.run(() => useUpdatePanel());
    harness.mutations.forEach((mutation, index) => { void mutation.mutationFn((index === 4 ? "42" : index === 5 ? 3600 : undefined) as never); });
    await Promise.resolve();
    scope.stop();
    for (const cancel of cancels) expect(cancel).toHaveBeenCalledOnce();
  });

  it("polls shutdown state while the install mutation is pending", async () => {
    vi.useFakeTimers();
    const scope = effectScope();
    try {
      scope.run(() => useUpdatePanel());
      const install = harness.mutations[2]!;
      install.onMutate?.();
      await vi.advanceTimersByTimeAsync(850);
      expect(harness.invalidates.mock.calls.length).toBeGreaterThanOrEqual(3);
      await install.onSettled?.();
      const afterSettled = harness.invalidates.mock.calls.length;
      await vi.advanceTimersByTimeAsync(1000);
      expect(harness.invalidates).toHaveBeenCalledTimes(afterSettled);
    } finally {
      scope.stop();
      vi.useRealTimers();
    }
  });

  it("coalesces a sustained event stream behind one in-flight invalidation", async () => {
    vi.useFakeTimers();
    let resolveFirst!: () => void;
    harness.invalidates.mockImplementationOnce(() => new Promise<void>((resolve) => { resolveFirst = resolve; }));
    harness.invalidates.mockResolvedValue(undefined);
    const scope = effectScope();
    try {
      scope.run(() => useUpdatePanel());
      harness.event?.();
      await vi.advanceTimersByTimeAsync(100);
      expect(harness.invalidates).toHaveBeenCalledTimes(1);
      for (let index = 0; index < 20; index++) harness.event?.();
      await vi.advanceTimersByTimeAsync(500);
      expect(harness.invalidates).toHaveBeenCalledTimes(1);
      resolveFirst();
      await Promise.resolve();
      await vi.advanceTimersByTimeAsync(100);
      expect(harness.invalidates).toHaveBeenCalledTimes(2);
    } finally {
      scope.stop();
      vi.useRealTimers();
    }
  });
});
