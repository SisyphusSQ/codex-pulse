<script setup lang="ts">
import {
  Database,
  FolderKanban,
  HeartPulse,
  MessagesSquare,
} from "@lucide/vue";
import { computed, defineAsyncComponent, ref } from "vue";
import { useI18n } from "vue-i18n";

import { NumericUnit, ResponseStatus, type NumericValue } from "@bindings/github.com/SisyphusSQ/codex-pulse/internal/query/models";
import type { ResponseMeta } from "@bindings/github.com/SisyphusSQ/codex-pulse/internal/query/models";
import type { CurrentWindow } from "@bindings/github.com/SisyphusSQ/codex-pulse/internal/codex/quota/models";
import StateEmpty from "@/components/ui/StateEmpty.vue";
import UiTable, { type UiTableColumn } from "@/components/ui/UiTable.vue";
import OverviewRangePicker, {
  type OverviewRangeSelection,
} from "@/features/overview/OverviewRangePicker.vue";
import OverviewRegion from "@/features/overview/OverviewRegion.vue";
import {
  formatCompactTokens,
  formatCount,
  formatDateTime,
  formatMicroUSD,
  formatOverviewDay,
  formatPercent,
  formatQuotaWindowFreshness,
  formatSourceFreshness,
  numericValue,
} from "@/features/overview/format";
import {
  createCustomOverviewRange,
  createOverviewPresetRange,
  type OverviewRangePreset,
} from "@/features/overview/range";
import { useOverviewQueries } from "@/features/overview/useOverviewQueries";

defineOptions({ name: "OverviewView" });

const UsageTrendChart = defineAsyncComponent(
  () => import("@/features/overview/UsageTrendChart.vue"),
);

const { t } = useI18n();
const resolvedTimeZone = Intl.DateTimeFormat().resolvedOptions().timeZone;
const timeZone = resolvedTimeZone && resolvedTimeZone !== "Local" ? resolvedTimeZone : "UTC";
const rangeSelection = ref<OverviewRangeSelection>("7d");
const range = ref(createOverviewPresetRange("7d", Date.now(), timeZone));
const rangeError = ref("");
const queries = useOverviewQueries(range);

const usageData = computed(() => queries.usage.data.value);
const quotaData = computed(() => queries.quota.data.value);
const sessionData = computed(() => queries.sessions.data.value);
const projectData = computed(() => queries.projects.data.value);
const sourceData = computed(() => queries.sources.data.value);
const healthData = computed(() => queries.health.data.value);

const usageHasData = computed(() => usageData.value !== undefined);
const quotaHasData = computed(() => quotaData.value !== undefined);
const sessionsHasData = computed(() => sessionData.value !== undefined);
const projectsHasData = computed(() => projectData.value !== undefined);
const sourcesHasData = computed(() => sourceData.value !== undefined);
const healthHasData = computed(() => healthData.value !== undefined);

const quotaWindows = computed(() => quotaData.value?.current.windows ?? []);
const quotaHasResetCredits = computed(() => (
  typeof quotaData.value?.current.resetCredits.availableCount === "number"
  || typeof quotaData.value?.current.resetCredits.totalCount === "number"
));
const quotaEmpty = computed(() => quotaWindows.value.length === 0 && !quotaHasResetCredits.value);
const usageTrend = computed(() => usageData.value?.trend ?? []);
const sessions = computed(() => sessionData.value?.items ?? []);
const projects = computed(() => projectData.value?.items ?? []);

const usageEmpty = computed(() => (
  usageTrend.value.length === 0
  && numericValue(usageData.value?.totals.turnCount ?? unknownCount) === 0
));
const dailyRows = computed(() => usageTrend.value.map((point) => ({
  cached: formatCompactTokens(point.totals.cachedInputTokens),
  cost: formatMicroUSD(point.totals.estimatedUsdMicros),
  date: formatOverviewDay(
    numericValue(point.startAtMs),
    point.key,
    usageData.value?.reportingTimeZone || timeZone,
  ),
  key: point.key,
  output: formatCompactTokens(point.totals.outputTokens),
  tokens: formatCompactTokens(point.totals.totalTokens),
})));

const unknownCount: NumericValue = {
  unit: NumericUnit.NumericCount,
  unknownReason: null,
  value: null,
};

const dailyColumns = computed<UiTableColumn[]>(() => [
  { key: "date", label: t("overview.daily.date") },
  { key: "tokens", label: t("overview.daily.tokens") },
  { key: "cached", label: t("overview.daily.cached") },
  { key: "output", label: t("overview.daily.output") },
  { key: "cost", label: t("overview.daily.cost") },
]);

const chartLabels = computed(() => ({
  cached: t("overview.usage.cached"),
  input: t("overview.usage.input"),
  output: t("overview.usage.output"),
  reasoning: t("overview.usage.reasoning"),
  unit: t("overview.usage.unit"),
}));

const presetLabels = computed<Record<OverviewRangePreset, string>>(() => ({
  "30d": t("overview.range.thirtyDays"),
  "7d": t("overview.range.sevenDays"),
  today: t("overview.range.today"),
}));

function partial(meta: ResponseMeta | undefined) {
  return meta?.status === ResponseStatus.ResponsePartial;
}

function stale(query: { isError: { value: boolean }; isPlaceholderData: { value: boolean } }) {
  return query.isError.value || query.isPlaceholderData.value;
}

function selectRange(selection: OverviewRangeSelection) {
  rangeSelection.value = selection;
  rangeError.value = "";
  if (selection !== "custom") {
    range.value = createOverviewPresetRange(selection, Date.now(), timeZone);
  }
}

function applyCustomRange(value: { startDate: string; endDateExclusive: string }) {
  try {
    range.value = createCustomOverviewRange(value.startDate, value.endDateExclusive, timeZone);
    rangeSelection.value = "custom";
    rangeError.value = "";
  } catch {
    rangeError.value = t("overview.range.invalid");
  }
}

function quotaTitle(window: CurrentWindow) {
  if (window.windowKind === "primary") return t("overview.quota.primary");
  if (window.windowKind === "secondary") return t("overview.quota.secondary");
  return t("overview.quota.other");
}

function resetLabel(valueMS: number | null) {
  if (valueMS === null) return t("overview.quota.resetUnknown");
  const minutes = Math.max(1, Math.ceil(valueMS / 60_000));
  if (minutes < 120) return t("overview.quota.resetsMinutes", { value: minutes });
  const hours = Math.ceil(minutes / 60);
  if (hours < 48) return t("overview.quota.resetsHours", { value: hours });
  return t("overview.quota.resetsDays", { value: Math.ceil(hours / 24) });
}

function sourceLabel(source: string | null) {
  if (source === "wham") return t("overview.quota.sourceWham");
  if (source === "local_jsonl" || source === "local") return t("overview.quota.sourceLocal");
  return t("overview.quota.sourceUnknown");
}

const freshnessLabels = computed(() => ({
  current: t("overview.quota.freshnessFresh"),
  lastKnown: t("overview.quota.freshnessLastKnown"),
  stale: t("overview.quota.freshnessStale"),
  unavailable: t("overview.quota.freshnessUnavailable"),
  unknown: t("overview.quota.freshnessUnknown"),
}));

function healthLevelLabel(level: string | undefined) {
  const key = ["healthy", "busy", "paused", "degraded", "blocked"].includes(level ?? "")
    ? level
    : "unknown";
  return t(`overview.runtime.${key}`);
}

function unpricedReasonLabel(reason: string) {
  const keys: Record<string, string> = {
    catalog_not_effective: "overview.cost.reasonCatalogNotEffective",
    conflict_model: "overview.cost.reasonConflictModel",
    invalid_model: "overview.cost.reasonInvalidModel",
    missing_attribution: "overview.cost.reasonMissingAttribution",
    missing_model: "overview.cost.reasonMissingModel",
    missing_price_component: "overview.cost.reasonMissingPriceComponent",
    missing_token: "overview.cost.reasonMissingToken",
    model_not_listed: "overview.cost.reasonModelNotListed",
  };
  return t(keys[reason] ?? "overview.cost.reasonUnknown");
}

function safeDisplayName(value: { displayName: string | null } | null | undefined, fallback: string) {
  return value?.displayName || fallback;
}
</script>

<template>
  <section data-testid="overview-view" class="w-full space-y-4 py-1">
    <OverviewRegion
      data-testid="quota-summary"
      bare
      :title="t('overview.quota.title')"
      :has-data="quotaHasData"
      :is-empty="quotaEmpty"
      :is-error="queries.quota.isError.value"
      :is-fetching="queries.quota.isFetching.value"
      :is-pending="queries.quota.isPending.value"
      :is-partial="partial(quotaData?.meta)"
      :is-stale="stale(queries.quota)"
      :loading-label="t('overview.state.loading')"
      :error-title="t('overview.state.errorTitle')"
      :error-description="t('overview.state.errorDescription')"
      :empty-title="t('overview.state.emptyTitle')"
      :empty-description="t('overview.state.emptyDescription')"
      :action-label="t('overview.state.retry')"
      :partial-label="t('overview.state.partial')"
      :stale-label="t('overview.state.stale')"
      @retry="queries.quota.refetch()"
    >
      <div class="grid gap-3 lg:grid-cols-2">
        <article
          v-for="(window, index) in quotaWindows"
          :key="window.windowKind"
          class="content-surface rounded-content px-5 py-3"
        >
          <div class="flex items-center justify-between gap-4">
            <p class="text-xs font-semibold text-ink-muted">
              {{ quotaTitle(window) }}
              <span
                v-if="window.windowKind === 'secondary' || (index === 0 && !quotaWindows.some((item) => item.windowKind === 'secondary'))"
                class="ml-1 font-normal text-ink-subtle"
              >
                · {{ t("overview.quota.resetCredits", {
                  available: quotaData?.current.resetCredits.availableCount ?? t("overview.common.unknown"),
                  total: quotaData?.current.resetCredits.totalCount ?? t("overview.common.unknown"),
                }) }}
              </span>
            </p>
            <p class="text-2xl font-bold tracking-tight text-ink">
              {{ formatPercent(window.remainingPercent) }}
            </p>
          </div>
          <div
            class="mt-1.5 h-1.5 overflow-hidden rounded-full bg-slate-100"
            :data-testid="window.remainingPercent === null
              ? 'quota-progress-unknown'
              : window.remainingPercent === 0
                ? 'quota-progress-zero'
                : 'quota-progress-known'"
            :role="window.remainingPercent === null ? undefined : 'progressbar'"
            :aria-valuemin="window.remainingPercent === null ? undefined : 0"
            :aria-valuemax="window.remainingPercent === null ? undefined : 100"
            :aria-valuenow="window.remainingPercent ?? undefined"
          >
            <div
              v-if="window.remainingPercent !== null"
              class="h-full rounded-full"
              :class="window.windowKind === 'secondary' ? 'bg-violet' : 'bg-accent'"
              :style="{
                minWidth: window.remainingPercent === 0 ? '3px' : undefined,
                width: `${Math.max(0, Math.min(100, window.remainingPercent))}%`,
              }"
            />
          </div>
          <div class="mt-1.5 flex flex-wrap justify-between gap-2 text-[11px] text-ink-subtle">
            <span>{{ resetLabel(window.resetRemainingMs) }}</span>
            <span>
              {{ t("overview.quota.source", { source: sourceLabel(window.selectedSource) }) }} ·
              {{ formatQuotaWindowFreshness(window.freshness, freshnessLabels) }}
            </span>
          </div>
        </article>
        <article
          v-if="quotaWindows.length === 0 && quotaHasResetCredits"
          class="content-surface rounded-content px-5 py-4"
        >
          <p class="text-xs font-semibold text-ink-muted">
            {{ t("overview.quota.resetCredits", {
              available: quotaData?.current.resetCredits.availableCount ?? t("overview.common.unknown"),
              total: quotaData?.current.resetCredits.totalCount ?? t("overview.common.unknown"),
            }) }}
          </p>
          <p class="mt-2 text-[11px] text-ink-subtle">
            {{ formatSourceFreshness(quotaData!.current.resetCredits.freshness, freshnessLabels) }}
          </p>
        </article>
      </div>
    </OverviewRegion>

    <OverviewRegion
      :data-testid="queries.usage.isError.value && !usageHasData ? 'usage-error' : 'usage-summary'"
      :title="t('overview.usage.trendTitle')"
      :has-data="usageHasData"
      :is-empty="false"
      :is-error="queries.usage.isError.value"
      :is-fetching="queries.usage.isFetching.value"
      :is-pending="queries.usage.isPending.value"
      :is-partial="partial(usageData?.meta)"
      :is-stale="stale(queries.usage)"
      :loading-label="t('overview.state.loading')"
      :error-title="t('overview.state.errorTitle')"
      :error-description="t('overview.state.errorDescription')"
      :empty-title="t('overview.state.emptyTitle')"
      :empty-description="t('overview.state.emptyDescription')"
      :action-label="t('overview.state.retry')"
      :partial-label="t('overview.state.partial')"
      :stale-label="t('overview.state.stale')"
      @retry="queries.usage.refetch()"
    >
      <div class="flex flex-wrap items-start justify-between gap-4">
        <div>
          <p class="text-xs font-medium text-ink-subtle">{{ t("overview.usage.total") }}</p>
          <p class="mt-1 text-3xl font-bold tracking-tight text-ink">
            {{ usageData ? formatCompactTokens(usageData.totals.totalTokens) : "--" }}
          </p>
        </div>
        <div class="flex flex-col items-end gap-2">
          <OverviewRangePicker
            :model-value="rangeSelection"
            :preset-labels="presetLabels"
            :custom-label="t('overview.range.custom')"
            :start-label="t('overview.range.start')"
            :end-label="t('overview.range.end')"
            :apply-label="t('overview.range.apply')"
            :custom-start="range.startDate"
            :custom-end-exclusive="range.endDateExclusive"
            @update:model-value="selectRange"
            @apply-custom="applyCustomRange"
          />
          <p class="text-[11px] text-ink-subtle">
            {{ t("overview.range.timeZone", { timeZone: range.timeZone }) }}
          </p>
          <p v-if="rangeError" role="alert" class="text-xs font-medium text-critical">
            {{ rangeError }}
          </p>
        </div>
      </div>
      <StateEmpty
        v-if="usageEmpty"
        class="mt-4"
        :title="t('overview.state.emptyTitle')"
        :description="t('overview.state.emptyDescription')"
      />
      <UsageTrendChart
        v-else
        class="mt-3"
        :points="usageTrend"
        :labels="chartLabels"
        :time-zone="usageData?.reportingTimeZone || timeZone"
        :label="t('overview.usage.trendAria')"
      />
    </OverviewRegion>

    <div
      v-if="usageHasData"
      class="grid gap-4 xl:grid-cols-2"
    >
      <OverviewRegion
        data-testid="token-composition"
        :title="t('overview.usage.compositionTitle')"
        :has-data="usageHasData"
        :is-empty="usageEmpty"
        :is-error="queries.usage.isError.value"
        :is-fetching="queries.usage.isFetching.value"
        :is-pending="queries.usage.isPending.value"
        :is-partial="partial(usageData?.meta)"
        :is-stale="stale(queries.usage)"
        :loading-label="t('overview.state.loading')"
        :error-title="t('overview.state.errorTitle')"
        :error-description="t('overview.state.errorDescription')"
        :empty-title="t('overview.state.emptyTitle')"
        :empty-description="t('overview.state.emptyDescription')"
        :action-label="t('overview.state.retry')"
        :partial-label="t('overview.state.partial')"
        :stale-label="t('overview.state.stale')"
        :show-status="false"
        @retry="queries.usage.refetch()"
      >
        <dl class="space-y-3">
          <div
            v-for="(metric, index) in usageData ? [
              [t('overview.usage.input'), usageData.totals.inputTokens],
              [t('overview.usage.cached'), usageData.totals.cachedInputTokens],
              [t('overview.usage.output'), usageData.totals.outputTokens],
              [t('overview.usage.reasoning'), usageData.totals.reasoningTokens],
            ] : []"
            :key="String(metric[0])"
            class="flex items-center gap-3"
          >
            <span
              aria-hidden="true"
              class="size-2 rounded-full"
              :class="['bg-accent', 'bg-sky-400', 'bg-violet', 'bg-caution'][index]"
            />
            <dt class="text-xs text-ink-muted">{{ metric[0] }}</dt>
            <dd class="ml-auto text-sm font-semibold text-ink">{{ formatCompactTokens(metric[1] as NumericValue) }}</dd>
          </div>
        </dl>
      </OverviewRegion>

      <OverviewRegion
        data-testid="cost-summary"
        :title="t('overview.cost.title')"
        :has-data="usageHasData"
        :is-empty="usageEmpty"
        :is-error="queries.usage.isError.value"
        :is-fetching="queries.usage.isFetching.value"
        :is-pending="queries.usage.isPending.value"
        :is-partial="partial(usageData?.meta)"
        :is-stale="stale(queries.usage)"
        :loading-label="t('overview.state.loading')"
        :error-title="t('overview.state.errorTitle')"
        :error-description="t('overview.state.errorDescription')"
        :empty-title="t('overview.state.emptyTitle')"
        :empty-description="t('overview.state.emptyDescription')"
        :action-label="t('overview.state.retry')"
        :partial-label="t('overview.state.partial')"
        :stale-label="t('overview.state.stale')"
        :show-status="false"
        @retry="queries.usage.refetch()"
      >
        <div class="flex items-start justify-between gap-3">
          <div>
            <p class="text-xs text-ink-subtle">{{ t("overview.cost.estimate") }}</p>
            <p class="mt-1 text-3xl font-bold tracking-tight text-ink">
              {{ usageData ? formatMicroUSD(usageData.totals.estimatedUsdMicros) : "--" }}
            </p>
          </div>
          <p class="max-w-[50%] text-right text-[11px] leading-5 text-ink-subtle">
            {{ usageData?.pricingSource || t("overview.cost.sourceUnknown") }}<br />
            {{ usageData?.pricingVersions?.join(", ") || t("overview.cost.versionUnknown") }}
          </p>
        </div>
        <p class="mt-2 text-xs leading-5 text-ink-subtle">{{ t("overview.cost.description") }}</p>
        <div
          class="mt-4 rounded-control bg-amber-50 px-3 py-2 text-[11px] text-amber-900"
        >
          <span class="font-semibold">{{ t("overview.cost.unpricedTurns") }}：</span>
          {{ usageData ? formatCount(usageData.totals.unpricedTurnCount) : "--" }}
          <template v-if="usageData?.unpricedReasons?.length">
            · {{ t("overview.cost.unpricedReasons") }}：
            {{ usageData.unpricedReasons.map((item) => `${unpricedReasonLabel(item.reason)} (${formatCount(item.count)})`).join(", ") }}
          </template>
        </div>
      </OverviewRegion>
    </div>

    <OverviewRegion
      v-if="usageHasData"
      data-testid="daily-table"
      :title="t('overview.daily.title')"
      :has-data="usageHasData"
      :is-empty="dailyRows.length === 0"
      :is-error="queries.usage.isError.value"
      :is-fetching="queries.usage.isFetching.value"
      :is-pending="queries.usage.isPending.value"
      :is-partial="partial(usageData?.meta)"
      :is-stale="stale(queries.usage)"
      :loading-label="t('overview.state.loading')"
      :error-title="t('overview.state.errorTitle')"
      :error-description="t('overview.state.errorDescription')"
      :empty-title="t('overview.state.emptyTitle')"
      :empty-description="t('overview.state.emptyDescription')"
      :action-label="t('overview.state.retry')"
      :partial-label="t('overview.state.partial')"
      :stale-label="t('overview.state.stale')"
      :show-status="false"
      @retry="queries.usage.refetch()"
    >
      <UiTable
        :caption="t('overview.daily.caption')"
        :columns="dailyColumns"
        :rows="dailyRows"
        row-key="key"
      />
    </OverviewRegion>

    <div class="grid gap-4 xl:grid-cols-2">
      <OverviewRegion
        data-testid="recent-sessions"
        :title="t('overview.sessions.title')"
        :has-data="sessionsHasData"
        :is-empty="sessions.length === 0"
        :is-error="queries.sessions.isError.value"
        :is-fetching="queries.sessions.isFetching.value"
        :is-pending="queries.sessions.isPending.value"
        :is-partial="partial(sessionData?.meta)"
        :is-stale="stale(queries.sessions)"
        :loading-label="t('overview.state.loading')"
        :error-title="t('overview.state.errorTitle')"
        :error-description="t('overview.state.errorDescription')"
        :empty-title="t('overview.state.emptyTitle')"
        :empty-description="t('overview.state.emptyDescription')"
        :action-label="t('overview.state.retry')"
        :partial-label="t('overview.state.partial')"
        :stale-label="t('overview.state.stale')"
        @retry="queries.sessions.refetch()"
      >
        <ul class="divide-y divide-line">
          <li v-for="session in sessions" :key="session.sessionId" class="flex items-center gap-3 py-3 first:pt-0 last:pb-0">
            <span class="flex size-9 shrink-0 items-center justify-center rounded-control bg-blue-50 text-accent">
              <MessagesSquare :size="16" aria-hidden="true" />
            </span>
            <div class="min-w-0 flex-1">
              <p class="truncate text-sm font-semibold text-ink">{{ session.displayTitle || t("overview.sessions.unknownTitle") }}</p>
              <p class="mt-0.5 truncate text-xs text-ink-subtle">
                {{ safeDisplayName(session.project, t("overview.sessions.unknownProject")) }} ·
                {{ safeDisplayName(session.model, t("overview.sessions.unknownModel")) }}
              </p>
            </div>
            <div class="text-right">
              <p class="text-sm font-semibold text-ink">{{ formatCompactTokens(session.totals.totalTokens) }}</p>
              <p class="mt-0.5 text-[11px] text-ink-subtle">{{ formatDateTime(numericValue(session.lastActivityAtMs), timeZone) }}</p>
            </div>
          </li>
        </ul>
      </OverviewRegion>

      <OverviewRegion
        data-testid="recent-projects"
        :title="t('overview.projects.title')"
        :has-data="projectsHasData"
        :is-empty="projects.length === 0"
        :is-error="queries.projects.isError.value"
        :is-fetching="queries.projects.isFetching.value"
        :is-pending="queries.projects.isPending.value"
        :is-partial="partial(projectData?.meta)"
        :is-stale="stale(queries.projects)"
        :loading-label="t('overview.state.loading')"
        :error-title="t('overview.state.errorTitle')"
        :error-description="t('overview.state.errorDescription')"
        :empty-title="t('overview.state.emptyTitle')"
        :empty-description="t('overview.state.emptyDescription')"
        :action-label="t('overview.state.retry')"
        :partial-label="t('overview.state.partial')"
        :stale-label="t('overview.state.stale')"
        @retry="queries.projects.refetch()"
      >
        <ul class="divide-y divide-line">
          <li v-for="project in projects" :key="project.dimensionKey" class="flex items-center gap-3 py-3 first:pt-0 last:pb-0">
            <span class="flex size-9 shrink-0 items-center justify-center rounded-control bg-violet/10 text-violet">
              <FolderKanban :size="16" aria-hidden="true" />
            </span>
            <p class="min-w-0 flex-1 truncate text-sm font-semibold text-ink">
              {{ safeDisplayName(project.project, t("overview.projects.unknownProject")) }}
            </p>
            <div class="text-right">
              <p class="text-sm font-semibold text-ink">{{ formatCompactTokens(project.totals.totalTokens) }}</p>
              <p class="mt-0.5 text-[11px] text-ink-subtle">{{ formatMicroUSD(project.totals.estimatedUsdMicros) }}</p>
            </div>
          </li>
        </ul>
      </OverviewRegion>
    </div>

    <section data-testid="index-health-summary" class="grid gap-4 xl:grid-cols-2">
      <OverviewRegion
        :title="t('overview.runtime.title')"
        :has-data="sourcesHasData"
        :is-empty="false"
        :is-error="queries.sources.isError.value"
        :is-fetching="queries.sources.isFetching.value"
        :is-pending="queries.sources.isPending.value"
        :is-partial="partial(sourceData?.meta)"
        :is-stale="stale(queries.sources)"
        :loading-label="t('overview.state.loading')"
        :error-title="t('overview.state.errorTitle')"
        :error-description="t('overview.state.errorDescription')"
        :empty-title="t('overview.state.emptyTitle')"
        :empty-description="t('overview.state.emptyDescription')"
        :action-label="t('overview.state.retry')"
        :partial-label="t('overview.state.partial')"
        :stale-label="t('overview.state.stale')"
        @retry="queries.sources.refetch()"
      >
        <div class="flex items-center gap-3">
          <span class="flex size-10 items-center justify-center rounded-full bg-blue-50 text-accent">
            <Database :size="18" aria-hidden="true" />
          </span>
          <div>
            <p class="text-xs text-ink-subtle">{{ t("overview.runtime.indexedFiles") }}</p>
            <p class="mt-1 text-xl font-bold text-ink">{{ sourceData ? formatCount(sourceData.summary.localFiles) : "--" }}</p>
          </div>
          <div class="ml-auto text-right">
            <p class="text-xs text-ink-subtle">{{ t("overview.runtime.attentionSources") }}</p>
            <p class="mt-1 text-xl font-bold text-ink">{{ sourceData ? formatCount(sourceData.summary.attention) : "--" }}</p>
          </div>
        </div>
      </OverviewRegion>

      <OverviewRegion
        :title="t('overview.runtime.title')"
        :has-data="healthHasData"
        :is-empty="false"
        :is-error="queries.health.isError.value"
        :is-fetching="queries.health.isFetching.value"
        :is-pending="queries.health.isPending.value"
        :is-partial="partial(healthData?.meta)"
        :is-stale="stale(queries.health)"
        :loading-label="t('overview.state.loading')"
        :error-title="t('overview.state.errorTitle')"
        :error-description="t('overview.state.errorDescription')"
        :empty-title="t('overview.state.emptyTitle')"
        :empty-description="t('overview.state.emptyDescription')"
        :action-label="t('overview.state.retry')"
        :partial-label="t('overview.state.partial')"
        :stale-label="t('overview.state.stale')"
        @retry="queries.health.refetch()"
      >
        <div class="flex items-center gap-3">
          <span class="flex size-10 items-center justify-center rounded-full bg-emerald-50 text-healthy">
            <HeartPulse :size="18" aria-hidden="true" />
          </span>
          <div>
            <p class="text-xs text-ink-subtle">{{ t("overview.runtime.level") }}</p>
            <p class="mt-1 text-xl font-bold text-ink">{{ healthLevelLabel(healthData?.summary.level) }}</p>
          </div>
          <div class="ml-auto text-right">
            <p class="text-xs text-ink-subtle">{{ t("overview.runtime.activeHealth") }}</p>
            <p class="mt-1 text-xl font-bold text-ink">{{ healthData ? formatCount(healthData.summary.active) : "--" }}</p>
          </div>
        </div>
      </OverviewRegion>
    </section>
  </section>
</template>
