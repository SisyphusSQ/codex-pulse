package store

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	storesqlite "github.com/SisyphusSQ/codex-pulse/internal/store/sqlite"
	"gorm.io/gorm"
)

func TestRetentionCandidateQueriesUseDedicatedIndexes(t *testing.T) {
	t.Parallel()

	repository := openRuntimeRepository(t)
	cutoffMS := int64(100)
	testCases := []struct {
		name    string
		query   func(*gorm.DB) *gorm.DB
		indexes []string
	}{
		{
			name: "health events",
			query: func(database *gorm.DB) *gorm.DB {
				var ids []string
				return expiredHealthEvents(database, cutoffMS).
					Order("resolved_at_ms, event_id").Limit(100).Pluck("event_id", &ids)
			},
			indexes: []string{"idx_health_events_retention"},
		},
		{
			name: "job runs and references",
			query: func(database *gorm.DB) *gorm.DB {
				var ids []string
				return expiredJobRunsForState(database, cutoffMS, JobSucceeded).
					Order("finished_at_ms, job_id").Limit(100).Pluck("job_id", &ids)
			},
			indexes: []string{
				"idx_job_runs_retention", "idx_health_events_job", "idx_job_runs_resume_lineage",
			},
		},
		{
			name: "source attempts",
			query: func(database *gorm.DB) *gorm.DB {
				var ids []string
				return expiredSourceAttempts(database, cutoffMS).
					Order("finished_at_ms, request_id").Limit(100).Pluck("request_id", &ids)
			},
			indexes: []string{"idx_source_attempts_retention"},
		},
	}

	err := repository.database.View(context.Background(), func(ctx context.Context, connection storesqlite.ReadConn) error {
		for _, testCase := range testCases {
			dryRun := testCase.query(connection.Session(&gorm.Session{DryRun: true, Context: ctx}))
			if dryRun.Error != nil {
				return dryRun.Error
			}
			rows, err := rawQueryRows(
				ctx, connection, "EXPLAIN QUERY PLAN "+dryRun.Statement.SQL.String(), dryRun.Statement.Vars...,
			)
			if err != nil {
				return err
			}
			var details []string
			for rows.Next() {
				var detail string
				if err := rows.Scan(new(int), new(int), new(int), &detail); err != nil {
					rows.Close()
					return err
				}
				details = append(details, detail)
			}
			if err := rows.Close(); err != nil {
				return err
			}
			plan := strings.Join(details, "; ")
			for _, index := range testCase.indexes {
				if !strings.Contains(plan, index) {
					t.Errorf("%s query plan = %q, want %s", testCase.name, plan, index)
				}
			}
			if strings.Contains(plan, "USE TEMP B-TREE") {
				t.Errorf("%s query plan = %q, want no temporary ordering", testCase.name, plan)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("inspect retention query plans: %v", err)
	}
}

func TestCleanupRetentionAppliesFixedWindowAndReferenceRules(t *testing.T) {
	t.Parallel()

	repository := openRuntimeRepository(t)
	now := time.UnixMilli(200_000_000).UTC()
	cutoff := now.Add(-RetentionWindow).UnixMilli()

	seedRetentionModels(t, repository, retentionFixture{
		projects: []projectModel{{
			ProjectID: "long-lived", DisplayName: "Long lived", RootPath: "/synthetic",
			CreatedAtMS: 1, UpdatedAtMS: 1,
		}},
		sourceStates: []sourceStateModel{{
			SourceInstanceID: "source-a", SourceType: "quota", ScopeKey: "default",
			ConsecutiveFailures: 0, FreshnessState: string(SourceFreshnessCurrent), CursorVersion: 1,
			UpdatedAtMS: cutoff + 1,
		}},
		sourceAttempts: []sourceAttemptModel{
			{RequestID: "attempt-old", SourceInstanceID: "source-a", StartedAtMS: cutoff - 2, FinishedAtMS: cutoff - 1, Outcome: string(SourceAttemptSucceeded)},
			{RequestID: "attempt-boundary", SourceInstanceID: "source-a", StartedAtMS: cutoff - 1, FinishedAtMS: cutoff, Outcome: string(SourceAttemptSucceeded)},
			{RequestID: "attempt-recent", SourceInstanceID: "source-a", StartedAtMS: cutoff, FinishedAtMS: cutoff + 1, Outcome: string(SourceAttemptSucceeded)},
		},
		jobs: []jobRunModel{
			retentionTerminalJob("job-old", cutoff-10, cutoff-1),
			retentionTerminalJob("job-boundary", cutoff-10, cutoff),
			retentionTerminalJob("job-recent", cutoff-10, cutoff+1),
			{JobID: "job-queued", JobType: "scan", RequestedBy: "test", Priority: 1, State: string(JobQueued), Phase: string(JobPhaseDiscover), CreatedAtMS: 1, UpdatedAtMS: cutoff - 1},
			{JobID: "job-running", JobType: "scan", RequestedBy: "test", Priority: 1, State: string(JobRunning), Phase: string(JobPhaseLive), CreatedAtMS: 1, StartedAtMS: pointerTo(int64(2)), UpdatedAtMS: cutoff - 1},
			{JobID: "job-interrupted", JobType: "scan", RequestedBy: "test", Priority: 1, State: string(JobInterrupted), Phase: string(JobPhaseHistoryBackfill), CreatedAtMS: 1, StartedAtMS: pointerTo(int64(2)), FinishedAtMS: pointerTo(cutoff - 1), UpdatedAtMS: cutoff - 1},
			retentionTerminalJob("job-active-health", cutoff-10, cutoff-1),
			retentionTerminalJob("job-recent-health", cutoff-10, cutoff-1),
			retentionTerminalJob("job-old-health", cutoff-10, cutoff-1),
			retentionTerminalJob("job-resume-parent", cutoff-10, cutoff-1),
			{JobID: "job-resume-child", JobType: "scan", RequestedBy: "test", Priority: 1, State: string(JobQueued), Phase: string(JobPhaseDiscover), ResumeOfJobID: pointerTo("job-resume-parent"), CreatedAtMS: cutoff, UpdatedAtMS: cutoff},
		},
		health: []healthEventModel{
			retentionHealthEvent("health-old", nil, cutoff-2, pointerTo(cutoff-1)),
			retentionHealthEvent("health-boundary", nil, cutoff-1, pointerTo(cutoff)),
			retentionHealthEvent("health-active", pointerTo("job-active-health"), cutoff-1, nil),
			retentionHealthEvent("health-recent-job", pointerTo("job-recent-health"), cutoff-1, pointerTo(cutoff)),
			retentionHealthEvent("health-old-job", pointerTo("job-old-health"), cutoff-2, pointerTo(cutoff-1)),
		},
	})

	var progress []RetentionCleanupProgress
	report, err := repository.CleanupRetentionBatch(context.Background(), RetentionCleanupOptions{
		Now: now, BatchSize: 100,
		Observe: func(update RetentionCleanupProgress) {
			progress = append(progress, update)
		},
	})
	if err != nil {
		t.Fatalf("CleanupRetentionBatch() error = %v", err)
	}
	wantDeleted := RetentionDeletedCounts{HealthEvents: 2, JobRuns: 2, SourceAttempts: 1}
	if report.CutoffMS != cutoff || report.Deleted != wantDeleted || report.More {
		t.Fatalf("CleanupRetentionBatch() = %#v, want cutoff=%d deleted=%#v more=false", report, cutoff, wantDeleted)
	}
	if len(progress) != 1 || progress[0].Batch != 1 || progress[0].Deleted != wantDeleted || progress[0].Total != wantDeleted || progress[0].More {
		t.Fatalf("CleanupRetentionBatch() progress = %#v, want one committed update", progress)
	}

	if got, want := retentionIDs(t, repository, &healthEventModel{}, "event_id"), []string{"health-active", "health-boundary", "health-recent-job"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("health IDs = %v, want %v", got, want)
	}
	if got, want := retentionIDs(t, repository, &jobRunModel{}, "job_id"), []string{
		"job-active-health", "job-boundary", "job-interrupted", "job-queued", "job-recent", "job-recent-health", "job-resume-child", "job-resume-parent", "job-running",
	}; !reflect.DeepEqual(got, want) {
		t.Fatalf("job IDs = %v, want %v", got, want)
	}
	if got, want := retentionIDs(t, repository, &sourceAttemptModel{}, "request_id"), []string{"attempt-boundary", "attempt-recent"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("source attempt IDs = %v, want %v", got, want)
	}
	if got := retentionIDs(t, repository, &projectModel{}, "project_id"); !reflect.DeepEqual(got, []string{"long-lived"}) {
		t.Fatalf("long-lived project IDs = %v, want untouched", got)
	}
}

func TestCleanupRetentionCancelsBetweenCommittedBatchesAndRetriesWithoutCursor(t *testing.T) {
	t.Parallel()

	repository := openRuntimeRepository(t)
	now := time.UnixMilli(200_000_000).UTC()
	cutoff := now.Add(-RetentionWindow).UnixMilli()
	attempts := make([]sourceAttemptModel, 0, 5)
	for index := range 5 {
		attempts = append(attempts, sourceAttemptModel{
			RequestID: "attempt-" + string(rune('a'+index)), SourceInstanceID: "source-a",
			StartedAtMS: cutoff - 10 + int64(index), FinishedAtMS: cutoff - 5 + int64(index),
			Outcome: string(SourceAttemptSucceeded),
		})
	}
	seedRetentionModels(t, repository, retentionFixture{
		sourceStates: []sourceStateModel{{
			SourceInstanceID: "source-a", SourceType: "quota", ScopeKey: "default",
			FreshnessState: string(SourceFreshnessCurrent), CursorVersion: 1, UpdatedAtMS: cutoff,
		}},
		sourceAttempts: attempts,
	})

	ctx, cancel := context.WithCancel(context.Background())
	progress := make([]RetentionCleanupProgress, 0, 1)
	report, err := repository.CleanupRetention(ctx, RetentionCleanupOptions{
		Now: now, BatchSize: 2,
		Observe: func(update RetentionCleanupProgress) {
			progress = append(progress, update)
			cancel()
		},
	})
	if !errors.Is(err, storesqlite.ErrCanceled) || !errors.Is(err, context.Canceled) {
		t.Fatalf("CleanupRetention() error = %v, want sqlite ErrCanceled and context.Canceled", err)
	}
	if report.Batches != 1 || report.Deleted.SourceAttempts != 2 || len(progress) != 1 || !progress[0].More {
		t.Fatalf("canceled report/progress = %#v / %#v, want one full committed batch", report, progress)
	}
	if got := retentionIDs(t, repository, &sourceAttemptModel{}, "request_id"); len(got) != 3 {
		t.Fatalf("remaining attempts after cancel = %v, want three", got)
	}

	retry, err := repository.CleanupRetention(context.Background(), RetentionCleanupOptions{Now: now, BatchSize: 2})
	if err != nil {
		t.Fatalf("CleanupRetention(retry) error = %v", err)
	}
	if retry.Batches != 2 || retry.Deleted.SourceAttempts != 3 {
		t.Fatalf("retry report = %#v, want two batches deleting remaining three", retry)
	}
	if got := retentionIDs(t, repository, &sourceAttemptModel{}, "request_id"); len(got) != 0 {
		t.Fatalf("remaining attempts after retry = %v, want none", got)
	}
}

func TestCleanupRetentionRecomputesEligibilityAcrossTerminalResumeLineage(t *testing.T) {
	t.Parallel()

	repository := openRuntimeRepository(t)
	now := time.UnixMilli(200_000_000).UTC()
	cutoff := now.Add(-RetentionWindow).UnixMilli()
	parent := retentionTerminalJob("job-parent", cutoff-10, cutoff-3)
	child := retentionTerminalJob("job-child", cutoff-9, cutoff-2)
	child.ResumeOfJobID = pointerTo(parent.JobID)
	grandchild := retentionTerminalJob("job-grandchild", cutoff-8, cutoff-1)
	grandchild.ResumeOfJobID = pointerTo(child.JobID)
	seedRetentionModels(t, repository, retentionFixture{jobs: []jobRunModel{parent, child, grandchild}})

	report, err := repository.CleanupRetention(context.Background(), RetentionCleanupOptions{
		Now: now, BatchSize: 10,
	})
	if err != nil {
		t.Fatalf("CleanupRetention() error = %v", err)
	}
	if report.Batches != 3 || report.Deleted != (RetentionDeletedCounts{JobRuns: 3}) {
		t.Fatalf("CleanupRetention() report = %#v, want three lineage batches", report)
	}
	if got := retentionIDs(t, repository, &jobRunModel{}, "job_id"); len(got) != 0 {
		t.Fatalf("remaining lineage jobs = %v, want none", got)
	}
}

func TestCleanupRetentionRollsBackFailedBatchAndCanRetry(t *testing.T) {
	t.Parallel()

	repository := openRuntimeRepository(t)
	now := time.UnixMilli(200_000_000).UTC()
	cutoff := now.Add(-RetentionWindow).UnixMilli()
	seedRetentionModels(t, repository, retentionFixture{
		sourceStates: []sourceStateModel{{
			SourceInstanceID: "source-a", SourceType: "quota", ScopeKey: "default",
			FreshnessState: string(SourceFreshnessCurrent), CursorVersion: 1, UpdatedAtMS: cutoff,
		}},
		sourceAttempts: []sourceAttemptModel{{
			RequestID: "attempt-old", SourceInstanceID: "source-a", StartedAtMS: cutoff - 2,
			FinishedAtMS: cutoff - 1, Outcome: string(SourceAttemptSucceeded),
		}},
		jobs: []jobRunModel{retentionTerminalJob("job-old", cutoff-10, cutoff-1)},
		health: []healthEventModel{
			retentionHealthEvent("health-old-job", pointerTo("job-old"), cutoff-2, pointerTo(cutoff-1)),
		},
	})

	err := repository.database.Write(context.Background(), func(ctx context.Context, transaction storesqlite.WriteTx) error {
		return transaction.WithContext(ctx).Exec(`
			CREATE TRIGGER fail_retention_job_delete
			BEFORE DELETE ON job_runs
			WHEN OLD.job_id = 'job-old'
			BEGIN SELECT RAISE(ABORT, 'injected retention failure'); END
		`).Error
	})
	if err != nil {
		t.Fatalf("create failure trigger: %v", err)
	}

	report, err := repository.CleanupRetentionBatch(context.Background(), RetentionCleanupOptions{Now: now, BatchSize: 100})
	if err == nil {
		t.Fatal("CleanupRetentionBatch() error = nil, want injected failure")
	}
	if report.Deleted.Total() != 0 {
		t.Fatalf("failed report = %#v, want no committed deletions", report)
	}
	if got := retentionIDs(t, repository, &healthEventModel{}, "event_id"); !reflect.DeepEqual(got, []string{"health-old-job"}) {
		t.Fatalf("health IDs after rollback = %v", got)
	}
	if got := retentionIDs(t, repository, &jobRunModel{}, "job_id"); !reflect.DeepEqual(got, []string{"job-old"}) {
		t.Fatalf("job IDs after rollback = %v", got)
	}
	if got := retentionIDs(t, repository, &sourceAttemptModel{}, "request_id"); !reflect.DeepEqual(got, []string{"attempt-old"}) {
		t.Fatalf("attempt IDs after rollback = %v", got)
	}

	err = repository.database.Write(context.Background(), func(ctx context.Context, transaction storesqlite.WriteTx) error {
		return transaction.WithContext(ctx).Exec("DROP TRIGGER fail_retention_job_delete").Error
	})
	if err != nil {
		t.Fatalf("drop failure trigger: %v", err)
	}
	retry, err := repository.CleanupRetentionBatch(context.Background(), RetentionCleanupOptions{Now: now, BatchSize: 100})
	if err != nil {
		t.Fatalf("CleanupRetentionBatch(retry) error = %v", err)
	}
	if retry.Deleted != (RetentionDeletedCounts{HealthEvents: 1, JobRuns: 1, SourceAttempts: 1}) {
		t.Fatalf("retry report = %#v", retry)
	}
}

func TestCleanupRetentionRejectsInvalidOptions(t *testing.T) {
	t.Parallel()

	repository := openRuntimeRepository(t)
	for _, batchSize := range []int{-1, MaxRetentionBatchSize + 1} {
		if _, err := repository.CleanupRetentionBatch(context.Background(), RetentionCleanupOptions{BatchSize: batchSize}); !errors.Is(err, ErrInvalidRetentionOptions) {
			t.Fatalf("batch size %d error = %v, want ErrInvalidRetentionOptions", batchSize, err)
		}
	}
	var nilRepository *Repository
	if _, err := nilRepository.CleanupRetentionBatch(context.Background(), RetentionCleanupOptions{}); !errors.Is(err, ErrInvalidRepository) {
		t.Fatalf("nil repository error = %v, want ErrInvalidRepository", err)
	}
}

type retentionFixture struct {
	projects       []projectModel
	sourceStates   []sourceStateModel
	sourceAttempts []sourceAttemptModel
	jobs           []jobRunModel
	health         []healthEventModel
}

func seedRetentionModels(t *testing.T, repository *Repository, fixture retentionFixture) {
	t.Helper()
	err := repository.database.Write(context.Background(), func(ctx context.Context, transaction storesqlite.WriteTx) error {
		for _, models := range []any{fixture.projects, fixture.sourceStates, fixture.sourceAttempts, fixture.jobs, fixture.health} {
			switch rows := models.(type) {
			case []projectModel:
				if len(rows) > 0 {
					if err := transaction.WithContext(ctx).Create(&rows).Error; err != nil {
						return err
					}
				}
			case []sourceStateModel:
				if len(rows) > 0 {
					if err := transaction.WithContext(ctx).Create(&rows).Error; err != nil {
						return err
					}
				}
			case []sourceAttemptModel:
				if len(rows) > 0 {
					if err := transaction.WithContext(ctx).Create(&rows).Error; err != nil {
						return err
					}
				}
			case []jobRunModel:
				if len(rows) > 0 {
					if err := transaction.WithContext(ctx).Create(&rows).Error; err != nil {
						return err
					}
				}
			case []healthEventModel:
				if len(rows) > 0 {
					if err := transaction.WithContext(ctx).Create(&rows).Error; err != nil {
						return err
					}
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("seed retention fixture: %v", err)
	}
}

func retentionTerminalJob(jobID string, createdAtMS, finishedAtMS int64) jobRunModel {
	startedAtMS := createdAtMS + 1
	return jobRunModel{
		JobID: jobID, JobType: "scan", RequestedBy: "test", Priority: 1,
		State: string(JobSucceeded), Phase: string(JobPhaseReconcile), CreatedAtMS: createdAtMS,
		StartedAtMS: &startedAtMS, FinishedAtMS: &finishedAtMS, UpdatedAtMS: finishedAtMS,
	}
}

func retentionHealthEvent(eventID string, jobID *string, lastSeenAtMS int64, resolvedAtMS *int64) healthEventModel {
	return healthEventModel{
		EventID: eventID, Fingerprint: SHA256DigestOf([]byte(eventID)).String(), Domain: string(HealthDomainJob),
		Severity: string(HealthError), Code: string(HealthCodeJobFailed), JobID: jobID,
		FirstSeenAtMS: lastSeenAtMS, LastSeenAtMS: lastSeenAtMS, ResolvedAtMS: resolvedAtMS,
		OccurrenceCount: 1, UpdatedAtMS: func() int64 {
			if resolvedAtMS != nil {
				return *resolvedAtMS
			}
			return lastSeenAtMS
		}(),
	}
}

func retentionIDs(t *testing.T, repository *Repository, model any, column string) []string {
	t.Helper()
	var identifiers []string
	err := repository.database.View(context.Background(), func(ctx context.Context, connection storesqlite.ReadConn) error {
		return connection.WithContext(ctx).Model(model).Order(column).Pluck(column, &identifiers).Error
	})
	if err != nil {
		t.Fatalf("read %s IDs: %v", column, err)
	}
	return identifiers
}
