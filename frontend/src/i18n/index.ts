import { createI18n } from "vue-i18n";

import { zhCNMessages } from "./messages/zh-CN";

export const supportedLocale = "zh-CN" as const;

export interface AppI18nOptions {
  onMissing?: (locale: string, key: string) => void;
}

export interface AppFormatterOptions {
  now?: () => number;
  timeZone?: string;
}

function reportMissingMessage(locale: string, key: string) {
  if (import.meta.env.DEV) {
    console.warn(`[i18n] missing message: ${locale}:${key}`);
  }
}

export function createAppI18n(options: AppI18nOptions = {}) {
  const onMissing = options.onMissing ?? reportMissingMessage;

  return createI18n({
    legacy: false,
    locale: supportedLocale,
    fallbackLocale: supportedLocale,
    messages: {
      [supportedLocale]: zhCNMessages,
    },
    missing(_locale, key) {
      onMissing(supportedLocale, key);
      return key;
    },
  });
}

export function createAppFormatters(options: AppFormatterOptions = {}) {
  const now = options.now ?? Date.now;
  const timeZone = options.timeZone ?? Intl.DateTimeFormat().resolvedOptions().timeZone;
  const numberFormatter = new Intl.NumberFormat(supportedLocale, { maximumFractionDigits: 2 });
  const dateTimeFormatter = new Intl.DateTimeFormat(supportedLocale, {
    dateStyle: "medium",
    timeStyle: "short",
    timeZone,
  });
  const relativeFormatter = new Intl.RelativeTimeFormat(supportedLocale, { numeric: "always" });

  return {
    dateTime(valueMS: number) {
      return dateTimeFormatter.format(new Date(valueMS));
    },
    number(value: number) {
      return numberFormatter.format(value);
    },
    relativeTime(valueMS: number) {
      const deltaSeconds = Math.round((valueMS - now()) / 1000);
      const absoluteSeconds = Math.abs(deltaSeconds);
      if (absoluteSeconds < 60) {
        return relativeFormatter.format(deltaSeconds, "second");
      }
      const deltaMinutes = Math.round(deltaSeconds / 60);
      if (Math.abs(deltaMinutes) < 60) {
        return relativeFormatter.format(deltaMinutes, "minute");
      }
      const deltaHours = Math.round(deltaMinutes / 60);
      if (Math.abs(deltaHours) < 24) {
        return relativeFormatter.format(deltaHours, "hour");
      }
      return relativeFormatter.format(Math.round(deltaHours / 24), "day");
    },
  };
}
