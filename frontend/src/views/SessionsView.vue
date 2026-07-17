<script setup lang="ts">
import { computed, nextTick, ref, watch } from "vue";
import { useI18n } from "vue-i18n";
import { useRoute, useRouter } from "vue-router";

import { ResponseStatus } from "@bindings/github.com/SisyphusSQ/codex-pulse/internal/query/models";
import type { SessionItem } from "@bindings/github.com/SisyphusSQ/codex-pulse/internal/query/usagecost/models";
import StateEmpty from "@/components/ui/StateEmpty.vue";
import StateError from "@/components/ui/StateError.vue";
import StateSkeleton from "@/components/ui/StateSkeleton.vue";
import UiButton from "@/components/ui/UiButton.vue";
import SessionDetailPanel from "@/features/sessions/SessionDetailPanel.vue";
import SessionsFilters, {
  type SessionsFilterOption,
} from "@/features/sessions/SessionsFilters.vue";
import SessionsTable from "@/features/sessions/SessionsTable.vue";
import { useLocalDateClock } from "@/features/sessions/localDateClock";
import { classifySessionsQueryError } from "@/features/sessions/queryError";
import {
  parseSessionsRouteState,
  selectSession,
  serializeSessionsRouteState,
  setSessionsListCursor,
  updateSessionsFilters,
  type SessionsFilterPatch,
  type SessionsRouteState,
} from "@/features/sessions/routeState";
import { useSessionQueries } from "@/features/sessions/useSessionQueries";
import { formatCount } from "@/features/overview/format";

defineOptions({ name: "SessionsView" });

const { t } = useI18n();
const route = useRoute();
const router = useRouter();
const resolvedTimeZone = Intl.DateTimeFormat().resolvedOptions().timeZone;
const timeZone = resolvedTimeZone && resolvedTimeZone !== "Local" ? resolvedTimeZone : "UTC";

const normalizedNotice = ref(false);
const detailPanel = ref<{ focusHeading: () => void } | null>(null);
const listRegion = ref<HTMLElement | null>(null);
const pendingDetailFocusSessionId = ref<string | null>(null);
const turnCursor = ref<string | null>(null);
const turnCursorHistory = ref<Array<string | null>>([]);
const turnPageTransitioning = ref(false);
const listCursorHistory = ref<Array<string | null>>([]);
const pendingListCursorNavigation = ref<{ cursor: string | null } | null>(null);
const parsedRoute = computed(() => parseSessionsRouteState(route.query));
const routeState = computed(() => parsedRoute.value.state);
const listScopeKey = computed(() => JSON.stringify([
  routeState.value.activity,
  routeState.value.range,
  routeState.value.projectId,
  routeState.value.modelKey,
  routeState.value.sort,
  routeState.value.direction,
]));
const localDateClock = useLocalDateClock(timeZone);
const queries = useSessionQueries(
  routeState,
  turnCursor,
  timeZone,
  () => localDateClock.value,
);

const listData = computed(() => queries.list.data.value);
const detailData = computed(() => queries.detail.data.value);
const items = computed(() => listData.value?.items ?? []);
const listError = computed(() => classifySessionsQueryError(queries.list.error.value));
const detailError = computed(() => classifySessionsQueryError(queries.detail.error.value));
const listPartial = computed(() => listData.value?.meta.status === ResponseStatus.ResponsePartial);
const listStale = computed(() => (
  listData.value !== undefined
  && (queries.list.isError.value || queries.list.isPlaceholderData.value)
));
const listFatal = computed(() => queries.list.isError.value && listData.value === undefined);
const detailFatal = computed(() => queries.detail.isError.value && detailData.value === undefined);
const detailStale = computed(() => detailData.value !== undefined && queries.detail.isError.value);
const listPageTransitioning = computed(() => pendingListCursorNavigation.value !== null);
const hasListPrevious = computed(() => (
  !listPageTransitioning.value
  && (listCursorHistory.value.length > 0 || routeState.value.cursor !== null)
));

function filterOptions(field: "model" | "project") {
  const options = new Map<string, string>();
  for (const item of items.value) {
    const value = field === "model" ? item.model : item.project;
    if (value.id && value.displayName && !options.has(value.id)) {
      options.set(value.id, value.displayName);
    }
  }
  return [...options].map(([value, label]) => ({ label, value }));
}

const projectOptions = computed<SessionsFilterOption[]>(() => filterOptions("project"));
const modelOptions = computed<SessionsFilterOption[]>(() => filterOptions("model"));

function navigate(state: SessionsRouteState, replace = false) {
  const target = { name: "sessions", query: serializeSessionsRouteState(state) };
  return replace ? router.replace(target) : router.push(target);
}

watch(
  () => route.fullPath,
  () => {
    const parsed = parseSessionsRouteState(route.query);
    if (parsed.normalized) {
      normalizedNotice.value = true;
      void navigate(parsed.state, true);
    }
  },
  { immediate: true },
);

watch(
  () => routeState.value.sessionId,
  (current, previous) => {
    if (current !== previous) {
      turnCursor.value = null;
      turnCursorHistory.value = [];
      turnPageTransitioning.value = false;
    }
  },
);

watch(
  () => queries.detail.isFetching.value,
  (current, previous) => {
    if (previous && !current) turnPageTransitioning.value = false;
  },
);

watch(detailData, (current, previous) => {
  if (current !== previous) turnPageTransitioning.value = false;
});

watch(listScopeKey, (current, previous) => {
  if (previous !== undefined && current !== previous) {
    listCursorHistory.value = [];
  }
});

watch(
  () => routeState.value.cursor,
  (cursor) => {
    if (pendingListCursorNavigation.value?.cursor === cursor) {
      pendingListCursorNavigation.value = null;
      return;
    }
    pendingListCursorNavigation.value = null;
    listCursorHistory.value = [];
  },
);

function updateFilters(patch: SessionsFilterPatch) {
  pendingDetailFocusSessionId.value = null;
  listCursorHistory.value = [];
  turnCursor.value = null;
  turnCursorHistory.value = [];
  turnPageTransitioning.value = false;
  void navigate(updateSessionsFilters(routeState.value, patch));
}

function chooseSession(sessionId: string) {
  pendingDetailFocusSessionId.value = sessionId;
  void navigate(selectSession(routeState.value, sessionId)).then(() => {
    focusDetailIfReady(sessionId);
  });
}

function focusDetailIfReady(sessionId: string) {
  if (
    pendingDetailFocusSessionId.value !== sessionId
    || routeState.value.sessionId !== sessionId
    || detailData.value?.item.sessionId !== sessionId
  ) {
    return;
  }
  pendingDetailFocusSessionId.value = null;
  void nextTick(() => detailPanel.value?.focusHeading());
}

function focusSessionRow(sessionId: string) {
  const rowIndex = items.value.findIndex((item) => item.sessionId === sessionId);
  if (rowIndex < 0) return;
  listRegion.value
    ?.querySelectorAll<HTMLElement>("[data-testid='session-row']")
    .item(rowIndex)
    .focus();
}

function closeDetail() {
  const selectedSessionId = routeState.value.sessionId;
  pendingDetailFocusSessionId.value = null;
  void navigate(selectSession(routeState.value, null)).then(() => nextTick(() => {
    if (selectedSessionId !== null) focusSessionRow(selectedSessionId);
  }));
}

function nextListPage() {
  if (listPageTransitioning.value || queries.list.isPlaceholderData.value) return;
  const nextCursor = listData.value?.meta.page?.nextCursor ?? null;
  if (!listData.value?.meta.page?.hasMore || nextCursor === null) return;
  listCursorHistory.value.push(routeState.value.cursor);
  pendingListCursorNavigation.value = { cursor: nextCursor };
  void navigate(setSessionsListCursor(routeState.value, nextCursor));
}

function previousListPage() {
  if (listPageTransitioning.value) return;
  const previous = listCursorHistory.value.length > 0
    ? listCursorHistory.value.pop() ?? null
    : null;
  pendingListCursorNavigation.value = { cursor: previous };
  void navigate(setSessionsListCursor(routeState.value, previous));
}

function recoverList() {
  if (listError.value.recovery === "list_first_page") {
    listCursorHistory.value = [];
    pendingListCursorNavigation.value = { cursor: null };
    void navigate(setSessionsListCursor(routeState.value, null), true);
    return;
  }
  void queries.list.refetch();
}

function nextTurnPage() {
  if (turnPageTransitioning.value) return;
  const nextCursor = detailData.value?.turnPage.nextCursor ?? null;
  if (!detailData.value?.turnPage.hasMore || nextCursor === null) return;
  turnPageTransitioning.value = true;
  turnCursorHistory.value.push(turnCursor.value);
  turnCursor.value = nextCursor;
}

function previousTurnPage() {
  if (turnPageTransitioning.value) return;
  turnPageTransitioning.value = true;
  turnCursor.value = turnCursorHistory.value.pop() ?? null;
}

function recoverDetail() {
  if (detailError.value.recovery === "close_detail") {
    closeDetail();
    return;
  }
  if (detailError.value.recovery === "turn_first_page") {
    turnPageTransitioning.value = true;
    turnCursor.value = null;
    turnCursorHistory.value = [];
    return;
  }
  void queries.detail.refetch();
}

watch(
  [
    () => routeState.value.sessionId,
    () => detailData.value?.item.sessionId,
  ],
  ([selectedSessionId]) => {
    if (selectedSessionId !== null) focusDetailIfReady(selectedSessionId);
  },
);

const listRecoveryLabel = computed(() => (
  listError.value.recovery === "list_first_page"
    ? t("sessions.state.firstPage")
    : t("sessions.state.retry")
));
const detailRecoveryLabel = computed(() => {
  if (detailError.value.recovery === "close_detail") return t("sessions.state.closeDetail");
  if (detailError.value.recovery === "turn_first_page") return t("sessions.state.firstPage");
  return t("sessions.state.retry");
});
</script>

<template>
  <section data-testid="sessions-view" class="w-full space-y-3 py-1">
    <p
      v-if="normalizedNotice"
      data-testid="sessions-normalized"
      role="status"
      class="rounded-control bg-blue-50/80 px-3 py-2 text-xs text-blue-900/75"
    >
      {{ t("sessions.state.normalized") }}
    </p>

    <SessionsFilters
      :state="routeState"
      :project-options="projectOptions"
      :model-options="modelOptions"
      @change="updateFilters"
    />

    <div class="grid items-start gap-4 xl:grid-cols-[minmax(0,1fr)_22rem]">
      <section ref="listRegion" data-testid="sessions-list-region" class="min-w-0 space-y-3">
        <div class="flex min-h-7 flex-wrap items-center justify-between gap-2 px-1">
          <p class="text-xs text-ink-muted">
            {{ t("sessions.summary.matched", {
              count: listData ? formatCount(listData.matchedCount) : "--",
            }) }}
            <span class="ml-1 text-ink-subtle">· {{ t("sessions.summary.estimate") }}</span>
          </p>
          <div class="flex items-center gap-2 text-[11px]">
            <span v-if="listPartial" class="rounded-full bg-amber-50 px-2 py-1 font-semibold text-amber-800">
              {{ t("sessions.state.partial") }}
            </span>
            <span v-if="listStale" class="rounded-full bg-slate-100 px-2 py-1 text-slate-600">
              {{ t("sessions.state.stale") }}
            </span>
            <span v-if="queries.list.isFetching.value && listData" class="text-ink-subtle">
              {{ t("sessions.state.refreshing") }}
            </span>
          </div>
        </div>

        <StateSkeleton
          v-if="queries.list.isPending.value && !listData"
          data-testid="sessions-list-loading"
          :label="t('sessions.state.loading')"
          :rows="7"
        />
        <StateError
          v-else-if="listFatal"
          data-testid="sessions-list-error"
          action-test-id="sessions-list-recover"
          :title="t('sessions.state.listErrorTitle')"
          :description="t('sessions.state.listErrorDescription')"
          :action-label="listRecoveryLabel"
          @retry="recoverList"
        />
        <StateEmpty
          v-else-if="items.length === 0"
          data-testid="sessions-list-empty"
          :title="t('sessions.state.emptyTitle')"
          :description="t('sessions.state.emptyDescription')"
        />
        <SessionsTable
          v-else
          :items="items as SessionItem[]"
          :selected-session-id="routeState.sessionId"
          :time-zone="timeZone"
          @select="chooseSession"
        />

        <div v-if="listData && items.length > 0" class="flex items-center justify-between gap-2 px-1">
          <UiButton
            data-testid="sessions-list-previous"
            variant="quiet"
            :disabled="!hasListPrevious"
            @click="previousListPage"
          >
            {{ t("sessions.pagination.previous") }}
          </UiButton>
          <UiButton
            data-testid="sessions-list-next"
            variant="quiet"
            :disabled="listPageTransitioning || queries.list.isPlaceholderData.value || !listData.meta.page?.hasMore || !listData.meta.page?.nextCursor"
            @click="nextListPage"
          >
            {{ t("sessions.pagination.next") }}
          </UiButton>
        </div>
      </section>

      <section class="min-w-0">
        <StateEmpty
          v-if="routeState.sessionId === null"
          data-testid="sessions-detail-empty"
          :title="t('sessions.state.selectTitle')"
          :description="t('sessions.state.selectDescription')"
        />
        <StateSkeleton
          v-else-if="queries.detail.isPending.value && !detailData"
          data-testid="sessions-detail-loading"
          :label="t('sessions.state.loading')"
          :rows="5"
        />
        <StateError
          v-else-if="detailFatal"
          data-testid="sessions-detail-error"
          action-test-id="sessions-detail-recover"
          :title="t('sessions.state.detailErrorTitle')"
          :description="t('sessions.state.detailErrorDescription')"
          :action-label="detailRecoveryLabel"
          @retry="recoverDetail"
        />
        <div v-else-if="detailData" class="space-y-2">
          <div
            v-if="detailStale"
            data-testid="sessions-detail-stale"
            role="status"
            class="flex items-center justify-between gap-2 rounded-control bg-amber-50 px-3 py-2 text-[11px] text-amber-900"
          >
            <span>{{ t("sessions.state.stale") }}</span>
            <UiButton
              data-testid="sessions-detail-stale-retry"
              variant="quiet"
              @click="queries.detail.refetch()"
            >
              {{ t("sessions.state.retry") }}
            </UiButton>
          </div>
          <SessionDetailPanel
            ref="detailPanel"
            :detail="detailData"
            :has-previous-turn-page="turnCursorHistory.length > 0"
            :is-fetching="turnPageTransitioning || queries.detail.isFetching.value"
            :time-zone="timeZone"
            @close="closeDetail"
            @next-turn-page="nextTurnPage"
            @previous-turn-page="previousTurnPage"
          />
        </div>
      </section>
    </div>
  </section>
</template>
