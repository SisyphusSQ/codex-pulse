package store

import (
	"context"
	"errors"
	"testing"
)

func TestRepositoryListsSessionIndexExpectationsWithDeterministicGORMQuery(t *testing.T) {
	t.Parallel()

	database := openTestDatabase(t)
	repository := NewRepository(database)
	if err := repository.EnsureApplicationSchema(context.Background()); err != nil {
		t.Fatalf("EnsureApplicationSchema() error = %v", err)
	}
	nameA, nameC := "alpha", "charlie"
	atA, atC := int64(30), int64(20)
	for _, batch := range []FactBatch{
		{Session: &Session{SessionID: "session-c", Provider: "codex", SourceKind: "session", CreatedAtMS: 1, FirstSeenAtMS: 1, LastSeenAtMS: 20}, SessionCurrent: &SessionCurrent{SessionID: "session-c", ThreadName: &nameC, ThreadNameUpdatedAtMS: &atC, UpdatedAtMS: 20}},
		{Session: &Session{SessionID: "session-b", Provider: "codex", SourceKind: "session", CreatedAtMS: 1, FirstSeenAtMS: 1, LastSeenAtMS: 20}, SessionCurrent: &SessionCurrent{SessionID: "session-b", UpdatedAtMS: 20}},
		{Session: &Session{SessionID: "session-a", Provider: "codex", SourceKind: "session", CreatedAtMS: 1, FirstSeenAtMS: 1, LastSeenAtMS: 30}, SessionCurrent: &SessionCurrent{SessionID: "session-a", ThreadName: &nameA, ThreadNameUpdatedAtMS: &atA, UpdatedAtMS: 30}},
	} {
		if err := repository.UpsertFacts(context.Background(), batch); err != nil {
			t.Fatalf("UpsertFacts() error = %v", err)
		}
	}

	got, err := repository.ListSessionIndexExpectations(context.Background())
	if err != nil {
		t.Fatalf("ListSessionIndexExpectations() error = %v", err)
	}
	want := []SessionIndexExpectation{
		{SessionID: "session-a", ThreadName: "alpha", UpdatedAtMS: 30},
		{SessionID: "session-c", ThreadName: "charlie", UpdatedAtMS: 20},
	}
	if len(got) != len(want) {
		t.Fatalf("expectations = %#v, want %#v", got, want)
	}
	for index := range want {
		if got[index] != want[index] {
			t.Fatalf("expectations[%d] = %#v, want %#v", index, got[index], want[index])
		}
	}
}

func TestRepositoryCompletesSessionIndexRepairOnlyForExactExpectationSnapshot(t *testing.T) {
	t.Parallel()

	database := openTestDatabase(t)
	repository := NewRepository(database)
	ctx := context.Background()
	if err := repository.EnsureApplicationSchema(ctx); err != nil {
		t.Fatalf("EnsureApplicationSchema() error = %v", err)
	}
	name := "alpha"
	nameAtMS := int64(30)
	if err := repository.UpsertFacts(ctx, FactBatch{
		Session: &Session{
			SessionID: "session-a", Provider: "codex", SourceKind: "session",
			CreatedAtMS: 1, FirstSeenAtMS: 1, LastSeenAtMS: nameAtMS,
		},
		SessionCurrent: &SessionCurrent{
			SessionID: "session-a", ThreadName: &name,
			ThreadNameUpdatedAtMS: &nameAtMS, UpdatedAtMS: nameAtMS,
		},
	}); err != nil {
		t.Fatalf("UpsertFacts(initial expectation) error = %v", err)
	}
	progress, total := int64(0), int64(4)
	job := JobRun{
		JobID: "session-index-repair-job", JobType: "session_index_repair:test",
		RequestedBy: "explicit_confirmation", State: JobQueued, Phase: JobPhaseMaintenance,
		CreatedAtMS: 1, UpdatedAtMS: 1,
		ProgressCurrent: &progress, ProgressTotal: &total,
	}
	if err := repository.CreateJobRun(ctx, job); err != nil {
		t.Fatalf("CreateJobRun() error = %v", err)
	}
	if err := repository.TransitionJobRun(ctx, JobTransition{
		JobID: job.JobID, ExpectedState: JobQueued, State: JobRunning,
		Phase: JobPhaseMaintenance, ProgressCurrent: &progress, ProgressTotal: &total, AtMS: 2,
	}); err != nil {
		t.Fatalf("TransitionJobRun(running) error = %v", err)
	}

	confirmed, err := repository.ListSessionIndexExpectations(ctx)
	if err != nil {
		t.Fatalf("ListSessionIndexExpectations(confirmed) error = %v", err)
	}
	changedName := "beta"
	changedAtMS := int64(40)
	if err := repository.UpsertFacts(ctx, FactBatch{SessionCurrent: &SessionCurrent{
		SessionID: "session-a", ThreadName: &changedName,
		ThreadNameUpdatedAtMS: &changedAtMS, UpdatedAtMS: changedAtMS,
	}}); err != nil {
		t.Fatalf("UpsertFacts(changed expectation) error = %v", err)
	}
	progress = total
	complete := JobTransition{
		JobID: job.JobID, ExpectedState: JobRunning, State: JobSucceeded,
		Phase: JobPhaseMaintenance, ProgressCurrent: &progress, ProgressTotal: &total, AtMS: 50,
	}
	if err := repository.CompleteSessionIndexRepairJob(ctx, confirmed, complete); !errors.Is(
		err, ErrSessionIndexExpectationDrift,
	) {
		t.Fatalf("CompleteSessionIndexRepairJob(drift) error = %v, want ErrSessionIndexExpectationDrift", err)
	}
	running, err := repository.JobRun(ctx, job.JobID)
	if err != nil {
		t.Fatalf("JobRun(after drift) error = %v", err)
	}
	if running.State != JobRunning {
		t.Fatalf("job state after drift = %s, want %s", running.State, JobRunning)
	}

	current, err := repository.ListSessionIndexExpectations(ctx)
	if err != nil {
		t.Fatalf("ListSessionIndexExpectations(current) error = %v", err)
	}
	if err := repository.CompleteSessionIndexRepairJob(ctx, current, complete); err != nil {
		t.Fatalf("CompleteSessionIndexRepairJob(current) error = %v", err)
	}
	succeeded, err := repository.JobRun(ctx, job.JobID)
	if err != nil {
		t.Fatalf("JobRun(succeeded) error = %v", err)
	}
	if succeeded.State != JobSucceeded {
		t.Fatalf("job state = %s, want %s", succeeded.State, JobSucceeded)
	}
}
