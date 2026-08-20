package app

import (
	"context"
	"errors"
	"fmt"
	"time"

	healthmodel "github.com/SisyphusSQ/codex-pulse/internal/health"
	"github.com/SisyphusSQ/codex-pulse/internal/store"
	storesqlite "github.com/SisyphusSQ/codex-pulse/internal/store/sqlite"
)

var ErrApplicationHealthRuntime = errors.New("application health runtime is unavailable")

type applicationHealthRuntime struct {
	service *healthmodel.Service
	worker  *backgroundWorker
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
	runtime := &applicationHealthRuntime{
		service: service,
		worker:  startBackgroundWorker(ctx, service.Run),
	}
	return runtime, nil
}

func (runtime *applicationHealthRuntime) Projection() healthmodel.Projection {
	if runtime == nil || runtime.service == nil {
		return healthmodel.Projection{Stale: true, Failure: healthmodel.FailureSnapshot}
	}
	return runtime.service.Projection()
}

func (runtime *applicationHealthRuntime) Close(ctx context.Context) error {
	if runtime == nil || !runtime.worker.valid() || ctx == nil {
		return ErrApplicationHealthRuntime
	}
	runErr, terminal := runtime.worker.stopAndWait(ctx)
	if !terminal {
		return runErr
	}
	if runErr != nil {
		return fmt.Errorf("%w: stop health worker: %w", ErrApplicationHealthRuntime, runErr)
	}
	return nil
}
