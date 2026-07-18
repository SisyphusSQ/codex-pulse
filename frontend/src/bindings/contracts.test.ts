import type { CancellablePromise } from "@wailsio/runtime";
import { describe, expect, expectTypeOf, it } from "vitest";

import * as Service from "@bindings/github.com/SisyphusSQ/codex-pulse/internal/app/service";
import * as MigrationRecoveryService from "@bindings/github.com/SisyphusSQ/codex-pulse/internal/app/migrationrecoveryservice";
import * as StartupService from "@bindings/github.com/SisyphusSQ/codex-pulse/internal/app/startupservice";
import type {
  BindingContractInfo,
  BootstrapInfo,
  HomeSwitchPlanRequest,
  HomeSwitchPlanReceipt,
  HomeSwitchReceipt,
  QuotaRefreshReceipt,
  RepairDryRunReceipt,
  RuntimeActionReceipt,
  SettingsUpdateReceipt,
  SettingsUpdateRequest,
} from "@bindings/github.com/SisyphusSQ/codex-pulse/internal/app/models";
import {
  HomeSwitchResult,
  HomeSwitchStrategy,
  RuntimeAction,
  SettingsUpdateResult,
} from "@bindings/github.com/SisyphusSQ/codex-pulse/internal/app/models";
import { RefreshSource } from "@bindings/github.com/SisyphusSQ/codex-pulse/internal/codex/quota/models";
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
        "AnalyzeSessionIndexRepair",
        "CancelUpdate",
        "CheckForUpdates",
        "ConfirmHomeSwitch",
        "Contracts",
        "DataHealth",
        "DownloadUpdate",
        "InstallUpdate",
        "Health",
        "HealthProjection",
        "Job",
        "ListHealth",
        "ListJobs",
        "ListProjects",
        "ListSessions",
        "ListSources",
        "PlanHomeSwitch",
        "ProjectDetail",
        "QuotaCurrent",
        "RecoverHomeSwitch",
        "RequestQuotaRefresh",
        "RunRuntimeAction",
        "SessionDetail",
        "Settings",
        "SkipUpdate",
        "SnoozeUpdate",
        "Source",
        "UpdateSettings",
        "UpdateState",
        "UsageCost",
      ].sort(),
    );
  });

  it("keeps startup and recovery capabilities on exact isolated services", () => {
    expect(Object.keys(StartupService)).toEqual(["Bootstrap"]);
    expect(Object.keys(MigrationRecoveryService).sort()).toEqual(["Cancel", "Confirm", "Exit", "Prepare", "Retry", "State"]);
  });

  it("keeps generated requests, responses and errors strongly typed", () => {
    expectTypeOf<Parameters<typeof StartupService.Bootstrap>>().toEqualTypeOf<[]>();
    expectTypeOf<ReturnType<typeof StartupService.Bootstrap>>().toEqualTypeOf<
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
    expectTypeOf<Parameters<typeof Service.RequestQuotaRefresh>>().toEqualTypeOf<
      [RefreshSource]
    >();
    expectTypeOf<ReturnType<typeof Service.RequestQuotaRefresh>>().toEqualTypeOf<
      CancellablePromise<QuotaRefreshReceipt>
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
    expectTypeOf<Parameters<typeof Service.UpdateSettings>>().toEqualTypeOf<
      [SettingsUpdateRequest]
    >();
    expectTypeOf<ReturnType<typeof Service.UpdateSettings>>().toEqualTypeOf<
      CancellablePromise<SettingsUpdateReceipt>
    >();
    expectTypeOf<Parameters<typeof Service.PlanHomeSwitch>>().toEqualTypeOf<
      [HomeSwitchPlanRequest]
    >();
    expectTypeOf<ReturnType<typeof Service.PlanHomeSwitch>>().toEqualTypeOf<
      CancellablePromise<HomeSwitchPlanReceipt>
    >();
    expectTypeOf<ReturnType<typeof Service.ConfirmHomeSwitch>>().toEqualTypeOf<
      CancellablePromise<HomeSwitchReceipt>
    >();
    expectTypeOf<ReturnType<typeof Service.RecoverHomeSwitch>>().toEqualTypeOf<
      CancellablePromise<HomeSwitchReceipt>
    >();
    expectTypeOf<Parameters<typeof Service.RunRuntimeAction>>().toEqualTypeOf<
      [RuntimeAction]
    >();
    expectTypeOf<ReturnType<typeof Service.RunRuntimeAction>>().toEqualTypeOf<
      CancellablePromise<RuntimeActionReceipt>
    >();
    expectTypeOf<ReturnType<typeof Service.AnalyzeSessionIndexRepair>>().toEqualTypeOf<
      CancellablePromise<RepairDryRunReceipt>
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

  it("keeps manual Quota refresh sources finite", () => {
    expect(Object.values(RefreshSource).filter(Boolean).sort()).toEqual([
      "quota",
      "reset_credits",
    ]);
  });

  it("keeps settings, Home switch, lifecycle and repair commands finite", () => {
    expect(Object.values(SettingsUpdateResult).filter(Boolean).sort()).toEqual([
      "applied",
      "applied_reconcile_required",
    ]);
    expect(Object.values(HomeSwitchStrategy).filter(Boolean).sort()).toEqual([
      "clear_and_rebuild",
      "independent_database",
    ]);
    expect(Object.values(HomeSwitchResult).filter(Boolean).sort()).toEqual([
      "completed",
      "recovery_required",
      "rolled_back",
    ]);
    expect(Object.values(RuntimeAction).filter(Boolean).sort()).toEqual([
      "pause_all",
      "pause_backfill",
      "reconcile",
      "resume",
    ]);
  });
});
