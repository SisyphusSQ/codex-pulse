<script setup lang="ts">
import { X } from "@lucide/vue";
import { computed, ref } from "vue";
import { useI18n } from "vue-i18n";

import { ResponseStatus } from "@bindings/github.com/SisyphusSQ/codex-pulse/internal/query/models";
import {
  SessionTurnPricingStatus,
  SessionTurnState,
  type SessionDetailResponse,
} from "@bindings/github.com/SisyphusSQ/codex-pulse/internal/query/usagecost/models";
import UiButton from "@/components/ui/UiButton.vue";
import {
  formatCompactTokens,
  formatCount,
  formatDateTime,
  formatMicroUSD,
  numericValue,
} from "@/features/overview/format";

const props = withDefaults(defineProps<{
  detail: SessionDetailResponse;
  hasPreviousTurnPage: boolean;
  isFetching?: boolean;
  timeZone: string;
}>(), {
  isFetching: false,
});

defineEmits<{
  close: [];
  "next-turn-page": [];
  "previous-turn-page": [];
}>();

const { t } = useI18n();
const turns = computed(() => props.detail.turns ?? []);
const heading = ref<HTMLElement | null>(null);

function focusHeading() {
  heading.value?.focus();
}

defineExpose({ focusHeading });

function attributionName(value: { displayName: string | null }, fallbackKey: string) {
  return value.displayName || t(fallbackKey);
}

function turnStateLabel(state: SessionTurnState) {
  return state === SessionTurnState.SessionTurnActive
    ? t("sessions.detail.turn.active")
    : t("sessions.detail.turn.complete");
}

function pricingStatusLabel(status: SessionTurnPricingStatus) {
  switch (status) {
    case SessionTurnPricingStatus.SessionTurnPricingPriced:
      return t("sessions.detail.pricing.priced");
    case SessionTurnPricingStatus.SessionTurnPricingUnpriced:
      return t("sessions.detail.pricing.unpriced");
    default:
      return t("sessions.detail.pricing.unknown");
  }
}

function unpricedReasonLabel(reason: string | null) {
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
  return t(keys[reason ?? ""] ?? "overview.cost.reasonUnknown");
}
</script>

<template>
  <aside
    class="content-surface rounded-content p-5 xl:max-h-[calc(100vh-13rem)] xl:overflow-y-auto"
    aria-labelledby="session-detail-title"
  >
    <div class="flex items-start justify-between gap-3">
      <div class="min-w-0">
        <div class="flex flex-wrap items-center gap-2">
          <span
            class="inline-flex rounded-full px-2 py-1 text-[10px] font-bold tracking-wide"
            :class="detail.item.activity === 'active'
              ? 'bg-emerald-50 text-emerald-700'
              : 'bg-slate-100 text-slate-500'"
          >
            {{ detail.item.activity === "active" ? t("sessions.activity.active") : t("sessions.activity.idle") }}
          </span>
          <span
            v-if="detail.meta.status === ResponseStatus.ResponsePartial"
            class="rounded-full bg-amber-50 px-2 py-1 text-[10px] font-semibold text-amber-800"
          >
            {{ t("sessions.state.partial") }}
          </span>
          <span v-if="isFetching" class="text-[10px] text-ink-subtle">{{ t("sessions.state.refreshing") }}</span>
        </div>
        <h2 ref="heading" id="session-detail-title" tabindex="-1" class="mt-3 truncate text-xl font-bold tracking-tight text-ink">
          {{ detail.item.displayTitle || t("sessions.table.unknownTitle") }}
        </h2>
        <p class="mt-1 truncate text-xs text-ink-subtle">
          {{ attributionName(detail.item.project, "sessions.table.unknownProject") }}
        </p>
      </div>
      <button
        type="button"
        data-testid="detail-close"
        :aria-label="t('sessions.detail.close')"
        class="flex size-9 shrink-0 items-center justify-center rounded-control text-ink-muted transition-colors hover:bg-black/5 hover:text-ink"
        @click="$emit('close')"
      >
        <X :size="18" aria-hidden="true" />
      </button>
    </div>

    <dl class="mt-4 grid grid-cols-2 gap-2">
      <div class="rounded-control bg-surface-base px-3 py-3">
        <dt class="text-[10px] text-ink-subtle">{{ t("sessions.detail.model") }}</dt>
        <dd class="mt-1 truncate font-mono text-[11px] font-semibold text-ink">
          {{ attributionName(detail.item.model, "sessions.table.unknownModel") }}
        </dd>
      </div>
      <div class="rounded-control bg-surface-base px-3 py-3">
        <dt class="text-[10px] text-ink-subtle">{{ t("sessions.detail.turns") }}</dt>
        <dd class="mt-1 font-mono text-sm font-semibold text-ink">
          {{ t("sessions.detail.turnCount", { count: formatCount(detail.item.totals.turnCount) }) }}
        </dd>
      </div>
      <div class="rounded-control bg-surface-base px-3 py-3">
        <dt class="text-[10px] text-ink-subtle">{{ t("sessions.detail.tokens") }}</dt>
        <dd class="mt-1 font-mono text-sm font-semibold text-accent">
          {{ formatCompactTokens(detail.item.totals.totalTokens) }}
        </dd>
      </div>
      <div class="rounded-control bg-surface-base px-3 py-3">
        <dt class="text-[10px] text-ink-subtle">{{ t("sessions.detail.cost") }}</dt>
        <dd class="mt-1 font-mono text-sm font-semibold text-violet">
          {{ formatMicroUSD(detail.item.totals.estimatedUsdMicros) }}
        </dd>
      </div>
    </dl>

    <div class="mt-3 rounded-control border border-line bg-white/55 px-3 py-2 text-[11px] leading-5 text-ink-subtle">
      <p>{{ t("sessions.detail.lastActivity") }}：{{ formatDateTime(numericValue(detail.item.lastActivityAtMs), timeZone) }}</p>
      <p>
        {{ t("sessions.detail.pricingVersions") }}：
        {{ detail.pricingVersions?.join(", ") || t("sessions.detail.pricing.versionUnknown") }}
      </p>
      <p v-if="detail.unpricedReasons?.length">
        {{ t("sessions.detail.unpricedReasons") }}：
        {{ detail.unpricedReasons.map((item) => `${unpricedReasonLabel(item.reason)} (${formatCount(item.count)})`).join(", ") }}
      </p>
    </div>

    <section class="mt-5" aria-labelledby="session-turn-timeline-title">
      <div class="flex items-center justify-between gap-3">
        <div>
          <h3 id="session-turn-timeline-title" class="text-sm font-semibold text-ink">
            {{ t("sessions.detail.timeline") }}
          </h3>
          <p class="mt-0.5 text-[11px] text-ink-subtle">{{ t("sessions.detail.timelineDescription") }}</p>
        </div>
      </div>

      <div
        v-if="turns.length === 0"
        role="status"
        class="mt-3 rounded-control border border-dashed border-line px-3 py-6 text-center text-xs text-ink-muted"
      >
        {{ t("sessions.detail.turn.empty") }}
      </div>
      <ol v-else class="mt-3 space-y-2">
        <li
          v-for="turn in turns"
          :key="turn.timelineKey"
          class="rounded-control border border-line bg-white/60 px-3 py-3"
        >
          <div class="flex items-start gap-3">
            <span
              aria-hidden="true"
              class="mt-1.5 size-2 shrink-0 rounded-full"
              :class="turn.state === SessionTurnState.SessionTurnActive ? 'bg-healthy' : 'bg-accent'"
            />
            <div class="min-w-0 flex-1">
              <div class="flex flex-wrap items-center justify-between gap-2">
                <p class="text-xs font-semibold text-ink">{{ turnStateLabel(turn.state) }}</p>
                <span class="text-[10px] text-ink-subtle">
                  {{ t("sessions.detail.turn.startedAt") }}
                  {{ formatDateTime(numericValue(turn.startedAtMs), timeZone) }}
                </span>
              </div>
              <p class="mt-1 truncate font-mono text-[10px] text-ink-subtle">
                {{ attributionName(turn.model, "sessions.table.unknownModel") }}
              </p>
              <div class="mt-1 flex flex-wrap gap-x-3 text-[10px] text-ink-subtle">
                <span>
                  {{ t("sessions.detail.turn.observedAt") }}
                  {{ formatDateTime(numericValue(turn.observedAtMs), timeZone) }}
                </span>
                <span>
                  {{ t("sessions.detail.turn.completedAt") }}
                  {{ formatDateTime(numericValue(turn.completedAtMs), timeZone) }}
                </span>
              </div>
              <div class="mt-2 flex flex-wrap gap-x-4 gap-y-1 text-[11px] text-ink-muted">
                <span>{{ t("sessions.detail.tokens") }} {{ formatCompactTokens(turn.totals.totalTokens) }}</span>
                <span>{{ t("sessions.detail.cost") }} {{ formatMicroUSD(turn.totals.estimatedUsdMicros) }}</span>
                <span>
                  {{ pricingStatusLabel(turn.pricingStatus) }}
                  <template v-if="turn.pricingVersion"> · {{ turn.pricingVersion }}</template>
                  <template v-else-if="turn.unpricedReason"> · {{ unpricedReasonLabel(turn.unpricedReason) }}</template>
                </span>
              </div>
            </div>
          </div>
        </li>
      </ol>

      <div class="mt-3 flex items-center justify-between gap-2">
        <UiButton
          data-testid="turn-previous"
          variant="quiet"
          :disabled="isFetching || !hasPreviousTurnPage"
          @click="$emit('previous-turn-page')"
        >
          {{ t("sessions.pagination.previous") }}
        </UiButton>
        <UiButton
          data-testid="turn-next"
          variant="quiet"
          :disabled="isFetching || !detail.turnPage.hasMore || !detail.turnPage.nextCursor"
          @click="$emit('next-turn-page')"
        >
          {{ t("sessions.pagination.next") }}
        </UiButton>
      </div>
    </section>

    <p class="mt-4 rounded-control bg-blue-50/80 px-3 py-2 text-[10px] leading-5 text-blue-900/70">
      {{ t("sessions.detail.privacy") }}
    </p>
  </aside>
</template>
