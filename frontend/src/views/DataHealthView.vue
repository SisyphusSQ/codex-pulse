<script setup lang="ts">
import { ArrowLeft, RefreshCw } from "@lucide/vue";
import { computed, nextTick, ref } from "vue";
import { useI18n } from "vue-i18n";
import { useRouter } from "vue-router";

import type { NumericValue } from "@bindings/github.com/SisyphusSQ/codex-pulse/internal/query/models";
import type { HealthItem, JobItem } from "@bindings/github.com/SisyphusSQ/codex-pulse/internal/query/runtimeinfo/models";
import StateEmpty from "@/components/ui/StateEmpty.vue";
import StateError from "@/components/ui/StateError.vue";
import StateSkeleton from "@/components/ui/StateSkeleton.vue";
import UiButton from "@/components/ui/UiButton.vue";
import UiCard from "@/components/ui/UiCard.vue";
import DataHealthTrendChart from "@/features/runtime/DataHealthTrendChart.vue";
import { dataHealthActionFor, useDataHealthPage } from "@/features/runtime/useDataHealthPage";

defineOptions({ name: "DataHealthView" });

type PreviewAction = "reconcile" | "repair_dry_run";

const { t } = useI18n();
const router = useRouter();
const page = useDataHealthPage();
const root = ref<HTMLElement>();
const actionDialog = ref<HTMLElement>();
const previewAction = ref<PreviewAction>();
const previewTrigger = ref<string>();

const initialPending = computed(() => page.metrics.data.value === undefined && page.metrics.isPending.value);
const unavailable = computed(() => page.metrics.data.value === undefined && page.metrics.isError.value);
const cached = computed(() => [page.metrics, page.projection].some(
  (query) => query.data.value !== undefined && (query.isError.value || query.isStale.value),
));
const projectionPending = computed(() => page.projection.data.value === undefined && page.projection.isPending.value);
const projectionUnavailable = computed(() => page.projection.data.value === undefined && page.projection.isError.value);
const partial = computed(() => page.metrics.data.value !== undefined && projectionUnavailable.value);
const components = computed(() => page.projection.data.value?.components ?? []);
const currentJobs = computed(() => page.metrics.data.value?.currentJobs ?? []);
const recentJobs = computed(() => page.metrics.data.value?.recentJobs ?? []);
const openEvents = computed(() => page.metrics.data.value?.openEvents ?? []);
const recentEvents = computed(() => page.metrics.data.value?.recentEvents ?? []);
const latest = computed(() => page.metrics.data.value?.latest);
const availableActions = computed(() => new Set(components.value.flatMap((component) => {
  const action = dataHealthActionFor(component.recoveryAction);
  return action === null ? [] : [action];
})));

const componentNames = new Set([
  "local_index", "live_queue", "history_backfill", "online_quota", "storage", "runtime", "updater",
]);
const levels = new Set(["healthy", "busy", "paused", "degraded", "blocked"]);
const phases = new Set(["discover", "fast_bootstrap", "history_backfill", "reconcile", "live", "maintenance"]);
const transitions = new Set(["steady", "draining", "reconciling", "blocked"]);
const evidenceKinds = new Set(["known", "unknown", "not_configured"]);

function numeric(value: NumericValue | undefined) {
  return value?.value ?? null;
}

function count(value: NumericValue | undefined) {
  return numeric(value) ?? "—";
}

function bytes(value: NumericValue | undefined) {
  const raw = numeric(value);
  if (raw === null) return "—";
  if (raw < 1024) return `${raw} B`;
  if (raw < 1024 ** 2) return `${Number((raw / 1024).toFixed(1))} KB`;
  if (raw < 1024 ** 3) return `${Number((raw / 1024 ** 2).toFixed(1))} MB`;
  return `${Number((raw / 1024 ** 3).toFixed(2))} GB`;
}

function dateTime(value: NumericValue | undefined) {
  const raw = numeric(value);
  if (raw === null) return "—";
  return new Intl.DateTimeFormat("zh-CN", { dateStyle: "short", timeStyle: "medium", hour12: false }).format(new Date(raw));
}

function componentLabel(value: string) {
  return t(`dataHealth.component.${componentNames.has(value) ? value : "unknown"}`);
}

function levelLabel(value: string) {
  return t(`dataHealth.level.${levels.has(value) ? value : "unknown"}`);
}

function evidenceLabel(value: string) {
  return t(`dataHealth.evidence.${evidenceKinds.has(value) ? value : "unknown"}`);
}

function jobLabel(item: JobItem) {
  return t(`dataHealth.job.${phases.has(item.phase) ? item.phase : "unknown"}`);
}

function eventTitle(item: HealthItem) {
  return componentLabel(item.component);
}

function transitionLabel(value: string | undefined) {
  return t(`dataHealth.transition.${value !== undefined && transitions.has(value) ? value : "unknown"}`);
}

async function focusControl(testId: string | undefined) {
  if (testId === undefined) return;
  await nextTick();
  root.value?.querySelector<HTMLElement>(`[data-testid='${testId}']`)?.focus();
}

function openPreview(action: PreviewAction, trigger: string) {
  previewTrigger.value = trigger;
  previewAction.value = action;
  void focusControl("data-health-action-confirm");
}

function confirmAction() {
  const trigger = previewTrigger.value;
  if (previewAction.value === "reconcile") page.reconcile.mutate();
  if (previewAction.value === "repair_dry_run") page.repair.mutate();
  previewAction.value = undefined;
  previewTrigger.value = undefined;
  void focusControl(trigger);
}

function closePreview() {
  const trigger = previewTrigger.value;
  previewAction.value = undefined;
  previewTrigger.value = undefined;
  void focusControl(trigger);
}

function keepDialogFocus(event: KeyboardEvent) {
  if (event.key !== "Tab") return;
  const controls = Array.from(actionDialog.value?.querySelectorAll<HTMLElement>("button:not([disabled])") ?? []);
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

async function openSettings() {
  await router.push({ name: "settings" });
}
</script>

<template>
  <section ref="root" data-testid="data-health-view" class="w-full space-y-4 py-1">
    <div
      data-testid="data-health-content"
      class="space-y-4"
      :inert="previewAction !== undefined || undefined"
      :aria-hidden="previewAction !== undefined || undefined"
    >
    <header class="flex flex-wrap items-center justify-between gap-3">
      <div class="flex items-center gap-3">
        <UiButton data-testid="data-health-back" variant="quiet" :aria-label="t('dataHealth.action.back')" @click="router.push({ name: 'local-status' })">
          <ArrowLeft :size="18" aria-hidden="true" />
        </UiButton>
        <div>
          <h2 class="text-2xl font-semibold tracking-tight">{{ t("dataHealth.title") }}</h2>
          <p class="mt-1 text-sm text-ink-muted">{{ t("dataHealth.subtitle", { time: dateTime(page.metrics.data.value?.evaluatedAtMs) }) }}</p>
        </div>
      </div>
      <UiButton data-testid="data-health-refresh" :loading="page.metrics.isFetching.value" @click="page.refresh()">
        <RefreshCw :size="16" aria-hidden="true" />{{ t("dataHealth.action.refresh") }}
      </UiButton>
    </header>

    <StateSkeleton v-if="initialPending" :label="t('dataHealth.state.loading')" :rows="8" />
    <div v-else-if="unavailable" data-testid="data-health-unavailable">
      <StateError :title="t('dataHealth.state.unavailable')" :description="t('dataHealth.state.unavailableDescription')" :action-label="t('dataHealth.action.refresh')" @retry="page.refresh()" />
    </div>

    <template v-else>
      <p v-if="cached || page.metrics.isStale.value" data-testid="data-health-last-trusted" role="status" class="text-sm text-amber-800">
        {{ t("dataHealth.state.lastTrusted") }}
      </p>
      <p v-if="partial" data-testid="data-health-partial" role="status" aria-live="polite" class="text-sm text-amber-800">
        {{ t("dataHealth.state.partial") }}
      </p>
      <section v-if="page.projection.data.value?.primary" data-testid="data-health-impact" class="rounded-content border border-amber-200 bg-amber-50 p-5">
        <p class="font-semibold text-amber-950">{{ t("dataHealth.impact.title", { component: componentLabel(page.projection.data.value.primary.component) }) }}</p>
        <p class="mt-1 text-sm text-amber-900/80">{{ t(`health.impact.${page.projection.data.value.primary.impact}`) }}</p>
        <p class="mt-1 text-sm text-amber-900/80">{{ t("dataHealth.impact.protection", { value: t(`health.protection.${page.projection.data.value.primary.protection}`) }) }}</p>
      </section>

      <UiCard :title="t('dataHealth.domain.title')" :description="t('dataHealth.domain.description')">
        <p v-if="page.projection.data.value" data-testid="data-health-projection-time" class="mb-3 text-xs text-ink-muted">
          {{ t("dataHealth.domain.evaluatedAt", { time: dateTime(page.projection.data.value.evaluatedAtMs) }) }}
        </p>
        <StateSkeleton v-if="projectionPending" :label="t('dataHealth.domain.loading')" :rows="4" />
        <div v-else-if="projectionUnavailable" data-testid="data-health-projection-unavailable">
          <StateError :title="t('dataHealth.domain.unavailable')" :description="t('dataHealth.domain.unavailableDescription')" :action-label="t('dataHealth.action.refresh')" @retry="page.refresh()" />
        </div>
        <div v-else-if="components.length" class="grid gap-3 md:grid-cols-2">
          <div v-for="item in components" :key="item.component" data-testid="data-health-component" class="flex items-center justify-between rounded-control border border-line bg-white/60 p-3">
            <div>
              <p class="font-medium">{{ componentLabel(item.component) }}</p>
              <p class="mt-1 text-xs text-ink-muted">{{ evidenceLabel(item.evidence) }} · {{ t(`health.reason.${item.reason}`) }}</p>
            </div>
            <span class="rounded-full bg-black/5 px-2 py-1 text-xs font-medium" :data-level="item.level">{{ levelLabel(item.level) }}</span>
          </div>
        </div>
        <StateEmpty v-else :title="t('dataHealth.domain.empty')" :description="t('dataHealth.domain.emptyDescription')" />
        <div v-if="page.metrics.data.value" class="mt-4 grid gap-3 md:grid-cols-2">
          <p data-testid="data-health-source-metrics" class="rounded-control bg-slate-50 p-3 text-sm text-ink-muted">
            {{ t("dataHealth.metric.sources", {
              total: count(page.metrics.data.value.sources.total),
              current: count(page.metrics.data.value.sources.current),
              unavailable: count(page.metrics.data.value.sources.unavailable),
              failures: count(page.metrics.data.value.sources.consecutiveFailures),
            }) }}
          </p>
          <p data-testid="data-health-scheduler-metrics" class="rounded-control bg-slate-50 p-3 text-sm text-ink-muted">
            {{ t("dataHealth.metric.scheduler", {
              cycles: count(page.metrics.data.value.scheduler.cycleCount),
              files: count(page.metrics.data.value.scheduler.filesScanned),
              progress: dateTime(page.metrics.data.value.scheduler.lastProgressAtMs),
            }) }}
          </p>
        </div>
      </UiCard>

      <div class="grid gap-4 xl:grid-cols-[minmax(0,1fr)_380px]">
        <div class="space-y-4">
          <UiCard :title="t('dataHealth.work.title')" :description="t('dataHealth.work.description')">
            <div v-if="currentJobs.length || recentJobs.length" class="space-y-4">
              <section v-if="currentJobs.length" data-testid="data-health-current-jobs">
                <p class="mb-2 text-xs font-semibold uppercase tracking-wide text-ink-muted">{{ t("dataHealth.work.current") }}</p>
                <div class="overflow-hidden rounded-control border border-line">
                  <div v-for="item in currentJobs" :key="item.jobId" data-testid="data-health-job" class="grid gap-2 border-b border-line p-3 last:border-b-0 md:grid-cols-4">
                    <p class="font-medium">{{ jobLabel(item) }}</p>
                    <p class="text-sm text-ink-muted">{{ item.state }}</p>
                    <p class="text-sm text-ink-muted">{{ count(item.progress?.current) }} / {{ count(item.progress?.total) }}</p>
                    <p class="text-sm text-ink-muted">{{ dateTime(item.nextRetryAtMs) }}</p>
                  </div>
                </div>
              </section>
              <section v-if="recentJobs.length" data-testid="data-health-recent-jobs">
                <p class="mb-2 text-xs font-semibold uppercase tracking-wide text-ink-muted">{{ t("dataHealth.work.recent") }}</p>
                <div class="overflow-hidden rounded-control border border-line">
                  <div v-for="item in recentJobs" :key="item.jobId" data-testid="data-health-job" class="grid gap-2 border-b border-line p-3 last:border-b-0 md:grid-cols-4">
                    <p class="font-medium">{{ jobLabel(item) }}</p>
                    <p class="text-sm text-ink-muted">{{ item.state }}</p>
                    <p class="text-sm text-ink-muted">{{ count(item.progress?.current) }} / {{ count(item.progress?.total) }}</p>
                    <p class="text-sm text-ink-muted">{{ dateTime(item.finishedAtMs) }}</p>
                  </div>
                </div>
              </section>
            </div>
            <StateEmpty v-else :title="t('dataHealth.work.empty')" :description="t('dataHealth.work.emptyDescription')" />
          </UiCard>

          <UiCard :title="t('dataHealth.event.title')" :description="t('dataHealth.event.description')">
            <div v-if="openEvents.length || recentEvents.length" class="space-y-4">
              <section v-if="openEvents.length" data-testid="data-health-open-events">
                <p class="mb-2 text-xs font-semibold uppercase tracking-wide text-ink-muted">{{ t("dataHealth.event.open") }}</p>
                <div class="space-y-2">
                  <div v-for="item in openEvents" :key="item.eventId" data-testid="data-health-event" class="rounded-control border border-line p-3">
                    <div class="flex items-center justify-between gap-3">
                      <p class="font-medium">{{ eventTitle(item) }}</p>
                      <span class="text-sm text-ink-muted">{{ t("dataHealth.event.occurrences", { count: count(item.occurrenceCount) }) }}</span>
                    </div>
                    <p class="mt-1 text-sm text-ink-muted">{{ t("dataHealth.event.active") }}</p>
                    <p class="mt-1 text-xs text-ink-muted">{{ t(`health.impact.${item.impact}`) }} · {{ dateTime(item.lastSeenAtMs) }}</p>
                  </div>
                </div>
              </section>
              <section v-if="recentEvents.length" data-testid="data-health-recent-events">
                <p class="mb-2 text-xs font-semibold uppercase tracking-wide text-ink-muted">{{ t("dataHealth.event.recent") }}</p>
                <div class="space-y-2">
                  <div v-for="item in recentEvents" :key="item.eventId" data-testid="data-health-event" class="rounded-control border border-line p-3">
                    <div class="flex items-center justify-between gap-3">
                      <p class="font-medium">{{ eventTitle(item) }}</p>
                      <span class="text-sm text-ink-muted">{{ t("dataHealth.event.occurrences", { count: count(item.occurrenceCount) }) }}</span>
                    </div>
                    <p class="mt-1 text-sm text-ink-muted">{{ t("dataHealth.event.resolved") }}</p>
                    <p class="mt-1 text-xs text-ink-muted">{{ t(`health.impact.${item.impact}`) }} · {{ dateTime(item.lastSeenAtMs) }}</p>
                  </div>
                </div>
              </section>
            </div>
            <StateEmpty v-else :title="t('dataHealth.event.empty')" :description="t('dataHealth.event.emptyDescription')" />
          </UiCard>
        </div>

        <div class="space-y-4">
          <UiCard data-testid="data-health-resources" :title="t('dataHealth.resource.title')" :description="t('dataHealth.resource.description')">
            <div v-if="latest" class="space-y-4 text-sm">
              <div><span>{{ t("dataHealth.resource.cpu") }}</span><strong class="float-right">{{ latest.cpuPercent }}%</strong></div>
              <div><span>{{ t("dataHealth.resource.rss") }}</span><strong class="float-right">{{ bytes(latest.rssBytes) }}</strong></div>
              <div><span>{{ t("dataHealth.resource.database") }}</span><strong class="float-right">{{ bytes(latest.dbBytes) }} / {{ bytes(latest.walBytes) }}</strong></div>
              <div><span>{{ t("dataHealth.resource.disk") }}</span><strong class="float-right">{{ bytes(latest.diskFreeBytes) }}</strong></div>
              <div><span>{{ t("dataHealth.resource.queue") }}</span><strong class="float-right">{{ count(latest.liveQueueDepth) }} / {{ count(latest.backfillQueueDepth) }}</strong></div>
              <DataHealthTrendChart :label="t('dataHealth.resource.trendAria')" :points="page.metrics.data.value?.runtime ?? []" />
            </div>
            <StateEmpty v-else :title="t('dataHealth.resource.empty')" :description="t('dataHealth.resource.emptyDescription')" />
          </UiCard>

          <UiCard :title="t('dataHealth.action.title')" :description="t('dataHealth.action.description')">
            <div class="grid gap-2">
              <UiButton v-if="availableActions.has('reconcile')" data-testid="data-health-reconcile" variant="primary" :loading="page.reconcile.isPending.value" @click="openPreview('reconcile', 'data-health-reconcile')">{{ t("dataHealth.action.reconcile") }}</UiButton>
              <UiButton v-if="availableActions.has('repair_dry_run')" data-testid="data-health-repair" :loading="page.repair.isPending.value" @click="openPreview('repair_dry_run', 'data-health-repair')">{{ t("dataHealth.action.repair") }}</UiButton>
              <UiButton v-if="availableActions.has('open_settings')" data-testid="data-health-settings" @click="openSettings">{{ t("dataHealth.action.settings") }}</UiButton>
            </div>
            <p v-if="page.reconcile.isPending.value || page.repair.isPending.value" data-testid="data-health-action-progress" role="status" aria-live="polite" class="mt-3 text-sm text-ink-muted">{{ t("dataHealth.action.progress") }}</p>
            <p v-if="page.reconcile.data.value" data-testid="data-health-action-result" role="status" aria-live="polite" class="mt-3 text-sm text-ink-muted">{{ t("dataHealth.action.reconcileResult", { transition: transitionLabel(page.reconcile.data.value.transition) }) }}</p>
            <p v-if="page.repair.data.value" data-testid="data-health-repair-result" role="status" aria-live="polite" class="mt-3 text-sm text-ink-muted">{{ t("dataHealth.action.repairResult", { actions: page.repair.data.value.actionCount, conflicts: page.repair.data.value.conflictCount }) }}</p>
            <p v-if="page.reconcile.isError.value || page.repair.isError.value" role="alert" class="mt-3 text-sm text-critical">{{ t("dataHealth.action.error") }}</p>
            <p class="mt-4 rounded-control bg-blue-50 p-3 text-xs text-blue-900">{{ t("dataHealth.privacy") }}</p>
          </UiCard>
        </div>
      </div>
    </template>
    </div>

    <div v-if="previewAction" class="fixed inset-0 z-50 grid place-items-center bg-slate-950/25 p-6">
      <div ref="actionDialog" data-testid="data-health-action-preview" role="dialog" aria-modal="true" :aria-label="t('dataHealth.preview.title')" class="w-full max-w-lg rounded-content border border-line bg-white p-5 shadow-xl" @keydown.esc.prevent="closePreview" @keydown="keepDialogFocus">
        <p class="font-semibold">{{ t("dataHealth.preview.title") }}</p>
        <p class="mt-2 text-sm text-ink-muted">{{ t(`dataHealth.preview.${previewAction}`) }}</p>
        <div class="mt-4 flex justify-end gap-2">
          <UiButton data-testid="data-health-action-cancel" @click="closePreview">{{ t("dataHealth.preview.cancel") }}</UiButton>
          <UiButton data-testid="data-health-action-confirm" variant="primary" @click="confirmAction">{{ t("dataHealth.preview.confirm") }}</UiButton>
        </div>
      </div>
    </div>
  </section>
</template>
