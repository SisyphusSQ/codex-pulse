<script setup lang="ts">
import { LineChart } from "echarts/charts";
import {
  AriaComponent,
  GridComponent,
  TooltipComponent,
} from "echarts/components";
import { init, use, type ECharts } from "echarts/core";
import { CanvasRenderer } from "echarts/renderers";
import { computed, onBeforeUnmount, onMounted, ref, watch } from "vue";

import type { ProjectDailyPoint } from "@bindings/github.com/SisyphusSQ/codex-pulse/internal/query/usagecost/models";

import { createProjectTrendOption } from "./projectChartOptions";

use([LineChart, AriaComponent, GridComponent, TooltipComponent, CanvasRenderer]);

const props = withDefaults(defineProps<{
  compact?: boolean;
  label: string;
  points: readonly ProjectDailyPoint[];
  timeZone: string;
  unitLabel: string;
}>(), {
  compact: false,
});

const host = ref<HTMLDivElement>();
const reducedMotion = ref(false);
let chart: ECharts | undefined;
let resizeObserver: ResizeObserver | undefined;
let motionQuery: MediaQueryList | undefined;

const option = computed(() => createProjectTrendOption(
  props.points,
  props.timeZone,
  reducedMotion.value,
  props.compact,
  props.unitLabel,
));

function updateMotionPreference(event: Pick<MediaQueryListEvent, "matches">) {
  reducedMotion.value = event.matches;
}

onMounted(() => {
  if (!host.value) return;
  motionQuery = window.matchMedia?.("(prefers-reduced-motion: reduce)");
  reducedMotion.value = motionQuery?.matches ?? false;
  motionQuery?.addEventListener("change", updateMotionPreference);
  chart = init(host.value, undefined, { renderer: "canvas" });
  chart.setOption(option.value);
  if (typeof ResizeObserver !== "undefined") {
    resizeObserver = new ResizeObserver(() => chart?.resize());
    resizeObserver.observe(host.value);
  }
});

watch(option, (value) => chart?.setOption(value, { notMerge: true }));

onBeforeUnmount(() => {
  motionQuery?.removeEventListener("change", updateMotionPreference);
  resizeObserver?.disconnect();
  chart?.dispose();
});
</script>

<template>
  <div
    ref="host"
    role="img"
    :aria-label="label"
    :class="compact ? 'h-12 min-h-12 w-24' : 'h-48 min-h-48 w-full'"
  />
</template>
