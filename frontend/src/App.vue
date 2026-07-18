<script setup lang="ts">
import { Events } from "@wailsio/runtime";
import { onBeforeUnmount } from "vue";
import { RouterView } from "vue-router";
import { useRoute, useRouter } from "vue-router";

const route = useRoute();
const router = useRouter();
const cleanup = Events.On("codex-pulse:navigate", (event) => {
  if (route.name === "popover") return;
  const path = typeof event.data?.path === "string" ? event.data.path : "/overview";
  void router.push(path.startsWith("/") ? path : "/overview");
});
onBeforeUnmount(cleanup);
</script>

<template>
  <RouterView />
</template>
