package retention

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/robfig/cron/v3"

	"github.com/SisyphusSQ/codex-pulse/internal/store"
	storesqlite "github.com/SisyphusSQ/codex-pulse/internal/store/sqlite"
)

const (
	retentionCronSpec = "@every 1m"
	successInterval   = time.Hour
)

var (
	ErrService          = errors.New("retention service")
	ErrServiceRunActive = errors.New("retention service is already running")
	failureBackoffs     = [...]time.Duration{5 * time.Minute, 10 * time.Minute, 20 * time.Minute, 40 * time.Minute, time.Hour}
)

type Cleaner interface {
	CleanupRetention(context.Context, store.RetentionCleanupOptions) (store.RetentionCleanupReport, error)
}

type Checkpointer interface {
	CheckpointWAL(context.Context) (storesqlite.WALCheckpointReport, error)
}

type State string

const (
	StateNeverRun  State = "never_run"
	StateRunning   State = "running"
	StateSucceeded State = "succeeded"
	StateFailed    State = "failed"
)

type Failure string

const (
	FailureNone       Failure = "none"
	FailureCleanup    Failure = "cleanup"
	FailureCheckpoint Failure = "checkpoint"
	FailurePanic      Failure = "panic"
)

// Attempt is a finite, queryable record of one in-process cleanup attempt. It
// deliberately excludes raw dependency and panic text.
type Attempt struct {
	StartedAtMS         int64
	FinishedAtMS        int64
	DurationMS          int64
	CutoffMS            int64
	Batches             int
	Deleted             store.RetentionDeletedCounts
	Checkpoint          storesqlite.WALCheckpointReport
	CheckpointCompleted bool
}

type Projection struct {
	State               State
	Failure             Failure
	Attempt             Attempt
	LastSuccess         *Attempt
	ConsecutiveFailures int
	NextDueAtMS         int64
	SkippedRuns         uint64
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
	if _, err := runner.cron.AddJob(retentionCronSpec, job); err != nil {
		return nil, fmt.Errorf("%w: register cron job: %w", ErrService, err)
	}
	return runner, nil
}

func (runner *defaultCronRunner) Start()                { runner.cron.Start() }
func (runner *defaultCronRunner) Stop() context.Context { return runner.cron.Stop() }

type ServiceConfig struct {
	Cleaner      Cleaner
	Checkpointer Checkpointer
	Clock        func() time.Time
}

type Service struct {
	cleaner       Cleaner
	checkpointer  Checkpointer
	clock         func() time.Time
	fallbackClock func() time.Time

	newCronRunner cronRunnerFactory
	active        atomic.Bool

	runMu   sync.Mutex
	running bool

	projectionMu sync.RWMutex
	projection   Projection
}

func NewService(config ServiceConfig) (*Service, error) {
	if config.Cleaner == nil || config.Checkpointer == nil || config.Clock == nil {
		return nil, fmt.Errorf("%w: invalid configuration", ErrService)
	}
	return &Service{
		cleaner: config.Cleaner, checkpointer: config.Checkpointer, clock: config.Clock,
		fallbackClock: time.Now,
		newCronRunner: defaultCronRunnerFactory,
		projection:    Projection{State: StateNeverRun, Failure: FailureNone},
	}, nil
}

func (service *Service) Run(ctx context.Context) error {
	if service == nil || service.cleaner == nil || service.checkpointer == nil || service.clock == nil ||
		service.newCronRunner == nil || ctx == nil {
		return fmt.Errorf("%w: invalid service", ErrService)
	}
	service.runMu.Lock()
	if service.running {
		service.runMu.Unlock()
		return ErrServiceRunActive
	}
	jobCtx, cancelJobs := context.WithCancel(ctx)
	runner, err := service.newCronRunner(cron.FuncJob(func() { service.runIfDue(jobCtx, false) }))
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

	service.runIfDue(jobCtx, true)
	runner.Start()
	<-ctx.Done()
	cancelJobs()
	<-runner.Stop().Done()
	return ctx.Err()
}

func (service *Service) Projection() Projection {
	if service == nil {
		return Projection{State: StateNeverRun, Failure: FailureNone}
	}
	service.projectionMu.RLock()
	defer service.projectionMu.RUnlock()
	return cloneProjection(service.projection)
}

func (service *Service) runIfDue(ctx context.Context, force bool) {
	if !service.active.CompareAndSwap(false, true) {
		service.projectionMu.Lock()
		service.projection.SkippedRuns++
		service.projectionMu.Unlock()
		return
	}
	defer service.active.Store(false)

	now, clockPanicked := service.readClock()
	attempt := Attempt{StartedAtMS: now.UnixMilli()}
	if clockPanicked {
		attempt.FinishedAtMS = attempt.StartedAtMS
		service.markRunning(attempt)
		service.publishAttempt(attempt, FailurePanic)
		return
	}
	service.projectionMu.RLock()
	nextDueAtMS := service.projection.NextDueAtMS
	service.projectionMu.RUnlock()
	if !force && nextDueAtMS > 0 && attempt.StartedAtMS < nextDueAtMS {
		return
	}
	service.markRunning(attempt)

	failure := service.executeAttempt(ctx, now, &attempt)
	finishedAt, finishClockPanicked := service.readClock()
	if finishedAt.Before(now) {
		finishedAt = now
	}
	attempt.FinishedAtMS = finishedAt.UnixMilli()
	attempt.DurationMS = attempt.FinishedAtMS - attempt.StartedAtMS
	if finishClockPanicked {
		failure = FailurePanic
	}
	service.publishAttempt(attempt, failure)
}

func (service *Service) markRunning(attempt Attempt) {
	service.projectionMu.Lock()
	service.projection.State = StateRunning
	service.projection.Failure = FailureNone
	service.projection.Attempt = attempt
	service.projectionMu.Unlock()
}

func (service *Service) readClock() (value time.Time, panicked bool) {
	value = service.fallbackClock().UTC()
	defer func() {
		if recover() != nil {
			panicked = true
		}
	}()
	value = service.clock().UTC()
	return value, false
}

func (service *Service) executeAttempt(ctx context.Context, now time.Time, attempt *Attempt) (failure Failure) {
	failure = FailureNone
	defer func() {
		if recover() != nil {
			failure = FailurePanic
		}
	}()
	report, err := service.cleaner.CleanupRetention(ctx, store.RetentionCleanupOptions{
		Now: now, BatchSize: store.DefaultRetentionBatchSize,
	})
	attempt.CutoffMS = report.CutoffMS
	attempt.Batches = report.Batches
	attempt.Deleted = report.Deleted
	if err != nil {
		return FailureCleanup
	}
	checkpoint, err := service.checkpointer.CheckpointWAL(ctx)
	attempt.Checkpoint = checkpoint
	if err != nil {
		return FailureCheckpoint
	}
	attempt.CheckpointCompleted = true
	return FailureNone
}

func (service *Service) publishAttempt(attempt Attempt, failure Failure) {
	service.projectionMu.Lock()
	defer service.projectionMu.Unlock()
	service.projection.Attempt = attempt
	service.projection.Failure = failure
	if failure == FailureNone {
		service.projection.State = StateSucceeded
		service.projection.ConsecutiveFailures = 0
		lastSuccess := attempt
		service.projection.LastSuccess = &lastSuccess
		service.projection.NextDueAtMS = time.UnixMilli(attempt.FinishedAtMS).Add(successInterval).UnixMilli()
		return
	}
	service.projection.State = StateFailed
	service.projection.ConsecutiveFailures++
	backoffIndex := min(service.projection.ConsecutiveFailures-1, len(failureBackoffs)-1)
	service.projection.NextDueAtMS = time.UnixMilli(attempt.FinishedAtMS).Add(failureBackoffs[backoffIndex]).UnixMilli()
}

func cloneProjection(value Projection) Projection {
	if value.LastSuccess != nil {
		lastSuccess := *value.LastSuccess
		value.LastSuccess = &lastSuccess
	}
	return value
}
