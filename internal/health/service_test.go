package health

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/robfig/cron/v3"

	"github.com/SisyphusSQ/codex-pulse/internal/store"
)

func TestDefaultCronRunnerUsesThirtySecondSchedule(t *testing.T) {
	t.Parallel()
	runner, err := defaultCronRunnerFactory(cron.FuncJob(func() {}))
	if err != nil {
		t.Fatalf("defaultCronRunnerFactory() error = %v", err)
	}
	concrete := runner.(*defaultCronRunner)
	entries := concrete.cron.Entries()
	if len(entries) != 1 {
		t.Fatalf("entries = %d", len(entries))
	}
	base := time.Unix(100, 0).UTC()
	if delay := entries[0].Schedule.Next(base).Sub(base); delay != 30*time.Second {
		t.Fatalf("delay = %s, want 30s", delay)
	}
}

func TestServiceRunsImmediatelyAndKeepsLastProjectionAcrossFailures(t *testing.T) {
	t.Parallel()
	now := time.UnixMilli(store.MetricsSnapshotWindowMS + 1_000)
	healthy := healthyInput().Snapshot
	healthy.Metrics.RuntimeSamples[0].CapturedAtMS = now.UnixMilli()
	source := &snapshotStub{responses: []snapshotResponse{
		{err: errors.New("snapshot unavailable")},
		{snapshot: healthy},
		{snapshot: healthy},
	}}
	sink := &batchSinkStub{}
	runner := newCronRunnerStub()
	service, err := NewService(ServiceConfig{
		Source: source, Sink: sink, Evaluator: mustEvaluator(t), Updater: UpdaterCurrent,
		Clock: func() time.Time { return now },
	})
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	service.newCronRunner = runner.factory
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- service.Run(ctx) }()
	runner.waitStarted(t, done)
	first := service.Projection()
	if first.HasValue || !first.Stale || first.Failure != FailureSnapshot {
		t.Fatalf("initial failed projection = %#v", first)
	}

	runner.trigger(t)
	success := service.Projection()
	if !success.HasValue || success.Stale || success.Failure != FailureNone || success.Result.Level != LevelHealthy ||
		len(sink.batchesSnapshot()) != 1 || len(source.managedSnapshot()) != len(service.evaluator.ManagedEvents()) {
		t.Fatalf("successful projection = %#v; batches=%#v", success, sink.batchesSnapshot())
	}
	sink.err = errors.New("persist unavailable")
	runner.trigger(t)
	failed := service.Projection()
	if !failed.HasValue || !failed.Stale || failed.Failure != FailurePersist ||
		!reflect.DeepEqual(failed.Result, success.Result) {
		t.Fatalf("projection after persistence failure = %#v, want retained %#v", failed, success)
	}
	cancel()
	if err := waitServiceDone(t, done); !errors.Is(err, context.Canceled) {
		t.Fatalf("Run() error = %v", err)
	}
}

func TestServiceRecoversPanicAndSkipsOverlappingEvaluation(t *testing.T) {
	t.Parallel()
	source := &blockingSnapshotStub{entered: make(chan struct{}), release: make(chan struct{})}
	service, err := NewService(ServiceConfig{
		Source: source, Sink: &batchSinkStub{}, Evaluator: mustEvaluator(t), Updater: UpdaterCurrent,
		Clock: func() time.Time { return time.UnixMilli(store.MetricsSnapshotWindowMS + 1_000) },
	})
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	firstDone := make(chan struct{})
	go func() {
		service.evaluateSafely(context.Background())
		close(firstDone)
	}()
	select {
	case <-source.entered:
	case <-time.After(time.Second):
		t.Fatal("evaluation did not enter source")
	}
	service.evaluateSafely(context.Background())
	if source.calls.Load() != 1 || service.SkippedEvaluations() != 1 {
		t.Fatalf("calls=%d skipped=%d", source.calls.Load(), service.SkippedEvaluations())
	}
	close(source.release)
	<-firstDone

	panicSource := snapshotSourceFunc(func(context.Context, store.MetricsSnapshotFilter) (store.HealthEvaluationSnapshot, error) {
		panic("synthetic secret panic")
	})
	service.source = panicSource
	service.evaluateSafely(context.Background())
	projection := service.Projection()
	if !projection.Stale || projection.Failure != FailurePanic {
		t.Fatalf("panic projection = %#v", projection)
	}
}

func TestServiceRunWaitsForInflightEvaluation(t *testing.T) {
	t.Parallel()
	source := &blockingSnapshotStub{entered: make(chan struct{}), release: make(chan struct{})}
	runner := newCronRunnerStub()
	service, err := NewService(ServiceConfig{
		Source: source, Sink: &batchSinkStub{}, Evaluator: mustEvaluator(t), Updater: UpdaterCurrent,
		Clock: func() time.Time { return time.UnixMilli(store.MetricsSnapshotWindowMS + 1_000) },
	})
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	service.newCronRunner = runner.factory
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- service.Run(ctx) }()
	select {
	case <-source.entered:
	case <-time.After(time.Second):
		t.Fatal("immediate evaluation did not enter")
	}
	cancel()
	select {
	case err := <-done:
		t.Fatalf("Run() returned before evaluation drained: %v", err)
	case <-time.After(20 * time.Millisecond):
	}
	close(source.release)
	if err := waitServiceDone(t, done); !errors.Is(err, context.Canceled) {
		t.Fatalf("Run() error = %v", err)
	}
}

type snapshotResponse struct {
	snapshot store.HealthEvaluationSnapshot
	err      error
}

type snapshotStub struct {
	mu        sync.Mutex
	responses []snapshotResponse
	calls     int
	managed   []store.HealthManagedEvent
}

func (stub *snapshotStub) HealthEvaluationSnapshot(
	_ context.Context, _ store.MetricsSnapshotFilter, managed []store.HealthManagedEvent,
) (store.HealthEvaluationSnapshot, error) {
	stub.mu.Lock()
	defer stub.mu.Unlock()
	stub.managed = append([]store.HealthManagedEvent(nil), managed...)
	index := stub.calls
	stub.calls++
	if index >= len(stub.responses) {
		index = len(stub.responses) - 1
	}
	return stub.responses[index].snapshot, stub.responses[index].err
}

func (stub *snapshotStub) managedSnapshot() []store.HealthManagedEvent {
	stub.mu.Lock()
	defer stub.mu.Unlock()
	return append([]store.HealthManagedEvent(nil), stub.managed...)
}

type blockingSnapshotStub struct {
	calls   atomic.Int32
	entered chan struct{}
	release chan struct{}
}

func (stub *blockingSnapshotStub) HealthEvaluationSnapshot(
	context.Context, store.MetricsSnapshotFilter, []store.HealthManagedEvent,
) (store.HealthEvaluationSnapshot, error) {
	if stub.calls.Add(1) == 1 {
		close(stub.entered)
		<-stub.release
	}
	return healthyInput().Snapshot, nil
}

type snapshotSourceFunc func(context.Context, store.MetricsSnapshotFilter) (store.HealthEvaluationSnapshot, error)

func (function snapshotSourceFunc) HealthEvaluationSnapshot(
	ctx context.Context, filter store.MetricsSnapshotFilter, _ []store.HealthManagedEvent,
) (store.HealthEvaluationSnapshot, error) {
	return function(ctx, filter)
}

type batchSinkStub struct {
	mu      sync.Mutex
	batches []store.HealthEvaluationBatch
	err     error
}

func (stub *batchSinkStub) ApplyHealthEvaluationBatch(_ context.Context, batch store.HealthEvaluationBatch) error {
	stub.mu.Lock()
	defer stub.mu.Unlock()
	if stub.err != nil {
		return stub.err
	}
	stub.batches = append(stub.batches, batch)
	return nil
}

func (stub *batchSinkStub) batchesSnapshot() []store.HealthEvaluationBatch {
	stub.mu.Lock()
	defer stub.mu.Unlock()
	return append([]store.HealthEvaluationBatch(nil), stub.batches...)
}

func mustEvaluator(t *testing.T) *Evaluator {
	t.Helper()
	evaluator, err := NewEvaluator(DefaultThresholds())
	if err != nil {
		t.Fatal(err)
	}
	return evaluator
}

type cronRunnerStub struct {
	mu      sync.Mutex
	job     cron.Job
	started chan struct{}
	jobs    sync.WaitGroup
}

func newCronRunnerStub() *cronRunnerStub { return &cronRunnerStub{started: make(chan struct{})} }

func (runner *cronRunnerStub) factory(job cron.Job) (cronRunner, error) {
	runner.mu.Lock()
	runner.job = job
	runner.mu.Unlock()
	return runner, nil
}

func (runner *cronRunnerStub) Start() { close(runner.started) }

func (runner *cronRunnerStub) Stop() context.Context {
	done := make(chan struct{})
	go func() {
		runner.jobs.Wait()
		close(done)
	}()
	return channelContext{done: done}
}

func (runner *cronRunnerStub) trigger(t *testing.T) {
	t.Helper()
	runner.jobs.Add(1)
	done := make(chan struct{})
	go func() {
		defer runner.jobs.Done()
		runner.mu.Lock()
		job := runner.job
		runner.mu.Unlock()
		job.Run()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("cron job did not finish")
	}
}

func (runner *cronRunnerStub) waitStarted(t *testing.T, done <-chan error) {
	t.Helper()
	select {
	case <-runner.started:
	case err := <-done:
		t.Fatalf("Run() returned before start: %v", err)
	case <-time.After(time.Second):
		t.Fatal("cron runner did not start")
	}
}

type channelContext struct{ done <-chan struct{} }

func (ctx channelContext) Deadline() (time.Time, bool) { return time.Time{}, false }
func (ctx channelContext) Done() <-chan struct{}       { return ctx.done }
func (ctx channelContext) Err() error {
	select {
	case <-ctx.done:
		return context.Canceled
	default:
		return nil
	}
}
func (ctx channelContext) Value(any) any { return nil }

func waitServiceDone(t *testing.T, done <-chan error) error {
	t.Helper()
	select {
	case err := <-done:
		return err
	case <-time.After(time.Second):
		t.Fatal("service did not stop")
		return nil
	}
}
