import type { CancellablePromiseLike } from "@wailsio/runtime";
import { useMutation, useQuery, useQueryClient } from "@tanstack/vue-query";
import { getCurrentScope, onScopeDispose } from "vue";

import { RuntimeAction } from "@bindings/github.com/SisyphusSQ/codex-pulse/internal/app/models";
import { AnalyzeSessionIndexRepair, RunRuntimeAction } from "@bindings/github.com/SisyphusSQ/codex-pulse/internal/app/service";
import {
  businessQueryRoots,
  dataHealthQueryOptions,
  healthProjectionQueryOptions,
} from "@/queries/business";

export type DataHealthAction = "refresh" | "reconcile" | "repair_dry_run" | "open_settings";

export function dataHealthActionFor(recoveryKind: string): DataHealthAction | null {
  switch (recoveryKind) {
    case "retry": return "refresh";
    case "check_source": return "reconcile";
    case "repair_store": return "repair_dry_run";
    case "grant_permission":
    case "free_space":
    case "choose_home": return "open_settings";
    default: return null;
  }
}

export function useDataHealthPage() {
  const queryClient = useQueryClient();
  const metrics = useQuery(dataHealthQueryOptions());
  const projection = useQuery(healthProjectionQueryOptions());
  const activeCalls = new Set<CancellablePromiseLike<unknown>>();

  async function track<T>(call: CancellablePromiseLike<T>) {
    activeCalls.add(call);
    try {
      return await call;
    } finally {
      activeCalls.delete(call);
    }
  }

  async function invalidate() {
    await Promise.all([
      queryClient.invalidateQueries({ queryKey: businessQueryRoots.sources }),
      queryClient.invalidateQueries({ queryKey: businessQueryRoots.jobs }),
      queryClient.invalidateQueries({ queryKey: businessQueryRoots.health }),
    ]);
  }

  const reconcile = useMutation({
    mutationFn: () => track(RunRuntimeAction(RuntimeAction.RuntimeActionReconcile)),
    onSettled: invalidate,
  });
  const repair = useMutation({
    mutationFn: () => track(AnalyzeSessionIndexRepair()),
    onSettled: invalidate,
  });

  if (getCurrentScope() !== undefined) {
    onScopeDispose(() => {
      const calls = Array.from(activeCalls);
      activeCalls.clear();
      for (const call of calls) void call.cancel();
    });
  }

  return {
    metrics,
    projection,
    reconcile,
    repair,
    refresh: () => Promise.all([metrics.refetch(), projection.refetch()]),
  };
}
