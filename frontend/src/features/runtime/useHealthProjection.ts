import { useQuery } from "@tanstack/vue-query";

import { healthProjectionQueryOptions } from "@/queries/business";

export function useHealthProjection() {
  return useQuery(healthProjectionQueryOptions());
}
