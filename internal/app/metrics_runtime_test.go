package app

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/SisyphusSQ/codex-pulse/internal/metrics"
	"github.com/SisyphusSQ/codex-pulse/internal/preferences"
	"github.com/SisyphusSQ/codex-pulse/internal/store"
)

func TestApplicationMetricsRuntimeComposesPersistsAndCloses(t *testing.T) {
	database, repository := openQuotaRuntimeStore(t)
	defer func() { _ = database.Close(context.Background()) }()
	if _, err := repository.InitializeSchedulerLifecycle(t.Context(), store.SchedulerLifecycle{
		HomeGeneration: 1, UserPauseScope: store.LifecyclePauseNone,
		SystemState: store.LifecycleSystemAwake, Transition: store.LifecycleTransitionSteady,
		SourceState: store.LifecycleSourceAvailable, LastEventID: "metrics:test",
		Revision: 1, UpdatedAtMS: 1,
	}); err != nil {
		t.Fatalf("InitializeSchedulerLifecycle() error = %v", err)
	}

	runtime, err := startApplicationMetricsRuntime(t.Context(), database, metrics.SamplingModeNormal)
	if err != nil {
		t.Fatalf("startApplicationMetricsRuntime() error = %v", err)
	}
	preferenceStore, err := preferences.NewFileStore(filepath.Join(t.TempDir(), "preferences.json"))
	if err != nil {
		t.Fatalf("NewFileStore() error = %v", err)
	}
	coreService, err := composeCoreService(database, preferenceStore, runtime.Observer())
	if err != nil {
		t.Fatalf("composeCoreService() error = %v", err)
	}
	if coreService == nil {
		t.Fatal("composeCoreService() returned nil")
	}

	time.Sleep(2 * time.Millisecond)
	if err := runtime.collector.Collect(t.Context()); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	samples, err := repository.ListAppRuntimeSamples(t.Context(), store.AppRuntimeSampleFilter{
		FromMS: 0, UntilMS: time.Now().Add(time.Second).UnixMilli(), Limit: 10,
	})
	if err != nil || len(samples) == 0 {
		t.Fatalf("ListAppRuntimeSamples() = %#v, %v", samples, err)
	}
	if err := runtime.Close(context.Background()); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if err := runtime.Close(context.Background()); err != nil {
		t.Fatalf("Close(replay) error = %v", err)
	}
}

func TestApplicationMetricsRuntimeCloseHonorsCallerCancellation(t *testing.T) {
	_, cancelRuntime := context.WithCancel(context.Background())
	workerDone := make(chan error, 1)
	runtime := &applicationMetricsRuntime{
		cancel: cancelRuntime, workerDone: workerDone, closeDone: make(chan struct{}),
	}
	defer func() { workerDone <- nil }()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := runtime.Close(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("Close() error = %v, want context.Canceled", err)
	}
}
