import { queryOptions } from "@tanstack/vue-query";

import {
  Health,
  HealthProjection,
  Job,
  ListHealth,
  ListJobs,
  ListProjects,
  ListSessions,
  ListSources,
  ProjectDetail,
  QuotaCurrent,
  SessionDetail,
  Settings,
  Source,
  UsageCost,
} from "@bindings/github.com/SisyphusSQ/codex-pulse/internal/app/service";
import type { Request } from "@bindings/github.com/SisyphusSQ/codex-pulse/internal/query/models";
import type {
  HealthDetailRequest,
  JobDetailRequest,
  SourceDetailRequest,
} from "@bindings/github.com/SisyphusSQ/codex-pulse/internal/query/runtimeinfo/models";
import type {
  ProjectDetailRequest,
  SessionDetailRequest,
  UsageCostRequest,
} from "@bindings/github.com/SisyphusSQ/codex-pulse/internal/query/usagecost/models";

export const BUSINESS_QUERY_STALE_MS = Object.freeze({
  index: 15_000,
  runtime: 5_000,
  settings: 60_000,
});

const indexQueryTiming = Object.freeze({
  staleTime: BUSINESS_QUERY_STALE_MS.index,
  refetchInterval: BUSINESS_QUERY_STALE_MS.index,
  refetchIntervalInBackground: false,
});
const runtimeQueryTiming = Object.freeze({
  staleTime: BUSINESS_QUERY_STALE_MS.runtime,
  refetchInterval: BUSINESS_QUERY_STALE_MS.runtime,
  refetchIntervalInBackground: false,
});
const settingsQueryTiming = Object.freeze({
  staleTime: BUSINESS_QUERY_STALE_MS.settings,
  refetchInterval: BUSINESS_QUERY_STALE_MS.settings,
  refetchIntervalInBackground: false,
});

export const businessQueryRoots = Object.freeze({
  usage: ["business", "usage"] as const,
  sessions: ["business", "sessions"] as const,
  projects: ["business", "projects"] as const,
  quota: ["business", "quota"] as const,
  sources: ["business", "sources"] as const,
  jobs: ["business", "jobs"] as const,
  health: ["business", "health"] as const,
  settings: ["business", "settings"] as const,
});

export const businessQueryRootList = Object.freeze([
  businessQueryRoots.usage,
  businessQueryRoots.sessions,
  businessQueryRoots.projects,
  businessQueryRoots.quota,
  businessQueryRoots.sources,
  businessQueryRoots.jobs,
  businessQueryRoots.health,
  businessQueryRoots.settings,
]);

export function usageCostQueryOptions(request: UsageCostRequest) {
  return queryOptions({
    queryKey: [...businessQueryRoots.usage, "cost", request] as const,
    queryFn: ({ signal }) => UsageCost(request).cancelOn(signal),
    ...indexQueryTiming,
  });
}

export function sessionListQueryOptions(request: Request) {
  return queryOptions({
    queryKey: [...businessQueryRoots.sessions, "list", request] as const,
    queryFn: ({ signal }) => ListSessions(request).cancelOn(signal),
    ...indexQueryTiming,
  });
}

export function sessionDetailQueryOptions(request: SessionDetailRequest) {
  return queryOptions({
    queryKey: [...businessQueryRoots.sessions, "detail", request] as const,
    queryFn: ({ signal }) => SessionDetail(request).cancelOn(signal),
    ...indexQueryTiming,
  });
}

export function projectListQueryOptions(request: Request) {
  return queryOptions({
    queryKey: [...businessQueryRoots.projects, "list", request] as const,
    queryFn: ({ signal }) => ListProjects(request).cancelOn(signal),
    ...indexQueryTiming,
  });
}

export function projectDetailQueryOptions(request: ProjectDetailRequest) {
  return queryOptions({
    queryKey: [...businessQueryRoots.projects, "detail", request] as const,
    queryFn: ({ signal }) => ProjectDetail(request).cancelOn(signal),
    ...indexQueryTiming,
  });
}

export function quotaCurrentQueryOptions(now: () => number = Date.now) {
  return queryOptions({
    queryKey: [...businessQueryRoots.quota, "current"] as const,
    queryFn: ({ signal }) => QuotaCurrent(now()).cancelOn(signal),
    ...runtimeQueryTiming,
  });
}

export function sourceListQueryOptions(request: Request) {
  return queryOptions({
    queryKey: [...businessQueryRoots.sources, "list", request] as const,
    queryFn: ({ signal }) => ListSources(request).cancelOn(signal),
    ...runtimeQueryTiming,
  });
}

export function sourceDetailQueryOptions(request: SourceDetailRequest) {
  return queryOptions({
    queryKey: [...businessQueryRoots.sources, "detail", request] as const,
    queryFn: ({ signal }) => Source(request).cancelOn(signal),
    ...runtimeQueryTiming,
  });
}

export function jobListQueryOptions(request: Request) {
  return queryOptions({
    queryKey: [...businessQueryRoots.jobs, "list", request] as const,
    queryFn: ({ signal }) => ListJobs(request).cancelOn(signal),
    ...runtimeQueryTiming,
  });
}

export function jobDetailQueryOptions(request: JobDetailRequest) {
  return queryOptions({
    queryKey: [...businessQueryRoots.jobs, "detail", request] as const,
    queryFn: ({ signal }) => Job(request).cancelOn(signal),
    ...runtimeQueryTiming,
  });
}

export function healthListQueryOptions(request: Request) {
  return queryOptions({
    queryKey: [...businessQueryRoots.health, "list", request] as const,
    queryFn: ({ signal }) => ListHealth(request).cancelOn(signal),
    ...runtimeQueryTiming,
  });
}

export function healthProjectionQueryOptions() {
  return queryOptions({
    queryKey: [...businessQueryRoots.health, "projection"] as const,
    queryFn: ({ signal }) => HealthProjection().cancelOn(signal),
    ...runtimeQueryTiming,
  });
}

export function healthDetailQueryOptions(request: HealthDetailRequest) {
  return queryOptions({
    queryKey: [...businessQueryRoots.health, "detail", request] as const,
    queryFn: ({ signal }) => Health(request).cancelOn(signal),
    ...runtimeQueryTiming,
  });
}

export function settingsQueryOptions() {
  return queryOptions({
    queryKey: [...businessQueryRoots.settings, "current"] as const,
    queryFn: ({ signal }) => Settings().cancelOn(signal),
    ...settingsQueryTiming,
  });
}
