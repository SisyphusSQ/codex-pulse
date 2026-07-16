import { mount } from "@vue/test-utils";
import { describe, expect, it } from "vitest";

import OverviewRegion from "./OverviewRegion.vue";

const copy = {
  actionLabel: "Retry",
  emptyDescription: "No facts",
  emptyTitle: "Empty",
  errorDescription: "Unavailable",
  errorTitle: "Error",
  loadingLabel: "Loading",
  partialLabel: "Partial",
  staleLabel: "Stale",
  title: "Usage",
};

describe("OverviewRegion", () => {
  it("shows initial loading, fatal error, and known empty independently", async () => {
    const loading = mount(OverviewRegion, { props: { ...copy, isPending: true } });
    expect(loading.find("[role='status']").exists()).toBe(true);

    const error = mount(OverviewRegion, { props: { ...copy, isError: true } });
    expect(error.find("[role='alert']").text()).toContain("Unavailable");
    await error.get("button").trigger("click");
    expect(error.emitted("retry")).toHaveLength(1);

    const empty = mount(OverviewRegion, { props: { ...copy, hasData: true, isEmpty: true } });
    expect(empty.find("[role='status']").text()).toContain("No facts");
  });

  it("keeps ready data visible while marking partial, stale, and refreshing", () => {
    const wrapper = mount(OverviewRegion, {
      props: {
        ...copy,
        hasData: true,
        isError: true,
        isFetching: true,
        isPartial: true,
        isStale: true,
      },
      slots: { default: "trusted facts" },
    });

    expect(wrapper.text()).toContain("trusted facts");
    expect(wrapper.text()).toContain("Partial");
    expect(wrapper.text()).toContain("Stale");
    expect(wrapper.attributes("aria-busy")).toBe("true");
  });

  it("supports source-matched bare ready content without an extra card heading", () => {
    const wrapper = mount(OverviewRegion, {
      props: { ...copy, bare: true, hasData: true },
      slots: { default: "compact quota facts" },
    });

    expect(wrapper.text()).toContain("compact quota facts");
    expect(wrapper.find("h2").exists()).toBe(false);
    expect(wrapper.attributes("aria-label")).toBe("Usage");
  });

  it("keeps partial truth visible when the available collection is empty", () => {
    const wrapper = mount(OverviewRegion, {
      props: { ...copy, hasData: true, isEmpty: true, isPartial: true },
    });

    expect(wrapper.text()).toContain("Partial");
    expect(wrapper.text()).toContain("No facts");
  });
});
