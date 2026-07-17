import { flushPromises, mount } from "@vue/test-utils";
import { computed, defineComponent, h } from "vue";
import { afterEach, describe, expect, it, vi } from "vitest";

import { createOverviewPresetRange } from "@/features/overview/range";

import { useLocalDateClock } from "./localDateClock";

describe("Sessions local date clock", () => {
  afterEach(() => {
    vi.useRealTimers();
  });

  it("advances preset request boundaries at local midnight and disposes its one-shot timer", async () => {
    vi.useFakeTimers();
    vi.setSystemTime("2026-07-16T15:59:59.000Z");
    const Harness = defineComponent({
      setup() {
        const nowMS = useLocalDateClock("Asia/Shanghai");
        const range = computed(() => createOverviewPresetRange(
          "today",
          nowMS.value,
          "Asia/Shanghai",
        ));
        return () => h("p", `${range.value.startDate}/${range.value.endDateExclusive}`);
      },
    });
    const wrapper = mount(Harness);

    expect(wrapper.text()).toBe("2026-07-16/2026-07-17");
    await vi.advanceTimersByTimeAsync(1_000);
    await flushPromises();
    expect(wrapper.text()).toBe("2026-07-17/2026-07-18");

    wrapper.unmount();
    expect(vi.getTimerCount()).toBe(0);
  });
});
