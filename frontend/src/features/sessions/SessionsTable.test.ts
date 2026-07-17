import { mount } from "@vue/test-utils";
import { describe, expect, it } from "vitest";

import { NumericUnit, UnknownReason } from "@bindings/github.com/SisyphusSQ/codex-pulse/internal/query/models";
import type { SessionItem } from "@bindings/github.com/SisyphusSQ/codex-pulse/internal/query/usagecost/models";
import { createAppI18n } from "@/i18n";

import SessionsTable from "./SessionsTable.vue";

const known = (value: number, unit = NumericUnit.NumericTokens) => ({
  unknownReason: null,
  unit,
  value,
});
const unknown = (unit = NumericUnit.NumericTokens) => ({
  unknownReason: UnknownReason.UnknownUnavailable,
  unit,
  value: null,
});
const attribution = (id: string, displayName: string | null) => ({
  confidence: "high",
  displayName,
  id,
  reason: "exact",
  source: "index",
});
const totals = (value: number | null) => ({
  cachedInputTokens: value === null ? unknown() : known(0),
  estimatedUsdMicros: value === null ? unknown(NumericUnit.NumericMicroUSD) : known(value, NumericUnit.NumericMicroUSD),
  firstActivityAtMs: known(Date.parse("2026-07-16T06:00:00Z"), NumericUnit.NumericMilliseconds),
  inputTokens: value === null ? unknown() : known(0),
  lastActivityAtMs: known(Date.parse("2026-07-16T07:00:00Z"), NumericUnit.NumericMilliseconds),
  outputTokens: value === null ? unknown() : known(0),
  pricedTurnCount: known(0, NumericUnit.NumericCount),
  reasoningTokens: value === null ? unknown() : known(0),
  totalTokens: value === null ? unknown() : known(0),
  turnCount: known(0, NumericUnit.NumericCount),
  unpricedTurnCount: known(0, NumericUnit.NumericCount),
});

const items: SessionItem[] = [
  {
    activity: "active",
    displayTitle: "设计 Codex Pulse",
    lastActivityAtMs: known(Date.parse("2026-07-16T07:00:00Z"), NumericUnit.NumericMilliseconds),
    model: attribution("model-safe-key", "GPT-5"),
    project: attribution("project-safe-id", "Codex Pulse"),
    sessionId: "opaque-session-a",
    titleConfidence: "high",
    titleReason: "exact",
    titleSource: "index",
    totals: totals(0),
  },
  {
    activity: "idle",
    displayTitle: "未定价会话",
    lastActivityAtMs: unknown(NumericUnit.NumericMilliseconds),
    model: attribution("model-two", null),
    project: attribution("project-two", null),
    sessionId: "opaque-session-b",
    titleConfidence: "low",
    titleReason: "fallback",
    titleSource: "index",
    totals: totals(null),
  },
];

describe("SessionsTable", () => {
  it("renders server order, confidence, real zero and unknown without opaque identities", async () => {
    const wrapper = mount(SessionsTable, {
      props: {
        items,
        selectedSessionId: "opaque-session-a",
        timeZone: "Asia/Shanghai",
      },
      global: { plugins: [createAppI18n()] },
    });

    const rows = wrapper.findAll("[data-testid='session-row']");
    expect(rows).toHaveLength(2);
    expect(rows[0].attributes("aria-selected")).toBe("true");
    expect(rows[0].text()).toContain("ACTIVE");
    expect(rows[0].text()).toContain("$0.00");
    expect(rows[1].text()).toContain("--");
    expect(wrapper.text()).toContain("高可信");
    expect(rows[0].text().match(/高可信/gu)).toHaveLength(3);
    expect(wrapper.text()).not.toContain("opaque-session-a");
    expect(wrapper.text()).not.toContain("project-safe-id");
    expect(wrapper.text()).not.toContain("model-safe-key");

    await rows[1].trigger("keydown", { key: "Enter" });
    await rows[0].trigger("keydown", { key: " " });
    expect(wrapper.emitted("select")).toEqual([
      ["opaque-session-b"],
      ["opaque-session-a"],
    ]);
  });
});
