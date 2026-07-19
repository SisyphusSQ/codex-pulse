package scheduler

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/robfig/cron/v3"

	"github.com/SisyphusSQ/codex-pulse/internal/store"
)

func TestDefaultCronRunnerRegistersOneSecondScheduleAndSkipsOverlap(t *testing.T) {
	t.Parallel()

	var active atomic.Int32
	var maximum atomic.Int32
	entered := make(chan struct{})
	release := make(chan struct{})
	runner, err := defaultCronRunnerFactory(cron.FuncJob(func() {
		current := active.Add(1)
		defer active.Add(-1)
		for {
			observed := maximum.Load()
			if current <= observed || maximum.CompareAndSwap(observed, current) {
				break
			}
		}
		close(entered)
		<-release
	}))
	if err != nil {
		t.Fatalf("defaultCronRunnerFactory() error = %v", err)
	}
	concrete, ok := runner.(*cron.Cron)
	if !ok {
		t.Fatalf("runner type = %T, want *cron.Cron", runner)
	}
	entries := concrete.Entries()
	if len(entries) != 1 {
		t.Fatalf("entries = %d, want 1", len(entries))
	}
	base := time.Unix(100, 0).UTC()
	if delay := entries[0].Schedule.Next(base).Sub(base); delay != time.Second {
		t.Fatalf("schedule delay = %s, want 1s", delay)
	}

	firstDone := make(chan struct{})
	go func() {
		entries[0].WrappedJob.Run()
		close(firstDone)
	}()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("first wrapped job did not start")
	}
	secondDone := make(chan struct{})
	go func() {
		entries[0].WrappedJob.Run()
		close(secondDone)
	}()
	select {
	case <-secondDone:
	case <-time.After(time.Second):
		t.Fatal("overlapping wrapped job was not skipped")
	}
	if maximum.Load() != 1 {
		t.Fatalf("maximum concurrent jobs = %d, want 1", maximum.Load())
	}
	close(release)
	select {
	case <-firstDone:
	case <-time.After(time.Second):
		t.Fatal("first wrapped job did not finish")
	}
}

func TestDefaultCronRunnerRestoresOverlapTokenAfterPanic(t *testing.T) {
	t.Parallel()

	var calls atomic.Int32
	runner, err := defaultCronRunnerFactory(cron.FuncJob(func() {
		if calls.Add(1) == 1 {
			panic("cron wrapper test panic")
		}
	}))
	if err != nil {
		t.Fatalf("defaultCronRunnerFactory() error = %v", err)
	}
	concrete, ok := runner.(*cron.Cron)
	if !ok {
		t.Fatalf("runner type = %T, want *cron.Cron", runner)
	}
	entry := concrete.Entries()[0]
	entry.WrappedJob.Run()
	entry.WrappedJob.Run()
	if calls.Load() != 2 {
		t.Fatalf("job calls = %d, want 2; overlap token was not restored after panic", calls.Load())
	}
}

func TestServiceRunUsesCronTriggerAfterImmediateEmptyCycle(t *testing.T) {
	t.Parallel()

	repository := openSchedulerRepository(t)
	initializeCronLifecycle(t, repository)
	called := make(chan struct{})
	executor := &recordingExecutor{execute: func(
		context.Context,
		store.SchedulerTask,
		ScanBudget,
	) (SliceResult, error) {
		close(called)
		return SliceResult{StopReason: store.SchedulerStopCompleted}, nil
	}}
	service := newSchedulerTestService(t, repository, executor)
	runner := newFakeCronRunner()
	service.newCronRunner = runner.factory

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- service.Run(ctx) }()
	runner.waitStarted(t, done)
	createSchedulerFixture(t, repository, "cron-trigger", store.SchedulerLaneLive, 10)
	runner.trigger(t)
	select {
	case <-called:
	case <-time.After(time.Second):
		t.Fatal("cron trigger did not execute queued task")
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Run() error = %v, want context.Canceled", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Run() did not stop after cancellation")
	}
	if runner.stopCalls.Load() != 1 {
		t.Fatalf("Stop() calls = %d, want 1", runner.stopCalls.Load())
	}
}

func TestServiceScheduledBurstContinuesInteractiveAndRechecksLivePriority(t *testing.T) {
	t.Parallel()

	repository := openSchedulerRepository(t)
	backfill := createSchedulerFixture(t, repository, "burst-backfill", store.SchedulerLaneBackfill, 10)
	if _, err := repository.PromoteSchedulerTask(context.Background(), backfill.DedupeKey, 11); err != nil {
		t.Fatalf("PromoteSchedulerTask() error = %v", err)
	}
	var live store.SchedulerTask
	executor := &recordingExecutor{}
	executor.execute = func(
		_ context.Context,
		task store.SchedulerTask,
		_ ScanBudget,
	) (SliceResult, error) {
		switch len(executor.calls) {
		case 1:
			if task.TaskID != backfill.TaskID {
				t.Fatalf("first burst task = %s, want backfill", task.TaskID)
			}
			live = createSchedulerFixture(t, repository, "burst-live", store.SchedulerLaneLive, 12)
			return SliceResult{BytesProcessed: 1, StopReason: store.SchedulerStopByteBudget}, nil
		case 2:
			if task.TaskID != live.TaskID {
				t.Fatalf("second burst task = %s, want newly queued live %s", task.TaskID, live.TaskID)
			}
			return SliceResult{StopReason: store.SchedulerStopCompleted}, nil
		default:
			t.Fatalf("unexpected executor call %d", len(executor.calls))
			return SliceResult{}, ErrInvalidSliceResult
		}
	}
	service := newSchedulerTestService(t, repository, executor)
	if err := service.runScheduledBurst(context.Background()); err != nil {
		t.Fatalf("runScheduledBurst() error = %v", err)
	}
	if len(executor.calls) != 2 || executor.budgets[0].YieldFor != 0 ||
		executor.budgets[1].YieldFor != DefaultBudgetPolicy().BackgroundNormal.YieldFor {
		t.Fatalf("burst calls = %#v budgets = %#v", executor.calls, executor.budgets)
	}
	storedBackfill, err := repository.SchedulerTask(context.Background(), backfill.TaskID)
	if err != nil || storedBackfill.State != store.SchedulerTaskQueued {
		t.Fatalf("backfill after live cooldown = %#v, %v", storedBackfill, err)
	}
}

func TestServiceRunCronTriggerResumesDurableDueRetry(t *testing.T) {
	t.Parallel()

	repository := openSchedulerRepository(t)
	task := createSchedulerFixture(t, repository, "cron-due-retry", store.SchedulerLaneLive, 10)
	var nowMS atomic.Int64
	nowMS.Store(100)
	completed := make(chan struct{})
	executor := &recordingExecutor{}
	executor.execute = func(
		_ context.Context,
		_ store.SchedulerTask,
		_ ScanBudget,
	) (SliceResult, error) {
		if len(executor.calls) == 1 {
			return SliceResult{}, errors.New("transient cron dependency")
		}
		close(completed)
		return SliceResult{StopReason: store.SchedulerStopCompleted}, nil
	}
	executor.retry = func(_ context.Context, failed store.SchedulerTask) (store.JobRun, error) {
		atMS := nowMS.Load()
		job := store.JobRun{
			JobID: failed.TargetID + "-retry", JobType: "scheduler-test", RequestedBy: "test", Priority: 1,
			State: store.JobQueued, Phase: store.JobPhaseLive, CreatedAtMS: atMS, UpdatedAtMS: atMS,
		}
		if err := repository.CreateJobRun(context.Background(), job); err != nil {
			return store.JobRun{}, err
		}
		return job, nil
	}
	cycle := 0
	service, err := NewService(ServiceConfig{
		Repository: repository,
		Executors: map[store.SchedulerTargetKind]Executor{
			store.SchedulerTargetLiveScan: executor, store.SchedulerTargetBootstrap: executor,
		},
		BudgetPolicy: DefaultBudgetPolicy(), MaxLiveBurst: 8,
		Clock: func() time.Time { return time.UnixMilli(nowMS.Load()) },
		NewCycleID: func() (string, error) {
			cycle++
			return "cron-due-retry-cycle-" + time.UnixMilli(int64(cycle)).Format("150405.000"), nil
		},
		RetryPolicy: fixedRetryPolicy{delay: time.Second},
	})
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	runner := newFakeCronRunner()
	service.newCronRunner = runner.factory
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- service.Run(ctx) }()
	runner.waitStarted(t, done)
	retryState, err := repository.SchedulerRetryState(context.Background(), task.TaskID)
	if err != nil || retryState.NextRetryAtMS == nil {
		cancel()
		t.Fatalf("SchedulerRetryState(waiting) = %#v, %v", retryState, err)
	}
	nowMS.Store(*retryState.NextRetryAtMS)
	runner.trigger(t)
	select {
	case <-completed:
	case <-time.After(time.Second):
		cancel()
		t.Fatal("cron trigger did not resume due retry")
	}
	deadline := time.Now().Add(time.Second)
	for {
		stored, readErr := repository.SchedulerTask(context.Background(), task.TaskID)
		if readErr == nil && stored.State == store.SchedulerTaskSucceeded {
			break
		}
		if time.Now().After(deadline) {
			cancel()
			t.Fatalf("cron retry did not commit before deadline: task=%#v read=%v", stored, readErr)
		}
		time.Sleep(time.Millisecond)
	}
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("Run() error = %v, want context.Canceled", err)
	}
	stored, err := repository.SchedulerTask(context.Background(), task.TaskID)
	if err != nil || stored.State != store.SchedulerTaskSucceeded || stored.TargetID != task.TargetID+"-retry" {
		t.Fatalf("SchedulerTask(after cron retry) = %#v, %v", stored, err)
	}
	if len(executor.retries) != 1 || executor.retries[0].TaskID != task.TaskID {
		t.Fatalf("retry calls = %#v", executor.retries)
	}
}

func TestServiceRunReturnsCronCycleFatalError(t *testing.T) {
	t.Parallel()

	repository := openSchedulerRepository(t)
	initializeCronLifecycle(t, repository)
	service := newSchedulerTestService(t, repository, &recordingExecutor{})
	wantErr := errors.New("system probe unavailable")
	service.systemProbe = &sequenceSystemProbe{results: []systemProbeResult{{}, {err: wantErr}}}
	runner := newFakeCronRunner()
	service.newCronRunner = runner.factory

	done := make(chan error, 1)
	go func() { done <- service.Run(context.Background()) }()
	runner.waitStarted(t, done)
	runner.trigger(t)
	select {
	case err := <-done:
		if !errors.Is(err, wantErr) {
			t.Fatalf("Run() error = %v, want %v", err, wantErr)
		}
	case <-time.After(time.Second):
		t.Fatal("Run() did not return cron cycle fatal error")
	}
	if runner.stopCalls.Load() != 1 {
		t.Fatalf("Stop() calls = %d, want 1", runner.stopCalls.Load())
	}
}

func TestServiceRunFencesQueuedCronTriggerAfterFirstFatalError(t *testing.T) {
	t.Parallel()

	repository := openSchedulerRepository(t)
	initializeCronLifecycle(t, repository)
	service := newSchedulerTestService(t, repository, &recordingExecutor{})
	wantErr := errors.New("first fatal scheduler error")
	probe := &sequenceSystemProbe{results: []systemProbeResult{{}, {err: wantErr}, {}}}
	service.systemProbe = probe
	runner := newFakeCronRunner()
	runner.stopHook = func() { runner.job.Run() }
	service.newCronRunner = runner.factory

	done := make(chan error, 1)
	go func() { done <- service.Run(context.Background()) }()
	runner.waitStarted(t, done)
	runner.trigger(t)
	select {
	case err := <-done:
		if !errors.Is(err, wantErr) {
			t.Fatalf("Run() error = %v, want %v", err, wantErr)
		}
	case <-time.After(time.Second):
		t.Fatal("Run() did not return first fatal error")
	}
	if calls := probe.callCount(); calls != 2 {
		t.Fatalf("system probe calls = %d, want 2; queued trigger crossed fatal fence", calls)
	}
}

func TestServiceRunReturnsTypedCronPanicWithoutLeakingPayload(t *testing.T) {
	t.Parallel()

	repository := openSchedulerRepository(t)
	initializeCronLifecycle(t, repository)
	service := newSchedulerTestService(t, repository, &recordingExecutor{})
	service.systemProbe = &panicAfterFirstSystemProbe{}
	runner := newFakeCronRunner()
	service.newCronRunner = runner.factory

	done := make(chan error, 1)
	go func() { done <- service.Run(context.Background()) }()
	runner.waitStarted(t, done)
	runner.trigger(t)
	select {
	case err := <-done:
		if !errors.Is(err, ErrSchedulerCronPanic) || err.Error() != ErrSchedulerCronPanic.Error() {
			t.Fatalf("Run() error = %v, want typed sanitized cron panic", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Run() did not return typed cron panic")
	}
}

func TestServiceRunReturnsTypedPanicFromImmediateZeroYieldBurst(t *testing.T) {
	t.Parallel()

	repository := openSchedulerRepository(t)
	initializeCronLifecycle(t, repository)
	task := createSchedulerFixture(t, repository, "immediate-burst-panic", store.SchedulerLaneBackfill, 10)
	if _, err := repository.PromoteSchedulerTask(context.Background(), task.DedupeKey, 11); err != nil {
		t.Fatalf("PromoteSchedulerTask() error = %v", err)
	}
	executor := &recordingExecutor{result: SliceResult{
		BytesProcessed: 1, StopReason: store.SchedulerStopByteBudget,
	}}
	service := newSchedulerTestService(t, repository, executor)
	service.systemProbe = &panicAfterFirstSystemProbe{}
	runner := newFakeCronRunner()
	service.newCronRunner = runner.factory

	if err := service.Run(context.Background()); !errors.Is(err, ErrSchedulerCronPanic) || err.Error() != ErrSchedulerCronPanic.Error() {
		t.Fatalf("Run() error = %v, want typed sanitized immediate burst panic", err)
	}
	select {
	case <-runner.started:
		t.Fatal("cron runner started after immediate burst panic")
	default:
	}
}

func TestServiceRunRejectsCronFactoryFailureBeforeTargetSideEffects(t *testing.T) {
	t.Parallel()

	repository := openSchedulerRepository(t)
	task := createSchedulerFixture(t, repository, "cron-factory-failure", store.SchedulerLaneLive, 10)
	executor := &recordingExecutor{}
	service := newSchedulerTestService(t, repository, executor)
	wantErr := errors.New("cron registration failed")
	service.newCronRunner = func(cron.Job) (cronRunner, error) { return nil, wantErr }

	if err := service.Run(context.Background()); !errors.Is(err, wantErr) {
		t.Fatalf("Run() error = %v, want %v", err, wantErr)
	}
	if len(executor.calls) != 0 {
		t.Fatalf("executor calls = %#v, want none", executor.calls)
	}
	stored, err := repository.SchedulerTask(context.Background(), task.TaskID)
	if err != nil || stored.State != store.SchedulerTaskQueued {
		t.Fatalf("SchedulerTask() = %#v, %v", stored, err)
	}
}

func TestServiceRunWaitsForCronJobWhenCancelled(t *testing.T) {
	t.Parallel()

	repository := openSchedulerRepository(t)
	initializeCronLifecycle(t, repository)
	entered := make(chan struct{})
	release := make(chan struct{})
	executor := &recordingExecutor{execute: func(
		context.Context,
		store.SchedulerTask,
		ScanBudget,
	) (SliceResult, error) {
		close(entered)
		<-release
		return SliceResult{StopReason: store.SchedulerStopCompleted}, nil
	}}
	service := newSchedulerTestService(t, repository, executor)
	runner := newFakeCronRunner()
	service.newCronRunner = runner.factory
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- service.Run(ctx) }()
	runner.waitStarted(t, done)
	createSchedulerFixture(t, repository, "cron-stop-drain", store.SchedulerLaneLive, 10)
	runner.trigger(t)
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("cron job did not enter executor")
	}
	cancel()
	select {
	case err := <-done:
		t.Fatalf("Run() returned before in-flight cron job drained: %v", err)
	case <-time.After(25 * time.Millisecond):
	}
	close(release)
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Run() error = %v, want context.Canceled", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Run() did not return after in-flight cron job drained")
	}
}

type fakeCronRunner struct {
	mu        sync.Mutex
	job       cron.Job
	accepting bool
	jobs      sync.WaitGroup
	started   chan struct{}
	stopCalls atomic.Int32
	startOnce sync.Once
	stopOnce  sync.Once
	stopHook  func()
}

func newFakeCronRunner() *fakeCronRunner {
	return &fakeCronRunner{started: make(chan struct{}), accepting: true}
}

func (runner *fakeCronRunner) factory(job cron.Job) (cronRunner, error) {
	runner.job = job
	return runner, nil
}

func (runner *fakeCronRunner) Start() {
	runner.startOnce.Do(func() { close(runner.started) })
}

func (runner *fakeCronRunner) Stop() context.Context {
	runner.stopCalls.Add(1)
	runner.stopOnce.Do(func() {
		runner.mu.Lock()
		runner.accepting = false
		hook := runner.stopHook
		runner.mu.Unlock()
		if hook != nil {
			hook()
		}
	})
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		runner.jobs.Wait()
		cancel()
	}()
	return ctx
}

func (runner *fakeCronRunner) waitStarted(t *testing.T, done <-chan error) {
	t.Helper()
	select {
	case <-runner.started:
	case err := <-done:
		t.Fatalf("Run() exited before cron start: %v", err)
	case <-time.After(time.Second):
		t.Fatal("cron runner did not start")
	}
}

func (runner *fakeCronRunner) trigger(t *testing.T) {
	t.Helper()
	runner.mu.Lock()
	if !runner.accepting || runner.job == nil {
		runner.mu.Unlock()
		t.Fatal("cron runner cannot accept trigger")
	}
	runner.jobs.Add(1)
	runner.mu.Unlock()
	go func() {
		defer runner.jobs.Done()
		runner.job.Run()
	}()
}

type systemProbeResult struct {
	value SystemSnapshot
	err   error
}

type sequenceSystemProbe struct {
	mu      sync.Mutex
	results []systemProbeResult
	calls   int
}

type panicAfterFirstSystemProbe struct {
	calls atomic.Int32
}

func (probe *panicAfterFirstSystemProbe) Snapshot(ctx context.Context) (SystemSnapshot, error) {
	if err := ctx.Err(); err != nil {
		return SystemSnapshot{}, err
	}
	if probe.calls.Add(1) == 1 {
		return SystemSnapshot{}, nil
	}
	panic("sensitive cron panic payload")
}

func initializeCronLifecycle(t *testing.T, repository *store.Repository) {
	t.Helper()
	_, err := repository.InitializeSchedulerLifecycle(context.Background(), store.SchedulerLifecycle{
		HomeGeneration: 1, UserPauseScope: store.LifecyclePauseNone,
		SystemState: store.LifecycleSystemAwake, Transition: store.LifecycleTransitionSteady,
		SourceState: store.LifecycleSourceAvailable, LastEventID: "cron-test-initialize",
		Revision: 1, UpdatedAtMS: 1,
	})
	if err != nil {
		t.Fatalf("InitializeSchedulerLifecycle() error = %v", err)
	}
}

func (probe *sequenceSystemProbe) Snapshot(ctx context.Context) (SystemSnapshot, error) {
	if err := ctx.Err(); err != nil {
		return SystemSnapshot{}, err
	}
	probe.mu.Lock()
	defer probe.mu.Unlock()
	probe.calls++
	if len(probe.results) == 0 {
		return SystemSnapshot{}, errors.New("system probe result exhausted")
	}
	result := probe.results[0]
	probe.results = probe.results[1:]
	return result.value, result.err
}

func (probe *sequenceSystemProbe) callCount() int {
	probe.mu.Lock()
	defer probe.mu.Unlock()
	return probe.calls
}
