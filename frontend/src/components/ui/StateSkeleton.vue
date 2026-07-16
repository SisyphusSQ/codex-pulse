<script setup lang="ts">
withDefaults(defineProps<{
  label: string;
  rows?: number;
}>(), {
  rows: 3,
});
</script>

<template>
  <div role="status" aria-busy="true" class="content-surface animate-pulse rounded-content p-6">
    <span class="sr-only">{{ label }}</span>
    <div class="h-4 w-36 rounded-full bg-slate-200" />
    <div class="mt-6 space-y-3">
      <div
        v-for="row in rows"
        :key="row"
        class="h-12 rounded-control bg-slate-100"
        :class="row === rows ? 'w-3/4' : 'w-full'"
      />
    </div>
  </div>
</template>
