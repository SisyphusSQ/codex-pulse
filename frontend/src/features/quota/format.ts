export interface ResetCountdown {
  unit: "day" | "hour" | "minute";
  value: number;
}

export function formatQuotaPercent(value: number | null) {
  return value === null ? "--" : `${Math.round(value)}%`;
}

export function quotaProgressValue(value: number | null) {
  return value === null ? null : Math.max(0, Math.min(100, value));
}

export function resetCountdown(resetsAtMS: number | null, nowMS: number): ResetCountdown | null {
  if (resetsAtMS === null) return null;
  const minutes = Math.max(0, Math.ceil((resetsAtMS - nowMS) / 60_000));
  if (minutes < 120) return { unit: "minute", value: minutes };
  const hours = Math.ceil(minutes / 60);
  if (hours < 48) return { unit: "hour", value: hours };
  return { unit: "day", value: Math.ceil(hours / 24) };
}
