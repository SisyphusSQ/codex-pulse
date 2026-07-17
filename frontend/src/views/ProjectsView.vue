<script setup lang="ts">
import { computed, nextTick, ref, watch } from "vue";
import { useI18n } from "vue-i18n";
import { useRoute, useRouter } from "vue-router";

import { ResponseStatus } from "@bindings/github.com/SisyphusSQ/codex-pulse/internal/query/models";
import type { ProjectItem } from "@bindings/github.com/SisyphusSQ/codex-pulse/internal/query/usagecost/models";
import StateEmpty from "@/components/ui/StateEmpty.vue";
import StateError from "@/components/ui/StateError.vue";
import StateSkeleton from "@/components/ui/StateSkeleton.vue";
import UiButton from "@/components/ui/UiButton.vue";
import {
  formatCompactTokens,
  formatCount,
  formatMicroUSD,
} from "@/features/overview/format";
import { createCustomOverviewRange } from "@/features/overview/range";
import ProjectDetailPanel from "@/features/projects/ProjectDetailPanel.vue";
import ProjectsFilters from "@/features/projects/ProjectsFilters.vue";
import ProjectsList from "@/features/projects/ProjectsList.vue";
import { classifyProjectsQueryError } from "@/features/projects/queryError";
import { createProjectsRange } from "@/features/projects/requests";
import {
  parseProjectsRouteState,
  selectProject,
  serializeProjectsRouteState,
  setProjectsListCursor,
  updateProjectsFilters,
  type ProjectsFilterPatch,
  type ProjectsRouteState,
} from "@/features/projects/routeState";
import { useProjectQueries } from "@/features/projects/useProjectQueries";
import { useLocalDateClock } from "@/features/shared/localDateClock";

defineOptions({ name: "ProjectsView" });

const { t } = useI18n();
const route = useRoute();
const router = useRouter();
const resolvedTimeZone = Intl.DateTimeFormat().resolvedOptions().timeZone;
const timeZone = resolvedTimeZone && resolvedTimeZone !== "Local" ? resolvedTimeZone : "UTC";

const normalizedNotice = ref(false);
const rangeError = ref("");
const detailPanel = ref<{ focusHeading: () => void } | null>(null);
const listRegion = ref<HTMLElement | null>(null);
const pendingDetailFocusProjectKey = ref<string | null>(null);
const sessionCursor = ref<string | null>(null);
const sessionCursorHistory = ref<Array<string | null>>([]);
const sessionPageTransitioning = ref(false);
const modelCursor = ref<string | null>(null);
const modelCursorHistory = ref<Array<string | null>>([]);
const modelPageTransitioning = ref(false);
const listCursorHistory = ref<Array<string | null>>([]);
const pendingListCursorNavigation = ref<{ cursor: string | null } | null>(null);
const parsedRoute = computed(() => parseProjectsRouteState(route.query));
const routeState = computed(() => parsedRoute.value.state);
const listScopeKey = computed(() => JSON.stringify([
  routeState.value.confidence,
  routeState.value.direction,
  routeState.value.endDateExclusive,
  routeState.value.range,
  routeState.value.sort,
  routeState.value.startDate,
]));
const localDateClock = useLocalDateClock(timeZone);
const queries = useProjectQueries(
  routeState,
  sessionCursor,
  modelCursor,
  timeZone,
  () => localDateClock.value,
);
const resolvedRangeKey = computed(() => {
  const range = queries.requests.value.list.timeRange;
  return range === null
    ? null
    : JSON.stringify([range.startDate, range.endDateExclusive, range.timeZone]);
});

const listData = computed(() => queries.list.data.value);
const detailData = computed(() => queries.detail.data.value);
const items = computed(() => listData.value?.items ?? []);
const listError = computed(() => classifyProjectsQueryError(queries.list.error.value));
const detailError = computed(() => classifyProjectsQueryError(queries.detail.error.value));
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

function navigate(state: ProjectsRouteState, replace = false) {
  const target = { name: "projects", query: serializeProjectsRouteState(state) };
  return replace ? router.replace(target) : router.push(target);
}

watch(
  () => route.fullPath,
  () => {
    const parsed = parseProjectsRouteState(route.query);
    if (parsed.normalized) {
      normalizedNotice.value = true;
      void navigate(parsed.state, true);
    }
  },
  { immediate: true },
);

watch(
  () => routeState.value.projectKey,
  (current, previous) => {
    if (current !== previous) {
      sessionCursor.value = null;
      sessionCursorHistory.value = [];
      sessionPageTransitioning.value = false;
      modelCursor.value = null;
      modelCursorHistory.value = [];
      modelPageTransitioning.value = false;
    }
  },
);

watch(
  () => queries.detail.isFetching.value,
  (current, previous) => {
    if (previous && !current) {
      sessionPageTransitioning.value = false;
      modelPageTransitioning.value = false;
    }
  },
);

watch(detailData, (current, previous) => {
  if (current !== previous) {
    sessionPageTransitioning.value = false;
    modelPageTransitioning.value = false;
  }
});

watch(listScopeKey, (current, previous) => {
  if (previous !== undefined && current !== previous) listCursorHistory.value = [];
});

watch(resolvedRangeKey, (current, previous) => {
  if (current === previous) return;
  pendingDetailFocusProjectKey.value = null;
  pendingListCursorNavigation.value = null;
  listCursorHistory.value = [];
  resetDetailPages();
  if (routeState.value.cursor !== null || routeState.value.projectKey !== null) {
    void navigate({ ...routeState.value, cursor: null, projectKey: null }, true);
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

function updateFilters(patch: ProjectsFilterPatch) {
  let normalizedPatch = patch;
  if (patch.range === "custom") {
    const currentRange = createProjectsRange(routeState.value, localDateClock.value, timeZone);
    const startDate = patch.startDate || currentRange.startDate;
    const endDateExclusive = patch.endDateExclusive || currentRange.endDateExclusive;
    try {
      createCustomOverviewRange(startDate, endDateExclusive, timeZone);
      normalizedPatch = { ...patch, endDateExclusive, startDate };
      rangeError.value = "";
    } catch {
      rangeError.value = t("overview.range.invalid");
      return;
    }
  } else {
    rangeError.value = "";
  }
  pendingDetailFocusProjectKey.value = null;
  listCursorHistory.value = [];
  resetDetailPages();
  void navigate(updateProjectsFilters(routeState.value, normalizedPatch));
}

function chooseProject(projectKey: string) {
  pendingDetailFocusProjectKey.value = projectKey;
  void navigate(selectProject(routeState.value, projectKey)).then(() => {
    focusDetailIfReady(projectKey);
  });
}

function focusDetailIfReady(projectKey: string) {
  if (
    pendingDetailFocusProjectKey.value !== projectKey
    || routeState.value.projectKey !== projectKey
    || detailData.value?.item.dimensionKey !== projectKey
  ) {
    return;
  }
  pendingDetailFocusProjectKey.value = null;
  void nextTick(() => detailPanel.value?.focusHeading());
}

function focusProjectRow(projectKey: string) {
  const rowIndex = items.value.findIndex((item) => item.dimensionKey === projectKey);
  if (rowIndex < 0) return;
  listRegion.value
    ?.querySelectorAll<HTMLElement>("[data-testid='project-row']")
    .item(rowIndex)
    .focus();
}

function closeDetail() {
  const selectedProjectKey = routeState.value.projectKey;
  pendingDetailFocusProjectKey.value = null;
  void navigate(selectProject(routeState.value, null)).then(() => nextTick(() => {
    if (selectedProjectKey !== null) focusProjectRow(selectedProjectKey);
  }));
}

function nextListPage() {
  if (listPageTransitioning.value || queries.list.isPlaceholderData.value) return;
  const nextCursor = listData.value?.meta.page?.nextCursor ?? null;
  if (!listData.value?.meta.page?.hasMore || nextCursor === null) return;
  listCursorHistory.value.push(routeState.value.cursor);
  pendingListCursorNavigation.value = { cursor: nextCursor };
  void navigate(setProjectsListCursor(routeState.value, nextCursor));
}

function previousListPage() {
  if (listPageTransitioning.value) return;
  const previous = listCursorHistory.value.length > 0
    ? listCursorHistory.value.pop() ?? null
    : null;
  pendingListCursorNavigation.value = { cursor: previous };
  void navigate(setProjectsListCursor(routeState.value, previous));
}

function resetDetailPages() {
  sessionCursor.value = null;
  sessionCursorHistory.value = [];
  sessionPageTransitioning.value = false;
  modelCursor.value = null;
  modelCursorHistory.value = [];
  modelPageTransitioning.value = false;
}

function nextSessionPage() {
  if (sessionPageTransitioning.value) return;
  const nextCursor = detailData.value?.sessionPage.nextCursor ?? null;
  if (!detailData.value?.sessionPage.hasMore || nextCursor === null) return;
  sessionPageTransitioning.value = true;
  sessionCursorHistory.value.push(sessionCursor.value);
  sessionCursor.value = nextCursor;
}

function previousSessionPage() {
  if (sessionPageTransitioning.value) return;
  sessionPageTransitioning.value = true;
  sessionCursor.value = sessionCursorHistory.value.pop() ?? null;
}

function nextModelPage() {
  if (modelPageTransitioning.value) return;
  const nextCursor = detailData.value?.modelPage.nextCursor ?? null;
  if (!detailData.value?.modelPage.hasMore || nextCursor === null) return;
  modelPageTransitioning.value = true;
  modelCursorHistory.value.push(modelCursor.value);
  modelCursor.value = nextCursor;
}

function previousModelPage() {
  if (modelPageTransitioning.value) return;
  modelPageTransitioning.value = true;
  modelCursor.value = modelCursorHistory.value.pop() ?? null;
}

function recoverList() {
  if (listError.value.recovery === "list_first_page") {
    listCursorHistory.value = [];
    pendingListCursorNavigation.value = { cursor: null };
    void navigate(setProjectsListCursor(routeState.value, null), true);
    return;
  }
  void queries.list.refetch();
}

function recoverDetail() {
  if (detailError.value.recovery === "close_detail") {
    closeDetail();
    return;
  }
  if (detailError.value.recovery === "session_first_page") {
    sessionPageTransitioning.value = true;
    sessionCursor.value = null;
    sessionCursorHistory.value = [];
    return;
  }
  if (detailError.value.recovery === "model_first_page") {
    modelPageTransitioning.value = true;
    modelCursor.value = null;
    modelCursorHistory.value = [];
    return;
  }
  void queries.detail.refetch();
}

watch(
  [
    () => routeState.value.projectKey,
    () => detailData.value?.item.dimensionKey,
  ],
  ([selectedProjectKey]) => {
    if (selectedProjectKey !== null) focusDetailIfReady(selectedProjectKey);
  },
);

const listRecoveryLabel = computed(() => (
  listError.value.recovery === "list_first_page"
    ? t("projects.state.firstPage")
    : t("projects.state.retry")
));
const detailRecoveryLabel = computed(() => {
  if (detailError.value.recovery === "close_detail") return t("projects.state.closeDetail");
  if (
    detailError.value.recovery === "session_first_page"
    || detailError.value.recovery === "model_first_page"
  ) return t("projects.state.firstPage");
  return t("projects.state.retry");
});
</script>

<template>
  <section data-testid="projects-view" class="w-full space-y-3 py-1">
    <p
      v-if="normalizedNotice"
      data-testid="projects-normalized"
      role="status"
      class="rounded-control bg-blue-50/80 px-3 py-2 text-xs text-blue-900/75"
    >
      {{ t("projects.state.normalized") }}
    </p>

    <ProjectsFilters :state="routeState" @change="updateFilters" />
    <p v-if="rangeError" role="alert" class="px-1 text-xs font-medium text-critical">{{ rangeError }}</p>

    <div v-if="listData" class="grid grid-cols-2 gap-2 lg:grid-cols-4">
      <article class="content-surface rounded-content px-4 py-3">
        <p class="text-[10px] text-ink-subtle">{{ t("projects.summary.matched", { count: formatCount(listData.matchedCount) }) }}</p>
        <p class="mt-1 font-mono text-lg font-bold text-ink">{{ formatCount(listData.matchedCount) }}</p>
      </article>
      <article class="content-surface rounded-content px-4 py-3">
        <p class="text-[10px] text-ink-subtle">{{ t("projects.summary.globalTokens") }}</p>
        <p class="mt-1 font-mono text-lg font-bold text-ink">{{ formatCompactTokens(listData.globalTotals.totalTokens) }}</p>
        <p class="mt-0.5 font-mono text-[10px] text-ink-subtle">{{ formatMicroUSD(listData.globalTotals.estimatedUsdMicros) }}</p>
      </article>
      <article class="content-surface rounded-content px-4 py-3">
        <p class="text-[10px] text-ink-subtle">{{ t("projects.summary.matchedTokens") }}</p>
        <p class="mt-1 font-mono text-lg font-bold text-accent">{{ formatCompactTokens(listData.matchedTotals.totalTokens) }}</p>
        <p class="mt-0.5 font-mono text-[10px] text-ink-subtle">{{ formatMicroUSD(listData.matchedTotals.estimatedUsdMicros) }}</p>
      </article>
      <article class="content-surface rounded-content px-4 py-3">
        <p class="text-[10px] text-ink-subtle">{{ t("projects.summary.pageTokens") }}</p>
        <p class="mt-1 font-mono text-lg font-bold text-violet">{{ formatCompactTokens(listData.pageTotals.totalTokens) }}</p>
        <p class="mt-0.5 font-mono text-[10px] text-ink-subtle">{{ formatMicroUSD(listData.pageTotals.estimatedUsdMicros) }}</p>
      </article>
    </div>

    <div class="grid items-start gap-4 xl:grid-cols-[minmax(20rem,25rem)_minmax(0,1fr)]">
      <section ref="listRegion" data-testid="projects-list-region" class="min-w-0 space-y-3">
        <div class="flex min-h-7 flex-wrap items-center justify-between gap-2 px-1">
          <p class="text-xs text-ink-muted">
            {{ t("projects.summary.matched", { count: listData ? formatCount(listData.matchedCount) : "--" }) }}
            <span class="ml-1 text-ink-subtle">· {{ t("projects.summary.estimate") }}</span>
          </p>
          <div class="flex items-center gap-2 text-[11px]">
            <span v-if="listPartial" class="rounded-full bg-amber-50 px-2 py-1 font-semibold text-amber-800">
              {{ t("projects.state.partial") }}
            </span>
            <span v-if="listStale" class="rounded-full bg-slate-100 px-2 py-1 text-slate-600">
              {{ t("projects.state.stale") }}
            </span>
            <span v-if="queries.list.isFetching.value && listData" class="text-ink-subtle">
              {{ t("projects.state.refreshing") }}
            </span>
          </div>
        </div>

        <StateSkeleton
          v-if="queries.list.isPending.value && !listData"
          data-testid="projects-list-loading"
          :label="t('projects.state.loading')"
          :rows="6"
        />
        <StateError
          v-else-if="listFatal"
          data-testid="projects-list-error"
          action-test-id="projects-list-recover"
          :title="t('projects.state.listErrorTitle')"
          :description="t('projects.state.listErrorDescription')"
          :action-label="listRecoveryLabel"
          @retry="recoverList"
        />
        <StateEmpty
          v-else-if="items.length === 0"
          data-testid="projects-list-empty"
          :title="t('projects.state.emptyTitle')"
          :description="t('projects.state.emptyDescription')"
        />
        <ProjectsList
          v-else
          :items="items as ProjectItem[]"
          :selected-project-key="routeState.projectKey"
          :time-zone="listData?.reportingTimeZone || timeZone"
          @select="chooseProject"
        />

        <div v-if="listData && items.length > 0" class="flex items-center justify-between gap-2 px-1">
          <UiButton
            data-testid="projects-list-previous"
            variant="quiet"
            :disabled="!hasListPrevious"
            @click="previousListPage"
          >
            {{ t("projects.pagination.previous") }}
          </UiButton>
          <UiButton
            data-testid="projects-list-next"
            variant="quiet"
            :disabled="listPageTransitioning || queries.list.isPlaceholderData.value || !listData.meta.page?.hasMore || !listData.meta.page?.nextCursor"
            @click="nextListPage"
          >
            {{ t("projects.pagination.next") }}
          </UiButton>
        </div>
      </section>

      <section class="min-w-0">
        <StateEmpty
          v-if="routeState.projectKey === null"
          data-testid="projects-detail-empty"
          :title="t('projects.state.selectTitle')"
          :description="t('projects.state.selectDescription')"
        />
        <StateSkeleton
          v-else-if="queries.detail.isPending.value && !detailData"
          data-testid="projects-detail-loading"
          :label="t('projects.state.loading')"
          :rows="7"
        />
        <StateError
          v-else-if="detailFatal"
          data-testid="projects-detail-error"
          action-test-id="projects-detail-recover"
          :title="t('projects.state.detailErrorTitle')"
          :description="t('projects.state.detailErrorDescription')"
          :action-label="detailRecoveryLabel"
          @retry="recoverDetail"
        />
        <div v-else-if="detailData" class="space-y-2">
          <div
            v-if="detailStale"
            data-testid="projects-detail-stale"
            class="flex items-center justify-between gap-2 rounded-control bg-amber-50 px-3 py-2 text-xs text-amber-900"
          >
            <span>{{ t("projects.state.stale") }}</span>
            <button
              type="button"
              data-testid="projects-detail-stale-retry"
              class="font-semibold underline decoration-amber-500/60 underline-offset-2"
              @click="queries.detail.refetch()"
            >
              {{ t("projects.state.retry") }}
            </button>
          </div>
          <ProjectDetailPanel
            ref="detailPanel"
            :detail="detailData"
            :has-previous-model-page="modelCursorHistory.length > 0 || modelCursor !== null"
            :has-previous-session-page="sessionCursorHistory.length > 0 || sessionCursor !== null"
            :is-fetching="queries.detail.isFetching.value"
            :time-zone="detailData.reportingTimeZone || timeZone"
            @close="closeDetail"
            @next-model-page="nextModelPage"
            @next-session-page="nextSessionPage"
            @previous-model-page="previousModelPage"
            @previous-session-page="previousSessionPage"
          />
        </div>
      </section>
    </div>
  </section>
</template>
