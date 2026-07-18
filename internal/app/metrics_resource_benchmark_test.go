package app

import (
	"context"
	"errors"
	"os"
	gort "runtime"
	"sync/atomic"
	"testing"
	"time"

	"github.com/SisyphusSQ/codex-pulse/internal/metrics"
	"github.com/SisyphusSQ/codex-pulse/internal/store"
)

// BenchmarkApplicationMetricsCollector 使用真实 gopsutil、SQLite/WAL、磁盘、
// scheduler snapshot 与 maintenance writer；数据库仅位于 b.TempDir()。
// duty_pct 按 Detailed 5 秒 cadence 计算单核平均占用门槛。
func BenchmarkApplicationMetricsCollector(b *testing.B) {
	database, repository := openQuotaRuntimeStore(b)
	defer func() { _ = database.Close(context.Background()) }()
	if _, err := repository.InitializeSchedulerLifecycle(b.Context(), store.SchedulerLifecycle{
		HomeGeneration: 1, UserPauseScope: store.LifecyclePauseNone,
		SystemState: store.LifecycleSystemAwake, Transition: store.LifecycleTransitionSteady,
		SourceState: store.LifecycleSourceAvailable, LastEventID: "metrics:resource-benchmark",
		Revision: 1, UpdatedAtMS: 1,
	}); err != nil {
		b.Fatalf("InitializeSchedulerLifecycle() error = %v", err)
	}
	processProbe, err := metrics.NewGopsutilProcessProbe(os.Getpid())
	if err != nil {
		b.Fatalf("NewGopsutilProcessProbe() error = %v", err)
	}
	storeProbe, err := metrics.NewFileStoreProbe(database.Config().Path, repository)
	if err != nil {
		b.Fatalf("NewFileStoreProbe() error = %v", err)
	}
	base := time.Unix(1_800_000_000, 0)
	var clockTick atomic.Int64
	queries := &metrics.QueryAccumulator{}
	collector, err := metrics.NewCollector(metrics.CollectorConfig{
		Process: processProbe, Store: storeProbe, Sink: repository,
		Queries: queries,
		Clock: func() time.Time {
			return base.Add(time.Duration(clockTick.Add(1)) * time.Millisecond)
		},
		GoroutineCount: gort.NumGoroutine,
	})
	if err != nil {
		b.Fatalf("NewCollector() error = %v", err)
	}

	b.ReportAllocs()
	b.ResetTimer()
	probeRetries := 0
	for index := 0; index < b.N; {
		queryStartedAt := time.Now()
		if _, err := repository.MetricsSnapshot(b.Context(), store.MetricsSnapshotFilter{
			FromMS:  base.Add(-24 * time.Hour).UnixMilli(),
			UntilMS: base.UnixMilli(),
		}); err != nil {
			b.Fatalf("MetricsSnapshot(%d) error = %v", index, err)
		}
		queries.Observe(time.Since(queryStartedAt))
		if err := collector.Collect(b.Context()); err != nil {
			// Real process counters can briefly move backwards after millisecond
			// conversion. Collector resets its baseline on that error, so retry the
			// bounded sample instead of turning host jitter into a false benchmark
			// failure. Persistent collector or probe failures still exhaust the cap.
			if (errors.Is(err, metrics.ErrProbe) || errors.Is(err, metrics.ErrCollector)) &&
				probeRetries < max(10, b.N) {
				probeRetries++
				continue
			}
			b.Fatalf("Collect(%d) error = %v", index, err)
		}
		index++
	}
	elapsed := b.Elapsed()
	b.StopTimer()
	if b.N > 0 {
		perSample := elapsed / time.Duration(b.N)
		b.ReportMetric(float64(perSample)/float64(5*time.Second)*100, "duty_pct")
		samples, err := repository.ListAppRuntimeSamples(b.Context(), store.AppRuntimeSampleFilter{
			FromMS: base.UnixMilli(), UntilMS: base.Add(time.Hour).UnixMilli(), Limit: b.N,
		})
		if err != nil || len(samples) != b.N {
			b.Fatalf("ListAppRuntimeSamples() = %#v, %v", samples, err)
		}
		var maxRSS, maxDB, maxWAL, maxQuery, maxLive, maxBackfill int64
		for _, sample := range samples {
			maxRSS = max(maxRSS, sample.PeakRSSBytes)
			maxDB = max(maxDB, sample.DBBytes)
			maxWAL = max(maxWAL, sample.WALBytes)
			maxQuery = max(maxQuery, sample.QueryMaxMicros)
			maxLive = max(maxLive, sample.LiveQueueDepth)
			maxBackfill = max(maxBackfill, sample.BackfillQueueDepth)
		}
		b.ReportMetric(float64(maxRSS)/(1<<20), "rss_mib")
		b.ReportMetric(float64(maxDB)/(1<<20), "db_mib")
		b.ReportMetric(float64(maxWAL)/(1<<20), "wal_mib")
		b.ReportMetric(float64(maxQuery)/1_000, "query_ms")
		b.ReportMetric(float64(maxLive), "live_depth")
		b.ReportMetric(float64(maxBackfill), "backfill_depth")
		b.ReportMetric(float64(probeRetries), "probe_retries")
	}
}
