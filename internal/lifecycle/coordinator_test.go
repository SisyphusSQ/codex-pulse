package lifecycle

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"sync"
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

func TestCoordinatorHomeChangedDrainsAndRebindsGeneration(t *testing.T) {
	t.Parallel()

	repository := openLifecycleRepository(t)
	scheduler := &fakeSchedulerControl{}
	reconciler := &fakeReconciler{result: ReconcileResult{
		HomeGeneration: 2, SourceState: store.LifecycleSourceAvailable,
	}}
	coordinator := newLifecycleCoordinator(t, repository, scheduler, reconciler)
	if _, err := coordinator.Initialize(context.Background(), 1, "startup:1"); err != nil {
		t.Fatalf("Initialize() error = %v", err)
	}
	if _, err := coordinator.Pause(
		context.Background(), "pause:backfill", store.LifecyclePauseBackfill,
	); err != nil {
		t.Fatalf("Pause(backfill) error = %v", err)
	}
	scheduler.drains = nil
	changed, err := coordinator.HomeChanged(context.Background(), "home:2", 2)
	if err != nil || changed.HomeGeneration != 2 ||
		changed.UserPauseScope != store.LifecyclePauseBackfill ||
		changed.SystemState != store.LifecycleSystemAwake ||
		changed.SourceState != store.LifecycleSourceAvailable ||
		changed.Transition != store.LifecycleTransitionSteady {
		t.Fatalf("HomeChanged() = %#v, %v", changed, err)
	}
	if !reflect.DeepEqual(scheduler.drains, []store.LifecyclePauseScope{store.LifecyclePauseAll}) {
		t.Fatalf("HomeChanged drain scopes = %v", scheduler.drains)
	}
	if len(reconciler.reasons) != 1 || reconciler.reasons[0] != ReconcileSourceChange ||
		len(reconciler.observed) != 1 || reconciler.observed[0].HomeGeneration != 2 ||
		reconciler.observed[0].Transition != store.LifecycleTransitionReconciling {
		t.Fatalf("HomeChanged reconcile = reasons:%v observed:%#v", reconciler.reasons, reconciler.observed)
	}
	revision := changed.Revision
	changed, err = coordinator.HomeChanged(context.Background(), "home:2", 2)
	if err != nil || changed.Revision != revision || len(scheduler.drains) != 1 ||
		len(reconciler.reasons) != 1 {
		t.Fatalf("HomeChanged(replay) = %#v, %v", changed, err)
	}
}

func TestCoordinatorHomeChangedWaitsForOldWriterBeforeGenerationCAS(t *testing.T) {
	t.Parallel()

	repository := openLifecycleRepository(t)
	drainStarted := make(chan struct{})
	releaseDrain := make(chan struct{})
	scheduler := &fakeSchedulerControl{
		drainStarted: drainStarted,
		releaseDrain: releaseDrain,
	}
	reconciler := &fakeReconciler{result: ReconcileResult{
		HomeGeneration: 2, SourceState: store.LifecycleSourceAvailable,
	}}
	coordinator := newLifecycleCoordinator(t, repository, scheduler, reconciler)
	if _, err := coordinator.Initialize(context.Background(), 1, "startup:1"); err != nil {
		t.Fatalf("Initialize() error = %v", err)
	}
	changed := make(chan commandResult, 1)
	go func() {
		state, err := coordinator.HomeChanged(context.Background(), "home:barrier:2", 2)
		changed <- commandResult{state: state, err: err}
	}()
	select {
	case <-drainStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("HomeChanged did not reach scheduler drain")
	}
	intent, err := repository.SchedulerLifecycle(context.Background())
	if err != nil || intent.HomeGeneration != 1 ||
		intent.Transition != store.LifecycleTransitionDraining ||
		intent.SourceState != store.LifecycleSourceUnknown {
		t.Fatalf("HomeChanged drain intent = %#v, %v", intent, err)
	}
	select {
	case result := <-changed:
		t.Fatalf("HomeChanged returned before old writer drained: %#v", result)
	default:
	}
	close(releaseDrain)
	select {
	case result := <-changed:
		if result.err != nil || result.state.HomeGeneration != 2 {
			t.Fatalf("HomeChanged(after drain) = %#v", result)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("HomeChanged did not finish after old writer drained")
	}
}

func TestCoordinatorHomeChangedPreservesSleepAndPauseUntilWake(t *testing.T) {
	t.Parallel()

	repository := openLifecycleRepository(t)
	scheduler := &fakeSchedulerControl{}
	reconciler := &fakeReconciler{result: ReconcileResult{
		HomeGeneration: 2, SourceState: store.LifecycleSourceAvailable,
	}}
	coordinator := newLifecycleCoordinator(t, repository, scheduler, reconciler)
	if _, err := coordinator.Initialize(context.Background(), 1, "startup:1"); err != nil {
		t.Fatalf("Initialize() error = %v", err)
	}
	if _, err := coordinator.Pause(
		context.Background(), "pause:all", store.LifecyclePauseAll,
	); err != nil {
		t.Fatalf("Pause(all) error = %v", err)
	}
	if _, err := coordinator.SystemWillSleep(context.Background(), "sleep:home-change"); err != nil {
		t.Fatalf("SystemWillSleep() error = %v", err)
	}
	scheduler.drains = nil
	changed, err := coordinator.HomeChanged(context.Background(), "home:sleeping:2", 2)
	if err != nil || changed.HomeGeneration != 2 ||
		changed.UserPauseScope != store.LifecyclePauseAll ||
		changed.SystemState != store.LifecycleSystemSleeping ||
		changed.SourceState != store.LifecycleSourceUnknown ||
		changed.Transition != store.LifecycleTransitionSteady || len(reconciler.reasons) != 0 {
		t.Fatalf("HomeChanged(sleeping) = %#v, %v; reasons=%v", changed, err, reconciler.reasons)
	}
	if !reflect.DeepEqual(scheduler.drains, []store.LifecyclePauseScope{store.LifecyclePauseAll}) {
		t.Fatalf("HomeChanged(sleeping) drains = %v", scheduler.drains)
	}
	woke, err := coordinator.SystemDidWake(context.Background(), "wake:home-change")
	if err != nil || woke.HomeGeneration != 2 ||
		woke.UserPauseScope != store.LifecyclePauseAll ||
		woke.SourceState != store.LifecycleSourceAvailable || len(reconciler.reasons) != 1 {
		t.Fatalf("SystemDidWake(after HomeChanged) = %#v, %v", woke, err)
	}
}

func TestCoordinatorHomeChangedFencesOldQueueAndOpensNewGeneration(t *testing.T) {
	t.Parallel()

	repository := openLifecycleRepository(t)
	scheduler := &fakeSchedulerControl{}
	reconciler := &fakeReconciler{result: ReconcileResult{
		HomeGeneration: 2, SourceState: store.LifecycleSourceAvailable,
	}}
	coordinator := newLifecycleCoordinator(t, repository, scheduler, reconciler)
	if _, err := coordinator.Initialize(context.Background(), 1, "startup:1"); err != nil {
		t.Fatalf("Initialize() error = %v", err)
	}
	oldJob := store.JobRun{
		JobID: "home-change-old-job", JobType: "lifecycle-test", RequestedBy: "test",
		Priority: 1, State: store.JobQueued, Phase: store.JobPhaseLive,
		CreatedAtMS: 10, UpdatedAtMS: 10,
	}
	if err := repository.CreateJobRun(context.Background(), oldJob); err != nil {
		t.Fatalf("CreateJobRun(old) error = %v", err)
	}
	oldTask := store.SchedulerTask{
		TaskID: "home-change-old-task", DedupeKey: "home-change:old",
		TargetKind: store.SchedulerTargetLiveScan, TargetID: oldJob.JobID,
		HomeGeneration: 1, Lane: store.SchedulerLaneLive,
		ServiceClass: store.SchedulerServiceBackground, State: store.SchedulerTaskQueued,
		QueueOrderMS: 10, EnqueuedAtMS: 10, UpdatedAtMS: 10,
	}
	if err := repository.EnqueueSchedulerTask(context.Background(), oldTask, 8); err != nil {
		t.Fatalf("EnqueueSchedulerTask(old) error = %v", err)
	}
	before, err := repository.SchedulerRunnableQueueSnapshot(context.Background())
	if err != nil || before.LiveDepth != 1 || before.LiveCandidate == nil ||
		before.LiveCandidate.TaskID != oldTask.TaskID {
		t.Fatalf("SchedulerRunnableQueueSnapshot(before) = %#v, %v", before, err)
	}
	if _, err := coordinator.HomeChanged(context.Background(), "home:queue:2", 2); err != nil {
		t.Fatalf("HomeChanged() error = %v", err)
	}
	after, err := repository.SchedulerRunnableQueueSnapshot(context.Background())
	if err != nil || after.LiveDepth != 0 || after.LiveCandidate != nil {
		t.Fatalf("SchedulerRunnableQueueSnapshot(old fenced) = %#v, %v", after, err)
	}
	newJob := store.JobRun{
		JobID: "home-change-new-job", JobType: "lifecycle-test", RequestedBy: "test",
		Priority: 1, State: store.JobQueued, Phase: store.JobPhaseLive,
		CreatedAtMS: 20, UpdatedAtMS: 20,
	}
	if err := repository.CreateJobRun(context.Background(), newJob); err != nil {
		t.Fatalf("CreateJobRun(new) error = %v", err)
	}
	newTask := store.SchedulerTask{
		TaskID: "home-change-new-task", DedupeKey: "home-change:new",
		TargetKind: store.SchedulerTargetLiveScan, TargetID: newJob.JobID,
		HomeGeneration: 2, Lane: store.SchedulerLaneLive,
		ServiceClass: store.SchedulerServiceBackground, State: store.SchedulerTaskQueued,
		QueueOrderMS: 20, EnqueuedAtMS: 20, UpdatedAtMS: 20,
	}
	if err := repository.EnqueueSchedulerTask(context.Background(), newTask, 8); err != nil {
		t.Fatalf("EnqueueSchedulerTask(new) error = %v", err)
	}
	after, err = repository.SchedulerRunnableQueueSnapshot(context.Background())
	if err != nil || after.LiveDepth != 1 || after.LiveCandidate == nil ||
		after.LiveCandidate.TaskID != newTask.TaskID {
		t.Fatalf("SchedulerRunnableQueueSnapshot(new active) = %#v, %v", after, err)
	}
}

type fakeSchedulerControl struct {
	drains       []store.LifecyclePauseScope
	drainErr     error
	recoverCalls int
	recoverErr   error
	drainStarted chan struct{}
	releaseDrain chan struct{}
	drainOnce    sync.Once
}

func (scheduler *fakeSchedulerControl) Drain(_ context.Context, scope store.LifecyclePauseScope) error {
	scheduler.drains = append(scheduler.drains, scope)
	if scheduler.drainStarted != nil {
		scheduler.drainOnce.Do(func() { close(scheduler.drainStarted) })
	}
	if scheduler.releaseDrain != nil {
		<-scheduler.releaseDrain
	}
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
