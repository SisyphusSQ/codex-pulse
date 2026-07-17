<script setup lang="ts">
import { ArrowDown, ArrowUp } from "@lucide/vue";
import { computed } from "vue";
import { useI18n } from "vue-i18n";

import OverviewRangePicker from "@/features/overview/OverviewRangePicker.vue";
import type { OverviewRangePreset } from "@/features/overview/range";

import type {
  ProjectsConfidence,
  ProjectsFilterPatch,
  ProjectsRange,
  ProjectsRouteState,
  ProjectsSort,
} from "./routeState";

const props = defineProps<{
  state: ProjectsRouteState;
}>();

const emit = defineEmits<{
  change: [patch: ProjectsFilterPatch];
}>();

const { t } = useI18n();
const presetLabels = computed<Record<OverviewRangePreset, string>>(() => ({
  "30d": t("overview.range.thirtyDays"),
  "7d": t("overview.range.sevenDays"),
  today: t("overview.range.today"),
}));

function selectValue(event: Event) {
  return (event.currentTarget as HTMLSelectElement).value;
}

function changeRange(range: ProjectsRange) {
  emit("change", {
    endDateExclusive: range === "custom" ? props.state.endDateExclusive : null,
    range,
    startDate: range === "custom" ? props.state.startDate : null,
  });
}

function applyCustom(value: { startDate: string; endDateExclusive: string }) {
  emit("change", { ...value, range: "custom" });
}

function changeConfidence(event: Event) {
  emit("change", { confidence: selectValue(event) as ProjectsConfidence });
}

function changeSort(event: Event) {
  emit("change", { sort: selectValue(event) as ProjectsSort });
}

function toggleDirection() {
  emit("change", { direction: props.state.direction === "desc" ? "asc" : "desc" });
}
</script>

<template>
  <form
    :aria-label="t('projects.filters.label')"
    class="content-surface rounded-content px-4 py-3"
    @submit.prevent
  >
    <div class="flex flex-wrap items-end gap-3">
      <div class="min-w-fit flex-1">
        <p class="mb-1 text-[11px] font-medium text-ink-subtle">{{ t("projects.filters.range") }}</p>
        <OverviewRangePicker
          :model-value="state.range"
          :preset-labels="presetLabels"
          :custom-label="t('overview.range.custom')"
          :start-label="t('overview.range.start')"
          :end-label="t('overview.range.end')"
          :apply-label="t('overview.range.apply')"
          :custom-start="state.startDate ?? ''"
          :custom-end-exclusive="state.endDateExclusive ?? ''"
          @update:model-value="changeRange"
          @apply-custom="applyCustom"
        />
      </div>

      <label class="grid min-w-36 gap-1 text-[11px] font-medium text-ink-subtle">
        {{ t("projects.filters.confidence.label") }}
        <select
          data-testid="projects-confidence"
          :value="state.confidence"
          class="min-h-9 rounded-control border border-line bg-white/85 px-3 text-xs text-ink"
          @change="changeConfidence"
        >
          <option value="all">{{ t("projects.filters.confidence.all") }}</option>
          <option value="high">{{ t("projects.confidence.high") }}</option>
          <option value="medium">{{ t("projects.confidence.medium") }}</option>
          <option value="low">{{ t("projects.confidence.low") }}</option>
          <option value="unknown">{{ t("projects.confidence.unknown") }}</option>
        </select>
      </label>

      <label class="grid min-w-40 gap-1 text-[11px] font-medium text-ink-subtle">
        {{ t("projects.filters.sort.label") }}
        <select
          data-testid="projects-sort"
          :value="state.sort"
          class="min-h-9 rounded-control border border-line bg-white/85 px-3 text-xs text-ink"
          @change="changeSort"
        >
          <option value="lastActivityAt">{{ t("projects.filters.sort.lastActivity") }}</option>
          <option value="totalTokens">{{ t("projects.filters.sort.totalTokens") }}</option>
          <option value="estimatedCost">{{ t("projects.filters.sort.estimatedCost") }}</option>
          <option value="displayName">{{ t("projects.filters.sort.displayName") }}</option>
        </select>
      </label>

      <button
        type="button"
        data-testid="projects-direction"
        :aria-label="state.direction === 'desc'
          ? t('projects.filters.sort.descending')
          : t('projects.filters.sort.ascending')"
        class="flex size-9 items-center justify-center rounded-control border border-line bg-white/85 text-ink-muted transition-colors hover:bg-white hover:text-ink"
        @click="toggleDirection"
      >
        <ArrowDown v-if="state.direction === 'desc'" :size="16" aria-hidden="true" />
        <ArrowUp v-else :size="16" aria-hidden="true" />
      </button>
    </div>
  </form>
</template>
