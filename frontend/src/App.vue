<script setup lang="ts">
import { Events } from "@wailsio/runtime";
import { useQuery } from "@tanstack/vue-query";
import { onBeforeUnmount } from "vue";
import { RouterView } from "vue-router";
import { useRoute, useRouter } from "vue-router";

import StateError from "@/components/ui/StateError.vue";
import StateSkeleton from "@/components/ui/StateSkeleton.vue";
import MigrationRecoveryView from "@/features/recovery/MigrationRecoveryView.vue";
import { useI18n } from "vue-i18n";
import { bootstrapQueryOptions } from "@/queries/bootstrap";
import { normalizeDesktopNavigationPath } from "@/router";

const { t } = useI18n();
const route = useRoute();
const router = useRouter();
const { data: bootstrap, isError, isPending, refetch } = useQuery(bootstrapQueryOptions());
const cleanup = Events.On("codex-pulse:navigate", (event) => {
  if (route.name === "popover") return;
  void router.push(normalizeDesktopNavigationPath(event.data?.path));
});
onBeforeUnmount(cleanup);
</script>

<template>
  <main v-if="isPending" class="app-shell-background min-h-screen p-8" data-testid="bootstrap-loading">
    <StateSkeleton :label="t('app.service.loading')" :rows="5" />
  </main>
  <main v-else-if="isError" class="app-shell-background min-h-screen p-8" data-testid="bootstrap-error">
    <StateError
      :title="t('app.service.error')"
      :description="t('app.service.errorDescription')"
      :action-label="t('app.service.retry')"
      @retry="refetch"
    />
  </main>
  <MigrationRecoveryView
    v-else-if="bootstrap?.mode === 'recovery' && bootstrap.recovery"
    :initial-snapshot="bootstrap.recovery"
  />
  <RouterView v-else />
</template>
