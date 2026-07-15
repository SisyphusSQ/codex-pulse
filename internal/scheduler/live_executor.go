package scheduler

import (
	"context"

	"github.com/SisyphusSQ/codex-pulse/internal/liveindex"
	"github.com/SisyphusSQ/codex-pulse/internal/store"
)

type LiveTarget interface {
	RunSlice(context.Context, string, liveindex.SliceBudget) (liveindex.SliceReport, error)
	Interrupt(context.Context, string, store.RuntimeErrorClass) error
	Recover(context.Context, string) (store.JobRun, error)
	Retry(context.Context, string) (store.JobRun, error)
}

func (executor *liveExecutor) Retry(
	ctx context.Context,
	task store.SchedulerTask,
) (store.JobRun, error) {
	if executor == nil || executor.target == nil || task.TargetKind != store.SchedulerTargetLiveScan {
		return store.JobRun{}, ErrExecutorMissing
	}
	return executor.target.Retry(ctx, task.TargetID)
}

type liveExecutor struct {
	target LiveTarget
}

func NewLiveExecutor(target LiveTarget) (Executor, error) {
	if target == nil {
		return nil, ErrExecutorMissing
	}
	return &liveExecutor{target: target}, nil
}

func (executor *liveExecutor) ExecuteSlice(
	ctx context.Context,
	task store.SchedulerTask,
	budget ScanBudget,
) (SliceResult, error) {
	if executor == nil || executor.target == nil || task.TargetKind != store.SchedulerTargetLiveScan {
		return SliceResult{}, ErrExecutorMissing
	}
	report, err := executor.target.RunSlice(ctx, task.TargetID, liveindex.SliceBudget{
		MaxFiles: budget.MaxFiles, MaxBytes: budget.MaxBytes, MaxActive: budget.MaxActive,
	})
	return SliceResult{
		FilesProcessed: report.FilesProcessed, BytesProcessed: report.BytesRead,
		Active:     boundedCooperativeActive(report.Active, budget.MaxActive),
		StopReason: liveStopReason(report.ExhaustedBy),
	}, err
}

func (executor *liveExecutor) Interrupt(
	ctx context.Context,
	task store.SchedulerTask,
	class store.RuntimeErrorClass,
) error {
	if executor == nil || executor.target == nil || task.TargetKind != store.SchedulerTargetLiveScan {
		return ErrExecutorMissing
	}
	return executor.target.Interrupt(ctx, task.TargetID, class)
}

func (executor *liveExecutor) Recover(
	ctx context.Context,
	task store.SchedulerTask,
) (store.JobRun, error) {
	if executor == nil || executor.target == nil || task.TargetKind != store.SchedulerTargetLiveScan {
		return store.JobRun{}, ErrExecutorMissing
	}
	return executor.target.Recover(ctx, task.TargetID)
}

func liveStopReason(reason liveindex.SliceStopReason) store.SchedulerStopReason {
	switch reason {
	case liveindex.SliceStopCompleted:
		return store.SchedulerStopCompleted
	case liveindex.SliceStopFileBudget:
		return store.SchedulerStopFileBudget
	case liveindex.SliceStopByteBudget:
		return store.SchedulerStopByteBudget
	case liveindex.SliceStopTimeBudget:
		return store.SchedulerStopTimeBudget
	default:
		return store.SchedulerStopDependencyError
	}
}

var _ Executor = (*liveExecutor)(nil)
