package scheduler

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/SisyphusSQ/codex-pulse/internal/store"
)

var (
	ErrInvalidService     = errors.New("invalid scheduler service")
	ErrExecutorMissing    = errors.New("scheduler executor is missing")
	ErrInvalidSliceResult = errors.New("invalid scheduler slice result")
	ErrExecutorPanic      = errors.New("scheduler executor panicked")
	ErrRunAlreadyActive   = errors.New("scheduler run is already active")
	ErrSchedulerRetry     = errors.New("scheduler cycle must retry from durable queue")
)

// SliceResult 只报告已由target持久化的消费量与协作式停止原因。
type SliceResult struct {
	FilesProcessed int64
	BytesProcessed int64
	Active         time.Duration
	StopReason     store.SchedulerStopReason
}

// Executor 把scheduler task映射到持有业务游标的target runtime。
type Executor interface {
	ExecuteSlice(context.Context, store.SchedulerTask, ScanBudget) (SliceResult, error)
	Interrupt(context.Context, store.SchedulerTask, store.RuntimeErrorClass) error
	Recover(context.Context, store.SchedulerTask) (store.JobRun, error)
}

type SystemProbe interface {
	Snapshot(context.Context) (SystemSnapshot, error)
}

type StaticSystemProbe struct {
	Value SystemSnapshot
}

func (probe StaticSystemProbe) Snapshot(ctx context.Context) (SystemSnapshot, error) {
	if err := ctx.Err(); err != nil {
		return SystemSnapshot{}, err
	}
	return probe.Value, nil
}

type ServiceConfig struct {
	Repository       *store.Repository
	Executors        map[store.SchedulerTargetKind]Executor
	BudgetPolicy     BudgetPolicy
	MaxLiveBurst     int
	Clock            func() time.Time
	NewCycleID       func() (string, error)
	InterruptTimeout time.Duration
	SystemProbe      SystemProbe
	IdleDelay        time.Duration
	RecoveryPageSize int
}

type Service struct {
	repository       *store.Repository
	executors        map[store.SchedulerTargetKind]Executor
	budgetPolicy     BudgetPolicy
	maxLiveBurst     int
	clock            func() time.Time
	newCycleID       func() (string, error)
	interruptTimeout time.Duration
	systemProbe      SystemProbe
	idleDelay        time.Duration
	recoveryPageSize int
	commitCycle      func(context.Context, store.SchedulerCycleCommit) error

	runMu   sync.Mutex
	cycleMu sync.Mutex
	running bool
}

func NewService(config ServiceConfig) (*Service, error) {
	if config.Repository == nil || config.MaxLiveBurst < 1 || config.MaxLiveBurst > 500 ||
		config.InterruptTimeout < 0 || config.IdleDelay < 0 || config.RecoveryPageSize < 0 ||
		config.RecoveryPageSize > 500 {
		return nil, ErrInvalidService
	}
	for _, kind := range []store.SchedulerTargetKind{
		store.SchedulerTargetBootstrap, store.SchedulerTargetLiveScan,
	} {
		if config.Executors[kind] == nil {
			return nil, ErrExecutorMissing
		}
	}
	if config.Clock == nil {
		config.Clock = time.Now
	}
	if config.NewCycleID == nil {
		config.NewCycleID = randomCycleID
	}
	if config.InterruptTimeout == 0 {
		config.InterruptTimeout = 5 * time.Second
	}
	if config.SystemProbe == nil {
		config.SystemProbe = StaticSystemProbe{}
	}
	if config.IdleDelay == 0 {
		config.IdleDelay = 250 * time.Millisecond
	}
	if config.RecoveryPageSize == 0 {
		config.RecoveryPageSize = 500
	}
	executors := make(map[store.SchedulerTargetKind]Executor, len(config.Executors))
	for kind, executor := range config.Executors {
		executors[kind] = executor
	}
	service := &Service{
		repository: config.Repository, executors: executors, budgetPolicy: config.BudgetPolicy,
		maxLiveBurst: config.MaxLiveBurst, clock: config.Clock, newCycleID: config.NewCycleID,
		interruptTimeout: config.InterruptTimeout, systemProbe: config.SystemProbe,
		idleDelay: config.IdleDelay, recoveryPageSize: config.RecoveryPageSize,
	}
	service.commitCycle = service.repository.CommitSchedulerCycle
	return service, nil
}

func randomCycleID() (string, error) {
	value, err := uuid.NewRandom()
	if err != nil {
		return "", err
	}
	return value.String(), nil
}
