<script setup lang="ts">
import { BarChart } from "echarts/charts";
import {
  AriaComponent,
  GridComponent,
  LegendComponent,
  TooltipComponent,
} from "echarts/components";
import { init, use, type ECharts } from "echarts/core";
import { CanvasRenderer } from "echarts/renderers";
import { computed, onBeforeUnmount, onMounted, ref, watch } from "vue";

import type { TrendPoint } from "@bindings/github.com/SisyphusSQ/codex-pulse/internal/query/usagecost/models";

import { createUsageTrendOption, type UsageTrendLabels } from "./chartOptions";

use([BarChart, AriaComponent, GridComponent, LegendComponent, TooltipComponent, CanvasRenderer]);

const props = defineProps<{
  label: string;
  labels: UsageTrendLabels;
  points: readonly TrendPoint[];
  timeZone: string;
}>();

const host = ref<HTMLDivElement>();
const reducedMotion = ref(false);
let chart: ECharts | undefined;
let resizeObserver: ResizeObserver | undefined;
let motionQuery: MediaQueryList | undefined;

const option = computed(() => createUsageTrendOption(
  props.points,
  props.labels,
  props.timeZone,
  reducedMotion.value,
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
    class="h-52 min-h-52 w-full"
  />
</template>
