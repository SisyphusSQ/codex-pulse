package health

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/robfig/cron/v3"

	"github.com/SisyphusSQ/codex-pulse/internal/runtimeclock"
	"github.com/SisyphusSQ/codex-pulse/internal/store"
)

const healthEvaluationCronSpec = "@every 30s"

var (
	ErrService          = errors.New("health evaluation service")
	ErrServiceRunActive = errors.New("health evaluation service is already running")
)

type SnapshotSource interface {
	HealthEvaluationSnapshot(context.Context, store.MetricsSnapshotFilter, []store.HealthManagedEvent) (store.HealthEvaluationSnapshot, error)
}

type EventBatchSink interface {
	ApplyHealthEvaluationBatch(context.Context, store.HealthEvaluationBatch) error
}

type Failure string

const (
	FailureNone     Failure = "none"
	FailureSnapshot Failure = "snapshot"
	FailureEvaluate Failure = "evaluate"
	FailurePersist  Failure = "persist"
	FailurePanic    Failure = "panic"
)

type Projection struct {
	HasValue      bool
	Stale         bool
	Failure       Failure
	EvaluatedAtMS int64
	Result        Result
}

type cronRunner interface {
	Start()
	Stop() context.Context
}

type cronRunnerFactory func(cron.Job) (cronRunner, error)

type defaultCronRunner struct{ cron *cron.Cron }

func defaultCronRunnerFactory(job cron.Job) (cronRunner, error) {
	if job == nil {
		return nil, fmt.Errorf("%w: cron job is required", ErrService)
	}
	runner := &defaultCronRunner{cron: cron.New(cron.WithChain(cron.Recover(cron.DiscardLogger)))}
	if _, err := runner.cron.AddJob(healthEvaluationCronSpec, job); err != nil {
		return nil, fmt.Errorf("%w: register cron job: %w", ErrService, err)
	}
	return runner, nil
}

func (runner *defaultCronRunner) Start()                { runner.cron.Start() }
func (runner *defaultCronRunner) Stop() context.Context { return runner.cron.Stop() }

type ServiceConfig struct {
	Source    SnapshotSource
	Sink      EventBatchSink
	Evaluator *Evaluator
	Updater   UpdaterState
	Clock     func() time.Time
}

type Service struct {
	source    SnapshotSource
	sink      EventBatchSink
	evaluator *Evaluator
	updater   UpdaterState
	clock     func() time.Time

	newCronRunner cronRunnerFactory
	evaluating    atomic.Bool
	skipped       atomic.Uint64

	runMu   sync.Mutex
	running bool

	projectionMu sync.RWMutex
	projection   Projection
}

func NewService(config ServiceConfig) (*Service, error) {
	if config.Source == nil || config.Sink == nil || config.Evaluator == nil || config.Clock == nil ||
		!validUpdaterState(config.Updater) {
		return nil, fmt.Errorf("%w: invalid configuration", ErrService)
	}
	return &Service{
		source: config.Source, sink: config.Sink, evaluator: config.Evaluator,
		updater: config.Updater, clock: config.Clock, newCronRunner: defaultCronRunnerFactory,
		projection: Projection{Stale: true, Failure: FailureSnapshot},
	}, nil
}

func (service *Service) Run(ctx context.Context) error {
	if service == nil || service.source == nil || service.sink == nil || service.evaluator == nil ||
		service.clock == nil || service.newCronRunner == nil || ctx == nil {
		return fmt.Errorf("%w: invalid service", ErrService)
	}
	service.runMu.Lock()
	if service.running {
		service.runMu.Unlock()
		return ErrServiceRunActive
	}
	jobCtx, cancelJobs := context.WithCancel(ctx)
	runner, err := service.newCronRunner(cron.FuncJob(func() { service.evaluateSafely(jobCtx) }))
	if err != nil {
		cancelJobs()
		service.runMu.Unlock()
		return err
	}
	service.running = true
	service.runMu.Unlock()
	defer func() {
		service.runMu.Lock()
		service.running = false
		service.runMu.Unlock()
	}()

	service.evaluateSafely(jobCtx)
	runner.Start()
	<-ctx.Done()
	cancelJobs()
	<-runner.Stop().Done()
	return ctx.Err()
}

func (service *Service) Projection() Projection {
	if service == nil {
		return Projection{Stale: true, Failure: FailureSnapshot}
	}
	service.projectionMu.RLock()
	defer service.projectionMu.RUnlock()
	return cloneProjection(service.projection)
}

func (service *Service) SkippedEvaluations() uint64 {
	if service == nil {
		return 0
	}
	return service.skipped.Load()
}

func (service *Service) evaluateSafely(ctx context.Context) {
	if !service.evaluating.CompareAndSwap(false, true) {
		service.skipped.Add(1)
		return
	}
	defer service.evaluating.Store(false)
	defer func() {
		if recover() != nil {
			service.markStale(FailurePanic)
		}
	}()
	atMS := service.clock().UnixMilli()
	if atMS < store.MetricsSnapshotWindowMS-1 || atMS >= runtimeclock.MaxTimestampMS {
		service.markStale(FailureSnapshot)
		return
	}
	untilMS := atMS + 1
	snapshot, err := service.source.HealthEvaluationSnapshot(ctx, store.MetricsSnapshotFilter{
		FromMS: untilMS - store.MetricsSnapshotWindowMS, UntilMS: untilMS,
	}, service.evaluator.ManagedEvents())
	if err != nil {
		service.markStale(FailureSnapshot)
		return
	}
	result, err := service.evaluator.Evaluate(Input{
		EvaluatedAtMS: atMS, Snapshot: snapshot, Updater: service.updater,
	})
	if err != nil {
		service.markStale(FailureEvaluate)
		return
	}
	if err := service.sink.ApplyHealthEvaluationBatch(ctx, result.EventBatch); err != nil {
		service.markStale(FailurePersist)
		return
	}
	service.projectionMu.Lock()
	service.projection = Projection{
		HasValue: true, Failure: FailureNone, EvaluatedAtMS: atMS, Result: cloneResult(result),
	}
	service.projectionMu.Unlock()
}

func (service *Service) markStale(failure Failure) {
	service.projectionMu.Lock()
	service.projection.Stale = true
	service.projection.Failure = failure
	service.projectionMu.Unlock()
}

func cloneProjection(value Projection) Projection {
	value.Result = cloneResult(value.Result)
	return value
}

func cloneResult(value Result) Result {
	value.Components = append([]ComponentStatus(nil), value.Components...)
	if value.Primary != nil {
		primary := *value.Primary
		value.Primary = &primary
	}
	value.EventBatch.ManagedEvents = append([]store.HealthManagedEvent(nil), value.EventBatch.ManagedEvents...)
	value.EventBatch.Observations = append([]store.HealthObservation(nil), value.EventBatch.Observations...)
	return value
}

func validUpdaterState(value UpdaterState) bool {
	switch value {
	case UpdaterCurrent, UpdaterChecking, UpdaterUnavailable, UpdaterUnknown, UpdaterNotConfigured:
		return true
	default:
		return false
	}
}
