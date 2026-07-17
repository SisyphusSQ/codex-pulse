<script setup lang="ts">
import { RefreshCw } from "@lucide/vue";
import { computed } from "vue";
import { useI18n } from "vue-i18n";

import UiButton from "@/components/ui/UiButton.vue";

import { useAppStatus } from "./useAppStatus";

const { t } = useI18n();
const appStatus = useAppStatus();
const status = computed(() => appStatus.status.value);
const presentation = computed(() => {
  switch (status.value?.kind) {
    case "blocked":
    case "unavailable":
      return {
        live: "assertive" as const,
        role: "alert" as const,
        style: "border-red-300 bg-red-50 text-red-950",
      };
    case "offline":
    case "degraded":
    case "partial":
    case "stale":
      return {
        live: "polite" as const,
        role: "status" as const,
        style: "border-amber-200 bg-amber-50 text-amber-950",
      };
    default:
      return {
        live: "polite" as const,
        role: "status" as const,
        style: "border-blue-200 bg-blue-50 text-blue-950",
      };
  }
});
</script>

<template>
  <aside
    v-if="status"
    data-testid="app-status-banner"
    :role="presentation.role"
    :aria-live="presentation.live"
    class="mb-3 flex min-h-11 items-center justify-between gap-3 rounded-control border px-4 py-2 text-sm"
    :class="presentation.style"
  >
    <p>{{ t(`shell.status.${status.kind}`) }}</p>
    <UiButton
      v-if="status.retryable"
      data-testid="app-status-retry"
      variant="quiet"
      class="shrink-0"
      @click="appStatus.retry"
    >
      <RefreshCw :size="15" aria-hidden="true" />
      {{ t("shell.status.retry") }}
    </UiButton>
  </aside>
</template>
