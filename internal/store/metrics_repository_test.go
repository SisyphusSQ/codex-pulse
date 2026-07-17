package store

import (
	"errors"
	"math"
	"testing"
)

// 测试 RecordAppRuntimeSample 对 exact replay 幂等并拒绝同一采样时间的冲突事实。
func TestRecordAppRuntimeSampleIsReplaySafe(t *testing.T) {
	t.Parallel()

	repository := openRuntimeRepository(t)
	sample := validAppRuntimeSample(1_000)
	if err := repository.RecordAppRuntimeSample(t.Context(), sample); err != nil {
		t.Fatalf("RecordAppRuntimeSample() error = %v", err)
	}
	if err := repository.RecordAppRuntimeSample(t.Context(), sample); err != nil {
		t.Fatalf("RecordAppRuntimeSample(replay) error = %v", err)
	}
	conflict := sample
	conflict.RSSBytes++
	if err := repository.RecordAppRuntimeSample(t.Context(), conflict); !errors.Is(err, ErrInvalidRecord) {
		t.Fatalf("RecordAppRuntimeSample(conflict) error = %v, want ErrInvalidRecord", err)
	}

	values, err := repository.ListAppRuntimeSamples(t.Context(), AppRuntimeSampleFilter{
		FromMS: 0, UntilMS: 2_000, Limit: 10,
	})
	if err != nil || len(values) != 1 || values[0] != sample {
		t.Fatalf("ListAppRuntimeSamples() = %#v, %v", values, err)
	}
}

// 测试 app runtime sample 查询使用半开时间窗口、倒序排序与显式 limit。
func TestListAppRuntimeSamplesUsesHalfOpenWindowAndDescendingOrder(t *testing.T) {
	t.Parallel()

	repository := openRuntimeRepository(t)
	for _, capturedAtMS := range []int64{999, 1_000, 1_500, 2_000} {
		if err := repository.RecordAppRuntimeSample(t.Context(), validAppRuntimeSample(capturedAtMS)); err != nil {
			t.Fatalf("RecordAppRuntimeSample(%d) error = %v", capturedAtMS, err)
		}
	}
	values, err := repository.ListAppRuntimeSamples(t.Context(), AppRuntimeSampleFilter{
		FromMS: 1_000, UntilMS: 2_000, Limit: 2,
	})
	if err != nil {
		t.Fatalf("ListAppRuntimeSamples() error = %v", err)
	}
	if len(values) != 2 || values[0].CapturedAtMS != 1_500 || values[1].CapturedAtMS != 1_000 {
		t.Fatalf("ListAppRuntimeSamples() = %#v", values)
	}
}

// 测试 app runtime sample 拒绝 NaN、负数、关系冲突与非法 query 聚合。
func TestRecordAppRuntimeSampleRejectsInvalidMetrics(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		mutate func(*AppRuntimeSample)
	}{
		{name: "negative timestamp", mutate: func(value *AppRuntimeSample) { value.CapturedAtMS = -1 }},
		{name: "nan cpu", mutate: func(value *AppRuntimeSample) { value.CPUPercent = math.NaN() }},
		{name: "cpu over boundary", mutate: func(value *AppRuntimeSample) { value.CPUPercent = 102_401 }},
		{name: "negative cpu time", mutate: func(value *AppRuntimeSample) { value.CPUUserMS = -1 }},
		{name: "peak below rss", mutate: func(value *AppRuntimeSample) { value.PeakRSSBytes = value.RSSBytes - 1 }},
		{name: "zero goroutines", mutate: func(value *AppRuntimeSample) { value.GoroutineCount = 0 }},
		{name: "negative storage", mutate: func(value *AppRuntimeSample) { value.WALBytes = -1 }},
		{name: "negative queue", mutate: func(value *AppRuntimeSample) { value.LiveQueueDepth = -1 }},
		{name: "zero count with latency", mutate: func(value *AppRuntimeSample) {
			value.QueryCount, value.QueryTotalMicros, value.QueryMaxMicros = 0, 1, 1
		}},
		{name: "max exceeds total", mutate: func(value *AppRuntimeSample) {
			value.QueryCount, value.QueryTotalMicros, value.QueryMaxMicros = 2, 1, 2
		}},
		{name: "negative collector duration", mutate: func(value *AppRuntimeSample) {
			value.CollectorDurationMicros = -1
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			repository := openRuntimeRepository(t)
			value := validAppRuntimeSample(1_000)
			test.mutate(&value)
			if err := repository.RecordAppRuntimeSample(t.Context(), value); !errors.Is(err, ErrInvalidRecord) {
				t.Fatalf("RecordAppRuntimeSample() error = %v, want ErrInvalidRecord", err)
			}
		})
	}
}

func validAppRuntimeSample(capturedAtMS int64) AppRuntimeSample {
	return AppRuntimeSample{
		CapturedAtMS: capturedAtMS, CPUPercent: 12.5, CPUUserMS: 30, CPUSystemMS: 10,
		RSSBytes: 64 << 20, PeakRSSBytes: 96 << 20, GoroutineCount: 12,
		DBBytes: 4 << 20, WALBytes: 512 << 10, DiskFreeBytes: 12 << 30,
		LiveQueueDepth: 2, BackfillQueueDepth: 3,
		OldestLiveWaitMS: 20, OldestBackfillWaitMS: 40,
		QueryCount: 2, QueryTotalMicros: 700, QueryMaxMicros: 500,
		CollectorDurationMicros: 250, DroppedSamples: 1,
	}
}
