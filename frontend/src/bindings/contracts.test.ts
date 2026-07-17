import type { CancellablePromise } from "@wailsio/runtime";
import { describe, expect, expectTypeOf, it } from "vitest";

import * as Service from "@bindings/github.com/SisyphusSQ/codex-pulse/internal/app/service";
import type {
  BindingContractInfo,
  BootstrapInfo,
} from "@bindings/github.com/SisyphusSQ/codex-pulse/internal/app/models";
import {
  ErrorCode,
  type ErrorEnvelope,
  type PageInfo,
  type PageRequest,
  type Request,
} from "@bindings/github.com/SisyphusSQ/codex-pulse/internal/query/models";
import type {
  HealthDetailRequest,
  HealthDetailResponse,
  HealthListResponse,
  JobDetailRequest,
  JobDetailResponse,
  JobListResponse,
  QuotaCurrentResponse,
  SettingsResponse,
  SourceDetailRequest,
  SourceDetailResponse,
  SourceListResponse,
} from "@bindings/github.com/SisyphusSQ/codex-pulse/internal/query/runtimeinfo/models";
import {
  SessionTurnPricingStatus,
  SessionTurnState,
  type ProjectDailyPoint,
  type ProjectDetailRequest,
  type ProjectDetailResponse,
  type ProjectItem,
  type ProjectListResponse,
  type ProjectModelItem,
  type ProjectSessionItem,
  type SessionDetailRequest,
  type SessionDetailResponse,
  type SessionListResponse,
  type SessionTurnItem,
  type UsageCostRequest,
  type UsageCostResponse,
} from "@bindings/github.com/SisyphusSQ/codex-pulse/internal/query/usagecost/models";

describe("generated Wails binding contract", () => {
  it("exposes only the frozen method allowlist", () => {
    expect(Object.keys(Service).sort()).toEqual(
      [
        "Bootstrap",
        "Contracts",
        "Health",
        "Job",
        "ListHealth",
        "ListJobs",
        "ListProjects",
        "ListSessions",
        "ListSources",
        "ProjectDetail",
        "QuotaCurrent",
        "SessionDetail",
        "Settings",
        "Source",
        "UsageCost",
      ].sort(),
    );
  });

  it("keeps generated requests, responses and errors strongly typed", () => {
    expectTypeOf<Parameters<typeof Service.Bootstrap>>().toEqualTypeOf<[]>();
    expectTypeOf<ReturnType<typeof Service.Bootstrap>>().toEqualTypeOf<
      CancellablePromise<BootstrapInfo>
    >();
    expectTypeOf<Parameters<typeof Service.Contracts>>().toEqualTypeOf<[]>();
    expectTypeOf<ReturnType<typeof Service.Contracts>>().toEqualTypeOf<
      CancellablePromise<BindingContractInfo>
    >();
    expectTypeOf<Parameters<typeof Service.UsageCost>>().toEqualTypeOf<
      [UsageCostRequest]
    >();
    expectTypeOf<ReturnType<typeof Service.UsageCost>>().toEqualTypeOf<
      CancellablePromise<UsageCostResponse>
    >();
    expectTypeOf<Parameters<typeof Service.ListSessions>>().toEqualTypeOf<
      [Request]
    >();
    expectTypeOf<ReturnType<typeof Service.ListSessions>>().toEqualTypeOf<
      CancellablePromise<SessionListResponse>
    >();
    expectTypeOf<Parameters<typeof Service.SessionDetail>>().toEqualTypeOf<
      [SessionDetailRequest]
    >();
    expectTypeOf<ReturnType<typeof Service.SessionDetail>>().toEqualTypeOf<
      CancellablePromise<SessionDetailResponse>
    >();
    expectTypeOf<SessionDetailRequest["turnPage"]>().toEqualTypeOf<PageRequest>();
    expectTypeOf<SessionDetailResponse["turnPage"]>().toEqualTypeOf<PageInfo>();
    expectTypeOf<SessionDetailResponse["turns"]>().toEqualTypeOf<
      SessionTurnItem[] | null
    >();
    expectTypeOf<Parameters<typeof Service.ListProjects>>().toEqualTypeOf<
      [Request]
    >();
    expectTypeOf<ReturnType<typeof Service.ListProjects>>().toEqualTypeOf<
      CancellablePromise<ProjectListResponse>
    >();
    expectTypeOf<Parameters<typeof Service.ProjectDetail>>().toEqualTypeOf<
      [ProjectDetailRequest]
    >();
    expectTypeOf<ReturnType<typeof Service.ProjectDetail>>().toEqualTypeOf<
      CancellablePromise<ProjectDetailResponse>
    >();
    expectTypeOf<ProjectDetailRequest["sessionPage"]>().toEqualTypeOf<PageRequest>();
    expectTypeOf<ProjectDetailRequest["modelPage"]>().toEqualTypeOf<PageRequest>();
    expectTypeOf<ProjectDetailResponse["sessionPage"]>().toEqualTypeOf<PageInfo>();
    expectTypeOf<ProjectDetailResponse["sessions"]>().toEqualTypeOf<
      ProjectSessionItem[] | null
    >();
    expectTypeOf<ProjectDetailResponse["modelPage"]>().toEqualTypeOf<PageInfo>();
    expectTypeOf<ProjectDetailResponse["models"]>().toEqualTypeOf<
      ProjectModelItem[] | null
    >();
    expectTypeOf<ProjectItem["sessionCount"]>().toMatchTypeOf<{
      value: number | null;
      unit: string;
      unknownReason: string | null;
    }>();
    expectTypeOf<ProjectItem["trend"]>().toEqualTypeOf<ProjectDailyPoint[] | null>();
    expectTypeOf<Parameters<typeof Service.QuotaCurrent>>().toEqualTypeOf<
      [number]
    >();
    expectTypeOf<ReturnType<typeof Service.QuotaCurrent>>().toEqualTypeOf<
      CancellablePromise<QuotaCurrentResponse>
    >();
    expectTypeOf<Parameters<typeof Service.ListSources>>().toEqualTypeOf<
      [Request]
    >();
    expectTypeOf<ReturnType<typeof Service.ListSources>>().toEqualTypeOf<
      CancellablePromise<SourceListResponse>
    >();
    expectTypeOf<Parameters<typeof Service.Source>>().toEqualTypeOf<
      [SourceDetailRequest]
    >();
    expectTypeOf<ReturnType<typeof Service.Source>>().toEqualTypeOf<
      CancellablePromise<SourceDetailResponse>
    >();
    expectTypeOf<Parameters<typeof Service.ListJobs>>().toEqualTypeOf<
      [Request]
    >();
    expectTypeOf<ReturnType<typeof Service.ListJobs>>().toEqualTypeOf<
      CancellablePromise<JobListResponse>
    >();
    expectTypeOf<Parameters<typeof Service.Job>>().toEqualTypeOf<
      [JobDetailRequest]
    >();
    expectTypeOf<ReturnType<typeof Service.Job>>().toEqualTypeOf<
      CancellablePromise<JobDetailResponse>
    >();
    expectTypeOf<Parameters<typeof Service.ListHealth>>().toEqualTypeOf<
      [Request]
    >();
    expectTypeOf<ReturnType<typeof Service.ListHealth>>().toEqualTypeOf<
      CancellablePromise<HealthListResponse>
    >();
    expectTypeOf<Parameters<typeof Service.Health>>().toEqualTypeOf<
      [HealthDetailRequest]
    >();
    expectTypeOf<ReturnType<typeof Service.Health>>().toEqualTypeOf<
      CancellablePromise<HealthDetailResponse>
    >();
    expectTypeOf<Parameters<typeof Service.Settings>>().toEqualTypeOf<[]>();
    expectTypeOf<ReturnType<typeof Service.Settings>>().toEqualTypeOf<
      CancellablePromise<SettingsResponse>
    >();

    const envelope: ErrorEnvelope = {
      version: "query-v1",
      error: {
        code: ErrorCode.ErrorInternal,
        messageKey: "query.error.internal",
        field: null,
        retryable: false,
      },
    };
    expect(envelope.error.code).toBe("internal");
  });

  it("keeps the content-free Session turn state and pricing enums finite", () => {
    expect(Object.values(SessionTurnState).filter(Boolean).sort()).toEqual([
      "active",
      "complete",
    ]);
    expect(Object.values(SessionTurnPricingStatus).filter(Boolean).sort()).toEqual([
      "priced",
      "unknown",
      "unpriced",
    ]);
  });
});
