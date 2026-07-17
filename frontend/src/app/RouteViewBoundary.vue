<script setup lang="ts">
import { computed, nextTick, onErrorCaptured, ref, watch } from "vue";
import { useI18n } from "vue-i18n";
import { RouterView, useRoute } from "vue-router";

import StateError from "@/components/ui/StateError.vue";

const { t } = useI18n();
const route = useRoute();
const failed = ref(false);
const revision = ref(0);
const routeIdentity = computed(() => route.matched.at(-1)?.path ?? route.name ?? route.path);

onErrorCaptured(() => {
  failed.value = true;
  return false;
});

function reset() {
  failed.value = false;
  revision.value++;
}

async function retry() {
  reset();
  await nextTick();
}

watch(routeIdentity, reset);
</script>

<template>
  <RouterView v-slot="{ Component }">
    <StateError
      v-if="failed"
      data-testid="route-error"
      action-test-id="route-error-retry"
      :title="t('shell.routeError.title')"
      :description="t('shell.routeError.description')"
      :action-label="t('shell.routeError.retry')"
      @retry="retry"
    />
    <component :is="Component" v-else :key="`${String(routeIdentity)}:${revision}`" />
  </RouterView>
</template>
