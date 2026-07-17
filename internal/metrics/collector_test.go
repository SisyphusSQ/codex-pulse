package metrics

import (
	"context"
	"errors"
	"math"
	"sync/atomic"
	"testing"
	"time"

	"github.com/SisyphusSQ/codex-pulse/internal/store"
)

// 测试 QueryAccumulator 在失败 restore 后合并新观测且保持 count/total/max。
func TestQueryAccumulatorDrainRestorePreservesConcurrentObservations(t *testing.T) {
	t.Parallel()

	accumulator := &QueryAccumulator{}
	accumulator.Observe(400 * time.Microsecond)
	accumulator.Observe(600 * time.Microsecond)
	drained := accumulator.Drain()
	accumulator.Observe(300 * time.Microsecond)
	accumulator.Restore(drained)
	merged := accumulator.Drain()
	if merged.Count != 3 || merged.TotalMicros != 1_300 || merged.MaxMicros != 600 {
		t.Fatalf("merged query aggregate = %#v", merged)
	}
}

// 测试 QueryAccumulator 在极端累计值下饱和而不发生整数回绕。
func TestQueryAccumulatorSaturatesCountsAndLatency(t *testing.T) {
	t.Parallel()

	accumulator := &QueryAccumulator{aggregate: QueryAggregate{
		Count: math.MaxInt64, TotalMicros: math.MaxInt64, MaxMicros: math.MaxInt64,
	}}
	accumulator.Observe(time.Microsecond)
	accumulator.Restore(QueryAggregate{Count: 1, TotalMicros: 1, MaxMicros: 1})
	got := accumulator.Drain()
	if got.Count != math.MaxInt64 || got.TotalMicros != math.MaxInt64 || got.MaxMicros != math.MaxInt64 {
		t.Fatalf("saturated aggregate = %#v", got)
	}
}

// 测试 Collector 记录真实 CPU delta、RSS peak、storage/queue 和 query 聚合。
func TestCollectorBuildsAppRuntimeSamplesFromTypedProbes(t *testing.T) {
	t.Parallel()

	base := time.UnixMilli(10_000)
	clock := &sequenceClock{values: []time.Time{
		base, base.Add(250 * time.Microsecond),
		base.Add(30 * time.Second), base.Add(30*time.Second + 500*time.Microsecond),
	}}
	process := &processProbeStub{values: []ProcessMeasurement{
		{CPUUserMS: 100, CPUSystemMS: 50, RSSBytes: 64 << 20},
		{CPUUserMS: 400, CPUSystemMS: 150, RSSBytes: 80 << 20},
	}}
	storage := StoreMeasurement{
		DBBytes: 4 << 20, WALBytes: 512 << 10, DiskFreeBytes: 10 << 30,
		LiveQueueDepth: 2, BackfillQueueDepth: 3,
		OldestLiveWaitMS: 20, OldestBackfillWaitMS: 40,
	}
	sink := &sampleSinkStub{}
	queries := &QueryAccumulator{}
	collector, err := NewCollector(CollectorConfig{
		Process: process, Store: storeProbeStub{value: storage}, Sink: sink,
		Queries: queries, Clock: clock.Now, GoroutineCount: func() int { return 11 },
	})
	if err != nil {
		t.Fatalf("NewCollector() error = %v", err)
	}
	queries.Observe(700 * time.Microsecond)
	queries.Observe(300 * time.Microsecond)
	if err := collector.Collect(t.Context()); err != nil {
		t.Fatalf("Collect(first) error = %v", err)
	}
	if err := collector.Collect(t.Context()); err != nil {
		t.Fatalf("Collect(second) error = %v", err)
	}
	if len(sink.samples) != 2 {
		t.Fatalf("samples = %#v", sink.samples)
	}
	first, second := sink.samples[0], sink.samples[1]
	if first.CapturedAtMS != 10_000 || first.CPUPercent != 0 ||
		first.CPUUserMS != 100 || first.CPUSystemMS != 50 ||
		first.RSSBytes != 64<<20 || first.PeakRSSBytes != 64<<20 ||
		first.GoroutineCount != 11 || first.QueryCount != 2 ||
		first.QueryTotalMicros != 1_000 || first.QueryMaxMicros != 700 ||
		first.CollectorDurationMicros != 250 || first.DroppedSamples != 0 {
		t.Fatalf("first sample = %#v", first)
	}
	wantCPU := float64(400) / float64((30 * time.Second).Milliseconds()) * 100
	if math.Abs(second.CPUPercent-wantCPU) > 0.000_001 ||
		second.RSSBytes != 80<<20 || second.PeakRSSBytes != 80<<20 ||
		second.QueryCount != 0 || second.CollectorDurationMicros != 500 ||
		second.DBBytes != storage.DBBytes || second.WALBytes != storage.WALBytes ||
		second.LiveQueueDepth != 2 || second.BackfillQueueDepth != 3 {
		t.Fatalf("second sample = %#v, want cpu %.9f", second, wantCPU)
	}
}

// 测试 sink 失败会恢复 query aggregate，并在下一次成功 sample 中报告 dropped count。
func TestCollectorRestoresQueriesAndCountsDroppedSampleAfterSinkFailure(t *testing.T) {
	t.Parallel()

	base := time.UnixMilli(20_000)
	clock := &sequenceClock{values: []time.Time{
		base, base.Add(time.Millisecond),
		base.Add(30 * time.Second), base.Add(30*time.Second + time.Millisecond),
	}}
	want := errors.New("sink unavailable")
	sink := &sampleSinkStub{errors: []error{want, nil}}
	queries := &QueryAccumulator{}
	collector, err := NewCollector(CollectorConfig{
		Process: &processProbeStub{values: []ProcessMeasurement{
			{CPUUserMS: 10, CPUSystemMS: 5, RSSBytes: 1},
			{CPUUserMS: 20, CPUSystemMS: 10, RSSBytes: 2},
		}},
		Store: storeProbeStub{value: StoreMeasurement{DiskFreeBytes: 1}},
		Sink:  sink, Queries: queries, Clock: clock.Now, GoroutineCount: func() int { return 1 },
	})
	if err != nil {
		t.Fatalf("NewCollector() error = %v", err)
	}
	queries.Observe(900 * time.Microsecond)
	if err := collector.Collect(t.Context()); !errors.Is(err, want) {
		t.Fatalf("Collect(failed sink) error = %v, want injected error", err)
	}
	queries.Observe(100 * time.Microsecond)
	if err := collector.Collect(t.Context()); err != nil {
		t.Fatalf("Collect(recovery) error = %v", err)
	}
	if len(sink.samples) != 1 || sink.samples[0].DroppedSamples != 1 ||
		sink.samples[0].QueryCount != 2 || sink.samples[0].QueryTotalMicros != 1_000 ||
		sink.samples[0].QueryMaxMicros != 900 {
		t.Fatalf("recovery samples = %#v", sink.samples)
	}
}

// 测试 probe 失败不写 partial sample，下一成功 sample 只报告缺口而不伪造失败时间点。
func TestCollectorDropsProbeFailureWithoutAdvancingCPUBaseline(t *testing.T) {
	t.Parallel()

	base := time.UnixMilli(30_000)
	clock := &sequenceClock{values: []time.Time{
		base,
		base.Add(30 * time.Second), base.Add(30*time.Second + time.Millisecond),
	}}
	want := errors.New("process probe failed")
	process := &processProbeStub{
		values: []ProcessMeasurement{{}, {CPUUserMS: 30, CPUSystemMS: 10, RSSBytes: 3}},
		errors: []error{want, nil},
	}
	sink := &sampleSinkStub{}
	collector, err := NewCollector(CollectorConfig{
		Process: process, Store: storeProbeStub{value: StoreMeasurement{DiskFreeBytes: 1}},
		Sink: sink, Queries: &QueryAccumulator{}, Clock: clock.Now,
		GoroutineCount: func() int { return 1 },
	})
	if err != nil {
		t.Fatalf("NewCollector() error = %v", err)
	}
	if err := collector.Collect(t.Context()); !errors.Is(err, want) {
		t.Fatalf("Collect(probe failure) error = %v, want injected error", err)
	}
	if err := collector.Collect(t.Context()); err != nil {
		t.Fatalf("Collect(recovery) error = %v", err)
	}
	if len(sink.samples) != 1 || sink.samples[0].CapturedAtMS != 60_000 ||
		sink.samples[0].CPUPercent != 0 || sink.samples[0].DroppedSamples != 1 {
		t.Fatalf("recovery samples = %#v", sink.samples)
	}
}

// 测试 sink panic 不会逃出 collector、不会丢失已 drain 查询，并在恢复样本中计 dropped。
func TestCollectorRestoresQueriesAndCountsDroppedSampleAfterSinkPanic(t *testing.T) {
	t.Parallel()

	base := time.UnixMilli(40_000)
	clock := &sequenceClock{values: []time.Time{
		base, base.Add(time.Millisecond),
		base.Add(30 * time.Second), base.Add(30*time.Second + time.Millisecond),
	}}
	sink := &panicOnceSink{}
	queries := &QueryAccumulator{}
	collector, err := NewCollector(CollectorConfig{
		Process: &processProbeStub{values: []ProcessMeasurement{
			{CPUUserMS: 10, CPUSystemMS: 5, RSSBytes: 1},
			{CPUUserMS: 20, CPUSystemMS: 10, RSSBytes: 2},
		}},
		Store: storeProbeStub{value: StoreMeasurement{DiskFreeBytes: 1}},
		Sink:  sink, Queries: queries, Clock: clock.Now, GoroutineCount: func() int { return 1 },
	})
	if err != nil {
		t.Fatalf("NewCollector() error = %v", err)
	}
	queries.Observe(900 * time.Microsecond)
	if err := collector.Collect(t.Context()); !errors.Is(err, ErrCollectorPanic) {
		t.Fatalf("Collect(sink panic) error = %v, want ErrCollectorPanic", err)
	}
	queries.Observe(100 * time.Microsecond)
	if err := collector.Collect(t.Context()); err != nil {
		t.Fatalf("Collect(recovery) error = %v", err)
	}
	if len(sink.samples) != 1 || sink.samples[0].DroppedSamples != 1 ||
		sink.samples[0].QueryCount != 2 || sink.samples[0].QueryTotalMicros != 1_000 ||
		sink.samples[0].QueryMaxMicros != 900 {
		t.Fatalf("recovery samples = %#v", sink.samples)
	}
}

// 测试 drain 后 clock panic 也会恢复查询批次并让下一次采集成功。
func TestCollectorRestoresQueriesAfterPostDrainClockPanic(t *testing.T) {
	t.Parallel()

	base := time.UnixMilli(50_000)
	clockCalls := 0
	clock := func() time.Time {
		clockCalls++
		switch clockCalls {
		case 1:
			return base
		case 2:
			panic("post-drain clock panic")
		case 3:
			return base.Add(30 * time.Second)
		default:
			return base.Add(30*time.Second + time.Millisecond)
		}
	}
	sink := &sampleSinkStub{}
	queries := &QueryAccumulator{}
	collector, err := NewCollector(CollectorConfig{
		Process: &processProbeStub{values: []ProcessMeasurement{
			{CPUUserMS: 10, CPUSystemMS: 5, RSSBytes: 1},
			{CPUUserMS: 20, CPUSystemMS: 10, RSSBytes: 2},
		}},
		Store: storeProbeStub{value: StoreMeasurement{DiskFreeBytes: 1}},
		Sink:  sink, Queries: queries, Clock: clock, GoroutineCount: func() int { return 1 },
	})
	if err != nil {
		t.Fatalf("NewCollector() error = %v", err)
	}
	queries.Observe(700 * time.Microsecond)
	if err := collector.Collect(t.Context()); !errors.Is(err, ErrCollectorPanic) {
		t.Fatalf("Collect(clock panic) error = %v, want ErrCollectorPanic", err)
	}
	queries.Observe(300 * time.Microsecond)
	if err := collector.Collect(t.Context()); err != nil {
		t.Fatalf("Collect(recovery) error = %v", err)
	}
	if len(sink.samples) != 1 || sink.samples[0].DroppedSamples != 1 ||
		sink.samples[0].QueryCount != 2 || sink.samples[0].QueryTotalMicros != 1_000 {
		t.Fatalf("recovery samples = %#v", sink.samples)
	}
}

// 测试 wall clock/counter 回退及 CPU delta 溢出只丢一个样本，并重建可继续采集的 baseline。
func TestCollectorRebuildsCPUBaselineAfterInvalidProgression(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		capturedAtMS []int64
		measurements []ProcessMeasurement
	}{
		{
			name: "wall clock rollback", capturedAtMS: []int64{100_000, 90_000, 91_000, 121_000},
			measurements: []ProcessMeasurement{{CPUUserMS: 100, CPUSystemMS: 50, RSSBytes: 1},
				{CPUUserMS: 120, CPUSystemMS: 60, RSSBytes: 1}, {CPUUserMS: 125, CPUSystemMS: 65, RSSBytes: 1},
				{CPUUserMS: 155, CPUSystemMS: 75, RSSBytes: 1}},
		},
		{
			name: "counter rollback", capturedAtMS: []int64{100_000, 130_000, 160_000, 190_000},
			measurements: []ProcessMeasurement{{CPUUserMS: 100, CPUSystemMS: 50, RSSBytes: 1},
				{CPUUserMS: 90, CPUSystemMS: 60, RSSBytes: 1}, {CPUUserMS: 95, CPUSystemMS: 65, RSSBytes: 1},
				{CPUUserMS: 125, CPUSystemMS: 75, RSSBytes: 1}},
		},
		{
			name: "delta overflow", capturedAtMS: []int64{100_000, 130_000, 160_000, 190_000},
			measurements: []ProcessMeasurement{{CPUUserMS: 0, CPUSystemMS: math.MaxInt64 - 1, RSSBytes: 1},
				{CPUUserMS: math.MaxInt64, CPUSystemMS: math.MaxInt64, RSSBytes: 1},
				{CPUUserMS: 5, CPUSystemMS: 3, RSSBytes: 1}, {CPUUserMS: 35, CPUSystemMS: 13, RSSBytes: 1}},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			clockValues := make([]time.Time, 0, len(test.capturedAtMS)*2)
			for _, capturedAtMS := range test.capturedAtMS {
				captured := time.UnixMilli(capturedAtMS)
				clockValues = append(clockValues, captured, captured.Add(time.Millisecond))
			}
			sink := &sampleSinkStub{}
			collector, err := NewCollector(CollectorConfig{
				Process: &processProbeStub{values: test.measurements},
				Store:   storeProbeStub{value: StoreMeasurement{DiskFreeBytes: 1}},
				Sink:    sink, Queries: &QueryAccumulator{}, Clock: (&sequenceClock{values: clockValues}).Now,
				GoroutineCount: func() int { return 1 },
			})
			if err != nil {
				t.Fatalf("NewCollector() error = %v", err)
			}
			if err := collector.Collect(t.Context()); err != nil {
				t.Fatalf("Collect(baseline) error = %v", err)
			}
			if err := collector.Collect(t.Context()); !errors.Is(err, ErrCollector) {
				t.Fatalf("Collect(invalid progression) error = %v", err)
			}
			if err := collector.Collect(t.Context()); err != nil {
				t.Fatalf("Collect(rebuilt baseline) error = %v", err)
			}
			if err := collector.Collect(t.Context()); err != nil {
				t.Fatalf("Collect(after recovery) error = %v", err)
			}
			if len(sink.samples) != 3 || sink.samples[1].CPUPercent != 0 ||
				sink.samples[1].DroppedSamples != 1 || sink.samples[2].CPUPercent <= 0 {
				t.Fatalf("recovery samples = %#v", sink.samples)
			}
		})
	}
}

func BenchmarkCollector(b *testing.B) {
	var clockCalls atomic.Int64
	base := time.UnixMilli(100_000)
	clock := func() time.Time { return base.Add(time.Duration(clockCalls.Add(1)) * time.Millisecond) }
	var processCalls atomic.Int64
	collector, err := NewCollector(CollectorConfig{
		Process: processProbeFunc(func(context.Context) (ProcessMeasurement, error) {
			value := processCalls.Add(1)
			return ProcessMeasurement{CPUUserMS: value, CPUSystemMS: value, RSSBytes: 64 << 20}, nil
		}),
		Store:   storeProbeStub{value: StoreMeasurement{DiskFreeBytes: 1}},
		Sink:    sampleSinkFunc(func(context.Context, store.AppRuntimeSample) error { return nil }),
		Queries: &QueryAccumulator{}, Clock: clock, GoroutineCount: func() int { return 8 },
	})
	if err != nil {
		b.Fatal(err)
	}
	b.ResetTimer()
	for range b.N {
		if err := collector.Collect(context.Background()); err != nil {
			b.Fatal(err)
		}
	}
}

type sequenceClock struct {
	values []time.Time
	index  int
}

func (clock *sequenceClock) Now() time.Time {
	if clock.index >= len(clock.values) {
		panic("sequence clock exhausted")
	}
	value := clock.values[clock.index]
	clock.index++
	return value
}

type processProbeStub struct {
	values []ProcessMeasurement
	errors []error
	index  int
}

type processProbeFunc func(context.Context) (ProcessMeasurement, error)

func (function processProbeFunc) Measure(ctx context.Context) (ProcessMeasurement, error) {
	return function(ctx)
}

func (probe *processProbeStub) Measure(context.Context) (ProcessMeasurement, error) {
	index := probe.index
	probe.index++
	var err error
	if index < len(probe.errors) {
		err = probe.errors[index]
	}
	return probe.values[index], err
}

type storeProbeStub struct {
	value StoreMeasurement
	err   error
}

func (probe storeProbeStub) Measure(context.Context, int64) (StoreMeasurement, error) {
	return probe.value, probe.err
}

type sampleSinkStub struct {
	samples []store.AppRuntimeSample
	errors  []error
	calls   int
}

func (sink *sampleSinkStub) RecordAppRuntimeSample(_ context.Context, value store.AppRuntimeSample) error {
	index := sink.calls
	sink.calls++
	var err error
	if index < len(sink.errors) {
		err = sink.errors[index]
	}
	if err == nil {
		sink.samples = append(sink.samples, value)
	}
	return err
}

type sampleSinkFunc func(context.Context, store.AppRuntimeSample) error

func (function sampleSinkFunc) RecordAppRuntimeSample(ctx context.Context, value store.AppRuntimeSample) error {
	return function(ctx, value)
}

type panicOnceSink struct {
	panicked bool
	samples  []store.AppRuntimeSample
}

func (sink *panicOnceSink) RecordAppRuntimeSample(_ context.Context, value store.AppRuntimeSample) error {
	if !sink.panicked {
		sink.panicked = true
		panic("sink panic")
	}
	sink.samples = append(sink.samples, value)
	return nil
}
