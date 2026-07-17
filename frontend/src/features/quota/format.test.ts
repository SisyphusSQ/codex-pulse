import { describe, expect, it } from "vitest";

import {
  formatQuotaPercent,
  quotaProgressValue,
  resetCountdown,
} from "./format";

describe("Quota presentation formatting", () => {
  it("keeps unknown and real zero quota values distinct", () => {
    expect(formatQuotaPercent(null)).toBe("--");
    expect(quotaProgressValue(null)).toBeNull();
    expect(formatQuotaPercent(0)).toBe("0%");
    expect(quotaProgressValue(0)).toBe(0);
    expect(quotaProgressValue(130)).toBe(100);
    expect(quotaProgressValue(-10)).toBe(0);
  });

  it("derives a display-only reset countdown from absolute DTO time", () => {
    const now = Date.parse("2026-07-17T08:00:00Z");
    expect(resetCountdown(null, now)).toBeNull();
    expect(resetCountdown(now - 1, now)).toEqual({ unit: "minute", value: 0 });
    expect(resetCountdown(now + 59_001, now)).toEqual({ unit: "minute", value: 1 });
    expect(resetCountdown(now + 2 * 60 * 60_000, now)).toEqual({ unit: "hour", value: 2 });
    expect(resetCountdown(now + 3 * 24 * 60 * 60_000, now)).toEqual({ unit: "day", value: 3 });
  });
});
