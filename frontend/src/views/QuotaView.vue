<script setup lang="ts">
import { RefreshCw } from "@lucide/vue";
import { computed } from "vue";
import { useI18n } from "vue-i18n";

import type {
  CurrentExplanation,
  CurrentRefreshStatus,
  CurrentSource,
  CurrentWindow,
} from "@bindings/github.com/SisyphusSQ/codex-pulse/internal/codex/quota/models";
import {
  CurrentRefreshState,
  CurrentSourceKind,
} from "@bindings/github.com/SisyphusSQ/codex-pulse/internal/codex/quota/models";
import { ResponseStatus } from "@bindings/github.com/SisyphusSQ/codex-pulse/internal/query/models";
import {
  QuotaEvidenceDisposition,
  QuotaExplanationCode,
  QuotaRejectionReason,
  QuotaSource,
  QuotaValidity,
  QuotaWindowKind,
  SourceFailureCode,
} from "@bindings/github.com/SisyphusSQ/codex-pulse/internal/store/models";
import StateEmpty from "@/components/ui/StateEmpty.vue";
import StateError from "@/components/ui/StateError.vue";
import StateSkeleton from "@/components/ui/StateSkeleton.vue";
import UiButton from "@/components/ui/UiButton.vue";
import UiCard from "@/components/ui/UiCard.vue";
import { useQuotaDisplayClock } from "@/features/quota/displayClock";
import {
  formatQuotaPercent,
  quotaProgressValue,
  resetCountdown,
} from "@/features/quota/format";
import { useQuotaPage } from "@/features/quota/useQuotaPage";
import {
  formatDateTime,
  formatQuotaWindowFreshness,
  formatSourceFreshness,
} from "@/features/overview/format";

defineOptions({ name: "QuotaView" });

const { t } = useI18n();
const page = useQuotaPage();
const nowMS = useQuotaDisplayClock();
const resolvedTimeZone = Intl.DateTimeFormat().resolvedOptions().timeZone;
const timeZone = resolvedTimeZone && resolvedTimeZone !== "Local" ? resolvedTimeZone : "UTC";
const response = computed(() => page.quota.data.value);
const current = computed(() => response.value?.current);
const windows = computed(() => current.value?.windows ?? []);
const sources = computed(() => current.value?.sources ?? []);
const initialPending = computed(() => page.quota.isPending.value && response.value === undefined);
const fatal = computed(() => page.quota.isError.value && response.value === undefined);
const partial = computed(() => response.value?.meta.status === ResponseStatus.ResponsePartial);
const stale = computed(() => page.quota.isError.value && response.value !== undefined);
const empty = computed(() => (
  windows.value.length === 0
  && sources.value.length === 0
  && current.value?.resetCredits.availableCount === null
  && current.value?.resetCredits.totalCount === null
));

const freshnessLabels = computed(() => ({
  current: t("quotaPage.freshness.current"),
  lastKnown: t("quotaPage.freshness.lastKnown"),
  stale: t("quotaPage.freshness.stale"),
  unavailable: t("quotaPage.freshness.unavailable"),
  unknown: t("quotaPage.freshness.unknown"),
}));

function windowTitle(window: CurrentWindow) {
  if (window.windowKind === QuotaWindowKind.QuotaWindowPrimary) {
    return t("quotaPage.window.primary");
  }
  if (window.windowKind === QuotaWindowKind.QuotaWindowSecondary) {
    return t("quotaPage.window.secondary");
  }
  return t("quotaPage.window.other");
}

function sourceLabel(source: CurrentSourceKind | QuotaSource | null) {
  if (source === CurrentSourceKind.CurrentSourceWham || source === QuotaSource.QuotaSourceWham) {
    return t("quotaPage.source.wham");
  }
  if (source === CurrentSourceKind.CurrentSourceLocal || source === QuotaSource.QuotaSourceLocalJSONL) {
    return t("quotaPage.source.local");
  }
  return t("quotaPage.source.unknown");
}

function countdownText(window: CurrentWindow) {
  const trustedResetAtMS = window.resetRemainingMs === null ? null : window.resetsAtMs;
  const countdown = resetCountdown(trustedResetAtMS, nowMS.value);
  if (countdown === null) return t("quotaPage.window.resetUnknown");
  return t(`quotaPage.window.reset${countdown.unit[0].toUpperCase()}${countdown.unit.slice(1)}`, {
    value: countdown.value,
  });
}

function durationText(milliseconds: number | null) {
  if (milliseconds === null) return t("quotaPage.duration.unknown");
  const duration = resetCountdown(nowMS.value + milliseconds, nowMS.value);
  return duration === null
    ? t("quotaPage.duration.unknown")
    : t(`quotaPage.duration.${duration.unit}`, { value: duration.value });
}

function failureLabel(code: SourceFailureCode | null) {
  const keys: Partial<Record<SourceFailureCode, string>> = {
    [SourceFailureCode.SourceFailureNetworkUnavailable]: "quotaPage.source.failure.network",
    [SourceFailureCode.SourceFailureTimeout]: "quotaPage.source.failure.timeout",
    [SourceFailureCode.SourceFailureAuthRequired]: "quotaPage.source.failure.auth",
    [SourceFailureCode.SourceFailureHTTP429]: "quotaPage.source.failure.rateLimit",
    [SourceFailureCode.SourceFailureServerError]: "quotaPage.source.failure.server",
    [SourceFailureCode.SourceFailureSchemaIncompatible]: "quotaPage.source.failure.schema",
    [SourceFailureCode.SourceFailureCancelled]: "quotaPage.source.failure.cancelled",
  };
  return t(code === null ? "quotaPage.source.failure.none" : (keys[code] ?? "quotaPage.source.failure.unknown"));
}

function explanationLabel(code: QuotaExplanationCode) {
  const keys: Partial<Record<QuotaExplanationCode, string>> = {
    [QuotaExplanationCode.QuotaExplanationTrusted]: "quotaPage.evidence.trusted",
    [QuotaExplanationCode.QuotaExplanationStale]: "quotaPage.evidence.stale",
    [QuotaExplanationCode.QuotaExplanationExpired]: "quotaPage.evidence.expired",
    [QuotaExplanationCode.QuotaExplanationSuspicious]: "quotaPage.evidence.suspicious",
    [QuotaExplanationCode.QuotaExplanationSourceConflict]: "quotaPage.evidence.conflict",
    [QuotaExplanationCode.QuotaExplanationUnavailable]: "quotaPage.evidence.unavailable",
  };
  return t(keys[code] ?? "quotaPage.evidence.unavailable");
}

function dispositionLabel(evidence: CurrentExplanation) {
  const keys: Partial<Record<QuotaEvidenceDisposition, string>> = {
    [QuotaEvidenceDisposition.QuotaEvidenceSelected]: "quotaPage.evidence.selected",
    [QuotaEvidenceDisposition.QuotaEvidenceEligible]: "quotaPage.evidence.eligible",
    [QuotaEvidenceDisposition.QuotaEvidenceSuperseded]: "quotaPage.evidence.superseded",
    [QuotaEvidenceDisposition.QuotaEvidenceSuspicious]: "quotaPage.evidence.suspicious",
    [QuotaEvidenceDisposition.QuotaEvidenceRejected]: "quotaPage.evidence.rejected",
  };
  return t(keys[evidence.disposition] ?? "quotaPage.evidence.unavailable");
}

function validityLabel(validity: QuotaValidity) {
  const keys: Partial<Record<QuotaValidity, string>> = {
    [QuotaValidity.QuotaValidityAccepted]: "quotaPage.evidence.validityAccepted",
    [QuotaValidity.QuotaValiditySuspicious]: "quotaPage.evidence.validitySuspicious",
    [QuotaValidity.QuotaValidityRejected]: "quotaPage.evidence.validityRejected",
  };
  return t(keys[validity] ?? "quotaPage.evidence.validityUnknown");
}

function rejectionReasonLabel(reason: QuotaRejectionReason) {
  const keys: Partial<Record<QuotaRejectionReason, string>> = {
    [QuotaRejectionReason.QuotaReasonMissingLimitID]: "quotaPage.reason.missingLimit",
    [QuotaRejectionReason.QuotaReasonMissingPrimaryWindow]: "quotaPage.reason.missingPrimary",
    [QuotaRejectionReason.QuotaReasonResetNotFuture]: "quotaPage.reason.resetNotFuture",
    [QuotaRejectionReason.QuotaReasonUnknownPlanType]: "quotaPage.reason.unknownPlan",
    [QuotaRejectionReason.QuotaReasonInvalidUsedPercent]: "quotaPage.reason.invalidUsed",
    [QuotaRejectionReason.QuotaReasonInvalidWindowMinutes]: "quotaPage.reason.invalidWindow",
    [QuotaRejectionReason.QuotaReasonInvalidResetsAt]: "quotaPage.reason.invalidReset",
    [QuotaRejectionReason.QuotaReasonInvalidStructure]: "quotaPage.reason.invalidStructure",
    [QuotaRejectionReason.QuotaReasonUsedRegression]: "quotaPage.reason.usedRegression",
    [QuotaRejectionReason.QuotaReasonResetRegression]: "quotaPage.reason.resetRegression",
    [QuotaRejectionReason.QuotaReasonObservedRegression]: "quotaPage.reason.observedRegression",
    [QuotaRejectionReason.QuotaReasonSourceConflict]: "quotaPage.reason.sourceConflict",
    [QuotaRejectionReason.QuotaReasonDefaultFallback]: "quotaPage.reason.fallback",
  };
  return t(keys[reason] ?? "quotaPage.reason.unknown");
}

function sourceFreshness(source: CurrentSource) {
  return formatSourceFreshness(source.freshness, freshnessLabels.value);
}

function refreshState(status: CurrentRefreshStatus) {
  const keys: Partial<Record<CurrentRefreshState, string>> = {
    [CurrentRefreshState.CurrentRefreshScheduled]: "quotaPage.refresh.scheduled",
    [CurrentRefreshState.CurrentRefreshPaused]: "quotaPage.refresh.paused",
    [CurrentRefreshState.CurrentRefreshInFlight]: "quotaPage.refresh.inFlight",
    [CurrentRefreshState.CurrentRefreshDisabled]: "quotaPage.refresh.disabled",
    [CurrentRefreshState.CurrentRefreshUnknown]: "quotaPage.refresh.unknown",
  };
  return t(keys[status.state] ?? "quotaPage.refresh.unknown");
}
</script>

<template>
  <section data-testid="quota-view" class="w-full space-y-4 py-1">
    <div class="flex flex-wrap items-start justify-between gap-4">
      <div class="flex min-h-10 items-center gap-2 text-xs font-semibold text-ink-muted">
        <span v-if="partial" data-testid="quota-partial" class="rounded-full bg-amber-100 px-3 py-1 text-amber-800">
          {{ t("quotaPage.state.partial") }}
        </span>
        <span v-if="stale" data-testid="quota-stale" class="rounded-full bg-slate-200 px-3 py-1 text-slate-700">
          {{ t("quotaPage.state.stale") }}
        </span>
      </div>
      <UiButton
        data-testid="quota-refresh-action"
        variant="primary"
        :loading="page.refresh.isPending.value"
        @click="page.refreshAll"
      >
        <RefreshCw v-if="!page.refresh.isPending.value" :size="16" aria-hidden="true" />
        {{ page.refresh.isPending.value ? t("quotaPage.action.refreshing") : t("quotaPage.action.refresh") }}
      </UiButton>
    </div>

    <p
      v-if="page.refresh.isError.value"
      data-testid="quota-refresh-error"
      role="alert"
      class="rounded-control border border-amber-200 bg-amber-50 px-4 py-3 text-sm text-amber-900"
    >
      {{ t("quotaPage.state.refreshError") }}
    </p>

    <StateSkeleton v-if="initialPending" :label="t('quotaPage.state.loading')" :rows="5" />
    <StateError
      v-else-if="fatal"
      data-testid="quota-fatal-error"
      action-test-id="quota-query-retry"
      :title="t('quotaPage.state.fatalTitle')"
      :description="t('quotaPage.state.fatalDescription')"
      :action-label="t('quotaPage.state.retry')"
      @retry="page.quota.refetch()"
    />
    <StateEmpty
      v-else-if="empty"
      :title="t('quotaPage.state.emptyTitle')"
      :description="t('quotaPage.state.emptyDescription')"
    />

    <template v-else-if="current">
      <UiCard :title="t('quotaPage.window.title')" :description="t('quotaPage.window.description')">
        <div class="grid gap-4 lg:grid-cols-2">
          <article
            v-for="window in windows"
            :key="`${window.windowKind}:${window.limitId}`"
            data-testid="quota-window"
            class="rounded-content border border-line bg-white/65 p-5"
          >
            <div class="flex items-start justify-between gap-4">
              <div>
                <h3 class="text-sm font-semibold text-ink">{{ windowTitle(window) }}</h3>
                <p class="mt-1 text-xs text-ink-muted">{{ t("quotaPage.window.remaining") }}</p>
              </div>
              <strong class="text-3xl tracking-tight text-ink">{{ formatQuotaPercent(window.remainingPercent) }}</strong>
            </div>
            <div
              class="mt-4 h-2 overflow-hidden rounded-full bg-slate-100"
              :data-testid="window.remainingPercent === null ? 'quota-progress-unknown' : window.remainingPercent === 0 ? 'quota-progress-zero' : 'quota-progress-known'"
              :role="window.remainingPercent === null ? undefined : 'progressbar'"
              :aria-valuemin="window.remainingPercent === null ? undefined : 0"
              :aria-valuemax="window.remainingPercent === null ? undefined : 100"
              :aria-valuenow="window.remainingPercent ?? undefined"
            >
              <div
                v-if="quotaProgressValue(window.remainingPercent) !== null"
                class="h-full rounded-full bg-accent"
                :style="{
                  minWidth: window.remainingPercent === 0 ? '3px' : undefined,
                  width: `${quotaProgressValue(window.remainingPercent)}%`,
                }"
              />
            </div>
            <dl class="mt-4 grid gap-2 text-xs text-ink-muted">
              <div class="flex justify-between gap-4"><dt>{{ countdownText(window) }}</dt><dd>{{ formatQuotaWindowFreshness(window.freshness, freshnessLabels) }}</dd></div>
              <div><dt>{{ t("quotaPage.window.resetAt", { time: formatDateTime(window.resetsAtMs, timeZone) }) }}</dt></div>
              <div class="flex justify-between gap-4"><dt>{{ t("quotaPage.window.source", { source: sourceLabel(window.selectedSource) }) }}</dt><dd>{{ explanationLabel(window.explanationCode) }}</dd></div>
            </dl>
            <ul v-if="window.explanations?.length" class="mt-4 space-y-2 border-t border-line pt-3">
              <li
                v-for="evidence in window.explanations"
                :key="evidence.observationId"
                data-testid="quota-evidence"
                class="rounded-control bg-slate-50 px-3 py-2 text-xs text-ink-muted"
              >
                <div class="flex flex-wrap justify-between gap-2">
                  <span>{{ sourceLabel(evidence.source) }} · {{ explanationLabel(evidence.explanationCode) }}</span>
                  <span>{{ dispositionLabel(evidence) }}</span>
                </div>
                <div class="mt-1 flex flex-wrap gap-x-3 gap-y-1 text-ink-subtle">
                  <span>{{ t("quotaPage.evidence.observedAt", { time: formatDateTime(evidence.observedAtMs, timeZone) }) }}</span>
                  <span>{{ t("quotaPage.evidence.remaining", { value: formatQuotaPercent(evidence.remainingPercent) }) }}</span>
                  <span>{{ validityLabel(evidence.validity) }}</span>
                  <span v-if="evidence.reason !== null">{{ t("quotaPage.evidence.reason", { reason: rejectionReasonLabel(evidence.reason) }) }}</span>
                </div>
              </li>
            </ul>
          </article>
        </div>
        <p class="mt-4 text-xs text-ink-subtle">{{ t("quotaPage.window.evaluatedAt", { time: formatDateTime(current.evaluatedAtMs, timeZone) }) }}</p>
      </UiCard>

      <div class="grid gap-4 xl:grid-cols-2">
        <UiCard :title="t('quotaPage.source.title')" :description="t('quotaPage.source.description')">
          <div class="space-y-3">
            <article
              v-for="source in sources"
              :key="source.source"
              data-testid="quota-source"
              class="rounded-control border border-line bg-white/60 p-4"
            >
              <div class="flex items-start justify-between gap-4">
                <div>
                  <h3 class="text-sm font-semibold text-ink">{{ sourceLabel(source.source) }}</h3>
                  <p class="mt-1 text-xs text-ink-muted">{{ t("quotaPage.source.lastSuccess", { time: formatDateTime(source.lastSuccessAtMs, timeZone) }) }}</p>
                </div>
                <span class="rounded-full bg-slate-100 px-2.5 py-1 text-xs text-ink-muted">{{ sourceFreshness(source) }}</span>
              </div>
              <div class="mt-3 flex flex-wrap gap-x-4 gap-y-1 text-xs text-ink-muted">
                <span>{{ t("quotaPage.source.selected", { count: source.selectedWindowCount }) }}</span>
                <span>{{ t("quotaPage.source.conflicts", { count: source.conflictWindowCount }) }}</span>
                <span class="font-medium text-ink">{{ failureLabel(source.failureCode) }}</span>
              </div>
            </article>
          </div>
        </UiCard>

        <UiCard :title="t('quotaPage.credits.title')" :description="t('quotaPage.credits.description')">
          <div data-testid="quota-reset-credits" class="rounded-content border border-line bg-white/60 p-5">
            <p class="text-3xl font-bold tracking-tight text-ink">
              {{ current.resetCredits.availableCount ?? t("quotaPage.credits.unknown") }} /
              {{ current.resetCredits.totalCount ?? t("quotaPage.credits.unknown") }}
            </p>
            <div class="mt-4 space-y-2 text-sm text-ink-muted">
              <p>{{ t("quotaPage.credits.redeemed", { count: current.resetCredits.redeemedCount ?? t("quotaPage.credits.unknown") }) }}</p>
              <p>{{ t("quotaPage.credits.cumulative", { duration: durationText(current.resetCredits.cumulativeRemainingMs) }) }}</p>
              <p>{{ t("quotaPage.credits.nextExpiry", { time: formatDateTime(current.resetCredits.nextExpiresAtMs, timeZone) }) }}</p>
              <p class="font-medium text-ink">{{ failureLabel(current.resetCredits.failureCode) }}</p>
            </div>
          </div>
        </UiCard>
      </div>

      <UiCard :title="t('quotaPage.refresh.title')" :description="t('quotaPage.refresh.description')">
        <div class="grid gap-3 md:grid-cols-2">
          <article
            v-for="item in [
              { key: 'quota', label: t('quotaPage.refresh.quota'), status: current.refresh.quota },
              { key: 'reset-credits', label: t('quotaPage.refresh.resetCredits'), status: current.refresh.resetCredits },
            ]"
            :key="item.key"
            data-testid="quota-refresh-status"
            class="rounded-control border border-line bg-white/60 p-4"
          >
            <div class="flex justify-between gap-4 text-sm"><strong class="text-ink">{{ item.label }}</strong><span class="text-ink-muted">{{ refreshState(item.status) }}</span></div>
            <p class="mt-2 text-xs text-ink-muted">{{ t("quotaPage.refresh.nextDue", { time: formatDateTime(item.status.nextDueAtMs, timeZone) }) }}</p>
            <p class="mt-1 text-xs text-ink-muted">{{ t("quotaPage.refresh.lastManual", { time: formatDateTime(item.status.lastManualAtMs, timeZone) }) }}</p>
          </article>
        </div>
      </UiCard>
    </template>
  </section>
</template>
