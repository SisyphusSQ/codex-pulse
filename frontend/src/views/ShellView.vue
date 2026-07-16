<script setup lang="ts">
import { useQuery } from "@tanstack/vue-query";
import { useI18n } from "vue-i18n";

import StateError from "@/components/ui/StateError.vue";
import StateSkeleton from "@/components/ui/StateSkeleton.vue";
import UiCard from "@/components/ui/UiCard.vue";
import { bootstrapQueryOptions } from "@/queries/bootstrap";

const { t } = useI18n();
const { data, isError, isPending, refetch } = useQuery(bootstrapQueryOptions());

function retryBinding() {
  void refetch();
}
</script>

<template>
  <section class="w-full py-1">
    <StateSkeleton
      v-if="isPending"
      data-testid="service-loading"
      :label="t('app.service.loading')"
      :rows="4"
    />

    <StateError
      v-else-if="isError"
      data-testid="service-error"
      action-test-id="retry-binding"
      :title="t('app.service.error')"
      :description="t('app.service.errorDescription')"
      :action-label="t('app.service.retry')"
      @retry="retryBinding"
    />

    <UiCard
      v-else
      data-testid="service-ready"
      :title="t('app.service.ready')"
      :description="t('shell.foundation.description')"
    >
      <dl class="grid gap-3 sm:grid-cols-2">
        <div class="rounded-control bg-surface-base px-4 py-3">
          <dt class="text-xs font-medium text-ink-subtle">{{ t("app.metadata.platform") }}</dt>
          <dd class="mt-1 font-mono text-sm text-ink">{{ data?.platform }}</dd>
        </div>
        <div class="rounded-control bg-surface-base px-4 py-3">
          <dt class="text-xs font-medium text-ink-subtle">{{ t("app.metadata.locale") }}</dt>
          <dd class="mt-1 font-mono text-sm text-ink">{{ data?.locale }}</dd>
        </div>
      </dl>
    </UiCard>
  </section>
</template>
