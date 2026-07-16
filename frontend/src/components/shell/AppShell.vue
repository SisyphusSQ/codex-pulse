<script setup lang="ts">
import { RouterView } from "vue-router";

import AppSidebar from "./AppSidebar.vue";
import AppTitlebar from "./AppTitlebar.vue";
</script>

<template>
  <main
    data-testid="app-shell"
    class="app-shell-background min-h-screen overflow-hidden text-ink"
  >
    <div class="app-shell-grid grid h-screen min-h-[600px] grid-cols-[14.75rem_minmax(0,1fr)] gap-5 p-6">
      <AppSidebar />
      <section class="flex min-h-0 min-w-0 flex-col">
        <AppTitlebar />
        <div class="min-h-0 flex-1 overflow-auto pb-1 pr-1">
          <RouterView />
        </div>
      </section>
    </div>
  </main>
</template>
