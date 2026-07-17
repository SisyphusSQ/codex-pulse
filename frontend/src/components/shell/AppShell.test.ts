import { flushPromises, mount } from "@vue/test-utils";
import { createMemoryHistory, createRouter } from "vue-router";
import { afterEach, describe, expect, it } from "vitest";

import { createAppI18n } from "@/i18n";

import AppShell from "./AppShell.vue";

function testRouter() {
  const component = { template: "<p>content</p>" };
  return createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: "/overview", name: "overview", component, meta: { titleKey: "routes.overview.title", descriptionKey: "routes.overview.description" } },
      { path: "/sessions", name: "sessions", component, meta: { titleKey: "routes.sessions.title", descriptionKey: "routes.sessions.description" } },
      { path: "/projects", name: "projects", component }, { path: "/quota", name: "quota", component },
      { path: "/local-status", name: "local-status", component }, { path: "/settings", name: "settings", component },
    ],
  });
}

describe("AppShell keyboard and route recovery", () => {
  afterEach(() => document.body.replaceChildren());

  it("provides a skip link and moves focus to the new route title while resetting scroll", async () => {
    const router = testRouter();
    await router.push("/overview");
    const wrapper = mount(AppShell, {
      attachTo: document.body,
      global: {
        plugins: [router, createAppI18n()],
        stubs: { AppStatusBanner: true },
      },
    });
    await router.isReady();

    const content = wrapper.get("[data-testid='app-content']");
    await wrapper.get("[data-testid='skip-to-content']").trigger("click");
    expect(document.activeElement).toBe(content.element);

    (content.element as HTMLElement).scrollTop = 160;
    await router.push("/sessions");
    await flushPromises();
    expect((content.element as HTMLElement).scrollTop).toBe(0);
    expect(document.activeElement).toBe(wrapper.get("[data-testid='route-title']").element);
    expect(wrapper.get("[data-testid='route-title']").text()).toBe("会话");
  });

  it("preserves page focus and scroll for query-only navigation", async () => {
    const router = testRouter();
    await router.push("/overview?range=7d");
    const wrapper = mount(AppShell, {
      attachTo: document.body,
      global: { plugins: [router, createAppI18n()], stubs: { AppStatusBanner: true } },
    });
    await router.isReady();
    const content = wrapper.get("[data-testid='app-content']");
    (content.element as HTMLElement).scrollTop = 160;
    (content.element as HTMLElement).focus();

    await router.push("/overview?range=30d");
    await flushPromises();
    expect((content.element as HTMLElement).scrollTop).toBe(160);
    expect(document.activeElement).toBe(content.element);
  });

  it("supports Arrow and Home/End focus movement across the primary navigation", async () => {
    const router = testRouter();
    await router.push("/overview");
    const wrapper = mount(AppShell, {
      attachTo: document.body,
      global: { plugins: [router, createAppI18n()], stubs: { AppStatusBanner: true } },
    });
    const links = wrapper.findAll("[data-testid='primary-navigation'] a");
    (links[0].element as HTMLElement).focus();
    await links[0].trigger("keydown", { key: "ArrowDown" });
    expect(document.activeElement).toBe(links[1].element);
    await links[1].trigger("keydown", { key: "End" });
    expect(document.activeElement).toBe(links.at(-1)?.element);
    await links.at(-1)!.trigger("keydown", { key: "Home" });
    expect(document.activeElement).toBe(links[0].element);
    await links[0].trigger("keydown", { key: "ArrowUp" });
    expect(document.activeElement).toBe(links.at(-1)?.element);
  });
});
