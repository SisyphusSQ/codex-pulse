import type { LocalDateRange } from "@bindings/github.com/SisyphusSQ/codex-pulse/internal/query/models";

export type OverviewRangePreset = "today" | "7d" | "30d";

const localDatePattern = /^\d{4}-\d{2}-\d{2}$/u;
const maximumCustomDays = 366;

function dateParts(valueMS: number, timeZone: string) {
  const parts = new Intl.DateTimeFormat("en-CA", {
    day: "2-digit",
    month: "2-digit",
    timeZone,
    year: "numeric",
  }).formatToParts(new Date(valueMS));
  const value = (type: Intl.DateTimeFormatPartTypes) =>
    parts.find((part) => part.type === type)?.value ?? "";
  return `${value("year")}-${value("month")}-${value("day")}`;
}

function calendarDayValue(localDate: string) {
  if (!localDatePattern.test(localDate)) {
    throw new Error("invalid overview range");
  }
  const [year, month, day] = localDate.split("-").map(Number);
  const value = Date.UTC(year, month - 1, day);
  if (new Date(value).toISOString().slice(0, 10) !== localDate) {
    throw new Error("invalid overview range");
  }
  return value;
}

function addCalendarDays(localDate: string, days: number) {
  const value = calendarDayValue(localDate) + days * 86_400_000;
  return new Date(value).toISOString().slice(0, 10);
}

function validateTimeZone(timeZone: string) {
  if (!timeZone || timeZone === "Local") {
    throw new Error("invalid overview range");
  }
  try {
    new Intl.DateTimeFormat("en", { timeZone }).format(0);
  } catch {
    throw new Error("invalid overview range");
  }
}

export function createOverviewPresetRange(
  preset: OverviewRangePreset,
  nowMS: number,
  timeZone: string,
): LocalDateRange {
  validateTimeZone(timeZone);
  const today = dateParts(nowMS, timeZone);
  const days = preset === "today" ? 1 : preset === "7d" ? 7 : 30;
  return {
    startDate: addCalendarDays(today, 1 - days),
    endDateExclusive: addCalendarDays(today, 1),
    timeZone,
  };
}

export function createCustomOverviewRange(
  startDate: string,
  endDateExclusive: string,
  timeZone: string,
): LocalDateRange {
  validateTimeZone(timeZone);
  const start = calendarDayValue(startDate);
  const end = calendarDayValue(endDateExclusive);
  const days = (end - start) / 86_400_000;
  if (days < 1 || days > maximumCustomDays) {
    throw new Error("invalid overview range");
  }
  return { startDate, endDateExclusive, timeZone };
}
