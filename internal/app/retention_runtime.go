package app

import (
	"context"
	"errors"
	"fmt"
	"time"

	retentionmodel "github.com/SisyphusSQ/codex-pulse/internal/retention"
	storeretention "github.com/SisyphusSQ/codex-pulse/internal/store/retention"
	storesqlite "github.com/SisyphusSQ/codex-pulse/internal/store/sqlite"
)

var ErrApplicationRetentionRuntime = errors.New("application retention runtime is unavailable")

type applicationRetentionRuntime struct {
	service *retentionmodel.Service
	worker  *backgroundWorker
}

func startApplicationRetentionRuntime(
	ctx context.Context,
	database *storesqlite.Store,
) (*applicationRetentionRuntime, error) {
	if ctx == nil || database == nil {
		return nil, ErrApplicationRetentionRuntime
	}
	repository := storeretention.NewRepository(database)
	service, err := retentionmodel.NewService(retentionmodel.ServiceConfig{
		Cleaner: repository, Checkpointer: database, Clock: time.Now,
	})
	if err != nil {
		return nil, errors.Join(ErrApplicationRetentionRuntime, err)
	}
	runtime := &applicationRetentionRuntime{
		service: service,
		worker:  startBackgroundWorker(ctx, service.Run),
	}
	return runtime, nil
}

func (runtime *applicationRetentionRuntime) Projection() retentionmodel.Projection {
	if runtime == nil || runtime.service == nil {
		return retentionmodel.Projection{State: retentionmodel.StateNeverRun, Failure: retentionmodel.FailureNone}
	}
	return runtime.service.Projection()
}

func (runtime *applicationRetentionRuntime) Close(ctx context.Context) error {
	if runtime == nil || !runtime.worker.valid() || ctx == nil {
		return ErrApplicationRetentionRuntime
	}
	runErr, terminal := runtime.worker.stopAndWait(ctx)
	if !terminal {
		return runErr
	}
	if runErr != nil {
		return fmt.Errorf("%w: stop retention worker: %w", ErrApplicationRetentionRuntime, runErr)
	}
	return nil
}
