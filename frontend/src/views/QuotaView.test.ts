import { flushPromises, mount } from "@vue/test-utils";
import { ref } from "vue";
import { beforeEach, describe, expect, it, vi } from "vitest";

import {
  CurrentRefreshState,
  CurrentSourceKind,
} from "@bindings/github.com/SisyphusSQ/codex-pulse/internal/codex/quota/models";
import { ErrorCode, ResponseStatus } from "@bindings/github.com/SisyphusSQ/codex-pulse/internal/query/models";
import type { QuotaCurrentResponse } from "@bindings/github.com/SisyphusSQ/codex-pulse/internal/query/runtimeinfo/models";
import {
  QuotaConflictState,
  QuotaCurrentFreshness,
  QuotaEvidenceDisposition,
  QuotaExplanationCode,
  QuotaRejectionReason,
  QuotaSource,
  QuotaValidity,
  QuotaWindowKind,
  SourceFailureCode,
  SourceFreshness,
  SourceRefreshReason,
} from "@bindings/github.com/SisyphusSQ/codex-pulse/internal/store/models";
import { createAppI18n } from "@/i18n";

import QuotaView from "./QuotaView.vue";

const quotaHarness = vi.hoisted(() => ({ page: {} as Record<string, unknown> }));

vi.mock("@/features/quota/useQuotaPage", () => ({
  useQuotaPage: () => quotaHarness.page,
}));

const evaluatedAtMS = Date.parse("2026-07-17T08:00:00Z");

function refreshStatus(state = CurrentRefreshState.CurrentRefreshScheduled) {
  return {
    activeTrigger: null,
    claimExpiresAtMs: null,
    claimStartedAtMs: null,
    lastManualAtMs: evaluatedAtMS - 60_000,
    nextDueAtMs: evaluatedAtMS + 60_000,
    reason: SourceRefreshReason.RefreshReasonManual,
    state,
    unknownReason: null,
  };
}

function quotaResponse(status = ResponseStatus.ResponseComplete) {
  return {
    current: {
      accountScope: "private-account-scope",
      evaluatedAtMs: evaluatedAtMS,
      nextReset: {
        atMs: evaluatedAtMS + 60 * 60_000,
        remainingMs: 60 * 60_000,
        trustedWindowCount: 1,
        unknownReason: null,
      },
      refresh: {
        quota: refreshStatus(),
        resetCredits: refreshStatus(CurrentRefreshState.CurrentRefreshPaused),
      },
      resetCredits: {
        availableCount: 0,
        cumulativeRemainingMs: 0,
        failureCode: SourceFailureCode.SourceFailureSchemaIncompatible,
        freshness: SourceFreshness.SourceFreshnessStale,
        lastAttemptAtMs: evaluatedAtMS,
        lastSuccessAtMs: evaluatedAtMS - 60_000,
        nextExpiresAtMs: null,
        redeemedCount: 3,
        totalCount: 3,
        unknownReason: null,
      },
      sources: [
        {
          conflictWindowCount: 0,
          failureCode: SourceFailureCode.SourceFailureAuthRequired,
          freshness: SourceFreshness.SourceFreshnessUnavailable,
          lastAttemptAtMs: evaluatedAtMS,
          lastObservedAtMs: evaluatedAtMS - 120_000,
          lastSuccessAtMs: evaluatedAtMS - 120_000,
          selectedWindowCount: 1,
          source: CurrentSourceKind.CurrentSourceWham,
          unknownReason: null,
        },
        {
          conflictWindowCount: 1,
          failureCode: SourceFailureCode.SourceFailureHTTP429,
          freshness: SourceFreshness.SourceFreshnessCurrent,
          lastAttemptAtMs: evaluatedAtMS,
          lastObservedAtMs: evaluatedAtMS,
          lastSuccessAtMs: evaluatedAtMS,
          selectedWindowCount: 0,
          source: CurrentSourceKind.CurrentSourceLocal,
          unknownReason: null,
        },
      ],
      version: "quota-current-v1",
      windows: [
        {
          conflict: QuotaConflictState.QuotaConflictNone,
          explanationCode: QuotaExplanationCode.QuotaExplanationTrusted,
          explanations: [{
            disposition: QuotaEvidenceDisposition.QuotaEvidenceSelected,
            explanationCode: QuotaExplanationCode.QuotaExplanationTrusted,
            observationId: "private-observation-id",
            observedAtMs: evaluatedAtMS,
            reason: null,
            remainingPercent: null,
            resetsAtMs: evaluatedAtMS + 60 * 60_000,
            source: QuotaSource.QuotaSourceWham,
            usedPercent: null,
            validity: QuotaValidity.QuotaValidityAccepted,
            windowGeneration: 1,
            windowMinutes: 300,
          }, {
            disposition: QuotaEvidenceDisposition.QuotaEvidenceSuspicious,
            explanationCode: QuotaExplanationCode.QuotaExplanationSourceConflict,
            observationId: "private-suspicious-observation-id",
            observedAtMs: evaluatedAtMS - 120_000,
            reason: QuotaRejectionReason.QuotaReasonSourceConflict,
            remainingPercent: 65,
            resetsAtMs: evaluatedAtMS + 60 * 60_000,
            source: QuotaSource.QuotaSourceLocalJSONL,
            usedPercent: 35,
            validity: QuotaValidity.QuotaValiditySuspicious,
            windowGeneration: 1,
            windowMinutes: 300,
          }],
          freshness: QuotaCurrentFreshness.QuotaCurrentFresh,
          lastAttemptAtMs: evaluatedAtMS,
          lastSuccessAtMs: evaluatedAtMS,
          limitId: "private-limit-id",
          remainingPercent: null,
          resetRemainingMs: 60 * 60_000,
          resetsAtMs: evaluatedAtMS + 60 * 60_000,
          selectedSource: QuotaSource.QuotaSourceWham,
          unknownReason: null,
          usedPercent: null,
          windowGeneration: 1,
          windowKind: QuotaWindowKind.QuotaWindowPrimary,
          windowMinutes: 300,
        },
        {
          conflict: QuotaConflictState.QuotaConflictPresent,
          explanationCode: QuotaExplanationCode.QuotaExplanationSourceConflict,
          explanations: null,
          freshness: QuotaCurrentFreshness.QuotaCurrentStale,
          lastAttemptAtMs: evaluatedAtMS,
          lastSuccessAtMs: evaluatedAtMS - 60_000,
          limitId: "private-secondary-limit-id",
          remainingPercent: 0,
          resetRemainingMs: 7 * 24 * 60 * 60_000,
          resetsAtMs: evaluatedAtMS + 7 * 24 * 60 * 60_000,
          selectedSource: QuotaSource.QuotaSourceLocalJSONL,
          unknownReason: null,
          usedPercent: 100,
          windowGeneration: 1,
          windowKind: QuotaWindowKind.QuotaWindowSecondary,
          windowMinutes: 10_080,
        },
      ],
    },
    meta: {
      issues: status === ResponseStatus.ResponseComplete ? null : [{
        code: ErrorCode.ErrorPartial,
        messageKey: "query.error.partial",
        retryable: true,
      }],
      page: null,
      status,
      version: "query-v1",
    },
  } satisfies QuotaCurrentResponse;
}

function page(data: QuotaCurrentResponse | undefined) {
  return {
    quota: {
      data: ref(data),
      error: ref<unknown>(null),
      isError: ref(false),
      isFetching: ref(false),
      isPending: ref(data === undefined),
      refetch: vi.fn(),
    },
    refresh: {
      error: ref<unknown>(null),
      isError: ref(false),
      isPending: ref(false),
    },
    refreshAll: vi.fn(),
  };
}

function renderQuota() {
  return mount(QuotaView, { global: { plugins: [createAppI18n()] } });
}

describe("QuotaView", () => {
  beforeEach(() => {
    quotaHarness.page = page(quotaResponse());
  });

  it("renders only returned windows, sources, evidence and Reset credits without private identities", async () => {
    const state = quotaHarness.page as ReturnType<typeof page>;
    const wrapper = renderQuota();

    expect(wrapper.findAll("[data-testid='quota-window']")).toHaveLength(2);
    expect(wrapper.findAll("[data-testid='quota-source']")).toHaveLength(2);
    expect(wrapper.findAll("[data-testid='quota-evidence']")).toHaveLength(2);
    expect(wrapper.get("[data-testid='quota-progress-unknown']").attributes("role")).toBeUndefined();
    expect(wrapper.get("[data-testid='quota-progress-zero']").attributes("aria-valuenow")).toBe("0");
    expect(wrapper.get("[data-testid='quota-reset-credits']").text()).toContain("0 / 3");
    expect(wrapper.text()).toContain("在线额度服务");
    expect(wrapper.text()).toContain("本机日志");
    expect(wrapper.text()).toContain("需要重新登录");
    expect(wrapper.text()).toContain("请求过于频繁");
    expect(wrapper.text()).toContain("来源数据结构不兼容");
    expect(wrapper.text()).toContain("来源存在冲突");
    expect(wrapper.text()).toContain("重置于");
    expect(wrapper.text()).toContain("观测于");
    expect(wrapper.text()).toContain("可疑候选");
    expect(wrapper.text()).toContain("原因：来源冲突");
    expect(wrapper.text()).not.toContain("private-observation-id");
    expect(wrapper.text()).not.toContain("private-suspicious-observation-id");
    expect(wrapper.text()).not.toContain("private-limit-id");
    expect(wrapper.text()).not.toContain("private-account-scope");

    state.quota.data.value!.current.sources![0].failureCode =
      SourceFailureCode.SourceFailureNetworkUnavailable;
    await flushPromises();
    expect(wrapper.text()).toContain("网络暂不可用");

    wrapper.unmount();
  });

  it("renders explicit loading and known-empty states", () => {
    quotaHarness.page = page(undefined);
    const loading = renderQuota();
    expect(loading.get("[role='status']").text()).toContain("正在读取配额数据");
    loading.unmount();

    const emptyResponse = quotaResponse() as QuotaCurrentResponse;
    emptyResponse.current.windows = [];
    emptyResponse.current.sources = [];
    emptyResponse.current.resetCredits.availableCount = null;
    emptyResponse.current.resetCredits.totalCount = null;
    quotaHarness.page = page(emptyResponse);
    const empty = renderQuota();
    expect(empty.get("[role='status']").text()).toContain("尚无配额事实");
    empty.unmount();
  });

  it("does not derive a countdown from an untrusted future reset timestamp", () => {
    const response = quotaResponse() as QuotaCurrentResponse;
    const expiredWindow = response.current.windows?.[0];
    if (expiredWindow === undefined) throw new Error("expected primary quota window fixture");
    expiredWindow.freshness = QuotaCurrentFreshness.QuotaCurrentExpiredUnknown;
    expiredWindow.resetRemainingMs = null;
    expiredWindow.resetsAtMs = evaluatedAtMS + 60 * 60_000;
    quotaHarness.page = page(response);

    const wrapper = renderQuota();
    const primaryWindow = wrapper.findAll("[data-testid='quota-window']")[0];

    expect(primaryWindow.text()).toContain("重置时间未知");
    expect(primaryWindow.text()).toContain("重置于");
    expect(primaryWindow.text()).not.toContain("约 1 小时后重置");
    wrapper.unmount();
  });

  it("keeps last-known-good data visible during query or refresh failure", async () => {
    const state = page(quotaResponse(ResponseStatus.ResponsePartial));
    state.quota.isError.value = true;
    state.quota.error.value = new Error("private-query-cause");
    state.refresh.isError.value = true;
    state.refresh.error.value = new Error("private-command-cause");
    quotaHarness.page = state;
    const wrapper = renderQuota();

    expect(wrapper.find("[data-testid='quota-stale']").exists()).toBe(true);
    expect(wrapper.find("[data-testid='quota-partial']").exists()).toBe(true);
    expect(wrapper.find("[data-testid='quota-refresh-error']").exists()).toBe(true);
    expect(wrapper.findAll("[data-testid='quota-window']")).toHaveLength(2);
    expect(wrapper.text()).not.toContain("private-query-cause");
    expect(wrapper.text()).not.toContain("private-command-cause");

    await wrapper.get("[data-testid='quota-refresh-action']").trigger("click");
    expect(state.refreshAll).toHaveBeenCalledOnce();
    state.refresh.isPending.value = true;
    await flushPromises();
    expect(wrapper.get("[data-testid='quota-refresh-action']").attributes()).toHaveProperty("disabled");
    wrapper.unmount();
  });

  it("uses a generic recoverable state for an initial fatal query failure", async () => {
    const state = page(undefined);
    state.quota.isPending.value = false;
    state.quota.isError.value = true;
    state.quota.error.value = new Error("private-initial-cause");
    quotaHarness.page = state;
    const wrapper = renderQuota();

    expect(wrapper.get("[data-testid='quota-fatal-error']").text()).toContain("配额数据暂不可用");
    expect(wrapper.text()).not.toContain("private-initial-cause");
    await wrapper.get("[data-testid='quota-query-retry']").trigger("click");
    expect(state.quota.refetch).toHaveBeenCalledOnce();
    wrapper.unmount();
  });
});
