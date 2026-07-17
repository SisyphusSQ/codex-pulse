import { mount } from "@vue/test-utils";
import { describe, expect, it, vi } from "vitest";

import { CostReason } from "@bindings/github.com/SisyphusSQ/codex-pulse/internal/pricing/models";
import { NumericUnit, ResponseStatus, UnknownReason } from "@bindings/github.com/SisyphusSQ/codex-pulse/internal/query/models";
import {
  SessionTurnPricingStatus,
  SessionTurnState,
  type SessionDetailResponse,
} from "@bindings/github.com/SisyphusSQ/codex-pulse/internal/query/usagecost/models";
import { createAppI18n } from "@/i18n";

import SessionDetailPanel from "./SessionDetailPanel.vue";

const known = (value: number, unit = NumericUnit.NumericTokens) => ({ value, unit, unknownReason: null });
const unknown = (unit = NumericUnit.NumericTokens) => ({ value: null, unit, unknownReason: UnknownReason.UnknownUnavailable });
const attribution = (displayName: string, id: string) => ({
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

const detail: SessionDetailResponse = {
  currency: "USD",
  degradedReason: null,
  item: {
    activity: "active",
    displayTitle: "设计 Agent Tracker",
    lastActivityAtMs: known(Date.parse("2026-07-16T07:00:00Z"), NumericUnit.NumericMilliseconds),
    model: attribution("GPT-5", "model-secret"),
    project: attribution("Codex Pulse", "project-secret"),
    sessionId: "session-secret",
    titleConfidence: "high",
    titleReason: "exact",
    titleSource: "index",
    totals: totals(),
  },
  meta: {
    issues: null,
    page: null,
    status: ResponseStatus.ResponsePartial,
    version: "query-v1",
  },
  pricingSource: "OpenAI public pricing",
  pricingVersions: ["2026.06"],
  turnPage: { hasMore: true, limit: 20, nextCursor: "turn-secret-cursor" },
  turns: [
    {
      completedAtMs: unknown(NumericUnit.NumericMilliseconds),
      model: attribution("GPT-5", "turn-model-secret"),
      observedAtMs: known(Date.parse("2026-07-16T07:00:00Z"), NumericUnit.NumericMilliseconds),
      pricingStatus: SessionTurnPricingStatus.SessionTurnPricingPriced,
      pricingVersion: "2026.06",
      startedAtMs: known(Date.parse("2026-07-16T06:58:00Z"), NumericUnit.NumericMilliseconds),
      state: SessionTurnState.SessionTurnActive,
      timelineKey: "timeline-secret-a",
      totals: totals(12_000, 500_000),
      unpricedReason: null,
    },
    {
      completedAtMs: known(Date.parse("2026-07-16T06:50:00Z"), NumericUnit.NumericMilliseconds),
      model: attribution("GPT-5", "turn-model-two"),
      observedAtMs: known(Date.parse("2026-07-16T06:50:00Z"), NumericUnit.NumericMilliseconds),
      pricingStatus: SessionTurnPricingStatus.SessionTurnPricingUnpriced,
      pricingVersion: null,
      startedAtMs: known(Date.parse("2026-07-16T06:48:00Z"), NumericUnit.NumericMilliseconds),
      state: SessionTurnState.SessionTurnComplete,
      timelineKey: "timeline-secret-b",
      totals: { ...totals(12_000, 0), estimatedUsdMicros: unknown(NumericUnit.NumericMicroUSD) },
      unpricedReason: CostReason.CostReasonModelNotListed,
    },
  ],
  unpricedReasons: [{ reason: CostReason.CostReasonModelNotListed, count: known(1, NumericUnit.NumericCount) }],
};

describe("SessionDetailPanel", () => {
  it("renders aggregate and content-free Turn evidence without leaking identities", async () => {
    const wrapper = mount(SessionDetailPanel, {
      props: { detail, hasPreviousTurnPage: false, timeZone: "Asia/Shanghai" },
      global: { plugins: [createAppI18n()] },
    });

    expect(wrapper.text()).toContain("设计 Agent Tracker");
    expect(wrapper.text()).toContain("2.4万");
    expect(wrapper.text()).toContain("$1.00");
    expect(wrapper.text()).toContain("2026.06");
    expect(wrapper.text()).toContain("进行中");
    expect(wrapper.text()).toContain("已完成");
    expect(wrapper.text()).toContain("未定价");
    expect(wrapper.text()).toContain("部分数据");
    expect(wrapper.text()).toContain("2 个 Turn");
    expect(wrapper.text()).toContain("模型未列入定价目录 (1)");
    expect(wrapper.text()).toContain("观测时间");
    expect(wrapper.text()).toContain("完成时间");
    for (const secret of [
      "session-secret",
      "project-secret",
      "model-secret",
      "timeline-secret-a",
      "turn-secret-cursor",
    ]) {
      expect(wrapper.text()).not.toContain(secret);
      expect(wrapper.html()).not.toContain(secret);
    }

    await wrapper.get("[data-testid='turn-next']").trigger("click");
    await wrapper.get("[data-testid='detail-close']").trigger("click");
    expect(wrapper.emitted("next-turn-page")).toHaveLength(1);
    expect(wrapper.emitted("close")).toHaveLength(1);
    expect(wrapper.get("[data-testid='turn-previous']").attributes()).toHaveProperty("disabled");
    expect(wrapper.get("aside").classes()).toContain("xl:max-h-[calc(100vh-13rem)]");

    const focus = vi.spyOn(HTMLElement.prototype, "focus");
    (wrapper.vm as unknown as { focusHeading: () => void }).focusHeading();
    expect(focus).toHaveBeenCalledOnce();
    focus.mockRestore();
  });
});
