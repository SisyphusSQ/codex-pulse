import { flushPromises, mount } from "@vue/test-utils";
import { defineComponent, h, ref } from "vue";
import { createMemoryHistory, createRouter } from "vue-router";
import { describe, expect, it } from "vitest";

import { createAppI18n } from "@/i18n";

import RouteViewBoundary from "./RouteViewBoundary.vue";

function throwingRoute(shouldThrow: ReturnType<typeof ref<boolean>>) {
  return defineComponent({
    name: "ThrowingRoute",
    setup() {
      if (shouldThrow.value) throw new Error("private route cause");
      return () => h("p", { "data-testid": "recovered-route" }, "recovered");
    },
  });
}

describe("RouteViewBoundary", () => {
  it("isolates a route error and retries without exposing the raw cause", async () => {
    const shouldThrow = ref(true);
    const router = createRouter({
      history: createMemoryHistory(),
      routes: [{ path: "/broken", component: throwingRoute(shouldThrow) }],
    });
    await router.push("/broken");
    const wrapper = mount(RouteViewBoundary, { global: { plugins: [router, createAppI18n()] } });
    await router.isReady();

    expect(wrapper.get("[data-testid='route-error']").attributes("role")).toBe("alert");
    expect(wrapper.text()).not.toContain("private route cause");
    shouldThrow.value = false;
    await wrapper.get("[data-testid='route-error-retry']").trigger("click");
    await flushPromises();
    expect(wrapper.find("[data-testid='recovered-route']").exists()).toBe(true);
  });

  it("clears a failed route when navigation reaches another page", async () => {
    const shouldThrow = ref(true);
    const router = createRouter({
      history: createMemoryHistory(),
      routes: [
        { path: "/broken", component: throwingRoute(shouldThrow) },
        { path: "/healthy", component: { template: "<p data-testid='healthy-route'>healthy</p>" } },
      ],
    });
    await router.push("/broken");
    const wrapper = mount(RouteViewBoundary, { global: { plugins: [router, createAppI18n()] } });
    await router.isReady();
    expect(wrapper.find("[data-testid='route-error']").exists()).toBe(true);

    await router.push("/healthy");
    await flushPromises();
    expect(wrapper.find("[data-testid='route-error']").exists()).toBe(false);
    expect(wrapper.find("[data-testid='healthy-route']").exists()).toBe(true);
  });

  it("does not remount a healthy page for query-only navigation", async () => {
    let mounts = 0;
    const stable = defineComponent({
      name: "StableRoute",
      setup() {
        mounts++;
        return () => h("p", { "data-testid": "stable-route" }, "stable");
      },
    });
    const router = createRouter({
      history: createMemoryHistory(),
      routes: [{ path: "/stable", name: "stable", component: stable }],
    });
    await router.push("/stable?cursor=first");
    const wrapper = mount(RouteViewBoundary, { global: { plugins: [router, createAppI18n()] } });
    await router.isReady();
    expect(wrapper.find("[data-testid='stable-route']").exists()).toBe(true);

    await router.push("/stable?cursor=second");
    await flushPromises();
    expect(mounts).toBe(1);
  });
});
