<script setup lang="ts">
import { computed, nextTick, ref, watch } from "vue";
import { useI18n } from "vue-i18n";
import { useRoute } from "vue-router";

import AppStatusBanner from "@/app/AppStatusBanner.vue";
import RouteViewBoundary from "@/app/RouteViewBoundary.vue";

import AppSidebar from "./AppSidebar.vue";
import AppTitlebar from "./AppTitlebar.vue";

const { t } = useI18n();
const route = useRoute();
const content = ref<HTMLElement>();
const titlebar = ref<{ focusHeading: () => void }>();
const routeIdentity = computed(() => route.matched.at(-1)?.path ?? route.name ?? route.path);

function focusContent() {
  content.value?.focus();
}

watch(routeIdentity, async (_current, previous) => {
  if (previous === undefined) return;
  await nextTick();
  if (content.value) content.value.scrollTop = 0;
  titlebar.value?.focusHeading();
});
</script>

<template>
  <div
    data-testid="app-shell"
    class="app-shell-background min-h-screen overflow-hidden text-ink"
  >
    <a
      data-testid="skip-to-content"
      href="#app-content"
      class="sr-only fixed left-4 top-4 z-50 rounded-control bg-white px-4 py-2 font-semibold text-ink shadow-lg focus:not-sr-only"
      @click.prevent="focusContent"
    >
      {{ t("shell.skipToContent") }}
    </a>
    <div class="app-shell-grid grid h-screen min-h-[600px] grid-cols-[14.75rem_minmax(0,1fr)] gap-5 p-6">
      <AppSidebar />
      <section class="flex min-h-0 min-w-0 flex-col">
        <AppTitlebar ref="titlebar" />
        <AppStatusBanner />
        <main id="app-content" ref="content" data-testid="app-content" tabindex="-1" class="min-h-0 flex-1 overflow-auto pb-1 pr-1">
          <RouteViewBoundary />
        </main>
      </section>
    </div>
  </div>
</template>
