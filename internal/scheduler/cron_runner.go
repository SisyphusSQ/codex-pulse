package scheduler

import (
	"context"

	"github.com/robfig/cron/v3"
)

const schedulerCronSpec = "@every 1s"

type cronRunner interface {
	Start()
	Stop() context.Context
}

type cronRunnerFactory func(cron.Job) (cronRunner, error)

func defaultCronRunnerFactory(job cron.Job) (cronRunner, error) {
	runner := cron.New(cron.WithChain(
		cron.SkipIfStillRunning(cron.DiscardLogger),
		cron.Recover(cron.DiscardLogger),
	))
	if _, err := runner.AddJob(schedulerCronSpec, job); err != nil {
		return nil, err
	}
	return runner, nil
}
