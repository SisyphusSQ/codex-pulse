package scheduler

import (
	"context"

	"github.com/SisyphusSQ/codex-pulse/internal/bootstrap"
	"github.com/SisyphusSQ/codex-pulse/internal/store"
)

type BootstrapTarget interface {
	RunSlice(context.Context, string, bootstrap.SliceBudget) (bootstrap.SliceReport, error)
	Interrupt(context.Context, string, store.RuntimeErrorClass) error
	Recover(context.Context, string) (store.JobRun, error)
	Retry(context.Context, string) (store.JobRun, error)
}

func (executor *bootstrapExecutor) Retry(
	ctx context.Context,
	task store.SchedulerTask,
) (store.JobRun, error) {
	if executor == nil || executor.target == nil || task.TargetKind != store.SchedulerTargetBootstrap {
		return store.JobRun{}, ErrExecutorMissing
	}
	return executor.target.Retry(ctx, task.TargetID)
}

type bootstrapExecutor struct {
	target BootstrapTarget
}

func NewBootstrapExecutor(target BootstrapTarget) (Executor, error) {
	if target == nil {
		return nil, ErrExecutorMissing
	}
	return &bootstrapExecutor{target: target}, nil
}

func (executor *bootstrapExecutor) ExecuteSlice(
	ctx context.Context,
	task store.SchedulerTask,
	budget ScanBudget,
) (SliceResult, error) {
	if executor == nil || executor.target == nil || task.TargetKind != store.SchedulerTargetBootstrap {
		return SliceResult{}, ErrExecutorMissing
	}
	report, err := executor.target.RunSlice(ctx, task.TargetID, bootstrap.SliceBudget{
		MaxFiles: budget.MaxFiles, MaxBytes: budget.MaxBytes, MaxActive: budget.MaxActive,
	})
	return SliceResult{
		FilesProcessed: report.FilesProcessed, BytesProcessed: report.BytesRead,
		Active:     boundedCooperativeActive(report.Active, budget.MaxActive),
		StopReason: bootstrapStopReason(report.ExhaustedBy),
	}, err
}

func (executor *bootstrapExecutor) Interrupt(
	ctx context.Context,
	task store.SchedulerTask,
	class store.RuntimeErrorClass,
) error {
	if executor == nil || executor.target == nil || task.TargetKind != store.SchedulerTargetBootstrap {
		return ErrExecutorMissing
	}
	return executor.target.Interrupt(ctx, task.TargetID, class)
}

func (executor *bootstrapExecutor) Recover(
	ctx context.Context,
	task store.SchedulerTask,
) (store.JobRun, error) {
	if executor == nil || executor.target == nil || task.TargetKind != store.SchedulerTargetBootstrap {
		return store.JobRun{}, ErrExecutorMissing
	}
	return executor.target.Recover(ctx, task.TargetID)
}

func bootstrapStopReason(reason bootstrap.SliceStopReason) store.SchedulerStopReason {
	switch reason {
	case bootstrap.SliceStopCompleted:
		return store.SchedulerStopCompleted
	case bootstrap.SliceStopFileBudget:
		return store.SchedulerStopFileBudget
	case bootstrap.SliceStopByteBudget:
		return store.SchedulerStopByteBudget
	case bootstrap.SliceStopTimeBudget:
		return store.SchedulerStopTimeBudget
	default:
		return store.SchedulerStopDependencyError
	}
}

var _ Executor = (*bootstrapExecutor)(nil)
