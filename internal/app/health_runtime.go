package app

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	healthmodel "github.com/SisyphusSQ/codex-pulse/internal/health"
	"github.com/SisyphusSQ/codex-pulse/internal/store"
	storesqlite "github.com/SisyphusSQ/codex-pulse/internal/store/sqlite"
)

var ErrApplicationHealthRuntime = errors.New("application health runtime is unavailable")

type applicationHealthRuntime struct {
	service    *healthmodel.Service
	cancel     context.CancelFunc
	workerDone chan error

	closeOnce sync.Once
	closeDone chan struct{}
	closeErr  error
}

func startApplicationHealthRuntime(
	ctx context.Context,
	database *storesqlite.Store,
) (*applicationHealthRuntime, error) {
	if ctx == nil || database == nil {
		return nil, ErrApplicationHealthRuntime
	}
	evaluator, err := healthmodel.NewEvaluator(healthmodel.DefaultThresholds())
	if err != nil {
		return nil, errors.Join(ErrApplicationHealthRuntime, err)
	}
	repository := store.NewRepository(database)
	service, err := healthmodel.NewService(healthmodel.ServiceConfig{
		Source: repository, Sink: repository, Evaluator: evaluator,
		Updater: healthmodel.UpdaterNotConfigured, Clock: time.Now,
	})
	if err != nil {
		return nil, errors.Join(ErrApplicationHealthRuntime, err)
	}
	runCtx, cancel := context.WithCancel(ctx)
	runtime := &applicationHealthRuntime{
		service: service, cancel: cancel, workerDone: make(chan error, 1), closeDone: make(chan struct{}),
	}
	go func() {
		runErr := service.Run(runCtx)
		if errors.Is(runErr, context.Canceled) {
			runErr = nil
		}
		runtime.workerDone <- runErr
	}()
	return runtime, nil
}

func (runtime *applicationHealthRuntime) Projection() healthmodel.Projection {
	if runtime == nil || runtime.service == nil {
		return healthmodel.Projection{Stale: true, Failure: healthmodel.FailureSnapshot}
	}
	return runtime.service.Projection()
}

func (runtime *applicationHealthRuntime) Close(ctx context.Context) error {
	if runtime == nil || runtime.cancel == nil || runtime.workerDone == nil || runtime.closeDone == nil || ctx == nil {
		return ErrApplicationHealthRuntime
	}
	runtime.closeOnce.Do(func() {
		runtime.cancel()
		go func() {
			runtime.closeErr = <-runtime.workerDone
			close(runtime.closeDone)
		}()
	})
	select {
	case <-runtime.closeDone:
		if runtime.closeErr != nil {
			return fmt.Errorf("%w: stop health worker: %w", ErrApplicationHealthRuntime, runtime.closeErr)
		}
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
