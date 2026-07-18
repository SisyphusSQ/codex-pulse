import type { CancellablePromiseLike } from "@wailsio/runtime";
import { Events } from "@wailsio/runtime";
import { useMutation, useQuery, useQueryClient } from "@tanstack/vue-query";
import { getCurrentScope, onScopeDispose } from "vue";

import {
  CancelUpdate,
  CheckForUpdates,
  DownloadUpdate,
  SkipUpdate,
  SnoozeUpdate,
} from "@bindings/github.com/SisyphusSQ/codex-pulse/internal/app/service";
import { businessQueryRoots, updateStateQueryOptions } from "@/queries/business";

export const UPDATE_STATE_CHANGED_EVENT_NAME = "codex-pulse:update-state-changed" satisfies keyof Events.CustomEvents;

export function useUpdatePanel() {
  const queryClient = useQueryClient();
  const state = useQuery(updateStateQueryOptions());
  const activeCalls = new Set<CancellablePromiseLike<unknown>>();
  let invalidationTimer: ReturnType<typeof setTimeout> | undefined;
  let invalidationInFlight = false;
  let trailingInvalidation = false;
  let disposed = false;

  async function track<T>(call: CancellablePromiseLike<T>) {
    activeCalls.add(call);
    try {
      return await call;
    } finally {
      activeCalls.delete(call);
    }
  }

  async function invalidate() {
    await queryClient.invalidateQueries({ queryKey: businessQueryRoots.updates });
  }

  function scheduleEventInvalidation() {
    if (disposed) return;
    if (invalidationInFlight) {
      trailingInvalidation = true;
      return;
    }
    if (invalidationTimer !== undefined) return;
    invalidationTimer = setTimeout(() => {
      invalidationTimer = undefined;
      invalidationInFlight = true;
      void invalidate().finally(() => {
        invalidationInFlight = false;
        if (trailingInvalidation && !disposed) {
          trailingInvalidation = false;
          scheduleEventInvalidation();
        }
      });
    }, 100);
  }

  const check = useMutation({ mutationFn: () => track(CheckForUpdates()), onSettled: invalidate });
  const download = useMutation({ mutationFn: () => track(DownloadUpdate()), onSettled: invalidate });
  const cancel = useMutation({ mutationFn: () => track(CancelUpdate()), onSettled: invalidate });
  const skip = useMutation({ mutationFn: (version: string) => track(SkipUpdate(version)), onSettled: invalidate });
  const snooze = useMutation({ mutationFn: (seconds: number) => track(SnoozeUpdate(seconds)), onSettled: invalidate });

  let offEvent: (() => void) | undefined;
  if (getCurrentScope() !== undefined) {
    offEvent = Events.On(UPDATE_STATE_CHANGED_EVENT_NAME, scheduleEventInvalidation);
    onScopeDispose(() => {
      disposed = true;
      trailingInvalidation = false;
      offEvent?.();
      if (invalidationTimer !== undefined) clearTimeout(invalidationTimer);
      const calls = Array.from(activeCalls);
      activeCalls.clear();
      for (const call of calls) void call.cancel();
    });
  }

  return { cancel, check, download, skip, snooze, state };
}
