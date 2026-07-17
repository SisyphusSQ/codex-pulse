import { mount } from "@vue/test-utils";
import { beforeEach, describe, expect, it, vi } from "vitest";

const echartsHarness = vi.hoisted(() => ({
  dispose: vi.fn(),
  init: vi.fn(),
  resize: vi.fn(),
  setOption: vi.fn(),
  use: vi.fn(),
}));

vi.mock("echarts/core", () => ({ init: echartsHarness.init, use: echartsHarness.use }));
vi.mock("echarts/charts", () => ({ LineChart: {} }));
vi.mock("echarts/components", () => ({ AriaComponent: {}, GridComponent: {}, TooltipComponent: {} }));
vi.mock("echarts/renderers", () => ({ CanvasRenderer: {} }));

import ProjectTrendChart from "./ProjectTrendChart.vue";

describe("ProjectTrendChart", () => {
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

  it("initializes the modular canvas chart, exposes a label, and disposes on unmount", () => {
    const wrapper = mount(ProjectTrendChart, {
      props: {
        label: "Project trend",
        points: [],
        timeZone: "Asia/Shanghai",
        unitLabel: "Token",
      },
    });

    expect(echartsHarness.init).toHaveBeenCalledOnce();
    expect(echartsHarness.setOption).toHaveBeenCalledOnce();
    expect(wrapper.attributes("role")).toBe("img");
    expect(wrapper.attributes("aria-label")).toBe("Project trend");
    wrapper.unmount();
    expect(echartsHarness.dispose).toHaveBeenCalledOnce();
  });
});
