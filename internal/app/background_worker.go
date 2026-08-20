package app

import (
	"context"
	"errors"
	"sync"
)

type backgroundWorker struct {
	cancel context.CancelFunc
	done   chan struct{}

	stopOnce sync.Once
	runErr   error
}

func startBackgroundWorker(ctx context.Context, run func(context.Context) error) *backgroundWorker {
	runCtx, cancel := context.WithCancel(ctx)
	worker := &backgroundWorker{cancel: cancel, done: make(chan struct{})}
	go func() {
		worker.runErr = run(runCtx)
		if errors.Is(worker.runErr, context.Canceled) {
			worker.runErr = nil
		}
		close(worker.done)
	}()
	return worker
}

func (worker *backgroundWorker) valid() bool {
	return worker != nil && worker.cancel != nil && worker.done != nil
}

// stopAndWait returns terminal=false only when the caller stops waiting before
// the worker exits. A later call may wait again without issuing another cancel.
func (worker *backgroundWorker) stopAndWait(ctx context.Context) (runErr error, terminal bool) {
	worker.stopOnce.Do(worker.cancel)
	select {
	case <-worker.done:
		return worker.runErr, true
	case <-ctx.Done():
		return ctx.Err(), false
	}
}
