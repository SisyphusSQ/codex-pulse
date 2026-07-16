import type { NumericValue } from "@bindings/github.com/SisyphusSQ/codex-pulse/internal/query/models";
import {
  QuotaCurrentFreshness,
  SourceFreshness,
} from "@bindings/github.com/SisyphusSQ/codex-pulse/internal/store/models";

const compactTokenFormatter = new Intl.NumberFormat("zh-CN", {
  maximumFractionDigits: 1,
  notation: "compact",
});
const microUSDFormatter = new Intl.NumberFormat("zh-CN", {
  currency: "USD",
  currencyDisplay: "narrowSymbol",
  maximumFractionDigits: 2,
  minimumFractionDigits: 2,
  style: "currency",
});
const countFormatter = new Intl.NumberFormat("zh-CN", { maximumFractionDigits: 0 });

export function numericValue(value: NumericValue) {
  return value.value;
}

export function formatCompactTokens(value: NumericValue) {
  const known = numericValue(value);
  return known === null ? "--" : compactTokenFormatter.format(known);
}

export function formatMicroUSD(value: NumericValue) {
  const known = numericValue(value);
  return known === null ? "--" : microUSDFormatter.format(known / 1_000_000);
}

export function formatPercent(value: number | null) {
  return value === null ? "--" : `${Math.round(value)}%`;
}

export function formatCount(value: NumericValue) {
  const known = numericValue(value);
  return known === null ? "--" : countFormatter.format(known);
}

export function formatDateTime(value: number | null, timeZone: string) {
  if (value === null) return "--";
  return new Intl.DateTimeFormat("zh-CN", {
    dateStyle: "medium",
    timeStyle: "short",
    timeZone,
  }).format(value);
}

export function formatOverviewDay(value: number | null, fallback: string, timeZone: string) {
  if (value === null) return fallback;
  return new Intl.DateTimeFormat("zh-CN", {
    day: "numeric",
    month: "long",
    timeZone,
    year: "numeric",
  }).format(value);
}

export interface FreshnessLabels {
  current: string;
  lastKnown: string;
  stale: string;
  unavailable: string;
  unknown: string;
}

export function formatQuotaWindowFreshness(
  freshness: QuotaCurrentFreshness,
  labels: FreshnessLabels,
) {
  switch (freshness) {
    case QuotaCurrentFreshness.QuotaCurrentFresh:
      return labels.current;
    case QuotaCurrentFreshness.QuotaCurrentStale:
      return labels.lastKnown;
    case QuotaCurrentFreshness.QuotaCurrentExpiredUnknown:
    case QuotaCurrentFreshness.QuotaCurrentSuspicious:
      return labels.stale;
    case QuotaCurrentFreshness.QuotaCurrentNeverLoaded:
    case QuotaCurrentFreshness.$zero:
      return labels.unknown;
  }
}

export function formatSourceFreshness(
  freshness: SourceFreshness,
  labels: FreshnessLabels,
) {
  switch (freshness) {
    case SourceFreshness.SourceFreshnessCurrent:
      return labels.current;
    case SourceFreshness.SourceFreshnessStale:
      return labels.lastKnown;
    case SourceFreshness.SourceFreshnessUnavailable:
      return labels.unavailable;
    case SourceFreshness.SourceFreshnessUnknown:
    case SourceFreshness.$zero:
      return labels.unknown;
  }
}
