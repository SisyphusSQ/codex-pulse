package store

import (
	"context"
	"errors"
	"testing"

	"gorm.io/gorm"
)

func TestCommitIngestBatchActivatesInitialGenerationAtomicallyAndReplays(t *testing.T) {
	t.Parallel()

	repository := openRuntimeRepository(t)
	fingerprint := testSourceFingerprint("source-a", "/synthetic/session-a.jsonl", 11, 100, 1_000)
	cursor, err := repository.PrepareGeneration(context.Background(), PrepareGenerationRequest{
		Mode: GenerationModeRebuild, Current: fingerprint,
		ParserVersion: "codex-rollout-v1", AtMS: 10,
	})
	if err != nil {
		t.Fatalf("PrepareGeneration() error = %v", err)
	}
	zero := int64(0)
	total := int64(100)
	sourceFileID := fingerprint.SourceFileID
	job := JobRun{
		JobID: "job-a", JobType: "codex_ingest", RequestedBy: "test", Priority: 0,
		State: JobQueued, Phase: JobPhaseReconcile, SourceFileID: &sourceFileID,
		CreatedAtMS: 11, ProgressCurrent: &zero, ProgressTotal: &total,
		ResumeCursor: &JobCursor{Generation: 0, Offset: 0}, UpdatedAtMS: 11,
	}
	if err := repository.CreateJobRun(context.Background(), job); err != nil {
		t.Fatalf("CreateJobRun() error = %v", err)
	}
	session, turn, checkpoint := testCommittedSessionTurn("session-a", "turn-a", 0, 100)
	progress := int64(100)
	transition := JobTransition{
		JobID: job.JobID, ExpectedState: JobQueued, State: JobRunning, Phase: JobPhaseReconcile,
		ProgressCurrent: &progress, ProgressTotal: &total,
		ResumeCursor: &JobCursor{Generation: 0, Offset: 100}, AtMS: 20,
	}
	batch := IngestBatch{
		SourceFileID: fingerprint.SourceFileID, Generation: cursor.Generation,
		PreviousCommittedOffset: 0, Fingerprint: fingerprint,
		Facts: []FactBatch{{Session: &session}, {Turn: &turn}},
		Diagnostics: []IngestDiagnostic{{
			Class: "compatibility", Code: "unknown_event_type",
			StartOffset: 40, EndOffset: 50, Retryable: false,
		}},
		Checkpoint: checkpoint, EOF: true, JobTransition: &transition, AtMS: 20,
	}
	committed, err := repository.CommitIngestBatch(context.Background(), batch)
	if err != nil {
		t.Fatalf("CommitIngestBatch() error = %v", err)
	}
	if committed.State != GenerationActive || committed.Generation != 0 ||
		committed.Checkpoint.CommittedOffset != 100 {
		t.Fatalf("CommitIngestBatch() = %#v, want active generation 0 at 100", committed)
	}
	if _, err := repository.Session(context.Background(), session.SessionID); err != nil {
		t.Fatalf("Session() error = %v", err)
	}
	if got, err := repository.Turn(context.Background(), turn.TurnID); err != nil || got.SourceGeneration != 0 {
		t.Fatalf("Turn() = %#v, %v, want generation 0", got, err)
	}
	file, err := repository.SourceFile(context.Background(), fingerprint.SourceFileID)
	if err != nil {
		t.Fatalf("SourceFile() error = %v", err)
	}
	if file.State != SourceFileActive || file.ActiveGeneration != 0 || file.ParsedOffset != 100 ||
		file.SessionID == nil || *file.SessionID != session.SessionID {
		t.Fatalf("SourceFile() = %#v, want active session at offset 100", file)
	}
	gotJob, err := repository.JobRun(context.Background(), job.JobID)
	if err != nil {
		t.Fatalf("JobRun() error = %v", err)
	}
	if gotJob.State != JobRunning || gotJob.ProgressCurrent == nil || *gotJob.ProgressCurrent != 100 ||
		gotJob.ResumeCursor == nil || gotJob.ResumeCursor.Offset != 100 {
		t.Fatalf("JobRun() = %#v, want running progress/cursor 100", gotJob)
	}

	replayed, err := repository.CommitIngestBatch(context.Background(), batch)
	if err != nil {
		t.Fatalf("CommitIngestBatch(replay) error = %v", err)
	}
	if replayed.Checkpoint.CommittedOffset != committed.Checkpoint.CommittedOffset || replayed.State != GenerationActive {
		t.Fatalf("CommitIngestBatch(replay) = %#v, want exact committed cursor", replayed)
	}
	conflict := batch
	conflict.Facts = append([]FactBatch(nil), batch.Facts...)
	conflictingSession := *conflict.Facts[0].Session
	conflictingSession.LastSeenAtMS++
	conflict.Facts[0].Session = &conflictingSession
	if _, err := repository.CommitIngestBatch(context.Background(), conflict); !errors.Is(err, ErrInvalidRecord) {
		t.Fatalf("CommitIngestBatch(conflicting replay) error = %v, want ErrInvalidRecord", err)
	}
}

func TestCommitIngestBatchExactReplayTreatsNilDiagnosticsAsEmpty(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	repository := openRuntimeRepository(t)
	fingerprint := testSourceFingerprint("source-a", "/synthetic/session-a.jsonl", 11, 100, 1_000)
	cursor, err := repository.PrepareGeneration(ctx, PrepareGenerationRequest{
		Mode: GenerationModeRebuild, Current: fingerprint,
		ParserVersion: "codex-rollout-v1", AtMS: 10,
	})
	if err != nil {
		t.Fatalf("PrepareGeneration() error = %v", err)
	}
	session, turn, checkpoint := testCommittedSessionTurn("session-a", "turn-a", 0, 100)
	batch := IngestBatch{
		SourceFileID: fingerprint.SourceFileID, Generation: cursor.Generation,
		PreviousCommittedOffset: 0, Fingerprint: fingerprint,
		Facts:       []FactBatch{{Session: &session}, {Turn: &turn}},
		Diagnostics: nil, Checkpoint: checkpoint, EOF: true, AtMS: 20,
	}
	if _, err := repository.CommitIngestBatch(ctx, batch); err != nil {
		t.Fatalf("CommitIngestBatch() error = %v", err)
	}
	if _, err := repository.CommitIngestBatch(ctx, batch); err != nil {
		t.Fatalf("CommitIngestBatch(exact nil-diagnostic replay) error = %v", err)
	}
}

func TestValidIngestDiagnosticAcceptsQuotaCompatibilityCodes(t *testing.T) {
	t.Parallel()

	for _, code := range []string{"invalid_quota_window", "invalid_quota_snapshot"} {
		diagnostic := IngestDiagnostic{
			Class: "compatibility", Code: code, StartOffset: 10, EndOffset: 20,
		}
		if !validIngestDiagnostic(diagnostic, 20) {
			t.Fatalf("validIngestDiagnostic(%q) = false, want true", code)
		}
	}
}

func TestCommitIngestBatchKeepsOldFactsVisibleUntilRebuildActivation(t *testing.T) {
	t.Parallel()

	repository := openRuntimeRepository(t)
	oldFingerprint := testSourceFingerprint("source-a", "/synthetic/session-a.jsonl", 11, 100, 1_000)
	initial, err := repository.PrepareGeneration(context.Background(), PrepareGenerationRequest{
		Mode: GenerationModeRebuild, Current: oldFingerprint,
		ParserVersion: "codex-rollout-v1", AtMS: 10,
	})
	if err != nil {
		t.Fatalf("PrepareGeneration(initial) error = %v", err)
	}
	oldSession, oldTurn, oldCheckpoint := testCommittedSessionTurn("session-a", "turn-old", 0, 100)
	if _, err := repository.CommitIngestBatch(context.Background(), IngestBatch{
		SourceFileID: oldFingerprint.SourceFileID, Generation: initial.Generation,
		PreviousCommittedOffset: 0, Fingerprint: oldFingerprint,
		Facts:      []FactBatch{{Session: &oldSession}, {Turn: &oldTurn}},
		Checkpoint: oldCheckpoint, EOF: true, AtMS: 20,
	}); err != nil {
		t.Fatalf("CommitIngestBatch(initial) error = %v", err)
	}

	newFingerprint := testSourceFingerprint("source-a", "/synthetic/session-a.jsonl", 11, 80, 2_000)
	rebuild, err := repository.PrepareGeneration(context.Background(), PrepareGenerationRequest{
		Mode: GenerationModeRebuild, Previous: &oldFingerprint, Current: newFingerprint,
		ParserVersion: "codex-rollout-v1", AtMS: 30,
	})
	if err != nil {
		t.Fatalf("PrepareGeneration(rebuild) error = %v", err)
	}
	if rebuild.Generation != 1 || rebuild.State != GenerationBuilding {
		t.Fatalf("PrepareGeneration(rebuild) = %#v, want building generation 1", rebuild)
	}
	newSession, newTurn, firstCheckpoint := testCommittedSessionTurn("session-a", "turn-new", 1, 50)
	firstFingerprint := newFingerprint
	firstFingerprint.SizeBytes = 80
	if _, err := repository.CommitIngestBatch(context.Background(), IngestBatch{
		SourceFileID: newFingerprint.SourceFileID, Generation: rebuild.Generation,
		PreviousCommittedOffset: 0, Fingerprint: firstFingerprint,
		Facts:      []FactBatch{{Session: &newSession}, {Turn: &newTurn}},
		Checkpoint: firstCheckpoint, EOF: false, AtMS: 40,
	}); err != nil {
		t.Fatalf("CommitIngestBatch(building) error = %v", err)
	}
	if _, err := repository.Turn(context.Background(), oldTurn.TurnID); err != nil {
		t.Fatalf("old Turn disappeared before activation: %v", err)
	}
	if _, err := repository.Turn(context.Background(), newTurn.TurnID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("new Turn before activation error = %v, want ErrNotFound", err)
	}
	file, err := repository.SourceFile(context.Background(), newFingerprint.SourceFileID)
	if err != nil || file.ActiveGeneration != 0 || file.ParsedOffset != 100 {
		t.Fatalf("SourceFile(before activation) = %#v, %v, want old generation", file, err)
	}

	finalCheckpoint := firstCheckpoint
	finalCheckpoint.CommittedOffset = 80
	if finalCheckpoint.Seed != nil && len(finalCheckpoint.Seed.ClosedTurns) == 1 {
		finalCheckpoint.Seed.ClosedTurns[0].Terminal.FinalUsage = nil
	}
	if _, err := repository.CommitIngestBatch(context.Background(), IngestBatch{
		SourceFileID: newFingerprint.SourceFileID, Generation: rebuild.Generation,
		PreviousCommittedOffset: 50, Fingerprint: newFingerprint,
		Checkpoint: finalCheckpoint, EOF: true, AtMS: 50,
	}); err != nil {
		t.Fatalf("CommitIngestBatch(activate) error = %v", err)
	}
	if _, err := repository.Turn(context.Background(), oldTurn.TurnID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("old Turn after activation error = %v, want ErrNotFound", err)
	}
	got, err := repository.Turn(context.Background(), newTurn.TurnID)
	if err != nil || got.SourceGeneration != 1 {
		t.Fatalf("new Turn after activation = %#v, %v, want generation 1", got, err)
	}
	file, err = repository.SourceFile(context.Background(), newFingerprint.SourceFileID)
	if err != nil || file.ActiveGeneration != 1 || file.ParsedOffset != 80 {
		t.Fatalf("SourceFile(after activation) = %#v, %v, want generation 1 offset 80", file, err)
	}
}

func TestCommitIngestBatchRebuildsSameSourceToEmptySnapshot(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	repository := openRuntimeRepository(t)
	previous, _ := activateFaultFixture(t, repository)
	current := testSourceFingerprint(previous.SourceFileID, previous.CurrentPath, previous.Inode, 0, 2_000)
	rebuild, err := repository.PrepareGeneration(ctx, PrepareGenerationRequest{
		Mode: GenerationModeRebuild, Previous: &previous, Current: current,
		ParserVersion: "codex-rollout-v1", AtMS: 30,
	})
	if err != nil {
		t.Fatalf("PrepareGeneration(rebuild empty) error = %v", err)
	}

	committed, err := repository.CommitIngestBatch(ctx, IngestBatch{
		SourceFileID: current.SourceFileID, Generation: rebuild.Generation,
		PreviousCommittedOffset: 0, Fingerprint: current,
		Checkpoint: ParserCheckpoint{
			Version: ParserCheckpointVersion, ParserVersion: "codex-rollout-v1",
		},
		EOF: true, AtMS: 40,
	})
	if err != nil {
		t.Fatalf("CommitIngestBatch(rebuild empty) error = %v", err)
	}
	if committed.State != GenerationActive || committed.Generation != 1 {
		t.Fatalf("CommitIngestBatch(rebuild empty) = %#v, want active generation 1", committed)
	}
	if _, err := repository.Session(ctx, "session-a"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Session(old after empty rebuild) error = %v, want ErrNotFound", err)
	}
	if _, err := repository.Turn(ctx, "turn-old"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Turn(old after empty rebuild) error = %v, want ErrNotFound", err)
	}
	file, err := repository.SourceFile(ctx, current.SourceFileID)
	if err != nil || file.ActiveGeneration != 1 || file.ParsedOffset != 0 || file.SessionID != nil {
		t.Fatalf("SourceFile(after empty rebuild) = %#v, %v, want generation 1 without session", file, err)
	}
}

func TestCommitIngestBatchRebuildReplacesSessionIdentity(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	repository := openRuntimeRepository(t)
	previous, _ := activateFaultFixture(t, repository)
	current := testSourceFingerprint(previous.SourceFileID, previous.CurrentPath, previous.Inode, 80, 2_000)
	rebuild, err := repository.PrepareGeneration(ctx, PrepareGenerationRequest{
		Mode: GenerationModeRebuild, Previous: &previous, Current: current,
		ParserVersion: "codex-rollout-v1", AtMS: 30,
	})
	if err != nil {
		t.Fatalf("PrepareGeneration(rebuild session identity) error = %v", err)
	}
	newSession, newTurn, checkpoint := testCommittedSessionTurn("session-b", "turn-new", 1, 80)
	if _, err := repository.CommitIngestBatch(ctx, IngestBatch{
		SourceFileID: current.SourceFileID, Generation: rebuild.Generation,
		PreviousCommittedOffset: 0, Fingerprint: current,
		Facts:      []FactBatch{{Session: &newSession}, {Turn: &newTurn}},
		Checkpoint: checkpoint, EOF: true, AtMS: 40,
	}); err != nil {
		t.Fatalf("CommitIngestBatch(rebuild session identity) error = %v", err)
	}
	if _, err := repository.Session(ctx, "session-a"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Session(old identity) error = %v, want ErrNotFound", err)
	}
	if _, err := repository.Turn(ctx, "turn-old"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Turn(old identity) error = %v, want ErrNotFound", err)
	}
	if session, err := repository.Session(ctx, "session-b"); err != nil || session.SessionID != "session-b" {
		t.Fatalf("Session(new identity) = %#v, %v", session, err)
	}
	file, err := repository.SourceFile(ctx, current.SourceFileID)
	if err != nil || file.SessionID == nil || *file.SessionID != "session-b" || file.ActiveGeneration != 1 {
		t.Fatalf("SourceFile(after session replacement) = %#v, %v", file, err)
	}
}

func TestCommitIngestBatchRebuildReplacesSessionMetadataAuthoritatively(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	repository := openRuntimeRepository(t)
	previous, _ := activateFaultFixture(t, repository)
	oldOriginator := "old-cli"
	oldCWD := "/synthetic/old"
	if err := repository.UpsertFacts(ctx, FactBatch{Session: &Session{
		SessionID: "session-a", Provider: "codex", SourceKind: "session",
		Originator: &oldOriginator, InitialCWD: &oldCWD,
		CreatedAtMS: 1_000, FirstSeenAtMS: 1_000, LastSeenAtMS: 2_000,
	}}); err != nil {
		t.Fatalf("UpsertFacts(old authoritative metadata) error = %v", err)
	}

	current := testSourceFingerprint(previous.SourceFileID, previous.CurrentPath, previous.Inode, 80, 2_000)
	rebuild, err := repository.PrepareGeneration(ctx, PrepareGenerationRequest{
		Mode: GenerationModeRebuild, Previous: &previous, Current: current,
		ParserVersion: "codex-rollout-v1", AtMS: 30,
	})
	if err != nil {
		t.Fatalf("PrepareGeneration(rebuild metadata) error = %v", err)
	}
	newSession, newTurn, checkpoint := testCommittedSessionTurn("session-a", "turn-new", 1, 80)
	newOriginator := "new-cli"
	newCWD := "/synthetic/new"
	newSession.Originator = &newOriginator
	newSession.InitialCWD = &newCWD
	newSession.LastSeenAtMS = 1_500
	if _, err := repository.CommitIngestBatch(ctx, IngestBatch{
		SourceFileID: current.SourceFileID, Generation: rebuild.Generation,
		PreviousCommittedOffset: 0, Fingerprint: current,
		Facts:      []FactBatch{{Session: &newSession}, {Turn: &newTurn}},
		Checkpoint: checkpoint, EOF: true, AtMS: 40,
	}); err != nil {
		t.Fatalf("CommitIngestBatch(rebuild metadata) error = %v", err)
	}
	stored, err := repository.Session(ctx, "session-a")
	if err != nil || stored.LastSeenAtMS != 1_500 || stored.Originator == nil || *stored.Originator != newOriginator ||
		stored.InitialCWD == nil || *stored.InitialCWD != newCWD {
		t.Fatalf("Session(authoritative replacement) = %#v, %v", stored, err)
	}
	if _, err := repository.Turn(ctx, "turn-old"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Turn(old metadata generation) error = %v, want ErrNotFound", err)
	}
}

func TestCommitIngestBatchRejectsStaleDualReplacementAtEOF(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	repository := openRuntimeRepository(t)
	previous, _ := activateFaultFixture(t, repository)
	replacedSourceID := previous.SourceFileID
	currentB := testSourceFingerprint("source-b", previous.CurrentPath, 12, 80, 2_000)
	currentC := testSourceFingerprint("source-c", previous.CurrentPath, 13, 80, 3_000)
	buildingB, err := repository.PrepareGeneration(ctx, PrepareGenerationRequest{
		Mode: GenerationModeRebuild, Previous: &previous, Current: currentB,
		ParserVersion: "codex-rollout-v1", ReplacesSourceFileID: &replacedSourceID, AtMS: 30,
	})
	if err != nil {
		t.Fatalf("PrepareGeneration(replacement B) error = %v", err)
	}
	buildingC, err := repository.PrepareGeneration(ctx, PrepareGenerationRequest{
		Mode: GenerationModeRebuild, Previous: &previous, Current: currentC,
		ParserVersion: "codex-rollout-v1", ReplacesSourceFileID: &replacedSourceID, AtMS: 31,
	})
	if err != nil {
		t.Fatalf("PrepareGeneration(replacement C) error = %v", err)
	}

	sessionB, turnB, checkpointB := testCommittedSessionTurn("session-b", "turn-b", 0, 80)
	if _, err := repository.CommitIngestBatch(ctx, IngestBatch{
		SourceFileID: currentB.SourceFileID, Generation: buildingB.Generation,
		PreviousCommittedOffset: 0, Fingerprint: currentB,
		Facts:      []FactBatch{{Session: &sessionB}, {Turn: &turnB}},
		Checkpoint: checkpointB, EOF: true, AtMS: 40,
	}); err != nil {
		t.Fatalf("CommitIngestBatch(replacement B) error = %v", err)
	}

	sessionC, turnC, checkpointC := testCommittedSessionTurn("session-c", "turn-c", 0, 80)
	_, err = repository.CommitIngestBatch(ctx, IngestBatch{
		SourceFileID: currentC.SourceFileID, Generation: buildingC.Generation,
		PreviousCommittedOffset: 0, Fingerprint: currentC,
		Facts:      []FactBatch{{Session: &sessionC}, {Turn: &turnC}},
		Checkpoint: checkpointC, EOF: true, AtMS: 50,
	})
	if !errors.Is(err, ErrInvalidRecord) {
		t.Fatalf("CommitIngestBatch(stale replacement C) error = %v, want ErrInvalidRecord", err)
	}
	cursorC, err := repository.GenerationCursor(ctx, currentC.SourceFileID, buildingC.Generation)
	if err != nil || cursorC.State != GenerationSuperseded || cursorC.Checkpoint.CommittedOffset != 0 {
		t.Fatalf("GenerationCursor(stale C) = %#v, %v, want superseded offset 0", cursorC, err)
	}
	assertGenerationBatchCount(t, repository, currentC.SourceFileID, buildingC.Generation, 0)
	if _, err := repository.Session(ctx, "session-c"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Session(stale C) error = %v, want ErrNotFound", err)
	}
	if _, err := repository.Turn(ctx, "turn-c"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Turn(stale C) error = %v, want ErrNotFound", err)
	}
	if session, err := repository.Session(ctx, "session-b"); err != nil || session.SessionID != "session-b" {
		t.Fatalf("Session(active B) = %#v, %v", session, err)
	}
	fileC, err := repository.SourceFile(ctx, currentC.SourceFileID)
	if err != nil || fileC.State != SourceFileUnavailable {
		t.Fatalf("SourceFile(stale C) = %#v, %v, want unavailable", fileC, err)
	}
}

func TestCommitIngestBatchAdvancesActiveFingerprintWithCAS(t *testing.T) {
	t.Parallel()

	repository := openRuntimeRepository(t)
	previous := testSourceFingerprint("source-a", "/synthetic/session-a.jsonl", 11, 100, 1_000)
	cursor, err := repository.PrepareGeneration(context.Background(), PrepareGenerationRequest{
		Mode: GenerationModeRebuild, Current: previous,
		ParserVersion: "codex-rollout-v1", AtMS: 10,
	})
	if err != nil {
		t.Fatalf("PrepareGeneration(initial) error = %v", err)
	}
	session, turn, checkpoint := testCommittedSessionTurn("session-a", "turn-a", 0, 100)
	if _, err := repository.CommitIngestBatch(context.Background(), IngestBatch{
		SourceFileID: previous.SourceFileID, Generation: cursor.Generation,
		PreviousCommittedOffset: 0, Fingerprint: previous,
		Facts: []FactBatch{{Session: &session}, {Turn: &turn}}, Checkpoint: checkpoint,
		EOF: true, AtMS: 20,
	}); err != nil {
		t.Fatalf("CommitIngestBatch(initial) error = %v", err)
	}

	current := testSourceFingerprint("source-a", "/synthetic/moved/session-a.jsonl", 11, 120, 2_000)
	opened, err := repository.PrepareGeneration(context.Background(), PrepareGenerationRequest{
		Mode: GenerationModeAppend, Previous: &previous, Current: current,
		ParserVersion: "codex-rollout-v1", AtMS: 30,
	})
	if err != nil {
		t.Fatalf("PrepareGeneration(append) error = %v", err)
	}
	next := opened.Checkpoint
	next.CommittedOffset = 120
	if _, err := repository.CommitIngestBatch(context.Background(), IngestBatch{
		SourceFileID: current.SourceFileID, Generation: opened.Generation,
		PreviousCommittedOffset: 100, PreviousFingerprint: &previous, Fingerprint: current,
		Checkpoint: next, EOF: true, AtMS: 40,
	}); err != nil {
		t.Fatalf("CommitIngestBatch(append) error = %v", err)
	}
	file, err := repository.SourceFile(context.Background(), current.SourceFileID)
	if err != nil || file.ParsedOffset != 120 || file.CurrentPath != current.CurrentPath {
		t.Fatalf("SourceFile() = %#v, %v, want moved path at offset 120", file, err)
	}
	snapshots, err := repository.CodexSnapshots(context.Background())
	if err != nil || len(snapshots) != 1 || snapshots[0] != current {
		t.Fatalf("CodexSnapshots() = %#v, %v, want current fingerprint", snapshots, err)
	}

	stale := previous
	stale.MTimeNS--
	stale.FingerprintSHA256 = sourceFingerprintDigest(stale)
	newer := testSourceFingerprint("source-a", current.CurrentPath, 11, 140, 3_000)
	bad := next
	bad.CommittedOffset = 140
	_, err = repository.CommitIngestBatch(context.Background(), IngestBatch{
		SourceFileID: newer.SourceFileID, Generation: opened.Generation,
		PreviousCommittedOffset: 120, PreviousFingerprint: &stale, Fingerprint: newer,
		Checkpoint: bad, EOF: true, AtMS: 50,
	})
	if !errors.Is(err, ErrInvalidRecord) {
		t.Fatalf("CommitIngestBatch(stale fingerprint) error = %v, want ErrInvalidRecord", err)
	}
}

func TestCommitIngestBatchReplacesRepeatedZeroProgressReceipt(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	repository := openRuntimeRepository(t)
	previous, _ := activateFaultFixture(t, repository)
	cursor, err := repository.GenerationCursor(ctx, previous.SourceFileID, 0)
	if err != nil {
		t.Fatalf("GenerationCursor(active) error = %v", err)
	}
	first := testSourceFingerprint(previous.SourceFileID, previous.CurrentPath, previous.Inode, 100, 2_000)
	firstCheckpoint := cursor.Checkpoint
	firstBatch := IngestBatch{
		SourceFileID: previous.SourceFileID, Generation: cursor.Generation,
		PreviousCommittedOffset: cursor.Checkpoint.CommittedOffset,
		PreviousFingerprint:     &previous, Fingerprint: first,
		Diagnostics: []IngestDiagnostic{{
			Class: "syntax", Code: "bad_json", StartOffset: 70, EndOffset: 80, Retryable: true,
		}},
		Checkpoint: firstCheckpoint, EOF: true, AtMS: 30,
	}
	committed, err := repository.CommitIngestBatch(ctx, firstBatch)
	if err != nil {
		t.Fatalf("CommitIngestBatch(first zero progress) error = %v", err)
	}

	second := testSourceFingerprint(previous.SourceFileID, previous.CurrentPath, previous.Inode, 120, 3_000)
	secondBatch := firstBatch
	secondBatch.PreviousFingerprint = &first
	secondBatch.Fingerprint = second
	secondBatch.Diagnostics = nil
	secondBatch.AtMS = 40
	committed, err = repository.CommitIngestBatch(ctx, secondBatch)
	if err != nil {
		t.Fatalf("CommitIngestBatch(second zero progress) error = %v", err)
	}
	if committed.Fingerprint != second || committed.Checkpoint.CommittedOffset != cursor.Checkpoint.CommittedOffset {
		t.Fatalf("CommitIngestBatch(second zero progress) = %#v", committed)
	}
	var diagnostics []IngestDiagnostic
	var firstDiagnostics []IngestDiagnostic
	err = repository.database.View(ctx, func(ctx context.Context, database *gorm.DB) error {
		var lookupErr error
		diagnostics, lookupErr = ingestDiagnosticsForBatch(
			ctx, database, previous.SourceFileID, cursor.Generation,
			cursor.Checkpoint.CommittedOffset, ingestBatchIdentityDigest(secondBatch),
		)
		if lookupErr != nil {
			return lookupErr
		}
		firstDiagnostics, lookupErr = ingestDiagnosticsForBatch(
			ctx, database, previous.SourceFileID, cursor.Generation,
			cursor.Checkpoint.CommittedOffset, ingestBatchIdentityDigest(firstBatch),
		)
		return lookupErr
	})
	if err != nil || len(diagnostics) != 0 {
		t.Fatalf("ingestDiagnosticsForBatch(second zero progress) = %#v, %v, want empty set", diagnostics, err)
	}
	if len(firstDiagnostics) != 1 || firstDiagnostics[0] != firstBatch.Diagnostics[0] {
		t.Fatalf("ingestDiagnosticsForBatch(first zero progress) = %#v, want preserved diagnostic", firstDiagnostics)
	}
	if replayed, err := repository.CommitIngestBatch(ctx, secondBatch); err != nil || replayed.Fingerprint != second {
		t.Fatalf("CommitIngestBatch(second zero progress replay) = %#v, %v", replayed, err)
	}
	if _, err := repository.CommitIngestBatch(ctx, firstBatch); !errors.Is(err, ErrInvalidRecord) {
		t.Fatalf("CommitIngestBatch(stale first zero progress) error = %v, want ErrInvalidRecord", err)
	}
}

func TestCommitIngestBatchAcceptsRepeatedMetadataMovesWithSamePhysicalFingerprint(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	repository := openRuntimeRepository(t)
	previous, _ := activateFaultFixture(t, repository)
	cursor, err := repository.GenerationCursor(ctx, previous.SourceFileID, 0)
	if err != nil {
		t.Fatalf("GenerationCursor(active) error = %v", err)
	}
	first := previous
	first.SourceKind = "archived_session"
	first.CurrentPath = "/synthetic/archived/session-a.jsonl"
	firstCheckpoint := cursor.Checkpoint
	firstCheckpoint.Seed.Session.SourceKind = "archived_session"
	firstBatch := IngestBatch{
		SourceFileID: previous.SourceFileID, Generation: cursor.Generation,
		PreviousCommittedOffset: cursor.Checkpoint.CommittedOffset,
		PreviousFingerprint:     &previous, Fingerprint: first,
		Diagnostics: []IngestDiagnostic{{
			Class: "syntax", Code: "bad_json", StartOffset: 70, EndOffset: 80, Retryable: true,
		}},
		Checkpoint: firstCheckpoint, EOF: true, AtMS: 30,
	}
	if _, err := repository.CommitIngestBatch(ctx, firstBatch); err != nil {
		t.Fatalf("CommitIngestBatch(first move) error = %v", err)
	}

	second := first
	second.CurrentPath = "/synthetic/archived/renamed-session-a.jsonl"
	secondBatch := firstBatch
	secondBatch.PreviousFingerprint = &first
	secondBatch.Fingerprint = second
	secondBatch.Diagnostics = nil
	secondBatch.AtMS = 40
	committed, err := repository.CommitIngestBatch(ctx, secondBatch)
	if err != nil {
		t.Fatalf("CommitIngestBatch(second move) error = %v", err)
	}
	if committed.Fingerprint != second {
		t.Fatalf("CommitIngestBatch(second move) = %#v, want renamed target", committed)
	}
	if replayed, err := repository.CommitIngestBatch(ctx, secondBatch); err != nil || replayed.Fingerprint != second {
		t.Fatalf("CommitIngestBatch(second move replay) = %#v, %v", replayed, err)
	}
	var firstDiagnostics []IngestDiagnostic
	err = repository.database.View(ctx, func(ctx context.Context, database *gorm.DB) error {
		var lookupErr error
		firstDiagnostics, lookupErr = ingestDiagnosticsForBatch(
			ctx, database, previous.SourceFileID, cursor.Generation,
			cursor.Checkpoint.CommittedOffset, ingestBatchIdentityDigest(firstBatch),
		)
		return lookupErr
	})
	if err != nil || len(firstDiagnostics) != 1 || firstDiagnostics[0] != firstBatch.Diagnostics[0] {
		t.Fatalf("ingestDiagnosticsForBatch(first move) = %#v, %v, want preserved diagnostic", firstDiagnostics, err)
	}
	third := first
	thirdBatch := secondBatch
	thirdBatch.PreviousFingerprint = &second
	thirdBatch.Fingerprint = third
	thirdBatch.AtMS = 50
	committed, err = repository.CommitIngestBatch(ctx, thirdBatch)
	if err != nil || committed.Fingerprint != third {
		t.Fatalf("CommitIngestBatch(return to first target) = %#v, %v", committed, err)
	}
	if replayed, err := repository.CommitIngestBatch(ctx, thirdBatch); err != nil || replayed.Fingerprint != third {
		t.Fatalf("CommitIngestBatch(return target replay) = %#v, %v", replayed, err)
	}
	if _, err := repository.CommitIngestBatch(ctx, firstBatch); !errors.Is(err, ErrInvalidRecord) {
		t.Fatalf("CommitIngestBatch(stale first move) error = %v, want ErrInvalidRecord", err)
	}
}

func TestCommitIngestBatchSupersedesSameSourceDependentOnMetadataAdvance(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	repository := openRuntimeRepository(t)
	active, _ := activateFaultFixture(t, repository)
	activeCursor, err := repository.GenerationCursor(ctx, active.SourceFileID, 0)
	if err != nil {
		t.Fatalf("GenerationCursor(active) error = %v", err)
	}
	building, err := repository.PrepareGeneration(ctx, PrepareGenerationRequest{
		Mode: GenerationModeRebuild, Previous: &active, Current: active,
		ParserVersion: "codex-rollout-v1", AtMS: 30,
	})
	if err != nil {
		t.Fatalf("PrepareGeneration(same-source dependent) error = %v", err)
	}
	session, _, checkpoint := testCommittedSessionTurn("session-a", "turn-building", building.Generation, 50)
	if _, err := repository.CommitIngestBatch(ctx, IngestBatch{
		SourceFileID: active.SourceFileID, Generation: building.Generation,
		Fingerprint: active, Facts: []FactBatch{{Session: &session}},
		Checkpoint: checkpoint, EOF: false, AtMS: 31,
	}); err != nil {
		t.Fatalf("CommitIngestBatch(stage same-source dependent) error = %v", err)
	}

	moved := active
	moved.SourceKind = "archived_session"
	moved.CurrentPath = "/synthetic/archived/session-a.jsonl"
	movedCheckpoint := activeCursor.Checkpoint
	movedCheckpoint.Seed.Session.SourceKind = "archived_session"
	if _, err := repository.CommitIngestBatch(ctx, IngestBatch{
		SourceFileID: active.SourceFileID, Generation: activeCursor.Generation,
		PreviousCommittedOffset: activeCursor.Checkpoint.CommittedOffset,
		PreviousFingerprint:     &active, Fingerprint: moved,
		Checkpoint: movedCheckpoint, EOF: true, AtMS: 40,
	}); err != nil {
		t.Fatalf("CommitIngestBatch(metadata advance) error = %v", err)
	}
	superseded, err := repository.GenerationCursor(ctx, building.SourceFileID, building.Generation)
	if err != nil || superseded.State != GenerationSuperseded {
		t.Fatalf("GenerationCursor(same-source dependent) = %#v, %v, want superseded", superseded, err)
	}
	assertGenerationBatchCount(t, repository, building.SourceFileID, building.Generation, 0)
	file, err := repository.SourceFile(ctx, active.SourceFileID)
	if err != nil || file.State != SourceFileActive || file.CurrentPath != moved.CurrentPath {
		t.Fatalf("SourceFile(active metadata target) = %#v, %v", file, err)
	}
}

func TestCommitIngestBatchSupersedesDependentsWhenActiveBaseAdvances(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	repository := openRuntimeRepository(t)
	active, _ := activateFaultFixture(t, repository)
	activeCursor, err := repository.GenerationCursor(ctx, active.SourceFileID, 0)
	if err != nil {
		t.Fatalf("GenerationCursor(active) error = %v", err)
	}
	replacedSourceID := active.SourceFileID
	buildingFingerprint := testSourceFingerprint("source-b", active.CurrentPath, 12, 80, 2_000)
	building, err := repository.PrepareGeneration(ctx, PrepareGenerationRequest{
		Mode: GenerationModeRebuild, Previous: &active, Current: buildingFingerprint,
		ParserVersion: "codex-rollout-v1", ReplacesSourceFileID: &replacedSourceID, AtMS: 30,
	})
	if err != nil {
		t.Fatalf("PrepareGeneration(dependent) error = %v", err)
	}
	buildingSession, _, buildingCheckpoint := testCommittedSessionTurn("session-b", "turn-b", 0, 80)
	if _, err := repository.CommitIngestBatch(ctx, IngestBatch{
		SourceFileID: building.SourceFileID, Generation: building.Generation,
		Fingerprint: buildingFingerprint, Facts: []FactBatch{{Session: &buildingSession}},
		Checkpoint: buildingCheckpoint, EOF: false, AtMS: 40,
	}); err != nil {
		t.Fatalf("CommitIngestBatch(stage dependent) error = %v", err)
	}

	advanced := testSourceFingerprint(active.SourceFileID, active.CurrentPath, active.Inode, 120, 3_000)
	advancedCheckpoint := activeCursor.Checkpoint
	advancedCheckpoint.CommittedOffset = 120
	if _, err := repository.CommitIngestBatch(ctx, IngestBatch{
		SourceFileID: active.SourceFileID, Generation: activeCursor.Generation,
		PreviousCommittedOffset: activeCursor.Checkpoint.CommittedOffset,
		PreviousFingerprint:     &active, Fingerprint: advanced,
		Checkpoint: advancedCheckpoint, EOF: true, AtMS: 50,
	}); err != nil {
		t.Fatalf("CommitIngestBatch(advance active base) error = %v", err)
	}
	dependent, err := repository.GenerationCursor(ctx, building.SourceFileID, building.Generation)
	if err != nil || dependent.State != GenerationSuperseded {
		t.Fatalf("GenerationCursor(dependent) = %#v, %v, want superseded", dependent, err)
	}
	assertGenerationBatchCount(t, repository, building.SourceFileID, building.Generation, 0)
	file, err := repository.SourceFile(ctx, building.SourceFileID)
	if err != nil || file.State != SourceFileUnavailable {
		t.Fatalf("SourceFile(dependent) = %#v, %v, want unavailable", file, err)
	}
}

func TestCommitIngestBatchSupersedesInitialSiblingBuilding(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	repository := openRuntimeRepository(t)
	path := "/synthetic/session-a.jsonl"
	fingerprintB := testSourceFingerprint("source-b", path, 12, 80, 1_000)
	fingerprintC := testSourceFingerprint("source-c", path, 13, 80, 2_000)
	buildingB, err := repository.PrepareGeneration(ctx, PrepareGenerationRequest{
		Mode: GenerationModeRebuild, Current: fingerprintB,
		ParserVersion: "codex-rollout-v1", AtMS: 10,
	})
	if err != nil {
		t.Fatalf("PrepareGeneration(initial B) error = %v", err)
	}
	buildingC, err := repository.PrepareGeneration(ctx, PrepareGenerationRequest{
		Mode: GenerationModeRebuild, Current: fingerprintC,
		ParserVersion: "codex-rollout-v1", AtMS: 11,
	})
	if err != nil {
		t.Fatalf("PrepareGeneration(initial C) error = %v", err)
	}
	sessionC, _, checkpointC := testCommittedSessionTurn("session-c", "turn-c", 0, 40)
	diagnosticC := IngestDiagnostic{
		Class: "syntax", Code: "bad_json", StartOffset: 30, EndOffset: 35, Retryable: true,
	}
	batchC := IngestBatch{
		SourceFileID: fingerprintC.SourceFileID, Generation: buildingC.Generation,
		Fingerprint: fingerprintC, Facts: []FactBatch{{Session: &sessionC}},
		Diagnostics: []IngestDiagnostic{diagnosticC}, Checkpoint: checkpointC, EOF: false, AtMS: 20,
	}
	if _, err := repository.CommitIngestBatch(ctx, batchC); err != nil {
		t.Fatalf("CommitIngestBatch(stage initial C) error = %v", err)
	}

	sessionB, turnB, checkpointB := testCommittedSessionTurn("session-b", "turn-b", 0, 80)
	if _, err := repository.CommitIngestBatch(ctx, IngestBatch{
		SourceFileID: fingerprintB.SourceFileID, Generation: buildingB.Generation,
		Fingerprint: fingerprintB, Facts: []FactBatch{{Session: &sessionB}, {Turn: &turnB}},
		Checkpoint: checkpointB, EOF: true, AtMS: 30,
	}); err != nil {
		t.Fatalf("CommitIngestBatch(activate initial B) error = %v", err)
	}
	cursorC, err := repository.GenerationCursor(ctx, fingerprintC.SourceFileID, buildingC.Generation)
	if err != nil || cursorC.State != GenerationSuperseded || cursorC.Checkpoint.CommittedOffset != 40 {
		t.Fatalf("GenerationCursor(initial C) = %#v, %v, want superseded offset 40", cursorC, err)
	}
	assertGenerationBatchCount(t, repository, fingerprintC.SourceFileID, buildingC.Generation, 0)
	var retainedDiagnostics []IngestDiagnostic
	err = repository.database.View(ctx, func(ctx context.Context, database *gorm.DB) error {
		var lookupErr error
		retainedDiagnostics, lookupErr = ingestDiagnosticsForBatch(
			ctx, database, fingerprintC.SourceFileID, buildingC.Generation,
			checkpointC.CommittedOffset, ingestBatchIdentityDigest(batchC),
		)
		return lookupErr
	})
	if err != nil || len(retainedDiagnostics) != 1 || retainedDiagnostics[0] != diagnosticC {
		t.Fatalf("ingestDiagnosticsForBatch(initial C) = %#v, %v, want retained diagnostic", retainedDiagnostics, err)
	}
	fileC, err := repository.SourceFile(ctx, fingerprintC.SourceFileID)
	if err != nil || fileC.State != SourceFileUnavailable {
		t.Fatalf("SourceFile(initial C) = %#v, %v, want unavailable", fileC, err)
	}
	if _, err := repository.Session(ctx, "session-c"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Session(initial C) error = %v, want ErrNotFound", err)
	}
}

func TestCommitIngestBatchRejectsCanonicalSessionSourceKindDrift(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	repository := openRuntimeRepository(t)
	previous, _ := activateFaultFixture(t, repository)
	cursor, err := repository.GenerationCursor(ctx, previous.SourceFileID, 0)
	if err != nil {
		t.Fatalf("GenerationCursor(active) error = %v", err)
	}
	moved := previous
	moved.SourceKind = "archived_session"
	moved.CurrentPath = "/synthetic/archived/session-a.jsonl"
	checkpoint := cursor.Checkpoint
	checkpoint.Seed.Session.SourceKind = "archived_session"
	checkpoint.Projector.SessionSourceKind = "archived_session"
	_, err = repository.CommitIngestBatch(ctx, IngestBatch{
		SourceFileID: previous.SourceFileID, Generation: cursor.Generation,
		PreviousCommittedOffset: cursor.Checkpoint.CommittedOffset,
		PreviousFingerprint:     &previous, Fingerprint: moved,
		Checkpoint: checkpoint, EOF: true, AtMS: 30,
	})
	if !errors.Is(err, ErrInvalidRecord) {
		t.Fatalf("CommitIngestBatch(canonical source kind drift) error = %v, want ErrInvalidRecord", err)
	}
}

func TestCommitIngestBatchRejectsSessionFactSourceKindMismatch(t *testing.T) {
	t.Parallel()

	repository := openRuntimeRepository(t)
	fingerprint := testSourceFingerprint("source-a", "/synthetic/session-a.jsonl", 11, 100, 1_000)
	cursor, err := repository.PrepareGeneration(context.Background(), PrepareGenerationRequest{
		Mode: GenerationModeRebuild, Current: fingerprint,
		ParserVersion: "codex-rollout-v1", AtMS: 10,
	})
	if err != nil {
		t.Fatalf("PrepareGeneration() error = %v", err)
	}
	session, _, checkpoint := testCommittedSessionTurn("session-a", "turn-a", 0, 100)
	session.SourceKind = "archived_session"
	_, err = repository.CommitIngestBatch(context.Background(), IngestBatch{
		SourceFileID: fingerprint.SourceFileID, Generation: cursor.Generation,
		Fingerprint: fingerprint, Facts: []FactBatch{{Session: &session}},
		Checkpoint: checkpoint, EOF: true, AtMS: 20,
	})
	if !errors.Is(err, ErrInvalidRecord) {
		t.Fatalf("CommitIngestBatch(source kind mismatch) error = %v, want ErrInvalidRecord", err)
	}
}

func TestCommitIngestBatchRejectsSessionFactWithoutCheckpointSession(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	repository := openRuntimeRepository(t)
	fingerprint := testSourceFingerprint("source-a", "/synthetic/session-a.jsonl", 11, 0, 1_000)
	cursor, err := repository.PrepareGeneration(ctx, PrepareGenerationRequest{
		Mode: GenerationModeRebuild, Current: fingerprint,
		ParserVersion: "codex-rollout-v1", AtMS: 10,
	})
	if err != nil {
		t.Fatalf("PrepareGeneration() error = %v", err)
	}
	session := Session{
		SessionID: "session-a", Provider: "codex", SourceKind: "session",
		CreatedAtMS: 1_000, FirstSeenAtMS: 1_000, LastSeenAtMS: 1_000,
	}
	_, err = repository.CommitIngestBatch(ctx, IngestBatch{
		SourceFileID: fingerprint.SourceFileID, Generation: cursor.Generation,
		Fingerprint: fingerprint, Facts: []FactBatch{{Session: &session}},
		Checkpoint: cursor.Checkpoint, EOF: true, AtMS: 20,
	})
	if !errors.Is(err, ErrInvalidRecord) {
		t.Fatalf("CommitIngestBatch(session fact without checkpoint session) error = %v, want ErrInvalidRecord", err)
	}
}

func TestCommitIngestBatchRejectsSessionIdentityDriftWithinGeneration(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	repository := openRuntimeRepository(t)
	fingerprint := testSourceFingerprint("source-a", "/synthetic/session-a.jsonl", 11, 100, 1_000)
	cursor, err := repository.PrepareGeneration(ctx, PrepareGenerationRequest{
		Mode: GenerationModeRebuild, Current: fingerprint,
		ParserVersion: "codex-rollout-v1", AtMS: 10,
	})
	if err != nil {
		t.Fatalf("PrepareGeneration() error = %v", err)
	}
	sessionA, _, checkpointA := testCommittedSessionTurn("session-a", "turn-a", cursor.Generation, 50)
	if _, err := repository.CommitIngestBatch(ctx, IngestBatch{
		SourceFileID: fingerprint.SourceFileID, Generation: cursor.Generation,
		Fingerprint: fingerprint, Facts: []FactBatch{{Session: &sessionA}},
		Checkpoint: checkpointA, EOF: false, AtMS: 20,
	}); err != nil {
		t.Fatalf("CommitIngestBatch(stage session A) error = %v", err)
	}
	sessionB, _, checkpointB := testCommittedSessionTurn("session-b", "turn-b", cursor.Generation, 100)
	_, err = repository.CommitIngestBatch(ctx, IngestBatch{
		SourceFileID: fingerprint.SourceFileID, Generation: cursor.Generation,
		PreviousCommittedOffset: 50, Fingerprint: fingerprint,
		Facts: []FactBatch{{Session: &sessionB}}, Checkpoint: checkpointB,
		EOF: true, AtMS: 30,
	})
	if !errors.Is(err, ErrInvalidRecord) {
		t.Fatalf("CommitIngestBatch(session identity drift) error = %v, want ErrInvalidRecord", err)
	}
	stored, err := repository.GenerationCursor(ctx, cursor.SourceFileID, cursor.Generation)
	if err != nil || checkpointSessionID(stored.Checkpoint) != "session-a" ||
		stored.Checkpoint.CommittedOffset != 50 || stored.State != GenerationBuilding {
		t.Fatalf("GenerationCursor(after session drift) = %#v, %v", stored, err)
	}
}

func TestCommitIngestBatchRejectsUsageOnlyFactFromAnotherSession(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	repository := openRuntimeRepository(t)
	fingerprintB := testSourceFingerprint("source-b", "/synthetic/session-b.jsonl", 12, 100, 1_000)
	cursorB, err := repository.PrepareGeneration(ctx, PrepareGenerationRequest{
		Mode: GenerationModeRebuild, Current: fingerprintB,
		ParserVersion: "codex-rollout-v1", AtMS: 10,
	})
	if err != nil {
		t.Fatalf("PrepareGeneration(session B) error = %v", err)
	}
	sessionB, turnB, checkpointB := testCommittedSessionTurn("session-b", "turn-b", cursorB.Generation, 100)
	baselineInput := int64(10)
	baselineUsage := TurnUsage{
		TurnID: turnB.TurnID, ObservedAtMS: 1_900, IsFinal: true,
		InputTokens: &baselineInput, SourceGeneration: cursorB.Generation,
		SourceOffset: 90, Confidence: "observed", UpdatedAtMS: 1_900,
	}
	if _, err := repository.CommitIngestBatch(ctx, IngestBatch{
		SourceFileID: fingerprintB.SourceFileID, Generation: cursorB.Generation,
		Fingerprint: fingerprintB,
		Facts:       []FactBatch{{Session: &sessionB}, {Turn: &turnB, Usage: &baselineUsage}},
		Checkpoint:  checkpointB, EOF: true, AtMS: 20,
	}); err != nil {
		t.Fatalf("CommitIngestBatch(session B) error = %v", err)
	}

	fingerprintA := testSourceFingerprint("source-a", "/synthetic/session-a.jsonl", 11, 100, 1_000)
	cursorA, err := repository.PrepareGeneration(ctx, PrepareGenerationRequest{
		Mode: GenerationModeRebuild, Current: fingerprintA,
		ParserVersion: "codex-rollout-v1", AtMS: 30,
	})
	if err != nil {
		t.Fatalf("PrepareGeneration(session A) error = %v", err)
	}
	sessionA, _, checkpointA := testCommittedSessionTurn("session-a", "turn-a", cursorA.Generation, 100)
	crossSessionInput := int64(20)
	crossSessionUsage := TurnUsage{
		TurnID: turnB.TurnID, ObservedAtMS: 2_100, IsFinal: true,
		InputTokens: &crossSessionInput, SourceGeneration: cursorA.Generation,
		SourceOffset: 95, Confidence: "observed", UpdatedAtMS: 2_100,
	}
	_, err = repository.CommitIngestBatch(ctx, IngestBatch{
		SourceFileID: fingerprintA.SourceFileID, Generation: cursorA.Generation,
		Fingerprint: fingerprintA,
		Facts:       []FactBatch{{Session: &sessionA}, {Usage: &crossSessionUsage}},
		Checkpoint:  checkpointA, EOF: true, AtMS: 40,
	})
	if !errors.Is(err, ErrInvalidRecord) {
		t.Fatalf("CommitIngestBatch(cross-session usage-only fact) error = %v, want ErrInvalidRecord", err)
	}

	storedA, err := repository.GenerationCursor(ctx, cursorA.SourceFileID, cursorA.Generation)
	if err != nil || storedA.State != GenerationBuilding || storedA.Checkpoint.CommittedOffset != 0 {
		t.Fatalf("GenerationCursor(session A after rejection) = %#v, %v, want unchanged building offset 0", storedA, err)
	}
	assertGenerationBatchCount(t, repository, cursorA.SourceFileID, cursorA.Generation, 0)
	if _, err := repository.Session(ctx, sessionA.SessionID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Session(session A after rejection) error = %v, want ErrNotFound", err)
	}
	storedB, err := repository.Turn(ctx, turnB.TurnID)
	if err != nil || storedB.Usage == nil || storedB.Usage.SourceOffset != baselineUsage.SourceOffset ||
		storedB.Usage.InputTokens == nil || *storedB.Usage.InputTokens != baselineInput {
		t.Fatalf("Turn(session B after rejection) = %#v, %v, want unchanged baseline usage", storedB, err)
	}
}

func TestCommitIngestBatchRejectsDistinctBuildingCommitAtSameTime(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	repository := openRuntimeRepository(t)
	fingerprint := testSourceFingerprint("source-a", "/synthetic/session-a.jsonl", 11, 100, 1_000)
	cursor, err := repository.PrepareGeneration(ctx, PrepareGenerationRequest{
		Mode: GenerationModeRebuild, Current: fingerprint,
		ParserVersion: "codex-rollout-v1", AtMS: 10,
	})
	if err != nil {
		t.Fatalf("PrepareGeneration() error = %v", err)
	}
	session, _, checkpoint50 := testCommittedSessionTurn("session-a", "turn-a", cursor.Generation, 50)
	if _, err := repository.CommitIngestBatch(ctx, IngestBatch{
		SourceFileID: fingerprint.SourceFileID, Generation: cursor.Generation,
		Fingerprint: fingerprint, Facts: []FactBatch{{Session: &session}},
		Checkpoint: checkpoint50, EOF: false, AtMS: 20,
	}); err != nil {
		t.Fatalf("CommitIngestBatch(first at time 20) error = %v", err)
	}
	checkpoint100 := checkpoint50
	checkpoint100.CommittedOffset = 100
	_, err = repository.CommitIngestBatch(ctx, IngestBatch{
		SourceFileID: fingerprint.SourceFileID, Generation: cursor.Generation,
		PreviousCommittedOffset: 50, Fingerprint: fingerprint,
		Checkpoint: checkpoint100, EOF: true, AtMS: 20,
	})
	if !errors.Is(err, ErrInvalidRecord) {
		t.Fatalf("CommitIngestBatch(distinct same-time commit) error = %v, want ErrInvalidRecord", err)
	}
}

func TestCommitIngestBatchRejectsFactAheadOfCheckpoint(t *testing.T) {
	t.Parallel()

	repository := openRuntimeRepository(t)
	fingerprint := testSourceFingerprint("source-a", "/synthetic/session-a.jsonl", 11, 100, 1_000)
	cursor, err := repository.PrepareGeneration(context.Background(), PrepareGenerationRequest{
		Mode: GenerationModeRebuild, Current: fingerprint,
		ParserVersion: "codex-rollout-v1", AtMS: 10,
	})
	if err != nil {
		t.Fatalf("PrepareGeneration() error = %v", err)
	}
	session, turn, checkpoint := testCommittedSessionTurn("session-a", "turn-a", 0, 100)
	turn.StartOffset = 101
	_, err = repository.CommitIngestBatch(context.Background(), IngestBatch{
		SourceFileID: fingerprint.SourceFileID, Generation: cursor.Generation,
		PreviousCommittedOffset: 0, Fingerprint: fingerprint,
		Facts: []FactBatch{{Session: &session}, {Turn: &turn}}, Checkpoint: checkpoint,
		EOF: true, AtMS: 20,
	})
	if !errors.Is(err, ErrInvalidRecord) {
		t.Fatalf("CommitIngestBatch(fact ahead) error = %v, want ErrInvalidRecord", err)
	}
	stored, err := repository.GenerationCursor(context.Background(), fingerprint.SourceFileID, 0)
	if err != nil || stored.Checkpoint.CommittedOffset != 0 || stored.State != GenerationBuilding {
		t.Fatalf("GenerationCursor() = %#v, %v, want unchanged building offset 0", stored, err)
	}
}

func testCommittedSessionTurn(
	sessionID string,
	turnID string,
	generation int64,
	offset int64,
) (Session, Turn, ParserCheckpoint) {
	completedAt := int64(2_000)
	outcome := "completed"
	completeOffset := offset - 10
	session := Session{
		SessionID: sessionID, Provider: "codex", SourceKind: "session",
		CreatedAtMS: 1_000, FirstSeenAtMS: 1_000, LastSeenAtMS: 2_000,
	}
	turn := Turn{
		TurnID: turnID, SessionID: sessionID, StartedAtMS: 1_100,
		CompletedAtMS: &completedAt, Outcome: &outcome, SourceGeneration: generation,
		StartOffset: 10, CompleteOffset: &completeOffset,
	}
	checkpoint := ParserCheckpoint{
		Version: ParserCheckpointVersion, ParserVersion: "codex-rollout-v1", CommittedOffset: offset,
		Seed: &ParserSeedCheckpoint{
			Session: &CheckpointSessionMeta{
				SessionID: sessionID, RootSessionID: sessionID, SourceKind: "session",
				CreatedAtMS: 1_000, ObservedAtMS: 1_000, InitialCWD: "/synthetic",
				Originator: "codex-cli", CLIVersion: "0.142.3", Source: "cli",
			},
			ClosedTurns: []CheckpointClosedTurn{{
				TurnID: turnID, StartedAtMS: 1_100,
				Terminal: CheckpointTurnEnd{
					SessionID: sessionID, TurnID: turnID, CompletedAtMS: completedAt, Outcome: outcome,
				},
			}},
		},
		Projector: ProjectorCheckpoint{SessionSourceKind: "session", Current: &SessionCurrent{
			SessionID: sessionID, LastActivityAtMS: &completedAt, UpdatedAtMS: completedAt,
		}},
	}
	return session, turn, checkpoint
}

func assertGenerationBatchCount(
	t *testing.T,
	repository *Repository,
	sourceFileID string,
	generation int64,
	want int64,
) {
	t.Helper()
	var count int64
	err := repository.database.View(context.Background(), func(ctx context.Context, database *gorm.DB) error {
		return database.WithContext(ctx).Model(&sourceGenerationBatchModel{}).
			Where("source_file_id = ? AND generation = ?", sourceFileID, generation).Count(&count).Error
	})
	if err != nil || count != want {
		t.Fatalf("source_generation_batches(%s, %d) count = %d, %v, want %d", sourceFileID, generation, count, err, want)
	}
}
