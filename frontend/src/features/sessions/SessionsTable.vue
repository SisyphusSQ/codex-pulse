<script setup lang="ts">
import { useI18n } from "vue-i18n";

import type { SessionItem } from "@bindings/github.com/SisyphusSQ/codex-pulse/internal/query/usagecost/models";
import {
  formatCompactTokens,
  formatDateTime,
  formatMicroUSD,
  numericValue,
} from "@/features/overview/format";

defineProps<{
  items: readonly SessionItem[];
  selectedSessionId: string | null;
  timeZone: string;
}>();

const emit = defineEmits<{
  select: [sessionId: string];
}>();

const { t } = useI18n();

function attributionName(value: { displayName: string | null }, fallbackKey: string) {
  return value.displayName || t(fallbackKey);
}

function confidenceLabel(value: string) {
  const key = ["high", "medium", "low"].includes(value) ? value : "unknown";
  return t(`sessions.confidence.${key}`);
}

function activityLabel(value: string) {
  return value === "active" ? t("sessions.activity.active") : t("sessions.activity.idle");
}
</script>

<template>
  <div class="overflow-x-auto rounded-content border border-line bg-white/75">
    <table class="w-full min-w-[760px] border-collapse text-left text-xs">
      <caption class="sr-only">{{ t("sessions.table.caption") }}</caption>
      <thead class="border-b border-line text-[11px] font-medium text-ink-subtle">
        <tr>
          <th scope="col" class="px-4 py-3">{{ t("sessions.table.session") }}</th>
          <th scope="col" class="px-3 py-3">{{ t("sessions.table.project") }}</th>
          <th scope="col" class="px-3 py-3">{{ t("sessions.table.model") }}</th>
          <th scope="col" class="px-3 py-3">{{ t("sessions.table.activity") }}</th>
          <th scope="col" class="px-3 py-3">{{ t("sessions.table.lastActivity") }}</th>
          <th scope="col" class="px-3 py-3 text-right">{{ t("sessions.table.tokens") }}</th>
          <th scope="col" class="px-4 py-3 text-right">{{ t("sessions.table.cost") }}</th>
        </tr>
      </thead>
      <tbody class="divide-y divide-line text-ink">
        <tr
          v-for="item in items"
          :key="item.sessionId"
          data-testid="session-row"
          :aria-label="item.displayTitle || t('sessions.table.unknownTitle')"
          :aria-selected="selectedSessionId === item.sessionId"
          tabindex="0"
          class="cursor-pointer transition-colors hover:bg-black/[0.025] focus-visible:bg-blue-50/70"
          :class="selectedSessionId === item.sessionId ? 'bg-surface-selected/70' : ''"
          @click="emit('select', item.sessionId)"
          @keydown.enter="emit('select', item.sessionId)"
          @keydown.space.prevent="emit('select', item.sessionId)"
        >
          <td class="max-w-64 px-4 py-3.5">
            <div class="flex items-center gap-2">
              <span
                aria-hidden="true"
                class="size-1.5 shrink-0 rounded-full"
                :class="item.activity === 'active' ? 'bg-accent' : 'bg-slate-300'"
              />
              <div class="min-w-0">
                <p class="truncate text-sm font-semibold">{{ item.displayTitle || t("sessions.table.unknownTitle") }}</p>
                <p class="mt-0.5 text-[10px] text-ink-subtle">{{ confidenceLabel(item.titleConfidence) }}</p>
              </div>
            </div>
          </td>
          <td class="max-w-40 px-3 py-3.5 text-ink-muted">
            <span class="block truncate">{{ attributionName(item.project, "sessions.table.unknownProject") }}</span>
            <span class="mt-0.5 block text-[10px] text-ink-subtle">{{ confidenceLabel(item.project.confidence) }}</span>
          </td>
          <td class="max-w-36 px-3 py-3.5 font-mono text-[11px] text-ink-muted">
            <span class="block truncate">{{ attributionName(item.model, "sessions.table.unknownModel") }}</span>
            <span class="mt-0.5 block font-sans text-[10px] text-ink-subtle">{{ confidenceLabel(item.model.confidence) }}</span>
          </td>
          <td class="px-3 py-3.5">
            <span
              class="inline-flex rounded-full px-2 py-1 text-[10px] font-bold tracking-wide"
              :class="item.activity === 'active'
                ? 'bg-emerald-50 text-emerald-700'
                : 'bg-slate-100 text-slate-500'"
            >
              {{ activityLabel(item.activity) }}
            </span>
          </td>
          <td class="whitespace-nowrap px-3 py-3.5 text-ink-subtle">
            {{ formatDateTime(numericValue(item.lastActivityAtMs), timeZone) }}
          </td>
          <td class="whitespace-nowrap px-3 py-3.5 text-right font-mono text-[11px]">
            {{ formatCompactTokens(item.totals.totalTokens) }}
          </td>
          <td class="whitespace-nowrap px-4 py-3.5 text-right font-mono text-[11px]">
            {{ formatMicroUSD(item.totals.estimatedUsdMicros) }}
          </td>
        </tr>
      </tbody>
    </table>
  </div>
</template>
