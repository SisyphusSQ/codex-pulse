<script setup lang="ts">
import { computed, nextTick, ref } from "vue";
import { useI18n } from "vue-i18n";

import { RuntimeAction } from "@bindings/github.com/SisyphusSQ/codex-pulse/internal/app/models";
import type { NumericValue } from "@bindings/github.com/SisyphusSQ/codex-pulse/internal/query/models";
import type {
  HealthItem,
  JobItem,
  SourceItem,
} from "@bindings/github.com/SisyphusSQ/codex-pulse/internal/query/runtimeinfo/models";
import StateEmpty from "@/components/ui/StateEmpty.vue";
import StateError from "@/components/ui/StateError.vue";
import StateSkeleton from "@/components/ui/StateSkeleton.vue";
import UiButton from "@/components/ui/UiButton.vue";
import UiCard from "@/components/ui/UiCard.vue";
import { useLocalStatusPage } from "@/features/runtime/useLocalStatusPage";

defineOptions({ name: "LocalStatusView" });

const { t } = useI18n();
const page = useLocalStatusPage();
const root = ref<HTMLElement>();
const pauseAllDialog = ref<HTMLElement>();
const pauseAllConfirmationOpen = ref(false);

const initialPending = computed(() => [page.sources, page.jobs, page.health].every(
  (query) => query.data.value === undefined && query.isPending.value,
));
const fatal = computed(() => [page.sources, page.jobs, page.health].every(
  (query) => query.data.value === undefined && query.isError.value,
));

function value(numeric: NumericValue | undefined) {
  return numeric?.value ?? "—";
}

function bytes(numeric: NumericValue | undefined) {
  const raw = numeric?.value;
  if (raw === null || raw === undefined) return "—";
  if (raw < 1024) return `${raw} B`;
  if (raw < 1024 * 1024) return `${Number((raw / 1024).toFixed(1))} KB`;
  if (raw < 1024 * 1024 * 1024) return `${Number((raw / 1024 / 1024).toFixed(1))} MB`;
  return `${Number((raw / 1024 / 1024 / 1024).toFixed(1))} GB`;
}

function dateTime(numeric: NumericValue | undefined) {
  const raw = numeric?.value;
  if (raw === null || raw === undefined) return "—";
  return new Intl.DateTimeFormat("zh-CN", {
    dateStyle: "short", timeStyle: "medium", hour12: false,
  }).format(new Date(raw));
}

function progress(item: JobItem) {
  return `${value(item.progress?.current)} / ${value(item.progress?.total)}`;
}

function sourceLabel(item: SourceItem) {
  const key = item.kind === "online" ? "online" : item.sourceType === "session_index" ? "index" : "local";
  return t(`localStatus.source.${key}`);
}

function stateLabel(state: string) {
  const allowed = ["active", "current", "completed", "succeeded", "running", "queued", "paused", "stale", "failed", "unavailable"];
  return t(`localStatus.state.${allowed.includes(state) ? state : "unknown"}`);
}

function jobLabel(item: JobItem) {
  if (item.phase === "maintenance") return t("localStatus.job.maintenance");
  if (item.phase === "backfill") return t("localStatus.job.backfill");
  return t("localStatus.job.indexing");
}

function healthLabel(item: HealthItem) {
  const severity = ["info", "warning", "error", "critical"].includes(item.severity)
    ? item.severity
    : "unknown";
  return t(`localStatus.health.${severity}`);
}

function retryAll() {
  void page.sources.refetch();
  void page.jobs.refetch();
  void page.health.refetch();
}

async function focusControl(testId: string) {
  await nextTick();
  root.value?.querySelector<HTMLElement>(`[data-testid='${testId}']`)?.focus();
}

function openPauseAllConfirmation() {
  pauseAllConfirmationOpen.value = true;
  void focusControl("pause-all-confirm");
}

function cancelPauseAllConfirmation() {
  pauseAllConfirmationOpen.value = false;
  void focusControl("runtime-pause-all");
}

function confirmPauseAll() {
  pauseAllConfirmationOpen.value = false;
  page.runAction(RuntimeAction.RuntimeActionPauseAll, {
    onSettled: () => void focusControl("runtime-pause-all"),
  });
}

function keepDialogFocus(event: KeyboardEvent) {
  if (event.key !== "Tab") return;
  const controls = Array.from(pauseAllDialog.value?.querySelectorAll<HTMLElement>("button:not([disabled])") ?? []);
  const first = controls[0];
  const last = controls.at(-1);
  if (first === undefined || last === undefined) return;
  if (event.shiftKey && document.activeElement === first) {
    event.preventDefault();
    last.focus();
  } else if (!event.shiftKey && document.activeElement === last) {
    event.preventDefault();
    first.focus();
  }
}
</script>

<template>
  <section ref="root" data-testid="local-status-view" class="w-full space-y-4 py-1">
    <div
      data-testid="local-status-content"
      class="space-y-4"
      :inert="pauseAllConfirmationOpen || undefined"
      :aria-hidden="pauseAllConfirmationOpen || undefined"
    >
    <div class="flex flex-wrap items-center justify-between gap-3">
      <div class="ml-auto flex flex-wrap gap-2" :aria-label="t('localStatus.action.controlsLabel')">
        <UiButton data-testid="runtime-pause-backfill" :loading="page.action.isPending.value" @click="page.runAction(RuntimeAction.RuntimeActionPauseBackfill)">{{ t("localStatus.action.pauseBackfill") }}</UiButton>
        <UiButton data-testid="runtime-pause-all" :loading="page.action.isPending.value" @click="openPauseAllConfirmation">{{ t("localStatus.action.pauseAll") }}</UiButton>
        <UiButton data-testid="runtime-resume" :loading="page.action.isPending.value" @click="page.runAction(RuntimeAction.RuntimeActionResume)">{{ t("localStatus.action.resume") }}</UiButton>
        <UiButton data-testid="runtime-reconcile" :loading="page.action.isPending.value" @click="page.runAction(RuntimeAction.RuntimeActionReconcile)">{{ t("localStatus.action.reconcile") }}</UiButton>
      </div>
    </div>

    <StateSkeleton v-if="initialPending" :label="t('localStatus.state.loading')" :rows="6" />
    <StateError v-else-if="fatal" data-testid="local-status-error" action-test-id="local-status-retry" :title="t('localStatus.state.errorTitle')" :description="t('localStatus.state.errorDescription')" :action-label="t('localStatus.state.retry')" @retry="retryAll" />

    <template v-else>
      <div class="grid gap-4 lg:grid-cols-3">
        <UiCard :title="t('localStatus.source.title')" :description="t('localStatus.source.description')">
          <StateSkeleton v-if="page.sources.data.value === undefined && page.sources.isPending.value" :label="t('localStatus.source.loading')" :rows="3" />
          <div v-else-if="page.sources.data.value === undefined && page.sources.isError.value" data-testid="source-unavailable"><StateError :title="t('localStatus.source.unavailable')" :description="t('localStatus.source.unavailableDescription')" :action-label="t('localStatus.state.retry')" @retry="page.sources.refetch()" /></div>
          <div v-else-if="page.sources.data.value?.meta.status === 'unavailable'" data-testid="source-unavailable"><StateError :title="t('localStatus.source.unavailable')" :description="t('localStatus.source.unavailableDescription')" :action-label="t('localStatus.state.retry')" @retry="page.sources.refetch()" /></div>
          <template v-else>
          <p v-if="page.sources.data.value?.meta.status === 'partial'" data-testid="source-partial" role="status" aria-live="polite" class="mb-3 text-sm text-amber-800">{{ t("localStatus.state.partial") }}</p>
          <p v-if="page.sources.data.value && (page.sources.isError.value || page.sources.isStale.value)" data-testid="source-stale" role="status" class="mb-3 text-sm text-amber-800">{{ t("localStatus.state.lastTrusted") }}</p>
          <p class="text-3xl font-semibold">{{ value(page.sources.data.value?.summary.total) }}</p>
          <p class="mt-2 text-sm text-ink-muted">{{ t("localStatus.source.attention", { count: value(page.sources.data.value?.summary.attention) }) }}</p>
          <div v-if="page.sources.data.value?.items?.length" class="mt-4 space-y-2">
            <div v-for="item in page.sources.data.value.items" :key="item.sourceKey" data-testid="runtime-source" class="rounded-control border border-line p-3">
              <p class="font-medium">{{ sourceLabel(item) }}</p><p class="text-sm text-ink-muted">{{ stateLabel(item.state) }}</p>
              <p class="mt-2 text-xs text-ink-muted">{{ t("localStatus.source.sizeProgress", { parsed: bytes(item.parsedBytes), total: bytes(item.sizeBytes) }) }}</p>
              <p class="text-xs text-ink-muted">{{ t("localStatus.source.lastSuccess", { value: dateTime(item.lastSuccessAtMs) }) }}</p>
              <p class="text-xs text-ink-muted">{{ t("localStatus.source.lastAttempt", { value: dateTime(item.lastAttemptAtMs) }) }}</p>
            </div>
          </div>
          <StateEmpty v-else :title="t('localStatus.source.empty')" :description="t('localStatus.source.emptyDescription')" />
          </template>
        </UiCard>

        <UiCard :title="t('localStatus.job.title')" :description="t('localStatus.job.description')">
          <StateSkeleton v-if="page.jobs.data.value === undefined && page.jobs.isPending.value" :label="t('localStatus.job.loading')" :rows="3" />
          <div v-else-if="page.jobs.data.value === undefined && page.jobs.isError.value" data-testid="job-unavailable"><StateError :title="t('localStatus.job.unavailable')" :description="t('localStatus.job.unavailableDescription')" :action-label="t('localStatus.state.retry')" @retry="page.jobs.refetch()" /></div>
          <div v-else-if="page.jobs.data.value?.meta.status === 'unavailable'" data-testid="job-unavailable"><StateError :title="t('localStatus.job.unavailable')" :description="t('localStatus.job.unavailableDescription')" :action-label="t('localStatus.state.retry')" @retry="page.jobs.refetch()" /></div>
          <template v-else>
          <p v-if="page.jobs.data.value?.meta.status === 'partial'" data-testid="job-partial" role="status" aria-live="polite" class="mb-3 text-sm text-amber-800">{{ t("localStatus.state.partial") }}</p>
          <p v-if="page.jobs.data.value && (page.jobs.isError.value || page.jobs.isStale.value)" data-testid="job-stale" role="status" class="mb-3 text-sm text-amber-800">{{ t("localStatus.state.lastTrusted") }}</p>
          <p class="text-3xl font-semibold">{{ value(page.jobs.data.value?.summary.total) }}</p>
          <div v-if="page.jobs.data.value?.items?.length" class="mt-4 space-y-2">
            <div v-for="item in page.jobs.data.value.items" :key="item.jobId" data-testid="runtime-job" class="rounded-control border border-line p-3">
              <p class="font-medium">{{ jobLabel(item) }}</p><p class="text-sm text-ink-muted">{{ stateLabel(item.state) }}</p>
              <p class="mt-2 text-xs text-ink-muted">{{ t("localStatus.job.progress", { value: progress(item) }) }}</p>
              <p class="text-xs text-ink-muted">{{ t("localStatus.job.startedAt", { value: dateTime(item.startedAtMs) }) }}</p>
              <p class="text-xs text-ink-muted">{{ t("localStatus.job.nextRetry", { value: dateTime(item.nextRetryAtMs) }) }}</p>
            </div>
          </div>
          <StateEmpty v-else :title="t('localStatus.job.empty')" :description="t('localStatus.job.emptyDescription')" />
          </template>
        </UiCard>

        <UiCard id="data-health" :title="t('localStatus.health.title')" :description="t('localStatus.health.description')">
          <StateSkeleton v-if="page.health.data.value === undefined && page.health.isPending.value" :label="t('localStatus.health.loading')" :rows="3" />
          <div v-else-if="page.health.data.value === undefined && page.health.isError.value" data-testid="health-unavailable"><StateError :title="t('localStatus.health.unavailable')" :description="t('localStatus.health.unavailableDescription')" :action-label="t('localStatus.state.retry')" @retry="page.health.refetch()" /></div>
          <div v-else-if="page.health.data.value?.meta.status === 'unavailable'" data-testid="health-unavailable"><StateError :title="t('localStatus.health.unavailable')" :description="t('localStatus.health.unavailableDescription')" :action-label="t('localStatus.state.retry')" @retry="page.health.refetch()" /></div>
          <template v-else>
          <p v-if="page.health.data.value && (page.health.isError.value || page.health.isStale.value)" data-testid="health-stale" role="status" class="mb-3 text-sm text-amber-800">{{ t("localStatus.state.lastTrusted") }}</p>
          <p class="text-3xl font-semibold">{{ value(page.health.data.value?.summary.active) }}</p>
          <div v-if="page.health.data.value?.items?.length" class="mt-4 space-y-2">
            <div v-for="item in page.health.data.value.items" :key="item.eventId" data-testid="runtime-health" class="rounded-control border border-line p-3">
              <p class="font-medium">{{ healthLabel(item) }}</p><p class="text-sm text-ink-muted">{{ item.active ? t("localStatus.health.active") : t("localStatus.health.resolved") }}</p>
            </div>
          </div>
          <StateEmpty v-else :title="t('localStatus.health.empty')" :description="t('localStatus.health.emptyDescription')" />
          </template>
        </UiCard>
      </div>

      <UiCard :title="t('localStatus.repair.title')" :description="t('localStatus.repair.description')">
        <UiButton data-testid="runtime-repair-analyze" :loading="page.repair.isPending.value" @click="page.analyzeRepair()">{{ t("localStatus.repair.analyze") }}</UiButton>
        <p v-if="page.repair.data.value" data-testid="repair-result" role="status" aria-live="polite" class="mt-3 text-sm text-ink-muted">
          {{ t("localStatus.repair.result", { actions: page.repair.data.value.actionCount, conflicts: page.repair.data.value.conflictCount }) }}
        </p>
        <p v-if="page.action.data.value" data-testid="runtime-action-result" role="status" aria-live="polite" class="mt-3 text-sm text-ink-muted">{{ t("localStatus.action.result") }}</p>
        <p v-if="page.action.isError.value || page.repair.isError.value" role="alert" class="mt-3 text-sm text-critical">{{ t("localStatus.state.commandError") }}</p>
      </UiCard>
    </template>
    </div>

    <div
      v-if="pauseAllConfirmationOpen"
      data-testid="pause-all-modal-layer"
      class="fixed inset-0 z-50 grid place-items-center bg-slate-950/25 p-6"
    >
      <div ref="pauseAllDialog" data-testid="pause-all-dialog" role="dialog" aria-modal="true" :aria-label="t('localStatus.action.pauseAllConfirmTitle')" class="w-full max-w-lg rounded-content border border-red-200 bg-red-50 p-5 shadow-xl" @keydown.esc.prevent="cancelPauseAllConfirmation" @keydown="keepDialogFocus">
        <p class="font-semibold text-red-950">{{ t("localStatus.action.pauseAllConfirmTitle") }}</p>
        <p class="mt-1 text-sm text-red-900/75">{{ t("localStatus.action.pauseAllConfirmDescription") }}</p>
        <div class="mt-4 flex gap-2">
          <UiButton data-testid="pause-all-cancel" @click="cancelPauseAllConfirmation">{{ t("localStatus.action.cancel") }}</UiButton>
          <UiButton data-testid="pause-all-confirm" variant="danger" @click="confirmPauseAll">{{ t("localStatus.action.confirmPauseAll") }}</UiButton>
        </div>
      </div>
    </div>
  </section>
</template>
