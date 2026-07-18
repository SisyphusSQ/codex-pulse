<script setup lang="ts">
import { computed, nextTick, reactive, ref, watch } from "vue";
import { useI18n } from "vue-i18n";

import {
  HomeSwitchStrategy,
  type SettingsUpdateRequest,
} from "@bindings/github.com/SisyphusSQ/codex-pulse/internal/app/models";
import StateError from "@/components/ui/StateError.vue";
import StateSkeleton from "@/components/ui/StateSkeleton.vue";
import UiButton from "@/components/ui/UiButton.vue";
import UiCard from "@/components/ui/UiCard.vue";
import { useSettingsPage } from "@/features/settings/useSettingsPage";
import UpdatePanel from "@/features/updates/UpdatePanel.vue";

defineOptions({ name: "SettingsView" });

const { t } = useI18n();
const page = useSettingsPage();
const draft = reactive({
  quotaEnabled: false,
  resetCreditsEnabled: false,
  quotaIntervalSeconds: 300,
  resetCreditsIntervalSeconds: 1800,
  reconcileIntervalSeconds: 1800,
  jsonlDebounceMilliseconds: 4000,
  autoCheckEnabled: true,
  checkIntervalSeconds: 3600,
  launchBehavior: "tray",
  overviewRange: "seven_days",
});
const homePath = ref("");
const homeStrategy = ref(HomeSwitchStrategy.HomeSwitchClearAndRebuild);
const homeInput = ref<HTMLInputElement>();
const homeImpact = ref<HTMLElement>();

watch(() => page.settings.data.value?.snapshot, (snapshot) => {
  if (snapshot === undefined) return;
  Object.assign(draft, {
    ...snapshot.online,
    ...snapshot.refresh,
    autoCheckEnabled: snapshot.updates.autoCheckEnabled,
    checkIntervalSeconds: snapshot.updates.checkIntervalSeconds,
    launchBehavior: snapshot.ui.launchBehavior,
    overviewRange: snapshot.ui.overviewRange,
  });
}, { immediate: true });

const editable = computed(() => new Map(
  (page.settings.data.value?.editableFields ?? []).map((field) => [field.key, field]),
));
const needsRecovery = computed(() => page.settings.data.value?.snapshot.home.switchStatus === "recovery_required");
const settingsStale = computed(() => page.settings.data.value !== undefined && (
  page.settings.isError.value || page.settings.isStale.value ||
  page.settings.data.value.meta.status === "partial" || page.settings.data.value.meta.status === "unavailable"
));

function canEdit(key: string) {
  return editable.value.get(key)?.editable === true;
}

function minimum(key: string) {
  return editable.value.get(key)?.minimum ?? undefined;
}

function maximum(key: string) {
  return editable.value.get(key)?.maximum ?? undefined;
}

function options(key: string) {
  return editable.value.get(key)?.options ?? [];
}

function optionLabel(option: string) {
  const labels: Record<string, string> = {
    main_window: "settingsPage.option.mainWindow", tray: "settingsPage.option.tray",
    today: "settingsPage.option.today", seven_days: "settingsPage.option.sevenDays",
    thirty_days: "settingsPage.option.thirtyDays",
  };
  return t(labels[option] ?? "settingsPage.option.unknown");
}

function saveSettings() {
  const snapshot = page.settings.data.value?.snapshot;
  if (snapshot === undefined) return;
  const request: SettingsUpdateRequest = {
    expectedRevision: snapshot.revision,
    online: { quotaEnabled: draft.quotaEnabled, resetCreditsEnabled: draft.resetCreditsEnabled },
    refresh: {
      quotaIntervalSeconds: draft.quotaIntervalSeconds,
      resetCreditsIntervalSeconds: draft.resetCreditsIntervalSeconds,
      reconcileIntervalSeconds: draft.reconcileIntervalSeconds,
      jsonlDebounceMilliseconds: draft.jsonlDebounceMilliseconds,
    },
    updates: {
      autoCheckEnabled: draft.autoCheckEnabled,
      checkIntervalSeconds: draft.checkIntervalSeconds,
    },
    ui: { launchBehavior: draft.launchBehavior, overviewRange: draft.overviewRange },
  };
  page.update.mutate(request);
}

function planHomeSwitch() {
  const targetPath = homePath.value;
  if (targetPath === "") return;
  page.plan.mutate({ targetPath, strategy: homeStrategy.value }, {
    onSuccess: async () => {
      homePath.value = "";
      await nextTick();
      homeImpact.value?.focus();
    },
  });
}

function confirmHomeSwitch() {
  page.confirm.mutate(undefined, { onSettled: () => { homeInput.value?.focus(); } });
}

function recoverHomeSwitch() {
  page.recover.mutate(undefined, { onSettled: () => { homeInput.value?.focus(); } });
}
</script>

<template>
  <section data-testid="settings-view" class="w-full space-y-4 py-1">
    <StateSkeleton v-if="page.settings.isPending.value && page.settings.data.value === undefined" data-testid="settings-loading" :label="t('settingsPage.state.loading')" :rows="7" />
    <StateError v-else-if="page.settings.isError.value && page.settings.data.value === undefined" data-testid="settings-error" action-test-id="settings-retry" :title="t('settingsPage.state.errorTitle')" :description="t('settingsPage.state.errorDescription')" :action-label="t('settingsPage.state.retry')" @retry="page.settings.refetch()" />

    <template v-else-if="page.settings.data.value">
      <p v-if="settingsStale" data-testid="settings-stale" role="status" aria-live="polite" class="rounded-control border border-amber-200 bg-amber-50 p-3 text-sm text-amber-900">{{ t("settingsPage.state.lastTrusted") }}</p>
      <form class="space-y-4" @submit.prevent="saveSettings">
        <UiCard :title="t('settingsPage.preferences.title')" :description="t('settingsPage.preferences.description')">
          <div class="grid gap-4 md:grid-cols-2">
            <label class="flex items-center justify-between gap-3 rounded-control border border-line p-4"><span>{{ t("settingsPage.field.quotaEnabled") }}</span><input v-model="draft.quotaEnabled" data-testid="setting-quota-enabled" type="checkbox" :disabled="!canEdit('online.quotaEnabled')"></label>
            <label class="flex items-center justify-between gap-3 rounded-control border border-line p-4"><span>{{ t("settingsPage.field.resetCreditsEnabled") }}</span><input v-model="draft.resetCreditsEnabled" type="checkbox" :disabled="!canEdit('online.resetCreditsEnabled')"></label>
            <label class="grid gap-2 text-sm"><span>{{ t("settingsPage.field.quotaInterval") }}</span><input v-model.number="draft.quotaIntervalSeconds" type="number" :min="minimum('refresh.quotaIntervalSeconds')" :max="maximum('refresh.quotaIntervalSeconds')" :disabled="!canEdit('refresh.quotaIntervalSeconds')" class="rounded-control border border-line bg-white px-3 py-2"></label>
            <label class="grid gap-2 text-sm"><span>{{ t("settingsPage.field.resetInterval") }}</span><input v-model.number="draft.resetCreditsIntervalSeconds" type="number" :min="minimum('refresh.resetCreditsIntervalSeconds')" :max="maximum('refresh.resetCreditsIntervalSeconds')" :disabled="!canEdit('refresh.resetCreditsIntervalSeconds')" class="rounded-control border border-line bg-white px-3 py-2"></label>
            <label class="grid gap-2 text-sm"><span>{{ t("settingsPage.field.reconcileInterval") }}</span><input v-model.number="draft.reconcileIntervalSeconds" type="number" :min="minimum('refresh.reconcileIntervalSeconds')" :max="maximum('refresh.reconcileIntervalSeconds')" :disabled="!canEdit('refresh.reconcileIntervalSeconds')" class="rounded-control border border-line bg-white px-3 py-2"></label>
            <label class="grid gap-2 text-sm"><span>{{ t("settingsPage.field.debounce") }}</span><input v-model.number="draft.jsonlDebounceMilliseconds" type="number" :min="minimum('refresh.jsonlDebounceMilliseconds')" :max="maximum('refresh.jsonlDebounceMilliseconds')" :disabled="!canEdit('refresh.jsonlDebounceMilliseconds')" class="rounded-control border border-line bg-white px-3 py-2"></label>
            <label class="flex items-center justify-between gap-3 rounded-control border border-line p-4"><span>{{ t("settingsPage.field.autoCheck") }}</span><input v-model="draft.autoCheckEnabled" type="checkbox" :disabled="!canEdit('updates.autoCheckEnabled')"></label>
            <label class="grid gap-2 text-sm"><span>{{ t("settingsPage.field.checkInterval") }}</span><input v-model.number="draft.checkIntervalSeconds" type="number" :min="minimum('updates.checkIntervalSeconds')" :max="maximum('updates.checkIntervalSeconds')" :disabled="!canEdit('updates.checkIntervalSeconds')" class="rounded-control border border-line bg-white px-3 py-2"></label>
            <label class="grid gap-2 text-sm"><span>{{ t("settingsPage.field.launchBehavior") }}</span><select v-model="draft.launchBehavior" :disabled="!canEdit('ui.launchBehavior')" class="rounded-control border border-line bg-white px-3 py-2"><option v-for="option in options('ui.launchBehavior')" :key="option" :value="option">{{ optionLabel(option) }}</option></select></label>
            <label class="grid gap-2 text-sm"><span>{{ t("settingsPage.field.overviewRange") }}</span><select v-model="draft.overviewRange" :disabled="!canEdit('ui.overviewRange')" class="rounded-control border border-line bg-white px-3 py-2"><option v-for="option in options('ui.overviewRange')" :key="option" :value="option">{{ optionLabel(option) }}</option></select></label>
          </div>
          <div class="mt-5 flex items-center gap-3"><UiButton data-testid="settings-save" type="submit" variant="primary" :loading="page.update.isPending.value">{{ t("settingsPage.action.save") }}</UiButton><span v-if="page.update.data.value" data-testid="settings-save-result" role="status" aria-live="polite" class="text-sm text-ink-muted">{{ page.update.data.value.result === 'applied_reconcile_required' ? t("settingsPage.state.reconcileRequired") : t("settingsPage.state.saved") }}</span></div>
          <p v-if="page.update.isError.value" role="alert" class="mt-3 text-sm text-critical">{{ t("settingsPage.state.saveError") }}</p>
        </UiCard>
      </form>

      <UiCard :title="t('settingsPage.readOnly.title')" :description="t('settingsPage.readOnly.description')">
        <dl class="grid gap-3 sm:grid-cols-3"><div><dt class="text-xs text-ink-muted">{{ t("settingsPage.field.locale") }}</dt><dd>{{ page.settings.data.value.snapshot.ui.locale }}</dd></div><div><dt class="text-xs text-ink-muted">{{ t("settingsPage.field.channel") }}</dt><dd>{{ page.settings.data.value.snapshot.updates.channel }}</dd></div><div><dt class="text-xs text-ink-muted">{{ t("settingsPage.field.homeGeneration") }}</dt><dd>{{ page.settings.data.value.snapshot.home.generation }}</dd></div></dl>
      </UiCard>

      <UpdatePanel />

      <UiCard :title="t('settingsPage.home.title')" :description="t('settingsPage.home.description')">
        <div class="grid gap-3 md:grid-cols-[1fr_16rem_auto]"><input ref="homeInput" v-model="homePath" data-testid="home-target-path" type="text" autocomplete="off" :aria-label="t('settingsPage.home.targetLabel')" :disabled="page.confirm.isPending.value || page.recover.isPending.value" :placeholder="t('settingsPage.home.placeholder')" class="rounded-control border border-line bg-white px-3 py-2"><select v-model="homeStrategy" data-testid="home-strategy" :aria-label="t('settingsPage.home.strategyLabel')" :disabled="page.confirm.isPending.value || page.recover.isPending.value" class="rounded-control border border-line bg-white px-3 py-2"><option :value="HomeSwitchStrategy.HomeSwitchClearAndRebuild">{{ t("settingsPage.home.clear") }}</option><option :value="HomeSwitchStrategy.HomeSwitchIndependentDatabase">{{ t("settingsPage.home.independent") }}</option></select><UiButton data-testid="home-plan" :disabled="page.confirm.isPending.value || page.recover.isPending.value" :loading="page.plan.isPending.value" @click="planHomeSwitch">{{ t("settingsPage.home.plan") }}</UiButton></div>
        <div v-if="page.plan.data.value" ref="homeImpact" tabindex="-1" data-testid="home-impact" class="mt-4 rounded-control border border-amber-200 bg-amber-50 p-4 text-sm"><p>{{ page.plan.data.value.clearsDerivedFacts ? t("settingsPage.home.impactClear") : t("settingsPage.home.impactPreserve") }}</p><UiButton data-testid="home-confirm" variant="danger" class="mt-3" :loading="page.confirm.isPending.value" :disabled="page.recover.isPending.value" @click="confirmHomeSwitch">{{ t("settingsPage.home.confirm") }}</UiButton></div>
        <UiButton v-if="needsRecovery" data-testid="home-recover" class="mt-4" :loading="page.recover.isPending.value" :disabled="page.confirm.isPending.value || page.plan.isPending.value" @click="recoverHomeSwitch">{{ t("settingsPage.home.recover") }}</UiButton>
        <p v-if="page.confirm.data.value || page.recover.data.value" data-testid="home-operation-result" role="status" aria-live="polite" class="mt-3 text-sm text-ink-muted">{{ t("settingsPage.home.completed") }}</p>
        <p v-if="page.plan.isError.value || page.confirm.isError.value || page.recover.isError.value" role="alert" class="mt-3 text-sm text-critical">{{ t("settingsPage.home.error") }}</p>
      </UiCard>
    </template>
  </section>
</template>
