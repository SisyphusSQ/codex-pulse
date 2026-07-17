import { mount } from "@vue/test-utils";
import { describe, expect, it, vi } from "vitest";

import { NumericUnit, ResponseStatus, UnknownReason } from "@bindings/github.com/SisyphusSQ/codex-pulse/internal/query/models";
import type { ProjectDetailResponse } from "@bindings/github.com/SisyphusSQ/codex-pulse/internal/query/usagecost/models";
import { createAppI18n } from "@/i18n";

import ProjectDetailPanel from "./ProjectDetailPanel.vue";

const known = (value: number, unit = NumericUnit.NumericTokens) => ({ value, unit, unknownReason: null });
const unknown = (unit = NumericUnit.NumericTokens) => ({ value: null, unit, unknownReason: UnknownReason.UnknownUnavailable });
const attribution = (displayName: string | null, id: string | null, confidence = "high") => ({
  confidence,
  displayName,
  id,
  reason: confidence === "unknown" ? "missing" : "root_matched",
  source: confidence === "unknown" ? "missing" : "registered_root",
});
const totals = (tokens = 24_000, cost = 1_000_000) => ({
  cachedInputTokens: known(8_000),
  estimatedUsdMicros: known(cost, NumericUnit.NumericMicroUSD),
  firstActivityAtMs: known(Date.parse("2026-07-15T06:00:00Z"), NumericUnit.NumericMilliseconds),
  inputTokens: known(10_000),
  lastActivityAtMs: known(Date.parse("2026-07-16T07:00:00Z"), NumericUnit.NumericMilliseconds),
  outputTokens: known(4_000),
  pricedTurnCount: known(1, NumericUnit.NumericCount),
  reasoningTokens: known(2_000),
  totalTokens: known(tokens),
  turnCount: known(2, NumericUnit.NumericCount),
  unpricedTurnCount: known(1, NumericUnit.NumericCount),
});

const detail: ProjectDetailResponse = {
  currency: "USD",
  daily: [{
    bucketStartAtMs: known(Date.parse("2026-07-15T16:00:00Z"), NumericUnit.NumericMilliseconds),
    confidence: "high",
    reason: "root_matched",
    source: "registered_root",
    totals: totals(),
  }],
  globalTotals: totals(100_000, 4_000_000),
  item: {
    dimensionKey: "project-secret",
    project: attribution("Codex Pulse", "project-secret"),
    sessionCount: known(2, NumericUnit.NumericCount),
    totals: totals(),
    trend: [],
  },
  meta: {
    issues: [{ code: "partial" as never, messageKey: "query.partial", retryable: true }],
    page: null,
    status: ResponseStatus.ResponsePartial,
    version: "query-v1",
  },
  modelPage: { hasMore: true, limit: 20, nextCursor: "model-secret-cursor" },
  models: [
    { dimensionKey: "model-secret", model: attribution("GPT-5", "model-secret"), totals: totals(16_000, 700_000) },
    { dimensionKey: "unknown-model-secret", model: attribution(null, null, "unknown"), totals: { ...totals(0, 0), totalTokens: unknown() } },
  ],
  pricingSource: "OpenAI public pricing",
  pricingVersions: ["2026.06"],
  range: {
    endAtMs: Date.parse("2026-07-17T16:00:00Z"),
    startAtMs: Date.parse("2026-07-10T16:00:00Z"),
    timeZone: "Asia/Shanghai",
  },
  reportingTimeZone: "Asia/Shanghai",
  sessionPage: { hasMore: true, limit: 20, nextCursor: "session-secret-cursor" },
  sessions: [
    {
      activity: "active",
      displayTitle: "实现 Projects 页面",
      lastActivityAtMs: known(Date.parse("2026-07-16T07:00:00Z"), NumericUnit.NumericMilliseconds),
      model: attribution("GPT-5", "session-model-secret"),
      sessionId: "session-secret",
      titleConfidence: "high",
      titleReason: "stable_identity",
      titleSource: "session_id_fallback",
      totals: totals(12_000, 500_000),
    },
  ],
};

describe("ProjectDetailPanel", () => {
  it("renders provider aggregate, daily, model, and Session contributions without identities", async () => {
    const wrapper = mount(ProjectDetailPanel, {
      props: {
        detail,
        hasPreviousModelPage: false,
        hasPreviousSessionPage: false,
        timeZone: "Asia/Shanghai",
      },
      global: {
        plugins: [createAppI18n()],
        stubs: { ProjectTrendChart: { template: "<div data-testid='project-detail-trend' />" } },
      },
    });

    expect(wrapper.text()).toContain("Codex Pulse");
    expect(wrapper.text()).toContain("2.4万");
    expect(wrapper.text()).toContain("$1.00");
    expect(wrapper.text()).toContain("2 个会话");
    expect(wrapper.text()).toContain("部分数据");
    expect(wrapper.text()).toContain("2026.06");
    expect(wrapper.text()).toContain("OpenAI public pricing");
    expect(wrapper.text()).toContain("全局同范围 Token 10万");
    expect(wrapper.text()).toContain("已定价 Turn 1");
    expect(wrapper.text()).toContain("未定价 Turn 1");
    expect(wrapper.text()).toContain("模型贡献");
    expect(wrapper.text()).toContain("GPT-5");
    expect(wrapper.text()).toContain("未知模型");
    expect(wrapper.text()).toContain("近期会话贡献");
    expect(wrapper.text()).toContain("实现 Projects 页面");
    expect(wrapper.text()).toContain("贡献 Token 1.2万");
    expect(wrapper.findAll("progress").every((progress) => progress.classes("project-model-progress"))).toBe(true);
    expect(wrapper.findAll("section").map((section) => section.attributes("aria-labelledby"))).toEqual([
      "project-models-title",
      "project-sessions-title",
      "project-daily-title",
    ]);
    for (const secret of [
      "project-secret",
      "model-secret",
      "unknown-model-secret",
      "session-secret",
      "session-model-secret",
      "model-secret-cursor",
      "session-secret-cursor",
    ]) {
      expect(wrapper.html()).not.toContain(secret);
    }

    await wrapper.get("[data-testid='project-model-next']").trigger("click");
    await wrapper.get("[data-testid='project-session-next']").trigger("click");
    await wrapper.get("[data-testid='project-detail-close']").trigger("click");
    expect(wrapper.emitted("next-model-page")).toHaveLength(1);
    expect(wrapper.emitted("next-session-page")).toHaveLength(1);
    expect(wrapper.emitted("close")).toHaveLength(1);
    expect(wrapper.get("[data-testid='project-model-previous']").attributes()).toHaveProperty("disabled");
    expect(wrapper.get("[data-testid='project-session-previous']").attributes()).toHaveProperty("disabled");

    const focus = vi.spyOn(HTMLElement.prototype, "focus");
    (wrapper.vm as unknown as { focusHeading: () => void }).focusHeading();
    expect(focus).toHaveBeenCalledOnce();
    focus.mockRestore();
  });
});
