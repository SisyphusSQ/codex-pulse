import type { RouterHistory, RouteRecordRaw } from "vue-router";
import { createRouter, createWebHistory } from "vue-router";

import AppShell from "@/components/shell/AppShell.vue";
import DataHealthView from "@/views/DataHealthView.vue";
import OverviewView from "@/views/OverviewView.vue";
import ProjectsView from "@/views/ProjectsView.vue";
import QuotaView from "@/views/QuotaView.vue";
import LocalStatusView from "@/views/LocalStatusView.vue";
import SettingsView from "@/views/SettingsView.vue";
import SessionsView from "@/views/SessionsView.vue";
import PopoverView from "@/views/PopoverView.vue";

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

const desktopNavigationPaths = new Set([
  ...appNavigation.map((item) => item.path),
  "/local-status/data-health",
]);

export function normalizeDesktopNavigationPath(candidate: unknown): string {
  if (typeof candidate !== "string" || !candidate.startsWith("/") || candidate.startsWith("//")) {
    return "/overview";
  }
  try {
    const parsed = new URL(candidate, "https://codex-pulse.local");
    if (parsed.origin !== "https://codex-pulse.local" || !desktopNavigationPaths.has(parsed.pathname)) {
      return "/overview";
    }
    return `${parsed.pathname}${parsed.search}`;
  } catch {
    return "/overview";
  }
}

const childRoutes = appNavigation.map((item) => ({
  path: item.path.slice(1),
  name: item.name,
  component: item.name === "overview"
    ? OverviewView
    : item.name === "sessions"
      ? SessionsView
      : item.name === "projects"
        ? ProjectsView
        : item.name === "quota"
          ? QuotaView
          : item.name === "local-status"
            ? LocalStatusView
            : SettingsView,
  meta: {
    descriptionKey: item.descriptionKey,
    titleKey: item.titleKey,
  },
})) satisfies RouteRecordRaw[];

const routes = [
  {
    path: "/popover",
    name: "popover",
    component: PopoverView,
  },
  {
    path: "/",
    component: AppShell,
    children: [
      { path: "", redirect: { name: "overview" } },
      {
        path: "local-status/data-health",
        name: "data-health",
        component: DataHealthView,
        meta: {
          descriptionKey: "routes.dataHealth.description",
          titleKey: "routes.dataHealth.title",
        },
      },
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
