import { mount } from "@vue/test-utils";
import { NumericUnit, ResponseStatus } from "@bindings/github.com/SisyphusSQ/codex-pulse/internal/query/models";
import { CostReason } from "@bindings/github.com/SisyphusSQ/codex-pulse/internal/pricing/models";
import { CurrentRefreshState } from "@bindings/github.com/SisyphusSQ/codex-pulse/internal/codex/quota/models";
import type { QuotaCurrentResponse } from "@bindings/github.com/SisyphusSQ/codex-pulse/internal/query/runtimeinfo/models";
import {
  QuotaConflictState,
  QuotaCurrentFreshness,
  QuotaExplanationCode,
  QuotaSource,
  QuotaWindowKind,
  SourceFreshness,
} from "@bindings/github.com/SisyphusSQ/codex-pulse/internal/store/models";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { createAppI18n } from "@/i18n";

import OverviewView from "./OverviewView.vue";

const overviewHarness = vi.hoisted(() => ({ queries: {} as Record<string, unknown> }));

vi.mock("@/features/overview/useOverviewQueries", () => ({
  useOverviewQueries: () => overviewHarness.queries,
}));

const known = (value: number, unit = NumericUnit.NumericTokens) => ({
  value,
  unit,
  unknownReason: null,
});

const meta = (status = ResponseStatus.ResponseComplete) => ({
  issues: status === ResponseStatus.ResponseComplete ? null : [{
    code: "partial",
    messageKey: "query.partial",
    retryable: true,
  }],
  page: null,
  status,
  version: "query-v1",
});

const totals = () => ({
  cachedInputTokens: known(8_000),
  estimatedUsdMicros: known(1_000_000, NumericUnit.NumericMicroUSD),
  firstActivityAtMs: known(Date.parse("2026-07-16T01:00:00Z"), NumericUnit.NumericMilliseconds),
  inputTokens: known(10_000),
  lastActivityAtMs: known(Date.parse("2026-07-16T02:00:00Z"), NumericUnit.NumericMilliseconds),
  outputTokens: known(4_000),
  pricedTurnCount: known(2, NumericUnit.NumericCount),
  reasoningTokens: known(2_000),
  totalTokens: known(24_000),
  turnCount: known(2, NumericUnit.NumericCount),
  unpricedTurnCount: known(1, NumericUnit.NumericCount),
});

function refreshStatus() {
  return {
    activeTrigger: null,
    claimExpiresAtMs: null,
    claimStartedAtMs: null,
    lastManualAtMs: null,
    nextDueAtMs: null,
    reason: null,
    state: CurrentRefreshState.CurrentRefreshScheduled,
    unknownReason: null,
  };
}

function quotaWindow(
  windowKind: QuotaWindowKind,
  remainingPercent: number | null,
  selectedSource: QuotaSource,
  freshness: QuotaCurrentFreshness,
  resetRemainingMs: number,
) {
  return {
    conflict: QuotaConflictState.QuotaConflictNone,
    explanationCode: QuotaExplanationCode.QuotaExplanationTrusted,
    explanations: null,
    freshness,
    lastAttemptAtMs: Date.parse("2026-07-16T07:58:00Z"),
    lastSuccessAtMs: Date.parse("2026-07-16T08:00:00Z"),
    limitId: windowKind,
    remainingPercent,
    resetRemainingMs,
    resetsAtMs: Date.parse("2026-07-17T08:00:00Z"),
    selectedSource,
    unknownReason: null,
    usedPercent: remainingPercent === null ? null : 100 - remainingPercent,
    windowGeneration: 1,
    windowKind,
    windowMinutes: windowKind === QuotaWindowKind.QuotaWindowPrimary ? 300 : 10_080,
  };
}

function quotaResponse() {
  const evaluatedAtMs = Date.parse("2026-07-16T08:00:00Z");
  return {
    current: {
      accountScope: "default",
      evaluatedAtMs,
      nextReset: {
        atMs: Date.parse("2026-07-16T10:00:00Z"),
        remainingMs: 7_200_000,
        trustedWindowCount: 2,
        unknownReason: null,
      },
      refresh: { quota: refreshStatus(), resetCredits: refreshStatus() },
      resetCredits: {
        availableCount: 2,
        cumulativeRemainingMs: 3_600_000,
        failureCode: null,
        freshness: SourceFreshness.SourceFreshnessCurrent,
        lastAttemptAtMs: evaluatedAtMs,
        lastSuccessAtMs: evaluatedAtMs,
        nextExpiresAtMs: Date.parse("2026-07-18T08:00:00Z"),
        redeemedCount: 1,
        totalCount: 3,
        unknownReason: null,
      },
      sources: null,
      version: "quota-current-v1",
      windows: [
        quotaWindow(
          QuotaWindowKind.QuotaWindowPrimary,
          55,
          QuotaSource.QuotaSourceWham,
          QuotaCurrentFreshness.QuotaCurrentFresh,
          7_200_000,
        ),
        quotaWindow(
          QuotaWindowKind.QuotaWindowSecondary,
          71,
          QuotaSource.QuotaSourceLocalJSONL,
          QuotaCurrentFreshness.QuotaCurrentStale,
          86_400_000,
        ),
      ],
    },
    meta: {
      issues: null,
      page: null,
      status: ResponseStatus.ResponseComplete,
      version: "query-v1",
    },
  } satisfies QuotaCurrentResponse;
}

function query(data: unknown, overrides: Record<string, unknown> = {}) {
  return {
    data: { value: data },
    isError: { value: false },
    isFetching: { value: false },
    isPending: { value: false },
    isPlaceholderData: { value: false },
    refetch: vi.fn(),
    ...overrides,
  };
}

function readyQueries() {
  const usageTotals = totals();
  return {
    usage: query({
      currency: "USD",
      degradedReason: null,
      meta: meta(),
      pricingSource: "OpenAI public pricing",
      pricingVersions: ["2026.06"],
      range: { startAtMs: 1, endAtMs: 2, timeZone: "Asia/Shanghai" },
      reportingTimeZone: "Asia/Shanghai",
      totals: usageTotals,
      trend: [{
        key: "2026-07-16",
        startAtMs: known(Date.parse("2026-07-16T00:00:00Z"), NumericUnit.NumericMilliseconds),
        endAtMs: known(Date.parse("2026-07-17T00:00:00Z"), NumericUnit.NumericMilliseconds),
        totals: usageTotals,
      }],
      unpricedReasons: [{
        count: known(1, NumericUnit.NumericCount),
        reason: CostReason.CostReasonModelNotListed,
      }],
    }),
    quota: query(quotaResponse()),
    sessions: query({
      meta: meta(),
      items: [{
        activity: "active",
        displayTitle: "Session A",
        lastActivityAtMs: known(Date.parse("2026-07-16T07:00:00Z"), NumericUnit.NumericMilliseconds),
        model: { displayName: "gpt-5", confidence: "high", source: "event", reason: "exact" },
        project: { displayName: "Codex Pulse", confidence: "high", source: "git", reason: "exact" },
        sessionId: "safe-session-id",
        totals: usageTotals,
      }],
    }),
    projects: query({
      meta: meta(),
      items: [{
        dimensionKey: "safe-project-key",
        project: { displayName: "Codex Pulse", confidence: "high", source: "git", reason: "exact" },
        totals: usageTotals,
      }],
    }),
    sources: query({
      meta: meta(),
      items: [{
        sourceKey: "local_file:opaque",
        lastSuccessAtMs: known(Date.parse("2026-07-16T07:30:00Z"), NumericUnit.NumericMilliseconds),
      }],
      summary: {
        attention: known(0, NumericUnit.NumericCount),
        localFiles: known(7, NumericUnit.NumericCount),
      },
    }),
    health: query({
      meta: meta(),
      items: [],
      summary: {
        active: known(0, NumericUnit.NumericCount),
        level: "healthy",
      },
    }),
  };
}

function renderOverview() {
  return mount(OverviewView, {
    global: {
      plugins: [createAppI18n()],
      stubs: { UsageTrendChart: true },
    },
  });
}

describe("OverviewView", () => {
  beforeEach(() => {
    overviewHarness.queries = readyQueries();
  });

  it("renders quota, usage, cost, daily, activity, and health from typed query data", () => {
    const wrapper = renderOverview();

    for (const testId of [
      "overview-view",
      "quota-summary",
      "usage-summary",
      "token-composition",
      "cost-summary",
      "daily-table",
      "recent-sessions",
      "recent-projects",
      "index-health-summary",
    ]) {
      expect(wrapper.find(`[data-testid='${testId}']`).exists()).toBe(true);
    }
    expect(wrapper.text()).toContain("55%");
    expect(wrapper.text()).toContain("$1.00");
    expect(wrapper.text()).toContain("Session A");
    expect(wrapper.text()).toContain("Codex Pulse");
    expect(wrapper.text()).toContain("模型未列入定价目录");
    expect(wrapper.text()).toContain("刚刚更新");
    expect(wrapper.find("[data-testid='daily-table']").text()).toContain("缓存输入");
    expect(wrapper.find("[data-testid='daily-table']").text()).toContain("2026年7月16日");
    expect(wrapper.find("[data-testid='daily-table']").text()).not.toContain("2026-07-16");
    expect(wrapper.text()).not.toContain("safe-session-id");
  });

  it("isolates fatal query errors and never renders the underlying cause", () => {
    overviewHarness.queries = {
      ...readyQueries(),
      usage: query(undefined, {
        isError: { value: true },
        error: { value: new Error("private backend cause") },
      }),
    };

    const wrapper = renderOverview();
    expect(wrapper.find("[data-testid='quota-summary']").exists()).toBe(true);
    expect(wrapper.find("[data-testid='usage-error']").exists()).toBe(true);
    expect(wrapper.findAll("[role='alert']")).toHaveLength(1);
    expect(wrapper.text()).not.toContain("private backend cause");
  });

  it("keeps trusted data visible with partial and stale labels", () => {
    const partial = readyQueries();
    const sessionData = (partial.sessions as ReturnType<typeof query>).data.value as {
      meta: ReturnType<typeof meta>;
    };
    sessionData.meta = meta(ResponseStatus.ResponsePartial);
    partial.sessions = {
      ...(partial.sessions as ReturnType<typeof query>),
      isError: { value: true },
      isPlaceholderData: { value: true },
    };
    overviewHarness.queries = partial;

    const wrapper = renderOverview();
    expect(wrapper.find("[data-testid='recent-sessions']").text()).toContain("部分数据");
    expect(wrapper.find("[data-testid='recent-sessions']").text()).toContain("上次可信数据");
  });

  it("keeps reset credits visible when quota windows are unavailable", () => {
    const creditsOnly = readyQueries();
    const quotaData = (creditsOnly.quota as ReturnType<typeof query>).data.value as QuotaCurrentResponse;
    quotaData.current.windows = [];
    overviewHarness.queries = creditsOnly;

    const wrapper = renderOverview();
    expect(wrapper.find("[data-testid='quota-summary']").text()).toContain("Reset credits 2/3");
    expect(wrapper.find("[data-testid='quota-summary']").text()).not.toContain("当前范围暂无数据");
  });

  it("distinguishes unknown quota from a real exhausted zero", () => {
    const quotaStates = readyQueries();
    const quotaData = (quotaStates.quota as ReturnType<typeof query>).data.value as QuotaCurrentResponse;
    quotaData.current.windows![0].remainingPercent = null;
    quotaData.current.windows![1].remainingPercent = 0;
    overviewHarness.queries = quotaStates;

    const wrapper = renderOverview();
    expect(wrapper.find("[data-testid='quota-progress-unknown']").exists()).toBe(true);
    expect(wrapper.find("[data-testid='quota-progress-zero']").exists()).toBe(true);
  });

  it("announces one shared usage loading or partial state", () => {
    const loading = readyQueries();
    loading.usage = query(undefined, { isPending: { value: true } });
    overviewHarness.queries = loading;
    const loadingWrapper = renderOverview();
    expect(loadingWrapper.findAll("[role='status']")).toHaveLength(1);

    const partialUsage = readyQueries();
    const usageData = (partialUsage.usage as ReturnType<typeof query>).data.value as {
      meta: ReturnType<typeof meta>;
    };
    usageData.meta = meta(ResponseStatus.ResponsePartial);
    overviewHarness.queries = partialUsage;
    const partialWrapper = renderOverview();
    expect(partialWrapper.findAll("[role='status']")).toHaveLength(1);
  });

  it("keeps range controls available when usage is known empty", () => {
    const emptyUsage = readyQueries();
    const usageData = (emptyUsage.usage as ReturnType<typeof query>).data.value as {
      totals: ReturnType<typeof totals>;
      trend: unknown[];
    };
    usageData.trend = [];
    usageData.totals.turnCount = known(0, NumericUnit.NumericCount);
    overviewHarness.queries = emptyUsage;

    const wrapper = renderOverview();
    expect(wrapper.find("button[data-range='30d']").exists()).toBe(true);
    expect(wrapper.find("[data-testid='usage-summary']").text()).toContain("当前范围暂无数据");
  });
});
