import { keepPreviousData, useQuery } from "@tanstack/vue-query";
import { computed, ref, watch, type ComputedRef, type Ref } from "vue";

import {
  projectDetailQueryOptions,
  projectListQueryOptions,
} from "@/queries/business";

import {
  createProjectDetailRequest,
  createProjectListRequest,
  createProjectsRange,
} from "./requests";
import type { ProjectsRouteState } from "./routeState";

function rangeKey(range: { endDateExclusive: string; startDate: string; timeZone: string }) {
  return JSON.stringify([range.startDate, range.endDateExclusive, range.timeZone]);
}

export function useProjectQueries(
  state: ComputedRef<ProjectsRouteState>,
  sessionCursor: Ref<string | null>,
  modelCursor: Ref<string | null>,
  timeZone: string,
  now: () => number = Date.now,
) {
  const currentRangeKey = computed(() => rangeKey(createProjectsRange(
    state.value,
    now(),
    timeZone,
  )));
  const listCursorRangeKey = ref(currentRangeKey.value);
  const projectRangeKey = ref(currentRangeKey.value);
  const sessionCursorRangeKey = ref(currentRangeKey.value);
  const modelCursorRangeKey = ref(currentRangeKey.value);

  watch(
    () => state.value.cursor,
    () => { listCursorRangeKey.value = currentRangeKey.value; },
    { flush: "sync" },
  );
  watch(
    () => state.value.projectKey,
    () => { projectRangeKey.value = currentRangeKey.value; },
    { flush: "sync" },
  );
  watch(
    sessionCursor,
    () => { sessionCursorRangeKey.value = currentRangeKey.value; },
    { flush: "sync" },
  );
  watch(
    modelCursor,
    () => { modelCursorRangeKey.value = currentRangeKey.value; },
    { flush: "sync" },
  );

  const requests = computed(() => {
    const unscopedList = createProjectListRequest(state.value, now(), timeZone);
    const activeRangeKey = rangeKey(unscopedList.timeRange!);
    const list = listCursorRangeKey.value === activeRangeKey
      ? unscopedList
      : { ...unscopedList, page: { ...unscopedList.page, cursor: null } };
    const detail = state.value.projectKey === null
      || list.timeRange === null
      || projectRangeKey.value !== activeRangeKey
      ? null
      : createProjectDetailRequest(
          state.value.projectKey,
          list.timeRange,
          sessionCursorRangeKey.value === activeRangeKey ? sessionCursor.value : null,
          modelCursorRangeKey.value === activeRangeKey ? modelCursor.value : null,
        );
    return { detail, list };
  });

  const list = useQuery(computed(() => ({
    ...projectListQueryOptions(requests.value.list),
    placeholderData: keepPreviousData,
  })));
  const detail = useQuery(computed(() => ({
    ...projectDetailQueryOptions(
      requests.value.detail
      ?? createProjectDetailRequest(
        "disabled-project",
        requests.value.list.timeRange!,
        null,
        null,
      ),
    ),
    enabled: requests.value.detail !== null,
  })));

  return { detail, list, requests };
}
