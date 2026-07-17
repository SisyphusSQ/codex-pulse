import type { CancellablePromiseLike } from "@wailsio/runtime";
import { useMutation, useQuery, useQueryClient } from "@tanstack/vue-query";
import { getCurrentScope, onScopeDispose } from "vue";

import type {
  HomeSwitchPlanRequest,
  SettingsUpdateRequest,
} from "@bindings/github.com/SisyphusSQ/codex-pulse/internal/app/models";
import {
  ConfirmHomeSwitch,
  PlanHomeSwitch,
  RecoverHomeSwitch,
  UpdateSettings,
} from "@bindings/github.com/SisyphusSQ/codex-pulse/internal/app/service";
import { businessQueryRoots, settingsQueryOptions } from "@/queries/business";

export function useSettingsPage() {
  const queryClient = useQueryClient();
  const settings = useQuery(settingsQueryOptions());
  const activeCalls = new Set<CancellablePromiseLike<unknown>>();

  async function track<T>(call: CancellablePromiseLike<T>) {
    activeCalls.add(call);
    try {
      return await call;
    } finally {
      activeCalls.delete(call);
    }
  }

  async function invalidateSettings() {
    await Promise.all([
      queryClient.invalidateQueries({ queryKey: businessQueryRoots.settings }),
      queryClient.invalidateQueries({ queryKey: businessQueryRoots.quota }),
      queryClient.invalidateQueries({ queryKey: businessQueryRoots.sources }),
      queryClient.invalidateQueries({ queryKey: businessQueryRoots.health }),
    ]);
  }

  async function invalidateHomeSwitch() {
    await Promise.all([
      queryClient.invalidateQueries({ queryKey: businessQueryRoots.usage }),
      queryClient.invalidateQueries({ queryKey: businessQueryRoots.sessions }),
      queryClient.invalidateQueries({ queryKey: businessQueryRoots.projects }),
      queryClient.invalidateQueries({ queryKey: businessQueryRoots.quota }),
      queryClient.invalidateQueries({ queryKey: businessQueryRoots.sources }),
      queryClient.invalidateQueries({ queryKey: businessQueryRoots.jobs }),
      queryClient.invalidateQueries({ queryKey: businessQueryRoots.health }),
      queryClient.invalidateQueries({ queryKey: businessQueryRoots.settings }),
    ]);
  }

  const update = useMutation({
    mutationFn: (request: SettingsUpdateRequest) => track(UpdateSettings(request)),
    onSettled: invalidateSettings,
  });
  const plan = useMutation({
    mutationFn: (request: HomeSwitchPlanRequest) => track(PlanHomeSwitch(request)),
  });
  const confirm = useMutation({
    mutationFn: () => track(ConfirmHomeSwitch()),
    onSuccess: () => plan.reset(),
    onSettled: invalidateHomeSwitch,
  });
  const recover = useMutation({
    mutationFn: () => track(RecoverHomeSwitch()),
    onSuccess: () => plan.reset(),
    onSettled: invalidateHomeSwitch,
  });

  if (getCurrentScope() !== undefined) {
    onScopeDispose(() => {
      const calls = Array.from(activeCalls);
      activeCalls.clear();
      for (const call of calls) void call.cancel();
    });
  }

  return { confirm, plan, recover, settings, update };
}
