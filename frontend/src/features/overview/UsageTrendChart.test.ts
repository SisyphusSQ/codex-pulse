import { mount } from "@vue/test-utils";
import { beforeEach, describe, expect, it, vi } from "vitest";

const echartsHarness = vi.hoisted(() => ({
  dispose: vi.fn(),
  init: vi.fn(),
  resize: vi.fn(),
  setOption: vi.fn(),
  use: vi.fn(),
}));

vi.mock("echarts/core", () => ({
  init: echartsHarness.init,
  use: echartsHarness.use,
}));
vi.mock("echarts/charts", () => ({ BarChart: {} }));
vi.mock("echarts/components", () => ({
  AriaComponent: {},
  GridComponent: {},
  LegendComponent: {},
  TooltipComponent: {},
}));
vi.mock("echarts/renderers", () => ({ CanvasRenderer: {} }));

import UsageTrendChart from "./UsageTrendChart.vue";

describe("UsageTrendChart", () => {
  beforeEach(() => {
    echartsHarness.dispose.mockReset();
    echartsHarness.init.mockReset();
    echartsHarness.resize.mockReset();
    echartsHarness.setOption.mockReset();
    echartsHarness.init.mockReturnValue({
      dispose: echartsHarness.dispose,
      resize: echartsHarness.resize,
      setOption: echartsHarness.setOption,
    });
    vi.stubGlobal("matchMedia", vi.fn(() => ({
      addEventListener: vi.fn(),
      matches: false,
      removeEventListener: vi.fn(),
    })));
  });

  it("initializes the modular canvas chart and disposes it on unmount", () => {
    const wrapper = mount(UsageTrendChart, {
      props: {
        label: "Usage trend",
        labels: { cached: "Cached", input: "Input", output: "Output", reasoning: "Reasoning", unit: "Token" },
        points: [],
        timeZone: "Asia/Shanghai",
      },
    });

    expect(echartsHarness.init).toHaveBeenCalledOnce();
    expect(echartsHarness.setOption).toHaveBeenCalledOnce();
    expect(wrapper.attributes("role")).toBe("img");
    expect(wrapper.attributes("aria-label")).toBe("Usage trend");

    wrapper.unmount();
    expect(echartsHarness.dispose).toHaveBeenCalledOnce();
  });
});
