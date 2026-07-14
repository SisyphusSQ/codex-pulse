package store

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestPrepareGenerationCreatesAndReplaysInitialBuildingCheckpoint(t *testing.T) {
	t.Parallel()

	repository := openRuntimeRepository(t)
	fingerprint := testSourceFingerprint("source-a", "/synthetic/session-a.jsonl", 11, 120, 1_000)
	request := PrepareGenerationRequest{
		Mode: GenerationModeRebuild, Current: fingerprint,
		ParserVersion: "codex-rollout-v1", AtMS: 10,
	}
	first, err := repository.PrepareGeneration(context.Background(), request)
	if err != nil {
		t.Fatalf("PrepareGeneration() error = %v", err)
	}
	if first.SourceFileID != fingerprint.SourceFileID || first.Generation != 0 ||
		first.State != GenerationBuilding || !reflect.DeepEqual(first.Fingerprint, fingerprint) ||
		first.Checkpoint.Version != ParserCheckpointVersion ||
		first.Checkpoint.ParserVersion != request.ParserVersion ||
		first.Checkpoint.CommittedOffset != 0 || first.Checkpoint.Seed != nil || first.Base != nil {
		t.Fatalf("PrepareGeneration() = %#v, want generation 0 building at offset 0", first)
	}

	replayed, err := repository.PrepareGeneration(context.Background(), request)
	if err != nil {
		t.Fatalf("PrepareGeneration(replay) error = %v", err)
	}
	if !reflect.DeepEqual(replayed, first) {
		t.Fatalf("PrepareGeneration(replay) = %#v, want %#v", replayed, first)
	}

	stored, err := repository.GenerationCursor(context.Background(), fingerprint.SourceFileID, 0)
	if err != nil {
		t.Fatalf("GenerationCursor() error = %v", err)
	}
	if !reflect.DeepEqual(stored, first) {
		t.Fatalf("GenerationCursor() = %#v, want %#v", stored, first)
	}
	file, err := repository.SourceFile(context.Background(), fingerprint.SourceFileID)
	if err != nil {
		t.Fatalf("SourceFile() error = %v", err)
	}
	if file.State != SourceFileDiscovered || file.ParsedOffset != 0 || file.ActiveGeneration != 0 ||
		file.CurrentPath != fingerprint.CurrentPath || file.SizeBytes != fingerprint.SizeBytes {
		t.Fatalf("SourceFile() = %#v, want discovered generation 0", file)
	}
}

func TestPrepareGenerationRejectsAppendWithoutActiveCheckpoint(t *testing.T) {
	t.Parallel()

	repository := openRuntimeRepository(t)
	fingerprint := testSourceFingerprint("source-a", "/synthetic/session-a.jsonl", 11, 120, 1_000)
	_, err := repository.PrepareGeneration(context.Background(), PrepareGenerationRequest{
		Mode: GenerationModeAppend, Current: fingerprint,
		ParserVersion: "codex-rollout-v1", AtMS: 10,
	})
	if !errors.Is(err, ErrInvalidRecord) {
		t.Fatalf("PrepareGeneration(append without active) error = %v, want ErrInvalidRecord", err)
	}
	invalid := fingerprint
	invalid.FingerprintSHA256 = strings.Repeat("0", 64)
	_, err = repository.PrepareGeneration(context.Background(), PrepareGenerationRequest{
		Mode: GenerationModeRebuild, Current: invalid,
		ParserVersion: "codex-rollout-v1", AtMS: 10,
	})
	if !errors.Is(err, ErrInvalidRecord) {
		t.Fatalf("PrepareGeneration(invalid digest) error = %v, want ErrInvalidRecord", err)
	}
}

func TestCodexSnapshotsExposeOnlyActivatedGenerations(t *testing.T) {
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
	if snapshots, err := repository.CodexSnapshots(context.Background()); err != nil || len(snapshots) != 0 {
		t.Fatalf("CodexSnapshots(building) = %#v, %v, want no active snapshot", snapshots, err)
	}
	session, turn, checkpoint := testCommittedSessionTurn("session-a", "turn-a", 0, 100)
	if _, err := repository.CommitIngestBatch(context.Background(), IngestBatch{
		SourceFileID: fingerprint.SourceFileID, Generation: cursor.Generation,
		PreviousCommittedOffset: 0, Fingerprint: fingerprint,
		Facts: []FactBatch{{Session: &session}, {Turn: &turn}}, Checkpoint: checkpoint,
		EOF: true, AtMS: 20,
	}); err != nil {
		t.Fatalf("CommitIngestBatch() error = %v", err)
	}
	snapshots, err := repository.CodexSnapshots(context.Background())
	if err != nil || len(snapshots) != 1 || !reflect.DeepEqual(snapshots[0], fingerprint) {
		t.Fatalf("CodexSnapshots(active) = %#v, %v, want %#v", snapshots, err, fingerprint)
	}
}

func TestPrepareGenerationCoversAppendRebuildUpgradeAndConflictMatrix(t *testing.T) {
	t.Parallel()

	t.Run("append and stale conflict", func(t *testing.T) {
		repository := openRuntimeRepository(t)
		previous, _ := activateFaultFixture(t, repository)
		current := testSourceFingerprint(previous.SourceFileID, previous.CurrentPath, previous.Inode, 120, 2_000)
		cursor, err := repository.PrepareGeneration(context.Background(), PrepareGenerationRequest{
			Mode: GenerationModeAppend, Previous: &previous, Current: current,
			ParserVersion: "codex-rollout-v1", AtMS: 30,
		})
		if err != nil || cursor.State != GenerationActive || cursor.Generation != 0 || cursor.Fingerprint != current {
			t.Fatalf("PrepareGeneration(append) = %#v, %v, want active generation 0 target", cursor, err)
		}
		stale := previous
		stale.MTimeNS--
		stale.FingerprintSHA256 = sourceFingerprintDigest(stale)
		_, err = repository.PrepareGeneration(context.Background(), PrepareGenerationRequest{
			Mode: GenerationModeAppend, Previous: &stale, Current: current,
			ParserVersion: "codex-rollout-v1", AtMS: 31,
		})
		if !errors.Is(err, ErrInvalidRecord) {
			t.Fatalf("PrepareGeneration(stale append) error = %v, want ErrInvalidRecord", err)
		}
	})

	t.Run("same identity truncate and replay", func(t *testing.T) {
		repository := openRuntimeRepository(t)
		previous, _ := activateFaultFixture(t, repository)
		current := testSourceFingerprint(previous.SourceFileID, previous.CurrentPath, previous.Inode, 80, 2_000)
		request := PrepareGenerationRequest{
			Mode: GenerationModeRebuild, Previous: &previous, Current: current,
			ParserVersion: "codex-rollout-v1", AtMS: 30,
		}
		cursor, err := repository.PrepareGeneration(context.Background(), request)
		if err != nil || cursor.State != GenerationBuilding || cursor.Generation != 1 ||
			cursor.Base == nil || cursor.Base.SourceFileID != previous.SourceFileID ||
			cursor.Base.Generation != 0 || cursor.Base.FingerprintSHA256 != previous.FingerprintSHA256 {
			t.Fatalf("PrepareGeneration(truncate) = %#v, %v, want building generation 1", cursor, err)
		}
		replayed, err := repository.PrepareGeneration(context.Background(), request)
		if err != nil || !reflect.DeepEqual(replayed, cursor) {
			t.Fatalf("PrepareGeneration(replay) = %#v, %v, want %#v", replayed, err, cursor)
		}
		conflict := request
		conflict.Current.MTimeNS++
		conflict.Current.FingerprintSHA256 = sourceFingerprintDigest(conflict.Current)
		if _, err := repository.PrepareGeneration(context.Background(), conflict); !errors.Is(err, ErrInvalidRecord) {
			t.Fatalf("PrepareGeneration(conflicting building) error = %v, want ErrInvalidRecord", err)
		}
	})

	t.Run("parser upgrade", func(t *testing.T) {
		repository := openRuntimeRepository(t)
		fingerprint := testSourceFingerprint("source-a", "/synthetic/session-a.jsonl", 11, 100, 1_000)
		initial, err := repository.PrepareGeneration(context.Background(), PrepareGenerationRequest{
			Mode: GenerationModeRebuild, Current: fingerprint, ParserVersion: "rollout-v0", AtMS: 10,
		})
		if err != nil {
			t.Fatalf("PrepareGeneration(old parser) error = %v", err)
		}
		session, turn, checkpoint := testCommittedSessionTurn("session-a", "turn-old", 0, 100)
		checkpoint.ParserVersion = "rollout-v0"
		if _, err := repository.CommitIngestBatch(context.Background(), IngestBatch{
			SourceFileID: fingerprint.SourceFileID, Generation: initial.Generation,
			PreviousCommittedOffset: 0, Fingerprint: fingerprint,
			Facts: []FactBatch{{Session: &session}, {Turn: &turn}}, Checkpoint: checkpoint,
			EOF: true, AtMS: 20,
		}); err != nil {
			t.Fatalf("CommitIngestBatch(old parser) error = %v", err)
		}
		upgraded, err := repository.PrepareGeneration(context.Background(), PrepareGenerationRequest{
			Mode: GenerationModeRebuild, Previous: &fingerprint, Current: fingerprint,
			ParserVersion: "codex-rollout-v1", AtMS: 30,
		})
		if err != nil || upgraded.State != GenerationBuilding || upgraded.Generation != 1 ||
			upgraded.ParserVersion != "codex-rollout-v1" {
			t.Fatalf("PrepareGeneration(upgrade) = %#v, %v, want building generation 1", upgraded, err)
		}
	})
}

func TestPrepareGenerationReplacesNewPhysicalIdentityAtEOF(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	repository := openRuntimeRepository(t)
	previous, _ := activateFaultFixture(t, repository)
	current := testSourceFingerprint("source-b", previous.CurrentPath, 12, 80, 2_000)
	replacedID := previous.SourceFileID
	cursor, err := repository.PrepareGeneration(ctx, PrepareGenerationRequest{
		Mode: GenerationModeRebuild, Previous: &previous, Current: current,
		ParserVersion: "codex-rollout-v1", ReplacesSourceFileID: &replacedID, AtMS: 30,
	})
	if err != nil || cursor.Generation != 0 || cursor.State != GenerationBuilding ||
		cursor.Base == nil || cursor.Base.SourceFileID != previous.SourceFileID ||
		cursor.Base.Generation != 0 || cursor.Base.FingerprintSHA256 != previous.FingerprintSHA256 {
		t.Fatalf("PrepareGeneration(new identity) = %#v, %v, want building generation 0", cursor, err)
	}
	session, turn, checkpoint := testCommittedSessionTurn("session-a", "turn-new", 0, 80)
	if _, err := repository.CommitIngestBatch(ctx, IngestBatch{
		SourceFileID: current.SourceFileID, Generation: cursor.Generation,
		PreviousCommittedOffset: 0, Fingerprint: current,
		Facts: []FactBatch{{Session: &session}, {Turn: &turn}}, Checkpoint: checkpoint,
		EOF: true, AtMS: 40,
	}); err != nil {
		t.Fatalf("CommitIngestBatch(new identity) error = %v", err)
	}
	oldFile, err := repository.SourceFile(ctx, previous.SourceFileID)
	if err != nil || oldFile.State != SourceFileUnavailable {
		t.Fatalf("SourceFile(old) = %#v, %v, want unavailable", oldFile, err)
	}
	snapshots, err := repository.CodexSnapshots(ctx)
	if err != nil || len(snapshots) != 1 || snapshots[0] != current {
		t.Fatalf("CodexSnapshots() = %#v, %v, want only replacement", snapshots, err)
	}
	if _, err := repository.Turn(ctx, "turn-old"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Turn(old) error = %v, want ErrNotFound", err)
	}
}

func TestPrepareGenerationSupersedesDriftedBuildingWithCAS(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	repository := openRuntimeRepository(t)
	initialFingerprint := testSourceFingerprint("source-a", "/synthetic/session-a.jsonl", 11, 50, 1_000)
	initial, err := repository.PrepareGeneration(ctx, PrepareGenerationRequest{
		Mode: GenerationModeRebuild, Current: initialFingerprint,
		ParserVersion: "rollout-v0", AtMS: 10,
	})
	if err != nil {
		t.Fatalf("PrepareGeneration(initial building) error = %v", err)
	}

	grownFingerprint := testSourceFingerprint("source-a", initialFingerprint.CurrentPath, 11, 80, 2_000)
	grown, err := repository.PrepareGeneration(ctx, PrepareGenerationRequest{
		Mode: GenerationModeAppend, Previous: &initialFingerprint, Current: grownFingerprint,
		ParserVersion: "rollout-v0", SupersedeBuilding: buildingExpectation(initial), AtMS: 20,
	})
	if err != nil {
		t.Fatalf("PrepareGeneration(grown building) error = %v", err)
	}
	if grown.State != GenerationBuilding || grown.Generation != 1 || grown.Checkpoint.CommittedOffset != 0 ||
		grown.SupersededBuilding == nil || grown.SupersededBuilding.SourceFileID != initial.SourceFileID ||
		grown.SupersededBuilding.Generation != initial.Generation {
		t.Fatalf("PrepareGeneration(grown building) = %#v, want restarted generation 1", grown)
	}
	old, err := repository.GenerationCursor(ctx, initial.SourceFileID, initial.Generation)
	if err != nil || old.State != GenerationSuperseded {
		t.Fatalf("GenerationCursor(old building) = %#v, %v, want superseded", old, err)
	}

	parserUpgraded, err := repository.PrepareGeneration(ctx, PrepareGenerationRequest{
		Mode: GenerationModeRebuild, Previous: &grownFingerprint, Current: grownFingerprint,
		ParserVersion: "codex-rollout-v1", SupersedeBuilding: buildingExpectation(grown), AtMS: 30,
	})
	if err != nil {
		t.Fatalf("PrepareGeneration(parser drift) error = %v", err)
	}
	if parserUpgraded.Generation != 2 || parserUpgraded.ParserVersion != "codex-rollout-v1" ||
		parserUpgraded.SupersededBuilding == nil || parserUpgraded.SupersededBuilding.Generation != 1 {
		t.Fatalf("PrepareGeneration(parser drift) = %#v, want restarted generation 2", parserUpgraded)
	}

	newerFingerprint := testSourceFingerprint("source-a", initialFingerprint.CurrentPath, 11, 100, 3_000)
	_, err = repository.PrepareGeneration(ctx, PrepareGenerationRequest{
		Mode: GenerationModeAppend, Previous: &grownFingerprint, Current: newerFingerprint,
		ParserVersion: "codex-rollout-v1", SupersedeBuilding: buildingExpectation(grown), AtMS: 40,
	})
	if !errors.Is(err, ErrInvalidRecord) {
		t.Fatalf("PrepareGeneration(stale building CAS) error = %v, want ErrInvalidRecord", err)
	}
	current, err := repository.GenerationCursor(ctx, parserUpgraded.SourceFileID, parserUpgraded.Generation)
	if err != nil || current.State != GenerationBuilding || current.Generation != 2 {
		t.Fatalf("GenerationCursor(after stale CAS) = %#v, %v, want building generation 2", current, err)
	}
}

func TestPrepareGenerationSupersedesBuildingAcrossPhysicalReplacement(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	repository := openRuntimeRepository(t)
	previous := testSourceFingerprint("source-a", "/synthetic/session-a.jsonl", 11, 80, 1_000)
	initial, err := repository.PrepareGeneration(ctx, PrepareGenerationRequest{
		Mode: GenerationModeRebuild, Current: previous,
		ParserVersion: "codex-rollout-v1", AtMS: 10,
	})
	if err != nil {
		t.Fatalf("PrepareGeneration(initial building) error = %v", err)
	}
	current := testSourceFingerprint("source-b", previous.CurrentPath, 12, 80, 2_000)
	replacedSourceID := previous.SourceFileID
	replacement, err := repository.PrepareGeneration(ctx, PrepareGenerationRequest{
		Mode: GenerationModeRebuild, Previous: &previous, Current: current,
		ParserVersion: "codex-rollout-v1", ReplacesSourceFileID: &replacedSourceID,
		SupersedeBuilding: buildingExpectation(initial), AtMS: 20,
	})
	if err != nil {
		t.Fatalf("PrepareGeneration(physical replacement building) error = %v", err)
	}
	if replacement.SourceFileID != current.SourceFileID || replacement.Generation != 0 ||
		replacement.Base != nil || replacement.SupersededBuilding == nil ||
		replacement.SupersededBuilding.SourceFileID != previous.SourceFileID {
		t.Fatalf("PrepareGeneration(physical replacement building) = %#v", replacement)
	}
	session, turn, checkpoint := testCommittedSessionTurn("session-b", "turn-b", 0, 80)
	if _, err := repository.CommitIngestBatch(ctx, IngestBatch{
		SourceFileID: current.SourceFileID, Generation: replacement.Generation,
		Fingerprint: current, Facts: []FactBatch{{Session: &session}, {Turn: &turn}},
		Checkpoint: checkpoint, EOF: true, AtMS: 30,
	}); err != nil {
		t.Fatalf("CommitIngestBatch(physical replacement building) error = %v", err)
	}
	oldFile, err := repository.SourceFile(ctx, previous.SourceFileID)
	if err != nil || oldFile.State != SourceFileUnavailable {
		t.Fatalf("SourceFile(superseded building) = %#v, %v, want unavailable", oldFile, err)
	}
}

func TestPrepareGenerationSupersedesInitialPhysicalReplacementChain(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	repository := openRuntimeRepository(t)
	fingerprintA := testSourceFingerprint("source-a", "/synthetic/session-a.jsonl", 11, 80, 1_000)
	buildingA, err := repository.PrepareGeneration(ctx, PrepareGenerationRequest{
		Mode: GenerationModeRebuild, Current: fingerprintA,
		ParserVersion: "codex-rollout-v1", AtMS: 10,
	})
	if err != nil {
		t.Fatalf("PrepareGeneration(building A) error = %v", err)
	}
	fingerprintB := testSourceFingerprint("source-b", fingerprintA.CurrentPath, 12, 80, 2_000)
	replacesA := fingerprintA.SourceFileID
	buildingB, err := repository.PrepareGeneration(ctx, PrepareGenerationRequest{
		Mode: GenerationModeRebuild, Previous: &fingerprintA, Current: fingerprintB,
		ParserVersion: "codex-rollout-v1", ReplacesSourceFileID: &replacesA,
		SupersedeBuilding: buildingExpectation(buildingA), AtMS: 20,
	})
	if err != nil {
		t.Fatalf("PrepareGeneration(building B) error = %v", err)
	}
	fingerprintC := testSourceFingerprint("source-c", fingerprintA.CurrentPath, 13, 80, 3_000)
	replacesB := fingerprintB.SourceFileID
	buildingC, err := repository.PrepareGeneration(ctx, PrepareGenerationRequest{
		Mode: GenerationModeRebuild, Previous: &fingerprintB, Current: fingerprintC,
		ParserVersion: "codex-rollout-v1", ReplacesSourceFileID: &replacesB,
		SupersedeBuilding: buildingExpectation(buildingB), AtMS: 30,
	})
	if err != nil {
		t.Fatalf("PrepareGeneration(building C) error = %v", err)
	}
	if buildingC.Base != nil || buildingC.SupersededBuilding == nil ||
		buildingC.SupersededBuilding.SourceFileID != fingerprintB.SourceFileID {
		t.Fatalf("PrepareGeneration(building C) = %#v, want base-less B predecessor", buildingC)
	}
	session, turn, checkpoint := testCommittedSessionTurn("session-c", "turn-c", 0, 80)
	if _, err := repository.CommitIngestBatch(ctx, IngestBatch{
		SourceFileID: fingerprintC.SourceFileID, Generation: buildingC.Generation,
		Fingerprint: fingerprintC, Facts: []FactBatch{{Session: &session}, {Turn: &turn}},
		Checkpoint: checkpoint, EOF: true, AtMS: 40,
	}); err != nil {
		t.Fatalf("CommitIngestBatch(building C) error = %v", err)
	}
	for _, sourceFileID := range []string{fingerprintA.SourceFileID, fingerprintB.SourceFileID} {
		file, err := repository.SourceFile(ctx, sourceFileID)
		if err != nil || file.State != SourceFileUnavailable {
			t.Fatalf("SourceFile(%s) = %#v, %v, want unavailable", sourceFileID, file, err)
		}
	}
}

func buildingExpectation(cursor GenerationCursor) *BuildingGenerationExpectation {
	return &BuildingGenerationExpectation{
		SourceFileID: cursor.SourceFileID, Generation: cursor.Generation,
		FingerprintSHA256: cursor.Fingerprint.FingerprintSHA256,
		ParserVersion:     cursor.ParserVersion,
	}
}

func testSourceFingerprint(
	sourceFileID string,
	path string,
	inode int64,
	size int64,
	mtime int64,
) SourceFingerprint {
	prefixBytes := size
	if prefixBytes > 4096 {
		prefixBytes = 4096
	}
	prefixSum := sha256.Sum256([]byte(sourceFileID))
	fingerprint := SourceFingerprint{
		SourceFileID: sourceFileID, Provider: "codex", SourceKind: "session",
		CurrentPath: path, DeviceID: "device-a", Inode: inode, SizeBytes: size,
		MTimeNS: mtime, PrefixBytes: prefixBytes, PrefixSHA256: hex.EncodeToString(prefixSum[:]),
	}
	fingerprint.FingerprintSHA256 = sourceFingerprintDigest(fingerprint)
	return fingerprint
}
