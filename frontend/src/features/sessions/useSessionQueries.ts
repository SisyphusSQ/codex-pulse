import { keepPreviousData, useQuery } from "@tanstack/vue-query";
import { computed, type ComputedRef, type Ref } from "vue";

import {
  sessionDetailQueryOptions,
  sessionListQueryOptions,
} from "@/queries/business";

import {
  createSessionDetailRequest,
  createSessionListRequest,
} from "./requests";
import type { SessionsRouteState } from "./routeState";

export function useSessionQueries(
  state: ComputedRef<SessionsRouteState>,
  turnCursor: Ref<string | null>,
  timeZone: string,
  now: () => number = Date.now,
) {
  const requests = computed(() => ({
    detail: state.value.sessionId === null
      ? null
      : createSessionDetailRequest(state.value.sessionId, turnCursor.value, timeZone),
    list: createSessionListRequest(state.value, now(), timeZone),
  }));

  const list = useQuery(computed(() => ({
    ...sessionListQueryOptions(requests.value.list),
    placeholderData: keepPreviousData,
  })));
  const detail = useQuery(computed(() => ({
    ...sessionDetailQueryOptions(
      requests.value.detail
      ?? createSessionDetailRequest("disabled-session", null, timeZone),
    ),
    enabled: requests.value.detail !== null,
  })));

  return { detail, list, requests };
}
