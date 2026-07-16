<script setup lang="ts">
import { Inbox } from "@lucide/vue";

defineProps<{
  description: string;
  title: string;
}>();
</script>

<template>
  <div role="status" class="flex flex-col items-center justify-center rounded-content border border-dashed border-line bg-white/45 px-6 py-12 text-center">
    <span class="flex size-11 items-center justify-center rounded-full bg-black/[0.035] text-ink-muted">
      <Inbox :size="20" aria-hidden="true" />
    </span>
    <p class="mt-4 text-sm font-semibold text-ink">{{ title }}</p>
    <p class="mt-1 max-w-sm text-sm leading-6 text-ink-muted">{{ description }}</p>
    <div v-if="$slots.default" class="mt-5"><slot /></div>
  </div>
</template>
