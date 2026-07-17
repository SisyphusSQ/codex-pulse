package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	gort "runtime"
	"sync"
	"time"

	"github.com/SisyphusSQ/codex-pulse/internal/metrics"
	"github.com/SisyphusSQ/codex-pulse/internal/store"
	storesqlite "github.com/SisyphusSQ/codex-pulse/internal/store/sqlite"
)

var ErrApplicationMetricsRuntime = errors.New("application metrics runtime is unavailable")

type applicationMetricsRuntime struct {
	observer   *metrics.QueryAccumulator
	collector  *metrics.Collector
	cancel     context.CancelFunc
	workerDone chan error

	closeOnce sync.Once
	closeDone chan struct{}
	closeErr  error
}

func startApplicationMetricsRuntime(
	ctx context.Context,
	database *storesqlite.Store,
	mode metrics.SamplingMode,
) (*applicationMetricsRuntime, error) {
	if ctx == nil || database == nil {
		return nil, ErrApplicationMetricsRuntime
	}
	repository := store.NewRepository(database)
	processProbe, err := metrics.NewGopsutilProcessProbe(os.Getpid())
	if err != nil {
		return nil, errors.Join(ErrApplicationMetricsRuntime, err)
	}
	storeProbe, err := metrics.NewFileStoreProbe(database.Config().Path, repository)
	if err != nil {
		return nil, errors.Join(ErrApplicationMetricsRuntime, err)
	}
	observer := &metrics.QueryAccumulator{}
	collector, err := metrics.NewCollector(metrics.CollectorConfig{
		Process: processProbe, Store: storeProbe, Sink: repository, Queries: observer,
		Clock: time.Now, GoroutineCount: gort.NumGoroutine,
	})
	if err != nil {
		return nil, errors.Join(ErrApplicationMetricsRuntime, err)
	}
	service, err := metrics.NewService(metrics.ServiceConfig{Collector: collector, Mode: mode})
	if err != nil {
		return nil, errors.Join(ErrApplicationMetricsRuntime, err)
	}
	runCtx, cancel := context.WithCancel(ctx)
	runtime := &applicationMetricsRuntime{
		observer: observer, collector: collector, cancel: cancel,
		workerDone: make(chan error, 1), closeDone: make(chan struct{}),
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

func (runtime *applicationMetricsRuntime) Observer() *metrics.QueryAccumulator {
	if runtime == nil {
		return nil
	}
	return runtime.observer
}

func (runtime *applicationMetricsRuntime) Close(ctx context.Context) error {
	if runtime == nil || runtime.cancel == nil || runtime.workerDone == nil || runtime.closeDone == nil || ctx == nil {
		return ErrApplicationMetricsRuntime
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
			return fmt.Errorf("%w: stop metrics worker: %w", ErrApplicationMetricsRuntime, runtime.closeErr)
		}
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
