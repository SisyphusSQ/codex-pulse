import type { EChartsCoreOption } from "echarts/core";

import type { ProjectDailyPoint } from "@bindings/github.com/SisyphusSQ/codex-pulse/internal/query/usagecost/models";
import { numericValue } from "@/features/overview/format";

export function createProjectTrendOption(
  points: readonly ProjectDailyPoint[],
  timeZone: string,
  reducedMotion: boolean,
  compact: boolean,
  unitLabel: string,
): EChartsCoreOption {
  const dateFormatter = new Intl.DateTimeFormat("zh-CN", {
    day: "numeric",
    month: "numeric",
    timeZone,
  });
  const categories = points.map((point) => {
    const start = numericValue(point.bucketStartAtMs);
    return start === null ? "--" : dateFormatter.format(start);
  });
  return {
    animation: !reducedMotion,
    aria: { decal: { show: true }, enabled: true },
    grid: compact
      ? { bottom: 4, left: 3, right: 3, top: 4 }
      : { bottom: 24, containLabel: true, left: 12, right: 12, top: 16 },
    series: [{
      areaStyle: { color: "rgba(22, 131, 248, .10)" },
      data: points.map((point) => numericValue(point.totals.totalTokens)),
      emphasis: { focus: "series" },
      itemStyle: { color: "#1683f8" },
      lineStyle: { color: "#1683f8", width: compact ? 1.5 : 2 },
      name: unitLabel,
      showSymbol: !compact,
      smooth: true,
      symbolSize: 6,
      type: "line",
    }],
    tooltip: {
      show: !compact,
      trigger: "axis",
      valueFormatter: (value: unknown) => {
        if (value === null || value === undefined || value === "-") return "--";
        const numeric = Number(value);
        return Number.isFinite(numeric)
          ? `${numeric.toLocaleString("zh-CN")} ${unitLabel}`
          : "--";
      },
    },
    xAxis: {
      axisLabel: { color: "#8e97a7", fontSize: 10, show: !compact },
      axisLine: { show: false },
      axisTick: { show: false },
      boundaryGap: false,
      data: categories,
      type: "category",
    },
    yAxis: {
      axisLabel: { color: "#8e97a7", fontSize: 10, show: !compact },
      axisLine: { show: false },
      axisTick: { show: false },
      splitLine: { lineStyle: { color: "rgba(99,112,132,.09)" }, show: !compact },
      type: "value",
    },
  };
}
