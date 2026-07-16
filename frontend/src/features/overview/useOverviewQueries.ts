import { keepPreviousData, useQuery } from "@tanstack/vue-query";
import { computed, type Ref } from "vue";

import type { LocalDateRange } from "@bindings/github.com/SisyphusSQ/codex-pulse/internal/query/models";
import {
  healthListQueryOptions,
  projectListQueryOptions,
  quotaCurrentQueryOptions,
  sessionListQueryOptions,
  sourceListQueryOptions,
  usageCostQueryOptions,
} from "@/queries/business";

import { createOverviewRequests } from "./requests";

export function useOverviewQueries(range: Ref<LocalDateRange>) {
  const requests = computed(() => createOverviewRequests(range.value));

  const usage = useQuery(computed(() => ({
    ...usageCostQueryOptions(requests.value.usage),
    placeholderData: keepPreviousData,
  })));
  const sessions = useQuery(computed(() => ({
    ...sessionListQueryOptions(requests.value.sessions),
    placeholderData: keepPreviousData,
  })));
  const projects = useQuery(computed(() => ({
    ...projectListQueryOptions(requests.value.projects),
    placeholderData: keepPreviousData,
  })));
  const sources = useQuery(sourceListQueryOptions(requests.value.sources));
  const health = useQuery(healthListQueryOptions(requests.value.health));
  const quota = useQuery(quotaCurrentQueryOptions());

  return { health, projects, quota, sessions, sources, usage };
}
