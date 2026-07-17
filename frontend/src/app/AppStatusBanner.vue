<script setup lang="ts">
import { ArrowRight, RefreshCw } from "@lucide/vue";
import { computed, ref } from "vue";
import { useI18n } from "vue-i18n";
import { useRouter } from "vue-router";

import UiButton from "@/components/ui/UiButton.vue";

import { useAppStatus } from "./useAppStatus";

const { t } = useI18n();
const router = useRouter();
const appStatus = useAppStatus();
const actionFailed = ref(false);
const status = computed(() => appStatus.status.value);
const presentation = computed(() => {
  switch (status.value?.kind) {
    case "blocked":
    case "unavailable":
      return { live: "assertive" as const, role: "alert" as const, style: "border-red-300 bg-red-50 text-red-950" };
    case "offline":
    case "degraded":
    case "stale":
    case "unknown":
      return { live: "polite" as const, role: "status" as const, style: "border-amber-200 bg-amber-50 text-amber-950" };
    default:
      return { live: "polite" as const, role: "status" as const, style: "border-blue-200 bg-blue-50 text-blue-950" };
  }
});
const detail = computed(() => {
  const current = status.value;
  if (current === null || current === undefined) return null;
  return {
    impact: current.primary === null ? t(`health.statusImpact.${current.kind}`) : t(`health.impact.${current.primary.impact}`),
    reason: current.primary === null
      ? t(current.failure === "none" ? `health.statusReason.${current.kind}` : `health.failure.${current.failure}`)
      : t(`health.reason.${current.primary.reason}`),
    time: current.evaluatedAtMs === null ? t("health.time.unknown") : new Intl.DateTimeFormat("zh-CN", {
      dateStyle: "short", timeStyle: "short",
    }).format(new Date(current.evaluatedAtMs)),
  };
});

async function openDetails() {
  actionFailed.value = false;
  try {
    await router.push({ name: "data-health" });
  } catch {
    actionFailed.value = true;
  }
}

async function recover() {
  actionFailed.value = false;
  if (status.value?.primary?.recoveryAction === "none") {
    await openDetails();
    return;
  }
  if (status.value?.primary?.recoveryAction === "retry" || status.value?.kind === "unavailable" || status.value?.kind === "unknown" || status.value?.kind === "stale") {
    try {
      await appStatus.retry();
    } catch {
      actionFailed.value = true;
    }
    return;
  }
  await openDetails();
}
</script>

<template>
  <aside
    v-if="status"
    data-testid="app-status-banner"
    :role="presentation.role"
    :aria-live="presentation.live"
    class="mb-3 flex min-h-11 items-center justify-between gap-4 rounded-control border px-4 py-3 text-sm"
    :class="presentation.style"
  >
    <div class="min-w-0">
      <p class="font-semibold">{{ t(`shell.status.${status.kind}`) }}</p>
      <p v-if="detail" data-testid="app-status-detail" class="mt-0.5 text-xs opacity-80">
        {{ detail.impact }} · {{ detail.reason }} · {{ t("health.time.evaluated", { time: detail.time }) }}
        <span v-if="status.lastTrusted"> · {{ t("health.time.lastTrusted") }}</span>
      </p>
      <p v-if="actionFailed" role="alert" class="mt-1 text-xs">{{ t("shell.status.actionFailed") }}</p>
    </div>
    <div class="flex shrink-0 gap-1">
      <UiButton data-testid="app-status-details" variant="quiet" @click="openDetails">
        <ArrowRight :size="15" aria-hidden="true" />{{ t("shell.status.details") }}
      </UiButton>
      <UiButton v-if="status.retryable || (status.primary && status.primary.recoveryAction !== 'none')" data-testid="app-status-recovery" variant="quiet" @click="recover">
        <RefreshCw :size="15" aria-hidden="true" />
        {{ t(`health.action.${status.primary?.recoveryAction ?? "retry"}`) }}
      </UiButton>
    </div>
  </aside>
</template>
