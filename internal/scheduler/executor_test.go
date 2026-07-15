package scheduler

import (
	"context"
	"testing"
	"time"

	"github.com/SisyphusSQ/codex-pulse/internal/bootstrap"
	"github.com/SisyphusSQ/codex-pulse/internal/liveindex"
	"github.com/SisyphusSQ/codex-pulse/internal/store"
)

func TestBuiltInExecutorsCapCooperativeActiveAccountingAtBudget(t *testing.T) {
	t.Parallel()

	budget := ScanBudget{MaxFiles: 1, MaxBytes: 1 << 20, MaxActive: 50 * time.Millisecond}
	tests := []struct {
		name     string
		executor Executor
		task     store.SchedulerTask
	}{
		{
			name:     "live",
			executor: mustLiveExecutor(t, overactiveLiveTarget{}),
			task: store.SchedulerTask{
				TargetKind: store.SchedulerTargetLiveScan, TargetID: "live-overactive",
			},
		},
		{
			name:     "bootstrap",
			executor: mustBootstrapExecutor(t, overactiveBootstrapTarget{}),
			task: store.SchedulerTask{
				TargetKind: store.SchedulerTargetBootstrap, TargetID: "bootstrap-overactive",
			},
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			result, err := test.executor.ExecuteSlice(context.Background(), test.task, budget)
			if err != nil || result.Active != budget.MaxActive {
				t.Fatalf("ExecuteSlice() = %#v, %v, want active %s", result, err, budget.MaxActive)
			}
		})
	}
}

func mustLiveExecutor(t *testing.T, target LiveTarget) Executor {
	t.Helper()
	executor, err := NewLiveExecutor(target)
	if err != nil {
		t.Fatalf("NewLiveExecutor() error = %v", err)
	}
	return executor
}

func mustBootstrapExecutor(t *testing.T, target BootstrapTarget) Executor {
	t.Helper()
	executor, err := NewBootstrapExecutor(target)
	if err != nil {
		t.Fatalf("NewBootstrapExecutor() error = %v", err)
	}
	return executor
}

type overactiveLiveTarget struct{}

func (overactiveLiveTarget) RunSlice(
	context.Context,
	string,
	liveindex.SliceBudget,
) (liveindex.SliceReport, error) {
	return liveindex.SliceReport{
		FilesProcessed: 1, Active: time.Second, ExhaustedBy: liveindex.SliceStopCompleted,
	}, nil
}

func (overactiveLiveTarget) Interrupt(context.Context, string, store.RuntimeErrorClass) error {
	return nil
}

func (overactiveLiveTarget) Recover(context.Context, string) (store.JobRun, error) {
	return store.JobRun{}, nil
}

func (overactiveLiveTarget) Retry(context.Context, string) (store.JobRun, error) {
	return store.JobRun{}, nil
}

type overactiveBootstrapTarget struct{}

func (overactiveBootstrapTarget) RunSlice(
	context.Context,
	string,
	bootstrap.SliceBudget,
) (bootstrap.SliceReport, error) {
	return bootstrap.SliceReport{
		FilesProcessed: 1, Active: time.Second, ExhaustedBy: bootstrap.SliceStopCompleted,
	}, nil
}

func (overactiveBootstrapTarget) Interrupt(context.Context, string, store.RuntimeErrorClass) error {
	return nil
}

func (overactiveBootstrapTarget) Recover(context.Context, string) (store.JobRun, error) {
	return store.JobRun{}, nil
}

func (overactiveBootstrapTarget) Retry(context.Context, string) (store.JobRun, error) {
	return store.JobRun{}, nil
}
