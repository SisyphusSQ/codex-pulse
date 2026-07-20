package metrics

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sync"
	"sync/atomic"
	"time"

	"github.com/SisyphusSQ/codex-pulse/internal/store"
)

var (
	ErrCollector      = errors.New("runtime metrics collector")
	ErrCollectorPanic = errors.New("runtime metrics collector dependency panicked")
)

// QueryAggregate 是相邻成功样本之间的 content-free 查询延迟聚合。
type QueryAggregate struct {
	Count       int64
	TotalMicros int64
	MaxMicros   int64
}

// QueryAccumulator 允许 RPC 查询和采集任务并发写入/提取聚合结果。
type QueryAccumulator struct {
	mu        sync.Mutex
	aggregate QueryAggregate
}

func (accumulator *QueryAccumulator) Observe(duration time.Duration) {
	if duration < 0 {
		return
	}
	micros := duration.Microseconds()
	accumulator.mu.Lock()
	defer accumulator.mu.Unlock()
	accumulator.aggregate.Count = saturatingAdd(accumulator.aggregate.Count, 1)
	accumulator.aggregate.TotalMicros = saturatingAdd(accumulator.aggregate.TotalMicros, micros)
	accumulator.aggregate.MaxMicros = max(accumulator.aggregate.MaxMicros, micros)
}

func (accumulator *QueryAccumulator) Drain() QueryAggregate {
	accumulator.mu.Lock()
	defer accumulator.mu.Unlock()
	drained := accumulator.aggregate
	accumulator.aggregate = QueryAggregate{}
	return drained
}

func (accumulator *QueryAccumulator) Restore(drained QueryAggregate) {
	accumulator.mu.Lock()
	defer accumulator.mu.Unlock()
	accumulator.aggregate.Count = saturatingAdd(accumulator.aggregate.Count, drained.Count)
	accumulator.aggregate.TotalMicros = saturatingAdd(accumulator.aggregate.TotalMicros, drained.TotalMicros)
	accumulator.aggregate.MaxMicros = max(accumulator.aggregate.MaxMicros, drained.MaxMicros)
}

type ProcessMeasurement struct {
	CPUUserMS   int64
	CPUSystemMS int64
	RSSBytes    int64
}

type StoreMeasurement struct {
	DBBytes              int64
	WALBytes             int64
	DiskFreeBytes        int64
	LiveQueueDepth       int64
	BackfillQueueDepth   int64
	OldestLiveWaitMS     int64
	OldestBackfillWaitMS int64
}

type ProcessProbe interface {
	Measure(context.Context) (ProcessMeasurement, error)
}

type StoreProbe interface {
	Measure(context.Context, int64) (StoreMeasurement, error)
}

type SampleSink interface {
	RecordAppRuntimeSample(context.Context, store.AppRuntimeSample) error
}

type CollectorConfig struct {
	Process        ProcessProbe
	Store          StoreProbe
	Sink           SampleSink
	Queries        *QueryAccumulator
	Clock          func() time.Time
	GoroutineCount func() int
}

// Collector 把进程、存储、队列和查询延迟事实合并成一个原子 runtime sample。
// 任何失败都不提交部分样本，也不推进 CPU/RSS 基线。
type Collector struct {
	process        ProcessProbe
	store          StoreProbe
	sink           SampleSink
	queries        *QueryAccumulator
	clock          func() time.Time
	goroutineCount func() int

	mu             sync.Mutex
	hasBaseline    bool
	previousAt     time.Time
	previousAtMS   int64
	previousUserMS int64
	previousSysMS  int64
	peakRSSBytes   int64
	droppedSamples atomic.Int64
}

func NewCollector(config CollectorConfig) (*Collector, error) {
	if config.Process == nil || config.Store == nil || config.Sink == nil ||
		config.Queries == nil || config.Clock == nil || config.GoroutineCount == nil {
		return nil, fmt.Errorf("%w: all dependencies are required", ErrCollector)
	}
	return &Collector{
		process: config.Process, store: config.Store, sink: config.Sink,
		queries: config.Queries, clock: config.Clock, goroutineCount: config.GoroutineCount,
	}, nil
}

func (collector *Collector) Collect(ctx context.Context) (returnErr error) {
	collector.mu.Lock()
	defer collector.mu.Unlock()
	var drained QueryAggregate
	drainedOwned := false
	defer func() {
		if recover() == nil {
			return
		}
		if drainedOwned {
			collector.queries.Restore(drained)
		}
		returnErr = collector.drop(ErrCollectorPanic)
	}()

	capturedAt := collector.clock()
	capturedAtMS := capturedAt.UnixMilli()
	if capturedAtMS < 0 {
		return collector.drop(fmt.Errorf("%w: captured time must not be negative", ErrCollector))
	}
	processMeasurement, err := collector.process.Measure(ctx)
	if err != nil {
		return collector.drop(fmt.Errorf("%w: measure process: %w", ErrCollector, err))
	}
	if err := validateProcessMeasurement(processMeasurement); err != nil {
		return collector.drop(err)
	}
	storeMeasurement, err := collector.store.Measure(ctx, capturedAtMS)
	if err != nil {
		return collector.drop(fmt.Errorf("%w: measure store: %w", ErrCollector, err))
	}
	if err := validateStoreMeasurement(storeMeasurement); err != nil {
		return collector.drop(err)
	}
	goroutines := collector.goroutineCount()
	if goroutines < 1 {
		return collector.drop(fmt.Errorf("%w: goroutine count must be positive", ErrCollector))
	}

	queries := collector.queries.Drain()
	drained = queries
	drainedOwned = true
	completedAt := collector.clock()
	collectorDurationMicros := completedAt.Sub(capturedAt).Microseconds()
	if collectorDurationMicros < 0 {
		collector.queries.Restore(queries)
		drainedOwned = false
		return collector.drop(fmt.Errorf("%w: clock moved backwards during collection", ErrCollector))
	}

	cpuPercent, err := collector.cpuPercent(capturedAt, processMeasurement)
	if err != nil {
		collector.queries.Restore(queries)
		drainedOwned = false
		collector.hasBaseline = false
		return collector.drop(err)
	}
	peakRSSBytes := max(collector.peakRSSBytes, processMeasurement.RSSBytes)
	sample := store.AppRuntimeSample{
		CapturedAtMS: capturedAtMS, CPUPercent: cpuPercent,
		CPUUserMS: processMeasurement.CPUUserMS, CPUSystemMS: processMeasurement.CPUSystemMS,
		RSSBytes: processMeasurement.RSSBytes, PeakRSSBytes: peakRSSBytes,
		GoroutineCount: int64(goroutines),
		DBBytes:        storeMeasurement.DBBytes, WALBytes: storeMeasurement.WALBytes,
		DiskFreeBytes:  storeMeasurement.DiskFreeBytes,
		LiveQueueDepth: storeMeasurement.LiveQueueDepth, BackfillQueueDepth: storeMeasurement.BackfillQueueDepth,
		OldestLiveWaitMS:     storeMeasurement.OldestLiveWaitMS,
		OldestBackfillWaitMS: storeMeasurement.OldestBackfillWaitMS,
		QueryCount:           queries.Count, QueryTotalMicros: queries.TotalMicros, QueryMaxMicros: queries.MaxMicros,
		CollectorDurationMicros: collectorDurationMicros,
		DroppedSamples:          collector.droppedSamples.Load(),
	}
	if err := collector.sink.RecordAppRuntimeSample(ctx, sample); err != nil {
		collector.queries.Restore(queries)
		drainedOwned = false
		return collector.drop(fmt.Errorf("%w: persist sample: %w", ErrCollector, err))
	}
	drainedOwned = false

	collector.hasBaseline = true
	collector.previousAt = capturedAt
	collector.previousAtMS = capturedAtMS
	collector.previousUserMS = processMeasurement.CPUUserMS
	collector.previousSysMS = processMeasurement.CPUSystemMS
	collector.peakRSSBytes = peakRSSBytes
	return nil
}

func (collector *Collector) cpuPercent(capturedAt time.Time, measurement ProcessMeasurement) (float64, error) {
	if !collector.hasBaseline {
		return 0, nil
	}
	elapsedMS := capturedAt.Sub(collector.previousAt).Milliseconds()
	capturedAtMS := capturedAt.UnixMilli()
	userDelta := measurement.CPUUserMS - collector.previousUserMS
	systemDelta := measurement.CPUSystemMS - collector.previousSysMS
	if elapsedMS <= 0 || capturedAtMS <= collector.previousAtMS || userDelta < 0 ||
		systemDelta < 0 || userDelta > math.MaxInt64-systemDelta {
		return 0, fmt.Errorf("%w: invalid CPU counter or sample time progression", ErrCollector)
	}
	return float64(userDelta+systemDelta) / float64(elapsedMS) * 100, nil
}

func (collector *Collector) drop(err error) error {
	incrementSaturating(&collector.droppedSamples)
	return err
}

// RecordDroppedSample 记录一个被 shared overlap gate 跳过的采样触发。
// 使用 atomic 避免为了记账等待正在执行的 collector mutex。
func (collector *Collector) RecordDroppedSample() {
	if collector != nil {
		incrementSaturating(&collector.droppedSamples)
	}
}

func incrementSaturating(value *atomic.Int64) {
	for {
		current := value.Load()
		if current == math.MaxInt64 || value.CompareAndSwap(current, current+1) {
			return
		}
	}
}

func validateProcessMeasurement(value ProcessMeasurement) error {
	if value.CPUUserMS < 0 || value.CPUSystemMS < 0 || value.RSSBytes < 0 {
		return fmt.Errorf("%w: process measurement must not be negative", ErrCollector)
	}
	return nil
}

func validateStoreMeasurement(value StoreMeasurement) error {
	if value.DBBytes < 0 || value.WALBytes < 0 || value.DiskFreeBytes < 0 ||
		value.LiveQueueDepth < 0 || value.BackfillQueueDepth < 0 ||
		value.OldestLiveWaitMS < 0 || value.OldestBackfillWaitMS < 0 {
		return fmt.Errorf("%w: store measurement must not be negative", ErrCollector)
	}
	return nil
}

func saturatingAdd(left, right int64) int64 {
	if right > 0 && left > math.MaxInt64-right {
		return math.MaxInt64
	}
	return left + right
}
