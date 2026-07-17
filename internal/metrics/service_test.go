package metrics

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/robfig/cron/v3"
)

func TestDefaultMetricsCronRunnerUsesNormalAndDetailedSchedules(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name string
		mode SamplingMode
		want time.Duration
	}{{"normal", SamplingModeNormal, 30 * time.Second}, {"detailed", SamplingModeDetailed, 5 * time.Second}} {
		t.Run(test.name, func(t *testing.T) {
			runner, err := defaultMetricsCronRunnerFactory(test.mode, cron.FuncJob(func() {}))
			if err != nil {
				t.Fatalf("defaultMetricsCronRunnerFactory() error = %v", err)
			}
			concrete, ok := runner.(*defaultMetricsCronRunner)
			if !ok {
				t.Fatalf("runner type = %T", runner)
			}
			entries := concrete.cron.Entries()
			if len(entries) != 1 {
				t.Fatalf("entries = %d", len(entries))
			}
			base := time.Unix(100, 0).UTC()
			if delay := entries[0].Schedule.Next(base).Sub(base); delay != test.want {
				t.Fatalf("delay = %s, want %s", delay, test.want)
			}
		})
	}
}

func TestServiceRunCollectsImmediatelyAndOnCronWithoutFailingOnSampleErrors(t *testing.T) {
	t.Parallel()

	collector := &collectStub{errors: []error{errors.New("initial unavailable"), nil}}
	runner := newMetricsCronRunnerStub()
	service, err := NewService(ServiceConfig{Collector: collector, Mode: SamplingModeNormal})
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	service.newCronRunner = runner.factory
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- service.Run(ctx) }()
	runner.waitStarted(t, done)
	if collector.calls.Load() != 1 {
		t.Fatalf("initial calls = %d", collector.calls.Load())
	}
	if err := service.SetMode(SamplingModeDetailed); err != nil {
		t.Fatalf("SetMode() error = %v", err)
	}
	if got := runner.modesSnapshot(); len(got) != 2 || got[0] != SamplingModeNormal || got[1] != SamplingModeDetailed {
		t.Fatalf("runner modes = %v", got)
	}
	runner.trigger(t)
	if collector.calls.Load() != 2 {
		t.Fatalf("triggered calls = %d", collector.calls.Load())
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Run() error = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Run() did not stop")
	}
}

func TestServiceCronRecoversCollectorPanicAndSkipsOverlap(t *testing.T) {
	t.Parallel()

	collector := &overlapCollectStub{entered: make(chan struct{}), release: make(chan struct{})}
	service, err := NewService(ServiceConfig{Collector: collector, Mode: SamplingModeDetailed})
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	runner, err := defaultMetricsCronRunnerFactory(SamplingModeDetailed, cron.FuncJob(func() {
		service.collectSafely(context.Background())
	}))
	if err != nil {
		t.Fatalf("defaultMetricsCronRunnerFactory() error = %v", err)
	}
	concrete := runner.(*defaultMetricsCronRunner)
	entry := concrete.cron.Entries()[0]
	entry.WrappedJob.Run()

	firstDone := make(chan struct{})
	go func() {
		entry.WrappedJob.Run()
		close(firstDone)
	}()
	select {
	case <-collector.entered:
	case <-time.After(time.Second):
		t.Fatal("long sample did not start")
	}
	if err := concrete.SetMode(SamplingModeNormal); err != nil {
		t.Fatalf("SetMode() error = %v", err)
	}
	concrete.cron.Entries()[0].WrappedJob.Run()
	if collector.calls.Load() != 2 || collector.dropped.Load() != 1 {
		t.Fatalf("calls = %d, dropped = %d, overlapping sample was not accounted",
			collector.calls.Load(), collector.dropped.Load())
	}
	close(collector.release)
	<-firstDone
}

// 测试 Run 取消后会等待在途 cron job 返回，再完成 service Close。
func TestServiceRunWaitsForInflightCollectionBeforeReturning(t *testing.T) {
	t.Parallel()

	entered := make(chan struct{})
	release := make(chan struct{})
	var calls atomic.Int32
	collector := collectFunc(func(context.Context) error {
		if calls.Add(1) == 1 {
			return nil
		}
		close(entered)
		<-release
		return nil
	})
	runner := newMetricsCronRunnerStub()
	service, err := NewService(ServiceConfig{Collector: collector, Mode: SamplingModeNormal})
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	service.newCronRunner = runner.factory
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- service.Run(ctx) }()
	runner.waitStarted(t, done)
	jobDone := runner.triggerAsync()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("in-flight collection did not start")
	}
	cancel()
	select {
	case err := <-done:
		t.Fatalf("Run() returned before in-flight collection: %v", err)
	case <-time.After(20 * time.Millisecond):
	}
	close(release)
	<-jobDone
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Run() error = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Run() did not return after in-flight collection")
	}
}

// 测试 mode 切换与 Close 并发时保持可重复、无 panic；race gate 负责数据竞争证明。
func TestServiceSetModeConcurrentWithClose(t *testing.T) {
	t.Parallel()

	runner := newMetricsCronRunnerStub()
	service, err := NewService(ServiceConfig{Collector: &collectStub{}, Mode: SamplingModeNormal})
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	service.newCronRunner = runner.factory
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- service.Run(ctx) }()
	runner.waitStarted(t, done)

	switchDone := make(chan struct{})
	go func() {
		defer close(switchDone)
		for index := 0; index < 100; index++ {
			mode := SamplingModeNormal
			if index%2 == 0 {
				mode = SamplingModeDetailed
			}
			if err := service.SetMode(mode); err != nil {
				return
			}
		}
	}()
	cancel()
	<-switchDone
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Run() error = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Run() did not stop")
	}
}

type collectStub struct {
	errors []error
	calls  atomic.Int32
}

func (stub *collectStub) Collect(context.Context) error {
	index := int(stub.calls.Add(1) - 1)
	if index < len(stub.errors) {
		return stub.errors[index]
	}
	return nil
}

type collectFunc func(context.Context) error

func (function collectFunc) Collect(ctx context.Context) error { return function(ctx) }

type overlapCollectStub struct {
	calls   atomic.Int32
	dropped atomic.Int32
	entered chan struct{}
	release chan struct{}
}

func (stub *overlapCollectStub) Collect(context.Context) error {
	switch stub.calls.Add(1) {
	case 1:
		panic("sample panic")
	default:
		close(stub.entered)
		<-stub.release
		return nil
	}
}

func (stub *overlapCollectStub) RecordDroppedSample() { stub.dropped.Add(1) }

type metricsCronRunnerStub struct {
	mu        sync.Mutex
	job       cron.Job
	modes     []SamplingMode
	started   chan struct{}
	startOnce sync.Once
	jobs      sync.WaitGroup
}

func newMetricsCronRunnerStub() *metricsCronRunnerStub {
	return &metricsCronRunnerStub{started: make(chan struct{})}
}

func (runner *metricsCronRunnerStub) factory(mode SamplingMode, job cron.Job) (metricsCronRunner, error) {
	runner.mu.Lock()
	runner.job = job
	runner.modes = append(runner.modes, mode)
	runner.mu.Unlock()
	return runner, nil
}

func (runner *metricsCronRunnerStub) SetMode(mode SamplingMode) error {
	runner.mu.Lock()
	defer runner.mu.Unlock()
	runner.modes = append(runner.modes, mode)
	return nil
}

func (runner *metricsCronRunnerStub) modesSnapshot() []SamplingMode {
	runner.mu.Lock()
	defer runner.mu.Unlock()
	return append([]SamplingMode(nil), runner.modes...)
}

func (runner *metricsCronRunnerStub) Start() { runner.startOnce.Do(func() { close(runner.started) }) }

func (runner *metricsCronRunnerStub) Stop() context.Context {
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		runner.jobs.Wait()
		cancel()
	}()
	return ctx
}

func (runner *metricsCronRunnerStub) trigger(t *testing.T) {
	t.Helper()
	runner.jobs.Add(1)
	runner.job.Run()
	runner.jobs.Done()
}

func (runner *metricsCronRunnerStub) triggerAsync() <-chan struct{} {
	done := make(chan struct{})
	runner.jobs.Add(1)
	go func() {
		defer close(done)
		defer runner.jobs.Done()
		runner.job.Run()
	}()
	return done
}

func (runner *metricsCronRunnerStub) waitStarted(t *testing.T, done <-chan error) {
	t.Helper()
	select {
	case <-runner.started:
	case err := <-done:
		t.Fatalf("Run() exited early: %v", err)
	case <-time.After(time.Second):
		t.Fatal("metrics cron did not start")
	}
}
