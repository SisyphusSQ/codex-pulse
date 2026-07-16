<script setup lang="ts">
import { TriangleAlert } from "@lucide/vue";

import UiButton from "./UiButton.vue";

withDefaults(defineProps<{
  actionLabel: string;
  actionTestId?: string;
  description: string;
  title: string;
}>(), {
  actionTestId: undefined,
});

defineEmits<{
  retry: [];
}>();
</script>

<template>
  <div role="alert" class="rounded-content border border-red-200 bg-red-50/90 p-5 text-red-950">
    <div class="flex items-start gap-3">
      <TriangleAlert :size="20" aria-hidden="true" class="mt-0.5 shrink-0 text-critical" />
      <div class="min-w-0 flex-1">
        <p class="text-sm font-semibold">{{ title }}</p>
        <p class="mt-1 text-sm leading-6 text-red-900/75">{{ description }}</p>
        <UiButton
          :data-testid="actionTestId"
          variant="danger"
          class="mt-4"
          @click="$emit('retry')"
        >
          {{ actionLabel }}
        </UiButton>
      </div>
    </div>
  </div>
</template>
