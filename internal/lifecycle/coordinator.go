package lifecycle

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"sync"
	"time"

	"github.com/SisyphusSQ/codex-pulse/internal/runtimeclock"
	"github.com/SisyphusSQ/codex-pulse/internal/store"
)

var (
	ErrInvalidCoordinator = errors.New("invalid lifecycle coordinator")
	ErrCoordinatorClosed  = errors.New("lifecycle coordinator is closed")
	ErrGenerationChanged  = errors.New("confirmed Home generation changed during reconcile")
	ErrSourceUnavailable  = errors.New("confirmed Home source is unavailable")
)

type ReconcileReason string

const (
	ReconcileUserResume   ReconcileReason = "user_resume"
	ReconcileSystemWake   ReconcileReason = "system_wake"
	ReconcileSourceChange ReconcileReason = "source_change"
	ReconcileStartup      ReconcileReason = "startup"
)

type ReconcileResult struct {
	HomeGeneration int64
	SourceState    store.LifecycleSourceState
}

type SchedulerControl interface {
	Drain(context.Context, store.LifecyclePauseScope) error
	RecoverActiveTasks(context.Context) ([]store.SchedulerTask, error)
}

type Reconciler interface {
	Reconcile(context.Context, store.SchedulerLifecycle, ReconcileReason) (ReconcileResult, error)
}

type Config struct {
	Repository *store.Repository
	Scheduler  SchedulerControl
	Reconciler Reconciler
	Clock      func() time.Time
}

type commandKind uint8

const (
	commandInitialize commandKind = iota + 1
	commandPause
	commandResume
	commandSleep
	commandWake
	commandSourceChanged
	commandRecover
)

type command struct {
	ctx        context.Context
	kind       commandKind
	eventID    string
	generation int64
	scope      store.LifecyclePauseScope
	available  bool
	response   chan commandResult
}

type commandResult struct {
	state store.SchedulerLifecycle
	err   error
}

// Coordinator 通过一个私有event loop线性化用户、系统、来源与startup事件。
// Wails callback和UI调用者只等待自己的response，不直接操作scheduler或Store。
type Coordinator struct {
	repository *store.Repository
	scheduler  SchedulerControl
	reconciler Reconciler
	clock      func() time.Time

	commands  chan command
	stop      chan struct{}
	done      chan struct{}
	closeOnce sync.Once
}

func NewCoordinator(config Config) (*Coordinator, error) {
	if config.Repository == nil || config.Scheduler == nil || config.Reconciler == nil {
		return nil, ErrInvalidCoordinator
	}
	if config.Clock == nil {
		config.Clock = time.Now
	}
	coordinator := &Coordinator{
		repository: config.Repository, scheduler: config.Scheduler,
		reconciler: config.Reconciler, clock: config.Clock,
		commands: make(chan command, 32), stop: make(chan struct{}), done: make(chan struct{}),
	}
	go coordinator.run()
	return coordinator, nil
}

func (coordinator *Coordinator) Initialize(
	ctx context.Context,
	generation int64,
	eventID string,
) (store.SchedulerLifecycle, error) {
	return coordinator.submit(command{ctx: ctx, kind: commandInitialize, generation: generation, eventID: eventID})
}

func (coordinator *Coordinator) Pause(
	ctx context.Context,
	eventID string,
	scope store.LifecyclePauseScope,
) (store.SchedulerLifecycle, error) {
	return coordinator.submit(command{ctx: ctx, kind: commandPause, eventID: eventID, scope: scope})
}

func (coordinator *Coordinator) Resume(
	ctx context.Context,
	eventID string,
) (store.SchedulerLifecycle, error) {
	return coordinator.submit(command{ctx: ctx, kind: commandResume, eventID: eventID})
}

func (coordinator *Coordinator) SystemWillSleep(
	ctx context.Context,
	eventID string,
) (store.SchedulerLifecycle, error) {
	return coordinator.submit(command{ctx: ctx, kind: commandSleep, eventID: eventID})
}

func (coordinator *Coordinator) SystemDidWake(
	ctx context.Context,
	eventID string,
) (store.SchedulerLifecycle, error) {
	return coordinator.submit(command{ctx: ctx, kind: commandWake, eventID: eventID})
}

func (coordinator *Coordinator) SourceChanged(
	ctx context.Context,
	eventID string,
	available bool,
) (store.SchedulerLifecycle, error) {
	return coordinator.submit(command{ctx: ctx, kind: commandSourceChanged, eventID: eventID, available: available})
}

func (coordinator *Coordinator) Recover(
	ctx context.Context,
	eventID string,
) (store.SchedulerLifecycle, error) {
	return coordinator.submit(command{ctx: ctx, kind: commandRecover, eventID: eventID})
}

func (coordinator *Coordinator) Close(ctx context.Context) error {
	if coordinator == nil || ctx == nil {
		return ErrInvalidCoordinator
	}
	coordinator.closeOnce.Do(func() { close(coordinator.stop) })
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-coordinator.done:
		return nil
	}
}

func (coordinator *Coordinator) submit(value command) (store.SchedulerLifecycle, error) {
	if coordinator == nil || value.ctx == nil || !validEventID(value.eventID) {
		return store.SchedulerLifecycle{}, ErrInvalidCoordinator
	}
	value.response = make(chan commandResult, 1)
	select {
	case <-value.ctx.Done():
		return store.SchedulerLifecycle{}, value.ctx.Err()
	case <-coordinator.stop:
		return store.SchedulerLifecycle{}, ErrCoordinatorClosed
	case coordinator.commands <- value:
	}
	select {
	case <-value.ctx.Done():
		return store.SchedulerLifecycle{}, value.ctx.Err()
	case <-coordinator.stop:
		return store.SchedulerLifecycle{}, ErrCoordinatorClosed
	case result := <-value.response:
		return result.state, result.err
	}
}

func (coordinator *Coordinator) run() {
	defer close(coordinator.done)
	for {
		select {
		case <-coordinator.stop:
			return
		case value := <-coordinator.commands:
			state, err := coordinator.execute(value)
			value.response <- commandResult{state: state, err: err}
		}
	}
}

func (coordinator *Coordinator) execute(value command) (store.SchedulerLifecycle, error) {
	if err := value.ctx.Err(); err != nil {
		return store.SchedulerLifecycle{}, err
	}
	if value.kind == commandInitialize {
		if value.generation < 0 {
			return store.SchedulerLifecycle{}, ErrInvalidCoordinator
		}
		return coordinator.repository.InitializeSchedulerLifecycle(value.ctx, store.SchedulerLifecycle{
			HomeGeneration: value.generation, UserPauseScope: store.LifecyclePauseNone,
			SystemState: store.LifecycleSystemAwake, Transition: store.LifecycleTransitionSteady,
			SourceState: store.LifecycleSourceAvailable, LastEventID: value.eventID,
			Revision: 1, UpdatedAtMS: coordinator.clock().UnixMilli(),
		})
	}
	current, err := coordinator.repository.SchedulerLifecycle(value.ctx)
	if err != nil {
		return store.SchedulerLifecycle{}, err
	}
	switch value.kind {
	case commandPause:
		return coordinator.pause(value.ctx, current, value.eventID, value.scope)
	case commandResume:
		return coordinator.resume(value.ctx, current, value.eventID)
	case commandSleep:
		return coordinator.sleep(value.ctx, current, value.eventID)
	case commandWake:
		return coordinator.reconcile(value.ctx, current, value.eventID, ReconcileSystemWake, true)
	case commandSourceChanged:
		return coordinator.sourceChanged(value.ctx, current, value.eventID, value.available)
	case commandRecover:
		return coordinator.recover(value.ctx, current, value.eventID)
	default:
		return store.SchedulerLifecycle{}, ErrInvalidCoordinator
	}
}

func (coordinator *Coordinator) pause(
	ctx context.Context,
	current store.SchedulerLifecycle,
	eventID string,
	scope store.LifecyclePauseScope,
) (store.SchedulerLifecycle, error) {
	if scope != store.LifecyclePauseBackfill && scope != store.LifecyclePauseAll {
		return current, ErrInvalidCoordinator
	}
	if current.UserPauseScope == scope && current.Transition == store.LifecycleTransitionSteady &&
		eventApplied(current.LastEventID, eventID) {
		return current, nil
	}
	transition := store.LifecycleTransitionSteady
	if scope == store.LifecyclePauseAll {
		transition = store.LifecycleTransitionDraining
	}
	intent, err := coordinator.update(ctx, current, eventID+":intent", func(next *store.SchedulerLifecycle) {
		next.UserPauseScope = scope
		next.Transition = transition
	})
	if err != nil {
		return current, err
	}
	if err := coordinator.scheduler.Drain(ctx, scope); err != nil {
		return intent, err
	}
	if transition == store.LifecycleTransitionSteady {
		return intent, nil
	}
	return coordinator.update(ctx, intent, eventID+":complete", func(next *store.SchedulerLifecycle) {
		next.Transition = store.LifecycleTransitionSteady
	})
}

func (coordinator *Coordinator) resume(
	ctx context.Context,
	current store.SchedulerLifecycle,
	eventID string,
) (store.SchedulerLifecycle, error) {
	if current.UserPauseScope == store.LifecyclePauseNone && current.SystemState == store.LifecycleSystemAwake &&
		current.Transition == store.LifecycleTransitionSteady &&
		current.SourceState == store.LifecycleSourceAvailable && eventApplied(current.LastEventID, eventID) {
		return current, nil
	}
	if current.SystemState == store.LifecycleSystemSleeping {
		return coordinator.update(ctx, current, eventID+":complete", func(next *store.SchedulerLifecycle) {
			next.UserPauseScope = store.LifecyclePauseNone
			next.Transition = store.LifecycleTransitionSteady
		})
	}
	return coordinator.reconcile(ctx, current, eventID, ReconcileUserResume, false)
}

func (coordinator *Coordinator) sleep(
	ctx context.Context,
	current store.SchedulerLifecycle,
	eventID string,
) (store.SchedulerLifecycle, error) {
	if current.SystemState == store.LifecycleSystemSleeping &&
		current.Transition == store.LifecycleTransitionSteady && eventApplied(current.LastEventID, eventID) {
		return current, nil
	}
	intent, err := coordinator.update(ctx, current, eventID+":intent", func(next *store.SchedulerLifecycle) {
		next.SystemState = store.LifecycleSystemSleeping
		next.Transition = store.LifecycleTransitionDraining
	})
	if err != nil {
		return current, err
	}
	if err := coordinator.scheduler.Drain(ctx, store.LifecyclePauseAll); err != nil {
		return intent, err
	}
	return coordinator.update(ctx, intent, eventID+":complete", func(next *store.SchedulerLifecycle) {
		next.Transition = store.LifecycleTransitionSteady
	})
}

func (coordinator *Coordinator) sourceChanged(
	ctx context.Context,
	current store.SchedulerLifecycle,
	eventID string,
	available bool,
) (store.SchedulerLifecycle, error) {
	if available {
		if current.SystemState == store.LifecycleSystemSleeping {
			if eventTerminal(current.LastEventID, eventID) {
				return current, nil
			}
			return coordinator.update(ctx, current, eventID+":deferred", func(next *store.SchedulerLifecycle) {
				next.SourceState = store.LifecycleSourceUnknown
			})
		}
		return coordinator.reconcile(ctx, current, eventID, ReconcileSourceChange, false)
	}
	if current.SourceState == store.LifecycleSourceUnavailable &&
		current.Transition == store.LifecycleTransitionBlocked && eventTerminal(current.LastEventID, eventID) {
		return current, nil
	}
	blocked, err := coordinator.update(ctx, current, eventID+":blocked", func(next *store.SchedulerLifecycle) {
		next.SourceState = store.LifecycleSourceUnavailable
		next.Transition = store.LifecycleTransitionBlocked
	})
	if err != nil {
		return current, err
	}
	if err := coordinator.scheduler.Drain(ctx, store.LifecyclePauseAll); err != nil {
		return blocked, err
	}
	return blocked, nil
}

func (coordinator *Coordinator) reconcile(
	ctx context.Context,
	current store.SchedulerLifecycle,
	eventID string,
	reason ReconcileReason,
	wake bool,
) (store.SchedulerLifecycle, error) {
	if eventTerminal(current.LastEventID, eventID) &&
		(current.Transition == store.LifecycleTransitionBlocked ||
			current.SourceState == store.LifecycleSourceUnavailable) {
		return current, ErrSourceUnavailable
	}
	if eventApplied(current.LastEventID, eventID) &&
		current.Transition == store.LifecycleTransitionSteady &&
		current.SourceState == store.LifecycleSourceAvailable &&
		(!wake || current.SystemState == store.LifecycleSystemAwake) &&
		(reason != ReconcileUserResume || current.UserPauseScope == store.LifecyclePauseNone) {
		return current, nil
	}
	intent, err := coordinator.update(ctx, current, eventID+":intent", func(next *store.SchedulerLifecycle) {
		if wake {
			next.SystemState = store.LifecycleSystemAwake
		}
		if reason == ReconcileUserResume {
			next.UserPauseScope = store.LifecyclePauseNone
		}
		next.SourceState = store.LifecycleSourceUnknown
		next.Transition = store.LifecycleTransitionReconciling
	})
	if err != nil {
		return current, err
	}
	result, reconcileErr := coordinator.reconciler.Reconcile(ctx, intent, reason)
	if reconcileErr == nil && (result.HomeGeneration != intent.HomeGeneration ||
		result.SourceState != store.LifecycleSourceAvailable) {
		if result.HomeGeneration != intent.HomeGeneration {
			reconcileErr = ErrGenerationChanged
		} else {
			reconcileErr = ErrSourceUnavailable
		}
	}
	if reconcileErr != nil {
		blocked, persistErr := coordinator.update(ctx, intent, eventID+":blocked", func(next *store.SchedulerLifecycle) {
			next.SourceState = store.LifecycleSourceUnavailable
			next.Transition = store.LifecycleTransitionBlocked
		})
		return blocked, errors.Join(reconcileErr, persistErr)
	}
	return coordinator.update(ctx, intent, eventID+":complete", func(next *store.SchedulerLifecycle) {
		next.SourceState = store.LifecycleSourceAvailable
		next.Transition = store.LifecycleTransitionSteady
	})
}

func (coordinator *Coordinator) recover(
	ctx context.Context,
	current store.SchedulerLifecycle,
	eventID string,
) (store.SchedulerLifecycle, error) {
	var err error
	if current.Transition == store.LifecycleTransitionDraining {
		scope := current.UserPauseScope
		if scope != store.LifecyclePauseBackfill || current.SystemState == store.LifecycleSystemSleeping ||
			current.SourceState == store.LifecycleSourceUnavailable {
			scope = store.LifecyclePauseAll
		}
		if err = coordinator.scheduler.Drain(ctx, scope); err != nil {
			return current, err
		}
		current, err = coordinator.update(ctx, current, eventID+":drained", func(next *store.SchedulerLifecycle) {
			if next.SourceState == store.LifecycleSourceUnavailable {
				next.Transition = store.LifecycleTransitionBlocked
			} else {
				next.Transition = store.LifecycleTransitionSteady
			}
		})
		if err != nil {
			return current, err
		}
	}
	if current.Transition == store.LifecycleTransitionReconciling &&
		current.SystemState == store.LifecycleSystemAwake {
		current, err = coordinator.reconcile(ctx, current, eventID, ReconcileStartup, false)
		if err != nil {
			return current, err
		}
	}
	if current.SystemState == store.LifecycleSystemAwake &&
		current.Transition == store.LifecycleTransitionSteady &&
		current.SourceState == store.LifecycleSourceAvailable &&
		current.UserPauseScope != store.LifecyclePauseAll {
		_, err = coordinator.scheduler.RecoverActiveTasks(ctx)
	}
	return current, err
}

func (coordinator *Coordinator) update(
	ctx context.Context,
	current store.SchedulerLifecycle,
	eventID string,
	mutate func(*store.SchedulerLifecycle),
) (store.SchedulerLifecycle, error) {
	next := current
	mutate(&next)
	next.LastEventID = eventID
	if current.Revision == math.MaxInt64 {
		return current, ErrInvalidCoordinator
	}
	next.Revision = current.Revision + 1
	updatedAtMS, ok := runtimeclock.After(
		coordinator.clock().UnixMilli(), current.UpdatedAtMS, runtimeclock.MaxTimestampMS,
	)
	if !ok {
		return current, ErrInvalidCoordinator
	}
	next.UpdatedAtMS = updatedAtMS
	stored, err := coordinator.repository.CompareAndSwapSchedulerLifecycle(ctx, current.Revision, next)
	if err != nil {
		return current, fmt.Errorf("persist lifecycle event %q: %w", eventID, err)
	}
	return stored, nil
}

func validEventID(value string) bool {
	return value != "" && len(value) <= 200 && !strings.ContainsAny(value, "\r\n")
}

func eventApplied(lastEventID string, eventID string) bool {
	return lastEventID == eventID || strings.HasPrefix(lastEventID, eventID+":")
}

func eventTerminal(lastEventID string, eventID string) bool {
	if lastEventID == eventID {
		return true
	}
	if !strings.HasPrefix(lastEventID, eventID+":") {
		return false
	}
	suffix := strings.TrimPrefix(lastEventID, eventID+":")
	return suffix == "complete" || suffix == "blocked" || suffix == "deferred"
}
