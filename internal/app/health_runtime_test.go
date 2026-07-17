package app

import (
	"context"
	"errors"
	"testing"
	"time"

	healthmodel "github.com/SisyphusSQ/codex-pulse/internal/health"
	"github.com/SisyphusSQ/codex-pulse/internal/store"
)

func TestApplicationHealthRuntimeEvaluatesPersistsAndCloses(t *testing.T) {
	database, repository := openQuotaRuntimeStore(t)
	defer func() { _ = database.Close(context.Background()) }()
	now := time.Now().UnixMilli()
	if _, err := repository.InitializeSchedulerLifecycle(t.Context(), store.SchedulerLifecycle{
		HomeGeneration: 1, UserPauseScope: store.LifecyclePauseNone,
		SystemState: store.LifecycleSystemAwake, Transition: store.LifecycleTransitionSteady,
		SourceState: store.LifecycleSourceAvailable, LastEventID: "health:test",
		Revision: 1, UpdatedAtMS: now - 1,
	}); err != nil {
		t.Fatalf("InitializeSchedulerLifecycle() error = %v", err)
	}
	if err := repository.RecordAppRuntimeSample(t.Context(), store.AppRuntimeSample{
		CapturedAtMS: now, CPUPercent: 1, CPUUserMS: 1, CPUSystemMS: 1,
		RSSBytes: 1, PeakRSSBytes: 1, GoroutineCount: 1,
		DBBytes: 1, DiskFreeBytes: 1,
	}); err != nil {
		t.Fatalf("RecordAppRuntimeSample() error = %v", err)
	}

	runtime, err := startApplicationHealthRuntime(t.Context(), database)
	if err != nil {
		t.Fatalf("startApplicationHealthRuntime() error = %v", err)
	}
	waitForAppCondition(t, func() bool {
		projection := runtime.Projection()
		if !projection.HasValue || projection.Stale || projection.Result.Level != healthmodel.LevelBusy {
			return false
		}
		events, listErr := repository.ListHealthEvents(t.Context(), store.HealthEventFilter{Limit: 20})
		return listErr == nil && len(events) == 1 && events[0].Code == store.HealthCodeStoreDiskLow &&
			events[0].ResolvedAtMS == nil
	}, "health runtime did not persist immediate evaluation")
	if err := runtime.Close(context.Background()); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if err := runtime.Close(context.Background()); err != nil {
		t.Fatalf("Close(replay) error = %v", err)
	}
}

func TestApplicationHealthRuntimeCloseHonorsCallerCancellation(t *testing.T) {
	_, cancelRuntime := context.WithCancel(context.Background())
	workerDone := make(chan error, 1)
	runtime := &applicationHealthRuntime{
		cancel: cancelRuntime, workerDone: workerDone, closeDone: make(chan struct{}),
	}
	defer func() { workerDone <- nil }()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := runtime.Close(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("Close() error = %v, want context.Canceled", err)
	}
}
