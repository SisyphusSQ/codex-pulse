import { mount } from "@vue/test-utils";
import { describe, expect, it } from "vitest";

import DataHealthTrendChart from "./DataHealthTrendChart.vue";

const numeric = (value: number, unit: string) => ({ value, unit, unknownReason: null });

describe("DataHealthTrendChart", () => {
  it("renders separate CPU and RSS typed series without exposing hidden facts", () => {
    const wrapper = mount(DataHealthTrendChart, {
      props: {
        label: "最近 24 小时 CPU 与 RSS 趋势",
        points: [
          { capturedAtMs: numeric(200, "milliseconds"), cpuPercent: 20, rssBytes: numeric(200, "bytes"), peakRssBytes: numeric(200, "bytes") },
          { capturedAtMs: numeric(100, "milliseconds"), cpuPercent: 10, rssBytes: numeric(100, "bytes"), peakRssBytes: numeric(200, "bytes") },
        ] as never,
      },
    });

    expect(wrapper.attributes("role")).toBe("img");
    expect(wrapper.attributes("aria-label")).toContain("CPU 与 RSS");
    expect(wrapper.findAll("[data-testid='data-health-cpu-point']")).toHaveLength(2);
    expect(wrapper.findAll("[data-testid='data-health-rss-point']")).toHaveLength(2);
    expect(wrapper.get("[data-testid='data-health-trend-cpu']").attributes("aria-hidden")).toBe("true");
    expect(wrapper.text()).not.toContain("200");
  });
});
