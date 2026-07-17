package store

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	storesqlite "github.com/SisyphusSQ/codex-pulse/internal/store/sqlite"
)

// 测试 MetricsSnapshot 在一个窗口中聚合 runtime、scheduler、job 与 source 权威事实。
func TestMetricsSnapshotAggregatesRuntimeSchedulerJobAndSourceFacts(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	repository := openRuntimeRepository(t)
	for _, capturedAtMS := range []int64{900, 1_500} {
		if err := repository.RecordAppRuntimeSample(ctx, validAppRuntimeSample(capturedAtMS)); err != nil {
			t.Fatalf("RecordAppRuntimeSample(%d) error = %v", capturedAtMS, err)
		}
	}

	schedulerJob := schedulerTargetJob("metrics-scheduler-job", JobPhaseLive, 1_000)
	if err := repository.CreateJobRun(ctx, schedulerJob); err != nil {
		t.Fatalf("CreateJobRun(scheduler) error = %v", err)
	}
	task := SchedulerTask{
		TaskID: "metrics-task", DedupeKey: "metrics:live", TargetKind: SchedulerTargetLiveScan,
		TargetID: schedulerJob.JobID, HomeGeneration: 1, Lane: SchedulerLaneLive,
		ServiceClass: SchedulerServiceBackground, State: SchedulerTaskQueued,
		QueueOrderMS: 1_000, EnqueuedAtMS: 1_000, UpdatedAtMS: 1_000,
	}
	if err := repository.EnqueueSchedulerTask(ctx, task, 10); err != nil {
		t.Fatalf("EnqueueSchedulerTask() error = %v", err)
	}
	if _, err := repository.ClaimSchedulerTask(ctx, task.TaskID, 1_100); err != nil {
		t.Fatalf("ClaimSchedulerTask() error = %v", err)
	}
	cycle := SchedulerCycle{
		CycleID: "metrics-cycle", TaskID: task.TaskID, Lane: SchedulerLaneLive,
		SelectionReason: SchedulerSelectionLiveOnly, StopReason: SchedulerStopByteBudget,
		Outcome: SchedulerCycleYielded, BudgetFiles: 4, BudgetBytes: 4_096,
		BudgetActiveMS: 100, ConsumedFiles: 2, ConsumedBytes: 2_048, ActiveMS: 75,
		LiveDepth: 1, BackfillDepth: 0, OldestLiveWaitMS: 100,
		StartedAtMS: 1_100, FinishedAtMS: 1_200,
	}
	if err := repository.CommitSchedulerCycle(ctx, SchedulerCycleCommit{
		TaskID: task.TaskID, ExpectedState: SchedulerTaskRunning, State: SchedulerTaskQueued,
		QueueOrderMS: 1_201, FilesDelta: 2, BytesDelta: 2_048, AtMS: 1_201, Cycle: cycle,
	}); err != nil {
		t.Fatalf("CommitSchedulerCycle() error = %v", err)
	}
	retryJob := schedulerTargetJob("metrics-retry-job", JobPhaseHistoryBackfill, 1_250)
	if err := repository.CreateJobRun(ctx, retryJob); err != nil {
		t.Fatalf("CreateJobRun(retry) error = %v", err)
	}
	retryTask := SchedulerTask{
		TaskID: "metrics-retry-task", DedupeKey: "metrics:retry", TargetKind: SchedulerTargetBootstrap,
		TargetID: retryJob.JobID, HomeGeneration: 1, Lane: SchedulerLaneBackfill,
		ServiceClass: SchedulerServiceInteractive, State: SchedulerTaskQueued,
		QueueOrderMS: 1_250, EnqueuedAtMS: 1_250, UpdatedAtMS: 1_250,
	}
	if err := repository.EnqueueSchedulerTask(ctx, retryTask, 10); err != nil {
		t.Fatalf("EnqueueSchedulerTask(retry) error = %v", err)
	}
	claimedRetry, err := repository.ClaimSchedulerTask(ctx, retryTask.TaskID, 1_300)
	if err != nil {
		t.Fatalf("ClaimSchedulerTask(retry) error = %v", err)
	}
	retryAtMS := int64(1_800)
	retryErrorClass := RuntimeErrorUnavailable
	if err := repository.CommitSchedulerCycle(ctx, SchedulerCycleCommit{
		TaskID: retryTask.TaskID, ExpectedState: SchedulerTaskRunning, State: SchedulerTaskFailed,
		ErrorClass: &retryErrorClass, AtMS: 1_351,
		Cycle: SchedulerCycle{
			CycleID: "metrics-retry-cycle", TaskID: retryTask.TaskID, Lane: SchedulerLaneBackfill,
			SelectionReason: SchedulerSelectionBackfillOnly, StopReason: SchedulerStopDependencyError,
			Outcome: SchedulerCycleFailed, BudgetFiles: 1, BudgetBytes: 1, BudgetActiveMS: 100,
			BackfillDepth: 1, StartedAtMS: *claimedRetry.LastStartedAtMS, FinishedAtMS: 1_350,
		},
		Retry: &SchedulerRetryMutation{
			Disposition: SchedulerRetryWaiting, FailureCount: 1, LastErrorClass: retryErrorClass,
			NextRetryAtMS: &retryAtMS, RecoveryAction: SchedulerRecoveryNone,
		},
	}); err != nil {
		t.Fatalf("CommitSchedulerCycle(retry) error = %v", err)
	}

	completed := JobRun{
		JobID: "metrics-completed", JobType: "history_scan", RequestedBy: "test", Priority: 1,
		State: JobQueued, Phase: JobPhaseHistoryBackfill, CreatedAtMS: 900, UpdatedAtMS: 900,
	}
	if err := repository.CreateJobRun(ctx, completed); err != nil {
		t.Fatalf("CreateJobRun(completed) error = %v", err)
	}
	if err := repository.TransitionJobRun(ctx, JobTransition{
		JobID: completed.JobID, ExpectedState: JobQueued, State: JobRunning,
		Phase: completed.Phase, AtMS: 1_000,
	}); err != nil {
		t.Fatalf("TransitionJobRun(running) error = %v", err)
	}
	if err := repository.TransitionJobRun(ctx, JobTransition{
		JobID: completed.JobID, ExpectedState: JobRunning, State: JobSucceeded,
		Phase: completed.Phase, AtMS: 1_600,
	}); err != nil {
		t.Fatalf("TransitionJobRun(succeeded) error = %v", err)
	}
	active := JobRun{
		JobID: "metrics-active", JobType: "live_scan", RequestedBy: "test", Priority: 2,
		State: JobQueued, Phase: JobPhaseLive, CreatedAtMS: 1_700, UpdatedAtMS: 1_700,
	}
	if err := repository.CreateJobRun(ctx, active); err != nil {
		t.Fatalf("CreateJobRun(active) error = %v", err)
	}

	lastAttempt, lastSuccess, nextDue := int64(1_500), int64(800), int64(1_800)
	errorClass := RuntimeErrorTimeout
	failureCode := SourceFailureTimeout
	state := SourceState{
		SourceInstanceID: "metrics-source", SourceType: "synthetic", ScopeKey: "default",
		LastAttemptAtMS: &lastAttempt, LastSuccessAtMS: &lastSuccess, NextDueAtMS: &nextDue,
		ConsecutiveFailures: 3, LastErrorClass: &errorClass, LastFailureCode: &failureCode,
		FreshnessState: SourceFreshnessStale, CursorVersion: 1, UpdatedAtMS: 1_500,
	}
	if err := repository.UpsertSourceState(ctx, state); err != nil {
		t.Fatalf("UpsertSourceState() error = %v", err)
	}
	if err := repository.AppendSourceAttempt(ctx, SourceAttempt{
		RequestID: "metrics-attempt", SourceInstanceID: state.SourceInstanceID,
		StartedAtMS: 1_400, FinishedAtMS: 1_500, Outcome: SourceAttemptFailed,
		ErrorClass: &errorClass, FailureCode: &failureCode, AttemptCount: 1,
	}); err != nil {
		t.Fatalf("AppendSourceAttempt() error = %v", err)
	}

	snapshot, err := repository.MetricsSnapshot(ctx, MetricsSnapshotFilter{
		FromMS: 0, UntilMS: MetricsSnapshotWindowMS,
	})
	if err != nil {
		t.Fatalf("MetricsSnapshot() error = %v", err)
	}
	if snapshot.FromMS != 0 || snapshot.UntilMS != MetricsSnapshotWindowMS ||
		len(snapshot.RuntimeSamples) != 2 || snapshot.RuntimeSamples[0].CapturedAtMS != 1_500 ||
		snapshot.RuntimeSamples[1].CapturedAtMS != 900 {
		t.Fatalf("runtime snapshot = %#v", snapshot)
	}
	if snapshot.Scheduler.CycleCount != 2 || snapshot.Scheduler.YieldedCycles != 1 ||
		snapshot.Scheduler.FailedCycles != 1 ||
		snapshot.Scheduler.FilesScanned != 2 || snapshot.Scheduler.BytesRead != 2_048 ||
		snapshot.Scheduler.ActiveMS != 75 || snapshot.Scheduler.MaxCycleActiveMS != 75 ||
		snapshot.Scheduler.LastProgressAtMS == nil || *snapshot.Scheduler.LastProgressAtMS != 1_200 ||
		snapshot.Scheduler.LastBackfillProgressAtMS != nil ||
		len(snapshot.Scheduler.TaskStates) != 1 ||
		snapshot.Scheduler.TaskStates[0] != (SchedulerTaskStateMetric{State: SchedulerTaskQueued, Count: 1}) ||
		len(snapshot.Scheduler.Lanes) != 1 ||
		snapshot.Scheduler.Lanes[0] != (SchedulerLaneMetric{Lane: SchedulerLaneLive, Count: 1}) ||
		len(snapshot.Scheduler.ServiceClasses) != 1 ||
		snapshot.Scheduler.ServiceClasses[0] != (SchedulerServiceClassMetric{ServiceClass: SchedulerServiceBackground, Count: 1}) ||
		len(snapshot.Scheduler.RetryDispositions) != 1 ||
		snapshot.Scheduler.RetryDispositions[0] != (SchedulerRetryDispositionMetric{Disposition: SchedulerRetryWaiting, Count: 1}) ||
		len(snapshot.Scheduler.StopReasons) != 2 {
		t.Fatalf("scheduler metrics = %#v", snapshot.Scheduler)
	}
	if snapshot.Jobs.Queued != 3 || snapshot.Jobs.Running != 0 ||
		snapshot.Jobs.Succeeded != 1 || snapshot.Jobs.DurationCount != 1 ||
		snapshot.Jobs.DurationTotalMS != 600 || snapshot.Jobs.DurationMaxMS != 600 {
		t.Fatalf("job metrics = %#v", snapshot.Jobs)
	}
	if snapshot.Sources.Total != 1 || snapshot.Sources.Stale != 1 ||
		snapshot.Sources.ConsecutiveFailures != 3 || snapshot.Sources.MaxConsecutiveFailures != 3 ||
		snapshot.Sources.Attempts != 1 || snapshot.Sources.FailedAttempts != 1 ||
		snapshot.Sources.LastAttemptAtMS == nil || *snapshot.Sources.LastAttemptAtMS != 1_500 ||
		snapshot.Sources.LastSuccessAtMS == nil || *snapshot.Sources.LastSuccessAtMS != 800 ||
		snapshot.Sources.NextRetryAtMS == nil || *snapshot.Sources.NextRetryAtMS != 1_800 ||
		len(snapshot.Sources.CurrentErrorClasses) != 1 ||
		snapshot.Sources.CurrentErrorClasses[0] != (RuntimeErrorClassMetric{ErrorClass: RuntimeErrorTimeout, Count: 1}) ||
		len(snapshot.Sources.CurrentFailureCodes) != 1 ||
		snapshot.Sources.CurrentFailureCodes[0] != (SourceFailureCodeMetric{FailureCode: SourceFailureTimeout, Count: 1}) ||
		len(snapshot.Sources.AttemptErrorClasses) != 1 ||
		snapshot.Sources.AttemptErrorClasses[0] != (RuntimeErrorClassMetric{ErrorClass: RuntimeErrorTimeout, Count: 1}) ||
		len(snapshot.Sources.AttemptFailureCodes) != 1 ||
		snapshot.Sources.AttemptFailureCodes[0] != (SourceFailureCodeMetric{FailureCode: SourceFailureTimeout, Count: 1}) {
		t.Fatalf("source metrics = %#v", snapshot.Sources)
	}
}

// 测试 MetricsSnapshot 的多次 SELECT 共享同一个 SQLite read snapshot。
func TestMetricsSnapshotUsesOneReadTransactionAcrossConcurrentWriterCommit(t *testing.T) {
	t.Parallel()

	repository := openRuntimeRepository(t)
	if err := repository.RecordAppRuntimeSample(t.Context(), validAppRuntimeSample(100)); err != nil {
		t.Fatalf("RecordAppRuntimeSample() error = %v", err)
	}
	var once sync.Once
	repository.metricsSnapshotReadHook = func(stage string) error {
		if stage != "runtime" {
			return nil
		}
		var err error
		once.Do(func() {
			err = repository.UpsertSourceState(context.Background(), SourceState{
				SourceInstanceID: "concurrent-source", SourceType: "synthetic", ScopeKey: "default",
				FreshnessState: SourceFreshnessCurrent, CursorVersion: 1, UpdatedAtMS: 150,
			})
		})
		return err
	}
	first, err := repository.MetricsSnapshot(t.Context(), MetricsSnapshotFilter{
		FromMS: 0, UntilMS: MetricsSnapshotWindowMS,
	})
	if err != nil {
		t.Fatalf("MetricsSnapshot() error = %v", err)
	}
	if first.Sources.Total != 0 {
		t.Fatalf("first snapshot sources = %#v, want pre-commit snapshot", first.Sources)
	}
	repository.metricsSnapshotReadHook = nil
	second, err := repository.MetricsSnapshot(t.Context(), MetricsSnapshotFilter{
		FromMS: 0, UntilMS: MetricsSnapshotWindowMS,
	})
	if err != nil || second.Sources.Total != 1 {
		t.Fatalf("second snapshot = %#v, %v", second, err)
	}
}

// 测试 snapshot 接受且完整返回 24h detailed cadence 的 17,280 个样本，并拒绝静默截断。
func TestMetricsSnapshotReturnsFullDetailedWindowAndRejectsOverflow(t *testing.T) {
	t.Parallel()

	repository := openRuntimeRepository(t)
	models := make([]appRuntimeSampleModel, MaxAppRuntimeSampleQueryLimit)
	for index := range models {
		models[index] = appRuntimeSampleModelFromDomain(validAppRuntimeSample(int64(index) * MetricsDetailedSampleIntervalMS))
	}
	if err := repository.database.WriteMaintenance(t.Context(), func(ctx context.Context, transaction storesqlite.WriteTx) error {
		return transaction.WithContext(ctx).CreateInBatches(models, 256).Error
	}); err != nil {
		t.Fatalf("seed runtime samples error = %v", err)
	}
	snapshot, err := repository.MetricsSnapshot(t.Context(), MetricsSnapshotFilter{
		FromMS: 0, UntilMS: MetricsSnapshotWindowMS,
	})
	if err != nil || len(snapshot.RuntimeSamples) != MaxAppRuntimeSampleQueryLimit ||
		snapshot.RuntimeSamples[0].CapturedAtMS != MetricsSnapshotWindowMS-MetricsDetailedSampleIntervalMS ||
		snapshot.RuntimeSamples[len(snapshot.RuntimeSamples)-1].CapturedAtMS != 0 {
		t.Fatalf("MetricsSnapshot(capacity) len = %d, error = %v", len(snapshot.RuntimeSamples), err)
	}
	extra := appRuntimeSampleModelFromDomain(validAppRuntimeSample(MetricsSnapshotWindowMS - 1))
	if err := repository.database.WriteMaintenance(t.Context(), func(ctx context.Context, transaction storesqlite.WriteTx) error {
		return transaction.WithContext(ctx).Create(&extra).Error
	}); err != nil {
		t.Fatalf("seed overflow sample error = %v", err)
	}
	if _, err := repository.MetricsSnapshot(t.Context(), MetricsSnapshotFilter{
		FromMS: 0, UntilMS: MetricsSnapshotWindowMS,
	}); !errors.Is(err, ErrInvalidRecord) {
		t.Fatalf("MetricsSnapshot(overflow) error = %v, want ErrInvalidRecord", err)
	}
}

func TestMetricsSnapshotRequiresExactTwentyFourHourWindow(t *testing.T) {
	t.Parallel()

	repository := openRuntimeRepository(t)
	for _, filter := range []MetricsSnapshotFilter{
		{FromMS: 0, UntilMS: MetricsSnapshotWindowMS - 1},
		{FromMS: 1, UntilMS: MetricsSnapshotWindowMS},
	} {
		if _, err := repository.MetricsSnapshot(t.Context(), filter); !errors.Is(err, ErrInvalidRecord) {
			t.Fatalf("MetricsSnapshot(%#v) error = %v, want ErrInvalidRecord", filter, err)
		}
	}
}

// 测试 interrupted job 只有在尚未被 resume child 消费时才属于当前可恢复工作。
func TestMetricsSnapshotCountsOnlyRecoverableInterruptedJobs(t *testing.T) {
	t.Parallel()

	repository := openRuntimeRepository(t)
	job := JobRun{
		JobID: "metrics-interrupted", JobType: "scan", RequestedBy: "test", Priority: 1,
		State: JobQueued, Phase: JobPhaseLive, CreatedAtMS: 100, UpdatedAtMS: 100,
	}
	if err := repository.CreateJobRun(t.Context(), job); err != nil {
		t.Fatalf("CreateJobRun() error = %v", err)
	}
	if err := repository.TransitionJobRun(t.Context(), JobTransition{
		JobID: job.JobID, ExpectedState: JobQueued, State: JobRunning, Phase: job.Phase, AtMS: 101,
	}); err != nil {
		t.Fatalf("TransitionJobRun(running) error = %v", err)
	}
	if err := repository.TransitionJobRun(t.Context(), JobTransition{
		JobID: job.JobID, ExpectedState: JobRunning, State: JobInterrupted, Phase: job.Phase, AtMS: 102,
	}); err != nil {
		t.Fatalf("TransitionJobRun(interrupted) error = %v", err)
	}
	filter := MetricsSnapshotFilter{FromMS: 0, UntilMS: MetricsSnapshotWindowMS}
	before, err := repository.MetricsSnapshot(t.Context(), filter)
	if err != nil || before.Jobs.Interrupted != 1 {
		t.Fatalf("MetricsSnapshot(before resume) jobs = %#v, error = %v", before.Jobs, err)
	}
	resumed := JobRun{
		JobID: "metrics-resumed", JobType: job.JobType, RequestedBy: "recovery", Priority: job.Priority,
		State: JobQueued, Phase: job.Phase, ResumeOfJobID: &job.JobID, CreatedAtMS: 103, UpdatedAtMS: 103,
	}
	if err := repository.ResumeInterruptedJob(t.Context(), job.JobID, resumed); err != nil {
		t.Fatalf("ResumeInterruptedJob() error = %v", err)
	}
	after, err := repository.MetricsSnapshot(t.Context(), filter)
	if err != nil || after.Jobs.Interrupted != 0 || after.Jobs.Queued != 1 {
		t.Fatalf("MetricsSnapshot(after resume) jobs = %#v, error = %v", after.Jobs, err)
	}
	if err := repository.TransitionJobRun(t.Context(), JobTransition{
		JobID: resumed.JobID, ExpectedState: JobQueued, State: JobRunning, Phase: resumed.Phase, AtMS: 104,
	}); err != nil {
		t.Fatalf("TransitionJobRun(resumed running) error = %v", err)
	}
	if err := repository.TransitionJobRun(t.Context(), JobTransition{
		JobID: resumed.JobID, ExpectedState: JobRunning, State: JobSucceeded, Phase: resumed.Phase, AtMS: 105,
	}); err != nil {
		t.Fatalf("TransitionJobRun(resumed succeeded) error = %v", err)
	}
	report, err := repository.CleanupRetention(t.Context(), RetentionCleanupOptions{
		Now: time.UnixMilli(3 * MetricsSnapshotWindowMS), BatchSize: 10,
	})
	if err != nil || report.Deleted.JobRuns != 1 {
		t.Fatalf("CleanupRetention() report = %#v, error = %v", report, err)
	}
	afterCleanup, err := repository.MetricsSnapshot(t.Context(), filter)
	if err != nil || afterCleanup.Jobs.Interrupted != 0 {
		t.Fatalf("MetricsSnapshot(after child retention) jobs = %#v, error = %v", afterCleanup.Jobs, err)
	}
	secondResume := resumed
	secondResume.JobID = "metrics-second-resume"
	secondResume.CreatedAtMS = 106
	secondResume.UpdatedAtMS = 106
	if err := repository.ResumeInterruptedJob(t.Context(), job.JobID, secondResume); !errors.Is(err, ErrInvalidRecord) {
		t.Fatalf("ResumeInterruptedJob(second consumer) error = %v, want ErrInvalidRecord", err)
	}
}
