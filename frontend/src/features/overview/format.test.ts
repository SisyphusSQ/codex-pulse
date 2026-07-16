import { NumericUnit, UnknownReason } from "@bindings/github.com/SisyphusSQ/codex-pulse/internal/query/models";
import { QuotaCurrentFreshness, SourceFreshness } from "@bindings/github.com/SisyphusSQ/codex-pulse/internal/store/models";
import { describe, expect, it } from "vitest";

import {
  formatCompactTokens,
  formatCount,
  formatDateTime,
  formatMicroUSD,
  formatPercent,
  formatQuotaWindowFreshness,
  formatSourceFreshness,
  numericValue,
} from "./format";

describe("overview DTO formatting", () => {
  it("keeps real zero distinct from unknown", () => {
    expect(numericValue({ value: 0, unit: NumericUnit.NumericTokens, unknownReason: null })).toBe(0);
    expect(numericValue({
      value: null,
      unit: NumericUnit.NumericTokens,
      unknownReason: UnknownReason.UnknownNeverLoaded,
    })).toBeNull();
  });

  it("formats server totals without recomputing aggregates", () => {
    expect(formatCompactTokens({
      value: 62_800_000,
      unit: NumericUnit.NumericTokens,
      unknownReason: null,
    })).not.toBe("--");
    expect(formatMicroUSD({
      value: 48_910_000,
      unit: NumericUnit.NumericMicroUSD,
      unknownReason: null,
    })).toContain("48.91");
    expect(formatMicroUSD({
      value: null,
      unit: NumericUnit.NumericMicroUSD,
      unknownReason: UnknownReason.UnknownNotComputed,
    })).toBe("--");
  });

  it("formats remaining percent while preserving exhausted zero", () => {
    expect(formatPercent(0)).toBe("0%");
    expect(formatPercent(55.4)).toBe("55%");
    expect(formatPercent(null)).toBe("--");
  });

  it("formats typed counts and timestamps without exposing unknown values", () => {
    expect(formatCount({
      value: 1_234,
      unit: NumericUnit.NumericCount,
      unknownReason: null,
    })).toContain("1,234");
    expect(formatDateTime(Date.parse("2026-07-16T00:00:00Z"), "Asia/Shanghai"))
      .toContain("2026");
    expect(formatDateTime(null, "Asia/Shanghai")).toBe("--");
  });

  it("keeps quota-window and source freshness contracts distinct", () => {
    const labels = {
      current: "current",
      lastKnown: "last-known",
      stale: "stale",
      unavailable: "unavailable",
      unknown: "unknown",
    };

    expect(formatQuotaWindowFreshness(QuotaCurrentFreshness.QuotaCurrentFresh, labels))
      .toBe("current");
    expect(formatQuotaWindowFreshness(QuotaCurrentFreshness.QuotaCurrentStale, labels))
      .toBe("last-known");
    expect(formatQuotaWindowFreshness(QuotaCurrentFreshness.QuotaCurrentNeverLoaded, labels))
      .toBe("unknown");
    expect(formatSourceFreshness(SourceFreshness.SourceFreshnessCurrent, labels))
      .toBe("current");
    expect(formatSourceFreshness(SourceFreshness.SourceFreshnessUnavailable, labels))
      .toBe("unavailable");
  });
});
