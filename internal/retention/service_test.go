package retention

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/robfig/cron/v3"

	"github.com/SisyphusSQ/codex-pulse/internal/store"
	storesqlite "github.com/SisyphusSQ/codex-pulse/internal/store/sqlite"
)

func TestServiceRunsStartupThenHonorsHourlySuccessDue(t *testing.T) {
	clock := newTestClock(time.UnixMilli(200_000_000).UTC())
	cleaner := &fakeCleaner{reports: []store.RetentionCleanupReport{{
		CutoffMS: 1, Batches: 2, Deleted: store.RetentionDeletedCounts{RuntimeSamples: 3, HealthEvents: 1},
	}}}
	checkpointer := &fakeCheckpointer{reports: []storesqlite.WALCheckpointReport{{LogFrames: 8, CheckpointedFrames: 8}}}
	service := newTestService(t, clock.Now, cleaner, checkpointer)

	service.runIfDue(context.Background(), true)
	projection := service.Projection()
	if projection.State != StateSucceeded || projection.Failure != FailureNone || projection.ConsecutiveFailures != 0 {
		t.Fatalf("startup projection = %#v, want succeeded", projection)
	}
	if projection.Attempt.Deleted.RuntimeSamples != 3 || projection.Attempt.Deleted.HealthEvents != 1 ||
		projection.Attempt.Checkpoint.LogFrames != 8 || !projection.Attempt.CheckpointCompleted {
		t.Fatalf("startup attempt = %#v, want cleanup and checkpoint evidence", projection.Attempt)
	}
	if projection.LastSuccess == nil || *projection.LastSuccess != projection.Attempt {
		t.Fatalf("last success = %#v, want startup attempt", projection.LastSuccess)
	}

	clock.Advance(successInterval - time.Millisecond)
	service.runIfDue(context.Background(), false)
	if cleaner.Calls() != 1 {
		t.Fatalf("cleanup calls before due = %d, want 1", cleaner.Calls())
	}
	clock.Advance(time.Millisecond)
	service.runIfDue(context.Background(), false)
	if cleaner.Calls() != 2 || checkpointer.Calls() != 2 {
		t.Fatalf("calls at due = cleanup %d checkpoint %d, want 2/2", cleaner.Calls(), checkpointer.Calls())
	}
}

func TestServiceBacksOffFailuresAndPreservesLastSuccess(t *testing.T) {
	clock := newTestClock(time.UnixMilli(200_000_000).UTC())
	cleaner := &fakeCleaner{
		reports: []store.RetentionCleanupReport{
			{Deleted: store.RetentionDeletedCounts{RuntimeSamples: 1}},
			{Deleted: store.RetentionDeletedCounts{RuntimeSamples: 2}},
			{Deleted: store.RetentionDeletedCounts{RuntimeSamples: 3}},
		},
		errors: []error{nil, errors.New("cleanup failed"), nil},
	}
	checkpointer := &fakeCheckpointer{reports: []storesqlite.WALCheckpointReport{{LogFrames: 1, CheckpointedFrames: 1}, {LogFrames: 2, CheckpointedFrames: 2}}}
	service := newTestService(t, clock.Now, cleaner, checkpointer)

	service.runIfDue(context.Background(), true)
	lastSuccess := *service.Projection().LastSuccess
	clock.Advance(successInterval)
	service.runIfDue(context.Background(), false)
	failed := service.Projection()
	if failed.State != StateFailed || failed.Failure != FailureCleanup || failed.ConsecutiveFailures != 1 {
		t.Fatalf("failed projection = %#v, want cleanup failure", failed)
	}
	if failed.NextDueAtMS != clock.Now().Add(failureBackoffs[0]).UnixMilli() {
		t.Fatalf("next due = %d, want first backoff", failed.NextDueAtMS)
	}
	if failed.LastSuccess == nil || *failed.LastSuccess != lastSuccess {
		t.Fatalf("last success changed on failure: %#v", failed.LastSuccess)
	}

	clock.Advance(failureBackoffs[0] - time.Millisecond)
	service.runIfDue(context.Background(), false)
	if cleaner.Calls() != 2 {
		t.Fatalf("cleanup calls before retry due = %d, want 2", cleaner.Calls())
	}
	clock.Advance(time.Millisecond)
	service.runIfDue(context.Background(), false)
	recovered := service.Projection()
	if recovered.State != StateSucceeded || recovered.ConsecutiveFailures != 0 || cleaner.Calls() != 3 {
		t.Fatalf("recovered projection = %#v, calls=%d", recovered, cleaner.Calls())
	}
}

func TestServiceClassifiesCheckpointFailureAndDependencyPanic(t *testing.T) {
	clock := newTestClock(time.UnixMilli(200_000_000).UTC())
	cleaner := &fakeCleaner{reports: []store.RetentionCleanupReport{{Deleted: store.RetentionDeletedCounts{JobRuns: 2}}, {}}}
	checkpointer := &fakeCheckpointer{errors: []error{errors.New("checkpoint failed")}, panicAt: 2}
	service := newTestService(t, clock.Now, cleaner, checkpointer)

	service.runIfDue(context.Background(), true)
	checkpointFailure := service.Projection()
	if checkpointFailure.Failure != FailureCheckpoint || checkpointFailure.Attempt.Deleted.JobRuns != 2 || checkpointFailure.Attempt.CheckpointCompleted {
		t.Fatalf("checkpoint failure projection = %#v", checkpointFailure)
	}
	clock.Advance(failureBackoffs[0])
	service.runIfDue(context.Background(), false)
	panicFailure := service.Projection()
	if panicFailure.Failure != FailurePanic || panicFailure.ConsecutiveFailures != 2 {
		t.Fatalf("panic projection = %#v, want finite panic and second failure", panicFailure)
	}
}

func TestServiceFailureBackoffSequenceCapsAtOneHour(t *testing.T) {
	clock := newTestClock(time.UnixMilli(200_000_000).UTC())
	failure := errors.New("synthetic cleanup failure")
	cleaner := &fakeCleaner{errors: []error{failure, failure, failure, failure, failure, failure, failure}}
	service := newTestService(t, clock.Now, cleaner, &fakeCheckpointer{})

	for index := range 7 {
		service.runIfDue(context.Background(), true)
		projection := service.Projection()
		wantBackoff := failureBackoffs[min(index, len(failureBackoffs)-1)]
		if projection.ConsecutiveFailures != index+1 ||
			projection.NextDueAtMS != clock.Now().Add(wantBackoff).UnixMilli() {
			t.Fatalf("failure %d projection = %#v, want backoff %s", index+1, projection, wantBackoff)
		}
	}
}

func TestServiceSkipsOverlappingRun(t *testing.T) {
	clock := newTestClock(time.UnixMilli(200_000_000).UTC())
	started := make(chan struct{})
	release := make(chan struct{})
	cleaner := &fakeCleaner{started: started, release: release}
	service := newTestService(t, clock.Now, cleaner, &fakeCheckpointer{})

	done := make(chan struct{})
	go func() {
		service.runIfDue(context.Background(), true)
		close(done)
	}()
	<-started
	service.runIfDue(context.Background(), true)
	if projection := service.Projection(); projection.SkippedRuns != 1 || projection.State != StateRunning {
		t.Fatalf("overlap projection = %#v, want one skip and running", projection)
	}
	close(release)
	<-done
}

func TestServiceClaimsOverlapGateBeforeReadingDueClock(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	var calls atomic.Int64
	clock := func() time.Time {
		if calls.Add(1) == 1 {
			close(started)
			<-release
		}
		return time.UnixMilli(200_000_000).UTC()
	}
	service := newTestService(t, clock, &fakeCleaner{}, &fakeCheckpointer{})

	firstDone := make(chan struct{})
	go func() {
		service.runIfDue(context.Background(), true)
		close(firstDone)
	}()
	<-started
	secondDone := make(chan struct{})
	go func() {
		service.runIfDue(context.Background(), true)
		close(secondDone)
	}()
	select {
	case <-secondDone:
	case <-time.After(time.Second):
		close(release)
		t.Fatal("overlapping contender read the Clock before being skipped")
	}
	if calls.Load() != 1 || service.Projection().SkippedRuns != 1 {
		close(release)
		t.Fatalf("clock calls/skips = %d/%d, want 1/1", calls.Load(), service.Projection().SkippedRuns)
	}
	close(release)
	<-firstDone
}

func TestServiceContainsClockPanicAtStartAndFinish(t *testing.T) {
	start := time.UnixMilli(200_000_000).UTC()
	tests := []struct {
		name        string
		clock       func() time.Time
		wantCleanup int
	}{
		{
			name: "startup clock",
			clock: func() time.Time {
				panic("synthetic startup clock panic")
			},
		},
		{
			name: "finish clock", clock: sequenceTestClock([]time.Time{start}, 2), wantCleanup: 1,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			cleaner := &fakeCleaner{}
			service := newTestService(t, test.clock, cleaner, &fakeCheckpointer{})
			service.fallbackClock = func() time.Time { return start }
			service.runIfDue(context.Background(), true)
			projection := service.Projection()
			if projection.State != StateFailed || projection.Failure != FailurePanic ||
				projection.ConsecutiveFailures != 1 || projection.Attempt.FinishedAtMS < projection.Attempt.StartedAtMS ||
				projection.NextDueAtMS < projection.Attempt.FinishedAtMS+failureBackoffs[0].Milliseconds() {
				t.Fatalf("clock panic projection = %#v", projection)
			}
			if cleaner.Calls() != test.wantCleanup || service.active.Load() {
				t.Fatalf("cleanup calls/active = %d/%t, want %d/false", cleaner.Calls(), service.active.Load(), test.wantCleanup)
			}
		})
	}
}

func TestServiceClampsWallClockRollbackBeforeComputingDue(t *testing.T) {
	start := time.UnixMilli(200_000_000).UTC()
	for _, test := range []struct {
		name        string
		cleanupErr  error
		wantState   State
		wantFailure Failure
		wantDelay   time.Duration
	}{
		{name: "success", wantState: StateSucceeded, wantFailure: FailureNone, wantDelay: successInterval},
		{name: "failure", cleanupErr: errors.New("synthetic cleanup failure"), wantState: StateFailed, wantFailure: FailureCleanup, wantDelay: failureBackoffs[0]},
	} {
		t.Run(test.name, func(t *testing.T) {
			cleaner := &fakeCleaner{errors: []error{test.cleanupErr}}
			service := newTestService(t, sequenceTestClock([]time.Time{start, start.Add(-2 * time.Hour)}, 0), cleaner, &fakeCheckpointer{})
			service.runIfDue(context.Background(), true)
			projection := service.Projection()
			if projection.State != test.wantState || projection.Failure != test.wantFailure ||
				projection.Attempt.FinishedAtMS != projection.Attempt.StartedAtMS || projection.Attempt.DurationMS != 0 ||
				projection.NextDueAtMS != projection.Attempt.FinishedAtMS+test.wantDelay.Milliseconds() {
				t.Fatalf("rollback projection = %#v", projection)
			}
		})
	}
}

func TestServiceRunStartsImmediatelyUsesCronAndStopsCleanly(t *testing.T) {
	clock := newTestClock(time.UnixMilli(200_000_000).UTC())
	cleaner := &fakeCleaner{}
	service := newTestService(t, clock.Now, cleaner, &fakeCheckpointer{})
	runner := &fakeCronRunner{stopped: make(chan struct{})}
	service.newCronRunner = func(job cron.Job) (cronRunner, error) {
		runner.job = job
		return runner, nil
	}

	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() { result <- service.Run(ctx) }()
	eventually(t, func() bool { return runner.started.Load() && cleaner.Calls() == 1 })
	cancel()
	if err := <-result; !errors.Is(err, context.Canceled) {
		t.Fatalf("Run() error = %v, want context.Canceled", err)
	}
	select {
	case <-runner.stopped:
	default:
		t.Fatal("cron runner was not stopped")
	}
}

type fakeCleaner struct {
	mu      sync.Mutex
	reports []store.RetentionCleanupReport
	errors  []error
	calls   int
	started chan struct{}
	release chan struct{}
}

func (cleaner *fakeCleaner) CleanupRetention(context.Context, store.RetentionCleanupOptions) (store.RetentionCleanupReport, error) {
	cleaner.mu.Lock()
	cleaner.calls++
	call := cleaner.calls
	var report store.RetentionCleanupReport
	if call <= len(cleaner.reports) {
		report = cleaner.reports[call-1]
	}
	var err error
	if call <= len(cleaner.errors) {
		err = cleaner.errors[call-1]
	}
	started, release := cleaner.started, cleaner.release
	cleaner.mu.Unlock()
	if started != nil && call == 1 {
		close(started)
	}
	if release != nil && call == 1 {
		<-release
	}
	return report, err
}

func (cleaner *fakeCleaner) Calls() int {
	cleaner.mu.Lock()
	defer cleaner.mu.Unlock()
	return cleaner.calls
}

type fakeCheckpointer struct {
	mu      sync.Mutex
	reports []storesqlite.WALCheckpointReport
	errors  []error
	calls   int
	panicAt int
}

func (checkpointer *fakeCheckpointer) CheckpointWAL(context.Context) (storesqlite.WALCheckpointReport, error) {
	checkpointer.mu.Lock()
	defer checkpointer.mu.Unlock()
	checkpointer.calls++
	if checkpointer.calls == checkpointer.panicAt {
		panic("synthetic checkpoint panic")
	}
	var report storesqlite.WALCheckpointReport
	if checkpointer.calls <= len(checkpointer.reports) {
		report = checkpointer.reports[checkpointer.calls-1]
	}
	var err error
	if checkpointer.calls <= len(checkpointer.errors) {
		err = checkpointer.errors[checkpointer.calls-1]
	}
	return report, err
}

func (checkpointer *fakeCheckpointer) Calls() int {
	checkpointer.mu.Lock()
	defer checkpointer.mu.Unlock()
	return checkpointer.calls
}

type testClock struct{ nowMS atomic.Int64 }

func newTestClock(now time.Time) *testClock {
	clock := &testClock{}
	clock.nowMS.Store(now.UnixMilli())
	return clock
}

func (clock *testClock) Now() time.Time { return time.UnixMilli(clock.nowMS.Load()).UTC() }
func (clock *testClock) Advance(duration time.Duration) {
	clock.nowMS.Add(duration.Milliseconds())
}

func sequenceTestClock(values []time.Time, panicAt int) func() time.Time {
	var mu sync.Mutex
	call := 0
	return func() time.Time {
		mu.Lock()
		defer mu.Unlock()
		call++
		if call == panicAt {
			panic("synthetic clock panic")
		}
		if len(values) == 0 {
			return time.Time{}
		}
		return values[min(call-1, len(values)-1)]
	}
}

type fakeCronRunner struct {
	job     cron.Job
	started atomic.Bool
	stopped chan struct{}
}

func (runner *fakeCronRunner) Start() { runner.started.Store(true) }
func (runner *fakeCronRunner) Stop() context.Context {
	select {
	case <-runner.stopped:
	default:
		close(runner.stopped)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	return ctx
}

func newTestService(
	t *testing.T,
	clock func() time.Time,
	cleaner Cleaner,
	checkpointer Checkpointer,
) *Service {
	t.Helper()
	service, err := NewService(ServiceConfig{Cleaner: cleaner, Checkpointer: checkpointer, Clock: clock})
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	return service
}

func eventually(t *testing.T, predicate func() bool) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if predicate() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("condition did not become true")
}
