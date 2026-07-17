import { useQuery } from "@tanstack/vue-query";
import { computed, onMounted, onUnmounted, ref } from "vue";

import { createRuntimeRequests } from "@/features/runtime/requests";
import { healthListQueryOptions } from "@/queries/business";

export type AppStatusKind =
  | "unavailable"
  | "blocked"
  | "offline"
  | "degraded"
  | "partial"
  | "stale"
  | "paused"
  | "busy"
  | "loading";

export interface AppStatus {
  kind: AppStatusKind;
  retryable: boolean;
}

export interface AppStatusInput {
  online: boolean;
  health: {
    data: {
      level: string;
      metaStatus: string;
    } | undefined;
    isError: boolean;
    isPending: boolean;
    isStale: boolean;
  };
}

const priorities: Record<AppStatusKind, number> = {
  unavailable: 100,
  blocked: 90,
  offline: 80,
  degraded: 70,
  partial: 60,
  stale: 50,
  paused: 40,
  busy: 30,
  loading: 20,
};

const retryable = new Set<AppStatusKind>([
  "unavailable",
  "blocked",
  "degraded",
  "partial",
  "stale",
]);

export function selectAppStatus(input: AppStatusInput): AppStatus | null {
  const candidates = new Set<AppStatusKind>();
  const { data } = input.health;

  if (data === undefined && input.health.isError) candidates.add("unavailable");
  if (data?.metaStatus === "unavailable") candidates.add("unavailable");
  if (data?.level === "blocked") candidates.add("blocked");
  if (!input.online) candidates.add("offline");
  if (data?.level === "degraded") candidates.add("degraded");
  if (data?.metaStatus === "partial") candidates.add("partial");
  if (data !== undefined && (input.health.isError || input.health.isStale)) candidates.add("stale");
  if (data?.level === "paused") candidates.add("paused");
  if (data?.level === "busy") candidates.add("busy");
  if (data === undefined && input.health.isPending) candidates.add("loading");

  const kind = Array.from(candidates).sort((left, right) => priorities[right] - priorities[left])[0];
  return kind === undefined ? null : { kind, retryable: retryable.has(kind) };
}

export function useAppStatus() {
  const online = ref(typeof navigator === "undefined" ? true : navigator.onLine);
  const health = useQuery(healthListQueryOptions(createRuntimeRequests().health));

  function updateOnlineState() {
    online.value = navigator.onLine;
  }

  onMounted(() => {
    window.addEventListener("online", updateOnlineState);
    window.addEventListener("offline", updateOnlineState);
  });
  onUnmounted(() => {
    window.removeEventListener("online", updateOnlineState);
    window.removeEventListener("offline", updateOnlineState);
  });

  const status = computed(() => selectAppStatus({
    online: online.value,
    health: {
      data: health.data.value === undefined
        ? undefined
        : {
            level: health.data.value.summary.level,
            metaStatus: health.data.value.meta.status,
          },
      isError: health.isError.value,
      isPending: health.isPending.value,
      isStale: health.isStale.value,
    },
  }));

  return {
    retry: () => health.refetch(),
    status,
  };
}
