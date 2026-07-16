import { NumericUnit } from "@bindings/github.com/SisyphusSQ/codex-pulse/internal/query/models";
import type { TrendPoint } from "@bindings/github.com/SisyphusSQ/codex-pulse/internal/query/usagecost/models";
import { describe, expect, it } from "vitest";

import { createUsageTrendOption } from "./chartOptions";

const known = (value: number) => ({ value, unit: NumericUnit.NumericTokens, unknownReason: null });

describe("overview ECharts option", () => {
  it("maps backend trend points to four accessible series", () => {
    const point = {
      key: "2026-07-16",
      startAtMs: known(Date.parse("2026-07-16T00:00:00Z")),
      endAtMs: known(Date.parse("2026-07-17T00:00:00Z")),
      totals: {
        turnCount: { value: 2, unit: NumericUnit.NumericCount, unknownReason: null },
        inputTokens: known(10),
        cachedInputTokens: known(8),
        outputTokens: known(4),
        reasoningTokens: known(2),
        totalTokens: known(24),
        estimatedUsdMicros: { value: 10_000, unit: NumericUnit.NumericMicroUSD, unknownReason: null },
        pricedTurnCount: { value: 2, unit: NumericUnit.NumericCount, unknownReason: null },
        unpricedTurnCount: { value: 0, unit: NumericUnit.NumericCount, unknownReason: null },
        firstActivityAtMs: known(Date.parse("2026-07-16T01:00:00Z")),
        lastActivityAtMs: known(Date.parse("2026-07-16T02:00:00Z")),
      },
    } satisfies TrendPoint;

    const option = createUsageTrendOption([point], {
      cached: "Cached",
      input: "Input",
      output: "Output",
      reasoning: "Reasoning",
      unit: "Token",
    }, "Asia/Shanghai", false);

    expect(option.aria).toMatchObject({ enabled: true, decal: { show: true } });
    expect(option.animation).toBe(true);
    expect(option.xAxis).toMatchObject({ data: ["7/16"] });
    expect(option.series).toHaveLength(4);
    const series = option.series as Array<{ name?: string }>;
    expect(series.map((item) => item.name)).toEqual([
      "Input",
      "Cached",
      "Output",
      "Reasoning",
    ]);
    const tooltip = option.tooltip as { valueFormatter: (value: unknown) => string };
    expect(tooltip.valueFormatter(null)).toBe("--");
    expect(tooltip.valueFormatter(1_234)).toBe("1,234 Token");
  });

  it("disables chart animation for reduced motion", () => {
    expect(createUsageTrendOption([], {
      cached: "Cached",
      input: "Input",
      output: "Output",
      reasoning: "Reasoning",
      unit: "Token",
    }, "Asia/Shanghai", true).animation).toBe(false);
  });
});
