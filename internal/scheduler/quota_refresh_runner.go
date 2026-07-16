package scheduler

import (
	"context"
	"sync"

	"github.com/robfig/cron/v3"
)

type QuotaRefreshRunner struct {
	coordinator   *QuotaRefreshCoordinator
	newCronRunner cronRunnerFactory

	runMu   sync.Mutex
	running bool
}

func NewQuotaRefreshRunner(coordinator *QuotaRefreshCoordinator) (*QuotaRefreshRunner, error) {
	if coordinator == nil {
		return nil, ErrInvalidQuotaRefreshCoordinator
	}
	return &QuotaRefreshRunner{coordinator: coordinator, newCronRunner: defaultCronRunnerFactory}, nil
}

// Run performs durable recovery and one immediate due cycle, then delegates
// all periodic wakeups to robfig/cron v3. The Store remains the due/claim truth.
func (runner *QuotaRefreshRunner) Run(ctx context.Context) error {
	if runner == nil || runner.coordinator == nil || runner.newCronRunner == nil || ctx == nil {
		return ErrInvalidQuotaRefreshCoordinator
	}
	runner.runMu.Lock()
	if runner.running {
		runner.runMu.Unlock()
		return ErrRunAlreadyActive
	}
	runner.running = true
	runner.runMu.Unlock()
	defer func() {
		runner.runMu.Lock()
		runner.running = false
		runner.runMu.Unlock()
	}()

	jobCtx, cancelJobs := context.WithCancel(ctx)
	defer cancelJobs()
	fatalErrors := make(chan error, 1)
	var fatalOnce sync.Once
	reportFatal := func(err error) {
		if err == nil {
			return
		}
		fatalOnce.Do(func() {
			cancelJobs()
			fatalErrors <- err
		})
	}
	job := cron.FuncJob(func() {
		defer func() {
			if recover() != nil {
				reportFatal(ErrSchedulerCronPanic)
			}
		}()
		if err := runner.coordinator.RunDueCycle(jobCtx); shouldStopQuotaRefreshRunner(err) {
			reportFatal(err)
		}
	})
	cronRunner, err := runner.newCronRunner(job)
	if err != nil {
		return err
	}
	if err := runner.coordinator.Initialize(jobCtx); shouldStopQuotaRefreshRunner(err) {
		return err
	}
	cronRunner.Start()
	var cause error
	select {
	case <-ctx.Done():
		cause = ctx.Err()
	case cause = <-fatalErrors:
	}
	<-cronRunner.Stop().Done()
	return cause
}
