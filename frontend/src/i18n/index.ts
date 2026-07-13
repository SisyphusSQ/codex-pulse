import { createI18n } from "vue-i18n";

import { zhCNMessages } from "./messages/zh-CN";

export const supportedLocale = "zh-CN" as const;

export interface AppI18nOptions {
  onMissing?: (locale: string, key: string) => void;
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
