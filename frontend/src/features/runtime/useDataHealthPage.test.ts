import { effectScope } from "vue";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { RuntimeAction } from "@bindings/github.com/SisyphusSQ/codex-pulse/internal/app/models";
import { AnalyzeSessionIndexRepair, RunRuntimeAction } from "@bindings/github.com/SisyphusSQ/codex-pulse/internal/app/service";

import { dataHealthActionFor, useDataHealthPage } from "./useDataHealthPage";

const harness = vi.hoisted(() => ({
  invalidates: vi.fn(),
  mutations: [] as Array<{ mutationFn: () => Promise<unknown>; onSettled: () => unknown }>,
  queries: [] as unknown[],
}));

vi.mock("@tanstack/vue-query", () => ({
  useMutation: (options: { mutationFn: () => Promise<unknown>; onSettled: () => unknown }) => {
    harness.mutations.push(options);
    return { mutate: vi.fn(), mutateAsync: vi.fn(), isPending: { value: false }, isError: { value: false }, data: { value: undefined } };
  },
  useQuery: (options: unknown) => {
    harness.queries.push(options);
    return { options, refetch: vi.fn() };
  },
  useQueryClient: () => ({ invalidateQueries: harness.invalidates }),
}));
vi.mock("@/queries/business", () => ({
  businessQueryRoots: { sources: ["sources"], jobs: ["jobs"], health: ["health"] },
  dataHealthQueryOptions: () => ({ name: "data-health" }),
  healthProjectionQueryOptions: () => ({ name: "projection" }),
}));
vi.mock("@bindings/github.com/SisyphusSQ/codex-pulse/internal/app/service", () => ({
  AnalyzeSessionIndexRepair: vi.fn(), RunRuntimeAction: vi.fn(),
}));

function pending(cancel: ReturnType<typeof vi.fn>) {
  return Object.assign(new Promise(() => undefined), { cancel });
}

describe("useDataHealthPage", () => {
  beforeEach(() => {
    harness.invalidates.mockReset();
    harness.mutations = [];
    harness.queries = [];
    vi.mocked(RunRuntimeAction).mockReset();
    vi.mocked(AnalyzeSessionIndexRepair).mockReset();
  });

  it("maps only finite recovery kinds to registered safe actions", () => {
    expect(dataHealthActionFor("none")).toBeNull();
    expect(dataHealthActionFor("retry")).toBe("refresh");
    expect(dataHealthActionFor("check_source")).toBe("reconcile");
    expect(dataHealthActionFor("repair_store")).toBe("repair_dry_run");
    expect(dataHealthActionFor("grant_permission")).toBe("open_settings");
    expect(dataHealthActionFor("free_space")).toBe("open_settings");
    expect(dataHealthActionFor("choose_home")).toBe("open_settings");
    expect(dataHealthActionFor("runtime.shell.anything")).toBeNull();
  });

  it("queries the bounded DataHealth snapshot plus projection and runs only reconcile plus Analyze dry-run", async () => {
    vi.mocked(RunRuntimeAction).mockResolvedValue({} as never);
    vi.mocked(AnalyzeSessionIndexRepair).mockResolvedValue({} as never);
    useDataHealthPage();

    expect(harness.queries).toEqual([{ name: "data-health" }, { name: "projection" }]);
    await harness.mutations[0]?.mutationFn();
    await harness.mutations[1]?.mutationFn();
    expect(RunRuntimeAction).toHaveBeenCalledWith(RuntimeAction.RuntimeActionReconcile);
    expect(AnalyzeSessionIndexRepair).toHaveBeenCalledOnce();
    await harness.mutations[0]?.onSettled();
    expect(harness.invalidates).toHaveBeenCalledTimes(3);
  });

  it("cancels in-flight reconcile and Analyze calls when the page is disposed", async () => {
    const cancels = [vi.fn(), vi.fn()];
    vi.mocked(RunRuntimeAction).mockReturnValue(pending(cancels[0]) as never);
    vi.mocked(AnalyzeSessionIndexRepair).mockReturnValue(pending(cancels[1]) as never);
    const scope = effectScope();
    scope.run(() => useDataHealthPage());
    void harness.mutations[0]?.mutationFn();
    void harness.mutations[1]?.mutationFn();
    await Promise.resolve();
    scope.stop();
    for (const cancel of cancels) expect(cancel).toHaveBeenCalledOnce();
  });
});
