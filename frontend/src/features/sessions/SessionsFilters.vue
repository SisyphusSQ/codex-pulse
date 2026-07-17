<script setup lang="ts">
import { ArrowDown, ArrowUp } from "@lucide/vue";
import { useI18n } from "vue-i18n";

import type {
  SessionsActivity,
  SessionsRange,
  SessionsRouteState,
  SessionsSort,
} from "./routeState";

export interface SessionsFilterOption {
  label: string;
  value: string;
}

const props = defineProps<{
  modelOptions: readonly SessionsFilterOption[];
  projectOptions: readonly SessionsFilterOption[];
  state: SessionsRouteState;
}>();

const emit = defineEmits<{
  change: [patch: Partial<SessionsRouteState>];
}>();

const { t } = useI18n();

const activities: SessionsActivity[] = ["all", "active", "idle"];

function activityLabel(activity: SessionsActivity) {
  return t(`sessions.filters.activity.${activity}`);
}

function selectValue(event: Event) {
  return (event.currentTarget as HTMLSelectElement).value;
}

function changeRange(event: Event) {
  emit("change", { range: selectValue(event) as SessionsRange });
}

function changeProject(event: Event) {
  emit("change", { projectId: selectValue(event) || null });
}

function changeModel(event: Event) {
  emit("change", { modelKey: selectValue(event) || null });
}

function changeSort(event: Event) {
  emit("change", { sort: selectValue(event) as SessionsSort });
}

function toggleDirection() {
  emit("change", { direction: props.state.direction === "desc" ? "asc" : "desc" });
}
</script>

<template>
  <form
    :aria-label="t('sessions.filters.label')"
    class="content-surface rounded-content px-4 py-3"
    @submit.prevent
  >
    <div class="flex flex-wrap items-end gap-3">
      <fieldset class="flex min-w-fit gap-1" :aria-label="t('sessions.filters.activity.label')">
        <legend class="sr-only">{{ t("sessions.filters.activity.label") }}</legend>
        <button
          v-for="activity in activities"
          :key="activity"
          type="button"
          :data-testid="`sessions-activity-${activity}`"
          :aria-pressed="state.activity === activity"
          class="min-h-9 rounded-control px-3 text-xs font-semibold transition-colors"
          :class="state.activity === activity
            ? 'bg-surface-selected text-accent-strong'
            : 'bg-black/[0.025] text-ink-muted hover:bg-black/[0.05] hover:text-ink'"
          @click="emit('change', { activity })"
        >
          {{ activityLabel(activity) }}
        </button>
      </fieldset>

      <label class="grid min-w-28 gap-1 text-[11px] font-medium text-ink-subtle">
        {{ t("sessions.filters.range.label") }}
        <select
          data-testid="sessions-range"
          :value="state.range"
          class="min-h-9 rounded-control border border-line bg-white/85 px-3 text-xs text-ink"
          @change="changeRange"
        >
          <option value="all">{{ t("sessions.filters.range.all") }}</option>
          <option value="today">{{ t("sessions.filters.range.today") }}</option>
          <option value="7d">{{ t("sessions.filters.range.sevenDays") }}</option>
          <option value="30d">{{ t("sessions.filters.range.thirtyDays") }}</option>
        </select>
      </label>

      <label class="grid min-w-36 flex-1 gap-1 text-[11px] font-medium text-ink-subtle">
        {{ t("sessions.filters.project") }}
        <select
          data-testid="sessions-project"
          :value="state.projectId ?? ''"
          class="min-h-9 rounded-control border border-line bg-white/85 px-3 text-xs text-ink"
          @change="changeProject"
        >
          <option value="">{{ t("sessions.filters.allProjects") }}</option>
          <option
            v-if="state.projectId && !projectOptions.some((item) => item.value === state.projectId)"
            :value="state.projectId"
          >
            {{ t("sessions.filters.selectedProject") }}
          </option>
          <option v-for="option in projectOptions" :key="option.value" :value="option.value">
            {{ option.label }}
          </option>
        </select>
      </label>

      <label class="grid min-w-36 flex-1 gap-1 text-[11px] font-medium text-ink-subtle">
        {{ t("sessions.filters.model") }}
        <select
          data-testid="sessions-model"
          :value="state.modelKey ?? ''"
          class="min-h-9 rounded-control border border-line bg-white/85 px-3 text-xs text-ink"
          @change="changeModel"
        >
          <option value="">{{ t("sessions.filters.allModels") }}</option>
          <option
            v-if="state.modelKey && !modelOptions.some((item) => item.value === state.modelKey)"
            :value="state.modelKey"
          >
            {{ t("sessions.filters.selectedModel") }}
          </option>
          <option v-for="option in modelOptions" :key="option.value" :value="option.value">
            {{ option.label }}
          </option>
        </select>
      </label>

      <label class="grid min-w-36 gap-1 text-[11px] font-medium text-ink-subtle">
        {{ t("sessions.filters.sort.label") }}
        <select
          data-testid="sessions-sort"
          :value="state.sort"
          class="min-h-9 rounded-control border border-line bg-white/85 px-3 text-xs text-ink"
          @change="changeSort"
        >
          <option value="lastActivityAt">{{ t("sessions.filters.sort.lastActivity") }}</option>
          <option value="totalTokens">{{ t("sessions.filters.sort.totalTokens") }}</option>
          <option value="estimatedCost">{{ t("sessions.filters.sort.estimatedCost") }}</option>
        </select>
      </label>

      <button
        type="button"
        data-testid="sessions-direction"
        :aria-label="state.direction === 'desc'
          ? t('sessions.filters.sort.descending')
          : t('sessions.filters.sort.ascending')"
        class="flex size-9 items-center justify-center rounded-control border border-line bg-white/85 text-ink-muted transition-colors hover:bg-white hover:text-ink"
        @click="toggleDirection"
      >
        <ArrowDown v-if="state.direction === 'desc'" :size="16" aria-hidden="true" />
        <ArrowUp v-else :size="16" aria-hidden="true" />
      </button>
    </div>
  </form>
</template>
