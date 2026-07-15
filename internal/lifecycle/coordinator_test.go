package lifecycle

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/SisyphusSQ/codex-pulse/internal/store"
	storesqlite "github.com/SisyphusSQ/codex-pulse/internal/store/sqlite"
)

func TestCoordinatorKeepsUserPauseAcrossSleepWakeAndExactReplay(t *testing.T) {
	t.Parallel()

	repository := openLifecycleRepository(t)
	scheduler := &fakeSchedulerControl{}
	reconciler := &fakeReconciler{result: ReconcileResult{
		HomeGeneration: 7, SourceState: store.LifecycleSourceAvailable,
	}}
	coordinator := newLifecycleCoordinator(t, repository, scheduler, reconciler)
	if _, err := coordinator.Initialize(context.Background(), 7, "startup:7"); err != nil {
		t.Fatalf("Initialize() error = %v", err)
	}
	paused, err := coordinator.Pause(context.Background(), "user:pause:1", store.LifecyclePauseAll)
	if err != nil || paused.UserPauseScope != store.LifecyclePauseAll ||
		paused.Transition != store.LifecycleTransitionSteady {
		t.Fatalf("Pause() = %#v, %v", paused, err)
	}
	revision := paused.Revision
	paused, err = coordinator.Pause(context.Background(), "user:pause:1", store.LifecyclePauseAll)
	if err != nil || paused.Revision != revision || len(scheduler.drains) != 1 {
		t.Fatalf("Pause(exact replay) = %#v, %v, drains=%v", paused, err, scheduler.drains)
	}
	if _, err := coordinator.SystemWillSleep(context.Background(), "system:sleep:1"); err != nil {
		t.Fatalf("SystemWillSleep() error = %v", err)
	}
	woke, err := coordinator.SystemDidWake(context.Background(), "system:wake:1")
	if err != nil || woke.SystemState != store.LifecycleSystemAwake ||
		woke.UserPauseScope != store.LifecyclePauseAll ||
		woke.Transition != store.LifecycleTransitionSteady ||
		woke.SourceState != store.LifecycleSourceAvailable {
		t.Fatalf("SystemDidWake() = %#v, %v", woke, err)
	}
	if want := []store.LifecyclePauseScope{
		store.LifecyclePauseAll, store.LifecyclePauseAll,
	}; !reflect.DeepEqual(scheduler.drains, want) {
		t.Fatalf("drain scopes = %v, want %v", scheduler.drains, want)
	}
	if len(reconciler.reasons) != 1 || reconciler.reasons[0] != ReconcileSystemWake {
		t.Fatalf("reconcile reasons = %v", reconciler.reasons)
	}
	wakeRevision := woke.Revision
	woke, err = coordinator.SystemDidWake(context.Background(), "system:wake:1")
	if err != nil || woke.Revision != wakeRevision || len(reconciler.reasons) != 1 {
		t.Fatalf("SystemDidWake(exact replay) = %#v, %v; reasons=%v", woke, err, reconciler.reasons)
	}
}

func TestEventTerminalRequiresMatchingEventPrefix(t *testing.T) {
	t.Parallel()

	for _, terminal := range []string{"blocked", "complete", "deferred"} {
		if eventTerminal(terminal, "new-event") {
			t.Fatalf("eventTerminal(%q, new-event) = true, want false", terminal)
		}
		if !eventTerminal("new-event:"+terminal, "new-event") {
			t.Fatalf("eventTerminal(new-event:%s, new-event) = false, want true", terminal)
		}
	}
}

func TestCoordinatorRejectsTimestampSuccessorPastRuntimeBoundary(t *testing.T) {
	t.Parallel()

	repository := openLifecycleRepository(t)
	scheduler := &fakeSchedulerControl{}
	reconciler := &fakeReconciler{result: ReconcileResult{
		HomeGeneration: 1, SourceState: store.LifecycleSourceAvailable,
	}}
	coordinator, err := NewCoordinator(Config{
		Repository: repository, Scheduler: scheduler, Reconciler: reconciler,
		Clock: func() time.Time { return time.UnixMilli(store.MaxSchedulerTimestampMS) },
	})
	if err != nil {
		t.Fatalf("NewCoordinator() error = %v", err)
	}
	t.Cleanup(func() { _ = coordinator.Close(context.Background()) })
	initial, err := coordinator.Initialize(context.Background(), 1, "boundary:initialize")
	if err != nil || initial.UpdatedAtMS != store.MaxSchedulerTimestampMS {
		t.Fatalf("Initialize() = %#v, %v", initial, err)
	}
	if _, err := coordinator.Pause(
		context.Background(), "boundary:pause", store.LifecyclePauseAll,
	); !errors.Is(err, ErrInvalidCoordinator) {
		t.Fatalf("Pause(runtime timestamp exhausted) error = %v, want ErrInvalidCoordinator", err)
	}
	stored, err := repository.SchedulerLifecycle(context.Background())
	if err != nil || stored != initial {
		t.Fatalf("SchedulerLifecycle() = %#v, %v, want unchanged %#v", stored, err, initial)
	}
}

func TestCoordinatorResumeReconcilesBeforeOpeningPermit(t *testing.T) {
	t.Parallel()

	repository := openLifecycleRepository(t)
	scheduler := &fakeSchedulerControl{}
	reconciler := &fakeReconciler{result: ReconcileResult{
		HomeGeneration: 3, SourceState: store.LifecycleSourceAvailable,
	}}
	coordinator := newLifecycleCoordinator(t, repository, scheduler, reconciler)
	if _, err := coordinator.Initialize(context.Background(), 3, "startup:3"); err != nil {
		t.Fatalf("Initialize() error = %v", err)
	}
	if _, err := coordinator.Pause(context.Background(), "pause:all", store.LifecyclePauseAll); err != nil {
		t.Fatalf("Pause() error = %v", err)
	}
	resumed, err := coordinator.Resume(context.Background(), "resume:all")
	if err != nil || resumed.UserPauseScope != store.LifecyclePauseNone ||
		resumed.Transition != store.LifecycleTransitionSteady ||
		resumed.SourceState != store.LifecycleSourceAvailable {
		t.Fatalf("Resume() = %#v, %v", resumed, err)
	}
	if len(reconciler.reasons) != 1 || reconciler.reasons[0] != ReconcileUserResume {
		t.Fatalf("reconcile reasons = %v", reconciler.reasons)
	}
	if reconciler.observed[0].Transition != store.LifecycleTransitionReconciling ||
		reconciler.observed[0].UserPauseScope != store.LifecyclePauseNone {
		t.Fatalf("reconciler observed = %#v", reconciler.observed[0])
	}
}

func TestCoordinatorBlocksGenerationDriftAndSourceLoss(t *testing.T) {
	t.Parallel()

	repository := openLifecycleRepository(t)
	scheduler := &fakeSchedulerControl{}
	reconciler := &fakeReconciler{result: ReconcileResult{
		HomeGeneration: 10, SourceState: store.LifecycleSourceAvailable,
	}}
	coordinator := newLifecycleCoordinator(t, repository, scheduler, reconciler)
	if _, err := coordinator.Initialize(context.Background(), 9, "startup:9"); err != nil {
		t.Fatalf("Initialize() error = %v", err)
	}
	if _, err := coordinator.SystemDidWake(context.Background(), "wake:drift"); !errors.Is(err, ErrGenerationChanged) {
		t.Fatalf("SystemDidWake(drift) error = %v, want ErrGenerationChanged", err)
	}
	blocked, err := repository.SchedulerLifecycle(context.Background())
	if err != nil || blocked.Transition != store.LifecycleTransitionBlocked ||
		blocked.SourceState != store.LifecycleSourceUnavailable {
		t.Fatalf("SchedulerLifecycle(blocked) = %#v, %v", blocked, err)
	}
	if _, err := coordinator.SourceChanged(context.Background(), "source:lost", false); err != nil {
		t.Fatalf("SourceChanged(unavailable) error = %v", err)
	}
	if len(scheduler.drains) != 1 || scheduler.drains[0] != store.LifecyclePauseAll {
		t.Fatalf("drain scopes = %v", scheduler.drains)
	}
}

func TestCoordinatorReplaysBlockedSourceEventsWithoutRepeatedWork(t *testing.T) {
	t.Parallel()

	repository := openLifecycleRepository(t)
	scheduler := &fakeSchedulerControl{}
	reconciler := &fakeReconciler{err: ErrSourceUnavailable}
	coordinator := newLifecycleCoordinator(t, repository, scheduler, reconciler)
	if _, err := coordinator.Initialize(context.Background(), 9, "startup:9"); err != nil {
		t.Fatalf("Initialize() error = %v", err)
	}

	if _, err := coordinator.SourceChanged(context.Background(), "source:lost", false); err != nil {
		t.Fatalf("SourceChanged(unavailable) error = %v", err)
	}
	blocked, err := repository.SchedulerLifecycle(context.Background())
	if err != nil {
		t.Fatalf("SchedulerLifecycle(unavailable) error = %v", err)
	}
	if replay, err := coordinator.SourceChanged(context.Background(), "source:lost", false); err != nil ||
		replay.Revision != blocked.Revision || len(scheduler.drains) != 1 {
		t.Fatalf("SourceChanged(unavailable replay) = %#v, %v; drains=%v", replay, err, scheduler.drains)
	}

	if _, err := coordinator.SourceChanged(context.Background(), "source:recover", true); !errors.Is(err, ErrSourceUnavailable) {
		t.Fatalf("SourceChanged(available failure) error = %v, want ErrSourceUnavailable", err)
	}
	failed, err := repository.SchedulerLifecycle(context.Background())
	if err != nil {
		t.Fatalf("SchedulerLifecycle(reconcile failure) error = %v", err)
	}
	if replay, err := coordinator.SourceChanged(context.Background(), "source:recover", true); !errors.Is(err, ErrSourceUnavailable) || replay.Revision != failed.Revision || len(reconciler.reasons) != 1 {
		t.Fatalf("SourceChanged(available replay) = %#v, %v; reasons=%v", replay, err, reconciler.reasons)
	}

	if _, err := coordinator.SystemDidWake(context.Background(), "wake:blocked"); !errors.Is(err, ErrSourceUnavailable) {
		t.Fatalf("SystemDidWake(failure) error = %v, want ErrSourceUnavailable", err)
	}
	wakeFailed, err := repository.SchedulerLifecycle(context.Background())
	if err != nil {
		t.Fatalf("SchedulerLifecycle(wake failure) error = %v", err)
	}
	if replay, err := coordinator.SystemDidWake(context.Background(), "wake:blocked"); !errors.Is(err, ErrSourceUnavailable) || replay.Revision != wakeFailed.Revision || len(reconciler.reasons) != 2 {
		t.Fatalf("SystemDidWake(blocked replay) = %#v, %v; reasons=%v", replay, err, reconciler.reasons)
	}
}

func TestCoordinatorDefersSourceRecoveryWhileSleeping(t *testing.T) {
	t.Parallel()

	repository := openLifecycleRepository(t)
	scheduler := &fakeSchedulerControl{}
	reconciler := &fakeReconciler{result: ReconcileResult{
		HomeGeneration: 3, SourceState: store.LifecycleSourceAvailable,
	}}
	coordinator := newLifecycleCoordinator(t, repository, scheduler, reconciler)
	if _, err := coordinator.Initialize(context.Background(), 3, "startup:3"); err != nil {
		t.Fatalf("Initialize() error = %v", err)
	}
	if _, err := coordinator.SystemWillSleep(context.Background(), "sleep:1"); err != nil {
		t.Fatalf("SystemWillSleep() error = %v", err)
	}
	deferred, err := coordinator.SourceChanged(context.Background(), "source:available", true)
	if err != nil || deferred.SystemState != store.LifecycleSystemSleeping ||
		deferred.SourceState != store.LifecycleSourceUnknown ||
		deferred.Transition != store.LifecycleTransitionSteady || len(reconciler.reasons) != 0 {
		t.Fatalf("SourceChanged(sleeping) = %#v, %v; reasons=%v", deferred, err, reconciler.reasons)
	}
	revision := deferred.Revision
	deferred, err = coordinator.SourceChanged(context.Background(), "source:available", true)
	if err != nil || deferred.Revision != revision || len(reconciler.reasons) != 0 {
		t.Fatalf("SourceChanged(sleeping replay) = %#v, %v; reasons=%v", deferred, err, reconciler.reasons)
	}
	woke, err := coordinator.SystemDidWake(context.Background(), "wake:1")
	if err != nil || woke.SourceState != store.LifecycleSourceAvailable || len(reconciler.reasons) != 1 ||
		reconciler.reasons[0] != ReconcileSystemWake {
		t.Fatalf("SystemDidWake(after deferred source) = %#v, %v; reasons=%v", woke, err, reconciler.reasons)
	}
}

func TestCoordinatorRecoverDoesNotBypassDurablePause(t *testing.T) {
	t.Parallel()

	repository := openLifecycleRepository(t)
	firstScheduler := &fakeSchedulerControl{}
	reconciler := &fakeReconciler{result: ReconcileResult{
		HomeGeneration: 5, SourceState: store.LifecycleSourceAvailable,
	}}
	first := newLifecycleCoordinator(t, repository, firstScheduler, reconciler)
	if _, err := first.Initialize(context.Background(), 5, "startup:5"); err != nil {
		t.Fatalf("Initialize() error = %v", err)
	}
	if _, err := first.Pause(context.Background(), "pause:restart", store.LifecyclePauseAll); err != nil {
		t.Fatalf("Pause() error = %v", err)
	}
	if err := first.Close(context.Background()); err != nil {
		t.Fatalf("Close(first) error = %v", err)
	}

	restartedScheduler := &fakeSchedulerControl{}
	restarted := newLifecycleCoordinator(t, repository, restartedScheduler, reconciler)
	recovered, err := restarted.Recover(context.Background(), "startup:recover")
	if err != nil || recovered.UserPauseScope != store.LifecyclePauseAll {
		t.Fatalf("Recover() = %#v, %v", recovered, err)
	}
	if restartedScheduler.recoverCalls != 0 {
		t.Fatalf("RecoverActiveTasks calls = %d, want 0", restartedScheduler.recoverCalls)
	}
}

type fakeSchedulerControl struct {
	drains       []store.LifecyclePauseScope
	drainErr     error
	recoverCalls int
	recoverErr   error
}

func (scheduler *fakeSchedulerControl) Drain(_ context.Context, scope store.LifecyclePauseScope) error {
	scheduler.drains = append(scheduler.drains, scope)
	return scheduler.drainErr
}

func (scheduler *fakeSchedulerControl) RecoverActiveTasks(context.Context) ([]store.SchedulerTask, error) {
	scheduler.recoverCalls++
	return nil, scheduler.recoverErr
}

type fakeReconciler struct {
	result   ReconcileResult
	err      error
	reasons  []ReconcileReason
	observed []store.SchedulerLifecycle
}

func (reconciler *fakeReconciler) Reconcile(
	_ context.Context,
	current store.SchedulerLifecycle,
	reason ReconcileReason,
) (ReconcileResult, error) {
	reconciler.reasons = append(reconciler.reasons, reason)
	reconciler.observed = append(reconciler.observed, current)
	return reconciler.result, reconciler.err
}

func newLifecycleCoordinator(
	t *testing.T,
	repository *store.Repository,
	scheduler SchedulerControl,
	reconciler Reconciler,
) *Coordinator {
	t.Helper()
	nowMS := int64(100)
	coordinator, err := NewCoordinator(Config{
		Repository: repository, Scheduler: scheduler, Reconciler: reconciler,
		Clock: func() time.Time {
			nowMS++
			return time.UnixMilli(nowMS)
		},
	})
	if err != nil {
		t.Fatalf("NewCoordinator() error = %v", err)
	}
	t.Cleanup(func() { _ = coordinator.Close(context.Background()) })
	return coordinator
}

func openLifecycleRepository(t *testing.T) *store.Repository {
	t.Helper()
	directory := t.TempDir()
	if err := os.Chmod(directory, 0o700); err != nil {
		t.Fatalf("secure temp directory: %v", err)
	}
	database, err := storesqlite.Open(context.Background(), storesqlite.Config{
		Path: filepath.Join(directory, "lifecycle.db"),
	})
	if err != nil {
		t.Fatalf("sqlite.Open() error = %v", err)
	}
	t.Cleanup(func() { _ = database.Close(context.Background()) })
	repository := store.NewRepository(database)
	if err := repository.EnsureApplicationSchema(context.Background()); err != nil {
		t.Fatalf("EnsureApplicationSchema() error = %v", err)
	}
	return repository
}
