<script setup lang="ts">
import { ref, watch } from "vue";

import type { OverviewRangePreset } from "./range";

export type OverviewRangeSelection = OverviewRangePreset | "custom";

const props = withDefaults(defineProps<{
  applyLabel: string;
  customEndExclusive?: string;
  customLabel: string;
  customStart?: string;
  endLabel: string;
  modelValue: OverviewRangeSelection;
  presetLabels: Record<OverviewRangePreset, string>;
  startLabel: string;
}>(), {
  customEndExclusive: "",
  customStart: "",
});

const emit = defineEmits<{
  applyCustom: [value: { startDate: string; endDateExclusive: string }];
  "update:modelValue": [value: OverviewRangeSelection];
}>();

const startDate = ref(props.customStart);
const endDateExclusive = ref(props.customEndExclusive);

watch(() => props.customStart, (value) => { startDate.value = value; });
watch(() => props.customEndExclusive, (value) => { endDateExclusive.value = value; });

function select(value: OverviewRangeSelection) {
  emit("update:modelValue", value);
}

function applyCustom() {
  emit("applyCustom", {
    startDate: startDate.value,
    endDateExclusive: endDateExclusive.value,
  });
}
</script>

<template>
  <div class="flex flex-wrap items-center gap-2">
    <div role="group" class="flex rounded-control bg-black/[0.035] p-1">
      <button
        v-for="preset in (['today', '7d', '30d'] as const)"
        :key="preset"
        type="button"
        :data-range="preset"
        :aria-pressed="modelValue === preset"
        class="min-h-8 rounded-[10px] px-3 text-xs font-medium text-ink-muted transition-colors aria-pressed:bg-white aria-pressed:text-ink aria-pressed:shadow-sm"
        @click="select(preset)"
      >
        {{ presetLabels[preset] }}
      </button>
      <button
        type="button"
        data-range="custom"
        :aria-pressed="modelValue === 'custom'"
        class="min-h-8 rounded-[10px] px-3 text-xs font-medium text-ink-muted transition-colors aria-pressed:bg-white aria-pressed:text-ink aria-pressed:shadow-sm"
        @click="select('custom')"
      >
        {{ customLabel }}
      </button>
    </div>

    <div v-if="modelValue === 'custom'" class="flex flex-wrap items-center gap-2">
      <label>
        <span class="sr-only">{{ startLabel }}</span>
        <input
          v-model="startDate"
          type="date"
          :aria-label="startLabel"
          class="min-h-9 rounded-control border border-line bg-white/80 px-3 text-xs text-ink"
        />
      </label>
      <label>
        <span class="sr-only">{{ endLabel }}</span>
        <input
          v-model="endDateExclusive"
          type="date"
          :aria-label="endLabel"
          class="min-h-9 rounded-control border border-line bg-white/80 px-3 text-xs text-ink"
        />
      </label>
      <button
        type="button"
        data-testid="apply-custom-range"
        class="min-h-9 rounded-control bg-accent px-3 text-xs font-semibold text-white hover:bg-accent-strong"
        @click="applyCustom"
      >
        {{ applyLabel }}
      </button>
    </div>
  </div>
</template>
