package index

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	factstore "github.com/SisyphusSQ/codex-pulse/internal/store"
	storesqlite "github.com/SisyphusSQ/codex-pulse/internal/store/sqlite"
)

var errBackupInjected = errors.New("backup injected failure")
var errCrashInjected = errors.New("simulated process interruption")

func TestClassifyRepairErrorTreatsUnsupportedUpstreamValidRowsAsInvalid(t *testing.T) {
	t.Parallel()

	for _, err := range []error{ErrIndexLineTooLarge, ErrUnsupportedIndexEntry} {
		if got := classifyRepairError(err); got != factstore.RuntimeErrorInvalid {
			t.Fatalf("classifyRepairError(%v) = %s, want %s", err, got, factstore.RuntimeErrorInvalid)
		}
	}
}

func TestServiceAnalyzeAndUnconfirmedExecuteHaveZeroSideEffects(t *testing.T) {
	t.Parallel()

	fixture := newRepairFixture(t)
	beforeIndex := append([]byte(nil), readFile(t, fixture.indexPath)...)
	backupRoot := filepath.Join(filepath.Dir(fixture.database.Config().Path), "backups")

	plan, err := fixture.service.Analyze(context.Background())
	if err != nil {
		t.Fatalf("Analyze() error = %v", err)
	}
	if len(plan.Actions) != 2 {
		t.Fatalf("Analyze() actions = %#v, want 2", plan.Actions)
	}
	assertNoRepairJobs(t, fixture.repository)
	if got := readFile(t, fixture.indexPath); string(got) != string(beforeIndex) {
		t.Fatal("Analyze() modified session index")
	}
	if _, err := os.Lstat(backupRoot); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("Analyze() created backup root: %v", err)
	}

	_, err = fixture.service.Execute(context.Background(), plan, Confirmation{})
	if !errors.Is(err, ErrConfirmationRequired) {
		t.Fatalf("Execute(unconfirmed) error = %v, want ErrConfirmationRequired", err)
	}
	assertNoRepairJobs(t, fixture.repository)
	if got := readFile(t, fixture.indexPath); string(got) != string(beforeIndex) {
		t.Fatal("unconfirmed Execute() modified session index")
	}
	if _, err := os.Lstat(backupRoot); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("unconfirmed Execute() created backup root: %v", err)
	}
}

func TestSessionIndexRepairLiveE2EConfirmedRepairBacksUpAppendsReconcilesAndReplays(t *testing.T) {
	t.Parallel()

	fixture := newRepairFixture(t)
	beforeIndex := append([]byte(nil), readFile(t, fixture.indexPath)...)
	sessionBefore := fileState(t, fixture.sessionPath)
	plan, err := fixture.service.Analyze(context.Background())
	if err != nil {
		t.Fatalf("Analyze() error = %v", err)
	}

	report, err := fixture.service.Execute(context.Background(), plan, Confirmation{PlanID: plan.ID})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if report.JobID == "" || report.PlanID != plan.ID || report.Actions != 2 || report.Replayed ||
		report.DatabaseBackupPath == "" || report.IndexBackupPath == "" || !report.FinalVersion.Exists {
		t.Fatalf("Execute() report = %#v", report)
	}
	if got := readFile(t, report.IndexBackupPath); string(got) != string(beforeIndex) {
		t.Fatalf("index backup differs from pre-repair bytes")
	}
	assertDatabaseBackupContainsRepairJob(t, report.DatabaseBackupPath, report.JobID)
	parsed, err := Parse(readFile(t, fixture.indexPath))
	if err != nil {
		t.Fatalf("Parse(repaired) error = %v", err)
	}
	for _, action := range plan.Actions {
		latest, found := parsed.Latest(action.SessionID)
		if !found || latest.ThreadName != action.ThreadName {
			t.Fatalf("latest[%s] = %#v, %v", action.SessionID, latest, found)
		}
	}
	job, err := fixture.repository.JobRun(context.Background(), report.JobID)
	if err != nil {
		t.Fatalf("JobRun() error = %v", err)
	}
	if job.State != factstore.JobSucceeded || job.ProgressCurrent == nil || *job.ProgressCurrent != repairProgressTotal ||
		job.ProgressTotal == nil || *job.ProgressTotal != repairProgressTotal {
		t.Fatalf("repair job = %#v", job)
	}
	if got := fileState(t, fixture.sessionPath); got != sessionBefore {
		t.Fatalf("raw session changed: before=%#v after=%#v", sessionBefore, got)
	}

	afterFirst := append([]byte(nil), readFile(t, fixture.indexPath)...)
	replayed, err := fixture.service.Execute(context.Background(), plan, Confirmation{PlanID: plan.ID})
	if err != nil {
		t.Fatalf("Execute(replay) error = %v", err)
	}
	if !replayed.Replayed || replayed.JobID != report.JobID {
		t.Fatalf("Execute(replay) report = %#v", replayed)
	}
	if got := readFile(t, fixture.indexPath); string(got) != string(afterFirst) {
		t.Fatal("replay appended duplicate corrections")
	}
}

func TestServiceStopsBeforeAuditAndBackupWhenPlanDrifts(t *testing.T) {
	t.Parallel()

	fixture := newRepairFixture(t)
	plan, err := fixture.service.Analyze(context.Background())
	if err != nil {
		t.Fatalf("Analyze() error = %v", err)
	}
	file, err := os.OpenFile(fixture.indexPath, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatalf("OpenFile(drift): %v", err)
	}
	if _, err := file.WriteString(`{"id":"019d4b69-25d1-7be2-b5f4-8c4234ed5df2","thread_name":"concurrent","updated_at":"2026-04-04T00:00:00Z"}` + "\n"); err != nil {
		t.Fatalf("WriteString(drift): %v", err)
	}
	if err := file.Close(); err != nil {
		t.Fatalf("Close(drift): %v", err)
	}

	_, err = fixture.service.Execute(context.Background(), plan, Confirmation{PlanID: plan.ID})
	if !errors.Is(err, ErrPlanDrift) {
		t.Fatalf("Execute(drift) error = %v, want ErrPlanDrift", err)
	}
	assertNoRepairJobs(t, fixture.repository)
	backupRoot := filepath.Join(filepath.Dir(fixture.database.Config().Path), "backups")
	if _, err := os.Lstat(backupRoot); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("drift created backup root: %v", err)
	}
}

func TestServiceStopsBeforeAuditAndBackupWhenExpectationsDrift(t *testing.T) {
	t.Parallel()

	fixture := newRepairFixture(t)
	plan, err := fixture.service.Analyze(context.Background())
	if err != nil {
		t.Fatalf("Analyze() error = %v", err)
	}
	before := append([]byte(nil), readFile(t, fixture.indexPath)...)
	seedExpectation(
		t,
		fixture.repository,
		"019d4b69-25d1-7be2-b5f4-8c4234ed5dee",
		"store-changed-after-confirmation",
		1775347200000,
	)

	_, err = fixture.service.Execute(context.Background(), plan, Confirmation{PlanID: plan.ID})
	if !errors.Is(err, ErrExpectationDrift) {
		t.Fatalf("Execute(expectation drift) error = %v, want ErrExpectationDrift", err)
	}
	assertNoRepairJobs(t, fixture.repository)
	if got := readFile(t, fixture.indexPath); string(got) != string(before) {
		t.Fatal("expectation drift modified session index")
	}
	backupRoot := filepath.Join(filepath.Dir(fixture.database.Config().Path), "backups")
	if _, err := os.Lstat(backupRoot); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expectation drift created backup root: %v", err)
	}
}

func TestServiceAnalyzeFailsClosedForUpstreamValidUnsupportedLatestEntry(t *testing.T) {
	tests := []struct {
		name       string
		threadName string
		wantErr    error
	}{
		{name: "blank thread name", threadName: " ", wantErr: ErrUnsupportedIndexEntry},
		{name: "thread name above local output limit", threadName: strings.Repeat("n", maxThreadNameBytes+1), wantErr: ErrUnsupportedIndexEntry},
		{name: "schema valid line above parser limit", threadName: strings.Repeat("n", maxIndexLineBytes), wantErr: ErrIndexLineTooLarge},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			fixture := newRepairFixture(t)
			content := append([]byte(nil), readFile(t, fixture.indexPath)...)
			content = append(content, encodeEntryForTest(t, Entry{
				ID: "019d4b69-25d1-7be2-b5f4-8c4234ed5dee", ThreadName: test.threadName,
				UpdatedAt: "2026-04-04T00:00:00Z",
			})...)
			content = append(content, '\n')
			writePrivateFile(t, fixture.indexPath, content)

			plan, err := fixture.service.Analyze(context.Background())
			if !errors.Is(err, test.wantErr) {
				t.Fatalf("Analyze(upstream-valid unsupported latest) = %#v, %v, want %v", plan, err, test.wantErr)
			}
			assertNoRepairJobs(t, fixture.repository)
			if got := readFile(t, fixture.indexPath); !bytes.Equal(got, content) {
				t.Fatal("failed Analyze modified session index")
			}
			backupRoot := filepath.Join(filepath.Dir(fixture.database.Config().Path), "backups")
			if _, statErr := os.Lstat(backupRoot); !errors.Is(statErr, os.ErrNotExist) {
				t.Fatalf("failed Analyze created backup root: %v", statErr)
			}
		})
	}
}

func TestServiceExpectationDriftDuringBackupsFailsBeforeAppend(t *testing.T) {
	t.Parallel()

	fixture := newRepairFixture(t)
	plan, err := fixture.service.Analyze(context.Background())
	if err != nil {
		t.Fatalf("Analyze() error = %v", err)
	}
	before := append([]byte(nil), readFile(t, fixture.indexPath)...)
	indexFile, err := OpenIndexFile(fixture.home)
	if err != nil {
		t.Fatalf("OpenIndexFile() error = %v", err)
	}
	service := newService(
		fixture.repository,
		fixture.database,
		indexFile,
		newMonotonicClock(1775260803000),
		serviceHooks{beforeAppend: func() {
			seedExpectation(
				t, fixture.repository,
				"019d4b69-25d1-7be2-b5f4-8c4234ed5dee",
				"changed-during-backup", 1775347200000,
			)
		}},
	)
	report, err := service.Execute(context.Background(), plan, Confirmation{PlanID: plan.ID})
	if !errors.Is(err, ErrExpectationDrift) {
		t.Fatalf("Execute(drift during backup) error = %v, want ErrExpectationDrift", err)
	}
	if got := readFile(t, fixture.indexPath); string(got) != string(before) {
		t.Fatal("drift during backup modified session index")
	}
	if report.DatabaseBackupPath == "" || report.IndexBackupPath == "" {
		t.Fatalf("backup report = %#v, want both completed backups", report)
	}
	assertFailedRepairJob(t, fixture.repository, report.JobID, factstore.RuntimeErrorInvalid)
}

func TestServiceExpectationDriftAfterAppendCannotSucceedOrReplay(t *testing.T) {
	t.Parallel()

	fixture := newRepairFixture(t)
	plan, err := fixture.service.Analyze(context.Background())
	if err != nil {
		t.Fatalf("Analyze() error = %v", err)
	}
	indexFile, err := OpenIndexFile(fixture.home)
	if err != nil {
		t.Fatalf("OpenIndexFile() error = %v", err)
	}
	service := newService(
		fixture.repository,
		fixture.database,
		indexFile,
		newMonotonicClock(1775260804000),
		serviceHooks{afterAppend: func() error {
			seedExpectation(
				t, fixture.repository,
				"019d4b69-25d1-7be2-b5f4-8c4234ed5dee",
				"changed-after-append", 1775347200000,
			)
			return nil
		}},
	)
	report, err := service.Execute(context.Background(), plan, Confirmation{PlanID: plan.ID})
	if !errors.Is(err, ErrExpectationDrift) {
		t.Fatalf("Execute(drift after append) error = %v, want ErrExpectationDrift", err)
	}
	assertFailedRepairJob(t, fixture.repository, report.JobID, factstore.RuntimeErrorInvalid)

	_, err = fixture.service.Execute(context.Background(), plan, Confirmation{PlanID: plan.ID})
	if !errors.Is(err, ErrRepairAlreadyRecorded) {
		t.Fatalf("Execute(failed replay) error = %v, want ErrRepairAlreadyRecorded", err)
	}
}

func TestServiceSucceededReplayRejectsCurrentExpectationDrift(t *testing.T) {
	t.Parallel()

	fixture := newRepairFixture(t)
	plan, err := fixture.service.Analyze(context.Background())
	if err != nil {
		t.Fatalf("Analyze() error = %v", err)
	}
	if _, err := fixture.service.Execute(
		context.Background(), plan, Confirmation{PlanID: plan.ID},
	); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	seedExpectation(
		t, fixture.repository,
		"019d4b69-25d1-7be2-b5f4-8c4234ed5dee",
		"changed-after-success", 1775347200000,
	)
	_, err = fixture.service.Execute(context.Background(), plan, Confirmation{PlanID: plan.ID})
	if !errors.Is(err, ErrExpectationDrift) {
		t.Fatalf("Execute(replay drift) error = %v, want ErrExpectationDrift", err)
	}
}

func TestServiceExpectationDriftBeforeTerminalTransitionCannotSucceed(t *testing.T) {
	t.Parallel()

	fixture := newRepairFixture(t)
	plan, err := fixture.service.Analyze(context.Background())
	if err != nil {
		t.Fatalf("Analyze() error = %v", err)
	}
	indexFile, err := OpenIndexFile(fixture.home)
	if err != nil {
		t.Fatalf("OpenIndexFile() error = %v", err)
	}
	repository := &driftingTerminalRepository{
		Repository: fixture.repository,
		beforeSuccess: func() {
			seedExpectation(
				t, fixture.repository,
				"019d4b69-25d1-7be2-b5f4-8c4234ed5dee",
				"changed-before-terminal-transition", 1775347200000,
			)
		},
	}
	service := newService(
		repository,
		fixture.database,
		indexFile,
		newMonotonicClock(1775260805000),
		serviceHooks{},
	)
	report, err := service.Execute(context.Background(), plan, Confirmation{PlanID: plan.ID})
	if !errors.Is(err, ErrExpectationDrift) {
		t.Fatalf("Execute(terminal expectation drift) error = %v, want ErrExpectationDrift", err)
	}
	assertFailedRepairJob(t, fixture.repository, report.JobID, factstore.RuntimeErrorInvalid)
}

func TestServiceBackupDirectoryRequiresParentSync(t *testing.T) {
	t.Parallel()

	fixture := newRepairFixture(t)
	plan, err := fixture.service.Analyze(context.Background())
	if err != nil {
		t.Fatalf("Analyze() error = %v", err)
	}
	before := append([]byte(nil), readFile(t, fixture.indexPath)...)
	fixture.service.syncDirectory = func(string) error { return errDirectorySyncInjected }
	report, err := fixture.service.Execute(context.Background(), plan, Confirmation{PlanID: plan.ID})
	if !errors.Is(err, errDirectorySyncInjected) {
		t.Fatalf("Execute(directory sync) error = %v, want injected failure", err)
	}
	if got := readFile(t, fixture.indexPath); string(got) != string(before) {
		t.Fatal("directory sync failure modified session index")
	}
	assertFailedRepairJob(t, fixture.repository, report.JobID, factstore.RuntimeErrorUnknown)
}

func TestServiceConcurrentIndexAppendAfterFinalCheckCannotSucceed(t *testing.T) {
	t.Parallel()

	fixture := newRepairFixture(t)
	plan, err := fixture.service.Analyze(context.Background())
	if err != nil {
		t.Fatalf("Analyze() error = %v", err)
	}
	concurrent := []byte(
		`{"id":"019d4b69-25d1-7be2-b5f4-8c4234ed5dee","thread_name":"codex-concurrent","updated_at":"2026-04-04T00:00:00Z"}` + "\n",
	)
	fixture.service.indexFile.afterFinalCheck = func() {
		file, openErr := os.OpenFile(fixture.indexPath, os.O_APPEND|os.O_WRONLY, 0)
		if openErr != nil {
			t.Fatalf("OpenFile(concurrent): %v", openErr)
		}
		if _, writeErr := file.Write(concurrent); writeErr != nil {
			t.Fatalf("Write(concurrent): %v", writeErr)
		}
		if closeErr := file.Close(); closeErr != nil {
			t.Fatalf("Close(concurrent): %v", closeErr)
		}
	}
	report, err := fixture.service.Execute(context.Background(), plan, Confirmation{PlanID: plan.ID})
	if !errors.Is(err, ErrPlanDrift) {
		t.Fatalf("Execute(concurrent append) error = %v, want ErrPlanDrift", err)
	}
	if got := readFile(t, fixture.indexPath); !bytes.Contains(got, concurrent) {
		t.Fatal("concurrent Codex append was not preserved")
	}
	assertFailedRepairJob(t, fixture.repository, report.JobID, factstore.RuntimeErrorInvalid)
}

func TestServiceConflictHasZeroSideEffects(t *testing.T) {
	t.Parallel()

	fixture := newRepairFixture(t)
	indexNewer := []byte(
		`{"id":"019d4b69-25d1-7be2-b5f4-8c4234ed5dee","thread_name":"index-newer","updated_at":"2026-04-04T00:00:00Z"}` + "\n",
	)
	writePrivateFile(t, fixture.indexPath, indexNewer)
	plan, err := fixture.service.Analyze(context.Background())
	if err != nil {
		t.Fatalf("Analyze() error = %v", err)
	}
	if len(plan.Conflicts) != 1 {
		t.Fatalf("Analyze() conflicts = %#v, want 1", plan.Conflicts)
	}

	_, err = fixture.service.Execute(context.Background(), plan, Confirmation{PlanID: plan.ID})
	if !errors.Is(err, ErrPlanConflict) {
		t.Fatalf("Execute(conflict) error = %v, want ErrPlanConflict", err)
	}
	assertNoRepairJobs(t, fixture.repository)
	if got := readFile(t, fixture.indexPath); string(got) != string(indexNewer) {
		t.Fatal("conflicted Execute() modified session index")
	}
	backupRoot := filepath.Join(filepath.Dir(fixture.database.Config().Path), "backups")
	if _, err := os.Lstat(backupRoot); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("conflicted Execute() created backup root: %v", err)
	}
}

func TestServiceBackupFailureLeavesIndexUntouchedAndAuditsFailure(t *testing.T) {
	t.Parallel()

	fixture := newRepairFixture(t)
	plan, err := fixture.service.Analyze(context.Background())
	if err != nil {
		t.Fatalf("Analyze() error = %v", err)
	}
	before := append([]byte(nil), readFile(t, fixture.indexPath)...)
	indexFile, err := OpenIndexFile(fixture.home)
	if err != nil {
		t.Fatalf("OpenIndexFile() error = %v", err)
	}
	service := newService(
		fixture.repository,
		failingBackuper{config: fixture.database.Config(), err: errBackupInjected},
		indexFile,
		newMonotonicClock(1775260801000),
		serviceHooks{},
	)

	report, err := service.Execute(context.Background(), plan, Confirmation{PlanID: plan.ID})
	if !errors.Is(err, errBackupInjected) {
		t.Fatalf("Execute(backup failure) error = %v", err)
	}
	if got := readFile(t, fixture.indexPath); string(got) != string(before) {
		t.Fatal("backup failure modified session index")
	}
	job, jobErr := fixture.repository.JobRun(context.Background(), report.JobID)
	if jobErr != nil {
		t.Fatalf("JobRun() error = %v", jobErr)
	}
	if job.State != factstore.JobFailed || job.ErrorClass == nil || *job.ErrorClass != factstore.RuntimeErrorUnknown {
		t.Fatalf("failed repair job = %#v", job)
	}
	if report.IndexBackupPath != "" {
		t.Fatalf("index backup unexpectedly published: %s", report.IndexBackupPath)
	}
}

func TestServiceInterruptedAfterAppendRecoversByRedryRunWithoutDuplicateWrite(t *testing.T) {
	t.Parallel()

	fixture := newRepairFixture(t)
	plan, err := fixture.service.Analyze(context.Background())
	if err != nil {
		t.Fatalf("Analyze() error = %v", err)
	}
	indexFile, err := OpenIndexFile(fixture.home)
	if err != nil {
		t.Fatalf("OpenIndexFile() error = %v", err)
	}
	service := newService(
		fixture.repository,
		fixture.database,
		indexFile,
		newMonotonicClock(1775260802000),
		serviceHooks{afterAppend: func() error { return errCrashInjected }},
	)
	report, err := service.Execute(context.Background(), plan, Confirmation{PlanID: plan.ID})
	if !errors.Is(err, errCrashInjected) {
		t.Fatalf("Execute(interrupted) error = %v", err)
	}
	job, err := fixture.repository.JobRun(context.Background(), report.JobID)
	if err != nil {
		t.Fatalf("JobRun() error = %v", err)
	}
	if job.State != factstore.JobRunning || job.ProgressCurrent == nil || *job.ProgressCurrent != repairProgressAppended {
		t.Fatalf("interrupted repair job = %#v", job)
	}
	if count, err := fixture.repository.InterruptIncompleteJobs(context.Background(), job.UpdatedAtMS+1); err != nil || count != 1 {
		t.Fatalf("InterruptIncompleteJobs() = %d, %v", count, err)
	}
	afterAppend := append([]byte(nil), readFile(t, fixture.indexPath)...)

	recoveryPlan, err := fixture.service.Analyze(context.Background())
	if err != nil {
		t.Fatalf("Analyze(recovery) error = %v", err)
	}
	if len(recoveryPlan.Actions) != 0 {
		t.Fatalf("recovery actions = %#v, want no duplicate correction", recoveryPlan.Actions)
	}
	noOp, err := fixture.service.Execute(context.Background(), recoveryPlan, Confirmation{PlanID: recoveryPlan.ID})
	if err != nil {
		t.Fatalf("Execute(recovery no-op) error = %v", err)
	}
	if !noOp.Noop || noOp.JobID != "" {
		t.Fatalf("recovery no-op report = %#v", noOp)
	}
	if got := readFile(t, fixture.indexPath); string(got) != string(afterAppend) {
		t.Fatal("recovery no-op changed index")
	}
}

type repairFixture struct {
	service     *Service
	repository  *factstore.Repository
	database    *storesqlite.Store
	home        string
	indexPath   string
	sessionPath string
}

type driftingTerminalRepository struct {
	*factstore.Repository
	beforeSuccess func()
}

func (repository *driftingTerminalRepository) TransitionJobRun(
	ctx context.Context,
	transition factstore.JobTransition,
) error {
	return repository.Repository.TransitionJobRun(ctx, transition)
}

func (repository *driftingTerminalRepository) CompleteSessionIndexRepairJob(
	ctx context.Context,
	expectations []factstore.SessionIndexExpectation,
	transition factstore.JobTransition,
) error {
	repository.beforeSuccess()
	return repository.Repository.CompleteSessionIndexRepairJob(ctx, expectations, transition)
}

func newRepairFixture(t *testing.T) repairFixture {
	t.Helper()
	ctx := context.Background()
	databaseDirectory := newPrivateDirectory(t, "application-data")
	database, err := storesqlite.Open(ctx, storesqlite.Config{Path: filepath.Join(databaseDirectory, "codex-pulse.db")})
	if err != nil {
		t.Fatalf("sqlite.Open() error = %v", err)
	}
	t.Cleanup(func() {
		if err := database.Close(context.Background()); err != nil {
			t.Errorf("Store.Close() error = %v", err)
		}
	})
	repository := factstore.NewRepository(database)
	if err := repository.EnsureApplicationSchema(ctx); err != nil {
		t.Fatalf("EnsureApplicationSchema() error = %v", err)
	}
	seedExpectation(t, repository, "019d4b69-25d1-7be2-b5f4-8c4234ed5dee", "store-newer", 1775174400000)
	seedExpectation(t, repository, "019d4b69-25d1-7be2-b5f4-8c4234ed5def", "store-missing", 1775260800000)

	home := newPrivateDirectory(t, "codex-home")
	indexPath := filepath.Join(home, sessionIndexFilename)
	writePrivateFile(t, indexPath, []byte(
		`{"id":"019d4b69-25d1-7be2-b5f4-8c4234ed5dee","thread_name":"index-old","updated_at":"2026-04-01T00:00:00Z"}`+"\n",
	))
	sessionDirectory := filepath.Join(home, "sessions", "2026", "07", "14")
	if err := os.MkdirAll(sessionDirectory, 0o700); err != nil {
		t.Fatalf("MkdirAll(session directory): %v", err)
	}
	sessionPath := filepath.Join(sessionDirectory, "rollout-private.jsonl")
	writePrivateFile(t, sessionPath, []byte("raw-session-must-not-change\n"))

	service, err := NewService(repository, database, home)
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	service.now = newMonotonicClock(1775260800000)
	return repairFixture{
		service: service, repository: repository, database: database, home: home,
		indexPath: indexPath, sessionPath: sessionPath,
	}
}

func seedExpectation(t *testing.T, repository *factstore.Repository, sessionID, name string, atMS int64) {
	t.Helper()
	if err := repository.UpsertFacts(context.Background(), factstore.FactBatch{
		Session: &factstore.Session{
			SessionID: sessionID, Provider: "codex", SourceKind: "session",
			CreatedAtMS: 1, FirstSeenAtMS: 1, LastSeenAtMS: atMS,
		},
		SessionCurrent: &factstore.SessionCurrent{
			SessionID: sessionID, ThreadName: &name, ThreadNameUpdatedAtMS: &atMS, UpdatedAtMS: atMS,
		},
	}); err != nil {
		t.Fatalf("UpsertFacts(%s) error = %v", sessionID, err)
	}
}

func assertNoRepairJobs(t *testing.T, repository *factstore.Repository) {
	t.Helper()
	jobs, err := repository.ListJobRuns(context.Background(), factstore.JobRunFilter{Limit: 100})
	if err != nil {
		t.Fatalf("ListJobRuns() error = %v", err)
	}
	if len(jobs) != 0 {
		t.Fatalf("repair jobs = %#v, want none", jobs)
	}
}

func assertFailedRepairJob(
	t *testing.T,
	repository *factstore.Repository,
	jobID string,
	wantClass factstore.RuntimeErrorClass,
) {
	t.Helper()
	job, err := repository.JobRun(context.Background(), jobID)
	if err != nil {
		t.Fatalf("JobRun() error = %v", err)
	}
	if job.State != factstore.JobFailed || job.ErrorClass == nil || *job.ErrorClass != wantClass {
		t.Fatalf("failed repair job = %#v, want class %s", job, wantClass)
	}
}

func assertDatabaseBackupContainsRepairJob(t *testing.T, path, jobID string) {
	t.Helper()
	database, err := storesqlite.Open(context.Background(), storesqlite.Config{Path: path})
	if err != nil {
		t.Fatalf("sqlite.Open(backup) error = %v", err)
	}
	defer func() {
		if err := database.Close(context.Background()); err != nil {
			t.Errorf("backup Store.Close() error = %v", err)
		}
	}()
	repository := factstore.NewRepository(database)
	job, err := repository.JobRun(context.Background(), jobID)
	if err != nil {
		t.Fatalf("JobRun(backup) error = %v", err)
	}
	if job.JobID != jobID {
		t.Fatalf("backup repair job = %#v", job)
	}
}

type failingBackuper struct {
	config storesqlite.Config
	err    error
}

func (backuper failingBackuper) Config() storesqlite.Config { return backuper.config }

func (backuper failingBackuper) Backup(
	context.Context,
	storesqlite.BackupOptions,
) (storesqlite.BackupReport, error) {
	return storesqlite.BackupReport{}, backuper.err
}

func newMonotonicClock(startMS int64) func() time.Time {
	var lock sync.Mutex
	current := startMS
	return func() time.Time {
		lock.Lock()
		defer lock.Unlock()
		current++
		return time.UnixMilli(current).UTC()
	}
}
