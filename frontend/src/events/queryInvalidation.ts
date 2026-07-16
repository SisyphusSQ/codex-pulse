import { type QueryClient, type QueryKey } from "@tanstack/vue-query";
import { Events } from "@wailsio/runtime";

import {
  QueryInvalidationDomain,
  QueryInvalidationVersion,
  type QueryInvalidationEvent,
} from "@bindings/github.com/SisyphusSQ/codex-pulse/internal/app/models";
import { businessQueryRootList, businessQueryRoots } from "@/queries/business";

export const QUERY_INVALIDATION_EVENT_NAME = "codex-pulse:query-invalidated" satisfies keyof Events.CustomEvents;
export const QUERY_INVALIDATION_CONTRACT_VERSION =
  QueryInvalidationVersion.QueryInvalidationContractVersion;
export const QUERY_INVALIDATION_BATCH_MS = 50;

export interface QueryInvalidationEventSource {
  onInvalidation(callback: (event: unknown) => void): () => void;
  onWake(callback: () => void): () => void;
  onRuntimeReady(callback: () => void): () => void;
  onForeground(callback: () => void): () => void;
}

export function createRuntimeQueryInvalidationEventSource(): QueryInvalidationEventSource {
  return {
    onInvalidation(callback) {
      return Events.On(QUERY_INVALIDATION_EVENT_NAME, (event) => callback(event.data));
    },
    onWake(callback) {
      return Events.On(Events.Types.Common.SystemDidWake, callback);
    },
    onRuntimeReady(callback) {
      return Events.On(Events.Types.Common.WindowRuntimeReady, callback);
    },
    onForeground(callback) {
      return Events.On(Events.Types.Mac.ApplicationDidBecomeActive, callback);
    },
  };
}

function rootsForDomain(domain: QueryInvalidationDomain): readonly QueryKey[] | undefined {
  switch (domain) {
    case QueryInvalidationDomain.QueryInvalidationIndex:
      return [
        businessQueryRoots.usage,
        businessQueryRoots.sessions,
        businessQueryRoots.projects,
        businessQueryRoots.sources,
        businessQueryRoots.jobs,
        businessQueryRoots.health,
      ];
    case QueryInvalidationDomain.QueryInvalidationQuota:
      return [businessQueryRoots.quota, businessQueryRoots.sources, businessQueryRoots.health];
    case QueryInvalidationDomain.QueryInvalidationHealth:
      return [businessQueryRoots.health];
    case QueryInvalidationDomain.QueryInvalidationSettings:
      return [businessQueryRoots.settings, businessQueryRoots.quota, businessQueryRoots.sources];
    default:
      return undefined;
  }
}

function rootsForEvent(event: unknown): readonly QueryKey[] | undefined {
  if (typeof event !== "object" || event === null) {
    return undefined;
  }
  const candidate = event as Partial<QueryInvalidationEvent>;
  if (candidate.version !== QUERY_INVALIDATION_CONTRACT_VERSION) {
    return undefined;
  }
  return rootsForDomain(candidate.domain as QueryInvalidationDomain);
}

export function installQueryInvalidationBridge(
  queryClient: QueryClient,
  source: QueryInvalidationEventSource = createRuntimeQueryInvalidationEventSource(),
): () => void {
  let disposed = false;
  let timer: ReturnType<typeof setTimeout> | undefined;
  const pending = new Set<QueryKey>();

  const flush = () => {
    timer = undefined;
    if (disposed) {
      pending.clear();
      return;
    }
    const roots = [...pending];
    pending.clear();
    for (const queryKey of roots) {
      void queryClient.invalidateQueries({ queryKey, exact: false, refetchType: "active" });
    }
  };

  const enqueue = (roots: readonly QueryKey[]) => {
    if (disposed) {
      return;
    }
    for (const root of roots) {
      pending.add(root);
    }
    timer ??= setTimeout(flush, QUERY_INVALIDATION_BATCH_MS);
  };

  const enqueueAll = () => enqueue(businessQueryRootList);
  const unsubscribers = [
    source.onInvalidation((event) => {
      enqueue(rootsForEvent(event) ?? businessQueryRootList);
    }),
    source.onWake(enqueueAll),
    source.onRuntimeReady(enqueueAll),
    source.onForeground(enqueueAll),
  ];

  return () => {
    if (disposed) {
      return;
    }
    disposed = true;
    for (const unsubscribe of unsubscribers) {
      unsubscribe();
    }
    if (timer !== undefined) {
      clearTimeout(timer);
      timer = undefined;
    }
    pending.clear();
  };
}
