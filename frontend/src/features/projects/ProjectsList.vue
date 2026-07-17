<script setup lang="ts">
import { computed, defineAsyncComponent } from "vue";
import { useI18n } from "vue-i18n";

import type { ProjectItem } from "@bindings/github.com/SisyphusSQ/codex-pulse/internal/query/usagecost/models";
import {
  formatCompactTokens,
  formatCount,
  formatDateTime,
  formatMicroUSD,
  numericValue,
} from "@/features/overview/format";

const ProjectTrendChart = defineAsyncComponent(() => import("./ProjectTrendChart.vue"));

const props = defineProps<{
  items: readonly ProjectItem[];
  selectedProjectKey: string | null;
  timeZone: string;
}>();

const emit = defineEmits<{
  select: [projectKey: string];
}>();

const { t } = useI18n();
const itemCount = computed(() => props.items.length);

function projectName(item: ProjectItem) {
  return item.project.displayName || t("projects.list.unknownProject");
}

function confidenceLabel(value: string) {
  const key = ["high", "medium", "low"].includes(value) ? value : "unknown";
  return t(`projects.confidence.${key}`);
}

function reasonLabel(value: string) {
  const keys: Record<string, string> = {
    conflict: "projects.reason.conflict",
    invalid: "projects.reason.invalid",
    missing: "projects.reason.missing",
    mixed: "projects.reason.mixed",
    path_derived: "projects.reason.pathDerived",
    root_matched: "projects.reason.rootMatched",
  };
  return t(keys[value] ?? "projects.reason.unknown");
}
</script>

<template>
  <div class="space-y-2" role="list" :aria-label="t('projects.list.label', { count: itemCount })">
    <article
      v-for="item in items"
      :key="item.dimensionKey"
      role="listitem"
    >
      <button
        type="button"
        data-testid="project-row"
        :aria-label="projectName(item)"
        :aria-pressed="selectedProjectKey === item.dimensionKey"
        class="content-surface flex w-full items-center gap-3 rounded-content px-4 py-3 text-left transition-colors hover:bg-white focus-visible:bg-blue-50/70"
        :class="selectedProjectKey === item.dimensionKey ? 'border-blue-200 bg-surface-selected/70' : ''"
        @click="emit('select', item.dimensionKey)"
      >
        <div class="min-w-0 flex-1">
        <div class="flex items-start justify-between gap-3">
          <div class="min-w-0">
            <p class="truncate text-sm font-semibold text-ink">{{ projectName(item) }}</p>
            <p class="mt-0.5 text-[10px] text-ink-subtle">
              {{ confidenceLabel(item.project.confidence) }} · {{ reasonLabel(item.project.reason) }}
            </p>
          </div>
          <ProjectTrendChart
            v-if="item.trend?.length"
            compact
            :points="item.trend"
            :time-zone="timeZone"
            :label="t('projects.list.trendLabel', { project: projectName(item) })"
            :unit-label="t('projects.common.tokenUnit')"
          />
        </div>
        <dl class="mt-3 grid grid-cols-2 gap-x-3 gap-y-2 text-[10px] sm:grid-cols-4">
          <div>
            <dt class="text-ink-subtle">{{ t("projects.list.sessions") }}</dt>
            <dd class="mt-0.5 font-semibold text-ink">
              {{ t("projects.list.sessionCount", { count: formatCount(item.sessionCount) }) }}
            </dd>
          </div>
          <div>
            <dt class="text-ink-subtle">{{ t("projects.list.tokens") }}</dt>
            <dd class="mt-0.5 font-mono font-semibold text-ink">{{ formatCompactTokens(item.totals.totalTokens) }}</dd>
          </div>
          <div>
            <dt class="text-ink-subtle">{{ t("projects.list.cost") }}</dt>
            <dd class="mt-0.5 font-mono font-semibold text-ink">{{ formatMicroUSD(item.totals.estimatedUsdMicros) }}</dd>
          </div>
          <div>
            <dt class="text-ink-subtle">{{ t("projects.list.lastActivity") }}</dt>
            <dd class="mt-0.5 truncate text-ink-muted">
              {{ formatDateTime(numericValue(item.totals.lastActivityAtMs), timeZone) }}
            </dd>
          </div>
        </dl>
        </div>
      </button>
    </article>
  </div>
</template>
