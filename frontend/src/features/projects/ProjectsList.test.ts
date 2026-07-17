import { mount } from "@vue/test-utils";
import { describe, expect, it } from "vitest";

import { NumericUnit, UnknownReason } from "@bindings/github.com/SisyphusSQ/codex-pulse/internal/query/models";
import type { ProjectItem } from "@bindings/github.com/SisyphusSQ/codex-pulse/internal/query/usagecost/models";
import { createAppI18n } from "@/i18n";

import ProjectsList from "./ProjectsList.vue";

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
const totals = (value: number | null) => ({
  cachedInputTokens: value === null ? unknown() : known(0),
  estimatedUsdMicros: value === null ? unknown(NumericUnit.NumericMicroUSD) : known(value, NumericUnit.NumericMicroUSD),
  firstActivityAtMs: known(Date.parse("2026-07-16T06:00:00Z"), NumericUnit.NumericMilliseconds),
  inputTokens: value === null ? unknown() : known(0),
  lastActivityAtMs: value === null ? unknown(NumericUnit.NumericMilliseconds) : known(Date.parse("2026-07-16T07:00:00Z"), NumericUnit.NumericMilliseconds),
  outputTokens: value === null ? unknown() : known(0),
  pricedTurnCount: known(0, NumericUnit.NumericCount),
  reasoningTokens: value === null ? unknown() : known(0),
  totalTokens: value === null ? unknown() : known(0),
  turnCount: known(0, NumericUnit.NumericCount),
  unpricedTurnCount: known(0, NumericUnit.NumericCount),
});

const items: ProjectItem[] = [
  {
    dimensionKey: "project-secret-a",
    project: {
      confidence: "high",
      displayName: "Codex Pulse",
      id: "project-secret-a",
      reason: "root_matched",
      source: "registered_root",
    },
    sessionCount: known(0, NumericUnit.NumericCount),
    totals: totals(0),
    trend: [],
  },
  {
    dimensionKey: "unknown|unknown|missing|missing",
    project: {
      confidence: "unknown",
      displayName: null,
      id: null,
      reason: "missing",
      source: "missing",
    },
    sessionCount: known(3, NumericUnit.NumericCount),
    totals: totals(null),
    trend: [],
  },
];

describe("ProjectsList", () => {
  it("renders server order, exact Session count, real zero, and unknown without opaque identities", async () => {
    const wrapper = mount(ProjectsList, {
      props: {
        items,
        selectedProjectKey: "project-secret-a",
        timeZone: "Asia/Shanghai",
      },
      global: {
        plugins: [createAppI18n()],
        stubs: { ProjectTrendChart: { template: "<div data-testid='project-trend' />" } },
      },
    });

    const rows = wrapper.findAll("[data-testid='project-row']");
    expect(rows).toHaveLength(2);
    expect(rows[0].attributes("aria-pressed")).toBe("true");
    expect(rows[0].text()).toContain("Codex Pulse");
    expect(rows[0].text()).toContain("0 个会话");
    expect(rows[0].text()).toContain("$0.00");
    expect(rows[1].text()).toContain("未归因项目");
    expect(rows[1].text()).toContain("--");
    expect(wrapper.text()).toContain("高可信");
    expect(wrapper.text()).toContain("归因不足");
    expect(wrapper.html()).not.toContain("project-secret-a");
    expect(wrapper.html()).not.toContain("unknown|unknown|missing|missing");

    expect(rows[0].element.tagName).toBe("BUTTON");
    expect(rows[0].attributes("type")).toBe("button");
    await rows[1].trigger("keydown", { key: "Enter" });
    await rows[1].trigger("click");
    await rows[0].trigger("keydown", { key: " " });
    await rows[0].trigger("click");
    expect(wrapper.emitted("select")).toEqual([
      ["unknown|unknown|missing|missing"],
      ["project-secret-a"],
    ]);
  });
});
