import type { EChartsCoreOption } from "echarts/core";

import type { TrendPoint } from "@bindings/github.com/SisyphusSQ/codex-pulse/internal/query/usagecost/models";

import { numericValue } from "./format";

export interface UsageTrendLabels {
  cached: string;
  input: string;
  output: string;
  reasoning: string;
  unit: string;
}

export function createUsageTrendOption(
  points: readonly TrendPoint[],
  labels: UsageTrendLabels,
  timeZone: string,
  reducedMotion: boolean,
): EChartsCoreOption {
  const dateFormatter = new Intl.DateTimeFormat("zh-CN", {
    day: "numeric",
    month: "numeric",
    timeZone,
  });
  const categories = points.map((point) => {
    const start = numericValue(point.startAtMs);
    return start === null ? point.key : dateFormatter.format(start);
  });
  const series = [
    [labels.input, "#1683f8", points.map((point) => numericValue(point.totals.inputTokens))],
    [labels.cached, "#57c5ee", points.map((point) => numericValue(point.totals.cachedInputTokens))],
    [labels.output, "#7657ff", points.map((point) => numericValue(point.totals.outputTokens))],
    [labels.reasoning, "#ff980f", points.map((point) => numericValue(point.totals.reasoningTokens))],
  ].map(([name, color, data]) => ({
    barGap: "18%",
    barMaxWidth: 14,
    data,
    emphasis: { focus: "series" },
    itemStyle: { borderRadius: [7, 7, 4, 4], color },
    name,
    type: "bar",
  }));

  return {
    animation: !reducedMotion,
    aria: { decal: { show: true }, enabled: true },
    grid: { bottom: 30, containLabel: true, left: 12, right: 12, top: 34 },
    legend: {
      icon: "circle",
      itemHeight: 7,
      itemWidth: 7,
      right: 0,
      textStyle: { color: "#687182", fontSize: 11 },
      top: 0,
    },
    series,
    tooltip: {
      trigger: "axis",
      valueFormatter: (value: unknown) => {
        if (value === null || value === undefined || value === "-") return "--";
        const numeric = Number(value);
        return Number.isFinite(numeric) ? `${numeric.toLocaleString("zh-CN")} ${labels.unit}` : "--";
      },
    },
    xAxis: {
      axisLine: { show: false },
      axisTick: { show: false },
      data: categories,
      type: "category",
      axisLabel: { color: "#8e97a7", fontSize: 11 },
    },
    yAxis: {
      axisLabel: { color: "#8e97a7", fontSize: 10 },
      splitLine: { lineStyle: { color: "rgba(99,112,132,.09)" } },
      type: "value",
    },
  };
}
