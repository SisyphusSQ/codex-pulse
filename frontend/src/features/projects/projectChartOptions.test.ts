import { describe, expect, it } from "vitest";

import { NumericUnit, UnknownReason } from "@bindings/github.com/SisyphusSQ/codex-pulse/internal/query/models";
import type { ProjectDailyPoint } from "@bindings/github.com/SisyphusSQ/codex-pulse/internal/query/usagecost/models";

import { createProjectTrendOption } from "./projectChartOptions";

const known = (value: number, unit = NumericUnit.NumericTokens) => ({ value, unit, unknownReason: null });
const unknown = (unit = NumericUnit.NumericTokens) => ({ value: null, unit, unknownReason: UnknownReason.UnknownUnavailable });
const totals = (tokens: number | null) => ({
  cachedInputTokens: tokens === null ? unknown() : known(0),
  estimatedUsdMicros: tokens === null ? unknown(NumericUnit.NumericMicroUSD) : known(0, NumericUnit.NumericMicroUSD),
  firstActivityAtMs: known(1, NumericUnit.NumericMilliseconds),
  inputTokens: tokens === null ? unknown() : known(tokens),
  lastActivityAtMs: known(2, NumericUnit.NumericMilliseconds),
  outputTokens: tokens === null ? unknown() : known(0),
  pricedTurnCount: known(0, NumericUnit.NumericCount),
  reasoningTokens: tokens === null ? unknown() : known(0),
  totalTokens: tokens === null ? unknown() : known(tokens),
  turnCount: known(0, NumericUnit.NumericCount),
  unpricedTurnCount: known(0, NumericUnit.NumericCount),
});

describe("Project trend chart option", () => {
  it("preserves provider bucket order and unknown gaps without filling missing days", () => {
    const points: ProjectDailyPoint[] = [
      {
        bucketStartAtMs: known(Date.parse("2026-07-14T16:00:00Z"), NumericUnit.NumericMilliseconds),
        confidence: "high",
        reason: "root_matched",
        source: "registered_root",
        totals: totals(12),
      },
      {
        bucketStartAtMs: known(Date.parse("2026-07-16T16:00:00Z"), NumericUnit.NumericMilliseconds),
        confidence: "unknown",
        reason: "missing",
        source: "missing",
        totals: totals(null),
      },
    ];

    const option = createProjectTrendOption(points, "Asia/Shanghai", false, false, "Token");
    expect(option.xAxis).toMatchObject({ data: ["7/15", "7/17"] });
    expect(option.series).toEqual([expect.objectContaining({ data: [12, null], type: "line" })]);
    expect(option.animation).toBe(true);
    expect((option.tooltip as { valueFormatter: (value: unknown) => string }).valueFormatter(null)).toBe("--");
  });
});
