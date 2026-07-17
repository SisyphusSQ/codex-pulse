<script setup lang="ts">
import {
  FolderKanban,
  Gauge,
  LayoutDashboard,
  MessagesSquare,
  MonitorCog,
  Settings,
} from "@lucide/vue";
import type { Component } from "vue";
import { useI18n } from "vue-i18n";
import { RouterLink } from "vue-router";

import appIconUrl from "../../../../docs/design/front/assets/icons/codex-pulse-app-icon-64.png";
import { appNavigation, type AppNavigationName } from "@/router";

const { t } = useI18n();

const navigationIcons: Record<AppNavigationName, Component> = {
  "local-status": MonitorCog,
  overview: LayoutDashboard,
  projects: FolderKanban,
  quota: Gauge,
  sessions: MessagesSquare,
  settings: Settings,
};

function moveNavigationFocus(event: KeyboardEvent) {
  if (!["ArrowDown", "ArrowUp", "Home", "End"].includes(event.key)) return;
  const current = event.currentTarget as HTMLElement;
  const links = Array.from(current.closest("nav")?.querySelectorAll<HTMLElement>("a") ?? []);
  const currentIndex = links.indexOf(current);
  if (currentIndex < 0 || links.length === 0) return;

  event.preventDefault();
  const nextIndex = event.key === "Home"
    ? 0
    : event.key === "End"
      ? links.length - 1
      : event.key === "ArrowDown"
        ? (currentIndex + 1) % links.length
        : (currentIndex - 1 + links.length) % links.length;
  links[nextIndex]?.focus();
}
</script>

<template>
  <aside class="glass-surface flex min-h-0 flex-col rounded-window px-4 pb-5 pt-6">
    <div class="flex items-center gap-3 px-2">
      <img :src="appIconUrl" alt="" aria-hidden="true" class="size-10 rounded-[12px] shadow-md" />
      <div class="min-w-0">
        <p class="truncate text-sm font-bold tracking-tight text-ink">{{ t("app.name") }}</p>
        <p class="sidebar-secondary-copy mt-0.5 truncate text-[11px] text-ink-subtle">{{ t("app.description") }}</p>
      </div>
    </div>

    <nav
      data-testid="primary-navigation"
      :aria-label="t('nav.primaryLabel')"
      class="mt-7 flex min-h-0 flex-1 flex-col gap-1"
    >
      <RouterLink
        v-for="item in appNavigation"
        :key="item.name"
        :to="item.path"
        class="group flex min-h-11 items-center gap-3 rounded-control px-3 text-sm font-medium text-ink-muted transition-colors hover:bg-white/70 hover:text-ink"
        active-class="bg-surface-selected text-ink"
        @keydown="moveNavigationFocus"
      >
        <component
          :is="navigationIcons[item.name]"
          :size="18"
          :stroke-width="1.8"
          aria-hidden="true"
          class="shrink-0 text-ink-subtle transition-colors group-[.router-link-active]:text-accent"
        />
        <span>{{ t(item.labelKey) }}</span>
      </RouterLink>
    </nav>

    <div class="rounded-control border border-white/70 bg-white/55 px-3 py-3">
      <p class="text-xs font-semibold text-ink">{{ t("shell.localOnly.title") }}</p>
      <p class="sidebar-secondary-copy mt-1 text-[11px] leading-5 text-ink-subtle">{{ t("shell.localOnly.description") }}</p>
    </div>
  </aside>
</template>
