import { describe, expect, it } from "vitest";

import {
  createCustomOverviewRange,
  createOverviewPresetRange,
} from "./range";

describe("overview local date ranges", () => {
  it("builds today, seven-day, and thirty-day local half-open ranges", () => {
    const nowMS = Date.parse("2026-07-16T18:30:00Z");

    expect(createOverviewPresetRange("today", nowMS, "Asia/Shanghai")).toEqual({
      startDate: "2026-07-17",
      endDateExclusive: "2026-07-18",
      timeZone: "Asia/Shanghai",
    });
    expect(createOverviewPresetRange("7d", nowMS, "Asia/Shanghai")).toEqual({
      startDate: "2026-07-11",
      endDateExclusive: "2026-07-18",
      timeZone: "Asia/Shanghai",
    });
    expect(createOverviewPresetRange("30d", nowMS, "Asia/Shanghai")).toEqual({
      startDate: "2026-06-18",
      endDateExclusive: "2026-07-18",
      timeZone: "Asia/Shanghai",
    });
  });

  it("keeps DST dates as calendar boundaries instead of fixed 24-hour offsets", () => {
    const nowMS = Date.parse("2026-03-08T12:00:00Z");

    expect(createOverviewPresetRange("today", nowMS, "America/Los_Angeles")).toEqual({
      startDate: "2026-03-08",
      endDateExclusive: "2026-03-09",
      timeZone: "America/Los_Angeles",
    });
  });

  it("validates custom ranges and preserves the requested IANA timezone", () => {
    expect(createCustomOverviewRange("2026-07-01", "2026-07-17", "Asia/Shanghai")).toEqual({
      startDate: "2026-07-01",
      endDateExclusive: "2026-07-17",
      timeZone: "Asia/Shanghai",
    });
    expect(() => createCustomOverviewRange("2026-07-17", "2026-07-17", "Asia/Shanghai"))
      .toThrow("overview range");
    expect(() => createCustomOverviewRange("2025-01-01", "2026-07-17", "Asia/Shanghai"))
      .toThrow("overview range");
    expect(() => createCustomOverviewRange("2026-07-01", "2026-07-17", "Local"))
      .toThrow("overview range");
  });
});
