import { computed, onMounted, onUnmounted, ref } from "vue";

import type { HealthComponentStatus, HealthProjectionResponse } from "@bindings/github.com/SisyphusSQ/codex-pulse/internal/app/models";
import { useHealthProjection } from "@/features/runtime/useHealthProjection";

export type AppStatusKind =
  | "unavailable"
  | "unknown"
  | "blocked"
  | "offline"
  | "degraded"
  | "stale"
  | "paused"
  | "busy"
  | "loading";

export interface AppStatus {
  kind: AppStatusKind;
  evaluatedAtMs: number | null;
  primary: HealthComponentStatus | null;
  retryable: boolean;
  lastTrusted: boolean;
  failure: string;
}

export interface AppStatusInput {
  online: boolean;
  health: {
    data: HealthProjectionResponse | undefined;
    isError: boolean;
    isPending: boolean;
  };
}

export function selectAppStatus(input: AppStatusInput): AppStatus | null {
  const { data } = input.health;
  const evaluatedAtMs = data?.evaluatedAtMs.value ?? null;
  const primary = data?.primary ?? null;
  const status = (kind: AppStatusKind, retryable = false): AppStatus => ({
    kind, evaluatedAtMs, primary, retryable,
    lastTrusted: data !== undefined && (data.stale || input.health.isError),
    failure: data?.failure ?? "none",
  });

  if (data?.hasValue && data.level === "blocked") return status("blocked", true);
  if (data?.hasValue && data.level === "degraded" && primary?.impact !== "none") return status("degraded", true);
  if (data === undefined && input.health.isError) return status("unavailable", true);
  if (!input.online) return status("offline");
  if (data === undefined && input.health.isPending) return status("loading");
  if (data !== undefined && !data.hasValue) return status("unknown", true);
  if (data?.stale || input.health.isError) return status("stale", true);
  if (data?.level === "paused") return status("paused");
  if (data?.level === "busy") return status("busy");
  return null;
}

export function useAppStatus() {
  const online = ref(typeof navigator === "undefined" ? true : navigator.onLine);
  const health = useHealthProjection();

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
      data: health.data.value,
      isError: health.isError.value,
      isPending: health.isPending.value,
    },
  }));

  return {
    retry: () => health.refetch({ throwOnError: true }),
    status,
  };
}
