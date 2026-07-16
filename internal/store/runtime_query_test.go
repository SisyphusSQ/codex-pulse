package store

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	storesqlite "github.com/SisyphusSQ/codex-pulse/internal/store/sqlite"
)

func TestRuntimeSourceQueryMergesKindsWithStableCursorAndExactSummary(t *testing.T) {
	t.Parallel()

	repository := openRuntimeRepository(t)
	ctx := context.Background()
	for _, file := range []SourceFile{
		{
			SourceFileID: "file-a", Provider: "codex", CurrentPath: "/private/a.jsonl",
			DeviceID: "device-a", Inode: 1, SizeBytes: 100, MTimeNS: 10,
			ParsedOffset: 50, ParserVersion: "v1", State: SourceFileActive,
			LastScannedAtMS: pointerTo(int64(30)), UpdatedAtMS: 30,
		},
		{
			SourceFileID: "file-b", Provider: "codex", CurrentPath: "/private/b.jsonl",
			DeviceID: "device-b", Inode: 2, SizeBytes: 80, MTimeNS: 20,
			ParsedOffset: 80, ParserVersion: "v1", State: SourceFileCompleted,
			LastScannedAtMS: pointerTo(int64(10)), UpdatedAtMS: 10,
		},
	} {
		if err := repository.UpsertSourceFile(ctx, file); err != nil {
			t.Fatalf("UpsertSourceFile(%s) error = %v", file.SourceFileID, err)
		}
	}
	for _, state := range []SourceState{
		{
			SourceInstanceID: "quota-a", SourceType: "wham_quota", ScopeKey: "private-account",
			FreshnessState: SourceFreshnessUnavailable, ConsecutiveFailures: 2,
			LastErrorClass: pointerTo(RuntimeErrorUnavailable), CursorVersion: 1, UpdatedAtMS: 20,
		},
		{
			SourceInstanceID: "quota-b", SourceType: "reset_credits", ScopeKey: "private-account",
			FreshnessState: SourceFreshnessCurrent, CursorVersion: 1, UpdatedAtMS: 10,
		},
	} {
		if err := repository.UpsertSourceState(ctx, state); err != nil {
			t.Fatalf("UpsertSourceState(%s) error = %v", state.SourceInstanceID, err)
		}
	}

	first, err := repository.RuntimeSourcePage(ctx, RuntimeSourceQuery{
		Limit: 2, Direction: RuntimeQueryDescending,
	})
	if err != nil {
		t.Fatalf("RuntimeSourcePage(first) error = %v", err)
	}
	if first.MatchedCount != 4 || first.Summary != (RuntimeSourceSummary{
		Total: 4, LocalFiles: 2, OnlineSources: 2, Attention: 1,
	}) || len(first.Records) != 2 || first.NextCursor == nil {
		t.Fatalf("RuntimeSourcePage(first) = %#v", first)
	}
	if got := []string{first.Records[0].SourceKey, first.Records[1].SourceKey}; !reflect.DeepEqual(got, []string{"local_file:file-a", "online:quota-a"}) {
		t.Fatalf("first keys = %#v", got)
	}
	replayed, err := repository.RuntimeSourcePage(ctx, RuntimeSourceQuery{
		Limit: 2, Direction: RuntimeQueryDescending,
	})
	if err != nil || !reflect.DeepEqual(replayed, first) {
		t.Fatalf("RuntimeSourcePage(replay) = %#v, %v, want %#v", replayed, err, first)
	}
	second, err := repository.RuntimeSourcePage(ctx, RuntimeSourceQuery{
		After: first.NextCursor, Limit: 2, Direction: RuntimeQueryDescending,
	})
	if err != nil {
		t.Fatalf("RuntimeSourcePage(second) error = %v", err)
	}
	if got := []string{second.Records[0].SourceKey, second.Records[1].SourceKey}; !reflect.DeepEqual(got, []string{"online:quota-b", "local_file:file-b"}) || second.NextCursor != nil {
		t.Fatalf("second page = %#v", second)
	}
	if _, err := repository.RuntimeSource(ctx, "local_file:file-a"); err != nil {
		t.Fatalf("RuntimeSource(local detail) error = %v", err)
	}
	if _, err := repository.RuntimeSource(ctx, "online:missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("RuntimeSource(missing) error = %v, want ErrNotFound", err)
	}
	ascending, err := repository.RuntimeSourcePage(ctx, RuntimeSourceQuery{
		Limit: 1, Direction: RuntimeQueryAscending,
	})
	if err != nil || len(ascending.Records) != 1 ||
		ascending.Records[0].SourceKey != "local_file:file-b" || ascending.NextCursor == nil {
		t.Fatalf("RuntimeSourcePage(ascending first) = %#v, %v", ascending, err)
	}
	ascendingNext, err := repository.RuntimeSourcePage(ctx, RuntimeSourceQuery{
		After: ascending.NextCursor, Limit: 1, Direction: RuntimeQueryAscending,
	})
	if err != nil || len(ascendingNext.Records) != 1 ||
		ascendingNext.Records[0].SourceKey != "online:quota-b" {
		t.Fatalf("RuntimeSourcePage(ascending second) = %#v, %v", ascendingNext, err)
	}
}

func TestRuntimeJobAndHealthQueriesFilterPageAndReopenFacts(t *testing.T) {
	t.Parallel()

	repository := openRuntimeRepository(t)
	ctx := context.Background()
	jobs := []JobRun{
		{
			JobID: "job-running", JobType: "backfill", RequestedBy: "startup", Priority: 1,
			State: JobQueued, Phase: JobPhaseDiscover, CreatedAtMS: 10, UpdatedAtMS: 10,
		},
		{
			JobID: "job-failed", JobType: "live_scan", RequestedBy: "scheduler", Priority: 2,
			State: JobQueued, Phase: JobPhaseDiscover, CreatedAtMS: 20, UpdatedAtMS: 20,
		},
	}
	for _, job := range jobs {
		if err := repository.CreateJobRun(ctx, job); err != nil {
			t.Fatalf("CreateJobRun(%s) error = %v", job.JobID, err)
		}
	}
	if err := repository.TransitionJobRun(ctx, JobTransition{
		JobID: "job-running", ExpectedState: JobQueued, State: JobRunning,
		Phase: JobPhaseHistoryBackfill, ProgressCurrent: pointerTo(int64(4)),
		ProgressTotal: pointerTo(int64(10)), AtMS: 30,
	}); err != nil {
		t.Fatalf("TransitionJobRun(running) error = %v", err)
	}
	if err := repository.TransitionJobRun(ctx, JobTransition{
		JobID: "job-failed", ExpectedState: JobQueued, State: JobRunning,
		Phase: JobPhaseLive, AtMS: 31,
	}); err != nil {
		t.Fatalf("TransitionJobRun(start failed) error = %v", err)
	}
	if err := repository.TransitionJobRun(ctx, JobTransition{
		JobID: "job-failed", ExpectedState: JobRunning, State: JobFailed,
		Phase: JobPhaseLive, ErrorClass: pointerTo(RuntimeErrorTimeout), AtMS: 40,
	}); err != nil {
		t.Fatalf("TransitionJobRun(failed) error = %v", err)
	}

	jobPage, err := repository.RuntimeJobPage(ctx, RuntimeJobQuery{
		States: []JobState{JobFailed}, Limit: 1, Direction: RuntimeQueryDescending,
	})
	if err != nil {
		t.Fatalf("RuntimeJobPage() error = %v", err)
	}
	if jobPage.MatchedCount != 1 || jobPage.Summary.Total != 1 ||
		jobPage.Summary.Failed != 1 || len(jobPage.Records) != 1 ||
		jobPage.Records[0].Job.JobID != "job-failed" {
		t.Fatalf("RuntimeJobPage() = %#v", jobPage)
	}
	if detail, err := repository.RuntimeJob(ctx, "job-running"); err != nil ||
		detail.Job.ProgressCurrent == nil || *detail.Job.ProgressCurrent != 4 {
		t.Fatalf("RuntimeJob() = %#v, %v", detail, err)
	}

	for _, observation := range []HealthObservation{
		{
			EventID: "health-active", Fingerprint: SHA256DigestOf([]byte("active")),
			Domain: HealthDomainJob, Severity: HealthError, Code: HealthCodeJobFailed,
			JobID: pointerTo("job-failed"), ErrorClass: pointerTo(RuntimeErrorTimeout), ObservedAtMS: 50,
		},
		{
			EventID: "health-resolved", Fingerprint: SHA256DigestOf([]byte("resolved")),
			Domain: HealthDomainRuntime, Severity: HealthWarning, Code: HealthCodeRuntimeUnknown,
			ObservedAtMS: 45,
		},
	} {
		if _, err := repository.ObserveHealthEvent(ctx, observation); err != nil {
			t.Fatalf("ObserveHealthEvent(%s) error = %v", observation.EventID, err)
		}
	}
	if err := repository.ResolveHealthEvent(ctx, "health-resolved", 46); err != nil {
		t.Fatalf("ResolveHealthEvent() error = %v", err)
	}
	active := true
	healthPage, err := repository.RuntimeHealthPage(ctx, RuntimeHealthQuery{
		Active: &active, Domains: []HealthDomain{HealthDomainJob}, Limit: 10,
		Direction: RuntimeQueryDescending,
	})
	if err != nil {
		t.Fatalf("RuntimeHealthPage() error = %v", err)
	}
	if healthPage.MatchedCount != 1 || healthPage.Summary.Active != 1 ||
		healthPage.Summary.Errors != 1 || len(healthPage.Records) != 1 ||
		healthPage.Records[0].EventID != "health-active" {
		t.Fatalf("RuntimeHealthPage() = %#v", healthPage)
	}
	if detail, err := repository.RuntimeHealth(ctx, "health-active"); err != nil ||
		detail.Fingerprint.String() == "" {
		t.Fatalf("RuntimeHealth() = %#v, %v", detail, err)
	}
}

func TestRuntimeQueriesRejectInvalidInputAndCanceledContext(t *testing.T) {
	t.Parallel()

	repository := openRuntimeRepository(t)
	if _, err := repository.RuntimeSourcePage(context.Background(), RuntimeSourceQuery{}); !errors.Is(err, ErrInvalidRecord) {
		t.Fatalf("RuntimeSourcePage(invalid) error = %v, want ErrInvalidRecord", err)
	}
	if _, err := repository.RuntimeJobPage(context.Background(), RuntimeJobQuery{
		States: []JobState{"private-state"}, Limit: 1, Direction: RuntimeQueryDescending,
	}); !errors.Is(err, ErrInvalidRecord) {
		t.Fatalf("RuntimeJobPage(invalid) error = %v, want ErrInvalidRecord", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := repository.RuntimeHealthPage(ctx, RuntimeHealthQuery{
		Limit: 1, Direction: RuntimeQueryDescending,
	}); !errors.Is(err, context.Canceled) {
		t.Fatalf("RuntimeHealthPage(cancelled) error = %v, want canceled", err)
	}
}

func TestRuntimeQueriesRemainStableAfterStoreReopen(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()
	if err := os.Chmod(directory, 0o700); err != nil {
		t.Fatalf("secure temp directory: %v", err)
	}
	path := filepath.Join(directory, "runtime-query-reopen.db")
	open := func() *storesqlite.Store {
		database, err := storesqlite.Open(context.Background(), storesqlite.Config{Path: path})
		if err != nil {
			t.Fatalf("sqlite.Open() error = %v", err)
		}
		return database
	}

	database := open()
	repository := NewRepository(database)
	if err := repository.EnsureApplicationSchema(context.Background()); err != nil {
		t.Fatalf("EnsureApplicationSchema() error = %v", err)
	}
	state := SourceState{
		SourceInstanceID: "reopen-source", SourceType: "quota", ScopeKey: "private",
		FreshnessState: SourceFreshnessCurrent, CursorVersion: 1, UpdatedAtMS: 100,
	}
	if err := repository.UpsertSourceState(context.Background(), state); err != nil {
		t.Fatalf("UpsertSourceState() error = %v", err)
	}
	before, err := repository.RuntimeSourcePage(context.Background(), RuntimeSourceQuery{
		Limit: 10, Direction: RuntimeQueryDescending,
	})
	if err != nil {
		t.Fatalf("RuntimeSourcePage(before reopen) error = %v", err)
	}
	if err := database.Close(context.Background()); err != nil {
		t.Fatalf("Close(before reopen) error = %v", err)
	}

	database = open()
	t.Cleanup(func() { _ = database.Close(context.Background()) })
	repository = NewRepository(database)
	if err := repository.EnsureApplicationSchema(context.Background()); err != nil {
		t.Fatalf("EnsureApplicationSchema(after reopen) error = %v", err)
	}
	after, err := repository.RuntimeSourcePage(context.Background(), RuntimeSourceQuery{
		Limit: 10, Direction: RuntimeQueryDescending,
	})
	if err != nil {
		t.Fatalf("RuntimeSourcePage(after reopen) error = %v", err)
	}
	if !reflect.DeepEqual(after, before) {
		t.Fatalf("after reopen = %#v, want %#v", after, before)
	}
}

func TestRuntimeSourceQueryPreservesSuccessfulKindAsPartial(t *testing.T) {
	t.Parallel()

	repository := openRuntimeRepository(t)
	ctx := context.Background()
	if err := repository.UpsertSourceState(ctx, SourceState{
		SourceInstanceID: "partial-online", SourceType: "quota", ScopeKey: "private",
		FreshnessState: SourceFreshnessCurrent, CursorVersion: 1, UpdatedAtMS: 10,
	}); err != nil {
		t.Fatalf("UpsertSourceState() error = %v", err)
	}
	synthetic := errors.New("synthetic local read failure")
	repository.runtimeQueryReadHook = func(stage string) error {
		if stage == "source_local_before" {
			return synthetic
		}
		return nil
	}
	page, err := repository.RuntimeSourcePage(ctx, RuntimeSourceQuery{
		Limit: 10, Direction: RuntimeQueryDescending,
	})
	if err != nil || len(page.Records) != 1 || page.Records[0].Kind != RuntimeSourceOnline ||
		!reflect.DeepEqual(page.UnavailableKinds, []RuntimeSourceKind{RuntimeSourceLocalFile}) {
		t.Fatalf("RuntimeSourcePage(single-side failure) = %#v, %v", page, err)
	}
	repository.runtimeQueryReadHook = func(stage string) error {
		if stage == "source_local_before" || stage == "source_online_before" {
			return synthetic
		}
		return nil
	}
	if _, err := repository.RuntimeSourcePage(ctx, RuntimeSourceQuery{
		Limit: 10, Direction: RuntimeQueryDescending,
	}); err == nil {
		t.Fatal("RuntimeSourcePage(all selected kinds failed) error = nil")
	}
}

func TestRuntimeQueriesUseOneReadSnapshotAcrossConcurrentWriterCommits(t *testing.T) {
	t.Parallel()

	t.Run("source counts and rows", func(t *testing.T) {
		repository := openRuntimeRepository(t)
		ctx := context.Background()
		first := runtimeSnapshotSourceFile("snapshot-source-a", 10)
		if err := repository.UpsertSourceFile(ctx, first); err != nil {
			t.Fatalf("UpsertSourceFile(first) error = %v", err)
		}
		called := false
		repository.runtimeQueryReadHook = func(stage string) error {
			if stage != "source_local_after_counts" || called {
				return nil
			}
			called = true
			return repository.UpsertSourceFile(ctx, runtimeSnapshotSourceFile("snapshot-source-b", 20))
		}
		page, err := repository.RuntimeSourcePage(ctx, RuntimeSourceQuery{
			Kinds: []RuntimeSourceKind{RuntimeSourceLocalFile},
			Limit: 10, Direction: RuntimeQueryDescending,
		})
		if err != nil || !called || page.MatchedCount != 1 || len(page.Records) != 1 ||
			page.Records[0].SourceKey != "local_file:snapshot-source-a" {
			t.Fatalf("RuntimeSourcePage(concurrent commit) = %#v, called=%v, err=%v", page, called, err)
		}
		repository.runtimeQueryReadHook = nil
		after, err := repository.RuntimeSourcePage(ctx, RuntimeSourceQuery{
			Kinds: []RuntimeSourceKind{RuntimeSourceLocalFile},
			Limit: 10, Direction: RuntimeQueryDescending,
		})
		if err != nil || after.MatchedCount != 2 || len(after.Records) != 2 {
			t.Fatalf("RuntimeSourcePage(after commit) = %#v, %v", after, err)
		}
	})

	t.Run("job counts rows and relation", func(t *testing.T) {
		repository := openRuntimeRepository(t)
		ctx := context.Background()
		job := schedulerTargetJob("snapshot-job-a", JobPhaseLive, 10)
		if err := repository.CreateJobRun(ctx, job); err != nil {
			t.Fatalf("CreateJobRun() error = %v", err)
		}
		called := false
		repository.runtimeQueryReadHook = func(stage string) error {
			if stage != "job_after_rows" || called {
				return nil
			}
			called = true
			return repository.EnqueueSchedulerTask(ctx, SchedulerTask{
				TaskID: "snapshot-task-a", DedupeKey: "snapshot:task:a",
				TargetKind: SchedulerTargetLiveScan, TargetID: job.JobID, HomeGeneration: 1,
				Lane: SchedulerLaneLive, ServiceClass: SchedulerServiceBackground,
				State: SchedulerTaskQueued, QueueOrderMS: 20, EnqueuedAtMS: 20, UpdatedAtMS: 20,
			}, 10)
		}
		page, err := repository.RuntimeJobPage(ctx, RuntimeJobQuery{
			Limit: 10, Direction: RuntimeQueryDescending,
		})
		if err != nil || !called || len(page.Records) != 1 || page.Records[0].Task != nil {
			t.Fatalf("RuntimeJobPage(concurrent relation) = %#v, called=%v, err=%v", page, called, err)
		}
		repository.runtimeQueryReadHook = nil
		detail, err := repository.RuntimeJob(ctx, job.JobID)
		if err != nil || detail.Task == nil || detail.Task.TaskID != "snapshot-task-a" {
			t.Fatalf("RuntimeJob(after commit) = %#v, %v", detail, err)
		}
	})

	t.Run("job detail relation", func(t *testing.T) {
		repository := openRuntimeRepository(t)
		ctx := context.Background()
		job := schedulerTargetJob("snapshot-detail-job", JobPhaseLive, 10)
		if err := repository.CreateJobRun(ctx, job); err != nil {
			t.Fatalf("CreateJobRun() error = %v", err)
		}
		called := false
		repository.runtimeQueryReadHook = func(stage string) error {
			if stage != "job_detail_after_job" || called {
				return nil
			}
			called = true
			return repository.EnqueueSchedulerTask(ctx, SchedulerTask{
				TaskID: "snapshot-detail-task", DedupeKey: "snapshot:detail:task",
				TargetKind: SchedulerTargetLiveScan, TargetID: job.JobID, HomeGeneration: 1,
				Lane: SchedulerLaneLive, ServiceClass: SchedulerServiceBackground,
				State: SchedulerTaskQueued, QueueOrderMS: 20, EnqueuedAtMS: 20, UpdatedAtMS: 20,
			}, 10)
		}
		detail, err := repository.RuntimeJob(ctx, job.JobID)
		if err != nil || !called || detail.Task != nil {
			t.Fatalf("RuntimeJob(concurrent relation) = %#v, called=%v, err=%v", detail, called, err)
		}
	})

	t.Run("health summary lifecycle and rows", func(t *testing.T) {
		repository := openRuntimeRepository(t)
		ctx := context.Background()
		if _, err := repository.ObserveHealthEvent(ctx, runtimeSnapshotHealth("snapshot-health-a", 10)); err != nil {
			t.Fatalf("ObserveHealthEvent(first) error = %v", err)
		}
		called := false
		repository.runtimeQueryReadHook = func(stage string) error {
			if stage != "health_after_counts" || called {
				return nil
			}
			called = true
			if _, err := repository.ObserveHealthEvent(ctx, runtimeSnapshotHealth("snapshot-health-b", 20)); err != nil {
				return err
			}
			_, err := repository.InitializeSchedulerLifecycle(ctx, SchedulerLifecycle{
				HomeGeneration: 1, UserPauseScope: LifecyclePauseNone,
				SystemState: LifecycleSystemAwake, Transition: LifecycleTransitionSteady,
				SourceState: LifecycleSourceAvailable, LastEventID: "snapshot:lifecycle",
				Revision: 1, UpdatedAtMS: 20,
			})
			return err
		}
		page, err := repository.RuntimeHealthPage(ctx, RuntimeHealthQuery{
			Limit: 10, Direction: RuntimeQueryDescending,
		})
		if err != nil || !called || page.MatchedCount != 1 || len(page.Records) != 1 ||
			page.Lifecycle != nil || page.Records[0].EventID != "snapshot-health-a" {
			t.Fatalf("RuntimeHealthPage(concurrent commit) = %#v, called=%v, err=%v", page, called, err)
		}
	})
}

func TestRuntimeHealthSummaryMaterializesBoundedAggregateRows(t *testing.T) {
	t.Parallel()

	repository := openRuntimeRepository(t)
	ctx := context.Background()
	for index := range 20 {
		eventID := "bounded-health-" + string(rune('a'+index))
		if _, err := repository.ObserveHealthEvent(ctx, runtimeSnapshotHealth(eventID, int64(10+index))); err != nil {
			t.Fatalf("ObserveHealthEvent(%s) error = %v", eventID, err)
		}
		if err := repository.ResolveHealthEvent(ctx, eventID, int64(100+index)); err != nil {
			t.Fatalf("ResolveHealthEvent(%s) error = %v", eventID, err)
		}
	}
	rowCount := -1
	repository.runtimeHealthAggregateRowsHook = func(value int) error {
		rowCount = value
		return nil
	}
	page, err := repository.RuntimeHealthPage(ctx, RuntimeHealthQuery{
		Limit: 1, Direction: RuntimeQueryDescending,
	})
	if err != nil || page.Summary.Total != 20 || page.Summary.Resolved != 20 ||
		rowCount < 1 || rowCount > 8 {
		t.Fatalf("RuntimeHealthPage(bounded summary) = %#v, rows=%d, err=%v", page, rowCount, err)
	}
}

func runtimeSnapshotSourceFile(id string, atMS int64) SourceFile {
	return SourceFile{
		SourceFileID: id, Provider: "codex", CurrentPath: "/synthetic/" + id,
		DeviceID: "device-" + id, Inode: atMS, SizeBytes: 10, MTimeNS: atMS,
		ParsedOffset: 0, ParserVersion: "v1", State: SourceFileActive, UpdatedAtMS: atMS,
	}
}

func runtimeSnapshotHealth(id string, atMS int64) HealthObservation {
	return HealthObservation{
		EventID: id, Fingerprint: SHA256DigestOf([]byte(id)), Domain: HealthDomainRuntime,
		Severity: HealthWarning, Code: HealthCodeRuntimeUnknown, ObservedAtMS: atMS,
	}
}
