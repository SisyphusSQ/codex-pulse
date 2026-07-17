package app

import (
	"context"
	"errors"
	"testing"
	"time"

	retentionmodel "github.com/SisyphusSQ/codex-pulse/internal/retention"
	"github.com/SisyphusSQ/codex-pulse/internal/store"
)

func TestApplicationRetentionRuntimeCleansExpiredSamplesAndCloses(t *testing.T) {
	database, repository := openQuotaRuntimeStore(t)
	defer func() { _ = database.Close(context.Background()) }()
	now := time.Now().UTC()
	for _, capturedAtMS := range []int64{
		now.Add(-store.RetentionWindow - time.Second).UnixMilli(),
		now.Add(-store.RetentionWindow + time.Second).UnixMilli(),
	} {
		if err := repository.RecordAppRuntimeSample(t.Context(), store.AppRuntimeSample{
			CapturedAtMS: capturedAtMS, GoroutineCount: 1,
		}); err != nil {
			t.Fatalf("RecordAppRuntimeSample(%d) error = %v", capturedAtMS, err)
		}
	}

	runtime, err := startApplicationRetentionRuntime(t.Context(), database)
	if err != nil {
		t.Fatalf("startApplicationRetentionRuntime() error = %v", err)
	}
	waitForAppCondition(t, func() bool {
		projection := runtime.Projection()
		if projection.State != retentionmodel.StateSucceeded || projection.Attempt.Deleted.RuntimeSamples != 1 ||
			!projection.Attempt.CheckpointCompleted {
			return false
		}
		samples, listErr := repository.ListAppRuntimeSamples(t.Context(), store.AppRuntimeSampleFilter{
			FromMS: 0, UntilMS: now.Add(time.Minute).UnixMilli(), Limit: 10,
		})
		return listErr == nil && len(samples) == 1 && samples[0].CapturedAtMS > now.Add(-store.RetentionWindow).UnixMilli()
	}, "retention runtime did not complete immediate cleanup and checkpoint")
	if err := runtime.Close(context.Background()); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if err := runtime.Close(context.Background()); err != nil {
		t.Fatalf("Close(replay) error = %v", err)
	}
}

func TestApplicationRetentionRuntimeCloseHonorsCallerCancellation(t *testing.T) {
	_, cancelRuntime := context.WithCancel(context.Background())
	workerDone := make(chan error, 1)
	runtime := &applicationRetentionRuntime{
		cancel: cancelRuntime, workerDone: workerDone, closeDone: make(chan struct{}),
	}
	defer func() { workerDone <- nil }()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := runtime.Close(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("Close() error = %v, want context.Canceled", err)
	}
}
