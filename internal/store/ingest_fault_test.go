package store

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"gorm.io/gorm"
)

func TestCommitIngestBatchRollsBackEveryActiveAppendPhase(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		trigger string
		job     bool
	}{
		{name: "diagnostic", trigger: `BEFORE INSERT ON parser_diagnostics`},
		{name: "checkpoint", trigger: `BEFORE UPDATE ON parser_checkpoints`},
		{name: "fact", trigger: `BEFORE INSERT ON turns`},
		{name: "source cursor", trigger: `BEFORE UPDATE OF parsed_offset ON source_files`},
		{name: "job cursor", trigger: `BEFORE UPDATE ON job_runs`, job: true},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			repository := openRuntimeRepository(t)
			previous, cursor := activateFaultFixture(t, repository)
			current := testSourceFingerprint(
				previous.SourceFileID, previous.CurrentPath, previous.Inode, 120, 2_000,
			)
			opened, err := repository.PrepareGeneration(ctx, PrepareGenerationRequest{
				Mode: GenerationModeAppend, Previous: &previous, Current: current,
				ParserVersion: "codex-rollout-v1", AtMS: 30,
			})
			if err != nil {
				t.Fatalf("PrepareGeneration(append) error = %v", err)
			}
			_, turn, checkpoint := testCommittedSessionTurn("session-a", "turn-new", cursor.Generation, 120)
			batch := IngestBatch{
				SourceFileID: current.SourceFileID, Generation: opened.Generation,
				PreviousCommittedOffset: 100, PreviousFingerprint: &previous, Fingerprint: current,
				Facts: []FactBatch{{Turn: &turn}}, Diagnostics: []IngestDiagnostic{{
					Class: "compatibility", Code: "unknown_event_type", StartOffset: 105, EndOffset: 110,
				}},
				Checkpoint: checkpoint, EOF: true, AtMS: 40,
			}
			if testCase.job {
				zero, total := int64(0), int64(120)
				sourceFileID := current.SourceFileID
				job := JobRun{
					JobID: "job-fault", JobType: "codex_ingest", RequestedBy: "test",
					State: JobQueued, Phase: JobPhaseReconcile, SourceFileID: &sourceFileID,
					CreatedAtMS: 31, ProgressCurrent: &zero, ProgressTotal: &total,
					ResumeCursor: &JobCursor{Generation: 0, Offset: 100}, UpdatedAtMS: 31,
				}
				if err := repository.CreateJobRun(ctx, job); err != nil {
					t.Fatalf("CreateJobRun() error = %v", err)
				}
				progress := int64(120)
				batch.JobTransition = &JobTransition{
					JobID: job.JobID, ExpectedState: JobQueued, State: JobRunning,
					Phase: JobPhaseReconcile, ProgressCurrent: &progress, ProgressTotal: &total,
					ResumeCursor: &JobCursor{Generation: 0, Offset: 120}, AtMS: 40,
				}
			}
			installIngestFaultTrigger(t, repository, "fault_active", testCase.trigger)
			if _, err := repository.CommitIngestBatch(ctx, batch); err == nil {
				t.Fatal("CommitIngestBatch(fault) succeeded, want rollback")
			}
			assertActiveAppendRolledBack(t, repository, previous, testCase.job)
		})
	}
}

func TestCommitIngestBatchRollsBackActivationAndCanResume(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	repository := openRuntimeRepository(t)
	previous, _ := activateFaultFixture(t, repository)
	current := testSourceFingerprint(previous.SourceFileID, previous.CurrentPath, previous.Inode, 80, 2_000)
	building, err := repository.PrepareGeneration(ctx, PrepareGenerationRequest{
		Mode: GenerationModeRebuild, Previous: &previous, Current: current,
		ParserVersion: "codex-rollout-v1", AtMS: 30,
	})
	if err != nil {
		t.Fatalf("PrepareGeneration(rebuild) error = %v", err)
	}
	session, turn, checkpoint := testCommittedSessionTurn("session-a", "turn-new", 1, 50)
	if _, err := repository.CommitIngestBatch(ctx, IngestBatch{
		SourceFileID: current.SourceFileID, Generation: building.Generation,
		PreviousCommittedOffset: 0, Fingerprint: current,
		Facts: []FactBatch{{Session: &session}, {Turn: &turn}}, Checkpoint: checkpoint,
		AtMS: 40,
	}); err != nil {
		t.Fatalf("CommitIngestBatch(staging) error = %v", err)
	}
	finalCheckpoint := checkpoint
	finalCheckpoint.CommittedOffset = 80
	installIngestFaultTrigger(t, repository, "fault_activation", `BEFORE DELETE ON turns`)
	batch := IngestBatch{
		SourceFileID: current.SourceFileID, Generation: building.Generation,
		PreviousCommittedOffset: 50, Fingerprint: current,
		Checkpoint: finalCheckpoint, EOF: true, AtMS: 50,
	}
	if _, err := repository.CommitIngestBatch(ctx, batch); err == nil {
		t.Fatal("CommitIngestBatch(activation fault) succeeded, want rollback")
	}
	if _, err := repository.Turn(ctx, "turn-old"); err != nil {
		t.Fatalf("old turn disappeared after activation rollback: %v", err)
	}
	if _, err := repository.Turn(ctx, "turn-new"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("new turn visible after activation rollback: %v", err)
	}
	cursor, err := repository.GenerationCursor(ctx, current.SourceFileID, 1)
	if err != nil || cursor.State != GenerationBuilding || cursor.Checkpoint.CommittedOffset != 50 {
		t.Fatalf("GenerationCursor(after rollback) = %#v, %v, want building offset 50", cursor, err)
	}
	file, err := repository.SourceFile(ctx, current.SourceFileID)
	if err != nil || file.ActiveGeneration != 0 || file.ParsedOffset != 100 {
		t.Fatalf("SourceFile(after rollback) = %#v, %v, want old active cursor", file, err)
	}
	dropIngestFaultTrigger(t, repository, "fault_activation")
	if _, err := repository.CommitIngestBatch(ctx, batch); err != nil {
		t.Fatalf("CommitIngestBatch(resume) error = %v", err)
	}
	if _, err := repository.Turn(ctx, "turn-old"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("old turn after resumed activation error = %v, want ErrNotFound", err)
	}
	if _, err := repository.Turn(ctx, "turn-new"); err != nil {
		t.Fatalf("new turn after resumed activation error = %v", err)
	}
}

func TestPrepareGenerationRollsBackSupersededStagingCleanup(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	repository := openRuntimeRepository(t)
	initialFingerprint := testSourceFingerprint("source-a", "/synthetic/session-a.jsonl", 11, 100, 1_000)
	initial, err := repository.PrepareGeneration(ctx, PrepareGenerationRequest{
		Mode: GenerationModeRebuild, Current: initialFingerprint,
		ParserVersion: "codex-rollout-v1", AtMS: 10,
	})
	if err != nil {
		t.Fatalf("PrepareGeneration(initial) error = %v", err)
	}
	session, _, checkpoint := testCommittedSessionTurn("session-a", "turn-a", 0, 100)
	if _, err := repository.CommitIngestBatch(ctx, IngestBatch{
		SourceFileID: initial.SourceFileID, Generation: initial.Generation,
		Fingerprint: initialFingerprint, Facts: []FactBatch{{Session: &session}},
		Checkpoint: checkpoint, EOF: false, AtMS: 20,
	}); err != nil {
		t.Fatalf("CommitIngestBatch(stage initial) error = %v", err)
	}
	assertGenerationBatchCount(t, repository, initial.SourceFileID, initial.Generation, 1)
	installIngestFaultTrigger(
		t, repository, "fail_superseded_staging_cleanup",
		"BEFORE DELETE ON source_generation_batches",
	)
	grown := testSourceFingerprint(initialFingerprint.SourceFileID, initialFingerprint.CurrentPath, 11, 120, 2_000)
	_, err = repository.PrepareGeneration(ctx, PrepareGenerationRequest{
		Mode: GenerationModeAppend, Previous: &initialFingerprint, Current: grown,
		ParserVersion: "codex-rollout-v1", SupersedeBuilding: buildingExpectation(initial), AtMS: 30,
	})
	if err == nil {
		t.Fatal("PrepareGeneration(cleanup fault) succeeded, want rollback")
	}
	dropIngestFaultTrigger(t, repository, "fail_superseded_staging_cleanup")
	cursor, err := repository.GenerationCursor(ctx, initial.SourceFileID, initial.Generation)
	if err != nil || cursor.State != GenerationBuilding || cursor.Checkpoint.CommittedOffset != 100 {
		t.Fatalf("GenerationCursor(after cleanup rollback) = %#v, %v, want original building", cursor, err)
	}
	assertGenerationBatchCount(t, repository, initial.SourceFileID, initial.Generation, 1)
}

func TestCommitIngestBatchRollsBackCompetingBuildingCleanup(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	repository := openRuntimeRepository(t)
	active, _ := activateFaultFixture(t, repository)
	replacedSourceID := active.SourceFileID
	fingerprintB := testSourceFingerprint("source-b", active.CurrentPath, 12, 80, 2_000)
	fingerprintC := testSourceFingerprint("source-c", active.CurrentPath, 13, 80, 3_000)
	buildingB, err := repository.PrepareGeneration(ctx, PrepareGenerationRequest{
		Mode: GenerationModeRebuild, Previous: &active, Current: fingerprintB,
		ParserVersion: "codex-rollout-v1", ReplacesSourceFileID: &replacedSourceID, AtMS: 30,
	})
	if err != nil {
		t.Fatalf("PrepareGeneration(replacement B) error = %v", err)
	}
	buildingC, err := repository.PrepareGeneration(ctx, PrepareGenerationRequest{
		Mode: GenerationModeRebuild, Previous: &active, Current: fingerprintC,
		ParserVersion: "codex-rollout-v1", ReplacesSourceFileID: &replacedSourceID, AtMS: 31,
	})
	if err != nil {
		t.Fatalf("PrepareGeneration(replacement C) error = %v", err)
	}
	sessionC, _, checkpointC := testCommittedSessionTurn("session-c", "turn-c", 0, 40)
	if _, err := repository.CommitIngestBatch(ctx, IngestBatch{
		SourceFileID: fingerprintC.SourceFileID, Generation: buildingC.Generation,
		Fingerprint: fingerprintC, Facts: []FactBatch{{Session: &sessionC}},
		Checkpoint: checkpointC, EOF: false, AtMS: 40,
	}); err != nil {
		t.Fatalf("CommitIngestBatch(stage C) error = %v", err)
	}
	assertGenerationBatchCount(t, repository, fingerprintC.SourceFileID, buildingC.Generation, 1)
	installIngestFaultTrigger(
		t, repository, "fail_competing_staging_cleanup",
		"BEFORE DELETE ON source_generation_batches WHEN OLD.source_file_id = 'source-c'",
	)
	sessionB, turnB, checkpointB := testCommittedSessionTurn("session-b", "turn-b", 0, 80)
	batchB := IngestBatch{
		SourceFileID: fingerprintB.SourceFileID, Generation: buildingB.Generation,
		Fingerprint: fingerprintB, Facts: []FactBatch{{Session: &sessionB}, {Turn: &turnB}},
		Checkpoint: checkpointB, EOF: true, AtMS: 50,
	}
	if _, err := repository.CommitIngestBatch(ctx, batchB); err == nil {
		t.Fatal("CommitIngestBatch(cleanup fault) succeeded, want rollback")
	}
	dropIngestFaultTrigger(t, repository, "fail_competing_staging_cleanup")
	activeCursor, err := repository.GenerationCursor(ctx, active.SourceFileID, 0)
	if err != nil || activeCursor.State != GenerationActive {
		t.Fatalf("GenerationCursor(active after rollback) = %#v, %v, want active", activeCursor, err)
	}
	for _, building := range []GenerationCursor{buildingB, buildingC} {
		cursor, err := repository.GenerationCursor(ctx, building.SourceFileID, building.Generation)
		if err != nil || cursor.State != GenerationBuilding {
			t.Fatalf("GenerationCursor(%s after rollback) = %#v, %v, want building", building.SourceFileID, cursor, err)
		}
	}
	assertGenerationBatchCount(t, repository, fingerprintB.SourceFileID, buildingB.Generation, 0)
	assertGenerationBatchCount(t, repository, fingerprintC.SourceFileID, buildingC.Generation, 1)
	if _, err := repository.Session(ctx, "session-b"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Session(B after rollback) error = %v, want ErrNotFound", err)
	}

	if _, err := repository.CommitIngestBatch(ctx, batchB); err != nil {
		t.Fatalf("CommitIngestBatch(resume B) error = %v", err)
	}
	cursorC, err := repository.GenerationCursor(ctx, fingerprintC.SourceFileID, buildingC.Generation)
	if err != nil || cursorC.State != GenerationSuperseded {
		t.Fatalf("GenerationCursor(C after resume) = %#v, %v, want superseded", cursorC, err)
	}
	assertGenerationBatchCount(t, repository, fingerprintC.SourceFileID, buildingC.Generation, 0)
}

func TestCommitIngestBatchRollsBackDependentCleanupOnActiveAdvance(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	repository := openRuntimeRepository(t)
	active, _ := activateFaultFixture(t, repository)
	activeCursor, err := repository.GenerationCursor(ctx, active.SourceFileID, 0)
	if err != nil {
		t.Fatalf("GenerationCursor(active) error = %v", err)
	}
	replacedSourceID := active.SourceFileID
	dependentFingerprint := testSourceFingerprint("source-b", active.CurrentPath, 12, 80, 2_000)
	dependent, err := repository.PrepareGeneration(ctx, PrepareGenerationRequest{
		Mode: GenerationModeRebuild, Previous: &active, Current: dependentFingerprint,
		ParserVersion: "codex-rollout-v1", ReplacesSourceFileID: &replacedSourceID, AtMS: 30,
	})
	if err != nil {
		t.Fatalf("PrepareGeneration(dependent) error = %v", err)
	}
	dependentSession, _, dependentCheckpoint := testCommittedSessionTurn("session-b", "turn-b", 0, 40)
	if _, err := repository.CommitIngestBatch(ctx, IngestBatch{
		SourceFileID: dependentFingerprint.SourceFileID, Generation: dependent.Generation,
		Fingerprint: dependentFingerprint, Facts: []FactBatch{{Session: &dependentSession}},
		Checkpoint: dependentCheckpoint, EOF: false, AtMS: 40,
	}); err != nil {
		t.Fatalf("CommitIngestBatch(stage dependent) error = %v", err)
	}
	installIngestFaultTrigger(
		t, repository, "fail_dependent_staging_cleanup",
		"BEFORE DELETE ON source_generation_batches WHEN OLD.source_file_id = 'source-b'",
	)
	advanced := testSourceFingerprint(active.SourceFileID, active.CurrentPath, active.Inode, 120, 3_000)
	advancedCheckpoint := activeCursor.Checkpoint
	advancedCheckpoint.CommittedOffset = 120
	batch := IngestBatch{
		SourceFileID: active.SourceFileID, Generation: activeCursor.Generation,
		PreviousCommittedOffset: activeCursor.Checkpoint.CommittedOffset,
		PreviousFingerprint:     &active, Fingerprint: advanced,
		Checkpoint: advancedCheckpoint, EOF: true, AtMS: 50,
	}
	if _, err := repository.CommitIngestBatch(ctx, batch); err == nil {
		t.Fatal("CommitIngestBatch(active cleanup fault) succeeded, want rollback")
	}
	dropIngestFaultTrigger(t, repository, "fail_dependent_staging_cleanup")
	rolledBack, err := repository.GenerationCursor(ctx, active.SourceFileID, activeCursor.Generation)
	if err != nil || rolledBack.State != GenerationActive || rolledBack.Fingerprint != active ||
		rolledBack.Checkpoint.CommittedOffset != activeCursor.Checkpoint.CommittedOffset {
		t.Fatalf("GenerationCursor(active after rollback) = %#v, %v, want original active", rolledBack, err)
	}
	dependentAfterRollback, err := repository.GenerationCursor(ctx, dependent.SourceFileID, dependent.Generation)
	if err != nil || dependentAfterRollback.State != GenerationBuilding ||
		dependentAfterRollback.Checkpoint.CommittedOffset != 40 {
		t.Fatalf("GenerationCursor(dependent after rollback) = %#v, %v, want building offset 40", dependentAfterRollback, err)
	}
	assertGenerationBatchCount(t, repository, dependent.SourceFileID, dependent.Generation, 1)

	if _, err := repository.CommitIngestBatch(ctx, batch); err != nil {
		t.Fatalf("CommitIngestBatch(resume active) error = %v", err)
	}
	dependentAfterResume, err := repository.GenerationCursor(ctx, dependent.SourceFileID, dependent.Generation)
	if err != nil || dependentAfterResume.State != GenerationSuperseded {
		t.Fatalf("GenerationCursor(dependent after resume) = %#v, %v, want superseded", dependentAfterResume, err)
	}
	assertGenerationBatchCount(t, repository, dependent.SourceFileID, dependent.Generation, 0)
}

func activateFaultFixture(t *testing.T, repository *Repository) (SourceFingerprint, GenerationCursor) {
	t.Helper()
	ctx := context.Background()
	fingerprint := testSourceFingerprint("source-a", "/synthetic/session-a.jsonl", 11, 100, 1_000)
	cursor, err := repository.PrepareGeneration(ctx, PrepareGenerationRequest{
		Mode: GenerationModeRebuild, Current: fingerprint,
		ParserVersion: "codex-rollout-v1", AtMS: 10,
	})
	if err != nil {
		t.Fatalf("PrepareGeneration(initial) error = %v", err)
	}
	session, turn, checkpoint := testCommittedSessionTurn("session-a", "turn-old", 0, 100)
	if _, err := repository.CommitIngestBatch(ctx, IngestBatch{
		SourceFileID: fingerprint.SourceFileID, Generation: cursor.Generation,
		PreviousCommittedOffset: 0, Fingerprint: fingerprint,
		Facts: []FactBatch{{Session: &session}, {Turn: &turn}}, Checkpoint: checkpoint,
		EOF: true, AtMS: 20,
	}); err != nil {
		t.Fatalf("CommitIngestBatch(initial) error = %v", err)
	}
	return fingerprint, cursor
}

func assertActiveAppendRolledBack(
	t *testing.T,
	repository *Repository,
	previous SourceFingerprint,
	wantJob bool,
) {
	t.Helper()
	ctx := context.Background()
	cursor, err := repository.GenerationCursor(ctx, previous.SourceFileID, 0)
	if err != nil || cursor.Checkpoint.CommittedOffset != 100 || cursor.Fingerprint != previous {
		t.Fatalf("GenerationCursor(after rollback) = %#v, %v, want old fingerprint/offset", cursor, err)
	}
	file, err := repository.SourceFile(ctx, previous.SourceFileID)
	if err != nil || file.ParsedOffset != 100 || file.SizeBytes != 100 {
		t.Fatalf("SourceFile(after rollback) = %#v, %v, want old source cursor", file, err)
	}
	if _, err := repository.Turn(ctx, "turn-new"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Turn(new after rollback) error = %v, want ErrNotFound", err)
	}
	var diagnosticCount, receiptCount int64
	err = repository.database.View(ctx, func(ctx context.Context, database *gorm.DB) error {
		if err := database.WithContext(ctx).Model(&parserDiagnosticModel{}).
			Where("source_file_id = ? AND generation = ? AND batch_end_offset = ?", previous.SourceFileID, 0, 120).
			Count(&diagnosticCount).Error; err != nil {
			return err
		}
		return database.WithContext(ctx).Model(&sourceGenerationBatchModel{}).
			Where("source_file_id = ? AND generation = ? AND to_offset = ?", previous.SourceFileID, 0, 120).
			Count(&receiptCount).Error
	})
	if err != nil || diagnosticCount != 0 || receiptCount != 0 {
		t.Fatalf("rollback residue: diagnostics=%d receipts=%d error=%v", diagnosticCount, receiptCount, err)
	}
	if wantJob {
		job, err := repository.JobRun(ctx, "job-fault")
		if err != nil || job.State != JobQueued || job.ResumeCursor == nil || job.ResumeCursor.Offset != 100 {
			t.Fatalf("JobRun(after rollback) = %#v, %v, want queued offset 100", job, err)
		}
	}
}

func installIngestFaultTrigger(t *testing.T, repository *Repository, name, clause string) {
	t.Helper()
	statement := fmt.Sprintf(
		`CREATE TRIGGER %s %s BEGIN SELECT RAISE(ABORT, 'synthetic ingest fault'); END`,
		name, clause,
	)
	// SQLite trigger DDL is a test-only fault injector; production business CRUD remains GORM-only.
	if err := repository.database.Write(context.Background(), func(ctx context.Context, database *gorm.DB) error {
		return database.WithContext(ctx).Exec(statement).Error
	}); err != nil {
		t.Fatalf("install fault trigger: %v", err)
	}
}

func dropIngestFaultTrigger(t *testing.T, repository *Repository, name string) {
	t.Helper()
	if err := repository.database.Write(context.Background(), func(ctx context.Context, database *gorm.DB) error {
		return database.WithContext(ctx).Exec(`DROP TRIGGER ` + name).Error
	}); err != nil {
		t.Fatalf("drop fault trigger: %v", err)
	}
}
