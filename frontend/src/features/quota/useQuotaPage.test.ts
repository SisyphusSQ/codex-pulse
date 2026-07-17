import { beforeEach, describe, expect, it, vi } from "vitest";
import { effectScope } from "vue";

import { RefreshSource } from "@bindings/github.com/SisyphusSQ/codex-pulse/internal/codex/quota/models";
import { RequestQuotaRefresh } from "@bindings/github.com/SisyphusSQ/codex-pulse/internal/app/service";

import { useQuotaPage } from "./useQuotaPage";

const queryHarness = vi.hoisted(() => ({
  invalidateQueries: vi.fn(),
  mutate: vi.fn(),
  mutationOptions: null as null | {
    mutationFn: () => Promise<unknown>;
    onSettled: () => Promise<unknown> | unknown;
  },
}));

vi.mock("@tanstack/vue-query", () => ({
  useMutation: (options: typeof queryHarness.mutationOptions) => {
    queryHarness.mutationOptions = options;
    return {
      error: { value: null },
      isError: { value: false },
      isPending: { value: false },
      mutate: queryHarness.mutate,
    };
  },
  useQuery: (options: unknown) => ({ options }),
  useQueryClient: () => ({ invalidateQueries: queryHarness.invalidateQueries }),
}));

vi.mock("@/queries/business", () => ({
  businessQueryRoots: { quota: ["business", "quota"] },
  quotaCurrentQueryOptions: () => ({ queryKey: ["business", "quota", "current"] }),
}));

vi.mock("@bindings/github.com/SisyphusSQ/codex-pulse/internal/app/service", () => ({
  RequestQuotaRefresh: vi.fn(),
}));

describe("useQuotaPage", () => {
  beforeEach(() => {
    queryHarness.invalidateQueries.mockReset();
    queryHarness.mutate.mockReset();
    queryHarness.mutationOptions = null;
    vi.mocked(RequestQuotaRefresh).mockReset().mockResolvedValue({} as never);
  });

  it("requests both finite sources and invalidates authoritative quota data after settling", async () => {
    const result = useQuotaPage();

    result.refreshAll();
    expect(queryHarness.mutate).toHaveBeenCalledOnce();
    await queryHarness.mutationOptions?.mutationFn();
    expect(RequestQuotaRefresh).toHaveBeenNthCalledWith(1, RefreshSource.RefreshSourceQuota);
    expect(RequestQuotaRefresh).toHaveBeenNthCalledWith(2, RefreshSource.RefreshSourceResetCredits);

    await queryHarness.mutationOptions?.onSettled();
    expect(queryHarness.invalidateQueries).toHaveBeenCalledWith({
      queryKey: ["business", "quota"],
    });
  });

  it("attempts both sources but rejects the mutation when either command fails", async () => {
    const failure = new Error("content-free binding failure");
    vi.mocked(RequestQuotaRefresh)
      .mockRejectedValueOnce(failure)
      .mockResolvedValueOnce({} as never);
    useQuotaPage();

    await expect(queryHarness.mutationOptions?.mutationFn()).rejects.toBe(failure);
    expect(RequestQuotaRefresh).toHaveBeenCalledTimes(2);
  });

  it("cancels every in-flight Wails command across overlapping refreshes on dispose", async () => {
    const cancels = Array.from({ length: 4 }, () => vi.fn());
    const pendingCalls = cancels.map((cancel) => (
      Object.assign(new Promise(() => undefined), { cancel })
    ));
    vi.mocked(RequestQuotaRefresh)
      .mockReturnValueOnce(pendingCalls[0] as never)
      .mockReturnValueOnce(pendingCalls[1] as never)
      .mockReturnValueOnce(pendingCalls[2] as never)
      .mockReturnValueOnce(pendingCalls[3] as never);
    const scope = effectScope();
    scope.run(() => useQuotaPage());

    void queryHarness.mutationOptions?.mutationFn();
    void queryHarness.mutationOptions?.mutationFn();
    await Promise.resolve();
    scope.stop();

    for (const cancel of cancels) expect(cancel).toHaveBeenCalledOnce();
  });
});
