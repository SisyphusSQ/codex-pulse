<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, ref, shallowRef } from "vue";
import { useI18n } from "vue-i18n";

import {
  Cancel,
  Confirm,
  Exit,
  Prepare,
  Retry,
  State,
} from "@bindings/github.com/SisyphusSQ/codex-pulse/internal/app/migrationrecoveryservice";
import type { MigrationRecoverySnapshot, MigrationRestoreConfirmation } from "@bindings/github.com/SisyphusSQ/codex-pulse/internal/app/models";

import UiButton from "@/components/ui/UiButton.vue";

const props = defineProps<{ initialSnapshot: MigrationRecoverySnapshot }>();
const { t } = useI18n();
const snapshot = ref(props.initialSnapshot);
const pending = ref<"retry" | "prepare" | "confirm" | "cancel" | "exit" | null>(null);
const actionFailed = ref(false);
const auditWarning = ref(props.initialSnapshot.auditWarning);
const confirmation = ref<MigrationRestoreConfirmation | null>(null);
const confirmationDialog = ref<HTMLElement>();
const activePromise = shallowRef<{ cancel: () => Promise<void> } | null>(null);
const cancelling = ref(false);
let disposed = false;

const restartRequired = computed(() => snapshot.value.phase === "restart_required");
const backups = computed(() => snapshot.value.backups ?? []);
const canMutate = computed(() => snapshot.value.phase === "failed");
const canCancelOperation = computed(() => pending.value === "retry" || pending.value === "prepare");

function formatSize(value: number) {
  return new Intl.NumberFormat("zh-CN", { maximumFractionDigits: 1, style: "unit", unit: "megabyte" }).format(value / 1_048_576);
}

function formatTime(value: number) {
  return new Intl.DateTimeFormat("zh-CN", { dateStyle: "medium", timeStyle: "short" }).format(new Date(value));
}

async function refresh() {
  snapshot.value = await State();
  auditWarning.value = snapshot.value.auditWarning;
}

async function refreshUntilOperationSettles() {
  for (let attempt = 0; attempt < 40; attempt += 1) {
    await refresh();
    if (snapshot.value.phase !== "running") return true;
    await new Promise((resolve) => window.setTimeout(resolve, 50));
  }
  return false;
}

async function monitorCancelledOperation() {
  while (!disposed && snapshot.value.phase === "running") {
    await new Promise((resolve) => window.setTimeout(resolve, 500));
    await refresh().catch(() => undefined);
  }
  if (!disposed && snapshot.value.phase !== "running") {
    confirmation.value = null;
    actionFailed.value = false;
  }
}

async function awaitCancellable<T>(promise: Promise<T> & { cancel: () => Promise<void> }) {
  activePromise.value = promise;
  try {
    return await promise;
  } finally {
    if (activePromise.value === promise) activePromise.value = null;
  }
}

async function cancelActiveOperation() {
  const active = activePromise.value;
  if (!active || cancelling.value) return;
  cancelling.value = true;
  actionFailed.value = false;
  try {
    await active.cancel();
  } catch {
    // The operation promise and CancelCall transport reject independently.
  }
  try {
    if (!await refreshUntilOperationSettles()) {
      actionFailed.value = true;
      void monitorCancelledOperation();
    }
  } catch {
    actionFailed.value = true;
  }
  if (snapshot.value.phase !== "running") confirmation.value = null;
  pending.value = null;
  cancelling.value = false;
}

onBeforeUnmount(() => {
  disposed = true;
  void activePromise.value?.cancel().catch(() => undefined);
});

async function runRetry() {
  pending.value = "retry";
  actionFailed.value = false;
  auditWarning.value = false;
  try {
    const receipt = await awaitCancellable(Retry());
    auditWarning.value = receipt.auditWarning;
    await refresh();
  } catch {
    if (!cancelling.value) {
      await refresh().catch(() => undefined);
      if (snapshot.value.phase === "restart_required") auditWarning.value = true;
      else actionFailed.value = true;
    }
  } finally {
    if (!cancelling.value) pending.value = null;
  }
}

async function prepareRestore(name: string) {
  pending.value = "prepare";
  actionFailed.value = false;
  try {
    confirmation.value = await awaitCancellable(Prepare(name));
    await refresh();
    pending.value = null;
    await nextTick();
    confirmationDialog.value?.querySelector<HTMLButtonElement>("[data-testid='cancel-restore']")?.focus();
  } catch {
    if (!cancelling.value) actionFailed.value = true;
  } finally {
    if (!cancelling.value) pending.value = null;
  }
}

async function confirmRestore() {
  if (!confirmation.value) return;
  pending.value = "confirm";
  actionFailed.value = false;
  auditWarning.value = false;
  try {
    const receipt = await awaitCancellable(Confirm(confirmation.value.token));
    auditWarning.value = receipt.auditWarning;
    confirmation.value = null;
    await refresh();
    pending.value = null;
    await nextTick();
    document.querySelector<HTMLButtonElement>("[data-testid='exit-recovery']")?.focus();
  } catch {
    if (!cancelling.value) {
      await refresh().catch(() => undefined);
      if (snapshot.value.phase === "restart_required") {
        auditWarning.value = true;
        confirmation.value = null;
      } else {
        confirmation.value = null;
        actionFailed.value = true;
      }
    }
  } finally {
    if (!cancelling.value) pending.value = null;
  }
}

async function cancelRestore() {
  const backupName = confirmation.value?.backup.name;
  pending.value = "cancel";
  actionFailed.value = false;
  try {
    await Cancel();
    confirmation.value = null;
    await refresh();
    pending.value = null;
    await nextTick();
    if (backupName) {
      Array.from(document.querySelectorAll<HTMLButtonElement>("[data-testid^='prepare-restore-']"))
        .find((button) => button.dataset.testid === `prepare-restore-${backupName}`)?.focus();
    }
  } catch {
    actionFailed.value = true;
  } finally {
    pending.value = null;
  }
}

async function exitRecovery() {
  pending.value = "exit";
  actionFailed.value = false;
  try {
    await Exit();
  } catch {
    actionFailed.value = true;
    pending.value = null;
  }
}

function trapDialogFocus(event: KeyboardEvent) {
  if (event.key !== "Tab" || !confirmationDialog.value) return;
  const controls = Array.from(confirmationDialog.value.querySelectorAll<HTMLElement>("button:not([disabled]), [href], [tabindex]:not([tabindex='-1'])"));
  if (controls.length === 0) return;
  const first = controls[0];
  const last = controls[controls.length - 1];
  if (event.shiftKey && document.activeElement === first) {
    event.preventDefault();
    last.focus();
  } else if (!event.shiftKey && document.activeElement === last) {
    event.preventDefault();
    first.focus();
  }
}

function handleDialogEscape() {
  if (pending.value === "confirm") cancelActiveOperation();
  else if (pending.value === null) void cancelRestore();
}
</script>

<template>
  <main data-testid="migration-recovery" class="app-shell-background min-h-screen overflow-auto px-6 py-10 text-ink">
    <section :inert="confirmation ? true : undefined" class="mx-auto max-w-3xl rounded-[28px] border border-white/70 bg-white/85 p-7 shadow-xl backdrop-blur-xl" aria-labelledby="recovery-title">
      <p class="text-xs font-semibold uppercase tracking-[0.2em] text-critical">{{ t("recovery.eyebrow") }}</p>
      <h1 id="recovery-title" class="mt-2 text-2xl font-semibold">{{ t("recovery.title") }}</h1>
      <p class="mt-3 max-w-2xl text-sm leading-6 text-ink-muted">{{ t("recovery.description") }}</p>

      <div class="mt-6 rounded-content border border-amber-200 bg-amber-50 p-5" role="status" aria-live="polite">
        <p class="font-semibold">{{ restartRequired ? t("recovery.restartTitle") : t("recovery.protectedTitle") }}</p>
        <p class="mt-1 text-sm leading-6 text-ink-muted">
          {{ restartRequired ? t("recovery.restartDescription") : t("recovery.protectedDescription") }}
        </p>
      </div>

      <dl class="mt-6 grid gap-3 sm:grid-cols-3">
        <div class="rounded-control bg-surface-base px-4 py-3">
          <dt class="text-xs text-ink-subtle">{{ t("recovery.stage") }}</dt>
          <dd class="mt-1 font-mono text-sm">{{ snapshot.stage || "—" }}</dd>
        </div>
        <div class="rounded-control bg-surface-base px-4 py-3">
          <dt class="text-xs text-ink-subtle">{{ t("recovery.code") }}</dt>
          <dd class="mt-1 font-mono text-sm">{{ snapshot.code || "—" }}</dd>
        </div>
        <div class="rounded-control bg-surface-base px-4 py-3">
          <dt class="text-xs text-ink-subtle">{{ t("recovery.version") }}</dt>
          <dd class="mt-1 font-mono text-sm">{{ snapshot.currentVersion }} → {{ snapshot.targetVersion }}</dd>
        </div>
      </dl>

      <p v-if="actionFailed" class="mt-5 rounded-control bg-red-50 px-4 py-3 text-sm text-red-900" role="alert">
        {{ t("recovery.actionFailed") }}
      </p>
      <p v-if="auditWarning" class="mt-5 rounded-control bg-amber-50 px-4 py-3 text-sm text-amber-900" role="alert">
        {{ t("recovery.auditWarning") }}
      </p>

      <div v-if="snapshot.phase === 'awaiting_confirmation' && !confirmation" class="mt-5 rounded-control border border-amber-200 bg-amber-50 px-4 py-3" role="status">
        <p class="text-sm text-ink-muted">{{ t("recovery.pendingConfirmation") }}</p>
        <UiButton data-testid="cancel-pending-restore" class="mt-3" :disabled="pending !== null" :loading="pending === 'cancel'" @click="cancelRestore">
          {{ t("recovery.cancelPending") }}
        </UiButton>
      </div>

      <section class="mt-7" aria-labelledby="backup-title">
        <h2 id="backup-title" class="text-base font-semibold">{{ t("recovery.backupsTitle") }}</h2>
        <p class="mt-1 text-sm text-ink-muted">{{ t("recovery.backupsDescription") }}</p>
        <p v-if="backups.length === 0" class="mt-4 rounded-control bg-surface-base px-4 py-3 text-sm text-ink-muted">
          {{ t("recovery.noBackups") }}
        </p>
        <ul v-else class="mt-4 space-y-3">
          <li v-for="backup in backups" :key="backup.name" class="flex flex-wrap items-center justify-between gap-3 rounded-control border border-line bg-white px-4 py-3">
            <div>
              <p class="font-mono text-sm font-semibold">{{ backup.name }}</p>
              <p class="mt-1 text-xs text-ink-subtle">{{ formatSize(backup.sizeBytes) }} · {{ formatTime(backup.modifiedAtMs) }}</p>
            </div>
            <UiButton
              :data-testid="`prepare-restore-${backup.name}`"
              :disabled="!canMutate || pending !== null"
              :loading="pending === 'prepare'"
              @click="prepareRestore(backup.name)"
            >
              {{ t("recovery.restore") }}
            </UiButton>
          </li>
        </ul>
      </section>

      <div class="mt-7 flex flex-wrap gap-3">
        <UiButton data-testid="retry-migration" variant="primary" :disabled="!snapshot.canRetry || pending !== null" :loading="pending === 'retry'" @click="runRetry">
          {{ t("recovery.retry") }}
        </UiButton>
        <UiButton data-testid="exit-recovery" variant="quiet" :disabled="pending !== null" :loading="pending === 'exit'" @click="exitRecovery">
          {{ t("recovery.exit") }}
        </UiButton>
        <UiButton v-if="canCancelOperation" data-testid="cancel-active-operation" variant="quiet" :disabled="cancelling" :loading="cancelling" @click="cancelActiveOperation">
          {{ t("recovery.cancelOperation") }}
        </UiButton>
      </div>
    </section>

    <div v-if="confirmation" class="fixed inset-0 z-50 grid place-items-center bg-black/30 p-6" role="presentation">
      <section ref="confirmationDialog" role="dialog" aria-modal="true" aria-labelledby="restore-confirm-title" aria-describedby="restore-confirm-description" class="w-full max-w-lg rounded-content bg-white p-6 shadow-2xl" @keydown="trapDialogFocus" @keydown.esc.prevent="handleDialogEscape">
        <h2 id="restore-confirm-title" class="text-lg font-semibold">{{ t("recovery.confirmTitle") }}</h2>
        <p id="restore-confirm-description" class="mt-3 text-sm leading-6 text-ink-muted">{{ t("recovery.confirmDescription", { name: confirmation.backup.name }) }}</p>
        <div class="mt-6 flex justify-end gap-3">
          <UiButton data-testid="cancel-restore" :disabled="pending !== null" :loading="pending === 'cancel'" @click="cancelRestore">{{ t("recovery.cancel") }}</UiButton>
          <UiButton v-if="pending === 'confirm'" data-testid="cancel-confirm-operation" :disabled="cancelling" :loading="cancelling" @click="cancelActiveOperation">{{ t("recovery.cancelOperation") }}</UiButton>
          <UiButton data-testid="confirm-restore" variant="danger" :disabled="pending !== null" :loading="pending === 'confirm'" @click="confirmRestore">{{ t("recovery.confirm") }}</UiButton>
        </div>
      </section>
    </div>
  </main>
</template>
