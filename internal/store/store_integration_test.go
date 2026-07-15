package store

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	storesqlite "github.com/SisyphusSQ/codex-pulse/internal/store/sqlite"
)

const abnormalExitDatabaseEnv = "CODEX_PULSE_ABNORMAL_EXIT_DATABASE"

func TestStoreIntegrationReopensAfterAbnormalExit(t *testing.T) {
	if path := os.Getenv(abnormalExitDatabaseEnv); path != "" {
		if err := seedStoreBeforeAbnormalExit(path); err != nil {
			_, _ = fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
		// Deliberately bypass Store.Close to simulate process termination.
		os.Exit(0)
	}
	t.Parallel()

	directory := t.TempDir()
	if err := os.Chmod(directory, 0o700); err != nil {
		t.Fatalf("secure temp directory: %v", err)
	}
	path := filepath.Join(directory, "abnormal-exit.db")
	command := exec.Command(os.Args[0], "-test.run=^TestStoreIntegrationReopensAfterAbnormalExit$")
	command.Env = append(os.Environ(), abnormalExitDatabaseEnv+"="+path)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("abnormal-exit helper error = %v, output = %s", err, output)
	}

	database, err := storesqlite.Open(context.Background(), storesqlite.Config{Path: path})
	if err != nil {
		t.Fatalf("sqlite.Open(after abnormal exit) error = %v", err)
	}
	t.Cleanup(func() { _ = database.Close(context.Background()) })
	repository := NewRepository(database)
	if err := repository.EnsureApplicationSchema(context.Background()); err != nil {
		t.Fatalf("EnsureApplicationSchema(after abnormal exit) error = %v", err)
	}
	pragmas := database.Pragmas()
	if pragmas.Writer.JournalMode != "wal" || !pragmas.Writer.ForeignKeys || !pragmas.Reader.QueryOnly {
		t.Fatalf("reopened pragmas = %#v, want WAL, foreign keys, and read-only reader", pragmas)
	}
	state, err := repository.SourceState(context.Background(), "abnormal-source")
	if err != nil || state.SourceInstanceID != "abnormal-source" {
		t.Fatalf("SourceState(after abnormal exit) = %#v, %v", state, err)
	}
	attempts, err := repository.ListSourceAttempts(context.Background(), state.SourceInstanceID, 10)
	if err != nil || len(attempts) != 1 || attempts[0].RequestID != "abnormal-attempt" {
		t.Fatalf("ListSourceAttempts(after abnormal exit) = %#v, %v", attempts, err)
	}
	job, err := repository.JobRun(context.Background(), "abnormal-job")
	if err != nil || job.State != JobRunning {
		t.Fatalf("JobRun(after abnormal exit) = %#v, %v, want running", job, err)
	}
	now := time.UnixMilli(200_000_000).UTC()
	interrupted, err := repository.InterruptIncompleteJobs(context.Background(), now.UnixMilli())
	if err != nil || interrupted != 1 {
		t.Fatalf("InterruptIncompleteJobs() = %d, %v, want 1, nil", interrupted, err)
	}
	cleanup, err := repository.CleanupRetention(context.Background(), RetentionCleanupOptions{Now: now})
	if err != nil {
		t.Fatalf("CleanupRetention(after abnormal exit) error = %v", err)
	}
	if cleanup.Deleted != (RetentionDeletedCounts{SourceAttempts: 1}) {
		t.Fatalf("CleanupRetention(after abnormal exit) = %#v, want one source attempt", cleanup)
	}
	job, err = repository.JobRun(context.Background(), job.JobID)
	if err != nil || job.State != JobInterrupted {
		t.Fatalf("JobRun(after recovery) = %#v, %v, want interrupted", job, err)
	}
}

func seedStoreBeforeAbnormalExit(path string) error {
	ctx := context.Background()
	database, err := storesqlite.Open(ctx, storesqlite.Config{Path: path})
	if err != nil {
		return fmt.Errorf("open abnormal-exit store: %w", err)
	}
	repository := NewRepository(database)
	if err := repository.EnsureApplicationSchema(ctx); err != nil {
		return fmt.Errorf("migrate abnormal-exit store: %w", err)
	}
	now := time.UnixMilli(200_000_000).UTC()
	cutoffMS := now.Add(-RetentionWindow).UnixMilli()
	state := SourceState{
		SourceInstanceID: "abnormal-source", SourceType: "quota", ScopeKey: "default",
		FreshnessState: SourceFreshnessCurrent, CursorVersion: 1, UpdatedAtMS: now.UnixMilli(),
	}
	if err := repository.UpsertSourceState(ctx, state); err != nil {
		return fmt.Errorf("write abnormal-exit source state: %w", err)
	}
	if err := repository.AppendSourceAttempt(ctx, SourceAttempt{
		RequestID: "abnormal-attempt", SourceInstanceID: state.SourceInstanceID,
		StartedAtMS: cutoffMS - 2, FinishedAtMS: cutoffMS - 1, Outcome: SourceAttemptSucceeded,
		AttemptCount: 1,
	}); err != nil {
		return fmt.Errorf("write abnormal-exit source attempt: %w", err)
	}
	job := JobRun{
		JobID: "abnormal-job", JobType: "scan", RequestedBy: "startup", Priority: 1,
		State: JobQueued, Phase: JobPhaseDiscover, CreatedAtMS: 100, UpdatedAtMS: 100,
	}
	if err := repository.CreateJobRun(ctx, job); err != nil {
		return fmt.Errorf("write abnormal-exit job: %w", err)
	}
	if err := repository.TransitionJobRun(ctx, JobTransition{
		JobID: job.JobID, ExpectedState: JobQueued, State: JobRunning,
		Phase: JobPhaseHistoryBackfill, AtMS: 101,
	}); err != nil {
		return fmt.Errorf("start abnormal-exit job: %w", err)
	}
	return nil
}

func TestStoreIntegrationFreshReplayCleanupInterruptedAndReopen(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()
	if err := os.Chmod(directory, 0o700); err != nil {
		t.Fatalf("secure temp directory: %v", err)
	}
	path := filepath.Join(directory, "store-integration.db")
	open := func() *storesqlite.Store {
		database, err := storesqlite.Open(context.Background(), storesqlite.Config{Path: path})
		if err != nil {
			t.Fatalf("sqlite.Open() error = %v", err)
		}
		return database
	}

	now := time.UnixMilli(200_000_000).UTC()
	cutoff := now.Add(-RetentionWindow).UnixMilli()
	database := open()
	repository := NewRepository(database)
	if err := repository.EnsureApplicationSchema(context.Background()); err != nil {
		t.Fatalf("EnsureApplicationSchema(fresh) error = %v", err)
	}
	state := SourceState{
		SourceInstanceID: "integration-source", SourceType: "quota", ScopeKey: "default",
		FreshnessState: SourceFreshnessCurrent, CursorVersion: 1, UpdatedAtMS: now.UnixMilli(),
	}
	if err := repository.UpsertSourceState(context.Background(), state); err != nil {
		t.Fatalf("UpsertSourceState() error = %v", err)
	}
	attempt := SourceAttempt{
		RequestID: "integration-attempt", SourceInstanceID: state.SourceInstanceID,
		StartedAtMS: cutoff - 2, FinishedAtMS: cutoff - 1, Outcome: SourceAttemptSucceeded,
		AttemptCount: 1,
	}
	for range 2 {
		if err := repository.AppendSourceAttempt(context.Background(), attempt); err != nil {
			t.Fatalf("AppendSourceAttempt(replay) error = %v", err)
		}
	}
	job := JobRun{
		JobID: "integration-job", JobType: "scan", RequestedBy: "startup", Priority: 1,
		State: JobQueued, Phase: JobPhaseDiscover, CreatedAtMS: 100, UpdatedAtMS: 100,
	}
	if err := repository.CreateJobRun(context.Background(), job); err != nil {
		t.Fatalf("CreateJobRun() error = %v", err)
	}
	if err := repository.TransitionJobRun(context.Background(), JobTransition{
		JobID: job.JobID, ExpectedState: JobQueued, State: JobRunning,
		Phase: JobPhaseHistoryBackfill, AtMS: 101,
	}); err != nil {
		t.Fatalf("TransitionJobRun(running) error = %v", err)
	}
	if err := database.Close(context.Background()); err != nil {
		t.Fatalf("Close(first lifecycle) error = %v", err)
	}

	database = open()
	repository = NewRepository(database)
	if err := repository.EnsureApplicationSchema(context.Background()); err != nil {
		t.Fatalf("EnsureApplicationSchema(reopen) error = %v", err)
	}
	pragmas := database.Pragmas()
	if pragmas.Writer.JournalMode != "wal" || !pragmas.Writer.ForeignKeys || !pragmas.Reader.QueryOnly {
		t.Fatalf("reopened pragmas = %#v, want WAL, foreign keys, and read-only reader", pragmas)
	}
	interrupted, err := repository.InterruptIncompleteJobs(context.Background(), now.UnixMilli())
	if err != nil || interrupted != 1 {
		t.Fatalf("InterruptIncompleteJobs() = %d, %v, want 1, nil", interrupted, err)
	}
	report, err := repository.CleanupRetention(context.Background(), RetentionCleanupOptions{Now: now, BatchSize: 1})
	if err != nil {
		t.Fatalf("CleanupRetention() error = %v", err)
	}
	if report.Deleted != (RetentionDeletedCounts{SourceAttempts: 1}) {
		t.Fatalf("CleanupRetention() report = %#v, want one source attempt", report)
	}
	if attempts, err := repository.ListSourceAttempts(context.Background(), state.SourceInstanceID, 10); err != nil || len(attempts) != 0 {
		t.Fatalf("ListSourceAttempts(after cleanup) = %#v, %v, want empty", attempts, err)
	}
	if persistedState, err := repository.SourceState(context.Background(), state.SourceInstanceID); err != nil || persistedState.SourceInstanceID != state.SourceInstanceID {
		t.Fatalf("SourceState(after cleanup) = %#v, %v, want long-lived state", persistedState, err)
	}
	interruptedJob, err := repository.JobRun(context.Background(), job.JobID)
	if err != nil || interruptedJob.State != JobInterrupted || interruptedJob.FinishedAtMS == nil || *interruptedJob.FinishedAtMS != now.UnixMilli() {
		t.Fatalf("JobRun(after interruption) = %#v, %v", interruptedJob, err)
	}
	if err := database.Close(context.Background()); err != nil {
		t.Fatalf("Close(second lifecycle) error = %v", err)
	}

	database = open()
	t.Cleanup(func() { _ = database.Close(context.Background()) })
	repository = NewRepository(database)
	if err := repository.EnsureApplicationSchema(context.Background()); err != nil {
		t.Fatalf("EnsureApplicationSchema(second reopen) error = %v", err)
	}
	jobAfterSecondReopen, err := repository.JobRun(context.Background(), job.JobID)
	if err != nil || jobAfterSecondReopen.State != JobInterrupted {
		t.Fatalf("JobRun(second reopen) = %#v, %v, want interrupted", jobAfterSecondReopen, err)
	}
}
