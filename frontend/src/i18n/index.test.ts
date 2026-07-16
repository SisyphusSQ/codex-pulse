import { describe, expect, it, vi } from "vitest";

import {
  createAppFormatters,
  createAppI18n,
  supportedLocale,
} from "./index";

describe("createAppI18n", () => {
  it("uses zh-CN as both active and fallback locale", () => {
    const i18n = createAppI18n();

    expect(i18n.global.locale.value).toBe(supportedLocale);
    expect(i18n.global.fallbackLocale.value).toBe(supportedLocale);
  });

  it("reports missing message keys without inventing a translation", () => {
    const onMissing = vi.fn();
    const i18n = createAppI18n({ onMissing });

    expect(i18n.global.t("missing.key")).toBe("missing.key");
    expect(onMissing).toHaveBeenCalledWith(supportedLocale, "missing.key");
  });

  it("formats numbers, dates, and relative times with the only bundled locale", () => {
    const formatters = createAppFormatters({
      now: () => Date.parse("2026-07-16T08:00:00Z"),
      timeZone: "Asia/Shanghai",
    });

    expect(formatters.number(12_345.5)).toBe("12,345.5");
    expect(formatters.dateTime(Date.parse("2026-07-16T07:30:00Z"))).toContain("2026");
    expect(formatters.relativeTime(Date.parse("2026-07-16T08:02:00Z"))).toMatch(/2.*分钟.*后/);
  });
});
