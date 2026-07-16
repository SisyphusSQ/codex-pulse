<script setup lang="ts">
import { useId } from "vue";

defineProps<{
  description?: string;
  title?: string;
}>();

const titleId = `ui-card-${useId()}`;
</script>

<template>
  <section
    class="content-surface rounded-content p-6"
    :aria-labelledby="title ? titleId : undefined"
  >
    <header v-if="title || description" class="mb-5">
      <h2 v-if="title" :id="titleId" class="text-base font-semibold tracking-tight text-ink">
        {{ title }}
      </h2>
      <p v-if="description" class="mt-1 text-sm leading-6 text-ink-muted">
        {{ description }}
      </p>
    </header>
    <slot />
  </section>
</template>
