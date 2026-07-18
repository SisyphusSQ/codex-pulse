package updater

import (
	"context"
	"errors"
	"math"
	"sync"
	"time"

	"github.com/SisyphusSQ/codex-pulse/internal/preferences"
	"github.com/robfig/cron/v3"
)

const (
	updateCronSpec      = "@every 1m"
	minimumSnooze       = 5 * time.Minute
	maximumSnooze       = 7 * 24 * time.Hour
	maximumCheckBackoff = 24 * time.Hour
	preferenceRetries   = 2
)

var (
	ErrInvalidCoordinator = errors.New("invalid update coordinator")
	ErrCoordinatorClosed  = errors.New("update coordinator is closed")
	ErrCoordinatorStarted = errors.New("update coordinator is already started")
	ErrPromptSuppressed   = errors.New("update prompt is suppressed")
	ErrUpdateUnavailable  = errors.New("update is unavailable")
	ErrInvalidSnooze      = errors.New("invalid update snooze duration")
)

type Trigger string

const (
	TriggerStartup   Trigger = "startup"
	TriggerScheduled Trigger = "scheduled"
	TriggerWake      Trigger = "wake"
	TriggerManual    Trigger = "manual"
)

type TriggerReason string

const (
	TriggerReasonDue         TriggerReason = "due"
	TriggerReasonManual      TriggerReason = "manual"
	TriggerReasonDisabled    TriggerReason = "disabled"
	TriggerReasonNotDue      TriggerReason = "not_due"
	TriggerReasonBusy        TriggerReason = "busy"
	TriggerReasonUnavailable TriggerReason = "unavailable"
	TriggerReasonBackoff     TriggerReason = "backoff"
)

type TriggerReceipt struct {
	Trigger     Trigger
	Accepted    bool
	Reason      TriggerReason
	CheckedAtMS *int64
}

type View struct {
	Snapshot             Snapshot
	AutoCheckEnabled     bool
	CheckIntervalSeconds int64
	SkippedVersion       *string
	SnoozeUntilMS        *int64
	LastCheckAtMS        *int64
	PromptVisible        bool
}

type PreferenceStore interface {
	LoadPreferences(context.Context) (preferences.Snapshot, error)
	CompareAndSwap(context.Context, uint64, preferences.Snapshot) error
}

type CoordinatorController interface {
	Snapshot() Snapshot
	Check() error
	Download() error
	Cancel() error
	Choose(UpdateChoice) error
	Close() error
}

type UpdateCronRunner interface {
	Start()
	Stop() context.Context
}

type UpdateCronRunnerFactory func(func()) (UpdateCronRunner, error)

type CoordinatorConfig struct {
	Store         PreferenceStore
	Controller    CoordinatorController
	Clock         func() time.Time
	NewCronRunner UpdateCronRunnerFactory
}

type Coordinator struct {
	store         PreferenceStore
	controller    CoordinatorController
	clock         func() time.Time
	newCronRunner UpdateCronRunnerFactory

	opMu                sync.Mutex
	mu                  sync.Mutex
	runner              UpdateCronRunner
	started             bool
	closed              bool
	closeDone           chan struct{}
	closeErr            error
	lastTriggerError    error
	consecutiveFailures uint8
	lastObservedPhase   Phase
}

func NewCoordinator(config CoordinatorConfig) (*Coordinator, error) {
	if config.Store == nil || config.Controller == nil {
		return nil, ErrInvalidCoordinator
	}
	if config.Clock == nil {
		config.Clock = time.Now
	}
	if config.NewCronRunner == nil {
		config.NewCronRunner = defaultUpdateCronRunner
	}
	return &Coordinator{
		store: config.Store, controller: config.Controller, clock: config.Clock,
		newCronRunner: config.NewCronRunner, closeDone: make(chan struct{}),
	}, nil
}

func (coordinator *Coordinator) Start(ctx context.Context) error {
	if coordinator == nil || ctx == nil {
		return ErrInvalidCoordinator
	}
	coordinator.mu.Lock()
	if coordinator.closed {
		coordinator.mu.Unlock()
		return ErrCoordinatorClosed
	}
	if coordinator.started {
		coordinator.mu.Unlock()
		return ErrCoordinatorStarted
	}
	runner, err := coordinator.newCronRunner(func() {
		_, triggerErr := coordinator.Trigger(ctx, TriggerScheduled)
		coordinator.recordTriggerError(triggerErr)
	})
	if err != nil {
		coordinator.mu.Unlock()
		return err
	}
	coordinator.runner = runner
	coordinator.started = true
	runner.Start()
	coordinator.mu.Unlock()
	_, triggerErr := coordinator.Trigger(ctx, TriggerStartup)
	coordinator.recordTriggerError(triggerErr)
	return nil
}

func (coordinator *Coordinator) Trigger(ctx context.Context, trigger Trigger) (TriggerReceipt, error) {
	if coordinator == nil || ctx == nil || !validTrigger(trigger) {
		return TriggerReceipt{}, ErrInvalidCoordinator
	}
	if err := ctx.Err(); err != nil {
		return TriggerReceipt{}, err
	}
	coordinator.opMu.Lock()
	defer coordinator.opMu.Unlock()
	if coordinator.isClosed() {
		return TriggerReceipt{}, ErrCoordinatorClosed
	}
	controllerSnapshot := coordinator.controller.Snapshot()
	if unavailableForCheck(controllerSnapshot) {
		return TriggerReceipt{Trigger: trigger, Reason: TriggerReasonUnavailable}, nil
	}
	if busyForCheck(controllerSnapshot) {
		return TriggerReceipt{Trigger: trigger, Reason: TriggerReasonBusy}, nil
	}

	now := coordinator.clock()
	for attempt := 0; attempt < preferenceRetries; attempt++ {
		current, err := coordinator.store.LoadPreferences(ctx)
		if err != nil {
			return TriggerReceipt{}, err
		}
		reason, accepted := evaluateTrigger(current.Updates, controllerSnapshot, trigger, now, coordinator.failureCount())
		if !accepted {
			return TriggerReceipt{Trigger: trigger, Reason: reason}, nil
		}
		checkedAt := now.UnixMilli()
		next := cloneCoordinatorPreferences(current)
		if next.Revision == math.MaxUint64 {
			return TriggerReceipt{}, preferences.ErrInvalidPreferences
		}
		next.Revision++
		next.Updates.LastCheckAtMS = &checkedAt
		if err := coordinator.store.CompareAndSwap(ctx, current.Revision, next); err != nil {
			if errors.Is(err, preferences.ErrPreferencesConflict) {
				continue
			}
			return TriggerReceipt{}, err
		}
		if err := coordinator.controller.Check(); err != nil {
			return TriggerReceipt{Trigger: trigger, Accepted: true, Reason: reason, CheckedAtMS: &checkedAt}, err
		}
		return TriggerReceipt{Trigger: trigger, Accepted: true, Reason: reason, CheckedAtMS: &checkedAt}, nil
	}
	return TriggerReceipt{}, preferences.ErrPreferencesConflict
}

func (coordinator *Coordinator) Wake(ctx context.Context) (TriggerReceipt, error) {
	return coordinator.Trigger(ctx, TriggerWake)
}

func (coordinator *Coordinator) View(ctx context.Context) (View, error) {
	if coordinator == nil || ctx == nil {
		return View{}, ErrInvalidCoordinator
	}
	current, err := coordinator.store.LoadPreferences(ctx)
	if err != nil {
		return View{}, err
	}
	snapshot := coordinator.controller.Snapshot()
	view := View{
		Snapshot: snapshot, AutoCheckEnabled: current.Updates.AutoCheckEnabled,
		CheckIntervalSeconds: current.Updates.CheckIntervalSeconds,
		SkippedVersion:       cloneString(current.Updates.SkippedVersion),
		SnoozeUntilMS:        cloneInt64(current.Updates.SnoozeUntilMS),
		LastCheckAtMS:        cloneInt64(current.Updates.LastCheckAtMS),
	}
	view.PromptVisible = promptVisible(snapshot, current.Updates, coordinator.clock())
	return view, nil
}

func (coordinator *Coordinator) Download(ctx context.Context) error {
	view, err := coordinator.View(ctx)
	if err != nil {
		return err
	}
	if !view.PromptVisible {
		return ErrPromptSuppressed
	}
	return coordinator.controller.Download()
}

func (coordinator *Coordinator) Cancel(ctx context.Context) error {
	if coordinator == nil || ctx == nil {
		return ErrInvalidCoordinator
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if coordinator.isClosed() {
		return ErrCoordinatorClosed
	}
	return coordinator.controller.Cancel()
}

func (coordinator *Coordinator) Skip(ctx context.Context, version string) error {
	if coordinator == nil || ctx == nil {
		return ErrInvalidCoordinator
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if coordinator.isClosed() {
		return ErrCoordinatorClosed
	}
	snapshot := coordinator.controller.Snapshot()
	if snapshot.Update == nil || snapshot.Update.Version == "" || snapshot.Update.Version != version {
		return ErrUpdateUnavailable
	}
	if err := coordinator.controller.Choose(UpdateChoiceSkip); err != nil {
		return err
	}
	return coordinator.updatePreferences(ctx, func(updates *preferences.UpdatePreferences) {
		updates.SkippedVersion = cloneString(&version)
		updates.SnoozeUntilMS = nil
	})
}

func (coordinator *Coordinator) Snooze(ctx context.Context, duration time.Duration) error {
	if coordinator == nil || ctx == nil {
		return ErrInvalidCoordinator
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if coordinator.isClosed() {
		return ErrCoordinatorClosed
	}
	if duration < minimumSnooze || duration > maximumSnooze {
		return ErrInvalidSnooze
	}
	if coordinator.controller.Snapshot().Update == nil {
		return ErrUpdateUnavailable
	}
	until := coordinator.clock().Add(duration).UnixMilli()
	if err := coordinator.controller.Choose(UpdateChoiceDismiss); err != nil {
		return err
	}
	return coordinator.updatePreferences(ctx, func(updates *preferences.UpdatePreferences) {
		updates.SnoozeUntilMS = &until
	})
}

// ObserveSnapshot maintains an in-memory failure streak for bounded exponential
// backoff. Preferences retain the durable last-attempt timestamp; restarting the
// app intentionally resets only the transient streak.
func (coordinator *Coordinator) ObserveSnapshot(snapshot Snapshot) {
	if coordinator == nil {
		return
	}
	coordinator.mu.Lock()
	defer coordinator.mu.Unlock()
	if transientCheckFailure(snapshot) && coordinator.lastObservedPhase != PhaseError {
		if coordinator.consecutiveFailures < 31 {
			coordinator.consecutiveFailures++
		}
	} else if snapshot.Phase == PhaseIdle || snapshot.Phase == PhaseAvailable || unavailableForCheck(snapshot) {
		coordinator.consecutiveFailures = 0
	}
	coordinator.lastObservedPhase = snapshot.Phase
}

func (coordinator *Coordinator) failureCount() uint8 {
	coordinator.mu.Lock()
	defer coordinator.mu.Unlock()
	return coordinator.consecutiveFailures
}

func (coordinator *Coordinator) Close() error {
	if coordinator == nil {
		return nil
	}
	coordinator.mu.Lock()
	if coordinator.closed {
		done := coordinator.closeDone
		coordinator.mu.Unlock()
		<-done
		coordinator.mu.Lock()
		err := coordinator.closeErr
		coordinator.mu.Unlock()
		return err
	}
	coordinator.closed = true
	runner := coordinator.runner
	coordinator.runner = nil
	coordinator.mu.Unlock()
	if runner != nil {
		<-runner.Stop().Done()
	}
	coordinator.opMu.Lock()
	err := coordinator.controller.Close()
	coordinator.opMu.Unlock()
	coordinator.mu.Lock()
	coordinator.closeErr = err
	close(coordinator.closeDone)
	coordinator.mu.Unlock()
	return err
}

func (coordinator *Coordinator) updatePreferences(ctx context.Context, mutate func(*preferences.UpdatePreferences)) error {
	if coordinator == nil || ctx == nil || mutate == nil {
		return ErrInvalidCoordinator
	}
	coordinator.opMu.Lock()
	defer coordinator.opMu.Unlock()
	if coordinator.isClosed() {
		return ErrCoordinatorClosed
	}
	for attempt := 0; attempt < preferenceRetries; attempt++ {
		current, err := coordinator.store.LoadPreferences(ctx)
		if err != nil {
			return err
		}
		next := cloneCoordinatorPreferences(current)
		if next.Revision == math.MaxUint64 {
			return preferences.ErrInvalidPreferences
		}
		next.Revision++
		mutate(&next.Updates)
		if err := coordinator.store.CompareAndSwap(ctx, current.Revision, next); err != nil {
			if errors.Is(err, preferences.ErrPreferencesConflict) {
				continue
			}
			return err
		}
		return nil
	}
	return preferences.ErrPreferencesConflict
}

func (coordinator *Coordinator) isClosed() bool {
	coordinator.mu.Lock()
	defer coordinator.mu.Unlock()
	return coordinator.closed
}

func (coordinator *Coordinator) recordTriggerError(err error) {
	if err == nil {
		return
	}
	coordinator.mu.Lock()
	coordinator.lastTriggerError = err
	coordinator.mu.Unlock()
}

func validTrigger(trigger Trigger) bool {
	return trigger == TriggerStartup || trigger == TriggerScheduled || trigger == TriggerWake || trigger == TriggerManual
}

func busyForCheck(snapshot Snapshot) bool {
	return snapshot.Closed || snapshot.Phase == PhaseChecking || snapshot.Phase == PhaseDownloading || snapshot.Phase == PhaseInstalling
}

func unavailableForCheck(snapshot Snapshot) bool {
	return snapshot.Phase == PhaseError && snapshot.Fault != nil &&
		(snapshot.Fault.Code == FaultConfiguration || snapshot.Fault.Code == FaultUnavailable)
}

func evaluateTrigger(updates preferences.UpdatePreferences, snapshot Snapshot, trigger Trigger, now time.Time, failures uint8) (TriggerReason, bool) {
	if trigger == TriggerManual {
		return TriggerReasonManual, true
	}
	if !updates.AutoCheckEnabled {
		return TriggerReasonDisabled, false
	}
	if updates.LastCheckAtMS != nil {
		interval := time.Duration(updates.CheckIntervalSeconds) * time.Second
		reason := TriggerReasonNotDue
		if transientCheckFailure(snapshot) {
			if failures == 0 {
				failures = 1
			}
			for step := uint8(0); step < failures && interval < maximumCheckBackoff; step++ {
				interval = min(interval*2, maximumCheckBackoff)
			}
			reason = TriggerReasonBackoff
		}
		due := time.UnixMilli(*updates.LastCheckAtMS).Add(interval)
		if now.Before(due) {
			return reason, false
		}
	}
	return TriggerReasonDue, true
}

func transientCheckFailure(snapshot Snapshot) bool {
	return snapshot.Phase == PhaseError && snapshot.Fault != nil &&
		snapshot.Fault.Code != FaultConfiguration && snapshot.Fault.Code != FaultUnavailable
}

func promptVisible(snapshot Snapshot, updates preferences.UpdatePreferences, now time.Time) bool {
	if snapshot.Phase != PhaseAvailable || snapshot.Update == nil {
		return false
	}
	if updates.SkippedVersion != nil && *updates.SkippedVersion == snapshot.Update.Version {
		return false
	}
	return updates.SnoozeUntilMS == nil || now.UnixMilli() >= *updates.SnoozeUntilMS
}

func cloneCoordinatorPreferences(snapshot preferences.Snapshot) preferences.Snapshot {
	clone := snapshot
	clone.Updates.SkippedVersion = cloneString(snapshot.Updates.SkippedVersion)
	clone.Updates.SnoozeUntilMS = cloneInt64(snapshot.Updates.SnoozeUntilMS)
	clone.Updates.LastCheckAtMS = cloneInt64(snapshot.Updates.LastCheckAtMS)
	return clone
}

func cloneString(value *string) *string {
	if value == nil {
		return nil
	}
	clone := *value
	return &clone
}

func cloneInt64(value *int64) *int64 {
	if value == nil {
		return nil
	}
	clone := *value
	return &clone
}

type robfigUpdateCronRunner struct{ runner *cron.Cron }

func defaultUpdateCronRunner(job func()) (UpdateCronRunner, error) {
	runner := cron.New(cron.WithChain(
		cron.SkipIfStillRunning(cron.DiscardLogger),
		cron.Recover(cron.DiscardLogger),
	))
	if _, err := runner.AddFunc(updateCronSpec, job); err != nil {
		return nil, err
	}
	return &robfigUpdateCronRunner{runner: runner}, nil
}

func (runner *robfigUpdateCronRunner) Start()                { runner.runner.Start() }
func (runner *robfigUpdateCronRunner) Stop() context.Context { return runner.runner.Stop() }
