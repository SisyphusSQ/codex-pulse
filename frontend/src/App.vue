<script setup lang="ts">
import { Events } from "@wailsio/runtime";
import { onBeforeUnmount } from "vue";
import { RouterView } from "vue-router";
import { useRoute, useRouter } from "vue-router";

import { normalizeDesktopNavigationPath } from "@/router";

const route = useRoute();
const router = useRouter();
const cleanup = Events.On("codex-pulse:navigate", (event) => {
  if (route.name === "popover") return;
  void router.push(normalizeDesktopNavigationPath(event.data?.path));
});
onBeforeUnmount(cleanup);
</script>

<template>
  <RouterView />
</template>
