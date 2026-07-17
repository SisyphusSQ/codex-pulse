import { effectScope } from "vue";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { RuntimeAction } from "@bindings/github.com/SisyphusSQ/codex-pulse/internal/app/models";
import { AnalyzeSessionIndexRepair, RunRuntimeAction } from "@bindings/github.com/SisyphusSQ/codex-pulse/internal/app/service";

import { useLocalStatusPage } from "./useLocalStatusPage";

const harness = vi.hoisted(() => ({
  invalidates: vi.fn(), mutations: [] as Array<{ mutationFn: (value?: unknown) => Promise<unknown>; onSettled: () => unknown }>,
}));
vi.mock("@tanstack/vue-query", () => ({
  useMutation: (options: { mutationFn: (value?: unknown) => Promise<unknown>; onSettled: () => unknown }) => {
    harness.mutations.push(options);
    return { mutate: vi.fn(), isPending: { value: false }, isError: { value: false }, data: { value: undefined } };
  },
  useQuery: (options: unknown) => ({ options }),
  useQueryClient: () => ({ invalidateQueries: harness.invalidates }),
}));
vi.mock("@/queries/business", () => ({
  businessQueryRoots: { sources: ["sources"], jobs: ["jobs"], health: ["health"] },
  sourceListQueryOptions: () => ({}), jobListQueryOptions: () => ({}), healthListQueryOptions: () => ({}),
}));
vi.mock("@bindings/github.com/SisyphusSQ/codex-pulse/internal/app/service", () => ({
  AnalyzeSessionIndexRepair: vi.fn(), RunRuntimeAction: vi.fn(),
}));

function pending(cancel: ReturnType<typeof vi.fn>) {
  return Object.assign(new Promise(() => undefined), { cancel });
}

describe("useLocalStatusPage", () => {
  beforeEach(() => { harness.invalidates.mockReset(); harness.mutations = []; vi.mocked(RunRuntimeAction).mockReset(); vi.mocked(AnalyzeSessionIndexRepair).mockReset(); });

  it("delegates finite actions and Analyze-only repair, then invalidates authoritative queries", async () => {
    vi.mocked(RunRuntimeAction).mockResolvedValue({} as never);
    vi.mocked(AnalyzeSessionIndexRepair).mockResolvedValue({} as never);
    useLocalStatusPage();
    await harness.mutations[0]?.mutationFn(RuntimeAction.RuntimeActionPauseAll);
    await harness.mutations[1]?.mutationFn();
    expect(RunRuntimeAction).toHaveBeenCalledWith(RuntimeAction.RuntimeActionPauseAll);
    expect(AnalyzeSessionIndexRepair).toHaveBeenCalledOnce();
    await harness.mutations[0]?.onSettled();
    expect(harness.invalidates).toHaveBeenCalledTimes(3);
  });

  it("cancels overlapping lifecycle and repair calls when the page is disposed", async () => {
    const cancels = [vi.fn(), vi.fn()];
    vi.mocked(RunRuntimeAction).mockReturnValue(pending(cancels[0]) as never);
    vi.mocked(AnalyzeSessionIndexRepair).mockReturnValue(pending(cancels[1]) as never);
    const scope = effectScope();
    scope.run(() => useLocalStatusPage());
    void harness.mutations[0]?.mutationFn(RuntimeAction.RuntimeActionReconcile);
    void harness.mutations[1]?.mutationFn();
    await Promise.resolve();
    scope.stop();
    for (const cancel of cancels) expect(cancel).toHaveBeenCalledOnce();
  });
});
