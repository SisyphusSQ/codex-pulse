<script setup lang="ts">
import { computed, nextTick, ref } from "vue";
import { useI18n } from "vue-i18n";

import UiButton from "@/components/ui/UiButton.vue";
import UiCard from "@/components/ui/UiCard.vue";
import { useUpdatePanel } from "./useUpdatePanel";

defineOptions({ name: "UpdatePanel" });

const { t } = useI18n();
const panel = useUpdatePanel();
const root = ref<HTMLElement>();
const dialog = ref<HTMLElement>();
const confirmationOpen = ref(false);
const installConfirmationOpen = ref(false);
const modalOpen = computed(() => confirmationOpen.value || installConfirmationOpen.value);
const state = computed(() => panel.state.data.value);
const draining = computed(() => panel.install.isPending.value || state.value?.shutdownPhase === "draining");
const shutdownFailed = computed(() => state.value?.shutdownPhase === "closed" && Boolean(state.value.shutdownFailedStage));
const working = computed(() => panel.check.isPending.value || panel.download.isPending.value ||
	  panel.install.isPending.value || panel.cancel.isPending.value || panel.skip.isPending.value || panel.snooze.isPending.value);
const progress = computed(() => Math.max(0, Math.min(100, (state.value?.progressFraction ?? 0) * 100)));
const hasActionError = computed(() => panel.check.isError.value || panel.download.isError.value ||
	  panel.install.isError.value || panel.cancel.isError.value || panel.skip.isError.value || panel.snooze.isError.value);

async function focus(testId: string) {
  await nextTick();
  root.value?.querySelector<HTMLElement>(`[data-testid='${testId}']`)?.focus();
}

function openDownloadConfirmation() {
  confirmationOpen.value = true;
  void focus("update-download-confirm");
}

function closeDownloadConfirmation() {
  confirmationOpen.value = false;
  void focus("update-download");
}

function confirmDownload() {
  confirmationOpen.value = false;
  panel.download.mutate(undefined, { onSettled: () => void focus("update-status") });
}

function openInstallConfirmation() {
  installConfirmationOpen.value = true;
  void focus("update-install-confirm");
}

function closeInstallConfirmation() {
  installConfirmationOpen.value = false;
  void focus("update-install");
}

function confirmInstall() {
  installConfirmationOpen.value = false;
  panel.install.mutate(undefined, { onSettled: () => void focus("update-status") });
}

function keepDialogFocus(event: KeyboardEvent) {
  if (event.key !== "Tab") return;
  const controls = Array.from(dialog.value?.querySelectorAll<HTMLElement>("button:not([disabled])") ?? []);
  const first = controls[0];
  const last = controls.at(-1);
  if (first === undefined || last === undefined) return;
  if (event.shiftKey && document.activeElement === first) {
    event.preventDefault();
    last.focus();
  } else if (!event.shiftKey && document.activeElement === last) {
    event.preventDefault();
    first.focus();
  }
}

function skipCurrentVersion() {
  if (state.value?.version) panel.skip.mutate(state.value.version);
}
</script>

<template>
  <section ref="root" data-testid="update-panel">
    <div :inert="modalOpen || undefined" :aria-hidden="modalOpen || undefined">
      <UiCard :title="t('settingsPage.updates.title')" :description="t('settingsPage.updates.description')">
        <p v-if="panel.state.isPending.value && state === undefined" role="status" class="text-sm text-ink-muted">{{ t("settingsPage.updates.loading") }}</p>
        <div v-else-if="panel.state.isError.value && state === undefined" role="alert" class="space-y-3">
          <p class="text-sm text-critical">{{ t("settingsPage.updates.queryError") }}</p>
          <UiButton data-testid="update-query-retry" @click="panel.state.refetch()">{{ t("settingsPage.updates.retry") }}</UiButton>
        </div>
        <div v-else-if="state" class="space-y-4">
          <div class="flex flex-wrap items-center justify-between gap-3">
            <p data-testid="update-status" tabindex="-1" role="status" aria-live="polite" class="text-sm text-ink-muted">{{ t(`settingsPage.updates.phase.${state.readyToInstall ? 'ready' : state.phase}`) }}</p>
            <UiButton v-if="state.phase !== 'checking' && state.phase !== 'downloading' && !state.readyToInstall" data-testid="update-check" :disabled="working" :loading="panel.check.isPending.value" @click="panel.check.mutate()">{{ t("settingsPage.updates.check") }}</UiButton>
            <UiButton v-else-if="state.canCancel" data-testid="update-cancel" :disabled="working" :loading="panel.cancel.isPending.value" @click="panel.cancel.mutate()">{{ t("settingsPage.updates.cancel") }}</UiButton>
          </div>

          <div v-if="state.phase === 'downloading'" class="space-y-2">
            <div class="h-2 overflow-hidden rounded-full bg-surface-muted" role="progressbar" :aria-label="t('settingsPage.updates.downloadProgress')" aria-valuemin="0" aria-valuemax="100" :aria-valuenow="Math.round(progress)">
              <div class="h-full bg-accent transition-[width]" :style="{ width: `${progress}%` }" />
            </div>
            <p class="text-xs text-ink-muted">{{ t("settingsPage.updates.bytes", { received: state.progressReceived, total: state.progressTotal }) }}</p>
          </div>

          <div v-if="state.version" class="rounded-control border border-line p-4">
            <dl class="grid gap-3 text-sm sm:grid-cols-2">
              <div><dt class="text-xs text-ink-muted">{{ t("settingsPage.updates.currentVersion") }}</dt><dd>{{ state.currentVersion }}</dd></div>
              <div><dt class="text-xs text-ink-muted">{{ t("settingsPage.updates.version") }}</dt><dd>{{ state.displayVersion || state.version }}</dd></div>
              <div><dt class="text-xs text-ink-muted">{{ t("settingsPage.updates.architecture") }}</dt><dd>{{ state.architecture || t("settingsPage.updates.unknown") }}</dd></div>
              <div><dt class="text-xs text-ink-muted">{{ t("settingsPage.updates.size") }}</dt><dd>{{ t("settingsPage.updates.bytesOnly", { value: state.contentLength }) }}</dd></div>
              <div><dt class="text-xs text-ink-muted">{{ t("settingsPage.updates.signature") }}</dt><dd>{{ t(`settingsPage.updates.signatureStatus.${state.signatureStatus || 'skipped'}`) }}</dd></div>
            </dl>
            <p v-if="state.releaseNotes" class="mt-4 whitespace-pre-wrap text-sm">{{ state.releaseNotes }}</p>
          </div>

          <div v-if="state.promptVisible && !state.readyToInstall" class="flex flex-wrap gap-2">
            <UiButton data-testid="update-download" variant="primary" :disabled="working" @click="openDownloadConfirmation">{{ t("settingsPage.updates.download") }}</UiButton>
            <UiButton data-testid="update-snooze" :disabled="working" @click="panel.snooze.mutate(3600)">{{ t("settingsPage.updates.snooze") }}</UiButton>
            <UiButton data-testid="update-skip" :disabled="working" @click="skipCurrentVersion">{{ t("settingsPage.updates.skip") }}</UiButton>
          </div>
          <p v-else-if="state.phase === 'available' && !state.readyToInstall" class="text-sm text-ink-muted">{{ t("settingsPage.updates.suppressed") }}</p>
          <div v-if="state.readyToInstall" data-testid="update-ready" role="status" class="space-y-3 rounded-control border border-emerald-200 bg-emerald-50 p-3 text-sm text-emerald-900">
            <p v-if="draining">{{ t("settingsPage.updates.draining", { stage: state.shutdownStage || "-" }) }}</p>
            <p v-else-if="shutdownFailed" role="alert" class="text-critical">{{ t("settingsPage.updates.shutdownFailed", { stage: state.shutdownFailedStage }) }}</p>
            <p v-else>{{ t("settingsPage.updates.readyBoundary") }}</p>
            <UiButton v-if="!draining && !shutdownFailed" data-testid="update-install" variant="primary" :disabled="working" @click="openInstallConfirmation">{{ t("settingsPage.updates.install") }}</UiButton>
          </div>
          <p v-if="state.phase === 'error'" role="alert" class="text-sm text-critical">{{ t(`settingsPage.updates.fault.${state.faultCode || 'native'}`) }}</p>
          <p v-if="hasActionError" role="alert" class="text-sm text-critical">{{ t("settingsPage.updates.actionError") }}</p>
        </div>
      </UiCard>
    </div>

    <div v-if="confirmationOpen" class="fixed inset-0 z-50 grid place-items-center bg-black/30 p-4">
      <div ref="dialog" data-testid="update-download-dialog" role="dialog" aria-modal="true" :aria-label="t('settingsPage.updates.confirmTitle')" class="w-full max-w-lg rounded-content border border-line bg-white p-5 shadow-xl" @keydown.esc.prevent="closeDownloadConfirmation" @keydown="keepDialogFocus">
        <h2 class="text-lg font-semibold">{{ t("settingsPage.updates.confirmTitle") }}</h2>
        <p class="mt-2 text-sm text-ink-muted">{{ t("settingsPage.updates.confirmDescription") }}</p>
        <div class="mt-5 flex justify-end gap-2">
          <UiButton data-testid="update-download-cancel" @click="closeDownloadConfirmation">{{ t("settingsPage.updates.cancel") }}</UiButton>
          <UiButton data-testid="update-download-confirm" variant="primary" @click="confirmDownload">{{ t("settingsPage.updates.confirmDownload") }}</UiButton>
        </div>
      </div>
    </div>

    <div v-if="installConfirmationOpen" class="fixed inset-0 z-50 grid place-items-center bg-black/30 p-4">
      <div ref="dialog" data-testid="update-install-dialog" role="dialog" aria-modal="true" :aria-label="t('settingsPage.updates.installConfirmTitle')" class="w-full max-w-lg rounded-content border border-line bg-white p-5 shadow-xl" @keydown.esc.prevent="closeInstallConfirmation" @keydown="keepDialogFocus">
        <h2 class="text-lg font-semibold">{{ t("settingsPage.updates.installConfirmTitle") }}</h2>
        <p class="mt-2 text-sm text-ink-muted">{{ t("settingsPage.updates.installConfirmDescription") }}</p>
        <div class="mt-5 flex justify-end gap-2">
          <UiButton data-testid="update-install-cancel" @click="closeInstallConfirmation">{{ t("settingsPage.updates.cancel") }}</UiButton>
          <UiButton data-testid="update-install-confirm" variant="primary" @click="confirmInstall">{{ t("settingsPage.updates.installConfirm") }}</UiButton>
        </div>
      </div>
    </div>
  </section>
</template>
