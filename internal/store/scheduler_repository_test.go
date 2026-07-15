package store

import (
	"context"
	"database/sql"
	"errors"
	"reflect"
	"sync"
	"testing"
)

// 测试 Scheduler repository 对 exact enqueue、容量、claim 与 yield cycle 做原子约束。
func TestSchedulerRepositoryEnqueuesClaimsAndYieldsWithCycleFacts(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	repository := openRuntimeRepository(t)
	job := schedulerTargetJob("live-job-1", JobPhaseLive, 10)
	if err := repository.CreateJobRun(ctx, job); err != nil {
		t.Fatalf("CreateJobRun() error = %v", err)
	}
	task := SchedulerTask{
		TaskID: "task-live-1", DedupeKey: "live:source-1:10", TargetKind: SchedulerTargetLiveScan,
		TargetID: job.JobID, HomeGeneration: 4, Lane: SchedulerLaneLive,
		ServiceClass: SchedulerServiceBackground, State: SchedulerTaskQueued,
		QueueOrderMS: 10, EnqueuedAtMS: 10, UpdatedAtMS: 10,
	}
	if err := repository.EnqueueSchedulerTask(ctx, task, 1); err != nil {
		t.Fatalf("EnqueueSchedulerTask() error = %v", err)
	}
	if err := repository.EnqueueSchedulerTask(ctx, task, 1); err != nil {
		t.Fatalf("EnqueueSchedulerTask(exact replay) error = %v", err)
	}
	conflict := task
	conflict.Lane = SchedulerLaneBackfill
	if err := repository.EnqueueSchedulerTask(ctx, conflict, 1); !errors.Is(err, ErrSchedulerConflict) {
		t.Fatalf("EnqueueSchedulerTask(conflict) error = %v, want ErrSchedulerConflict", err)
	}

	secondJob := schedulerTargetJob("live-job-2", JobPhaseLive, 11)
	if err := repository.CreateJobRun(ctx, secondJob); err != nil {
		t.Fatalf("CreateJobRun(second) error = %v", err)
	}
	second := task
	second.TaskID, second.DedupeKey, second.TargetID = "task-live-2", "live:source-2:10", secondJob.JobID
	second.QueueOrderMS, second.EnqueuedAtMS, second.UpdatedAtMS = 11, 11, 11
	if err := repository.EnqueueSchedulerTask(ctx, second, 1); !errors.Is(err, ErrSchedulerQueueFull) {
		t.Fatalf("EnqueueSchedulerTask(full) error = %v, want ErrSchedulerQueueFull", err)
	}

	claimed, err := repository.ClaimSchedulerTask(ctx, task.TaskID, 20)
	if err != nil {
		t.Fatalf("ClaimSchedulerTask() error = %v", err)
	}
	if claimed.State != SchedulerTaskRunning || claimed.FirstStartedAtMS == nil ||
		*claimed.FirstStartedAtMS != 20 || claimed.LastStartedAtMS == nil || *claimed.LastStartedAtMS != 20 {
		t.Fatalf("ClaimSchedulerTask() = %#v, want first running claim at 20", claimed)
	}
	cycle := SchedulerCycle{
		CycleID: "cycle-live-1", TaskID: task.TaskID, Lane: SchedulerLaneLive,
		SelectionReason: SchedulerSelectionLiveOnly, StopReason: SchedulerStopByteBudget,
		Outcome: SchedulerCycleYielded, BudgetFiles: 4, BudgetBytes: 1024,
		BudgetActiveMS: 50, ConsumedFiles: 1, ConsumedBytes: 1024, ActiveMS: 12,
		LiveDepth: 1, BackfillDepth: 1, OldestLiveWaitMS: 10,
		OldestBackfillWaitMS: 30, StartedAtMS: 20, FinishedAtMS: 32,
	}
	if err := repository.CommitSchedulerCycle(ctx, SchedulerCycleCommit{
		TaskID: task.TaskID, ExpectedState: SchedulerTaskRunning, State: SchedulerTaskQueued,
		QueueOrderMS: 33, FilesDelta: 1, BytesDelta: 1024, AtMS: 33, Cycle: cycle,
	}); err != nil {
		t.Fatalf("CommitSchedulerCycle() error = %v", err)
	}

	got, err := repository.SchedulerTask(ctx, task.TaskID)
	if err != nil {
		t.Fatalf("SchedulerTask() error = %v", err)
	}
	if got.State != SchedulerTaskQueued || got.QueueOrderMS != 33 || got.FilesProcessed != 1 ||
		got.BytesProcessed != 1024 || got.SliceCount != 1 || got.FinishedAtMS != nil {
		t.Fatalf("SchedulerTask(after yield) = %#v", got)
	}
	cycles, err := repository.ListSchedulerCycles(ctx, SchedulerCycleFilter{TaskID: &task.TaskID, Limit: 10})
	if err != nil || len(cycles) != 1 || cycles[0] != cycle {
		t.Fatalf("ListSchedulerCycles() = %#v, %v, want exact cycle", cycles, err)
	}

	if _, err := repository.ClaimSchedulerTask(ctx, task.TaskID, 32); !errors.Is(err, ErrSchedulerTransition) {
		t.Fatalf("ClaimSchedulerTask(stale time) error = %v, want ErrSchedulerTransition", err)
	}
}

func TestSchedulerCycleRejectsActiveTimeAboveBudget(t *testing.T) {
	t.Parallel()

	commit := SchedulerCycleCommit{
		TaskID: "task-active-over-budget", ExpectedState: SchedulerTaskRunning,
		State: SchedulerTaskQueued, QueueOrderMS: 4, AtMS: 4,
		Cycle: SchedulerCycle{
			CycleID: "cycle-active-over-budget", TaskID: "task-active-over-budget",
			Lane: SchedulerLaneLive, SelectionReason: SchedulerSelectionLiveOnly,
			StopReason: SchedulerStopTimeBudget, Outcome: SchedulerCycleYielded,
			BudgetFiles: 1, BudgetBytes: 1, BudgetActiveMS: 1,
			ActiveMS: 2, StartedAtMS: 2, FinishedAtMS: 3,
		},
	}
	if err := validateSchedulerCycleCommit(commit); !errors.Is(err, ErrSchedulerTransition) {
		t.Fatalf("validateSchedulerCycleCommit(active over budget) error = %v, want ErrSchedulerTransition", err)
	}
}

func TestSchedulerRepositoryExactReplayUsesImmutableAdmissionPayload(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	repository := openRuntimeRepository(t)
	initialJob := schedulerTargetJob("replay-lifecycle", JobPhaseLive, 10)
	resumeJob := schedulerTargetJob("replay-lifecycle-resume", JobPhaseLive, 11)
	for _, job := range []JobRun{initialJob, resumeJob} {
		if err := repository.CreateJobRun(ctx, job); err != nil {
			t.Fatalf("CreateJobRun(%q) error = %v", job.JobID, err)
		}
	}
	original := SchedulerTask{
		TaskID: "task-replay-lifecycle", DedupeKey: "replay:lifecycle",
		TargetKind: SchedulerTargetLiveScan, TargetID: initialJob.JobID, HomeGeneration: 1,
		Lane: SchedulerLaneLive, ServiceClass: SchedulerServiceBackground,
		State: SchedulerTaskQueued, QueueOrderMS: 12, EnqueuedAtMS: 12, UpdatedAtMS: 12,
	}
	if err := repository.EnqueueSchedulerTask(ctx, original, 10_000); err != nil {
		t.Fatalf("EnqueueSchedulerTask() error = %v", err)
	}
	promoted, err := repository.PromoteSchedulerTask(ctx, original.DedupeKey, 13)
	if err != nil || promoted.ServiceClass != SchedulerServiceInteractive {
		t.Fatalf("PromoteSchedulerTask() = %#v, %v", promoted, err)
	}
	if err := repository.EnqueueSchedulerTask(ctx, original, 10_000); err != nil {
		t.Fatalf("EnqueueSchedulerTask(replay after promotion) error = %v", err)
	}
	conflict := original
	conflict.ServiceClass = SchedulerServiceInteractive
	if err := repository.EnqueueSchedulerTask(ctx, conflict, 10_000); !errors.Is(err, ErrSchedulerConflict) {
		t.Fatalf("EnqueueSchedulerTask(conflicting admission class) error = %v, want conflict", err)
	}

	claimed, err := repository.ClaimSchedulerTask(ctx, original.TaskID, 20)
	if err != nil {
		t.Fatalf("ClaimSchedulerTask() error = %v", err)
	}
	yielded := SchedulerCycle{
		CycleID: "cycle-replay-yielded", TaskID: original.TaskID, Lane: original.Lane,
		SelectionReason: SchedulerSelectionLiveOnly, StopReason: SchedulerStopByteBudget,
		Outcome: SchedulerCycleYielded, BudgetFiles: 1, BudgetBytes: 1,
		BudgetActiveMS: 1, LiveDepth: 1, StartedAtMS: *claimed.LastStartedAtMS, FinishedAtMS: 21,
	}
	if err := repository.CommitSchedulerCycle(ctx, SchedulerCycleCommit{
		TaskID: original.TaskID, ExpectedState: SchedulerTaskRunning, State: SchedulerTaskQueued,
		QueueOrderMS: 22, AtMS: 22, Cycle: yielded,
	}); err != nil {
		t.Fatalf("CommitSchedulerCycle(yielded) error = %v", err)
	}
	if err := repository.EnqueueSchedulerTask(ctx, original, 10_000); err != nil {
		t.Fatalf("EnqueueSchedulerTask(replay after yield) error = %v", err)
	}
	claimed, err = repository.ClaimSchedulerTask(ctx, original.TaskID, 23)
	if err != nil {
		t.Fatalf("ClaimSchedulerTask(after yield) error = %v", err)
	}
	cancelled := RuntimeErrorCanceled
	interrupted := SchedulerCycle{
		CycleID: "cycle-replay-interrupted", TaskID: original.TaskID, Lane: original.Lane,
		SelectionReason: SchedulerSelectionLiveOnly, StopReason: SchedulerStopCancelled,
		Outcome: SchedulerCycleInterrupted, BudgetFiles: 1, BudgetBytes: 1,
		BudgetActiveMS: 1, LiveDepth: 1, StartedAtMS: *claimed.LastStartedAtMS, FinishedAtMS: 24,
	}
	if err := repository.CommitSchedulerCycle(ctx, SchedulerCycleCommit{
		TaskID: original.TaskID, ExpectedState: SchedulerTaskRunning, State: SchedulerTaskInterrupted,
		ErrorClass: &cancelled, AtMS: 25, Cycle: interrupted,
	}); err != nil {
		t.Fatalf("CommitSchedulerCycle(interrupted) error = %v", err)
	}
	if _, err := repository.RecoverSchedulerTask(
		ctx, original.TaskID, initialJob.JobID, resumeJob.JobID, 26, 26,
	); err != nil {
		t.Fatalf("RecoverSchedulerTask() error = %v", err)
	}
	if err := repository.EnqueueSchedulerTask(ctx, original, 10_000); err != nil {
		t.Fatalf("EnqueueSchedulerTask(replay after recovery) error = %v", err)
	}
	if _, err := repository.ClaimSchedulerTask(ctx, original.TaskID, 27); err != nil {
		t.Fatalf("ClaimSchedulerTask(recovered) error = %v", err)
	}
	completed := SchedulerCycle{
		CycleID: "cycle-replay-completed", TaskID: original.TaskID, Lane: original.Lane,
		SelectionReason: SchedulerSelectionLiveOnly, StopReason: SchedulerStopCompleted,
		Outcome: SchedulerCycleCompleted, BudgetFiles: 1, BudgetBytes: 1,
		BudgetActiveMS: 1, LiveDepth: 1, StartedAtMS: 27, FinishedAtMS: 28,
	}
	if err := repository.CommitSchedulerCycle(ctx, SchedulerCycleCommit{
		TaskID: original.TaskID, ExpectedState: SchedulerTaskRunning, State: SchedulerTaskSucceeded,
		AtMS: 29, Cycle: completed,
	}); err != nil {
		t.Fatalf("CommitSchedulerCycle(completed) error = %v", err)
	}
	if err := repository.EnqueueSchedulerTask(ctx, original, 10_000); err != nil {
		t.Fatalf("EnqueueSchedulerTask(replay after terminal) error = %v", err)
	}
}

func TestSchedulerRepositoryOrdersSameMillisecondCyclesByCommitOrder(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	repository := openRuntimeRepository(t)
	liveJob := schedulerTargetJob("order-live", JobPhaseLive, 10)
	backfillJob := schedulerTargetJob("order-backfill", JobPhaseHistoryBackfill, 11)
	for _, job := range []JobRun{liveJob, backfillJob} {
		if err := repository.CreateJobRun(ctx, job); err != nil {
			t.Fatalf("CreateJobRun(%q) error = %v", job.JobID, err)
		}
	}
	tasks := []SchedulerTask{
		{
			TaskID: "task-order-live", DedupeKey: "order:live",
			TargetKind: SchedulerTargetLiveScan, TargetID: liveJob.JobID, HomeGeneration: 1,
			Lane: SchedulerLaneLive, ServiceClass: SchedulerServiceBackground,
			State: SchedulerTaskQueued, QueueOrderMS: 10, EnqueuedAtMS: 10, UpdatedAtMS: 10,
		},
		{
			TaskID: "task-order-backfill", DedupeKey: "order:backfill",
			TargetKind: SchedulerTargetBootstrap, TargetID: backfillJob.JobID, HomeGeneration: 1,
			Lane: SchedulerLaneBackfill, ServiceClass: SchedulerServiceBackground,
			State: SchedulerTaskQueued, QueueOrderMS: 11, EnqueuedAtMS: 11, UpdatedAtMS: 11,
		},
	}
	for index, task := range tasks {
		if err := repository.EnqueueSchedulerTask(ctx, task, 10_000); err != nil {
			t.Fatalf("EnqueueSchedulerTask(%q) error = %v", task.TaskID, err)
		}
		claimed, err := repository.ClaimSchedulerTask(ctx, task.TaskID, 20)
		if err != nil {
			t.Fatalf("ClaimSchedulerTask(%q) error = %v", task.TaskID, err)
		}
		cycleID := "z-earlier"
		if index == 1 {
			cycleID = "a-later"
		}
		cycle := SchedulerCycle{
			CycleID: cycleID, TaskID: task.TaskID, Lane: task.Lane,
			SelectionReason: SchedulerSelectionLiveOnly, StopReason: SchedulerStopCompleted,
			Outcome: SchedulerCycleCompleted, BudgetFiles: 1, BudgetBytes: 1,
			BudgetActiveMS: 1, StartedAtMS: *claimed.LastStartedAtMS, FinishedAtMS: 30,
		}
		if task.Lane == SchedulerLaneBackfill {
			cycle.SelectionReason = SchedulerSelectionBackfillOnly
		}
		if err := repository.CommitSchedulerCycle(ctx, SchedulerCycleCommit{
			TaskID: task.TaskID, ExpectedState: SchedulerTaskRunning, State: SchedulerTaskSucceeded,
			AtMS: int64(31 + index), Cycle: cycle,
		}); err != nil {
			t.Fatalf("CommitSchedulerCycle(%q) error = %v", task.TaskID, err)
		}
	}

	restarted := NewRepository(repository.database)
	cycles, err := restarted.ListSchedulerCycles(ctx, SchedulerCycleFilter{Limit: 8})
	if err != nil || len(cycles) != 2 || cycles[0].CycleID != "a-later" ||
		cycles[1].CycleID != "z-earlier" || consecutiveSchedulerLiveCyclesForTest(cycles) != 0 {
		t.Fatalf("ListSchedulerCycles(restart) = %#v, %v", cycles, err)
	}
}

func consecutiveSchedulerLiveCyclesForTest(cycles []SchedulerCycle) int {
	count := 0
	for _, cycle := range cycles {
		if cycle.Lane != SchedulerLaneLive {
			break
		}
		count++
	}
	return count
}

// 测试 Scheduler repository 的交互提升、稳定队列顺序与重启恢复目标替换。
func TestSchedulerRepositoryPromotesAndRecoversWithoutDuplicatingWork(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	repository := openRuntimeRepository(t)
	liveOne := schedulerTargetJob("live-promote-1", JobPhaseLive, 10)
	liveTwo := schedulerTargetJob("live-promote-2", JobPhaseLive, 11)
	backfill := schedulerTargetJob("bootstrap-promote-1", JobPhaseHistoryBackfill, 12)
	for _, job := range []JobRun{liveOne, liveTwo, backfill} {
		if err := repository.CreateJobRun(ctx, job); err != nil {
			t.Fatalf("CreateJobRun(%q) error = %v", job.JobID, err)
		}
	}
	tasks := []SchedulerTask{
		{
			TaskID: "task-promote-1", DedupeKey: "live:promote:1",
			TargetKind: SchedulerTargetLiveScan, TargetID: liveOne.JobID, HomeGeneration: 7,
			Lane: SchedulerLaneLive, ServiceClass: SchedulerServiceBackground,
			State: SchedulerTaskQueued, QueueOrderMS: 10, EnqueuedAtMS: 10, UpdatedAtMS: 10,
		},
		{
			TaskID: "task-promote-2", DedupeKey: "live:promote:2",
			TargetKind: SchedulerTargetLiveScan, TargetID: liveTwo.JobID, HomeGeneration: 7,
			Lane: SchedulerLaneLive, ServiceClass: SchedulerServiceBackground,
			State: SchedulerTaskQueued, QueueOrderMS: 11, EnqueuedAtMS: 11, UpdatedAtMS: 11,
		},
		{
			TaskID: "task-backfill-1", DedupeKey: "bootstrap:promote:1",
			TargetKind: SchedulerTargetBootstrap, TargetID: backfill.JobID, HomeGeneration: 7,
			Lane: SchedulerLaneBackfill, ServiceClass: SchedulerServiceBackground,
			State: SchedulerTaskQueued, QueueOrderMS: 12, EnqueuedAtMS: 12, UpdatedAtMS: 12,
		},
	}
	for _, task := range tasks {
		if err := repository.EnqueueSchedulerTask(ctx, task, 4); err != nil {
			t.Fatalf("EnqueueSchedulerTask(%q) error = %v", task.TaskID, err)
		}
	}

	active := true
	got, err := repository.ListSchedulerTasks(ctx, SchedulerTaskFilter{Active: &active, Limit: 10})
	if err != nil || len(got) != 3 || got[0].TaskID != tasks[0].TaskID ||
		got[1].TaskID != tasks[1].TaskID || got[2].TaskID != tasks[2].TaskID {
		t.Fatalf("ListSchedulerTasks() = %#v, %v, want queue order 10/11/12", got, err)
	}
	promoted, err := repository.PromoteSchedulerTask(ctx, tasks[0].DedupeKey, 20)
	if err != nil || promoted.ServiceClass != SchedulerServiceInteractive || promoted.TaskID != tasks[0].TaskID {
		t.Fatalf("PromoteSchedulerTask() = %#v, %v", promoted, err)
	}
	if replay, err := repository.PromoteSchedulerTask(ctx, tasks[0].DedupeKey, 21); err != nil || replay != promoted {
		t.Fatalf("PromoteSchedulerTask(replay) = %#v, %v, want exact readback", replay, err)
	}

	if _, err := repository.ClaimSchedulerTask(ctx, tasks[0].TaskID, 22); err != nil {
		t.Fatalf("ClaimSchedulerTask() error = %v", err)
	}
	resumedJob := schedulerTargetJob("live-promote-resume-1", JobPhaseLive, 23)
	if err := repository.CreateJobRun(ctx, resumedJob); err != nil {
		t.Fatalf("CreateJobRun(resume target) error = %v", err)
	}
	recovered, err := repository.RecoverSchedulerTask(
		ctx, tasks[0].TaskID, liveOne.JobID, resumedJob.JobID, 30, 30,
	)
	if err != nil {
		t.Fatalf("RecoverSchedulerTask() error = %v", err)
	}
	if recovered.State != SchedulerTaskQueued || recovered.TargetID != resumedJob.JobID ||
		recovered.QueueOrderMS != 30 || recovered.ServiceClass != SchedulerServiceInteractive ||
		recovered.FinishedAtMS != nil || recovered.LastErrorClass != nil {
		t.Fatalf("RecoverSchedulerTask() = %#v", recovered)
	}
	if replay, err := repository.RecoverSchedulerTask(
		ctx, tasks[0].TaskID, liveOne.JobID, resumedJob.JobID, 30, 31,
	); err != nil || !reflect.DeepEqual(replay, recovered) {
		t.Fatalf("RecoverSchedulerTask(replay) = %#v, %v, want exact readback", replay, err)
	}
}

// 测试 Live scan job 以 typed snapshot 持久化，并按 interrupted lineage 克隆恢复。
func TestLiveScanRepositoryCreatesReplaysAndResumesTypedAction(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	repository := openRuntimeRepository(t)
	current := testSourceFingerprint("source-live-1", "/confirmed/sessions/live.jsonl", 41, 2048, 100)
	job := schedulerTargetJob("live-scan-job-1", JobPhaseLive, 10)
	facts := LiveScanJob{
		JobID: job.JobID, RequestID: "live-request-1", HomeGeneration: 8,
		HomePath: "/confirmed", HomeDeviceID: "device-home", HomeInode: 90,
		ActionKind: LiveScanActionAdded, Current: current, UpdatedAtMS: 10,
	}
	if err := repository.CreateLiveScanJob(ctx, job, facts); err != nil {
		t.Fatalf("CreateLiveScanJob() error = %v", err)
	}
	if err := repository.CreateLiveScanJob(ctx, job, facts); err != nil {
		t.Fatalf("CreateLiveScanJob(exact replay) error = %v", err)
	}
	conflict := facts
	conflict.Current.SizeBytes++
	if err := repository.CreateLiveScanJob(ctx, job, conflict); !errors.Is(err, ErrLiveScanConflict) {
		t.Fatalf("CreateLiveScanJob(conflict) error = %v, want ErrLiveScanConflict", err)
	}
	gotJob, gotFacts, err := repository.LiveScanRun(ctx, job.JobID)
	if err != nil || !jobRunsEqual(gotJob, job) || !liveScanJobsEqual(gotFacts, facts) {
		t.Fatalf("LiveScanRun() = %#v, %#v, %v", gotJob, gotFacts, err)
	}

	if err := repository.TransitionJobRun(ctx, JobTransition{
		JobID: job.JobID, ExpectedState: JobQueued, State: JobRunning,
		Phase: JobPhaseLive, AtMS: 20,
	}); err != nil {
		t.Fatalf("TransitionJobRun(running) error = %v", err)
	}
	if err := repository.TransitionJobRun(ctx, JobTransition{
		JobID: job.JobID, ExpectedState: JobRunning, State: JobInterrupted,
		Phase: JobPhaseLive, AtMS: 30,
	}); err != nil {
		t.Fatalf("TransitionJobRun(interrupted) error = %v", err)
	}
	oldID := job.JobID
	resumedJob := JobRun{
		JobID: "live-scan-resume-1", JobType: job.JobType, RequestedBy: job.RequestedBy,
		Priority: job.Priority, State: JobQueued, Phase: JobPhaseLive,
		ResumeOfJobID: &oldID, CreatedAtMS: 31, UpdatedAtMS: 31,
	}
	resumedFacts := facts
	resumedFacts.JobID = resumedJob.JobID
	resumedFacts.RequestID = "live-request-resume-1"
	resumedFacts.UpdatedAtMS = 31
	if err := repository.ResumeLiveScanJob(ctx, job.JobID, resumedJob, resumedFacts); err != nil {
		t.Fatalf("ResumeLiveScanJob() error = %v", err)
	}
	if err := repository.ResumeLiveScanJob(ctx, job.JobID, resumedJob, resumedFacts); err != nil {
		t.Fatalf("ResumeLiveScanJob(exact replay) error = %v", err)
	}
	resumedRead, factsRead, err := repository.LiveScanRun(ctx, resumedJob.JobID)
	if err != nil || !jobRunsEqual(resumedRead, resumedJob) || !liveScanJobsEqual(factsRead, resumedFacts) {
		t.Fatalf("LiveScanRun(resumed) = %#v, %#v, %v", resumedRead, factsRead, err)
	}
}

// 测试 queue snapshot 直接聚合每个lane，而不是从一个全局截断列表推断深度和候选。
func TestSchedulerRepositoryBuildsExactLaneSnapshot(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	repository := openRuntimeRepository(t)
	backfillJob := schedulerTargetJob("snapshot-backfill", JobPhaseHistoryBackfill, 10)
	liveJob := schedulerTargetJob("snapshot-live", JobPhaseLive, 11)
	for _, job := range []JobRun{backfillJob, liveJob} {
		if err := repository.CreateJobRun(ctx, job); err != nil {
			t.Fatalf("CreateJobRun(%q) error = %v", job.JobID, err)
		}
	}
	backfill := SchedulerTask{
		TaskID: "task-snapshot-backfill", DedupeKey: "snapshot:backfill",
		TargetKind: SchedulerTargetBootstrap, TargetID: backfillJob.JobID, HomeGeneration: 1,
		Lane: SchedulerLaneBackfill, ServiceClass: SchedulerServiceBackground,
		State: SchedulerTaskQueued, QueueOrderMS: 10, EnqueuedAtMS: 10, UpdatedAtMS: 10,
	}
	live := SchedulerTask{
		TaskID: "task-snapshot-live", DedupeKey: "snapshot:live",
		TargetKind: SchedulerTargetLiveScan, TargetID: liveJob.JobID, HomeGeneration: 1,
		Lane: SchedulerLaneLive, ServiceClass: SchedulerServiceBackground,
		State: SchedulerTaskQueued, QueueOrderMS: 11, EnqueuedAtMS: 11, UpdatedAtMS: 11,
	}
	for _, task := range []SchedulerTask{backfill, live} {
		if err := repository.EnqueueSchedulerTask(ctx, task, 10_000); err != nil {
			t.Fatalf("EnqueueSchedulerTask(%q) error = %v", task.TaskID, err)
		}
	}

	snapshot, err := repository.SchedulerQueueSnapshot(ctx)
	if err != nil || snapshot.LiveDepth != 1 || snapshot.BackfillDepth != 1 ||
		snapshot.LiveCandidate == nil || snapshot.LiveCandidate.TaskID != live.TaskID ||
		snapshot.BackfillCandidate == nil || snapshot.BackfillCandidate.TaskID != backfill.TaskID ||
		snapshot.MaxQueueOrderMS != live.QueueOrderMS {
		t.Fatalf("SchedulerQueueSnapshot() = %#v, %v", snapshot, err)
	}
}

func TestSchedulerRepositoryQueueSnapshotUsesOneReadTransaction(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	repository := openRuntimeRepository(t)
	job := schedulerTargetJob("snapshot-concurrent-live", JobPhaseLive, 10)
	if err := repository.CreateJobRun(ctx, job); err != nil {
		t.Fatalf("CreateJobRun() error = %v", err)
	}
	task := SchedulerTask{
		TaskID: "task-snapshot-concurrent-live", DedupeKey: "snapshot:concurrent:live",
		TargetKind: SchedulerTargetLiveScan, TargetID: job.JobID, HomeGeneration: 1,
		Lane: SchedulerLaneLive, ServiceClass: SchedulerServiceBackground,
		State: SchedulerTaskQueued, QueueOrderMS: 10, EnqueuedAtMS: 10, UpdatedAtMS: 10,
	}
	var once sync.Once
	repository.schedulerQueueSnapshotHook = func(lane SchedulerLane) error {
		if lane != SchedulerLaneLive {
			return nil
		}
		once.Do(func() {
			done := make(chan error, 1)
			go func() { done <- repository.EnqueueSchedulerTask(ctx, task, 10_000) }()
			if err := <-done; err != nil {
				t.Errorf("concurrent EnqueueSchedulerTask() error = %v", err)
			}
		})
		return nil
	}
	snapshot, err := repository.SchedulerQueueSnapshot(ctx)
	if err != nil || snapshot.LiveDepth != 0 || snapshot.LiveCandidate != nil ||
		snapshot.MaxQueueOrderMS != 0 {
		t.Fatalf("SchedulerQueueSnapshot(concurrent commit) = %#v, %v", snapshot, err)
	}
	repository.schedulerQueueSnapshotHook = nil
	next, err := repository.SchedulerQueueSnapshot(ctx)
	if err != nil || next.LiveDepth != 1 || next.LiveCandidate == nil ||
		next.LiveCandidate.TaskID != task.TaskID || next.MaxQueueOrderMS != task.QueueOrderMS {
		t.Fatalf("SchedulerQueueSnapshot(next) = %#v, %v", next, err)
	}
}

// 测试取消发生在queue snapshot transaction内部时仍保留context因果。
func TestSchedulerRepositoryQueueSnapshotPreservesCancellationCause(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	repository := openRuntimeRepository(t)
	repository.schedulerQueueSnapshotHook = func(lane SchedulerLane) error {
		if lane == SchedulerLaneLive {
			cancel()
			return sql.ErrTxDone
		}
		return nil
	}

	_, err := repository.SchedulerQueueSnapshot(ctx)
	if !errors.Is(err, context.Canceled) || !errors.Is(err, sql.ErrTxDone) {
		t.Fatalf(
			"SchedulerQueueSnapshot(cancel in transaction) error = %v, want context.Canceled and sql.ErrTxDone",
			err,
		)
	}
}

// 测试 Store 的single-heavy-owner门禁和recoverable keyset分页。
func TestSchedulerRepositoryRejectsSecondRunningTaskAndPagesRecovery(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	repository := openRuntimeRepository(t)
	firstJob := schedulerTargetJob("owner-first", JobPhaseLive, 10)
	secondJob := schedulerTargetJob("owner-second", JobPhaseLive, 11)
	for _, job := range []JobRun{firstJob, secondJob} {
		if err := repository.CreateJobRun(ctx, job); err != nil {
			t.Fatalf("CreateJobRun(%q) error = %v", job.JobID, err)
		}
	}
	tasks := []SchedulerTask{
		{
			TaskID: "task-owner-first", DedupeKey: "owner:first",
			TargetKind: SchedulerTargetLiveScan, TargetID: firstJob.JobID, HomeGeneration: 1,
			Lane: SchedulerLaneLive, ServiceClass: SchedulerServiceBackground,
			State: SchedulerTaskQueued, QueueOrderMS: 10, EnqueuedAtMS: 10, UpdatedAtMS: 10,
		},
		{
			TaskID: "task-owner-second", DedupeKey: "owner:second",
			TargetKind: SchedulerTargetLiveScan, TargetID: secondJob.JobID, HomeGeneration: 1,
			Lane: SchedulerLaneLive, ServiceClass: SchedulerServiceBackground,
			State: SchedulerTaskQueued, QueueOrderMS: 11, EnqueuedAtMS: 11, UpdatedAtMS: 11,
		},
	}
	for _, task := range tasks {
		if err := repository.EnqueueSchedulerTask(ctx, task, 10_000); err != nil {
			t.Fatalf("EnqueueSchedulerTask(%q) error = %v", task.TaskID, err)
		}
	}
	if _, err := repository.ClaimSchedulerTask(ctx, tasks[1].TaskID, 20); err != nil {
		t.Fatalf("ClaimSchedulerTask(second) error = %v", err)
	}
	if err := repository.CommitSchedulerCycle(ctx, SchedulerCycleCommit{
		TaskID: tasks[1].TaskID, ExpectedState: SchedulerTaskRunning, State: SchedulerTaskInterrupted,
		QueueOrderMS: 21, ErrorClass: pointerToValue(RuntimeErrorCanceled), AtMS: 21,
		Cycle: SchedulerCycle{
			CycleID: "cycle-owner-second-interrupted", TaskID: tasks[1].TaskID, Lane: SchedulerLaneLive,
			SelectionReason: SchedulerSelectionLiveOnly, StopReason: SchedulerStopCancelled,
			Outcome: SchedulerCycleInterrupted, BudgetFiles: 1, BudgetBytes: 1,
			BudgetActiveMS: 1, StartedAtMS: 20, FinishedAtMS: 20,
		},
	}); err != nil {
		t.Fatalf("CommitSchedulerCycle(interrupted) error = %v", err)
	}
	if _, err := repository.ClaimSchedulerTask(ctx, tasks[0].TaskID, 22); err != nil {
		t.Fatalf("ClaimSchedulerTask(first) error = %v", err)
	}
	thirdJob := schedulerTargetJob("owner-third", JobPhaseLive, 23)
	if err := repository.CreateJobRun(ctx, thirdJob); err != nil {
		t.Fatalf("CreateJobRun(third) error = %v", err)
	}
	third := SchedulerTask{
		TaskID: "task-owner-third", DedupeKey: "owner:third",
		TargetKind: SchedulerTargetLiveScan, TargetID: thirdJob.JobID, HomeGeneration: 1,
		Lane: SchedulerLaneLive, ServiceClass: SchedulerServiceBackground,
		State: SchedulerTaskQueued, QueueOrderMS: 23, EnqueuedAtMS: 23, UpdatedAtMS: 23,
	}
	if err := repository.EnqueueSchedulerTask(ctx, third, 10_000); err != nil {
		t.Fatalf("EnqueueSchedulerTask(third) error = %v", err)
	}
	if _, err := repository.ClaimSchedulerTask(ctx, third.TaskID, 24); !errors.Is(err, ErrSchedulerBusy) {
		t.Fatalf("ClaimSchedulerTask(third) error = %v, want ErrSchedulerBusy", err)
	}

	firstPage, cursor, err := repository.ListRecoverableSchedulerTasks(ctx, nil, 1)
	if err != nil || len(firstPage) != 1 || cursor == nil || firstPage[0].TaskID != tasks[0].TaskID {
		t.Fatalf("ListRecoverableSchedulerTasks(first) = %#v, %#v, %v", firstPage, cursor, err)
	}
	secondPage, cursor, err := repository.ListRecoverableSchedulerTasks(ctx, cursor, 1)
	if err != nil || len(secondPage) != 1 || cursor == nil || secondPage[0].TaskID != tasks[1].TaskID {
		t.Fatalf("ListRecoverableSchedulerTasks(second) = %#v, %#v, %v", secondPage, cursor, err)
	}
	lastPage, cursor, err := repository.ListRecoverableSchedulerTasks(ctx, cursor, 1)
	if err != nil || len(lastPage) != 0 || cursor != nil {
		t.Fatalf("ListRecoverableSchedulerTasks(last) = %#v, %#v, %v", lastPage, cursor, err)
	}
}

func schedulerTargetJob(jobID string, phase JobPhase, atMS int64) JobRun {
	return JobRun{
		JobID: jobID, JobType: "scheduler-test", RequestedBy: "test", Priority: 1,
		State: JobQueued, Phase: phase, CreatedAtMS: atMS, UpdatedAtMS: atMS,
	}
}
