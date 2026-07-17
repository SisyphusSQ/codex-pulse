<script setup lang="ts">
import { X } from "@lucide/vue";
import { computed, defineAsyncComponent, ref } from "vue";
import { useI18n } from "vue-i18n";

import { ResponseStatus } from "@bindings/github.com/SisyphusSQ/codex-pulse/internal/query/models";
import type {
  AttributionValue,
  ProjectDetailResponse,
} from "@bindings/github.com/SisyphusSQ/codex-pulse/internal/query/usagecost/models";
import UiButton from "@/components/ui/UiButton.vue";
import {
  formatCompactTokens,
  formatCount,
  formatDateTime,
  formatMicroUSD,
  numericValue,
} from "@/features/overview/format";

const ProjectTrendChart = defineAsyncComponent(() => import("./ProjectTrendChart.vue"));

const props = withDefaults(defineProps<{
  detail: ProjectDetailResponse;
  hasPreviousModelPage: boolean;
  hasPreviousSessionPage: boolean;
  isFetching?: boolean;
  timeZone: string;
}>(), {
  isFetching: false,
});

defineEmits<{
  close: [];
  "next-model-page": [];
  "next-session-page": [];
  "previous-model-page": [];
  "previous-session-page": [];
}>();

const { t } = useI18n();
const heading = ref<HTMLElement | null>(null);
const models = computed(() => props.detail.models ?? []);
const sessions = computed(() => props.detail.sessions ?? []);
const itemTokens = computed(() => numericValue(props.detail.item.totals.totalTokens));

function focusHeading() {
  heading.value?.focus();
}

defineExpose({ focusHeading });

function attributionName(value: AttributionValue, fallbackKey: string) {
  return value.displayName || t(fallbackKey);
}

function confidenceLabel(value: string) {
  const key = ["high", "medium", "low"].includes(value) ? value : "unknown";
  return t(`projects.confidence.${key}`);
}

function progressValue(value: number | null) {
  if (value === null || itemTokens.value === null || itemTokens.value <= 0) return null;
  return Math.min(value, itemTokens.value);
}
</script>

<template>
  <aside
    class="content-surface rounded-content p-5 xl:max-h-[calc(100vh-13rem)] xl:overflow-y-auto"
    aria-labelledby="project-detail-title"
  >
    <div class="flex items-start justify-between gap-3">
      <div class="min-w-0">
        <div class="flex flex-wrap items-center gap-2">
          <span class="rounded-full bg-blue-50 px-2 py-1 text-[10px] font-semibold text-blue-800">
            {{ confidenceLabel(detail.item.project.confidence) }}
          </span>
          <span
            v-if="detail.meta.status === ResponseStatus.ResponsePartial"
            class="rounded-full bg-amber-50 px-2 py-1 text-[10px] font-semibold text-amber-800"
          >
            {{ t("projects.state.partial") }}
          </span>
          <span v-if="isFetching" class="text-[10px] text-ink-subtle">{{ t("projects.state.refreshing") }}</span>
        </div>
        <h2 ref="heading" id="project-detail-title" tabindex="-1" class="mt-3 truncate text-xl font-bold tracking-tight text-ink">
          {{ attributionName(detail.item.project, "projects.list.unknownProject") }}
        </h2>
        <p class="mt-1 text-xs text-ink-subtle">{{ t("projects.detail.safeIdentity") }}</p>
      </div>
      <button
        type="button"
        data-testid="project-detail-close"
        :aria-label="t('projects.detail.close')"
        class="flex size-9 shrink-0 items-center justify-center rounded-control text-ink-muted transition-colors hover:bg-black/5 hover:text-ink"
        @click="$emit('close')"
      >
        <X :size="18" aria-hidden="true" />
      </button>
    </div>

    <dl class="mt-4 grid grid-cols-2 gap-2">
      <div class="rounded-control bg-surface-base px-3 py-3">
        <dt class="text-[10px] text-ink-subtle">{{ t("projects.detail.sessions") }}</dt>
        <dd class="mt-1 text-sm font-semibold text-ink">
          {{ t("projects.list.sessionCount", { count: formatCount(detail.item.sessionCount) }) }}
        </dd>
      </div>
      <div class="rounded-control bg-surface-base px-3 py-3">
        <dt class="text-[10px] text-ink-subtle">{{ t("projects.detail.turns") }}</dt>
        <dd class="mt-1 font-mono text-sm font-semibold text-ink">{{ formatCount(detail.item.totals.turnCount) }}</dd>
      </div>
      <div class="rounded-control bg-surface-base px-3 py-3">
        <dt class="text-[10px] text-ink-subtle">{{ t("projects.detail.tokens") }}</dt>
        <dd class="mt-1 font-mono text-sm font-semibold text-accent">{{ formatCompactTokens(detail.item.totals.totalTokens) }}</dd>
      </div>
      <div class="rounded-control bg-surface-base px-3 py-3">
        <dt class="text-[10px] text-ink-subtle">{{ t("projects.detail.cost") }}</dt>
        <dd class="mt-1 font-mono text-sm font-semibold text-violet">{{ formatMicroUSD(detail.item.totals.estimatedUsdMicros) }}</dd>
      </div>
    </dl>

    <div class="mt-3 grid gap-x-4 rounded-control border border-line bg-white/55 px-3 py-2 text-[11px] leading-5 text-ink-subtle lg:grid-cols-2">
      <p>{{ t("projects.detail.lastActivity") }}：{{ formatDateTime(numericValue(detail.item.totals.lastActivityAtMs), detail.reportingTimeZone || timeZone) }}</p>
      <p>{{ t("projects.detail.pricingSource") }}：{{ detail.pricingSource || t("projects.common.unknown") }}</p>
      <p>{{ t("projects.detail.pricingVersions") }}：{{ detail.pricingVersions?.join(", ") || t("projects.common.unknown") }}</p>
      <p>
        {{ t("projects.detail.globalContext", {
          cost: formatMicroUSD(detail.globalTotals.estimatedUsdMicros),
          tokens: formatCompactTokens(detail.globalTotals.totalTokens),
        }) }}
      </p>
      <p>
        {{ t("projects.detail.pricedTurns", { count: formatCount(detail.item.totals.pricedTurnCount) }) }} ·
        {{ t("projects.detail.unpricedTurns", { count: formatCount(detail.item.totals.unpricedTurnCount) }) }}
      </p>
      <p class="lg:col-span-2">{{ t("projects.detail.priceNotice") }}</p>
    </div>

    <section class="mt-5" aria-labelledby="project-models-title">
      <h3 id="project-models-title" class="text-sm font-semibold text-ink">{{ t("projects.detail.models") }}</h3>
      <p class="mt-0.5 text-[11px] text-ink-subtle">{{ t("projects.detail.modelsDescription") }}</p>
      <p v-if="models.length === 0" class="mt-3 text-xs text-ink-muted">{{ t("projects.detail.modelsEmpty") }}</p>
      <ul v-else class="mt-3 space-y-2">
        <li v-for="model in models" :key="model.dimensionKey" class="rounded-control border border-line bg-white/60 px-3 py-3">
          <div class="flex items-start justify-between gap-3">
            <div class="min-w-0">
              <p class="truncate font-mono text-xs font-semibold text-ink">
                {{ attributionName(model.model, "projects.detail.unknownModel") }}
              </p>
              <p class="mt-0.5 text-[10px] text-ink-subtle">{{ confidenceLabel(model.model.confidence) }}</p>
            </div>
            <div class="text-right text-[11px] text-ink-muted">
              <p class="font-mono font-semibold text-ink">{{ formatCompactTokens(model.totals.totalTokens) }}</p>
              <p>{{ formatMicroUSD(model.totals.estimatedUsdMicros) }}</p>
            </div>
          </div>
          <progress
            v-if="progressValue(numericValue(model.totals.totalTokens)) !== null"
            class="project-model-progress mt-2 h-1.5 w-full"
            :aria-label="t('projects.detail.modelShare', { model: attributionName(model.model, 'projects.detail.unknownModel') })"
            :max="itemTokens ?? 1"
            :value="progressValue(numericValue(model.totals.totalTokens)) ?? 0"
          />
        </li>
      </ul>
      <div class="mt-3 flex items-center justify-between gap-2">
        <UiButton
          data-testid="project-model-previous"
          variant="quiet"
          :disabled="isFetching || !hasPreviousModelPage"
          @click="$emit('previous-model-page')"
        >
          {{ t("projects.pagination.previous") }}
        </UiButton>
        <UiButton
          data-testid="project-model-next"
          variant="quiet"
          :disabled="isFetching || !detail.modelPage.hasMore || !detail.modelPage.nextCursor"
          @click="$emit('next-model-page')"
        >
          {{ t("projects.pagination.next") }}
        </UiButton>
      </div>
    </section>

    <section class="mt-5" aria-labelledby="project-sessions-title">
      <h3 id="project-sessions-title" class="text-sm font-semibold text-ink">{{ t("projects.detail.recentSessions") }}</h3>
      <p class="mt-0.5 text-[11px] text-ink-subtle">{{ t("projects.detail.sessionsDescription") }}</p>
      <p v-if="sessions.length === 0" class="mt-3 text-xs text-ink-muted">{{ t("projects.detail.sessionsEmpty") }}</p>
      <ol v-else class="mt-3 space-y-2">
        <li v-for="session in sessions" :key="session.sessionId" class="rounded-control border border-line bg-white/60 px-3 py-3">
          <div class="flex items-start justify-between gap-3">
            <div class="min-w-0">
              <p class="truncate text-xs font-semibold text-ink">{{ session.displayTitle || t("projects.detail.unknownSession") }}</p>
              <p class="mt-0.5 truncate font-mono text-[10px] text-ink-subtle">
                {{ attributionName(session.model, "projects.detail.unknownModel") }} ·
                {{ session.activity === "active" ? t("projects.activity.active") : t("projects.activity.idle") }}
              </p>
            </div>
            <time class="shrink-0 text-[10px] text-ink-subtle">
              {{ formatDateTime(numericValue(session.lastActivityAtMs), detail.reportingTimeZone || timeZone) }}
            </time>
          </div>
          <p class="mt-2 text-[11px] text-ink-muted">
            {{ t("projects.detail.contributionTokens", { value: formatCompactTokens(session.totals.totalTokens) }) }} ·
            {{ t("projects.detail.contributionCost", { value: formatMicroUSD(session.totals.estimatedUsdMicros) }) }}
          </p>
        </li>
      </ol>
      <div class="mt-3 flex items-center justify-between gap-2">
        <UiButton
          data-testid="project-session-previous"
          variant="quiet"
          :disabled="isFetching || !hasPreviousSessionPage"
          @click="$emit('previous-session-page')"
        >
          {{ t("projects.pagination.previous") }}
        </UiButton>
        <UiButton
          data-testid="project-session-next"
          variant="quiet"
          :disabled="isFetching || !detail.sessionPage.hasMore || !detail.sessionPage.nextCursor"
          @click="$emit('next-session-page')"
        >
          {{ t("projects.pagination.next") }}
        </UiButton>
      </div>
    </section>

    <section class="mt-5" aria-labelledby="project-daily-title">
      <h3 id="project-daily-title" class="text-sm font-semibold text-ink">{{ t("projects.detail.daily") }}</h3>
      <p class="mt-0.5 text-[11px] text-ink-subtle">{{ t("projects.detail.dailyDescription") }}</p>
      <ProjectTrendChart
        v-if="detail.daily?.length"
        class="mt-2"
        :points="detail.daily"
        :time-zone="detail.reportingTimeZone || timeZone"
        :label="t('projects.detail.dailyAria')"
        :unit-label="t('projects.common.tokenUnit')"
      />
      <p v-else class="mt-3 rounded-control border border-dashed border-line px-3 py-5 text-center text-xs text-ink-muted">
        {{ t("projects.detail.dailyEmpty") }}
      </p>
    </section>

    <p class="mt-4 rounded-control bg-blue-50/80 px-3 py-2 text-[10px] leading-5 text-blue-900/70">
      {{ t("projects.detail.privacy") }}
    </p>
  </aside>
</template>

<style scoped>
.project-model-progress {
  appearance: none;
  overflow: hidden;
  border: 0;
  border-radius: 9999px;
  background: var(--color-line);
}

.project-model-progress::-webkit-progress-bar {
  border-radius: 9999px;
  background: var(--color-line);
}

.project-model-progress::-webkit-progress-value {
  border-radius: 9999px;
  background: var(--color-accent);
}
</style>
