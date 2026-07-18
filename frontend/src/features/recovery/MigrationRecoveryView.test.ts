import { flushPromises, mount } from "@vue/test-utils";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import {
  Cancel,
  Confirm,
  Exit,
  State,
  Prepare,
  Retry,
} from "@bindings/github.com/SisyphusSQ/codex-pulse/internal/app/migrationrecoveryservice";
import { MigrationRecoveryPhase, type MigrationRecoverySnapshot } from "@bindings/github.com/SisyphusSQ/codex-pulse/internal/app/models";
import { MigrationStage } from "@bindings/github.com/SisyphusSQ/codex-pulse/internal/store/models";

import { createAppI18n } from "@/i18n";

import MigrationRecoveryView from "./MigrationRecoveryView.vue";

vi.mock("@bindings/github.com/SisyphusSQ/codex-pulse/internal/app/migrationrecoveryservice", () => ({
  Cancel: vi.fn(),
  Confirm: vi.fn(),
  Exit: vi.fn(),
  State: vi.fn(),
  Prepare: vi.fn(),
  Retry: vi.fn(),
}));

const cancelMock = vi.mocked(Cancel);
const confirmMock = vi.mocked(Confirm);
const exitMock = vi.mocked(Exit);
const stateMock = vi.mocked(State);
const prepareMock = vi.mocked(Prepare);
const retryMock = vi.mocked(Retry);

function failedSnapshot(): MigrationRecoverySnapshot {
  return {
    version: "migration-recovery-v1", phase: MigrationRecoveryPhase.MigrationRecoveryFailed,
    stage: MigrationStage.MigrationStageApply, code: "apply_failed",
    currentVersion: 13, targetVersion: 14, failedVersion: 14, canRetry: true, canExit: true,
    auditWarning: false,
    backups: [{ name: "known-good.db", sizeBytes: 2_097_152, modifiedAtMs: 1_784_100_000_000 }],
  };
}

function restartSnapshot(): MigrationRecoverySnapshot {
  return {
    ...failedSnapshot(), phase: MigrationRecoveryPhase.MigrationRecoveryRestartRequired,
    stage: MigrationStage.$zero, code: "", canRetry: false,
  };
}

function render(snapshot = failedSnapshot()) {
  return mount(MigrationRecoveryView, {
    props: { initialSnapshot: snapshot },
    global: { plugins: [createAppI18n()] },
    attachTo: document.body,
  });
}

describe("MigrationRecoveryView", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    stateMock.mockResolvedValue(failedSnapshot());
    cancelMock.mockResolvedValue(undefined);
    confirmMock.mockResolvedValue({ phase: MigrationRecoveryPhase.MigrationRecoveryRestartRequired, restartRequired: true, auditWarning: false });
    exitMock.mockResolvedValue(undefined);
    prepareMock.mockResolvedValue({
      token: "confirmation-token",
      backup: { name: "known-good.db", sizeBytes: 2_097_152, modifiedAtMs: 1_784_100_000_000 },
    });
    retryMock.mockResolvedValue({ phase: MigrationRecoveryPhase.MigrationRecoveryRestartRequired, restartRequired: true, auditWarning: false });
  });

  afterEach(() => document.body.replaceChildren());

  it("renders stable diagnosis and a real backup without exposing a normal app shell", () => {
    const wrapper = render();

    expect(wrapper.get("[data-testid='migration-recovery']").text()).toContain("只读安全模式");
    expect(wrapper.text()).toContain("apply_failed");
    expect(wrapper.text()).toContain("known-good.db");
    expect(wrapper.find("[data-testid='app-shell']").exists()).toBe(false);
  });

  it("requires explicit confirmation and supports cancelling the frozen restore intent", async () => {
    const wrapper = render();

    await wrapper.get("[data-testid='prepare-restore-known-good.db']").trigger("click");
    await flushPromises();
    expect(wrapper.get("[role='dialog']").text()).toContain("known-good.db");
    expect(prepareMock).toHaveBeenCalledWith("known-good.db");
    expect(document.activeElement).toBe(wrapper.get("[data-testid='cancel-restore']").element);

    await wrapper.get("[role='dialog']").trigger("keydown", { key: "Tab", shiftKey: true });
    expect(document.activeElement).toBe(wrapper.get("[data-testid='confirm-restore']").element);
    await wrapper.get("[role='dialog']").trigger("keydown", { key: "Tab" });
    expect(document.activeElement).toBe(wrapper.get("[data-testid='cancel-restore']").element);

    await wrapper.get("[data-testid='cancel-restore']").trigger("click");
    await flushPromises();
    expect(cancelMock).toHaveBeenCalledWith();
    expect(wrapper.find("[role='dialog']").exists()).toBe(false);
    expect(document.activeElement).toBe(wrapper.get("[data-testid='prepare-restore-known-good.db']").element);
  });

  it("allows a reloaded awaiting-confirmation page to cancel safely", async () => {
    const wrapper = render({ ...failedSnapshot(), phase: MigrationRecoveryPhase.MigrationRecoveryAwaitingConfirmation, canRetry: false });

    await wrapper.get("[data-testid='cancel-pending-restore']").trigger("click");
    await flushPromises();

    expect(cancelMock).toHaveBeenCalledWith();
    expect(stateMock).toHaveBeenCalledTimes(1);
  });

  it("shows restart-required only after retry readback confirms success", async () => {
    stateMock.mockResolvedValueOnce(restartSnapshot());
    const wrapper = render();

    await wrapper.get("[data-testid='retry-migration']").trigger("click");
    await flushPromises();

    expect(retryMock).toHaveBeenCalledTimes(1);
    expect(wrapper.text()).toContain("恢复已完成，请重新启动");
    expect(wrapper.get("[data-testid='retry-migration']").attributes("disabled")).toBeDefined();
  });

  it("cancels the active Wails operation instead of starting a second mutation", async () => {
    const cancel = vi.fn();
    retryMock.mockReturnValue(Object.assign(new Promise(() => undefined), {
      cancel,
      cancelOn() { return this; },
    }) as unknown as ReturnType<typeof Retry>);
    const wrapper = render();

    await wrapper.get("[data-testid='retry-migration']").trigger("click");
    await wrapper.vm.$nextTick();
    await wrapper.get("[data-testid='cancel-active-operation']").trigger("click");
    await new Promise((resolve) => window.setTimeout(resolve, 80));
    await flushPromises();

    expect(cancel).toHaveBeenCalledTimes(1);
    expect(retryMock).toHaveBeenCalledTimes(1);
  });

  it("cancels backup preparation through its Wails context", async () => {
    const cancel = vi.fn();
    stateMock
      .mockResolvedValueOnce({ ...failedSnapshot(), phase: MigrationRecoveryPhase.MigrationRecoveryRunning, canRetry: false })
      .mockResolvedValueOnce(failedSnapshot());
    prepareMock.mockReturnValue(Object.assign(new Promise(() => undefined), {
      cancel,
      cancelOn() { return this; },
    }) as unknown as ReturnType<typeof Prepare>);
    const wrapper = render();

    await wrapper.get("[data-testid='prepare-restore-known-good.db']").trigger("click");
    await wrapper.vm.$nextTick();
    await wrapper.get("[data-testid='cancel-active-operation']").trigger("click");
    await new Promise((resolve) => window.setTimeout(resolve, 80));
    await flushPromises();

    expect(cancel).toHaveBeenCalledTimes(1);
    expect(prepareMock).toHaveBeenCalledTimes(1);
    expect(stateMock).toHaveBeenCalledTimes(2);
    expect(wrapper.get("[data-testid='retry-migration']").attributes("disabled")).toBeUndefined();
  });

  it("keeps Confirm cancellation reachable inside the modal dialog", async () => {
    const cancel = vi.fn();
    let rejectConfirm!: (reason: Error) => void;
    const pendingConfirm = new Promise((_, reject) => { rejectConfirm = reject; });
    cancel.mockImplementation(() => rejectConfirm(new Error("cancelled")));
    confirmMock.mockReturnValue(Object.assign(pendingConfirm, {
      cancel,
      cancelOn() { return this; },
    }) as unknown as ReturnType<typeof Confirm>);
    const wrapper = render();
    await wrapper.get("[data-testid='prepare-restore-known-good.db']").trigger("click");
    await flushPromises();

    await wrapper.get("[data-testid='confirm-restore']").trigger("click");
    await wrapper.vm.$nextTick();
    await wrapper.get("[data-testid='cancel-confirm-operation']").trigger("click");

    expect(cancel).toHaveBeenCalledTimes(1);
    expect(confirmMock).toHaveBeenCalledWith("confirmation-token");
    await flushPromises();
    expect(wrapper.find("[role='dialog']").exists()).toBe(false);
    expect(wrapper.find("[role='alert']").exists()).toBe(false);
  });

  it("shows an audit warning without misreporting a completed retry as failed", async () => {
    retryMock.mockResolvedValueOnce({
      phase: MigrationRecoveryPhase.MigrationRecoveryRestartRequired,
      restartRequired: true,
      auditWarning: true,
    });
    stateMock.mockResolvedValueOnce({ ...restartSnapshot(), auditWarning: true });
    const wrapper = render();

    await wrapper.get("[data-testid='retry-migration']").trigger("click");
    await flushPromises();

    expect(wrapper.get("[role='alert']").text()).toContain("恢复已完成并需要重新启动");
    expect(wrapper.text()).not.toContain("操作未完成");
  });

  it("keeps failures content-free and leaves recovery actions visible", async () => {
    retryMock.mockRejectedValueOnce(new Error("private driver detail"));
    const wrapper = render();

    await wrapper.get("[data-testid='retry-migration']").trigger("click");
    await flushPromises();

    expect(wrapper.get("[role='alert']").text()).toContain("操作未完成");
    expect(wrapper.text()).not.toContain("private driver detail");
    expect(wrapper.get("[data-testid='exit-recovery']")).toBeDefined();
  });
});
