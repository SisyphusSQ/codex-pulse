package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	gort "runtime"
	"time"

	"github.com/SisyphusSQ/codex-pulse/internal/metrics"
	"github.com/SisyphusSQ/codex-pulse/internal/store"
	storesqlite "github.com/SisyphusSQ/codex-pulse/internal/store/sqlite"
)

var ErrApplicationMetricsRuntime = errors.New("application metrics runtime is unavailable")

type applicationMetricsRuntime struct {
	observer  *metrics.QueryAccumulator
	collector *metrics.Collector
	worker    *backgroundWorker
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
	runtime := &applicationMetricsRuntime{
		observer: observer, collector: collector,
		worker: startBackgroundWorker(ctx, service.Run),
	}
	return runtime, nil
}

func (runtime *applicationMetricsRuntime) Observer() *metrics.QueryAccumulator {
	if runtime == nil {
		return nil
	}
	return runtime.observer
}

func (runtime *applicationMetricsRuntime) Close(ctx context.Context) error {
	if runtime == nil || !runtime.worker.valid() || ctx == nil {
		return ErrApplicationMetricsRuntime
	}
	runErr, terminal := runtime.worker.stopAndWait(ctx)
	if !terminal {
		return runErr
	}
	if runErr != nil {
		return fmt.Errorf("%w: stop metrics worker: %w", ErrApplicationMetricsRuntime, runErr)
	}
	return nil
}
