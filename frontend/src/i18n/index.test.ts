import { describe, expect, it, vi } from "vitest";

import { createAppI18n, supportedLocale } from "./index";

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
});
