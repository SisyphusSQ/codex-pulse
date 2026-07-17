package runtimeinfo

import (
	"context"
	"errors"
	"testing"

	basequery "github.com/SisyphusSQ/codex-pulse/internal/query"
	"github.com/SisyphusSQ/codex-pulse/internal/store"
)

// 测试 DataHealth 在最近 24 小时场景下映射有界资源时间线与聚合事实。
func TestDataHealthMapsBoundedTwentyFourHourMetrics(t *testing.T) {
	t.Parallel()

	const evaluatedAtMS = store.MetricsSnapshotWindowMS + 10_000
	runtimeReader := &runtimeStub{metrics: func(_ context.Context, filter store.MetricsSnapshotFilter) (store.MetricsSnapshot, error) {
		if filter.FromMS != 10_000 || filter.UntilMS != evaluatedAtMS {
			t.Fatalf("MetricsSnapshot() filter = %#v", filter)
		}
		lastProgress, lastBackfill, lastAttempt, lastSuccess, nextRetry := int64(20_000), int64(19_000), int64(18_000), int64(17_000), int64(21_000)
		return store.MetricsSnapshot{
			FromMS: filter.FromMS, UntilMS: filter.UntilMS,
			RuntimeSamples: []store.AppRuntimeSample{
				{
					CapturedAtMS: 20_000, CPUPercent: 8.4, CPUUserMS: 11, CPUSystemMS: 7,
					RSSBytes: 186, PeakRSSBytes: 200, GoroutineCount: 12, DBBytes: 1_280,
					WALBytes: 48, DiskFreeBytes: 86_000, LiveQueueDepth: 2,
					BackfillQueueDepth: 1, OldestLiveWaitMS: 800, OldestBackfillWaitMS: 1_200,
					QueryCount: 3, QueryTotalMicros: 400, QueryMaxMicros: 200,
					CollectorDurationMicros: 30, DroppedSamples: 1,
				},
				{
					CapturedAtMS: 15_000, CPUPercent: 4.2, RSSBytes: 160, PeakRSSBytes: 180,
					GoroutineCount: 10, DBBytes: 1_200, WALBytes: 40, DiskFreeBytes: 87_000,
				},
			},
			Scheduler: store.SchedulerMetrics{
				CycleCount: 9, CompletedCycles: 6, YieldedCycles: 2, FailedCycles: 1,
				FilesScanned: 12, BytesRead: 2_048, ActiveMS: 500, MaxCycleActiveMS: 100,
				LastProgressAtMS: &lastProgress, LastBackfillProgressAtMS: &lastBackfill,
			},
			Jobs: store.JobMetrics{
				Queued: 1, Running: 2, Interrupted: 3, Succeeded: 4, Failed: 5,
				Cancelled: 6, DurationCount: 7, DurationTotalMS: 8, DurationMaxMS: 9,
			},
			Sources: store.SourceMetrics{
				Total: 7, Current: 5, Stale: 1, Unavailable: 1, ConsecutiveFailures: 2,
				MaxConsecutiveFailures: 2, Attempts: 10, SucceededAttempts: 8,
				FailedAttempts: 2, ResponseBytes: 4_096, LastAttemptAtMS: &lastAttempt,
				LastSuccessAtMS: &lastSuccess, NextRetryAtMS: &nextRetry,
			},
		}, nil
	}}
	service := newTestService(t, &quotaStub{}, runtimeReader, &preferencesStub{})

	response, err := service.DataHealth(context.Background(), evaluatedAtMS)
	if err != nil {
		t.Fatalf("DataHealth() error = %v", err)
	}
	if response.Meta.Version != basequery.ContractVersion || response.Meta.Status != basequery.ResponseComplete ||
		response.EvaluatedAtMS.Value == nil || *response.EvaluatedAtMS.Value != evaluatedAtMS ||
		response.Window.FromMS.Value == nil || *response.Window.FromMS.Value != 10_000 ||
		response.Window.UntilMS.Value == nil || *response.Window.UntilMS.Value != evaluatedAtMS {
		t.Fatalf("DataHealth() meta/window = %#v", response)
	}
	if len(response.Runtime) != 2 || response.Runtime[0].CPUPercent != 8.4 ||
		response.Runtime[0].RSSBytes.Value == nil || *response.Runtime[0].RSSBytes.Value != 186 ||
		response.Latest == nil || response.Latest.CapturedAtMS.Value == nil || *response.Latest.CapturedAtMS.Value != 20_000 {
		t.Fatalf("DataHealth() runtime = %#v", response.Runtime)
	}
	if response.Scheduler.CycleCount.Value == nil || *response.Scheduler.CycleCount.Value != 9 ||
		response.Scheduler.LastBackfillProgressAtMS.Value == nil || *response.Scheduler.LastBackfillProgressAtMS.Value != 19_000 ||
		response.Jobs.Running.Value == nil || *response.Jobs.Running.Value != 2 ||
		response.Sources.Unavailable.Value == nil || *response.Sources.Unavailable.Value != 1 ||
		response.Sources.NextRetryAtMS.Value == nil || *response.Sources.NextRetryAtMS.Value != 21_000 {
		t.Fatalf("DataHealth() aggregates = %#v", response)
	}
}

// 测试 DataHealth 在无采样场景下保持 known empty，不伪造最新资源值。
func TestDataHealthPreservesKnownEmptyRuntimeSeries(t *testing.T) {
	t.Parallel()

	runtimeReader := &runtimeStub{metrics: func(_ context.Context, filter store.MetricsSnapshotFilter) (store.MetricsSnapshot, error) {
		return store.MetricsSnapshot{FromMS: filter.FromMS, UntilMS: filter.UntilMS, RuntimeSamples: []store.AppRuntimeSample{}}, nil
	}}
	service := newTestService(t, &quotaStub{}, runtimeReader, &preferencesStub{})
	response, err := service.DataHealth(context.Background(), store.MetricsSnapshotWindowMS)
	if err != nil || response.Runtime == nil || len(response.Runtime) != 0 || response.Latest != nil {
		t.Fatalf("DataHealth(empty) = %#v, %v", response, err)
	}
}

// 测试 DataHealth 在 Detailed 采样场景下有界压缩时间线并保留两端证据。
func TestDataHealthBoundsDetailedRuntimeSeries(t *testing.T) {
	t.Parallel()

	const evaluatedAtMS = store.MetricsSnapshotWindowMS + 1_000_000
	samples := make([]store.AppRuntimeSample, MaxDataHealthRuntimePoints+11)
	for index := range samples {
		samples[index] = store.AppRuntimeSample{
			CapturedAtMS: evaluatedAtMS - 1 - int64(index),
			PeakRSSBytes: 1, GoroutineCount: 1,
		}
	}
	runtimeReader := &runtimeStub{metrics: func(_ context.Context, filter store.MetricsSnapshotFilter) (store.MetricsSnapshot, error) {
		return store.MetricsSnapshot{FromMS: filter.FromMS, UntilMS: filter.UntilMS, RuntimeSamples: samples}, nil
	}}
	service := newTestService(t, &quotaStub{}, runtimeReader, &preferencesStub{})
	response, err := service.DataHealth(context.Background(), evaluatedAtMS)
	if err != nil {
		t.Fatalf("DataHealth(detailed) error = %v", err)
	}
	if len(response.Runtime) != MaxDataHealthRuntimePoints ||
		*response.Runtime[0].CapturedAtMS.Value != samples[0].CapturedAtMS ||
		*response.Runtime[len(response.Runtime)-1].CapturedAtMS.Value != samples[len(samples)-1].CapturedAtMS {
		t.Fatalf("DataHealth(detailed) runtime endpoints = %d, %#v, %#v", len(response.Runtime), response.Runtime[0], response.Runtime[len(response.Runtime)-1])
	}
}

// 测试 DataHealth 优先返回当前 Job / open Health，并只补充 24 小时内的最近终态事实。
func TestDataHealthKeepsCurrentAndOpenFactsAheadOfBoundedRecentHistory(t *testing.T) {
	t.Parallel()

	const evaluatedAtMS = store.MetricsSnapshotWindowMS + 10_000
	fromMS := evaluatedAtMS - store.MetricsSnapshotWindowMS
	runtimeReader := &runtimeStub{
		jobPage: func(_ context.Context, filter store.RuntimeJobQuery) (store.RuntimeJobPage, error) {
			if filter.CurrentOnly {
				return store.RuntimeJobPage{Records: []store.RuntimeJobRecord{{Job: dataHealthJob("current", store.JobRunning, store.JobPhaseHistoryBackfill, fromMS-1)}}}, nil
			}
			return store.RuntimeJobPage{Records: []store.RuntimeJobRecord{
				{Job: dataHealthJob("recent", store.JobSucceeded, store.JobPhaseLive, fromMS)},
				{Job: dataHealthJob("expired", store.JobFailed, store.JobPhaseLive, fromMS-1)},
			}}, nil
		},
		healthPage: func(_ context.Context, filter store.RuntimeHealthQuery) (store.RuntimeHealthPage, error) {
			if filter.Active != nil && *filter.Active {
				return store.RuntimeHealthPage{Records: []store.HealthEvent{dataHealthEvent("open", fromMS-1, nil)}}, nil
			}
			resolvedAtMS := fromMS + 1
			expiredResolvedAtMS := fromMS - 1
			return store.RuntimeHealthPage{Records: []store.HealthEvent{
				dataHealthEvent("recent", fromMS, &resolvedAtMS),
				dataHealthEvent("expired", fromMS-1, &expiredResolvedAtMS),
			}}, nil
		},
	}
	service := newTestService(t, &quotaStub{}, runtimeReader, &preferencesStub{})

	response, err := service.DataHealth(context.Background(), evaluatedAtMS)
	if err != nil {
		t.Fatalf("DataHealth(activity) error = %v", err)
	}
	if len(response.CurrentJobs) != 1 || response.CurrentJobs[0].JobID != "current" ||
		len(response.RecentJobs) != 1 || response.RecentJobs[0].JobID != "recent" ||
		len(response.OpenEvents) != 1 || response.OpenEvents[0].EventID != "open" ||
		len(response.RecentEvents) != 1 || response.RecentEvents[0].EventID != "recent" {
		t.Fatalf("DataHealth(activity) = %#v", response)
	}
}

// 测试 DataHealth 在取消、非法时间与不一致 Store 快照场景下 fail closed。
func TestDataHealthRejectsCancelledInvalidAndInconsistentSnapshots(t *testing.T) {
	t.Parallel()

	service := newTestService(t, &quotaStub{}, &runtimeStub{}, &preferencesStub{})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := service.DataHealth(ctx, store.MetricsSnapshotWindowMS); !errors.Is(err, context.Canceled) {
		t.Fatalf("DataHealth(cancelled) error = %v", err)
	}
	for _, evaluatedAtMS := range []int64{-1, store.MetricsSnapshotWindowMS - 1, basequery.JavaScriptMaxSafeInteger + 1} {
		if _, err := service.DataHealth(context.Background(), evaluatedAtMS); err == nil {
			t.Fatalf("DataHealth(%d) error = nil", evaluatedAtMS)
		}
	}

	inconsistent := &runtimeStub{metrics: func(_ context.Context, filter store.MetricsSnapshotFilter) (store.MetricsSnapshot, error) {
		return store.MetricsSnapshot{FromMS: filter.FromMS + 1, UntilMS: filter.UntilMS}, nil
	}}
	service = newTestService(t, &quotaStub{}, inconsistent, &preferencesStub{})
	if _, err := service.DataHealth(context.Background(), store.MetricsSnapshotWindowMS); err == nil {
		t.Fatal("DataHealth(inconsistent) error = nil")
	}
}

func dataHealthJob(id string, state store.JobState, phase store.JobPhase, atMS int64) store.JobRun {
	startedAtMS := atMS
	finishedAtMS := atMS
	job := store.JobRun{
		JobID: id, JobType: "synthetic", RequestedBy: "test", State: state, Phase: phase,
		CreatedAtMS: atMS, UpdatedAtMS: atMS,
	}
	if state != store.JobQueued {
		job.StartedAtMS = &startedAtMS
	}
	if state != store.JobRunning {
		job.FinishedAtMS = &finishedAtMS
	}
	if state == store.JobFailed {
		job.ErrorClass = pointerTo(store.RuntimeErrorTimeout)
	}
	return job
}

func dataHealthEvent(id string, atMS int64, resolvedAtMS *int64) store.HealthEvent {
	updatedAtMS := atMS
	if resolvedAtMS != nil {
		updatedAtMS = *resolvedAtMS
	}
	return store.HealthEvent{
		EventID: id, Fingerprint: store.SHA256DigestOf([]byte(id)), Domain: store.HealthDomainRuntime,
		Severity: store.HealthWarning, Code: store.HealthCodeRuntimeUnknown, FirstSeenAtMS: atMS,
		LastSeenAtMS: atMS, ResolvedAtMS: resolvedAtMS, OccurrenceCount: 1, UpdatedAtMS: updatedAtMS,
	}
}
