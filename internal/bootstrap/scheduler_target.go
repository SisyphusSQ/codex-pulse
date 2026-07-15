package bootstrap

import (
	"context"

	"github.com/SisyphusSQ/codex-pulse/internal/store"
)

// Interrupt 由scheduler在panic/cancel或启动恢复时调用；error class由scheduler task持久化。
func (runtime *Runtime) Interrupt(
	ctx context.Context,
	jobID string,
	_ store.RuntimeErrorClass,
) error {
	if runtime == nil || runtime.repository == nil || jobID == "" {
		return ErrInvalidRuntime
	}
	job, facts, err := runtime.repository.BootstrapRun(ctx, jobID)
	if err != nil {
		return err
	}
	if job.State == store.JobInterrupted {
		return nil
	}
	if job.State != store.JobQueued && job.State != store.JobRunning {
		return ErrSourceUnavailable
	}
	return runtime.interrupt(ctx, job, facts, nil)
}

// Recover 读回terminal attempt，或把非terminal旧attempt稳定恢复成同generation的新queued attempt。
func (runtime *Runtime) Recover(ctx context.Context, jobID string) (store.JobRun, error) {
	if runtime == nil || runtime.repository == nil || jobID == "" {
		return store.JobRun{}, ErrInvalidRuntime
	}
	job, facts, err := runtime.repository.BootstrapRun(ctx, jobID)
	if err != nil {
		return store.JobRun{}, err
	}
	if job.State == store.JobSucceeded || job.State == store.JobFailed || job.State == store.JobCancelled {
		return job, nil
	}
	if job.State == store.JobQueued || job.State == store.JobRunning {
		if err := runtime.Interrupt(ctx, jobID, store.RuntimeErrorUnknown); err != nil {
			return store.JobRun{}, err
		}
		job, facts, err = runtime.repository.BootstrapRun(ctx, jobID)
		if err != nil {
			return store.JobRun{}, err
		}
	}
	if job.State != store.JobInterrupted {
		return store.JobRun{}, ErrSourceUnavailable
	}
	if facts.HomeGeneration < 0 {
		return store.JobRun{}, ErrInvalidRuntime
	}
	if err := runtime.Resume(ctx, uint64(facts.HomeGeneration)); err != nil {
		return store.JobRun{}, err
	}
	resumed, resumedFacts, err := runtime.repository.LatestBootstrapRunByGeneration(
		ctx, facts.HomeGeneration,
	)
	if err != nil {
		return store.JobRun{}, err
	}
	if resumedFacts.SwitchID != facts.SwitchID || resumed.State != store.JobQueued ||
		resumed.ResumeOfJobID == nil {
		return store.JobRun{}, ErrInvalidRuntime
	}
	if *resumed.ResumeOfJobID != jobID {
		return store.JobRun{}, ErrInvalidRuntime
	}
	return resumed, nil
}
