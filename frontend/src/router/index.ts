import type { RouterHistory, RouteRecordRaw } from "vue-router";
import { createRouter, createWebHistory } from "vue-router";

import ShellView from "@/views/ShellView.vue";

const routes = [
  {
    path: "/",
    name: "shell",
    component: ShellView,
  },
] satisfies RouteRecordRaw[];

export function createAppRouter(history: RouterHistory = createWebHistory()) {
  return createRouter({ history, routes });
}
