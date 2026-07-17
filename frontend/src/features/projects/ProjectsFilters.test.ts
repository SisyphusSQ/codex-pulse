import { mount } from "@vue/test-utils";
import { describe, expect, it } from "vitest";

import { createAppI18n } from "@/i18n";

import { defaultProjectsRouteState } from "./routeState";
import ProjectsFilters from "./ProjectsFilters.vue";

describe("ProjectsFilters", () => {
  it("emits only finite range, confidence, sort, and direction patches with accessible labels", async () => {
    const wrapper = mount(ProjectsFilters, {
      props: { state: defaultProjectsRouteState },
      global: { plugins: [createAppI18n()] },
    });

    expect(wrapper.get("button[data-range='7d']").attributes("aria-pressed")).toBe("true");
    await wrapper.get("button[data-range='30d']").trigger("click");
    await wrapper.get("[data-testid='projects-confidence']").setValue("low");
    await wrapper.get("[data-testid='projects-sort']").setValue("totalTokens");
    await wrapper.get("[data-testid='projects-direction']").trigger("click");

    expect(wrapper.emitted("change")).toEqual([
      [{ endDateExclusive: null, range: "30d", startDate: null }],
      [{ confidence: "low" }],
      [{ sort: "totalTokens" }],
      [{ direction: "asc" }],
    ]);
    expect(wrapper.get("form").attributes("aria-label")).toBe("项目筛选与排序");
  });

  it("passes a custom half-open local date range without inventing timezone conversion", async () => {
    const wrapper = mount(ProjectsFilters, {
      props: {
        state: {
          ...defaultProjectsRouteState,
          endDateExclusive: "2026-07-17",
          range: "custom",
          startDate: "2026-07-01",
        },
      },
      global: { plugins: [createAppI18n()] },
    });

    await wrapper.get("[data-testid='apply-custom-range']").trigger("click");
    expect(wrapper.emitted("change")?.at(-1)).toEqual([{
      endDateExclusive: "2026-07-17",
      range: "custom",
      startDate: "2026-07-01",
    }]);
  });
});
