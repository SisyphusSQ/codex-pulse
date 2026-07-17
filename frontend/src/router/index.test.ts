import { createMemoryHistory } from "vue-router";
import { describe, expect, it } from "vitest";

import { appNavigation, createAppRouter } from "./index";

describe("application navigation", () => {
  it("keeps the six frozen zh-CN routes in product order", () => {
    expect(appNavigation.map(({ name, path }) => [name, path])).toEqual([
      ["overview", "/overview"],
      ["sessions", "/sessions"],
      ["projects", "/projects"],
      ["quota", "/quota"],
      ["local-status", "/local-status"],
      ["settings", "/settings"],
    ]);
  });

  it("recovers root and unknown locations to overview", async () => {
    const router = createAppRouter(createMemoryHistory());

    await router.push("/");
    expect(router.currentRoute.value.fullPath).toBe("/overview");

    await router.push("/not-a-route");
    expect(router.currentRoute.value.fullPath).toBe("/overview");
  });

  it("mounts the real Overview, Sessions, Projects, and Quota views on their owned routes", async () => {
    const router = createAppRouter(createMemoryHistory());

    await router.push("/overview");
    const overviewRecord = router.currentRoute.value.matched.at(-1);
    expect(overviewRecord?.components?.default).toMatchObject({ name: "OverviewView" });

    await router.push("/sessions");
    const sessionsRecord = router.currentRoute.value.matched.at(-1);
    expect(sessionsRecord?.components?.default).toMatchObject({ name: "SessionsView" });

    await router.push("/projects");
    const projectsRecord = router.currentRoute.value.matched.at(-1);
    expect(projectsRecord?.components?.default).toMatchObject({ name: "ProjectsView" });

    await router.push("/quota");
    const quotaRecord = router.currentRoute.value.matched.at(-1);
    expect(quotaRecord?.components?.default).toMatchObject({ name: "QuotaView" });
  });
});
