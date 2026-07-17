import { useMutation, useQuery, useQueryClient } from "@tanstack/vue-query";
import { getCurrentScope, onScopeDispose } from "vue";

import { RequestQuotaRefresh } from "@bindings/github.com/SisyphusSQ/codex-pulse/internal/app/service";
import { RefreshSource } from "@bindings/github.com/SisyphusSQ/codex-pulse/internal/codex/quota/models";
import {
  businessQueryRoots,
  quotaCurrentQueryOptions,
} from "@/queries/business";

const refreshSources = Object.freeze([
  RefreshSource.RefreshSourceQuota,
  RefreshSource.RefreshSourceResetCredits,
]);

export function useQuotaPage() {
  const queryClient = useQueryClient();
  const quota = useQuery(quotaCurrentQueryOptions());
  const activeCalls = new Set<ReturnType<typeof RequestQuotaRefresh>>();
  const refresh = useMutation({
    mutationFn: async () => {
      const calls = refreshSources.map((source) => RequestQuotaRefresh(source));
      for (const call of calls) activeCalls.add(call);
      try {
        const results = await Promise.allSettled(calls);
        const failure = results.find(
          (result): result is PromiseRejectedResult => result.status === "rejected",
        );
        if (failure !== undefined) throw failure.reason;
        return results.map((result) => (result as PromiseFulfilledResult<unknown>).value);
      } finally {
        for (const call of calls) activeCalls.delete(call);
      }
    },
    onSettled: () => queryClient.invalidateQueries({ queryKey: businessQueryRoots.quota }),
  });

  if (getCurrentScope() !== undefined) {
    onScopeDispose(() => {
      const calls = Array.from(activeCalls);
      activeCalls.clear();
      for (const call of calls) void call.cancel();
    });
  }

  return {
    quota,
    refresh,
    refreshAll: () => refresh.mutate(),
  };
}
