package scheduler

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sync"
	"time"

	"github.com/robfig/cron/v3"

	"github.com/SisyphusSQ/codex-pulse/internal/runtimeclock"
	"github.com/SisyphusSQ/codex-pulse/internal/store"
)

// CycleResult 是一次已持久化worker cycle的readback与下一次协作等待时间。
type CycleResult struct {
	Cycle    store.SchedulerCycle
	YieldFor time.Duration
}

// Run 先接管遗留task并立即推进一次，再由robfig cron周期触发后续有界cycle。
func (service *Service) Run(ctx context.Context) error {
	if ctx == nil || service == nil || service.repository == nil || service.systemProbe == nil ||
		service.newCronRunner == nil {
		return ErrInvalidService
	}
	service.runMu.Lock()
	if service.running {
		service.runMu.Unlock()
		return ErrRunAlreadyActive
	}
	service.running = true
	service.runMu.Unlock()
	defer func() {
		service.runMu.Lock()
		service.running = false
		service.runMu.Unlock()
	}()

	jobCtx, cancelJobs := context.WithCancel(ctx)
	defer cancelJobs()
	fatalErrors := make(chan error, 1)
	var fatalOnce sync.Once
	reportFatal := func(err error) {
		if err == nil {
			return
		}
		fatalOnce.Do(func() {
			cancelJobs()
			fatalErrors <- err
		})
	}
	job := cron.FuncJob(func() {
		defer func() {
			if recover() != nil {
				reportFatal(ErrSchedulerCronPanic)
			}
		}()
		reportFatal(service.runScheduledCycle(jobCtx))
	})
	runner, err := service.newCronRunner(job)
	if err != nil {
		return err
	}
	ownerLease, err := service.repository.AcquireSchedulerOwner(jobCtx)
	if err != nil {
		return err
	}
	defer ownerLease.Release()
	if _, err := service.recoverActiveTasksSerialized(jobCtx); err != nil {
		return err
	}
	if err := service.runScheduledCycle(jobCtx); err != nil {
		return err
	}
	runner.Start()
	var cause error
	select {
	case <-ctx.Done():
		cause = ctx.Err()
	case cause = <-fatalErrors:
	}
	<-runner.Stop().Done()
	return cause
}

func (service *Service) runScheduledCycle(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	system, err := service.systemProbe.Snapshot(ctx)
	if err != nil {
		return err
	}
	result, err := service.runCycleSerialized(ctx, system)
	if errors.Is(err, ErrQueueEmpty) || errors.Is(err, ErrSchedulerRetry) {
		return nil
	}
	if err != nil && result.Cycle.Outcome == store.SchedulerCycleFailed {
		return nil
	}
	return err
}

// RecoverActiveTasks 接管上次进程遗留的running/interrupted target，并把新attempt移到队尾。
func (service *Service) RecoverActiveTasks(ctx context.Context) ([]store.SchedulerTask, error) {
	if service == nil || service.repository == nil {
		return nil, ErrInvalidService
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	ownerLease, err := service.repository.AcquireSchedulerOwner(ctx)
	if err != nil {
		return nil, err
	}
	defer ownerLease.Release()
	return service.recoverActiveTasksSerialized(ctx)
}

func (service *Service) recoverActiveTasksSerialized(ctx context.Context) ([]store.SchedulerTask, error) {
	service.cycleMu.Lock()
	defer service.cycleMu.Unlock()
	service.setPreflightActivity(true)
	defer service.setPreflightActivity(false)
	return service.recoverActiveTasksOwned(ctx)
}

func (service *Service) recoverActiveTasksOwned(ctx context.Context) ([]store.SchedulerTask, error) {
	recovered := make([]store.SchedulerTask, 0)
	lifecycle, err := service.repository.SchedulerLifecycle(ctx)
	if err != nil {
		return recovered, err
	}
	var cursor *store.SchedulerTaskCursor
	for {
		tasks, next, err := service.repository.ListRecoverableSchedulerTasks(
			ctx, cursor, service.recoveryPageSize,
		)
		if err != nil {
			return recovered, err
		}
		if len(tasks) == 0 {
			return recovered, nil
		}
		for _, task := range tasks {
			if err := ctx.Err(); err != nil {
				return recovered, err
			}
			if !schedulerLifecyclePermits(lifecycle, task) {
				continue
			}
			executor := service.executors[task.TargetKind]
			if executor == nil {
				return recovered, ErrExecutorMissing
			}
			target, err := executor.Recover(ctx, task)
			if err != nil {
				return recovered, fmt.Errorf("recover scheduler target: %w", err)
			}
			if target.JobID == "" {
				return recovered, ErrInvalidService
			}
			var value store.SchedulerTask
			switch target.State {
			case store.JobQueued:
				snapshot, err := service.repository.SchedulerQueueSnapshot(ctx)
				if err != nil {
					return recovered, err
				}
				atMS, err := service.afterMS(maxInt64(
					task.UpdatedAtMS,
					maxInt64(task.QueueOrderMS, snapshot.MaxQueueOrderMS),
				), store.MaxSchedulerQueuedTimestampMS)
				if err != nil {
					return recovered, err
				}
				value, err = service.repository.RecoverSchedulerTask(
					ctx, task.TaskID, task.TargetID, target.JobID, atMS, atMS,
				)
				if errors.Is(err, store.ErrSchedulerPaused) {
					continue
				}
				if err != nil {
					return recovered, err
				}
			case store.JobSucceeded, store.JobFailed, store.JobCancelled, store.JobInterrupted:
				value, err = service.reconcileTerminalTarget(ctx, task, target)
				if err != nil {
					return recovered, err
				}
			default:
				return recovered, ErrInvalidService
			}
			recovered = append(recovered, value)
		}
		cursor = next
		if cursor == nil {
			return recovered, nil
		}
	}
}

func (service *Service) RunCycle(
	ctx context.Context,
	system SystemSnapshot,
) (CycleResult, error) {
	if service == nil || service.repository == nil {
		return CycleResult{}, ErrInvalidService
	}
	if err := ctx.Err(); err != nil {
		return CycleResult{}, err
	}
	ownerLease, err := service.repository.TryAcquireSchedulerOwner(ctx)
	if err != nil {
		if errors.Is(err, store.ErrSchedulerOwnerBusy) {
			return CycleResult{}, errors.Join(ErrSchedulerRetry, err)
		}
		return CycleResult{}, err
	}
	defer ownerLease.Release()
	return service.runCycleSerialized(ctx, system)
}

func (service *Service) runCycleSerialized(
	ctx context.Context,
	system SystemSnapshot,
) (CycleResult, error) {
	service.cycleMu.Lock()
	defer service.cycleMu.Unlock()
	return service.runCycleOwned(ctx, system)
}

func (service *Service) runCycleOwned(
	ctx context.Context,
	system SystemSnapshot,
) (CycleResult, error) {
	preflightActive := true
	service.setPreflightActivity(true)
	defer func() {
		if preflightActive {
			service.setPreflightActivity(false)
		}
	}()
	// Recovery is cycle-local, not startup-only: a process may restart while a
	// durable pause masks running/interrupted tasks, then resume without another
	// restart. Rechecking here makes the first permitted cycle adopt them.
	if _, err := service.recoverActiveTasksOwned(ctx); err != nil {
		return CycleResult{}, err
	}
	if _, err := service.resumeDueRetryOwned(ctx); err != nil {
		return CycleResult{}, err
	}
	service.setPreflightActivity(false)
	preflightActive = false
	queueSnapshot, err := service.repository.SchedulerRunnableQueueSnapshot(ctx)
	if err != nil {
		return CycleResult{}, err
	}
	recentCycles, err := service.repository.ListSchedulerCycles(ctx, store.SchedulerCycleFilter{
		Limit: service.maxLiveBurst,
	})
	if err != nil {
		return CycleResult{}, err
	}
	snapshotAtMS := service.logicalQueueSnapshotMS(queueSnapshot)
	selection, err := selectQueueSnapshot(queueSnapshot, recentCycles, service.maxLiveBurst, snapshotAtMS)
	if err != nil {
		return CycleResult{}, err
	}
	budget, err := service.budgetPolicy.Resolve(selection.Task.ServiceClass, system)
	if err != nil {
		return CycleResult{}, err
	}
	executor := service.executors[selection.Task.TargetKind]
	if executor == nil {
		return CycleResult{}, ErrExecutorMissing
	}
	cycleID, err := service.newCycleID()
	if err != nil || cycleID == "" {
		if err == nil {
			err = ErrInvalidService
		}
		return CycleResult{}, err
	}

	startedAtMS, err := service.afterMS(selection.Task.UpdatedAtMS, store.MaxSchedulerRunningTimestampMS)
	if err != nil {
		return CycleResult{}, err
	}
	service.setActivity(&selection.Task)
	defer service.setActivity(nil)
	claimed, err := service.repository.ClaimSchedulerTask(ctx, selection.Task.TaskID, startedAtMS)
	if err != nil {
		if errors.Is(err, store.ErrSchedulerBusy) || errors.Is(err, store.ErrSchedulerTransition) {
			return CycleResult{}, errors.Join(ErrSchedulerRetry, err)
		}
		return CycleResult{}, err
	}
	result := SliceResult{StopReason: store.SchedulerStopSystemPressure}
	var executeErr error
	panicked := false
	persistenceExhausted := !sliceHasQueuedCommitHeadroom(startedAtMS, budget) ||
		!taskHasSliceCommitHeadroom(claimed, budget)
	if !budget.Blocked && !persistenceExhausted {
		result, executeErr, panicked = executeSliceSafely(ctx, executor, claimed, budget)
	}
	commitSnapshot := queueSnapshot
	if ctx.Err() == nil {
		postSnapshot, listErr := service.repository.SchedulerRunnableQueueSnapshot(ctx)
		if listErr != nil {
			return CycleResult{}, listErr
		}
		commitSnapshot = postSnapshot
		if claimed.Lane == store.SchedulerLaneBackfill && executeErr == nil && !panicked &&
			(result.StopReason == store.SchedulerStopFileBudget ||
				result.StopReason == store.SchedulerStopByteBudget ||
				result.StopReason == store.SchedulerStopTimeBudget) && postSnapshot.LiveDepth > 0 {
			result.StopReason = store.SchedulerStopLivePreempted
		}
	}

	transition := failedTransition(store.RuntimeErrorInvalid)
	requiresInterrupt := persistenceExhausted
	if !persistenceExhausted {
		transition, err = service.cycleTransition(ctx, executor, claimed, budget, result, executeErr, panicked)
		if err != nil {
			return CycleResult{}, err
		}
	}
	if err := validateSliceConsumption(result, budget, budget.Blocked); err != nil {
		transition = failedTransition(store.ClassifyRuntimeError(err))
		result.FilesProcessed = 0
		result.BytesProcessed = 0
		result.Active = 0
		requiresInterrupt = true
	}
	if requiresInterrupt {
		interruptCtx, cancel := context.WithTimeout(context.Background(), service.interruptTimeout)
		interruptErr := executor.Interrupt(interruptCtx, claimed, store.RuntimeErrorInvalid)
		cancel()
		if interruptErr != nil {
			transition.returnErr = errors.Join(transition.returnErr, interruptErr)
		}
	}
	activeMS := durationMillisecondsCeil(result.Active)
	finishedBaseMS, err := finishedTimestampBase(startedAtMS, activeMS)
	if err != nil {
		return CycleResult{}, err
	}
	finishedAtMS, err := service.afterMS(finishedBaseMS, store.MaxSchedulerRunningTimestampMS)
	if err != nil {
		return CycleResult{}, err
	}
	commitAtMS, err := service.afterMS(
		maxInt64(commitSnapshot.MaxQueueOrderMS, finishedAtMS), store.MaxSchedulerTimestampMS,
	)
	if err != nil {
		return CycleResult{}, err
	}
	cycle := store.SchedulerCycle{
		CycleID: cycleID, TaskID: claimed.TaskID, Lane: claimed.Lane,
		SelectionReason: selection.Reason, StopReason: transition.stopReason,
		Outcome: transition.outcome, BudgetFiles: budget.MaxFiles, BudgetBytes: budget.MaxBytes,
		BudgetActiveMS: durationMillisecondsCeil(budget.MaxActive),
		ConsumedFiles:  result.FilesProcessed, ConsumedBytes: result.BytesProcessed, ActiveMS: activeMS,
		LiveDepth: selection.LiveDepth, BackfillDepth: selection.BackfillDepth,
		OldestLiveWaitMS:     selection.OldestLiveWaitMS,
		OldestBackfillWaitMS: selection.OldestBackfillWaitMS,
		StartedAtMS:          startedAtMS, FinishedAtMS: finishedAtMS,
	}
	commit := store.SchedulerCycleCommit{
		TaskID: claimed.TaskID, ExpectedState: store.SchedulerTaskRunning, State: transition.state,
		QueueOrderMS: commitAtMS, FilesDelta: result.FilesProcessed,
		BytesDelta: result.BytesProcessed, ErrorClass: transition.errorClass,
		AtMS: commitAtMS, Cycle: cycle,
	}
	if transition.outcome == store.SchedulerCycleFailed && transition.errorClass != nil {
		retry, err := service.retryMutation(ctx, claimed.TaskID, *transition.errorClass, commitAtMS)
		if err != nil {
			return CycleResult{}, err
		}
		commit.Retry = &retry
	}
	// Once the target has run, its cycle checkpoint must outlive a parent
	// cancellation that races with BeginTx/GORM binding. The bounded detached
	// context preserves values but never lets a cancellation tear the target
	// side effect away from its durable scheduler fact.
	commitCtx, commitCancel := context.WithTimeout(
		context.WithoutCancel(ctx), service.interruptTimeout,
	)
	defer commitCancel()
	if err := service.commitCycleWithReadback(commitCtx, commit); err != nil {
		return CycleResult{}, err
	}
	service.notifyCycleCommitted(commitCtx, cycle)
	return CycleResult{Cycle: cycle, YieldFor: transition.yieldFor(budget)}, transition.returnErr
}

func (service *Service) resumeDueRetryOwned(ctx context.Context) (bool, error) {
	lifecycle, err := service.repository.SchedulerLifecycle(ctx)
	if err != nil {
		return false, err
	}
	nowMS := service.clock().UnixMilli()
	var cursor *store.SchedulerRetryCursor
	for {
		states, next, err := service.repository.ListDueSchedulerRetries(
			ctx, lifecycle.HomeGeneration, nowMS, cursor, service.recoveryPageSize,
		)
		if err != nil {
			return false, err
		}
		for _, retryState := range states {
			task, err := service.repository.SchedulerTask(ctx, retryState.TaskID)
			if err != nil {
				return false, err
			}
			if !schedulerLifecyclePermits(lifecycle, task) {
				continue
			}
			executor := service.executors[task.TargetKind]
			if executor == nil {
				return false, ErrExecutorMissing
			}
			target, err := executor.Retry(ctx, task)
			if err != nil {
				return false, err
			}
			if target.JobID == "" || target.State != store.JobQueued {
				return false, ErrInvalidService
			}
			snapshot, err := service.repository.SchedulerQueueSnapshot(ctx)
			if err != nil {
				return false, err
			}
			atMS, err := service.afterMS(maxInt64(
				maxInt64(task.UpdatedAtMS, retryState.UpdatedAtMS),
				maxInt64(snapshot.MaxQueueOrderMS, target.UpdatedAtMS),
			), store.MaxSchedulerQueuedTimestampMS)
			if err != nil {
				return false, err
			}
			if _, err := service.repository.RequeueFailedSchedulerTask(
				ctx, task.TaskID, task.TargetID, target.JobID, retryState.Revision, atMS, atMS,
			); err != nil {
				if errors.Is(err, store.ErrSchedulerPaused) {
					return false, nil
				}
				return false, err
			}
			return true, nil
		}
		cursor = next
		if cursor == nil {
			return false, nil
		}
	}
}

func schedulerLifecyclePermits(lifecycle store.SchedulerLifecycle, task store.SchedulerTask) bool {
	if lifecycle.HomeGeneration != task.HomeGeneration || lifecycle.SystemState != store.LifecycleSystemAwake ||
		lifecycle.Transition != store.LifecycleTransitionSteady ||
		lifecycle.SourceState != store.LifecycleSourceAvailable ||
		lifecycle.UserPauseScope == store.LifecyclePauseAll {
		return false
	}
	return task.Lane == store.SchedulerLaneLive ||
		task.Lane == store.SchedulerLaneBackfill && lifecycle.UserPauseScope != store.LifecyclePauseBackfill
}

func (service *Service) retryMutation(
	ctx context.Context,
	taskID string,
	class store.RuntimeErrorClass,
	atMS int64,
) (store.SchedulerRetryMutation, error) {
	mutation := store.SchedulerRetryMutation{
		Disposition: store.SchedulerRetryBlocked, FailureCount: 1,
		LastErrorClass: class, RecoveryAction: recoveryActionFor(class, false),
	}
	current, err := service.repository.SchedulerRetryState(ctx, taskID)
	if err == nil {
		if current.Revision == math.MaxInt64 || current.FailureCount == math.MaxInt64 {
			return store.SchedulerRetryMutation{}, ErrInvalidService
		}
		mutation.ExpectedRevision = current.Revision
		mutation.FailureCount = current.FailureCount + 1
	} else if !errors.Is(err, store.ErrNotFound) {
		return store.SchedulerRetryMutation{}, err
	}
	if !transientRuntimeError(class) {
		return mutation, nil
	}
	delay, retry, err := service.retryPolicy.Delay(int(mutation.FailureCount))
	if err != nil {
		return store.SchedulerRetryMutation{}, err
	}
	if !retry {
		mutation.RecoveryAction = recoveryActionFor(class, true)
		return mutation, nil
	}
	delayMS := durationMillisecondsCeil(delay)
	nextRetryAt, ok := runtimeclock.Add(atMS, delayMS)
	if delayMS <= 0 || !ok || nextRetryAt > store.MaxSchedulerRetryDueTimestampMS {
		mutation.RecoveryAction = recoveryActionFor(class, true)
		return mutation, nil
	}
	mutation.Disposition = store.SchedulerRetryWaiting
	mutation.NextRetryAtMS = &nextRetryAt
	mutation.RecoveryAction = store.SchedulerRecoveryNone
	return mutation, nil
}

func transientRuntimeError(class store.RuntimeErrorClass) bool {
	switch class {
	case store.RuntimeErrorBusy, store.RuntimeErrorTimeout, store.RuntimeErrorIO,
		store.RuntimeErrorUnavailable, store.RuntimeErrorUnknown:
		return true
	default:
		return false
	}
}

func recoveryActionFor(class store.RuntimeErrorClass, exhausted bool) store.SchedulerRecoveryAction {
	switch class {
	case store.RuntimeErrorDiskFull:
		return store.SchedulerRecoveryFreeSpace
	case store.RuntimeErrorReadOnly, store.RuntimeErrorPermission:
		return store.SchedulerRecoveryGrantPermission
	case store.RuntimeErrorCorrupt:
		return store.SchedulerRecoveryRepairStore
	case store.RuntimeErrorInvalid:
		return store.SchedulerRecoveryChooseHome
	case store.RuntimeErrorUnavailable:
		if exhausted {
			return store.SchedulerRecoveryCheckSource
		}
		return store.SchedulerRecoveryRetry
	default:
		return store.SchedulerRecoveryRetry
	}
}

func (service *Service) reconcileTerminalTarget(
	ctx context.Context,
	task store.SchedulerTask,
	target store.JobRun,
) (store.SchedulerTask, error) {
	if task.State != store.SchedulerTaskRunning || task.LastStartedAtMS == nil || target.JobID != task.TargetID {
		return store.SchedulerTask{}, store.ErrSchedulerTransition
	}
	queueSnapshot, err := service.repository.SchedulerRunnableQueueSnapshot(ctx)
	if err != nil {
		return store.SchedulerTask{}, err
	}
	cycleID, err := service.newCycleID()
	if err != nil || cycleID == "" {
		if err == nil {
			err = ErrInvalidService
		}
		return store.SchedulerTask{}, err
	}
	commitAtMS, err := service.afterMS(
		maxInt64(task.UpdatedAtMS, queueSnapshot.MaxQueueOrderMS), store.MaxSchedulerTimestampMS,
	)
	if err != nil {
		return store.SchedulerTask{}, err
	}
	if target.UpdatedAtMS > commitAtMS {
		commitAtMS = target.UpdatedAtMS
	}
	finishedAtMS := commitAtMS
	outcome := store.SchedulerCycleCompleted
	stopReason := store.SchedulerStopCompleted
	state := store.SchedulerTaskSucceeded
	var errorClass *store.RuntimeErrorClass
	if target.State != store.JobSucceeded {
		outcome = store.SchedulerCycleFailed
		stopReason = store.SchedulerStopDependencyError
		state = store.SchedulerTaskFailed
		value := store.RuntimeErrorUnknown
		if target.ErrorClass != nil {
			value = *target.ErrorClass
		} else if target.State == store.JobCancelled {
			value = store.RuntimeErrorCanceled
		}
		errorClass = &value
	}
	selectionReason := store.SchedulerSelectionBackfillOnly
	if task.Lane == store.SchedulerLaneLive {
		selectionReason = store.SchedulerSelectionLiveOnly
	}
	nowMS := service.logicalQueueSnapshotMS(queueSnapshot)
	cycle := store.SchedulerCycle{
		CycleID: cycleID, TaskID: task.TaskID, Lane: task.Lane,
		SelectionReason: selectionReason, StopReason: stopReason, Outcome: outcome,
		LiveDepth: queueSnapshot.LiveDepth, BackfillDepth: queueSnapshot.BackfillDepth,
		OldestLiveWaitMS:     queueWaitMS(queueSnapshot.LiveCandidate, nowMS),
		OldestBackfillWaitMS: queueWaitMS(queueSnapshot.BackfillCandidate, nowMS),
		StartedAtMS:          *task.LastStartedAtMS, FinishedAtMS: finishedAtMS,
	}
	commit := store.SchedulerCycleCommit{
		TaskID: task.TaskID, ExpectedState: store.SchedulerTaskRunning, State: state,
		QueueOrderMS: commitAtMS, ErrorClass: errorClass, AtMS: commitAtMS, Cycle: cycle,
	}
	if state == store.SchedulerTaskFailed && errorClass != nil {
		retry, err := service.retryMutation(ctx, task.TaskID, *errorClass, commitAtMS)
		if err != nil {
			return store.SchedulerTask{}, err
		}
		commit.Retry = &retry
	}
	if err := service.commitCycleWithReadback(ctx, commit); err != nil {
		return store.SchedulerTask{}, err
	}
	service.notifyCycleCommitted(ctx, cycle)
	return service.repository.SchedulerTask(ctx, task.TaskID)
}

func (service *Service) notifyCycleCommitted(ctx context.Context, cycle store.SchedulerCycle) {
	if service == nil || service.cycleCommitted == nil || ctx == nil {
		return
	}
	defer func() { _ = recover() }()
	service.cycleCommitted(ctx, cycle)
}

func (service *Service) commitCycleWithReadback(
	ctx context.Context,
	commit store.SchedulerCycleCommit,
) error {
	err := service.commitCycle(ctx, commit)
	if err == nil {
		return nil
	}
	if service.committedCycleExact(ctx, commit.Cycle) {
		return nil
	}
	if !errors.Is(err, store.ErrSchedulerTransition) {
		return err
	}
	task, readErr := service.repository.SchedulerTask(ctx, commit.TaskID)
	if readErr != nil || task.State != store.SchedulerTaskRunning || task.LastStartedAtMS == nil ||
		*task.LastStartedAtMS != commit.Cycle.StartedAtMS {
		return errors.Join(err, readErr)
	}
	snapshot, readErr := service.repository.SchedulerQueueSnapshot(ctx)
	if readErr != nil {
		return errors.Join(err, readErr)
	}
	maximum := store.MaxSchedulerTimestampMS
	if commit.State == store.SchedulerTaskQueued {
		maximum = store.MaxSchedulerQueuedTimestampMS
	} else if commit.State == store.SchedulerTaskInterrupted {
		maximum = store.MaxSchedulerRunningTimestampMS
	}
	rebasedAtMS, readErr := service.afterMS(
		maxInt64(task.UpdatedAtMS, snapshot.MaxQueueOrderMS), maximum,
	)
	if readErr != nil {
		return errors.Join(err, readErr)
	}
	if readErr = rebaseRetryDue(commit.Retry, commit.AtMS, rebasedAtMS); readErr != nil {
		return errors.Join(err, readErr)
	}
	commit.AtMS = rebasedAtMS
	commit.QueueOrderMS = commit.AtMS
	err = service.commitCycle(ctx, commit)
	if err == nil || service.committedCycleExact(ctx, commit.Cycle) {
		return nil
	}
	return err
}

func rebaseRetryDue(mutation *store.SchedulerRetryMutation, previousAtMS, rebasedAtMS int64) error {
	if mutation == nil || mutation.Disposition != store.SchedulerRetryWaiting ||
		previousAtMS == rebasedAtMS {
		return nil
	}
	if mutation.NextRetryAtMS == nil || *mutation.NextRetryAtMS <= previousAtMS ||
		rebasedAtMS < previousAtMS {
		return ErrInvalidService
	}
	delayMS := *mutation.NextRetryAtMS - previousAtMS
	nextRetryAtMS, ok := runtimeclock.Add(rebasedAtMS, delayMS)
	if !ok || nextRetryAtMS > store.MaxSchedulerRetryDueTimestampMS {
		mutation.Disposition = store.SchedulerRetryBlocked
		mutation.NextRetryAtMS = nil
		mutation.RecoveryAction = recoveryActionFor(mutation.LastErrorClass, true)
		return nil
	}
	mutation.NextRetryAtMS = &nextRetryAtMS
	return nil
}

func (service *Service) committedCycleExact(ctx context.Context, expected store.SchedulerCycle) bool {
	cycle, err := service.repository.SchedulerCycle(ctx, expected.CycleID)
	return err == nil && cycle == expected
}

type cycleTransition struct {
	state      store.SchedulerTaskState
	outcome    store.SchedulerCycleOutcome
	stopReason store.SchedulerStopReason
	errorClass *store.RuntimeErrorClass
	returnErr  error
}

func (service *Service) cycleTransition(
	ctx context.Context,
	executor Executor,
	task store.SchedulerTask,
	budget ScanBudget,
	result SliceResult,
	executeErr error,
	panicked bool,
) (cycleTransition, error) {
	if budget.Blocked {
		return yieldedTransition(store.SchedulerStopSystemPressure), nil
	}
	if panicked || errors.Is(executeErr, context.Canceled) || errors.Is(executeErr, context.DeadlineExceeded) {
		class := store.RuntimeErrorCanceled
		stop := store.SchedulerStopCancelled
		returnErr := executeErr
		if panicked {
			class = store.RuntimeErrorUnknown
			stop = store.SchedulerStopWorkerPanic
			returnErr = ErrExecutorPanic
		}
		interruptCtx, cancel := context.WithTimeout(context.Background(), service.interruptTimeout)
		defer cancel()
		if err := executor.Interrupt(interruptCtx, task, class); err != nil {
			return cycleTransition{}, errors.Join(returnErr, err)
		}
		return cycleTransition{
			state: store.SchedulerTaskInterrupted, outcome: store.SchedulerCycleInterrupted,
			stopReason: stop, errorClass: pointerToErrorClass(class), returnErr: returnErr,
		}, nil
	}
	if executeErr != nil {
		class := store.ClassifyRuntimeError(executeErr)
		return cycleTransition{
			state: store.SchedulerTaskFailed, outcome: store.SchedulerCycleFailed,
			stopReason: store.SchedulerStopDependencyError,
			errorClass: pointerToErrorClass(class), returnErr: executeErr,
		}, nil
	}
	switch result.StopReason {
	case store.SchedulerStopCompleted:
		return cycleTransition{
			state: store.SchedulerTaskSucceeded, outcome: store.SchedulerCycleCompleted,
			stopReason: result.StopReason,
		}, nil
	case store.SchedulerStopFileBudget, store.SchedulerStopByteBudget,
		store.SchedulerStopTimeBudget, store.SchedulerStopLivePreempted:
		return yieldedTransition(result.StopReason), nil
	default:
		return failedTransition(store.RuntimeErrorInvalid), nil
	}
}

func yieldedTransition(reason store.SchedulerStopReason) cycleTransition {
	return cycleTransition{
		state: store.SchedulerTaskQueued, outcome: store.SchedulerCycleYielded, stopReason: reason,
	}
}

func failedTransition(class store.RuntimeErrorClass) cycleTransition {
	return cycleTransition{
		state: store.SchedulerTaskFailed, outcome: store.SchedulerCycleFailed,
		stopReason: store.SchedulerStopDependencyError, errorClass: pointerToErrorClass(class),
		returnErr: ErrInvalidSliceResult,
	}
}

func (transition cycleTransition) yieldFor(budget ScanBudget) time.Duration {
	if transition.outcome == store.SchedulerCycleYielded {
		return budget.YieldFor
	}
	return 0
}

func validateSliceConsumption(result SliceResult, budget ScanBudget, blocked bool) error {
	if result.FilesProcessed < 0 || result.BytesProcessed < 0 || result.Active < 0 {
		return ErrInvalidSliceResult
	}
	if blocked {
		if result.FilesProcessed != 0 || result.BytesProcessed != 0 || result.Active != 0 {
			return ErrInvalidSliceResult
		}
		return nil
	}
	if result.FilesProcessed > budget.MaxFiles || result.BytesProcessed > budget.MaxBytes ||
		result.Active > budget.MaxActive {
		return ErrInvalidSliceResult
	}
	return nil
}

func executeSliceSafely(
	ctx context.Context,
	executor Executor,
	task store.SchedulerTask,
	budget ScanBudget,
) (result SliceResult, err error, panicked bool) {
	defer func() {
		if recover() != nil {
			result = SliceResult{}
			err = ErrExecutorPanic
			panicked = true
		}
	}()
	result, err = executor.ExecuteSlice(ctx, task, budget)
	return result, err, false
}

func (service *Service) logicalQueueSnapshotMS(snapshot store.SchedulerQueueSnapshot) int64 {
	value := service.clock().UnixMilli()
	for _, task := range []*store.SchedulerTask{snapshot.LiveCandidate, snapshot.BackfillCandidate} {
		if task != nil && task.EnqueuedAtMS > value {
			value = task.EnqueuedAtMS
		}
	}
	return value
}

func (service *Service) afterMS(value int64, maximum int64) (int64, error) {
	result, ok := runtimeclock.After(service.clock().UnixMilli(), value, maximum)
	if !ok {
		return 0, ErrInvalidService
	}
	return result, nil
}

func sliceHasQueuedCommitHeadroom(startedAtMS int64, budget ScanBudget) bool {
	activeMS := durationMillisecondsCeil(budget.MaxActive)
	if budget.Blocked {
		activeMS = 0
	}
	finishedAtMS, ok := runtimeclock.Add(startedAtMS, activeMS)
	if !ok {
		return false
	}
	commitAtMS, ok := runtimeclock.Successor(finishedAtMS)
	return ok && commitAtMS <= store.MaxSchedulerQueuedTimestampMS
}

func taskHasSliceCommitHeadroom(task store.SchedulerTask, budget ScanBudget) bool {
	if task.SliceCount >= math.MaxInt64-1 {
		return false
	}
	return task.FilesProcessed <= math.MaxInt64-budget.MaxFiles &&
		task.BytesProcessed <= math.MaxInt64-budget.MaxBytes
}

func finishedTimestampBase(startedAtMS int64, activeMS int64) (int64, error) {
	if startedAtMS < 0 || activeMS < 0 {
		return 0, ErrInvalidService
	}
	if activeMS == 0 {
		return startedAtMS - 1, nil
	}
	if startedAtMS > store.MaxSchedulerTimestampMS-activeMS+1 {
		return 0, ErrInvalidService
	}
	return startedAtMS + activeMS - 1, nil
}

func maxInt64(left int64, right int64) int64 {
	if left > right {
		return left
	}
	return right
}

func queueWaitMS(task *store.SchedulerTask, nowMS int64) int64 {
	if task == nil || nowMS <= task.EnqueuedAtMS {
		return 0
	}
	return nowMS - task.EnqueuedAtMS
}

func durationMillisecondsCeil(value time.Duration) int64 {
	if value <= 0 {
		return 0
	}
	milliseconds := int64(value / time.Millisecond)
	if value%time.Millisecond != 0 {
		milliseconds++
	}
	return milliseconds
}

func pointerToErrorClass(value store.RuntimeErrorClass) *store.RuntimeErrorClass {
	return &value
}

func (result SliceResult) String() string {
	return fmt.Sprintf("files=%d bytes=%d active=%s stop=%s", result.FilesProcessed,
		result.BytesProcessed, result.Active, result.StopReason)
}
