package app

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/SisyphusSQ/codex-pulse/internal/apisubscriptions"
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
	composition, err := composeCoreGraph(database, preferenceStore, runtime.Observer(), nil, nil, nil)
	if err != nil {
		t.Fatalf("composeCoreGraph() error = %v", err)
	}
	if composition == nil || composition.service == nil {
		t.Fatal("composeCoreGraph() returned nil service")
	}
	coreService := composition.service
	apiSnapshot, err := coreService.APISubscriptionsCurrent(t.Context(), 123)
	if err != nil {
		t.Fatalf("APISubscriptionsCurrent() error = %v", err)
	}
	if apiSnapshot.DeepSeek.Status.State != apisubscriptions.StateUnconfigured ||
		apiSnapshot.OpenCodeGo.Status.State != apisubscriptions.StateUnconfigured {
		t.Fatalf("API subscription states = %#v", apiSnapshot)
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
	workerDone := make(chan struct{})
	runtime := &applicationMetricsRuntime{
		worker: &backgroundWorker{cancel: cancelRuntime, done: workerDone},
	}
	defer close(workerDone)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := runtime.Close(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("Close() error = %v, want context.Canceled", err)
	}
}
