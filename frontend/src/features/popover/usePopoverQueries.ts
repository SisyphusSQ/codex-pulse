import { keepPreviousData, useQuery } from "@tanstack/vue-query";
import { computed, ref, toValue, type MaybeRefOrGetter } from "vue";

import { quotaCurrentQueryOptions, sessionListQueryOptions, usageCostQueryOptions } from "@/queries/business";
import { createPopoverRequests } from "./requests";

export function usePopoverQueries(nowMS: MaybeRefOrGetter<number> = ref(Date.now())) {
  const resolved = Intl.DateTimeFormat().resolvedOptions().timeZone;
  const timeZone = resolved && resolved !== "Local" ? resolved : "UTC";
  const requests = computed(() => createPopoverRequests(toValue(nowMS), timeZone));
  return {
    quota: useQuery({ ...quotaCurrentQueryOptions(), placeholderData: keepPreviousData }),
    sessions: useQuery(computed(() => ({
      ...sessionListQueryOptions(requests.value.sessions),
      placeholderData: keepPreviousData,
    }))),
    usage: useQuery(computed(() => ({
      ...usageCostQueryOptions(requests.value.usage),
      placeholderData: keepPreviousData,
    }))),
    timeZone,
  };
}
