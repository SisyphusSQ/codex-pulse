<script setup lang="ts">
import { RefreshCw } from "@lucide/vue";
import { useQueryClient } from "@tanstack/vue-query";
import { Events, Window } from "@wailsio/runtime";
import { computed, nextTick, onBeforeUnmount, onMounted, ref } from "vue";
import { useI18n } from "vue-i18n";

import type { CurrentWindow } from "@bindings/github.com/SisyphusSQ/codex-pulse/internal/codex/quota/models";
import type { SessionItem } from "@bindings/github.com/SisyphusSQ/codex-pulse/internal/query/usagecost/models";
import { formatCompactTokens, formatMicroUSD, formatPercent, numericValue } from "@/features/overview/format";
import { businessQueryRoots } from "@/queries/business";
import { usePopoverQueries } from "@/features/popover/usePopoverQueries";
import { normalizeDesktopNavigationPath } from "@/router";

defineOptions({ name: "PopoverView" });

const { t } = useI18n();
const queryClient = useQueryClient();
const requestClock = ref(Date.now());
const queries = usePopoverQueries(requestClock);
const quota = computed(() => queries.quota.data.value?.current);
const windows = computed(() => quota.value?.windows ?? []);
const sessions = computed(() => queries.sessions.data.value?.items ?? []);
const usage = computed(() => queries.usage.data.value);
const quotaFatal = computed(() => queries.quota.isError.value && quota.value === undefined);
const quotaStale = computed(() => queries.quota.isError.value && quota.value !== undefined);
const usageFatal = computed(() => queries.usage.isError.value && usage.value === undefined);
const usageStale = computed(() => queries.usage.isError.value && usage.value !== undefined);
const sessionsFatal = computed(() => queries.sessions.isError.value && queries.sessions.data.value === undefined);
const sessionsStale = computed(() => queries.sessions.isError.value && queries.sessions.data.value !== undefined);
const latestUpdatedAt = computed(() => Math.max(
  queries.quota.dataUpdatedAt.value,
  queries.usage.dataUpdatedAt.value,
  queries.sessions.dataUpdatedAt.value,
));

let disposeWindowEvents = () => {};

onMounted(() => {
  const offHide = Events.On(Events.Types.Common.WindowHide, () => {
    void Promise.all([
      queryClient.cancelQueries({ queryKey: businessQueryRoots.quota }),
      queryClient.cancelQueries({ queryKey: businessQueryRoots.usage }),
      queryClient.cancelQueries({ queryKey: businessQueryRoots.sessions }),
    ]);
  });
  const offShow = Events.On(Events.Types.Common.WindowShow, () => {
    requestClock.value = Date.now();
    void nextTick(refresh);
  });
  disposeWindowEvents = () => {
    offHide();
    offShow();
  };
});

onBeforeUnmount(() => disposeWindowEvents());

function windowLabel(window: CurrentWindow) {
  return window.windowKind === "primary" ? t("popover.quota.primary") : t("popover.quota.secondary");
}

function resetLabel(value: number | null) {
  if (value === null) return t("popover.common.unknown");
  const minutes = Math.max(1, Math.ceil(value / 60_000));
  if (minutes < 60) return t("popover.quota.resetMinutes", { value: minutes });
  return t("popover.quota.resetHours", { value: Math.ceil(minutes / 60) });
}

function formatDuration(value: number | null) {
  if (value === null) return t("popover.common.unknown");
  const days = Math.floor(value / 86_400_000);
  const hours = Math.floor((value % 86_400_000) / 3_600_000);
  return days > 0 ? t("popover.reset.durationDays", { days, hours }) : t("popover.reset.durationHours", { hours });
}

function formatDateTime(value: number | null) {
  if (value === null || value <= 0) return t("popover.common.unknown");
  return new Intl.DateTimeFormat("zh-CN", {
    hour: "2-digit",
    minute: "2-digit",
    timeZone: queries.timeZone,
  }).format(value);
}

function relativeTime(value: number | null) {
  if (value === null) return t("popover.common.unknown");
  const minutes = Math.max(0, Math.floor((Date.now() - value) / 60_000));
  if (minutes < 1) return t("popover.time.now");
  if (minutes < 60) return t("popover.time.minutes", { value: minutes });
  return t("popover.time.hours", { value: Math.floor(minutes / 60) });
}

function projectName(session: SessionItem) {
  return session.project.displayName ?? t("popover.sessions.unknownProject");
}

function modelName(session: SessionItem) {
  return session.model.displayName ?? t("popover.sessions.unknownModel");
}

async function refresh() {
  await Promise.all([
    queryClient.invalidateQueries({ queryKey: businessQueryRoots.quota }),
    queryClient.invalidateQueries({ queryKey: businessQueryRoots.usage }),
    queryClient.invalidateQueries({ queryKey: businessQueryRoots.sessions }),
  ]);
}

async function openMain(path: string) {
  await Events.Emit("codex-pulse:navigate", { path: normalizeDesktopNavigationPath(path) });
  const main = Window.Get("main");
  await main.UnMinimise();
  await main.Show();
  await main.Focus();
  await Window.Hide();
}

function openSession(session: SessionItem) {
  void openMain(`/sessions?session=${encodeURIComponent(session.sessionId)}`);
}
</script>

<template>
  <main class="popover" aria-labelledby="popover-title">
    <header class="popover__header">
      <div><span class="popover__mark" aria-hidden="true">&gt;_</span><strong id="popover-title">{{ t("app.name") }}</strong></div>
      <span class="popover__updated" aria-live="polite">{{ t("popover.updated", { value: formatDateTime(latestUpdatedAt) }) }}</span>
    </header>

    <section class="popover__quota" :aria-label="t('popover.quota.title')">
      <p v-if="queries.quota.isPending.value && quota === undefined" class="popover__state">{{ t("popover.state.loading") }}</p>
      <button v-else-if="quotaFatal" class="popover__state popover__retry" @click="queries.quota.refetch()">{{ t("popover.state.retryQuota") }}</button>
      <p v-else-if="windows.length === 0" class="popover__state">{{ t("popover.state.emptyQuota") }}</p>
      <article v-for="window in windows" :key="window.windowKind" class="popover__quota-row">
        <div><strong>{{ windowLabel(window) }}</strong><small>{{ resetLabel(window.resetRemainingMs) }}</small></div>
        <div class="popover__quota-value">
          <strong>{{ formatPercent(window.remainingPercent) }}</strong>
          <span class="popover__track"><span :class="['popover__fill', `popover__fill--${window.windowKind}`]" :style="{ width: `${window.remainingPercent ?? 0}%` }" /></span>
        </div>
      </article>
      <small v-if="quotaStale" class="popover__stale" aria-live="polite">{{ t("popover.state.stale") }}</small>
    </section>

    <section class="popover__summary" :aria-label="t('popover.reset.title')">
      <h2>{{ t("popover.reset.title") }}</h2>
      <p v-if="quotaFatal">{{ t("popover.state.unavailable") }}</p>
      <template v-else>
        <strong>{{ t("popover.reset.count", { available: quota?.resetCredits.availableCount ?? '--', total: quota?.resetCredits.totalCount ?? '--' }) }}</strong>
        <small>{{ t("popover.reset.remaining", { value: formatDuration(quota?.resetCredits.cumulativeRemainingMs ?? null) }) }}</small>
        <small>{{ t("popover.reset.expiresAt", { value: formatDateTime(quota?.resetCredits.nextExpiresAtMs ?? null) }) }}</small>
        <small v-if="quotaStale" class="popover__stale" aria-live="polite">{{ t("popover.state.stale") }}</small>
      </template>
    </section>

    <section class="popover__summary" :aria-label="t('popover.cost.title')">
      <h2>{{ t("popover.cost.title") }}</h2>
      <button v-if="usageFatal" class="popover__retry" @click="queries.usage.refetch()">{{ t("popover.state.retryCost") }}</button>
      <template v-else-if="usage">
        <strong class="popover__cost">{{ formatMicroUSD(usage.totals.estimatedUsdMicros) }}</strong>
        <small>{{ t("popover.cost.tokens", { value: formatCompactTokens(usage.totals.totalTokens) }) }}</small>
        <p>{{ t("popover.cost.disclaimer") }}</p>
        <small v-if="usageStale" class="popover__stale" aria-live="polite">{{ t("popover.state.stale") }}</small>
      </template>
      <p v-else>{{ t("popover.state.loading") }}</p>
    </section>

    <section class="popover__sessions" :aria-label="t('popover.sessions.title')">
      <h2>{{ t("popover.sessions.title") }}</h2>
      <button v-if="sessionsFatal" class="popover__retry" @click="queries.sessions.refetch()">{{ t("popover.state.retrySessions") }}</button>
      <p v-else-if="queries.sessions.isPending.value">{{ t("popover.state.loading") }}</p>
      <p v-else-if="sessions.length === 0">{{ t("popover.state.emptySessions") }}</p>
      <button v-for="session in sessions" v-else :key="session.sessionId" class="popover__session" @click="openSession(session)">
        <span><strong>{{ session.displayTitle }}</strong><small>{{ projectName(session) }} · {{ modelName(session) }}</small></span>
        <span><strong>{{ formatCompactTokens(session.totals.totalTokens) }}</strong><small>{{ relativeTime(numericValue(session.lastActivityAtMs)) }}</small></span>
      </button>
      <small v-if="sessionsStale" class="popover__stale" aria-live="polite">{{ t("popover.state.stale") }}</small>
    </section>

    <footer class="popover__actions">
      <button class="popover__refresh" :aria-label="t('popover.actions.refresh')" @click="refresh"><RefreshCw :size="16" aria-hidden="true" /></button>
      <button class="popover__open" @click="openMain('/overview')">{{ t("popover.actions.openOverview") }}</button>
    </footer>
  </main>
</template>

<style scoped>
.popover { width: 100vw; height: 100vh; overflow: hidden; color: var(--color-ink); background: linear-gradient(145deg, rgb(238 247 255 / 94%), rgb(251 249 255 / 96%) 55%, rgb(244 246 249 / 96%)); border: 1px solid rgb(255 255 255 / 88%); border-radius: 28px; box-shadow: 0 22px 56px rgb(21 51 79 / 19%); display: flex; flex-direction: column; }
.popover button { font: inherit; }
.popover__header { height: 64px; padding: 0 20px; display: flex; align-items: center; justify-content: space-between; border-bottom: 1px solid var(--color-line); }
.popover__header div { display: flex; align-items: center; gap: 9px; }
.popover__mark { font-family: var(--font-mono); font-weight: 800; color: var(--color-accent); }
.popover__updated, .popover small, .popover__state { color: var(--color-ink-subtle); font-size: 10px; }
.popover__quota { min-height: 116px; padding: 12px 20px; display: grid; gap: 10px; }
.popover__quota-row { display: flex; align-items: center; justify-content: space-between; }
.popover__quota-row > div:first-child, .popover__quota-value, .popover__session span { display: flex; flex-direction: column; gap: 4px; }
.popover__quota-value { align-items: flex-end; }
.popover__quota-value > strong { font: 650 25px var(--font-mono); }
.popover__track { width: 116px; height: 7px; border-radius: 4px; overflow: hidden; background: rgb(23 25 28 / 8%); }
.popover__fill { display: block; height: 100%; border-radius: inherit; background: var(--color-accent); }
.popover__fill--secondary { background: var(--color-violet); }
.popover__summary { min-height: 86px; padding: 11px 20px; border-top: 1px solid var(--color-line); background: rgb(255 255 255 / 28%); display: grid; grid-template-columns: 1fr auto; gap: 3px 12px; align-items: center; }
.popover h2 { margin: 0; font-size: 11px; color: var(--color-ink-muted); }
.popover__summary > strong { font: 650 16px var(--font-mono); }
.popover__summary > small, .popover__summary > p { grid-column: 1 / -1; margin: 0; }
.popover__cost { color: var(--color-violet); font-size: 20px !important; }
.popover__sessions { flex: 1; min-height: 0; padding: 12px 20px; border-top: 1px solid var(--color-line); overflow: hidden; }
.popover__sessions h2 { margin-bottom: 7px; }
.popover__session { width: 100%; min-height: 37px; padding: 4px 2px; border: 0; border-radius: 8px; background: transparent; display: flex; justify-content: space-between; text-align: left; color: inherit; }
.popover__session:hover, .popover__session:focus-visible { background: rgb(255 255 255 / 58%); outline: 2px solid var(--color-accent); outline-offset: 1px; }
.popover__session > span:last-child { align-items: flex-end; }
.popover__session strong { max-width: 260px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; font-size: 10.5px; }
.popover__actions { height: 70px; padding: 14px 20px; display: flex; gap: 10px; border-top: 1px solid rgb(255 255 255 / 63%); background: rgb(255 255 255 / 40%); }
.popover__actions button, .popover__retry { border: 1px solid rgb(255 255 255 / 75%); border-radius: 14px; cursor: pointer; }
.popover__refresh { width: 44px; background: rgb(255 255 255 / 72%); color: var(--color-ink-muted); }
.popover__open { flex: 1; border-color: transparent !important; background: var(--color-accent); color: white; font-weight: 700; }
.popover__retry { padding: 6px 9px; background: rgb(255 255 255 / 70%); color: var(--color-ink); }
.popover__stale { color: var(--color-warning) !important; }
@media (prefers-reduced-transparency: reduce) { .popover { background: #f4f6f9; box-shadow: none; } }
:global(body:has(.popover)) { min-width: 0; min-height: 0; background: transparent; }
</style>
