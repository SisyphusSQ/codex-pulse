<script setup lang="ts">
import { computed } from "vue";
import { useI18n } from "vue-i18n";
import { useRoute } from "vue-router";

const route = useRoute();
const { t } = useI18n();

const title = computed(() => t(String(route.meta.titleKey ?? "routes.overview.title")));
const description = computed(() => t(String(route.meta.descriptionKey ?? "routes.overview.description")));
</script>

<template>
  <header class="wails-drag-region flex min-h-[76px] items-end justify-between gap-6 px-1 pb-4 pt-2">
    <div class="min-w-0">
      <h1 class="truncate text-2xl font-bold tracking-tight text-ink">{{ title }}</h1>
      <p class="mt-1 truncate text-xs text-ink-subtle">{{ description }}</p>
    </div>
    <slot />
  </header>
</template>
