import type { CancellablePromiseLike } from "@wailsio/runtime";
import { useMutation, useQuery, useQueryClient } from "@tanstack/vue-query";
import { getCurrentScope, onScopeDispose } from "vue";

import {
  AnalyzeSessionIndexRepair,
  RunRuntimeAction,
} from "@bindings/github.com/SisyphusSQ/codex-pulse/internal/app/service";
import type { RuntimeAction } from "@bindings/github.com/SisyphusSQ/codex-pulse/internal/app/models";
import {
  businessQueryRoots,
  healthListQueryOptions,
  jobListQueryOptions,
  sourceListQueryOptions,
} from "@/queries/business";

import { createRuntimeRequests } from "./requests";

export function useLocalStatusPage() {
  const queryClient = useQueryClient();
  const requests = createRuntimeRequests();
  const sources = useQuery(sourceListQueryOptions(requests.sources));
  const jobs = useQuery(jobListQueryOptions(requests.jobs));
  const health = useQuery(healthListQueryOptions(requests.health));
  const activeCalls = new Set<CancellablePromiseLike<unknown>>();

  async function track<T>(call: CancellablePromiseLike<T>) {
    activeCalls.add(call);
    try {
      return await call;
    } finally {
      activeCalls.delete(call);
    }
  }

  const action = useMutation({
    mutationFn: (value: RuntimeAction) => track(RunRuntimeAction(value)),
    onSettled: async () => {
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: businessQueryRoots.sources }),
        queryClient.invalidateQueries({ queryKey: businessQueryRoots.jobs }),
        queryClient.invalidateQueries({ queryKey: businessQueryRoots.health }),
      ]);
    },
  });
  const repair = useMutation({
    mutationFn: () => track(AnalyzeSessionIndexRepair()),
    onSettled: () => queryClient.invalidateQueries({ queryKey: businessQueryRoots.health }),
  });

  if (getCurrentScope() !== undefined) {
    onScopeDispose(() => {
      const calls = Array.from(activeCalls);
      activeCalls.clear();
      for (const call of calls) void call.cancel();
    });
  }

  return {
    action,
    analyzeRepair: () => repair.mutate(),
    health,
    jobs,
    repair,
    runAction: (value: RuntimeAction) => action.mutate(value),
    sources,
  };
}
