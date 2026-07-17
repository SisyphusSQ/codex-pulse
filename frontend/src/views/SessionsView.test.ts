import { flushPromises, mount } from "@vue/test-utils";
import { ref } from "vue";
import { createMemoryHistory } from "vue-router";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { CostReason } from "@bindings/github.com/SisyphusSQ/codex-pulse/internal/pricing/models";
import {
  ErrorCode,
  NumericUnit,
  ResponseStatus,
  UnknownReason,
} from "@bindings/github.com/SisyphusSQ/codex-pulse/internal/query/models";
import {
  SessionTurnPricingStatus,
  SessionTurnState,
} from "@bindings/github.com/SisyphusSQ/codex-pulse/internal/query/usagecost/models";
import { createAppI18n } from "@/i18n";
import { createAppRouter } from "@/router";

import SessionsView from "./SessionsView.vue";

const sessionsHarness = vi.hoisted(() => ({
  capturedState: null as unknown,
  capturedTurnCursor: null as unknown,
  queries: {} as Record<string, unknown>,
}));

vi.mock("@/features/sessions/useSessionQueries", () => ({
  useSessionQueries: (state: unknown, turnCursor: unknown) => {
    sessionsHarness.capturedState = state;
    sessionsHarness.capturedTurnCursor = turnCursor;
    return sessionsHarness.queries;
  },
}));

const known = (value: number, unit = NumericUnit.NumericTokens) => ({ value, unit, unknownReason: null });
const unknown = (unit = NumericUnit.NumericTokens) => ({ value: null, unit, unknownReason: UnknownReason.UnknownUnavailable });
const attribution = (id: string, displayName: string | null) => ({
  confidence: "high",
  displayName,
  id,
  reason: "exact",
  source: "index",
});
const totals = (tokens = 24_000, cost = 1_000_000) => ({
  cachedInputTokens: known(8_000),
  estimatedUsdMicros: known(cost, NumericUnit.NumericMicroUSD),
  firstActivityAtMs: known(Date.parse("2026-07-16T06:00:00Z"), NumericUnit.NumericMilliseconds),
  inputTokens: known(10_000),
  lastActivityAtMs: known(Date.parse("2026-07-16T07:00:00Z"), NumericUnit.NumericMilliseconds),
  outputTokens: known(4_000),
  pricedTurnCount: known(1, NumericUnit.NumericCount),
  reasoningTokens: known(2_000),
  totalTokens: known(tokens),
  turnCount: known(2, NumericUnit.NumericCount),
  unpricedTurnCount: known(1, NumericUnit.NumericCount),
});
const item = {
  activity: "active",
  displayTitle: "设计 Codex Pulse",
  lastActivityAtMs: known(Date.parse("2026-07-16T07:00:00Z"), NumericUnit.NumericMilliseconds),
  model: attribution("model-safe-key", "GPT-5"),
  project: attribution("project-safe-id", "Codex Pulse"),
  sessionId: "opaque-session-a",
  titleConfidence: "high",
  titleReason: "exact",
  titleSource: "index",
  totals: totals(),
};
const meta = (status = ResponseStatus.ResponseComplete, page = { hasMore: true, limit: 50, nextCursor: "opaque-list-next" }) => ({
  issues: status === ResponseStatus.ResponseComplete ? null : [{
    code: ErrorCode.ErrorPartial,
    messageKey: "query.error.partial",
    retryable: true,
  }],
  page,
  status,
  version: "query-v1",
});
const listResponse = (status = ResponseStatus.ResponseComplete, items = [item]) => ({
  currency: "USD",
  degradedReason: status === ResponseStatus.ResponsePartial ? "rollup_missing" : null,
  items,
  matchedCount: known(items.length, NumericUnit.NumericCount),
  matchedTotals: totals(),
  meta: meta(status),
  pageTotals: totals(),
  pricingSource: "OpenAI public pricing",
});
const detailResponse = () => ({
  currency: "USD",
  degradedReason: null,
  item,
  meta: { ...meta(ResponseStatus.ResponsePartial), page: null },
  pricingSource: "OpenAI public pricing",
  pricingVersions: ["2026.06"],
  turnPage: { hasMore: true, limit: 20, nextCursor: "opaque-turn-next" },
  turns: [{
    completedAtMs: unknown(NumericUnit.NumericMilliseconds),
    model: attribution("model-safe-key", "GPT-5"),
    observedAtMs: known(Date.parse("2026-07-16T07:00:00Z"), NumericUnit.NumericMilliseconds),
    pricingStatus: SessionTurnPricingStatus.SessionTurnPricingPriced,
    pricingVersion: "2026.06",
    startedAtMs: known(Date.parse("2026-07-16T06:58:00Z"), NumericUnit.NumericMilliseconds),
    state: SessionTurnState.SessionTurnActive,
    timelineKey: "timeline-key",
    totals: totals(),
    unpricedReason: null,
  }],
  unpricedReasons: [{ count: known(1, NumericUnit.NumericCount), reason: CostReason.CostReasonModelNotListed }],
});

function query(data: unknown, error: unknown = null, overrides: Record<string, boolean> = {}) {
  return {
    data: ref(data),
    error: ref(error),
    isError: ref(error !== null),
    isFetching: ref(false),
    isPending: ref(data === undefined && error === null),
    isPlaceholderData: ref(false),
    refetch: vi.fn(),
    ...Object.fromEntries(Object.entries(overrides).map(([key, value]) => [key, ref(value)])),
  };
}

function runtimeError(code: ErrorCode, field: string | null) {
  return {
    cause: JSON.stringify({
      error: { code, field, messageKey: "query.error.safe", retryable: code === ErrorCode.ErrorUnavailable },
      version: "query-v1",
    }),
    message: "binding query failed",
  };
}

async function renderSessions(path = "/sessions?session=opaque-session-a") {
  const router = createAppRouter(createMemoryHistory());
  await router.push(path);
  await router.isReady();
  const wrapper = mount(SessionsView, {
    global: { plugins: [createAppI18n(), router] },
  });
  await flushPromises();
  return { router, wrapper };
}

describe("SessionsView", () => {
  beforeEach(() => {
    sessionsHarness.queries = {
      detail: query(detailResponse()),
      list: query(listResponse()),
    };
  });

  it("keeps filters, list cursor and selection in URL while Turn cursor stays ephemeral", async () => {
    const { router, wrapper } = await renderSessions();

    expect(wrapper.findAll("[data-testid='session-row']")).toHaveLength(1);
    expect(wrapper.text()).toContain("匹配 1 个会话");
    expect(wrapper.text()).toContain("Turn 用量与成本");

    const listQuery = sessionsHarness.queries.list as ReturnType<typeof query>;
    listQuery.isPlaceholderData.value = true;
    await flushPromises();
    expect(wrapper.get("[data-testid='sessions-list-next']").attributes()).toHaveProperty("disabled");
    listQuery.isPlaceholderData.value = false;

    await wrapper.get("[data-testid='sessions-activity-idle']").trigger("click");
    await flushPromises();
    expect(router.currentRoute.value.query).toEqual({ activity: "idle" });

    await wrapper.get("[data-testid='session-row']").trigger("click");
    await flushPromises();
    expect(router.currentRoute.value.query).toEqual({ activity: "idle", session: "opaque-session-a" });

    const turnNext = wrapper.get("[data-testid='turn-next']");
    await Promise.all([turnNext.trigger("click"), turnNext.trigger("click")]);
    expect((sessionsHarness.capturedTurnCursor as { value: string | null }).value).toBe("opaque-turn-next");
    expect(router.currentRoute.value.query).not.toHaveProperty("turnCursor");
    const detailQuery = sessionsHarness.queries.detail as ReturnType<typeof query>;
    detailQuery.isFetching.value = true;
    await flushPromises();
    detailQuery.isFetching.value = false;
    await flushPromises();
    await wrapper.get("[data-testid='turn-previous']").trigger("click");
    expect((sessionsHarness.capturedTurnCursor as { value: string | null }).value).toBeNull();
    expect(wrapper.get("[data-testid='turn-previous']").attributes()).toHaveProperty("disabled");

    const firstNext = wrapper.get("[data-testid='sessions-list-next']");
    await Promise.all([firstNext.trigger("click"), firstNext.trigger("click")]);
    await flushPromises();
    expect(router.currentRoute.value.query).toEqual({ activity: "idle", cursor: "opaque-list-next" });
    await wrapper.get("[data-testid='sessions-list-previous']").trigger("click");
    await flushPromises();
    expect(router.currentRoute.value.query).toEqual({ activity: "idle" });
    expect(wrapper.get("[data-testid='sessions-list-previous']").attributes()).toHaveProperty("disabled");

    await wrapper.get("[data-testid='sessions-list-next']").trigger("click");
    await flushPromises();
    router.back();
    await flushPromises();
    expect(router.currentRoute.value.query).toEqual({ activity: "idle" });
    expect(wrapper.get("[data-testid='sessions-list-previous']").attributes()).toHaveProperty("disabled");

    await wrapper.get("[data-testid='sessions-list-next']").trigger("click");
    await flushPromises();
    await router.push("/sessions?activity=active");
    await flushPromises();
    expect(wrapper.get("[data-testid='sessions-list-previous']").attributes()).toHaveProperty("disabled");
  });

  it("normalizes invalid URL state and keeps partial known-empty explicit", async () => {
    sessionsHarness.queries = {
      detail: query(undefined),
      list: query(listResponse(ResponseStatus.ResponsePartial, [])),
    };
    const { router, wrapper } = await renderSessions("/sessions?activity=busy&ignored=secret");

    expect(router.currentRoute.value.query).toEqual({});
    expect(wrapper.get("[data-testid='sessions-normalized']").text()).toContain("恢复为默认值");
    expect(wrapper.get("[data-testid='sessions-list-empty']").text()).toContain("暂无会话");
    expect(wrapper.text()).toContain("部分数据");
    expect(wrapper.text()).not.toContain("secret");
  });

  it("does not steal focus on URL restore and focuses detail after a user selection", async () => {
    const focus = vi.spyOn(HTMLElement.prototype, "focus");
    const { wrapper } = await renderSessions();

    expect(focus).not.toHaveBeenCalled();
    const row = wrapper.get("[data-testid='session-row']").element;
    await wrapper.get("[data-testid='detail-close']").trigger("click");
    await flushPromises();
    expect(focus).toHaveBeenCalledOnce();
    expect(focus.mock.instances[0]).toBe(row);
    await wrapper.get("[data-testid='session-row']").trigger("click");
    await flushPromises();
    expect(focus).toHaveBeenCalledTimes(2);
    expect(focus.mock.instances[1]).toBe(wrapper.get("#session-detail-title").element);
    focus.mockRestore();
  });

  it("focuses detail only after an asynchronous selection response arrives", async () => {
    const detail = query(undefined);
    sessionsHarness.queries = { detail, list: query(listResponse()) };
    const focus = vi.spyOn(HTMLElement.prototype, "focus");
    const { wrapper } = await renderSessions("/sessions");

    await wrapper.get("[data-testid='session-row']").trigger("click");
    await flushPromises();
    expect(focus).not.toHaveBeenCalled();

    detail.data.value = detailResponse();
    detail.isPending.value = false;
    await flushPromises();
    expect(focus).toHaveBeenCalledOnce();
    expect(focus.mock.instances[0]).toBe(wrapper.get("#session-detail-title").element);
    focus.mockRestore();
  });

  it("keeps trusted detail visible with stale recovery after a background error", async () => {
    const detail = query(
      detailResponse(),
      runtimeError(ErrorCode.ErrorUnavailable, null),
    );
    sessionsHarness.queries = { detail, list: query(listResponse()) };
    const { wrapper } = await renderSessions();

    expect(wrapper.get("[data-testid='sessions-detail-stale']").text()).toContain("上次可信数据");
    expect(wrapper.text()).toContain("Turn 用量与成本");
    expect(wrapper.text()).not.toContain("binding query failed");
    await wrapper.get("[data-testid='sessions-detail-stale-retry']").trigger("click");
    expect(detail.refetch).toHaveBeenCalledOnce();
  });

  it("recovers list cursor validation and detail not-found without exposing causes", async () => {
    sessionsHarness.queries = {
      detail: query(undefined, runtimeError(ErrorCode.ErrorNotFound, null)),
      list: query(undefined, runtimeError(ErrorCode.ErrorValidation, "page.cursor")),
    };
    const { router, wrapper } = await renderSessions("/sessions?cursor=tampered&session=opaque-session-a");

    expect(wrapper.text()).not.toContain("tampered");
    expect(wrapper.get("[data-testid='sessions-list-error']").text()).toContain("暂不可用");
    await wrapper.get("[data-testid='sessions-list-recover']").trigger("click");
    await flushPromises();
    expect(router.currentRoute.value.query).toEqual({});

    await router.push("/sessions?session=opaque-session-a");
    await flushPromises();
    expect(wrapper.get("[data-testid='sessions-detail-error']").text()).toContain("详情暂不可用");
    await wrapper.get("[data-testid='sessions-detail-recover']").trigger("click");
    await flushPromises();
    expect(router.currentRoute.value.query).toEqual({});
  });
});
