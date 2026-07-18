package updater

import (
	"context"
	"errors"
	"math"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/SisyphusSQ/codex-pulse/internal/preferences"
)

func TestCoordinatorAutomaticDueAndManualPolicy(t *testing.T) {
	t.Parallel()

	now := time.UnixMilli(10_000_000)
	store := &fakeUpdatePreferenceStore{snapshot: updatePreferenceSnapshot(true, now.Add(-time.Hour).UnixMilli())}
	controller := &fakeUpdateController{snapshot: Snapshot{Phase: PhaseIdle}}
	coordinator := mustCoordinator(t, CoordinatorConfig{Store: store, Controller: controller, Clock: func() time.Time { return now }})
	coordinator.ObserveSnapshot(controller.snapshot)

	receipt, err := coordinator.Trigger(t.Context(), TriggerScheduled)
	if err != nil || !receipt.Accepted || receipt.Reason != TriggerReasonDue {
		t.Fatalf("scheduled receipt=%#v err=%v, want due accepted", receipt, err)
	}
	if controller.checkCalls != 1 || store.snapshot.Updates.LastCheckAtMS == nil || *store.snapshot.Updates.LastCheckAtMS != now.UnixMilli() {
		t.Fatalf("checkCalls=%d last=%v", controller.checkCalls, store.snapshot.Updates.LastCheckAtMS)
	}

	controller.snapshot = Snapshot{Phase: PhaseIdle}
	receipt, err = coordinator.Trigger(t.Context(), TriggerWake)
	if err != nil || receipt.Accepted || receipt.Reason != TriggerReasonNotDue || controller.checkCalls != 1 {
		t.Fatalf("wake receipt=%#v calls=%d err=%v, want not due", receipt, controller.checkCalls, err)
	}

	store.snapshot.Updates.AutoCheckEnabled = false
	receipt, err = coordinator.Trigger(t.Context(), TriggerScheduled)
	if err != nil || receipt.Accepted || receipt.Reason != TriggerReasonDisabled {
		t.Fatalf("disabled scheduled receipt=%#v err=%v", receipt, err)
	}
	controller.snapshot = Snapshot{Phase: PhaseIdle}
	now = now.Add(time.Minute)
	receipt, err = coordinator.Trigger(t.Context(), TriggerManual)
	if err != nil || !receipt.Accepted || receipt.Reason != TriggerReasonManual || controller.checkCalls != 2 {
		t.Fatalf("manual receipt=%#v calls=%d err=%v", receipt, controller.checkCalls, err)
	}
}

func TestCoordinatorMergesBusyTriggers(t *testing.T) {
	t.Parallel()

	store := &fakeUpdatePreferenceStore{snapshot: updatePreferenceSnapshot(true, 0)}
	controller := &fakeUpdateController{snapshot: Snapshot{Phase: PhaseChecking, CanCancel: true}}
	coordinator := mustCoordinator(t, CoordinatorConfig{Store: store, Controller: controller})

	receipt, err := coordinator.Trigger(t.Context(), TriggerManual)
	if err != nil || receipt.Accepted || receipt.Reason != TriggerReasonBusy || controller.checkCalls != 0 || store.compareCalls != 0 {
		t.Fatalf("busy receipt=%#v checks=%d compares=%d err=%v", receipt, controller.checkCalls, store.compareCalls, err)
	}
}

func TestCoordinatorSuspendWaitsForAdmittedDownloadAndRejectsNewActions(t *testing.T) {
	t.Parallel()

	controller := &blockingDownloadController{started: make(chan struct{}), release: make(chan struct{})}
	store := &fakeUpdatePreferenceStore{snapshot: updatePreferenceSnapshot(true, 0)}
	coordinator := mustCoordinator(t, CoordinatorConfig{Store: store, Controller: controller})
	downloadDone := make(chan error, 1)
	go func() { downloadDone <- coordinator.Download(context.Background()) }()
	<-controller.started
	suspendDone := make(chan error, 1)
	go func() { suspendDone <- coordinator.Suspend() }()
	select {
	case err := <-suspendDone:
		t.Fatalf("Suspend returned before admitted download: %v", err)
	case <-time.After(25 * time.Millisecond):
	}
	close(controller.release)
	if err := <-downloadDone; err != nil {
		t.Fatalf("Download=%v", err)
	}
	if err := <-suspendDone; err != nil {
		t.Fatalf("Suspend=%v", err)
	}
	if err := coordinator.Cancel(t.Context()); !errors.Is(err, ErrCoordinatorClosed) {
		t.Fatalf("Cancel after suspend=%v", err)
	}
}

func TestCoordinatorAllowsRecheckWhenUpdateIsAvailable(t *testing.T) {
	t.Parallel()

	store := &fakeUpdatePreferenceStore{snapshot: updatePreferenceSnapshot(true, 0)}
	controller := &fakeUpdateController{snapshot: Snapshot{Phase: PhaseAvailable, Update: &Update{Version: "42"}}}
	coordinator := mustCoordinator(t, CoordinatorConfig{Store: store, Controller: controller})

	receipt, err := coordinator.Trigger(t.Context(), TriggerManual)
	if err != nil || !receipt.Accepted || receipt.Reason != TriggerReasonManual || controller.checkCalls != 1 {
		t.Fatalf("available recheck receipt=%#v calls=%d err=%v", receipt, controller.checkCalls, err)
	}
}

func TestCoordinatorRejectsUnavailableControllerWithoutPersistingCheck(t *testing.T) {
	t.Parallel()

	store := &fakeUpdatePreferenceStore{snapshot: updatePreferenceSnapshot(true, 0)}
	controller := &fakeUpdateController{snapshot: Snapshot{Phase: PhaseError, Fault: &Fault{Code: FaultConfiguration, Message: "missing feed"}}}
	coordinator := mustCoordinator(t, CoordinatorConfig{Store: store, Controller: controller})

	receipt, err := coordinator.Trigger(t.Context(), TriggerManual)
	if err != nil || receipt.Accepted || receipt.Reason != TriggerReasonUnavailable {
		t.Fatalf("unavailable receipt=%#v err=%v", receipt, err)
	}
	if controller.checkCalls != 0 || store.compareCalls != 0 || store.snapshot.Updates.LastCheckAtMS != nil {
		t.Fatalf("checks=%d compares=%d last=%v, want no persisted attempt", controller.checkCalls, store.compareCalls, store.snapshot.Updates.LastCheckAtMS)
	}
}

func TestCoordinatorBacksOffTransientAutomaticFailuresButAllowsManualRetry(t *testing.T) {
	t.Parallel()

	now := time.UnixMilli(30_000_000)
	lastCheck := now.Add(-time.Hour).UnixMilli()
	store := &fakeUpdatePreferenceStore{snapshot: updatePreferenceSnapshot(true, lastCheck)}
	controller := &fakeUpdateController{snapshot: Snapshot{
		Phase: PhaseError, Fault: &Fault{Code: FaultCheck, Message: "offline"},
	}}
	coordinator := mustCoordinator(t, CoordinatorConfig{Store: store, Controller: controller, Clock: func() time.Time { return now }})

	receipt, err := coordinator.Trigger(t.Context(), TriggerScheduled)
	if err != nil || receipt.Accepted || receipt.Reason != TriggerReasonBackoff || controller.checkCalls != 0 {
		t.Fatalf("backoff receipt=%#v calls=%d err=%v", receipt, controller.checkCalls, err)
	}
	receipt, err = coordinator.Trigger(t.Context(), TriggerManual)
	if err != nil || !receipt.Accepted || receipt.Reason != TriggerReasonManual || controller.checkCalls != 1 {
		t.Fatalf("manual retry receipt=%#v calls=%d err=%v", receipt, controller.checkCalls, err)
	}
}

func TestCoordinatorRejectsPreferenceRevisionOverflow(t *testing.T) {
	t.Parallel()

	store := &fakeUpdatePreferenceStore{snapshot: updatePreferenceSnapshot(true, 0)}
	store.snapshot.Revision = math.MaxUint64
	controller := &fakeUpdateController{snapshot: Snapshot{Phase: PhaseIdle}}
	coordinator := mustCoordinator(t, CoordinatorConfig{Store: store, Controller: controller})

	if _, err := coordinator.Trigger(t.Context(), TriggerManual); !errors.Is(err, preferences.ErrInvalidPreferences) {
		t.Fatalf("Trigger error=%v, want ErrInvalidPreferences", err)
	}
	if controller.checkCalls != 0 || store.compareCalls != 0 {
		t.Fatalf("checks=%d compares=%d, want zero", controller.checkCalls, store.compareCalls)
	}
}

func TestCoordinatorExponentiallyBacksOffAndResetsAfterSuccess(t *testing.T) {
	now := time.UnixMilli(40_000_000)
	lastCheck := now.Add(-3 * time.Hour).UnixMilli()
	store := &fakeUpdatePreferenceStore{snapshot: updatePreferenceSnapshot(true, lastCheck)}
	controller := &fakeUpdateController{snapshot: Snapshot{Phase: PhaseError, Fault: &Fault{Code: FaultCheck, Message: "offline"}}}
	coordinator := mustCoordinator(t, CoordinatorConfig{Store: store, Controller: controller, Clock: func() time.Time { return now }})
	coordinator.ObserveSnapshot(Snapshot{Phase: PhaseChecking})
	coordinator.ObserveSnapshot(controller.snapshot)
	coordinator.ObserveSnapshot(Snapshot{Phase: PhaseChecking})
	coordinator.ObserveSnapshot(controller.snapshot)
	receipt, err := coordinator.Trigger(t.Context(), TriggerScheduled)
	if err != nil || receipt.Accepted || receipt.Reason != TriggerReasonBackoff {
		t.Fatalf("second failure receipt=%#v err=%v, want four-hour backoff", receipt, err)
	}
	coordinator.ObserveSnapshot(Snapshot{Phase: PhaseIdle})
	receipt, err = coordinator.Trigger(t.Context(), TriggerScheduled)
	if err != nil || !receipt.Accepted {
		t.Fatalf("reset receipt=%#v err=%v, want base interval due", receipt, err)
	}
}

func TestCoordinatorSkipSnoozeAndExplicitDownload(t *testing.T) {
	t.Parallel()

	now := time.UnixMilli(20_000_000)
	store := &fakeUpdatePreferenceStore{snapshot: updatePreferenceSnapshot(true, 0)}
	controller := &fakeUpdateController{snapshot: Snapshot{Phase: PhaseAvailable, Update: &Update{
		Version: "42", DisplayVersion: "0.2.0", Architecture: "arm64",
	}}}
	coordinator := mustCoordinator(t, CoordinatorConfig{Store: store, Controller: controller, Clock: func() time.Time { return now }})

	view, err := coordinator.View(t.Context())
	if err != nil || !view.PromptVisible || view.Snapshot.Update == nil {
		t.Fatalf("initial view=%#v err=%v", view, err)
	}
	if err := coordinator.Download(t.Context()); err != nil || controller.downloadCalls != 1 {
		t.Fatalf("Download calls=%d err=%v", controller.downloadCalls, err)
	}
	controller.snapshot.Phase = PhaseAvailable

	if err := coordinator.Snooze(t.Context(), time.Hour); err != nil {
		t.Fatalf("Snooze: %v", err)
	}
	if len(controller.chooseCalls) != 1 || controller.chooseCalls[0] != UpdateChoiceDismiss {
		t.Fatalf("snooze choices=%v, want dismiss", controller.chooseCalls)
	}
	view, err = coordinator.View(t.Context())
	if err != nil || view.PromptVisible || view.SnoozeUntilMS == nil || *view.SnoozeUntilMS != now.Add(time.Hour).UnixMilli() {
		t.Fatalf("snoozed view=%#v err=%v", view, err)
	}
	if err := coordinator.Download(t.Context()); !errors.Is(err, ErrPromptSuppressed) {
		t.Fatalf("Download while snoozed error=%v, want ErrPromptSuppressed", err)
	}

	now = now.Add(2 * time.Hour)
	controller.snapshot = Snapshot{Phase: PhaseAvailable, Update: &Update{Version: "42", Architecture: "arm64"}}
	if err := coordinator.Skip(t.Context(), "42"); err != nil {
		t.Fatalf("Skip: %v", err)
	}
	if len(controller.chooseCalls) != 2 || controller.chooseCalls[1] != UpdateChoiceSkip {
		t.Fatalf("skip choices=%v, want dismiss then skip", controller.chooseCalls)
	}
	view, err = coordinator.View(t.Context())
	if err != nil || view.PromptVisible || view.SkippedVersion == nil || *view.SkippedVersion != "42" {
		t.Fatalf("skipped view=%#v err=%v", view, err)
	}
}

func TestCoordinatorChoiceFailureDoesNotSuppressPrompt(t *testing.T) {
	store := &fakeUpdatePreferenceStore{snapshot: updatePreferenceSnapshot(true, 0)}
	controller := &fakeUpdateController{snapshot: Snapshot{Phase: PhaseAvailable, Update: &Update{Version: "42", Architecture: "arm64"}}, chooseErr: errors.New("native choice failed")}
	coordinator := mustCoordinator(t, CoordinatorConfig{Store: store, Controller: controller})

	if err := coordinator.Skip(t.Context(), "42"); err == nil {
		t.Fatal("Skip error=nil, want native choice failure")
	}
	if store.snapshot.Updates.SkippedVersion == nil || *store.snapshot.Updates.SkippedVersion != "42" {
		t.Fatalf("updates=%#v, want durable skip intent", store.snapshot.Updates)
	}
	controller.chooseErr = nil
	view, err := coordinator.View(t.Context())
	if err != nil || view.PromptVisible || view.SkippedVersion == nil || *view.SkippedVersion != "42" ||
		store.compareCalls != 1 || len(controller.chooseCalls) != 2 {
		t.Fatalf("view=%#v updates=%#v compares=%d choices=%v err=%v, want reconciled durable intent",
			view, store.snapshot.Updates, store.compareCalls, controller.chooseCalls, err)
	}
}

func TestCoordinatorPreferenceFailureDoesNotConsumeNativeChoice(t *testing.T) {
	tests := []struct {
		name  string
		store *fakeUpdatePreferenceStore
		act   func(*Coordinator) error
	}{
		{
			name:  "skip load",
			store: &fakeUpdatePreferenceStore{snapshot: updatePreferenceSnapshot(true, 0), loadErr: errors.New("load failed")},
			act:   func(coordinator *Coordinator) error { return coordinator.Skip(t.Context(), "42") },
		},
		{
			name:  "skip compare",
			store: &fakeUpdatePreferenceStore{snapshot: updatePreferenceSnapshot(true, 0), compareErr: errors.New("compare failed")},
			act:   func(coordinator *Coordinator) error { return coordinator.Skip(t.Context(), "42") },
		},
		{
			name:  "snooze load",
			store: &fakeUpdatePreferenceStore{snapshot: updatePreferenceSnapshot(true, 0), loadErr: errors.New("load failed")},
			act:   func(coordinator *Coordinator) error { return coordinator.Snooze(t.Context(), time.Hour) },
		},
		{
			name:  "snooze compare",
			store: &fakeUpdatePreferenceStore{snapshot: updatePreferenceSnapshot(true, 0), compareErr: errors.New("compare failed")},
			act:   func(coordinator *Coordinator) error { return coordinator.Snooze(t.Context(), time.Hour) },
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			controller := &fakeUpdateController{snapshot: Snapshot{Phase: PhaseAvailable, Update: &Update{Version: "42", Architecture: "arm64"}}}
			coordinator := mustCoordinator(t, CoordinatorConfig{Store: test.store, Controller: controller})
			if err := test.act(coordinator); err == nil {
				t.Fatal("action error=nil, want preference failure")
			}
			if len(controller.chooseCalls) != 0 || controller.snapshot.Update == nil {
				t.Fatalf("choices=%v snapshot=%#v, native update must remain retryable", controller.chooseCalls, controller.snapshot)
			}
		})
	}
}

func TestCoordinatorSnoozeChoiceFailureRetainsIntentAndPreservesSkippedVersion(t *testing.T) {
	store := &fakeUpdatePreferenceStore{snapshot: updatePreferenceSnapshot(true, 0)}
	previousSkip := "41"
	store.snapshot.Updates.SkippedVersion = &previousSkip
	controller := &fakeUpdateController{
		snapshot:  Snapshot{Phase: PhaseAvailable, Update: &Update{Version: "42", Architecture: "arm64"}},
		chooseErr: errors.New("native choice failed"),
	}
	coordinator := mustCoordinator(t, CoordinatorConfig{Store: store, Controller: controller})
	if err := coordinator.Snooze(t.Context(), time.Hour); err == nil {
		t.Fatal("Snooze error=nil, want native choice failure")
	}
	if store.snapshot.Updates.SnoozeUntilMS == nil || store.snapshot.Updates.SkippedVersion == nil ||
		*store.snapshot.Updates.SkippedVersion != previousSkip ||
		store.compareCalls != 1 {
		t.Fatalf("updates=%#v compares=%d, want durable snooze intent preserving skipped version", store.snapshot.Updates, store.compareCalls)
	}
	controller.chooseErr = nil
	view, err := coordinator.View(t.Context())
	if err != nil || len(controller.chooseCalls) != 2 || controller.chooseCalls[1] != UpdateChoiceDismiss ||
		view.SnoozeUntilMS == nil || view.SkippedVersion == nil || *view.SkippedVersion != previousSkip {
		t.Fatalf("view=%#v choices=%v err=%v, want snooze reconcile preserving skipped version", view, controller.chooseCalls, err)
	}
}

func TestCoordinatorReconcilesCommittedDurabilityUnknown(t *testing.T) {
	store := &fakeUpdatePreferenceStore{
		snapshot:     updatePreferenceSnapshot(true, 0),
		compareSteps: []fakePreferenceCompareStep{{publish: true, err: preferences.ErrDurabilityUnknown}},
	}
	controller := &fakeUpdateController{snapshot: Snapshot{Phase: PhaseAvailable, Update: &Update{Version: "42", Architecture: "arm64"}}}
	coordinator := mustCoordinator(t, CoordinatorConfig{Store: store, Controller: controller})
	if err := coordinator.Skip(t.Context(), "42"); err != nil {
		t.Fatalf("Skip committed durability unknown: %v", err)
	}
	if len(controller.chooseCalls) != 1 || store.compareCalls != 1 {
		t.Fatalf("choices=%v updates=%#v compares=%d", controller.chooseCalls, store.snapshot.Updates, store.compareCalls)
	}
}

func TestCoordinatorRecoversCommittedUnknownAfterReadbackFailure(t *testing.T) {
	store := &fakeUpdatePreferenceStore{
		snapshot:     updatePreferenceSnapshot(true, 0),
		loadSteps:    []fakePreferenceLoadStep{{}, {err: errors.New("readback failed")}},
		compareSteps: []fakePreferenceCompareStep{{publish: true, err: preferences.ErrDurabilityUnknown}},
	}
	controller := &fakeUpdateController{snapshot: Snapshot{Phase: PhaseAvailable, Update: &Update{Version: "42", Architecture: "arm64"}}}
	coordinator := mustCoordinator(t, CoordinatorConfig{Store: store, Controller: controller})
	if err := coordinator.Skip(t.Context(), "42"); err == nil {
		t.Fatal("Skip error=nil, want unconfirmed durability error")
	}
	if len(controller.chooseCalls) != 0 || store.snapshot.Updates.SkippedVersion == nil {
		t.Fatalf("choices=%v updates=%#v, persisted intent must remain recoverable", controller.chooseCalls, store.snapshot.Updates)
	}
	view, err := coordinator.View(t.Context())
	if err != nil || len(controller.chooseCalls) != 1 || view.SkippedVersion == nil || *view.SkippedVersion != "42" {
		t.Fatalf("view=%#v choices=%v err=%v, want later readback reconcile", view, controller.chooseCalls, err)
	}
}

func TestCoordinatorPersistsIntentBeforeNativeChoice(t *testing.T) {
	store := &fakeUpdatePreferenceStore{snapshot: updatePreferenceSnapshot(true, 0)}
	observed := false
	controller := &fakeUpdateController{
		snapshot: Snapshot{Phase: PhaseAvailable, Update: &Update{Version: "42", Architecture: "arm64"}},
		chooseHook: func() {
			store.mu.Lock()
			defer store.mu.Unlock()
			observed = store.snapshot.Updates.SkippedVersion != nil && *store.snapshot.Updates.SkippedVersion == "42"
		},
	}
	coordinator := mustCoordinator(t, CoordinatorConfig{Store: store, Controller: controller})
	if err := coordinator.Skip(t.Context(), "42"); err != nil || !observed {
		t.Fatalf("Skip err=%v observedDurableIntent=%t", err, observed)
	}
}

func TestCoordinatorIntentSurvivesRequestCancellationDuringNativeChoice(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	store := &fakeUpdatePreferenceStore{snapshot: updatePreferenceSnapshot(true, 0)}
	controller := &fakeUpdateController{
		snapshot:   Snapshot{Phase: PhaseAvailable, Update: &Update{Version: "42", Architecture: "arm64"}},
		chooseHook: cancel,
	}
	coordinator := mustCoordinator(t, CoordinatorConfig{Store: store, Controller: controller})
	if err := coordinator.Skip(ctx, "42"); err != nil {
		t.Fatalf("Skip cleanup after cancellation: %v", err)
	}
	if store.snapshot.Updates.SkippedVersion == nil || *store.snapshot.Updates.SkippedVersion != "42" || store.compareCalls != 1 {
		t.Fatalf("updates=%#v compares=%d, durable intent must survive cancellation", store.snapshot.Updates, store.compareCalls)
	}
}

func TestCoordinatorRestartReconcilesPendingNativeChoice(t *testing.T) {
	store := &fakeUpdatePreferenceStore{snapshot: updatePreferenceSnapshot(true, 0)}
	firstController := &fakeUpdateController{
		snapshot:  Snapshot{Phase: PhaseAvailable, Update: &Update{Version: "42", Architecture: "arm64"}},
		chooseErr: errors.New("native choice failed"),
	}
	first := mustCoordinator(t, CoordinatorConfig{Store: store, Controller: firstController})
	if err := first.Skip(t.Context(), "42"); err == nil {
		t.Fatal("first Skip error=nil")
	}
	secondController := &fakeUpdateController{snapshot: Snapshot{Phase: PhaseAvailable, Update: &Update{Version: "42", Architecture: "arm64"}}}
	second := mustCoordinator(t, CoordinatorConfig{Store: store, Controller: secondController})
	view, err := second.View(t.Context())
	if err != nil || len(secondController.chooseCalls) != 1 || secondController.chooseCalls[0] != UpdateChoiceSkip ||
		view.SkippedVersion == nil || *view.SkippedVersion != "42" {
		t.Fatalf("view=%#v choices=%v updates=%#v err=%v", view, secondController.chooseCalls, store.snapshot.Updates, err)
	}
}

func TestCoordinatorAvailableObserverReconcilesSuppressedIntent(t *testing.T) {
	store := &fakeUpdatePreferenceStore{snapshot: updatePreferenceSnapshot(true, 0)}
	skipped := "42"
	store.snapshot.Updates.SkippedVersion = &skipped
	chosen := make(chan struct{})
	controller := &fakeUpdateController{
		snapshot: Snapshot{Phase: PhaseAvailable, Update: &Update{Version: skipped, Architecture: "arm64"}},
		chooseHook: func() {
			select {
			case <-chosen:
			default:
				close(chosen)
			}
		},
	}
	coordinator := mustCoordinator(t, CoordinatorConfig{Store: store, Controller: controller})
	coordinator.ObserveSnapshot(controller.snapshot)
	select {
	case <-chosen:
	case <-time.After(time.Second):
		t.Fatal("available observer did not reconcile pending choice")
	}
}

func TestCoordinatorRunUsesCronAndCloseDrains(t *testing.T) {
	t.Parallel()

	store := &fakeUpdatePreferenceStore{snapshot: updatePreferenceSnapshot(false, 0)}
	controller := &fakeUpdateController{snapshot: Snapshot{Phase: PhaseIdle}}
	runner := &fakeUpdateCronRunner{stopped: make(chan struct{})}
	coordinator := mustCoordinator(t, CoordinatorConfig{
		Store: store, Controller: controller,
		NewCronRunner: func(job func()) (UpdateCronRunner, error) {
			runner.job = job
			return runner, nil
		},
	})
	if err := coordinator.Start(t.Context()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if runner.startCalls != 1 || runner.job == nil {
		t.Fatalf("runner start=%d hasJob=%t", runner.startCalls, runner.job != nil)
	}
	runner.job()
	if controller.checkCalls != 0 {
		t.Fatalf("disabled cron checks=%d", controller.checkCalls)
	}
	close(runner.stopped)
	if err := coordinator.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if runner.stopCalls != 1 || controller.closeCalls != 1 {
		t.Fatalf("stop=%d close=%d", runner.stopCalls, controller.closeCalls)
	}
	if err := coordinator.Close(); err != nil || runner.stopCalls != 1 || controller.closeCalls != 1 {
		t.Fatalf("second Close err=%v stop=%d close=%d", err, runner.stopCalls, controller.closeCalls)
	}
}

func TestCoordinatorCloseWaitsForInflightTrigger(t *testing.T) {
	t.Parallel()

	store := &fakeUpdatePreferenceStore{snapshot: updatePreferenceSnapshot(false, 0)}
	controller := &blockingUpdateController{started: make(chan struct{}), release: make(chan struct{})}
	coordinator := mustCoordinator(t, CoordinatorConfig{Store: store, Controller: controller})
	triggerDone := make(chan error, 1)
	go func() {
		_, err := coordinator.Trigger(context.Background(), TriggerManual)
		triggerDone <- err
	}()
	<-controller.started
	closeDone := make(chan error, 1)
	go func() { closeDone <- coordinator.Close() }()
	select {
	case err := <-closeDone:
		t.Fatalf("Close returned before trigger drained: %v", err)
	case <-time.After(25 * time.Millisecond):
	}
	close(controller.release)
	if err := <-triggerDone; err != nil {
		t.Fatalf("Trigger: %v", err)
	}
	if err := <-closeDone; err != nil {
		t.Fatalf("Close: %v", err)
	}
	if controller.closeCalls.Load() != 1 {
		t.Fatalf("close calls=%d, want 1", controller.closeCalls.Load())
	}
}

func mustCoordinator(t *testing.T, config CoordinatorConfig) *Coordinator {
	t.Helper()
	coordinator, err := NewCoordinator(config)
	if err != nil {
		t.Fatalf("NewCoordinator: %v", err)
	}
	return coordinator
}

func updatePreferenceSnapshot(enabled bool, lastCheck int64) preferences.Snapshot {
	snapshot := preferences.Snapshot{Revision: 1, Updates: preferences.DefaultUpdatePreferences()}
	snapshot.Updates.AutoCheckEnabled = enabled
	if lastCheck > 0 {
		snapshot.Updates.LastCheckAtMS = &lastCheck
	}
	return snapshot
}

type fakeUpdatePreferenceStore struct {
	mu           sync.Mutex
	snapshot     preferences.Snapshot
	compareCalls int
	loadErr      error
	compareErr   error
	loadSteps    []fakePreferenceLoadStep
	compareSteps []fakePreferenceCompareStep
}

type fakePreferenceLoadStep struct {
	err error
}

type fakePreferenceCompareStep struct {
	publish bool
	err     error
}

func (store *fakeUpdatePreferenceStore) LoadPreferences(context.Context) (preferences.Snapshot, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	if len(store.loadSteps) > 0 {
		step := store.loadSteps[0]
		store.loadSteps = store.loadSteps[1:]
		if step.err != nil {
			return preferences.Snapshot{}, step.err
		}
	}
	if store.loadErr != nil {
		return preferences.Snapshot{}, store.loadErr
	}
	return clonePreferencesForUpdaterTest(store.snapshot), nil
}

func (store *fakeUpdatePreferenceStore) CompareAndSwap(_ context.Context, expected uint64, next preferences.Snapshot) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	store.compareCalls++
	if len(store.compareSteps) > 0 {
		step := store.compareSteps[0]
		store.compareSteps = store.compareSteps[1:]
		if step.publish {
			store.snapshot = clonePreferencesForUpdaterTest(next)
		}
		return step.err
	}
	if store.compareErr != nil {
		return store.compareErr
	}
	if store.snapshot.Revision != expected {
		return preferences.ErrPreferencesConflict
	}
	store.snapshot = clonePreferencesForUpdaterTest(next)
	return nil
}

func clonePreferencesForUpdaterTest(snapshot preferences.Snapshot) preferences.Snapshot {
	clone := snapshot
	if snapshot.Updates.SkippedVersion != nil {
		value := *snapshot.Updates.SkippedVersion
		clone.Updates.SkippedVersion = &value
	}
	if snapshot.Updates.SnoozeUntilMS != nil {
		value := *snapshot.Updates.SnoozeUntilMS
		clone.Updates.SnoozeUntilMS = &value
	}
	if snapshot.Updates.LastCheckAtMS != nil {
		value := *snapshot.Updates.LastCheckAtMS
		clone.Updates.LastCheckAtMS = &value
	}
	return clone
}

type fakeUpdateController struct {
	snapshot      Snapshot
	checkCalls    int
	downloadCalls int
	installCalls  int
	chooseCalls   []UpdateChoice
	chooseErr     error
	chooseHook    func()
	cancelCalls   int
	closeCalls    int
}

func (controller *fakeUpdateController) Snapshot() Snapshot {
	return cloneSnapshot(controller.snapshot)
}
func (controller *fakeUpdateController) Check() error {
	controller.checkCalls++
	controller.snapshot = Snapshot{Phase: PhaseChecking, CanCancel: true}
	return nil
}
func (controller *fakeUpdateController) Download() error {
	controller.downloadCalls++
	return nil
}
func (controller *fakeUpdateController) Install() error { controller.installCalls++; return nil }
func (controller *fakeUpdateController) Cancel() error  { controller.cancelCalls++; return nil }
func (controller *fakeUpdateController) Choose(choice UpdateChoice) error {
	controller.chooseCalls = append(controller.chooseCalls, choice)
	if controller.chooseHook != nil {
		controller.chooseHook()
	}
	if controller.chooseErr != nil {
		return controller.chooseErr
	}
	controller.snapshot = Snapshot{Phase: PhaseIdle}
	return nil
}
func (controller *fakeUpdateController) Close() error { controller.closeCalls++; return nil }

type fakeUpdateCronRunner struct {
	job        func()
	stopped    chan struct{}
	startCalls int
	stopCalls  int
}

type blockingUpdateController struct {
	started    chan struct{}
	release    chan struct{}
	closeCalls atomic.Int64
}

type blockingDownloadController struct {
	started chan struct{}
	release chan struct{}
}

func (*blockingDownloadController) Snapshot() Snapshot {
	return Snapshot{Phase: PhaseAvailable, Update: &Update{Version: "42"}}
}
func (*blockingDownloadController) Check() error { return nil }
func (controller *blockingDownloadController) Download() error {
	close(controller.started)
	<-controller.release
	return nil
}
func (*blockingDownloadController) Install() error            { return nil }
func (*blockingDownloadController) Cancel() error             { return nil }
func (*blockingDownloadController) Choose(UpdateChoice) error { return nil }
func (*blockingDownloadController) Close() error              { return nil }

func (*blockingUpdateController) Snapshot() Snapshot { return Snapshot{Phase: PhaseIdle} }
func (controller *blockingUpdateController) Check() error {
	close(controller.started)
	<-controller.release
	return nil
}
func (*blockingUpdateController) Download() error           { return nil }
func (*blockingUpdateController) Install() error            { return nil }
func (*blockingUpdateController) Cancel() error             { return nil }
func (*blockingUpdateController) Choose(UpdateChoice) error { return nil }
func (controller *blockingUpdateController) Close() error {
	controller.closeCalls.Add(1)
	return nil
}

func (runner *fakeUpdateCronRunner) Start() { runner.startCalls++ }
func (runner *fakeUpdateCronRunner) Stop() context.Context {
	runner.stopCalls++
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		<-runner.stopped
		cancel()
	}()
	return ctx
}
