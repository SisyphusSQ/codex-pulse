import { effectScope } from "vue";
import { beforeEach, describe, expect, it, vi } from "vitest";

import {
  ConfirmHomeSwitch,
  PlanHomeSwitch,
  RecoverHomeSwitch,
  UpdateSettings,
} from "@bindings/github.com/SisyphusSQ/codex-pulse/internal/app/service";

import { useSettingsPage } from "./useSettingsPage";

const harness = vi.hoisted(() => ({
  invalidates: vi.fn(),
  mutations: [] as Array<{
    mutationFn: (value?: unknown) => Promise<unknown>;
    onSuccess?: () => unknown;
    onSettled?: () => unknown;
    reset: ReturnType<typeof vi.fn>;
  }>,
}));
vi.mock("@tanstack/vue-query", () => ({
  useMutation: (options: { mutationFn: (value?: unknown) => Promise<unknown>; onSuccess?: () => unknown; onSettled?: () => unknown }) => {
    const reset = vi.fn();
    harness.mutations.push({ ...options, reset });
    return { mutate: vi.fn(), reset, isPending: { value: false }, isError: { value: false }, data: { value: undefined } };
  },
  useQuery: (options: unknown) => ({ options }),
  useQueryClient: () => ({ invalidateQueries: harness.invalidates }),
}));
vi.mock("@/queries/business", () => ({
  businessQueryRoots: {
    usage: ["usage"], sessions: ["sessions"], projects: ["projects"], quota: ["quota"],
    sources: ["sources"], jobs: ["jobs"], health: ["health"], settings: ["settings"],
  },
  settingsQueryOptions: () => ({}),
}));
vi.mock("@bindings/github.com/SisyphusSQ/codex-pulse/internal/app/service", () => ({
  ConfirmHomeSwitch: vi.fn(), PlanHomeSwitch: vi.fn(), RecoverHomeSwitch: vi.fn(), UpdateSettings: vi.fn(),
}));

function pending(cancel: ReturnType<typeof vi.fn>) {
  return Object.assign(new Promise(() => undefined), { cancel });
}

describe("useSettingsPage", () => {
  beforeEach(() => {
    harness.invalidates.mockReset(); harness.mutations = [];
    for (const mock of [UpdateSettings, PlanHomeSwitch, ConfirmHomeSwitch, RecoverHomeSwitch]) vi.mocked(mock).mockReset();
  });

  it("keeps four explicit mutations and invalidates settings consumers after durable commands", async () => {
    vi.mocked(UpdateSettings).mockResolvedValue({} as never);
    vi.mocked(PlanHomeSwitch).mockResolvedValue({} as never);
    vi.mocked(ConfirmHomeSwitch).mockResolvedValue({} as never);
    vi.mocked(RecoverHomeSwitch).mockResolvedValue({} as never);
    useSettingsPage();
    const update = { expectedRevision: "1" };
    const plan = { targetPath: "/tmp/home", strategy: "clear_and_rebuild" };
    await harness.mutations[0]?.mutationFn(update);
    await harness.mutations[1]?.mutationFn(plan);
    await harness.mutations[2]?.mutationFn();
    await harness.mutations[3]?.mutationFn();
    expect(UpdateSettings).toHaveBeenCalledWith(update);
    expect(PlanHomeSwitch).toHaveBeenCalledWith(plan);
    expect(ConfirmHomeSwitch).toHaveBeenCalledOnce();
    expect(RecoverHomeSwitch).toHaveBeenCalledOnce();
    await harness.mutations[0]?.onSettled?.();
    expect(harness.invalidates).toHaveBeenCalledTimes(4);
  });

  it("consumes Home impact state and invalidates every index/settings root after confirm or recovery", async () => {
    useSettingsPage();
    await harness.mutations[2]?.onSuccess?.();
    await harness.mutations[2]?.onSettled?.();
    expect(harness.mutations[1]?.reset).toHaveBeenCalledOnce();
    expect(harness.invalidates).toHaveBeenCalledTimes(8);
    harness.invalidates.mockClear();
    await harness.mutations[3]?.onSuccess?.();
    await harness.mutations[3]?.onSettled?.();
    expect(harness.mutations[1]?.reset).toHaveBeenCalledTimes(2);
    expect(harness.invalidates).toHaveBeenCalledTimes(8);
  });

  it("cancels every overlapping settings command when the page is disposed", async () => {
    const cancels = [vi.fn(), vi.fn(), vi.fn(), vi.fn()];
    vi.mocked(UpdateSettings).mockReturnValue(pending(cancels[0]) as never);
    vi.mocked(PlanHomeSwitch).mockReturnValue(pending(cancels[1]) as never);
    vi.mocked(ConfirmHomeSwitch).mockReturnValue(pending(cancels[2]) as never);
    vi.mocked(RecoverHomeSwitch).mockReturnValue(pending(cancels[3]) as never);
    const scope = effectScope();
    scope.run(() => useSettingsPage());
    void harness.mutations[0]?.mutationFn({});
    void harness.mutations[1]?.mutationFn({});
    void harness.mutations[2]?.mutationFn();
    void harness.mutations[3]?.mutationFn();
    await Promise.resolve();
    scope.stop();
    for (const cancel of cancels) expect(cancel).toHaveBeenCalledOnce();
  });
});
