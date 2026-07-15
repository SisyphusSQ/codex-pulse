package store

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestSchedulerAdmissionLifecycleEventDoesNotEmbedUnboundedTaskID(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	repository := openRuntimeRepository(t)
	job := schedulerTargetJob("long-task-id-job", JobPhaseLive, 10)
	if err := repository.CreateJobRun(ctx, job); err != nil {
		t.Fatalf("CreateJobRun() error = %v", err)
	}
	task := SchedulerTask{
		TaskID: strings.Repeat("task-id-", 40), DedupeKey: "long-task-id-dedupe",
		TargetKind: SchedulerTargetLiveScan, TargetID: job.JobID, HomeGeneration: 7,
		Lane: SchedulerLaneLive, ServiceClass: SchedulerServiceBackground,
		State: SchedulerTaskQueued, QueueOrderMS: 20, EnqueuedAtMS: 20, UpdatedAtMS: 20,
	}
	if err := repository.EnqueueSchedulerTask(ctx, task, 16); err != nil {
		t.Fatalf("EnqueueSchedulerTask(long task ID) error = %v", err)
	}
	lifecycle, err := repository.SchedulerLifecycle(ctx)
	if err != nil || lifecycle.LastEventID != "scheduler-admission" {
		t.Fatalf("SchedulerLifecycle() = %#v, %v", lifecycle, err)
	}
}

func TestLifecycleRepositoryInitializesAndCASesOrthogonalState(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	repository := openRuntimeRepository(t)
	initial := SchedulerLifecycle{
		HomeGeneration: 7,
		UserPauseScope: LifecyclePauseNone,
		SystemState:    LifecycleSystemAwake,
		Transition:     LifecycleTransitionSteady,
		SourceState:    LifecycleSourceAvailable,
		LastEventID:    "startup:7",
		Revision:       1,
		UpdatedAtMS:    10,
	}
	got, err := repository.InitializeSchedulerLifecycle(ctx, initial)
	if err != nil || got != initial {
		t.Fatalf("InitializeSchedulerLifecycle() = %#v, %v", got, err)
	}
	got, err = repository.InitializeSchedulerLifecycle(ctx, initial)
	if err != nil || got != initial {
		t.Fatalf("InitializeSchedulerLifecycle(exact replay) = %#v, %v", got, err)
	}

	paused := initial
	paused.UserPauseScope = LifecyclePauseAll
	paused.LastEventID = "user:pause:1"
	paused.Revision = 2
	paused.UpdatedAtMS = 11
	got, err = repository.CompareAndSwapSchedulerLifecycle(ctx, initial.Revision, paused)
	if err != nil || got != paused {
		t.Fatalf("CompareAndSwapSchedulerLifecycle() = %#v, %v", got, err)
	}
	got, err = repository.CompareAndSwapSchedulerLifecycle(ctx, initial.Revision, paused)
	if err != nil || got != paused {
		t.Fatalf("CompareAndSwapSchedulerLifecycle(exact replay) = %#v, %v", got, err)
	}

	sleeping := paused
	sleeping.SystemState = LifecycleSystemSleeping
	sleeping.LastEventID = "system:sleep:1"
	sleeping.Revision = 3
	sleeping.UpdatedAtMS = 12
	got, err = repository.CompareAndSwapSchedulerLifecycle(ctx, paused.Revision, sleeping)
	if err != nil || got.UserPauseScope != LifecyclePauseAll || got.SystemState != LifecycleSystemSleeping {
		t.Fatalf("CompareAndSwapSchedulerLifecycle(sleep) = %#v, %v", got, err)
	}

	stale := sleeping
	stale.LastEventID = "stale:event"
	stale.Revision = 4
	stale.UpdatedAtMS = 13
	if _, err := repository.CompareAndSwapSchedulerLifecycle(ctx, initial.Revision, stale); !errors.Is(err, ErrLifecycleConflict) {
		t.Fatalf("CompareAndSwapSchedulerLifecycle(stale) error = %v, want ErrLifecycleConflict", err)
	}
	readback, err := repository.SchedulerLifecycle(ctx)
	if err != nil || readback != sleeping {
		t.Fatalf("SchedulerLifecycle() = %#v, %v", readback, err)
	}
}

func TestLifecycleRepositoryRejectsInvalidAndConflictingInitialization(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	repository := openRuntimeRepository(t)
	initial := SchedulerLifecycle{
		HomeGeneration: 1,
		UserPauseScope: LifecyclePauseNone,
		SystemState:    LifecycleSystemAwake,
		Transition:     LifecycleTransitionSteady,
		SourceState:    LifecycleSourceAvailable,
		LastEventID:    "startup:1",
		Revision:       1,
		UpdatedAtMS:    10,
	}
	if _, err := repository.InitializeSchedulerLifecycle(ctx, initial); err != nil {
		t.Fatalf("InitializeSchedulerLifecycle() error = %v", err)
	}
	conflict := initial
	conflict.HomeGeneration = 2
	if _, err := repository.InitializeSchedulerLifecycle(ctx, conflict); !errors.Is(err, ErrLifecycleConflict) {
		t.Fatalf("InitializeSchedulerLifecycle(conflict) error = %v", err)
	}
	invalid := initial
	invalid.UserPauseScope = "everything"
	if _, err := repository.CompareAndSwapSchedulerLifecycle(ctx, initial.Revision, invalid); !errors.Is(err, ErrLifecycleTransition) {
		t.Fatalf("CompareAndSwapSchedulerLifecycle(invalid) error = %v", err)
	}
}

func TestSchedulerFailureCommitsRetryFactAtomicallyAndListsDueWork(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	repository := openRuntimeRepository(t)
	job := schedulerTargetJob("retry-live-job", JobPhaseLive, 10)
	if err := repository.CreateJobRun(ctx, job); err != nil {
		t.Fatalf("CreateJobRun() error = %v", err)
	}
	task := SchedulerTask{
		TaskID: "retry-live-task", DedupeKey: "retry:live:task",
		TargetKind: SchedulerTargetLiveScan, TargetID: job.JobID, HomeGeneration: 7,
		Lane: SchedulerLaneLive, ServiceClass: SchedulerServiceBackground,
		State: SchedulerTaskQueued, QueueOrderMS: 11, EnqueuedAtMS: 11, UpdatedAtMS: 11,
	}
	if err := repository.EnqueueSchedulerTask(ctx, task, 100); err != nil {
		t.Fatalf("EnqueueSchedulerTask() error = %v", err)
	}
	claimed, err := repository.ClaimSchedulerTask(ctx, task.TaskID, 20)
	if err != nil {
		t.Fatalf("ClaimSchedulerTask() error = %v", err)
	}
	nextRetryAt := int64(2_000)
	class := RuntimeErrorIO
	cycle := SchedulerCycle{
		CycleID: "retry-failed-cycle", TaskID: task.TaskID, Lane: SchedulerLaneLive,
		SelectionReason: SchedulerSelectionLiveOnly, StopReason: SchedulerStopDependencyError,
		Outcome: SchedulerCycleFailed, BudgetFiles: 1, BudgetBytes: 1,
		BudgetActiveMS: 1, LiveDepth: 1, StartedAtMS: *claimed.LastStartedAtMS, FinishedAtMS: 21,
	}
	commit := SchedulerCycleCommit{
		TaskID: task.TaskID, ExpectedState: SchedulerTaskRunning, State: SchedulerTaskFailed,
		ErrorClass: &class, AtMS: 22, Cycle: cycle,
		Retry: &SchedulerRetryMutation{
			ExpectedRevision: 0, Disposition: SchedulerRetryWaiting, FailureCount: 1,
			LastErrorClass: class, NextRetryAtMS: &nextRetryAt,
			RecoveryAction: SchedulerRecoveryNone,
		},
	}
	if err := repository.CommitSchedulerCycle(ctx, commit); err != nil {
		t.Fatalf("CommitSchedulerCycle() error = %v", err)
	}
	retryState, err := repository.SchedulerRetryState(ctx, task.TaskID)
	if err != nil || retryState.TaskID != task.TaskID || retryState.Disposition != SchedulerRetryWaiting ||
		retryState.FailureCount != 1 || retryState.LastErrorClass != class ||
		retryState.NextRetryAtMS == nil || *retryState.NextRetryAtMS != nextRetryAt ||
		retryState.Revision != 1 || retryState.UpdatedAtMS != commit.AtMS {
		t.Fatalf("SchedulerRetryState() = %#v, %v", retryState, err)
	}
	if due, _, err := repository.ListDueSchedulerRetries(ctx, 7, nextRetryAt-1, nil, 100); err != nil || len(due) != 0 {
		t.Fatalf("ListDueSchedulerRetries(before due) = %#v, %v", due, err)
	}
	due, cursor, err := repository.ListDueSchedulerRetries(ctx, 7, nextRetryAt, nil, 100)
	if err != nil || len(due) != 1 || due[0].TaskID != retryState.TaskID ||
		due[0].NextRetryAtMS == nil || *due[0].NextRetryAtMS != nextRetryAt ||
		cursor == nil || cursor.TaskID != task.TaskID {
		t.Fatalf("ListDueSchedulerRetries(due) = %#v, %#v, %v", due, cursor, err)
	}
	if due, _, err := repository.ListDueSchedulerRetries(ctx, 8, nextRetryAt, nil, 100); err != nil || len(due) != 0 {
		t.Fatalf("ListDueSchedulerRetries(other generation) = %#v, %v", due, err)
	}
	resumedJob := schedulerTargetJob("retry-live-job-resumed", JobPhaseLive, nextRetryAt)
	if err := repository.CreateJobRun(ctx, resumedJob); err != nil {
		t.Fatalf("CreateJobRun(resumed) error = %v", err)
	}
	requeued, err := repository.RequeueFailedSchedulerTask(
		ctx, task.TaskID, job.JobID, resumedJob.JobID, retryState.Revision, nextRetryAt+1, nextRetryAt+1,
	)
	if err != nil || requeued.State != SchedulerTaskQueued || requeued.TargetID != resumedJob.JobID {
		t.Fatalf("RequeueFailedSchedulerTask() = %#v, %v", requeued, err)
	}
	resolved, err := repository.SchedulerRetryState(ctx, task.TaskID)
	if err != nil || resolved.Disposition != SchedulerRetryResolved || resolved.Revision != 2 ||
		resolved.NextRetryAtMS != nil || resolved.FailureCount != 1 {
		t.Fatalf("SchedulerRetryState(resolved) = %#v, %v", resolved, err)
	}
}

func TestSchedulerFailureRejectsWaitingRetryDueAtOrBeforeCommit(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	repository := openRuntimeRepository(t)
	job := schedulerTargetJob("retry-past-due-job", JobPhaseLive, 10)
	if err := repository.CreateJobRun(ctx, job); err != nil {
		t.Fatalf("CreateJobRun() error = %v", err)
	}
	task := SchedulerTask{
		TaskID: "retry-past-due-task", DedupeKey: "retry:past-due:task",
		TargetKind: SchedulerTargetLiveScan, TargetID: job.JobID, HomeGeneration: 7,
		Lane: SchedulerLaneLive, ServiceClass: SchedulerServiceBackground,
		State: SchedulerTaskQueued, QueueOrderMS: 11, EnqueuedAtMS: 11, UpdatedAtMS: 11,
	}
	if err := repository.EnqueueSchedulerTask(ctx, task, 100); err != nil {
		t.Fatalf("EnqueueSchedulerTask() error = %v", err)
	}
	claimed, err := repository.ClaimSchedulerTask(ctx, task.TaskID, 20)
	if err != nil {
		t.Fatalf("ClaimSchedulerTask() error = %v", err)
	}
	class := RuntimeErrorIO
	commitAtMS := int64(22)
	cycle := SchedulerCycle{
		CycleID: "retry-past-due-cycle", TaskID: task.TaskID, Lane: task.Lane,
		SelectionReason: SchedulerSelectionLiveOnly, StopReason: SchedulerStopDependencyError,
		Outcome: SchedulerCycleFailed, BudgetFiles: 1, BudgetBytes: 1,
		BudgetActiveMS: 1, LiveDepth: 1,
		StartedAtMS: *claimed.LastStartedAtMS, FinishedAtMS: 21,
	}
	err = repository.CommitSchedulerCycle(ctx, SchedulerCycleCommit{
		TaskID: task.TaskID, ExpectedState: SchedulerTaskRunning, State: SchedulerTaskFailed,
		ErrorClass: &class, AtMS: commitAtMS, Cycle: cycle,
		Retry: &SchedulerRetryMutation{
			Disposition: SchedulerRetryWaiting, FailureCount: 1,
			LastErrorClass: class, NextRetryAtMS: &commitAtMS,
			RecoveryAction: SchedulerRecoveryNone,
		},
	})
	if !errors.Is(err, ErrSchedulerRetryTransition) {
		t.Fatalf("CommitSchedulerCycle() error = %v, want ErrSchedulerRetryTransition", err)
	}
	stored, readErr := repository.SchedulerTask(ctx, task.TaskID)
	if readErr != nil || stored.State != SchedulerTaskRunning {
		t.Fatalf("SchedulerTask(after rollback) = %#v, %v", stored, readErr)
	}
}

func TestSchedulerFailureRejectsInvalidRetryWithoutPartialCycle(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	repository := openRuntimeRepository(t)
	job := schedulerTargetJob("retry-rollback-job", JobPhaseLive, 10)
	if err := repository.CreateJobRun(ctx, job); err != nil {
		t.Fatalf("CreateJobRun() error = %v", err)
	}
	task := SchedulerTask{
		TaskID: "retry-rollback-task", DedupeKey: "retry:rollback",
		TargetKind: SchedulerTargetLiveScan, TargetID: job.JobID, HomeGeneration: 1,
		Lane: SchedulerLaneLive, ServiceClass: SchedulerServiceBackground,
		State: SchedulerTaskQueued, QueueOrderMS: 11, EnqueuedAtMS: 11, UpdatedAtMS: 11,
	}
	if err := repository.EnqueueSchedulerTask(ctx, task, 100); err != nil {
		t.Fatalf("EnqueueSchedulerTask() error = %v", err)
	}
	claimed, err := repository.ClaimSchedulerTask(ctx, task.TaskID, 20)
	if err != nil {
		t.Fatalf("ClaimSchedulerTask() error = %v", err)
	}
	class := RuntimeErrorPermission
	nextRetryAt := int64(30)
	cycle := SchedulerCycle{
		CycleID: "retry-rollback-cycle", TaskID: task.TaskID, Lane: task.Lane,
		SelectionReason: SchedulerSelectionLiveOnly, StopReason: SchedulerStopDependencyError,
		Outcome: SchedulerCycleFailed, BudgetFiles: 1, BudgetBytes: 1,
		BudgetActiveMS: 1, LiveDepth: 1, StartedAtMS: *claimed.LastStartedAtMS, FinishedAtMS: 21,
	}
	err = repository.CommitSchedulerCycle(ctx, SchedulerCycleCommit{
		TaskID: task.TaskID, ExpectedState: SchedulerTaskRunning, State: SchedulerTaskFailed,
		ErrorClass: &class, AtMS: 22, Cycle: cycle,
		Retry: &SchedulerRetryMutation{
			ExpectedRevision: 0, Disposition: SchedulerRetryBlocked, FailureCount: 1,
			LastErrorClass: class, NextRetryAtMS: &nextRetryAt,
			RecoveryAction: SchedulerRecoveryGrantPermission,
		},
	})
	if !errors.Is(err, ErrSchedulerRetryTransition) {
		t.Fatalf("CommitSchedulerCycle(invalid retry) error = %v, want ErrSchedulerRetryTransition", err)
	}
	readback, err := repository.SchedulerTask(ctx, task.TaskID)
	if err != nil || readback.State != SchedulerTaskRunning {
		t.Fatalf("SchedulerTask(after rollback) = %#v, %v", readback, err)
	}
	if _, err := repository.SchedulerCycle(ctx, cycle.CycleID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("SchedulerCycle(after rollback) error = %v, want ErrNotFound", err)
	}
	if _, err := repository.SchedulerRetryState(ctx, task.TaskID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("SchedulerRetryState(after rollback) error = %v, want ErrNotFound", err)
	}
}

func TestRunnableQueueAndClaimHonorDurableLifecyclePermit(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	repository := openRuntimeRepository(t)
	for index, lane := range []SchedulerLane{SchedulerLaneLive, SchedulerLaneBackfill} {
		phase := JobPhaseLive
		targetKind := SchedulerTargetLiveScan
		if lane == SchedulerLaneBackfill {
			phase = JobPhaseHistoryBackfill
			targetKind = SchedulerTargetBootstrap
		}
		job := schedulerTargetJob("permit-job-"+string(lane), phase, int64(10+index))
		if err := repository.CreateJobRun(ctx, job); err != nil {
			t.Fatalf("CreateJobRun(%s) error = %v", lane, err)
		}
		task := SchedulerTask{
			TaskID: "permit-task-" + string(lane), DedupeKey: "permit:" + string(lane),
			TargetKind: targetKind, TargetID: job.JobID, HomeGeneration: 3,
			Lane: lane, ServiceClass: SchedulerServiceBackground, State: SchedulerTaskQueued,
			QueueOrderMS: int64(20 + index), EnqueuedAtMS: int64(20 + index), UpdatedAtMS: int64(20 + index),
		}
		if err := repository.EnqueueSchedulerTask(ctx, task, 100); err != nil {
			t.Fatalf("EnqueueSchedulerTask(%s) error = %v", lane, err)
		}
	}
	lifecycle, err := repository.SchedulerLifecycle(ctx)
	if err != nil || lifecycle.HomeGeneration != 3 || lifecycle.UserPauseScope != LifecyclePauseNone {
		t.Fatalf("SchedulerLifecycle(auto init) = %#v, %v", lifecycle, err)
	}
	snapshot, err := repository.SchedulerRunnableQueueSnapshot(ctx)
	if err != nil || snapshot.LiveDepth != 1 || snapshot.BackfillDepth != 1 {
		t.Fatalf("SchedulerRunnableQueueSnapshot() = %#v, %v", snapshot, err)
	}

	lifecycle.UserPauseScope = LifecyclePauseBackfill
	lifecycle.LastEventID = "pause:backfill"
	lifecycle.Revision++
	lifecycle.UpdatedAtMS++
	lifecycle, err = repository.CompareAndSwapSchedulerLifecycle(ctx, lifecycle.Revision-1, lifecycle)
	if err != nil {
		t.Fatalf("CompareAndSwapSchedulerLifecycle(backfill pause) error = %v", err)
	}
	snapshot, err = repository.SchedulerRunnableQueueSnapshot(ctx)
	if err != nil || snapshot.LiveDepth != 1 || snapshot.BackfillDepth != 0 || snapshot.BackfillCandidate != nil {
		t.Fatalf("SchedulerRunnableQueueSnapshot(backfill paused) = %#v, %v", snapshot, err)
	}
	if _, err := repository.ClaimSchedulerTask(ctx, "permit-task-backfill", lifecycle.UpdatedAtMS+10); !errors.Is(err, ErrSchedulerPaused) {
		t.Fatalf("ClaimSchedulerTask(backfill paused) error = %v, want ErrSchedulerPaused", err)
	}

	lifecycle.UserPauseScope = LifecyclePauseAll
	lifecycle.LastEventID = "pause:all"
	lifecycle.Revision++
	lifecycle.UpdatedAtMS++
	if _, err := repository.CompareAndSwapSchedulerLifecycle(ctx, lifecycle.Revision-1, lifecycle); err != nil {
		t.Fatalf("CompareAndSwapSchedulerLifecycle(all pause) error = %v", err)
	}
	snapshot, err = repository.SchedulerRunnableQueueSnapshot(ctx)
	if err != nil || snapshot.LiveDepth != 0 || snapshot.BackfillDepth != 0 {
		t.Fatalf("SchedulerRunnableQueueSnapshot(all paused) = %#v, %v", snapshot, err)
	}
}

func TestRunnableQueueTailUsesTheSameGenerationAndPausePermit(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		highGeneration int64
		pauseBackfill  bool
	}{
		{name: "historical generation", highGeneration: 2},
		{name: "paused backfill lane", highGeneration: 1, pauseBackfill: true},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			repository := openRuntimeRepository(t)
			liveJob := schedulerTargetJob("tail-live", JobPhaseLive, 10)
			if err := repository.CreateJobRun(ctx, liveJob); err != nil {
				t.Fatalf("CreateJobRun(live) error = %v", err)
			}
			live := SchedulerTask{
				TaskID: "tail-live-task", DedupeKey: "tail-live-dedupe",
				TargetKind: SchedulerTargetLiveScan, TargetID: liveJob.JobID, HomeGeneration: 1,
				Lane: SchedulerLaneLive, ServiceClass: SchedulerServiceBackground,
				State: SchedulerTaskQueued, QueueOrderMS: 20, EnqueuedAtMS: 20, UpdatedAtMS: 20,
			}
			if err := repository.EnqueueSchedulerTask(ctx, live, 16); err != nil {
				t.Fatalf("EnqueueSchedulerTask(live) error = %v", err)
			}
			highJobAtMS := MaxSchedulerAdmissionTimestampMS - 2
			highTaskAtMS := MaxSchedulerAdmissionTimestampMS - 1
			highJob := schedulerTargetJob("tail-high", JobPhaseHistoryBackfill, highJobAtMS)
			if err := repository.CreateJobRun(ctx, highJob); err != nil {
				t.Fatalf("CreateJobRun(high tail) error = %v", err)
			}
			high := SchedulerTask{
				TaskID: "tail-high-task", DedupeKey: "tail-high-dedupe",
				TargetKind: SchedulerTargetBootstrap, TargetID: highJob.JobID,
				HomeGeneration: testCase.highGeneration, Lane: SchedulerLaneBackfill,
				ServiceClass: SchedulerServiceBackground, State: SchedulerTaskQueued,
				QueueOrderMS: highTaskAtMS, EnqueuedAtMS: highTaskAtMS,
				UpdatedAtMS: highTaskAtMS,
			}
			if err := repository.EnqueueSchedulerTask(ctx, high, 16); err != nil {
				t.Fatalf("EnqueueSchedulerTask(high tail) error = %v", err)
			}
			if testCase.pauseBackfill {
				lifecycle, err := repository.SchedulerLifecycle(ctx)
				if err != nil {
					t.Fatalf("SchedulerLifecycle() error = %v", err)
				}
				lifecycle.UserPauseScope = LifecyclePauseBackfill
				lifecycle.LastEventID = "tail:pause-backfill"
				lifecycle.Revision++
				lifecycle.UpdatedAtMS++
				if _, err := repository.CompareAndSwapSchedulerLifecycle(
					ctx, lifecycle.Revision-1, lifecycle,
				); err != nil {
					t.Fatalf("CompareAndSwapSchedulerLifecycle() error = %v", err)
				}
			}
			snapshot, err := repository.SchedulerRunnableQueueSnapshot(ctx)
			if err != nil || snapshot.MaxQueueOrderMS != live.QueueOrderMS ||
				snapshot.LiveDepth != 1 || snapshot.BackfillDepth != 0 {
				t.Fatalf("SchedulerRunnableQueueSnapshot() = %#v, %v", snapshot, err)
			}
		})
	}
}
