import type { RouterHistory, RouteRecordRaw } from "vue-router";
import { createRouter, createWebHistory } from "vue-router";

import AppShell from "@/components/shell/AppShell.vue";
import ShellView from "@/views/ShellView.vue";

export type AppNavigationName =
  | "overview"
  | "sessions"
  | "projects"
  | "quota"
  | "local-status"
  | "settings";

interface AppNavigationItem {
  descriptionKey: string;
  labelKey: string;
  name: AppNavigationName;
  path: string;
  titleKey: string;
}

export const appNavigation = [
  { name: "overview", path: "/overview", labelKey: "nav.overview", titleKey: "routes.overview.title", descriptionKey: "routes.overview.description" },
  { name: "sessions", path: "/sessions", labelKey: "nav.sessions", titleKey: "routes.sessions.title", descriptionKey: "routes.sessions.description" },
  { name: "projects", path: "/projects", labelKey: "nav.projects", titleKey: "routes.projects.title", descriptionKey: "routes.projects.description" },
  { name: "quota", path: "/quota", labelKey: "nav.quota", titleKey: "routes.quota.title", descriptionKey: "routes.quota.description" },
  { name: "local-status", path: "/local-status", labelKey: "nav.localStatus", titleKey: "routes.localStatus.title", descriptionKey: "routes.localStatus.description" },
  { name: "settings", path: "/settings", labelKey: "nav.settings", titleKey: "routes.settings.title", descriptionKey: "routes.settings.description" },
] as const satisfies readonly AppNavigationItem[];

const childRoutes = appNavigation.map((item) => ({
  path: item.path.slice(1),
  name: item.name,
  component: ShellView,
  meta: {
    descriptionKey: item.descriptionKey,
    titleKey: item.titleKey,
  },
})) satisfies RouteRecordRaw[];

const routes = [
  {
    path: "/",
    component: AppShell,
    children: [
      { path: "", redirect: { name: "overview" } },
      ...childRoutes,
    ],
  },
  {
    path: "/:pathMatch(.*)*",
    redirect: { name: "overview" },
  },
] satisfies RouteRecordRaw[];

export function createAppRouter(history: RouterHistory = createWebHistory()) {
  return createRouter({ history, routes });
}
