import { mount } from "@vue/test-utils";
import { describe, expect, it } from "vitest";

import OverviewRangePicker from "./OverviewRangePicker.vue";

describe("OverviewRangePicker", () => {
  it("emits preset changes and exposes an accessible pressed state", async () => {
    const wrapper = mount(OverviewRangePicker, {
      props: {
        applyLabel: "Apply",
        customLabel: "Custom",
        endLabel: "End",
        modelValue: "7d",
        presetLabels: { today: "Today", "7d": "7 days", "30d": "30 days" },
        startLabel: "Start",
      },
    });

    expect(wrapper.get("button[data-range='7d']").attributes("aria-pressed")).toBe("true");
    await wrapper.get("button[data-range='30d']").trigger("click");
    expect(wrapper.emitted("update:modelValue")?.at(-1)).toEqual(["30d"]);
  });

  it("collects custom dates and emits apply without changing their meaning", async () => {
    const wrapper = mount(OverviewRangePicker, {
      props: {
        applyLabel: "Apply",
        customLabel: "Custom",
        endLabel: "End",
        modelValue: "custom",
        presetLabels: { today: "Today", "7d": "7 days", "30d": "30 days" },
        startLabel: "Start",
        customStart: "2026-07-01",
        customEndExclusive: "2026-07-17",
      },
    });

    await wrapper.get("button[data-range='custom']").trigger("click");
    await wrapper.get("button[data-testid='apply-custom-range']").trigger("click");
    expect(wrapper.emitted("applyCustom")?.at(-1)).toEqual([
      { startDate: "2026-07-01", endDateExclusive: "2026-07-17" },
    ]);
  });
});
