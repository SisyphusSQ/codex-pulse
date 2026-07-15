package scheduler

import (
	"context"
	"errors"
	"math/rand/v2"
	"sync"
	"time"

	"github.com/google/uuid"

	retrypolicy "github.com/SisyphusSQ/codex-pulse/internal/retry"
	"github.com/SisyphusSQ/codex-pulse/internal/store"
)

var (
	ErrInvalidService     = errors.New("invalid scheduler service")
	ErrExecutorMissing    = errors.New("scheduler executor is missing")
	ErrInvalidSliceResult = errors.New("invalid scheduler slice result")
	ErrExecutorPanic      = errors.New("scheduler executor panicked")
	ErrRunAlreadyActive   = errors.New("scheduler run is already active")
	ErrSchedulerRetry     = errors.New("scheduler cycle must retry from durable queue")
	ErrSchedulerCronPanic = errors.New("scheduler cron job panicked")
)

// SliceResult 只报告已由target持久化的消费量与协作式停止原因。
type SliceResult struct {
	FilesProcessed int64
	BytesProcessed int64
	Active         time.Duration
	StopReason     store.SchedulerStopReason
}

// boundedCooperativeActive keeps scheduler accounting inside the admission
// token even when a target reaches its next cooperative checkpoint after the
// wall-clock boundary. Negative reports remain invalid for worker validation.
func boundedCooperativeActive(actual, maximum time.Duration) time.Duration {
	if actual > maximum {
		return maximum
	}
	return actual
}

// Executor 把scheduler task映射到持有业务游标的target runtime。
type Executor interface {
	ExecuteSlice(context.Context, store.SchedulerTask, ScanBudget) (SliceResult, error)
	Interrupt(context.Context, store.SchedulerTask, store.RuntimeErrorClass) error
	Recover(context.Context, store.SchedulerTask) (store.JobRun, error)
	Retry(context.Context, store.SchedulerTask) (store.JobRun, error)
}

type RetryPolicy interface {
	Delay(int) (time.Duration, bool, error)
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
	RecoveryPageSize int
	RetryPolicy      RetryPolicy
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
	recoveryPageSize int
	retryPolicy      RetryPolicy
	commitCycle      func(context.Context, store.SchedulerCycleCommit) error
	newCronRunner    cronRunnerFactory

	runMu   sync.Mutex
	cycleMu sync.Mutex
	running bool

	activityMu      sync.Mutex
	activityTask    *store.SchedulerTask
	activityChanged chan struct{}
}

func NewService(config ServiceConfig) (*Service, error) {
	if config.Repository == nil || config.MaxLiveBurst < 1 || config.MaxLiveBurst > 500 ||
		config.InterruptTimeout < 0 || config.RecoveryPageSize < 0 ||
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
	if config.RecoveryPageSize == 0 {
		config.RecoveryPageSize = 500
	}
	if config.RetryPolicy == nil {
		policy, err := retrypolicy.NewPolicy(retrypolicy.Config{
			BaseDelay: time.Second, MaxDelay: 30 * time.Second, MaxAttempts: 5,
			Jitter: rand.Float64,
		})
		if err != nil {
			return nil, ErrInvalidService
		}
		config.RetryPolicy = policy
	}
	executors := make(map[store.SchedulerTargetKind]Executor, len(config.Executors))
	for kind, executor := range config.Executors {
		executors[kind] = executor
	}
	service := &Service{
		repository: config.Repository, executors: executors, budgetPolicy: config.BudgetPolicy,
		maxLiveBurst: config.MaxLiveBurst, clock: config.Clock, newCycleID: config.NewCycleID,
		interruptTimeout: config.InterruptTimeout, systemProbe: config.SystemProbe,
		recoveryPageSize: config.RecoveryPageSize, retryPolicy: config.RetryPolicy,
		newCronRunner:   defaultCronRunnerFactory,
		activityChanged: make(chan struct{}),
	}
	service.commitCycle = service.repository.CommitSchedulerCycle
	return service, nil
}

// Drain 等待在intent落盘前已进入选择/claim窗口的受影响slice退出。新的claim由
// Store lifecycle CAS阻断；PauseBackfill不会等待或阻塞live slice。
func (service *Service) Drain(ctx context.Context, scope store.LifecyclePauseScope) error {
	if service == nil || ctx == nil ||
		(scope != store.LifecyclePauseBackfill && scope != store.LifecyclePauseAll) {
		return ErrInvalidService
	}
	for {
		service.activityMu.Lock()
		task := service.activityTask
		changed := service.activityChanged
		blocked := task != nil && (scope == store.LifecyclePauseAll || task.Lane == store.SchedulerLaneBackfill)
		service.activityMu.Unlock()
		if !blocked {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-changed:
		}
	}
}

func (service *Service) setActivity(task *store.SchedulerTask) {
	service.activityMu.Lock()
	close(service.activityChanged)
	service.activityChanged = make(chan struct{})
	if task == nil {
		service.activityTask = nil
	} else {
		copy := *task
		service.activityTask = &copy
	}
	service.activityMu.Unlock()
}

func randomCycleID() (string, error) {
	value, err := uuid.NewRandom()
	if err != nil {
		return "", err
	}
	return value.String(), nil
}
