<script setup lang="ts">
import { computed } from "vue";

import type { DataHealthRuntimePoint } from "@bindings/github.com/SisyphusSQ/codex-pulse/internal/query/runtimeinfo/models";

const props = defineProps<{
  label: string;
  points: readonly DataHealthRuntimePoint[];
}>();

const maximumRSS = computed(() => Math.max(1, ...props.points.map((point) => point.peakRssBytes.value ?? point.rssBytes.value ?? 0)));

function cpuHeight(value: number) {
  return Math.max(2, Math.min(100, value));
}

function rssHeight(point: DataHealthRuntimePoint) {
  return Math.max(2, Math.min(100, ((point.rssBytes.value ?? 0) / maximumRSS.value) * 100));
}
</script>

<template>
  <div role="img" :aria-label="label" class="mt-4 space-y-2 rounded-control bg-slate-50 p-3">
    <div data-testid="data-health-trend-cpu" class="flex h-12 items-end gap-px" aria-hidden="true">
      <span
        v-for="point in points"
        :key="point.capturedAtMs.value ?? 0"
        data-testid="data-health-cpu-point"
        class="min-w-px flex-1 rounded-t-sm bg-blue-400"
        :style="{ height: `${cpuHeight(point.cpuPercent)}%` }"
      />
    </div>
    <div data-testid="data-health-trend-rss" class="flex h-12 items-end gap-px" aria-hidden="true">
      <span
        v-for="point in points"
        :key="`rss-${point.capturedAtMs.value ?? 0}`"
        data-testid="data-health-rss-point"
        class="min-w-px flex-1 rounded-t-sm bg-violet-400"
        :style="{ height: `${rssHeight(point)}%` }"
      />
    </div>
  </div>
</template>
