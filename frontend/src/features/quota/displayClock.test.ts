import { mount } from "@vue/test-utils";
import { defineComponent, h } from "vue";
import { afterEach, describe, expect, it, vi } from "vitest";

import { useQuotaDisplayClock } from "./displayClock";

describe("Quota display clock", () => {
  afterEach(() => vi.useRealTimers());

  it("ticks once per second for display only and clears its timer on unmount", async () => {
    vi.useFakeTimers();
    vi.setSystemTime("2026-07-17T08:00:00.000Z");
    const Harness = defineComponent({
      setup() {
        const nowMS = useQuotaDisplayClock();
        return () => h("p", String(nowMS.value));
      },
    });
    const wrapper = mount(Harness);

    expect(wrapper.text()).toBe(String(Date.parse("2026-07-17T08:00:00.000Z")));
    await vi.advanceTimersByTimeAsync(1_000);
    expect(wrapper.text()).toBe(String(Date.parse("2026-07-17T08:00:01.000Z")));

    wrapper.unmount();
    expect(vi.getTimerCount()).toBe(0);
  });
});
