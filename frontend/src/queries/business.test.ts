import { QueryClient, QueryObserver } from "@tanstack/vue-query";
import { describe, expect, it, vi } from "vitest";

import {
  DataHealth,
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
  UpdateState,
} from "@bindings/github.com/SisyphusSQ/codex-pulse/internal/app/service";
import type { Request } from "@bindings/github.com/SisyphusSQ/codex-pulse/internal/query/models";
import type {
  HealthDetailRequest,
  JobDetailRequest,
  SourceDetailRequest,
} from "@bindings/github.com/SisyphusSQ/codex-pulse/internal/query/runtimeinfo/models";
import {
  TrendGranularity,
  type ProjectDetailRequest,
  type SessionDetailRequest,
  type UsageCostRequest,
  type UsageCostResponse,
} from "@bindings/github.com/SisyphusSQ/codex-pulse/internal/query/usagecost/models";

import {
  BUSINESS_QUERY_STALE_MS,
  businessQueryRoots,
  dataHealthQueryOptions,
  healthDetailQueryOptions,
  healthListQueryOptions,
  healthProjectionQueryOptions,
  jobDetailQueryOptions,
  jobListQueryOptions,
  projectDetailQueryOptions,
  projectListQueryOptions,
  quotaCurrentQueryOptions,
  sessionDetailQueryOptions,
  sessionListQueryOptions,
  settingsQueryOptions,
  sourceDetailQueryOptions,
  sourceListQueryOptions,
  usageCostQueryOptions,
  updateStateQueryOptions,
} from "./business";

const bindingHarness = vi.hoisted(() => {
  const cancelOn = vi.fn();
  const result = () => {
    const promise = Promise.resolve(undefined);
    return Object.assign(promise, {
      cancelOn(signal: AbortSignal) {
        cancelOn(signal);
        return promise;
      },
    });
  };
  return { cancelOn, result };
});

vi.mock("@bindings/github.com/SisyphusSQ/codex-pulse/internal/app/service", () => ({
  DataHealth: vi.fn(() => bindingHarness.result()),
  Health: vi.fn(() => bindingHarness.result()),
  HealthProjection: vi.fn(() => bindingHarness.result()),
  Job: vi.fn(() => bindingHarness.result()),
  ListHealth: vi.fn(() => bindingHarness.result()),
  ListJobs: vi.fn(() => bindingHarness.result()),
  ListProjects: vi.fn(() => bindingHarness.result()),
  ListSessions: vi.fn(() => bindingHarness.result()),
  ListSources: vi.fn(() => bindingHarness.result()),
  ProjectDetail: vi.fn(() => bindingHarness.result()),
  QuotaCurrent: vi.fn(() => bindingHarness.result()),
  SessionDetail: vi.fn(() => bindingHarness.result()),
  Settings: vi.fn(() => bindingHarness.result()),
  Source: vi.fn(() => bindingHarness.result()),
  UsageCost: vi.fn(() => bindingHarness.result()),
  UpdateState: vi.fn(() => bindingHarness.result()),
}));

const pageRequest: Request = {
  page: { cursor: null, limit: 25 },
  sort: null,
  filters: null,
  timeRange: null,
};
const dateRange = {
  startDate: "2026-07-01",
  endDateExclusive: "2026-07-17",
  timeZone: "Asia/Shanghai",
};

async function runQuery(options: { queryFn?: unknown }) {
  await (options.queryFn as (context: { signal: AbortSignal }) => Promise<unknown>)({
    signal: new AbortController().signal,
  });
}

describe("business Vue Query contract", () => {
  it("uses the complete request in stable keys and delegates all 16 bindings", async () => {
    bindingHarness.cancelOn.mockClear();
    const usageRequest: UsageCostRequest = {
      range: dateRange,
      granularity: TrendGranularity.TrendDay,
    };
    const sessionRequest: SessionDetailRequest = {
      sessionId: "session-1",
      reportingTimezone: "Asia/Shanghai",
      turnPage: { cursor: null, limit: 20 },
    };
    const projectRequest: ProjectDetailRequest = {
      dimensionKey: "project-1",
      range: dateRange,
      sessionPage: { cursor: null, limit: 20 },
      modelPage: { cursor: null, limit: 20 },
    };
    const sourceRequest: SourceDetailRequest = { sourceKey: "source-1" };
    const jobRequest: JobDetailRequest = { jobId: "job-1" };
    const healthRequest: HealthDetailRequest = { eventId: "health-1" };
    const quotaClock = vi.fn(() => 1_784_100_000_000);

    const cases = [
      [usageCostQueryOptions(usageRequest), [...businessQueryRoots.usage, "cost", usageRequest], UsageCost, usageRequest],
      [sessionListQueryOptions(pageRequest), [...businessQueryRoots.sessions, "list", pageRequest], ListSessions, pageRequest],
      [sessionDetailQueryOptions(sessionRequest), [...businessQueryRoots.sessions, "detail", sessionRequest], SessionDetail, sessionRequest],
      [projectListQueryOptions(pageRequest), [...businessQueryRoots.projects, "list", pageRequest], ListProjects, pageRequest],
      [projectDetailQueryOptions(projectRequest), [...businessQueryRoots.projects, "detail", projectRequest], ProjectDetail, projectRequest],
      [quotaCurrentQueryOptions(quotaClock), [...businessQueryRoots.quota, "current"], QuotaCurrent, 1_784_100_000_000],
      [sourceListQueryOptions(pageRequest), [...businessQueryRoots.sources, "list", pageRequest], ListSources, pageRequest],
      [sourceDetailQueryOptions(sourceRequest), [...businessQueryRoots.sources, "detail", sourceRequest], Source, sourceRequest],
      [jobListQueryOptions(pageRequest), [...businessQueryRoots.jobs, "list", pageRequest], ListJobs, pageRequest],
      [jobDetailQueryOptions(jobRequest), [...businessQueryRoots.jobs, "detail", jobRequest], Job, jobRequest],
      [healthListQueryOptions(pageRequest), [...businessQueryRoots.health, "list", pageRequest], ListHealth, pageRequest],
      [healthDetailQueryOptions(healthRequest), [...businessQueryRoots.health, "detail", healthRequest], Health, healthRequest],
      [healthProjectionQueryOptions(), [...businessQueryRoots.health, "projection"], HealthProjection, undefined],
      [dataHealthQueryOptions(quotaClock), [...businessQueryRoots.health, "data-health"], DataHealth, 1_784_100_000_000],
      [settingsQueryOptions(), [...businessQueryRoots.settings, "current"], Settings, undefined],
    ] as const;

    for (const [options, expectedKey, binding, argument] of cases) {
      expect(options.queryKey).toEqual(expectedKey);
      expect(options.refetchInterval).toBe(options.staleTime);
      expect(options.refetchIntervalInBackground).toBe(false);
      await runQuery(options);
      if (argument === undefined) {
        expect(binding).toHaveBeenCalledWith();
      } else {
        expect(binding).toHaveBeenCalledWith(argument);
      }
    }
	const updateOptions = updateStateQueryOptions();
	expect(updateOptions.queryKey).toEqual([...businessQueryRoots.updates, "state"]);
	expect(updateOptions.refetchIntervalInBackground).toBe(false);
	expect(typeof updateOptions.refetchInterval).toBe("function");
	await runQuery(updateOptions);
	expect(UpdateState).toHaveBeenCalledWith();
    expect(quotaClock).toHaveBeenCalledTimes(2);
    expect(bindingHarness.cancelOn).toHaveBeenCalledTimes(16);
  });

  it("uses bounded domain stale times without changing bootstrap policy", () => {
    expect(usageCostQueryOptions({
      range: dateRange,
      granularity: TrendGranularity.TrendDay,
    }).staleTime).toBe(BUSINESS_QUERY_STALE_MS.index);
    expect(quotaCurrentQueryOptions(() => 1).staleTime).toBe(BUSINESS_QUERY_STALE_MS.runtime);
    expect(healthListQueryOptions(pageRequest).staleTime).toBe(BUSINESS_QUERY_STALE_MS.runtime);
    expect(settingsQueryOptions().staleTime).toBe(BUSINESS_QUERY_STALE_MS.settings);
	expect(updateStateQueryOptions().staleTime).toBe(BUSINESS_QUERY_STALE_MS.updates);
    expect(usageCostQueryOptions({
      range: dateRange,
      granularity: TrendGranularity.TrendDay,
    })).toMatchObject({
      refetchInterval: BUSINESS_QUERY_STALE_MS.index,
      refetchIntervalInBackground: false,
    });
    expect(quotaCurrentQueryOptions(() => 1)).toMatchObject({
      refetchInterval: BUSINESS_QUERY_STALE_MS.runtime,
      refetchIntervalInBackground: false,
    });
    expect(settingsQueryOptions()).toMatchObject({
      refetchInterval: BUSINESS_QUERY_STALE_MS.settings,
      refetchIntervalInBackground: false,
    });
  });

  it("periodically refetches only while observed and re-evaluates quota time", async () => {
    vi.useFakeTimers();
    try {
      const client = new QueryClient();
      const request: UsageCostRequest = {
        range: dateRange,
        granularity: TrendGranularity.TrendDay,
      };
      const fetch = vi.fn(async () => ({} as UsageCostResponse));
      const observer = new QueryObserver(client, {
        ...usageCostQueryOptions(request),
        queryFn: fetch,
      } as never);
      const unsubscribe = observer.subscribe(() => undefined);
      await vi.advanceTimersByTimeAsync(0);
      expect(fetch).toHaveBeenCalledTimes(1);

      await vi.advanceTimersByTimeAsync(BUSINESS_QUERY_STALE_MS.index);
      expect(fetch).toHaveBeenCalledTimes(2);

      unsubscribe();
      await vi.advanceTimersByTimeAsync(BUSINESS_QUERY_STALE_MS.index * 2);
      expect(fetch).toHaveBeenCalledTimes(2);

      const now = vi.fn()
        .mockReturnValueOnce(1_784_100_000_000)
        .mockReturnValueOnce(1_784_100_005_000);
      vi.mocked(QuotaCurrent).mockClear();
      const quota = quotaCurrentQueryOptions(now);
      await runQuery(quota);
      await runQuery(quota);
      expect(QuotaCurrent).toHaveBeenNthCalledWith(1, 1_784_100_000_000);
      expect(QuotaCurrent).toHaveBeenNthCalledWith(2, 1_784_100_005_000);
    } finally {
      vi.useRealTimers();
    }
  });

  it("cancels the Wails binding when its query observer unmounts", async () => {
    const client = new QueryClient();
    let boundSignal: AbortSignal | undefined;
    const pending = new Promise<UsageCostResponse>(() => undefined);
    const cancelOn = vi.fn((signal: AbortSignal) => {
      boundSignal = signal;
      return pending;
    });
    const cancellable = Object.assign(pending, { cancelOn });
    vi.mocked(UsageCost).mockReturnValueOnce(
      cancellable as unknown as ReturnType<typeof UsageCost>,
    );
    const observer = new QueryObserver(client, usageCostQueryOptions({
      range: dateRange,
      granularity: TrendGranularity.TrendDay,
    }) as never);
    const unsubscribe = observer.subscribe(() => undefined);
    await Promise.resolve();

    expect(cancelOn).toHaveBeenCalledOnce();
    expect(boundSignal?.aborted).toBe(false);
    unsubscribe();
    expect(boundSignal?.aborted).toBe(true);
  });
});
