package app

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	retentionmodel "github.com/SisyphusSQ/codex-pulse/internal/retention"
	"github.com/SisyphusSQ/codex-pulse/internal/store"
	storesqlite "github.com/SisyphusSQ/codex-pulse/internal/store/sqlite"
)

var ErrApplicationRetentionRuntime = errors.New("application retention runtime is unavailable")

type applicationRetentionRuntime struct {
	service    *retentionmodel.Service
	cancel     context.CancelFunc
	workerDone chan error

	closeOnce sync.Once
	closeDone chan struct{}
	closeErr  error
}

func startApplicationRetentionRuntime(
	ctx context.Context,
	database *storesqlite.Store,
) (*applicationRetentionRuntime, error) {
	if ctx == nil || database == nil {
		return nil, ErrApplicationRetentionRuntime
	}
	repository := store.NewRepository(database)
	service, err := retentionmodel.NewService(retentionmodel.ServiceConfig{
		Cleaner: repository, Checkpointer: database, Clock: time.Now,
	})
	if err != nil {
		return nil, errors.Join(ErrApplicationRetentionRuntime, err)
	}
	runCtx, cancel := context.WithCancel(ctx)
	runtime := &applicationRetentionRuntime{
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

func (runtime *applicationRetentionRuntime) Projection() retentionmodel.Projection {
	if runtime == nil || runtime.service == nil {
		return retentionmodel.Projection{State: retentionmodel.StateNeverRun, Failure: retentionmodel.FailureNone}
	}
	return runtime.service.Projection()
}

func (runtime *applicationRetentionRuntime) Close(ctx context.Context) error {
	if runtime == nil || runtime.cancel == nil || runtime.workerDone == nil || runtime.closeDone == nil || ctx == nil {
		return ErrApplicationRetentionRuntime
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
			return fmt.Errorf("%w: stop retention worker: %w", ErrApplicationRetentionRuntime, runtime.closeErr)
		}
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
