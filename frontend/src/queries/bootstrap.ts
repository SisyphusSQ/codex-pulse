import { queryOptions } from "@tanstack/vue-query";

import { Bootstrap } from "@bindings/github.com/SisyphusSQ/codex-pulse/internal/app/service";

export const bootstrapQueryKey = ["application", "bootstrap"] as const;

export function bootstrapQueryOptions() {
  return queryOptions({
    queryKey: bootstrapQueryKey,
    queryFn: () => Bootstrap(),
    retry: false,
    staleTime: Number.POSITIVE_INFINITY,
  });
}
