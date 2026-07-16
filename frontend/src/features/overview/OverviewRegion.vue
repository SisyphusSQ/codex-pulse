<script setup lang="ts">
import StateEmpty from "@/components/ui/StateEmpty.vue";
import StateError from "@/components/ui/StateError.vue";
import StateSkeleton from "@/components/ui/StateSkeleton.vue";
import UiCard from "@/components/ui/UiCard.vue";

withDefaults(defineProps<{
  actionLabel: string;
  bare?: boolean;
  emptyDescription: string;
  emptyTitle: string;
  errorDescription: string;
  errorTitle: string;
  hasData?: boolean;
  isEmpty?: boolean;
  isError?: boolean;
  isFetching?: boolean;
  isPartial?: boolean;
  isPending?: boolean;
  isStale?: boolean;
  loadingLabel: string;
  partialLabel: string;
  staleLabel: string;
  showStatus?: boolean;
  title: string;
}>(), {
  hasData: false,
  bare: false,
  isEmpty: false,
  isError: false,
  isFetching: false,
  isPartial: false,
  isPending: false,
  isStale: false,
  showStatus: true,
});

defineEmits<{
  retry: [];
}>();
</script>

<template>
  <section
    :aria-busy="isFetching ? 'true' : undefined"
    :aria-label="bare ? title : undefined"
  >
    <StateSkeleton
      v-if="isPending && !hasData"
      :label="loadingLabel"
      :rows="3"
    />
    <StateError
      v-else-if="isError && !hasData"
      :title="errorTitle"
      :description="errorDescription"
      :action-label="actionLabel"
      @retry="$emit('retry')"
    />
    <StateEmpty
      v-else-if="hasData && isEmpty && !isPartial && !isStale && !isError"
      :title="emptyTitle"
      :description="emptyDescription"
    />
    <div v-else-if="hasData && bare">
      <div
        v-if="showStatus && (isPartial || isStale)"
        role="status"
        class="mb-3 flex flex-wrap gap-2 text-xs font-medium"
      >
        <span v-if="isPartial" class="rounded-full bg-blue-50 px-2.5 py-1 text-blue-700">
          {{ partialLabel }}
        </span>
        <span v-if="isStale || isError" class="rounded-full bg-amber-50 px-2.5 py-1 text-amber-700">
          {{ staleLabel }}
        </span>
      </div>
      <StateEmpty
        v-if="isEmpty"
        :title="emptyTitle"
        :description="emptyDescription"
      />
      <slot v-else />
    </div>
    <UiCard v-else-if="hasData" :title="title">
      <div
        v-if="showStatus && (isPartial || isStale)"
        role="status"
        class="mb-4 flex flex-wrap gap-2 text-xs font-medium"
      >
        <span v-if="isPartial" class="rounded-full bg-blue-50 px-2.5 py-1 text-blue-700">
          {{ partialLabel }}
        </span>
        <span v-if="isStale || isError" class="rounded-full bg-amber-50 px-2.5 py-1 text-amber-700">
          {{ staleLabel }}
        </span>
      </div>
      <StateEmpty
        v-if="isEmpty"
        :title="emptyTitle"
        :description="emptyDescription"
      />
      <slot v-else />
    </UiCard>
    <StateSkeleton v-else :label="loadingLabel" :rows="3" />
  </section>
</template>
