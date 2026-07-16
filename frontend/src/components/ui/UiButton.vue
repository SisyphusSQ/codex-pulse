<script setup lang="ts">
import { LoaderCircle } from "@lucide/vue";
import { computed } from "vue";

type ButtonVariant = "primary" | "secondary" | "quiet" | "danger";

const props = withDefaults(defineProps<{
  disabled?: boolean;
  loading?: boolean;
  type?: "button" | "submit" | "reset";
  variant?: ButtonVariant;
}>(), {
  disabled: false,
  loading: false,
  type: "button",
  variant: "secondary",
});

const variantClass = computed(() => ({
  danger: "bg-critical text-white hover:bg-red-600",
  primary: "bg-accent text-white shadow-lg shadow-blue-500/15 hover:bg-accent-strong",
  quiet: "bg-transparent text-ink-muted hover:bg-black/5 hover:text-ink",
  secondary: "border border-line bg-white/80 text-ink hover:bg-white",
}[props.variant]));
</script>

<template>
  <button
    :type="type"
    :disabled="disabled || loading"
    :aria-busy="loading ? 'true' : undefined"
    class="inline-flex min-h-10 items-center justify-center gap-2 rounded-control px-4 py-2 text-sm font-semibold transition disabled:cursor-not-allowed disabled:opacity-50"
    :class="variantClass"
  >
    <LoaderCircle v-if="loading" :size="16" aria-hidden="true" class="animate-spin" />
    <slot />
  </button>
</template>
