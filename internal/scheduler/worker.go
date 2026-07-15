package scheduler

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/SisyphusSQ/codex-pulse/internal/store"
)

// CycleResult 是一次已持久化worker cycle的readback与下一次协作等待时间。
type CycleResult struct {
	Cycle    store.SchedulerCycle
	YieldFor time.Duration
}

// Run 先接管遗留task，再以单owner循环执行一个有界cycle并进行可取消等待。
func (service *Service) Run(ctx context.Context) error {
	if service == nil || service.repository == nil || service.systemProbe == nil {
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

	ownerLease, err := service.repository.AcquireSchedulerOwner(ctx)
	if err != nil {
		return err
	}
	defer ownerLease.Release()
	if _, err := service.recoverActiveTasksSerialized(ctx); err != nil {
		return err
	}
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		system, err := service.systemProbe.Snapshot(ctx)
		if err != nil {
			return err
		}
		result, err := service.runCycleSerialized(ctx, system)
		if errors.Is(err, ErrQueueEmpty) {
			if err := waitForContext(ctx, service.idleDelay); err != nil {
				return err
			}
			continue
		}
		if errors.Is(err, ErrSchedulerRetry) {
			if err := waitForContext(ctx, service.idleDelay); err != nil {
				return err
			}
			continue
		}
		if err != nil {
			if result.Cycle.Outcome == store.SchedulerCycleFailed {
				continue
			}
			return err
		}
		if result.YieldFor > 0 {
			if err := waitForContext(ctx, result.YieldFor); err != nil {
				return err
			}
		}
	}
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
	return service.recoverActiveTasksOwned(ctx)
}

func (service *Service) recoverActiveTasksOwned(ctx context.Context) ([]store.SchedulerTask, error) {
	recovered := make([]store.SchedulerTask, 0)
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
				atMS := service.afterMS(maxInt64(
					task.UpdatedAtMS,
					maxInt64(task.QueueOrderMS, snapshot.MaxQueueOrderMS),
				))
				value, err = service.repository.RecoverSchedulerTask(
					ctx, task.TaskID, task.TargetID, target.JobID, atMS, atMS,
				)
				if err != nil {
					return recovered, err
				}
			case store.JobSucceeded, store.JobFailed, store.JobCancelled:
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
	queueSnapshot, err := service.repository.SchedulerQueueSnapshot(ctx)
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

	startedAtMS := service.afterMS(selection.Task.UpdatedAtMS)
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
	if !budget.Blocked {
		result, executeErr, panicked = executeSliceSafely(ctx, executor, claimed, budget)
	}
	commitSnapshot := queueSnapshot
	if ctx.Err() == nil {
		postSnapshot, listErr := service.repository.SchedulerQueueSnapshot(ctx)
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

	transition, err := service.cycleTransition(ctx, executor, claimed, budget, result, executeErr, panicked)
	if err != nil {
		return CycleResult{}, err
	}
	if err := validateSliceConsumption(result, budget, budget.Blocked); err != nil {
		transition = failedTransition(store.ClassifyRuntimeError(err))
		result.FilesProcessed = 0
		result.BytesProcessed = 0
	}
	activeMS := durationMillisecondsCeil(result.Active)
	finishedAtMS := service.afterMS(startedAtMS + activeMS - 1)
	commitAtMS := service.afterMS(maxInt64(commitSnapshot.MaxQueueOrderMS, finishedAtMS))
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
	commitCtx := ctx
	commitCancel := func() {}
	if ctx.Err() != nil || transition.outcome == store.SchedulerCycleInterrupted {
		commitCtx, commitCancel = context.WithTimeout(context.Background(), service.interruptTimeout)
	}
	defer commitCancel()
	if err := service.commitCycleWithReadback(commitCtx, commit); err != nil {
		return CycleResult{}, err
	}
	return CycleResult{Cycle: cycle, YieldFor: transition.yieldFor(budget)}, transition.returnErr
}

func (service *Service) reconcileTerminalTarget(
	ctx context.Context,
	task store.SchedulerTask,
	target store.JobRun,
) (store.SchedulerTask, error) {
	if task.State != store.SchedulerTaskRunning || task.LastStartedAtMS == nil || target.JobID != task.TargetID {
		return store.SchedulerTask{}, store.ErrSchedulerTransition
	}
	queueSnapshot, err := service.repository.SchedulerQueueSnapshot(ctx)
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
	finishedAtMS := service.afterMS(maxInt64(*task.LastStartedAtMS, target.UpdatedAtMS))
	commitAtMS := service.afterMS(maxInt64(queueSnapshot.MaxQueueOrderMS, finishedAtMS))
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
	if err := service.commitCycleWithReadback(ctx, commit); err != nil {
		return store.SchedulerTask{}, err
	}
	return service.repository.SchedulerTask(ctx, task.TaskID)
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
	task, readErr := service.repository.SchedulerTask(context.WithoutCancel(ctx), commit.TaskID)
	if readErr != nil || task.State != store.SchedulerTaskRunning || task.LastStartedAtMS == nil ||
		*task.LastStartedAtMS != commit.Cycle.StartedAtMS {
		return errors.Join(err, readErr)
	}
	snapshot, readErr := service.repository.SchedulerQueueSnapshot(context.WithoutCancel(ctx))
	if readErr != nil {
		return errors.Join(err, readErr)
	}
	commit.AtMS = service.afterMS(maxInt64(task.UpdatedAtMS, snapshot.MaxQueueOrderMS))
	commit.QueueOrderMS = commit.AtMS
	err = service.commitCycle(ctx, commit)
	if err == nil || service.committedCycleExact(ctx, commit.Cycle) {
		return nil
	}
	return err
}

func (service *Service) committedCycleExact(ctx context.Context, expected store.SchedulerCycle) bool {
	readCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), service.interruptTimeout)
	defer cancel()
	cycle, err := service.repository.SchedulerCycle(readCtx, expected.CycleID)
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
	if result.FilesProcessed > budget.MaxFiles || result.BytesProcessed > budget.MaxBytes {
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

func (service *Service) afterMS(value int64) int64 {
	now := service.clock().UnixMilli()
	if now <= value {
		return value + 1
	}
	return now
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
	return int64((value + time.Millisecond - 1) / time.Millisecond)
}

func pointerToErrorClass(value store.RuntimeErrorClass) *store.RuntimeErrorClass {
	return &value
}

func waitForContext(ctx context.Context, duration time.Duration) error {
	if duration <= 0 {
		return ctx.Err()
	}
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func (result SliceResult) String() string {
	return fmt.Sprintf("files=%d bytes=%d active=%s stop=%s", result.FilesProcessed,
		result.BytesProcessed, result.Active, result.StopReason)
}
