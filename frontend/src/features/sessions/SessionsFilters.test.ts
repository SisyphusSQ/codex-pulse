import { mount } from "@vue/test-utils";
import { describe, expect, it } from "vitest";

import { createAppI18n } from "@/i18n";

import { defaultSessionsRouteState } from "./routeState";
import SessionsFilters from "./SessionsFilters.vue";

describe("SessionsFilters", () => {
  it("emits only finite server filter and sort patches with accessible labels", async () => {
    const wrapper = mount(SessionsFilters, {
      props: {
        modelOptions: [{ label: "GPT-5", value: "model-safe-key" }],
        projectOptions: [{ label: "Codex Pulse", value: "project-safe-id" }],
        state: defaultSessionsRouteState,
      },
      global: { plugins: [createAppI18n()] },
    });

    expect(wrapper.get("[data-testid='sessions-activity-active']").attributes("aria-pressed")).toBe("false");
    await wrapper.get("[data-testid='sessions-activity-active']").trigger("click");
    await wrapper.get("[data-testid='sessions-range']").setValue("7d");
    await wrapper.get("[data-testid='sessions-project']").setValue("project-safe-id");
    await wrapper.get("[data-testid='sessions-model']").setValue("model-safe-key");
    await wrapper.get("[data-testid='sessions-sort']").setValue("estimatedCost");
    await wrapper.get("[data-testid='sessions-direction']").trigger("click");

    expect(wrapper.emitted("change")).toEqual([
      [{ activity: "active" }],
      [{ range: "7d" }],
      [{ projectId: "project-safe-id" }],
      [{ modelKey: "model-safe-key" }],
      [{ sort: "estimatedCost" }],
      [{ direction: "asc" }],
    ]);
    expect(wrapper.text()).toContain("项目");
    expect(wrapper.text()).toContain("模型");
    expect(wrapper.text()).not.toContain("project-safe-id");
    expect(wrapper.text()).not.toContain("model-safe-key");
  });
});
