package store

import (
	"context"
	"errors"
	"reflect"
	"testing"

	storesqlite "github.com/SisyphusSQ/codex-pulse/internal/store/sqlite"
)

func TestQuotaObservationRepositoryCoalescesOnlyContinuousLocalSamples(t *testing.T) {
	t.Parallel()

	database := openTestDatabase(t)
	repository := NewRepository(database)
	ctx := context.Background()
	if err := repository.EnsureApplicationSchema(ctx); err != nil {
		t.Fatalf("EnsureApplicationSchema() error = %v", err)
	}
	session := quotaTestSession("session-quota")
	sourceFileID := prepareQuotaTestSource(t, repository, ctx, "source-quota-coalesce", 101)
	if err := repository.UpsertFacts(ctx, FactBatch{Session: &session}); err != nil {
		t.Fatalf("UpsertFacts(session) error = %v", err)
	}

	limitID, planType, sessionID := "codex", "pro", session.SessionID
	initial := QuotaObservationSample{
		ObservationID: "quota-observation-1", AccountScope: QuotaAccountScopeDefault,
		Source: QuotaSourceLocalJSONL, LimitID: &limitID, WindowKind: QuotaWindowPrimary,
		UsedPercent: 38, WindowMinutes: 300, ResetsAtMS: 1_784_008_800_000,
		PlanType: &planType, ObservedAtMS: 1_783_990_801_000,
		Validity: QuotaValidityAccepted, SessionID: &sessionID,
		SourceFileID:     &sourceFileID,
		SourceGeneration: 0, SourceOffset: 100,
	}
	if err := repository.UpsertFacts(ctx, FactBatch{QuotaObservation: &initial}); err != nil {
		t.Fatalf("UpsertFacts(initial quota) error = %v", err)
	}
	if err := repository.UpsertFacts(ctx, FactBatch{QuotaObservation: &initial}); err != nil {
		t.Fatalf("UpsertFacts(exact replay) error = %v", err)
	}

	continuous := initial
	continuous.ObservationID = "quota-observation-2"
	continuous.ObservedAtMS++
	continuous.SourceOffset = 200
	if err := repository.UpsertFacts(ctx, FactBatch{QuotaObservation: &continuous}); err != nil {
		t.Fatalf("UpsertFacts(continuous quota) error = %v", err)
	}

	changed := continuous
	changed.ObservationID = "quota-observation-3"
	changed.UsedPercent = 39
	changed.ObservedAtMS++
	changed.SourceOffset = 300
	if err := repository.UpsertFacts(ctx, FactBatch{QuotaObservation: &changed}); err != nil {
		t.Fatalf("UpsertFacts(changed quota) error = %v", err)
	}

	changedBack := changed
	changedBack.ObservationID = "quota-observation-4"
	changedBack.UsedPercent = 38
	changedBack.ObservedAtMS++
	changedBack.SourceOffset = 400
	if err := repository.UpsertFacts(ctx, FactBatch{QuotaObservation: &changedBack}); err != nil {
		t.Fatalf("UpsertFacts(changed-back quota) error = %v", err)
	}
	if err := repository.UpsertFacts(ctx, FactBatch{QuotaObservation: &continuous}); err != nil {
		t.Fatalf("UpsertFacts(coalesced exact replay after A-B-A) error = %v", err)
	}

	observations, err := repository.ListQuotaObservations(ctx, QuotaObservationFilter{
		SessionID: &sessionID, Limit: 10,
	})
	if err != nil {
		t.Fatalf("ListQuotaObservations() error = %v", err)
	}
	if len(observations) != 3 {
		t.Fatalf("observations = %#v", observations)
	}
	if observations[0].ObservationID != initial.ObservationID ||
		observations[0].FirstObservedAtMS != initial.ObservedAtMS ||
		observations[0].LastObservedAtMS != continuous.ObservedAtMS ||
		observations[0].SampleCount != 2 || observations[0].SourceOffset != 200 {
		t.Fatalf("coalesced observation = %#v", observations[0])
	}
	if observations[1].ObservationID != changed.ObservationID || observations[1].SampleCount != 1 ||
		observations[2].ObservationID != changedBack.ObservationID || observations[2].SampleCount != 1 {
		t.Fatalf("segmented observations = %#v", observations)
	}

	readback, err := repository.QuotaObservation(ctx, initial.ObservationID)
	if err != nil || !reflect.DeepEqual(readback, observations[0]) {
		t.Fatalf("QuotaObservation() = %#v, %v", readback, err)
	}
}

func TestQuotaObservationRepositoryRejectsConflictsAndRollsBackFactBatch(t *testing.T) {
	t.Parallel()

	database := openTestDatabase(t)
	repository := NewRepository(database)
	ctx := context.Background()
	if err := repository.EnsureApplicationSchema(ctx); err != nil {
		t.Fatalf("EnsureApplicationSchema() error = %v", err)
	}
	session := quotaTestSession("session-existing")
	sourceFileID := prepareQuotaTestSource(t, repository, ctx, "source-quota-conflict", 102)
	if err := repository.UpsertFacts(ctx, FactBatch{Session: &session}); err != nil {
		t.Fatalf("UpsertFacts(session) error = %v", err)
	}
	limitID, sessionID := "codex", session.SessionID
	sample := QuotaObservationSample{
		ObservationID: "quota-conflict", AccountScope: QuotaAccountScopeDefault,
		Source: QuotaSourceLocalJSONL, LimitID: &limitID, WindowKind: QuotaWindowPrimary,
		UsedPercent: 38, WindowMinutes: 300, ResetsAtMS: 1_784_008_800_000,
		ObservedAtMS: 1_783_990_801_000, Validity: QuotaValidityAccepted,
		SessionID: &sessionID, SourceFileID: &sourceFileID,
		SourceGeneration: 0, SourceOffset: 100,
	}
	if err := repository.UpsertFacts(ctx, FactBatch{QuotaObservation: &sample}); err != nil {
		t.Fatalf("UpsertFacts(sample) error = %v", err)
	}
	identityConflict := sample
	identityConflict.UsedPercent = 37
	if err := repository.UpsertFacts(ctx, FactBatch{QuotaObservation: &identityConflict}); !errors.Is(err, ErrInvalidRecord) {
		t.Fatalf("UpsertFacts(identity conflict) error = %v, want ErrInvalidRecord", err)
	}
	positionConflict := sample
	positionConflict.ObservationID = "quota-position-conflict"
	positionConflict.UsedPercent = 40
	if err := repository.UpsertFacts(ctx, FactBatch{QuotaObservation: &positionConflict}); !errors.Is(err, ErrInvalidRecord) {
		t.Fatalf("UpsertFacts(position conflict) error = %v, want ErrInvalidRecord", err)
	}
	unknownPlan := "unknown"
	for _, testCase := range []struct {
		name   string
		mutate func(*QuotaObservationSample)
	}{
		{name: "missing limit", mutate: func(value *QuotaObservationSample) { value.LimitID = nil }},
		{name: "expired reset", mutate: func(value *QuotaObservationSample) { value.ResetsAtMS = value.ObservedAtMS }},
		{name: "unknown plan", mutate: func(value *QuotaObservationSample) { value.PlanType = &unknownPlan }},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			invalidTrust := sample
			invalidTrust.ObservationID = "quota-untrusted-" + testCase.name
			invalidTrust.SourceOffset++
			testCase.mutate(&invalidTrust)
			if err := repository.UpsertFacts(ctx, FactBatch{QuotaObservation: &invalidTrust}); !errors.Is(err, ErrInvalidRecord) {
				t.Fatalf("UpsertFacts() error = %v, want ErrInvalidRecord", err)
			}
		})
	}

	newSession := quotaTestSession("session-rollback")
	newSessionID := newSession.SessionID
	invalid := sample
	invalid.ObservationID = "quota-invalid"
	invalid.SessionID = &newSessionID
	invalid.WindowMinutes = maxQuotaWindowMinutes + 1
	if err := repository.UpsertFacts(ctx, FactBatch{
		Session: &newSession, QuotaObservation: &invalid,
	}); !errors.Is(err, ErrInvalidRecord) {
		t.Fatalf("UpsertFacts(invalid batch) error = %v, want ErrInvalidRecord", err)
	}
	if _, err := repository.Session(ctx, newSession.SessionID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Session(rolled back) error = %v, want ErrNotFound", err)
	}
}

func TestQuotaObservationHistorySurvivesSessionDeletion(t *testing.T) {
	t.Parallel()

	database := openTestDatabase(t)
	repository := NewRepository(database)
	ctx := context.Background()
	if err := repository.EnsureApplicationSchema(ctx); err != nil {
		t.Fatalf("EnsureApplicationSchema() error = %v", err)
	}
	session := quotaTestSession("session-deleted")
	sourceFileID := prepareQuotaTestSource(t, repository, ctx, "source-quota-history", 103)
	limitID, sessionID := "codex", session.SessionID
	sample := QuotaObservationSample{
		ObservationID: "quota-survives", AccountScope: QuotaAccountScopeDefault,
		Source: QuotaSourceLocalJSONL, LimitID: &limitID, WindowKind: QuotaWindowSecondary,
		UsedPercent: 12, WindowMinutes: 10080, ResetsAtMS: 1_784_595_600_000,
		ObservedAtMS: 1_783_990_801_000, Validity: QuotaValidityAccepted,
		SessionID: &sessionID, SourceFileID: &sourceFileID,
		SourceGeneration: 0, SourceOffset: 100,
	}
	if err := repository.UpsertFacts(ctx, FactBatch{Session: &session, QuotaObservation: &sample}); err != nil {
		t.Fatalf("UpsertFacts() error = %v", err)
	}
	if err := database.Write(ctx, func(ctx context.Context, transaction storesqlite.WriteTx) error {
		return transaction.WithContext(ctx).Delete(&sessionModel{}, "session_id = ?", session.SessionID).Error
	}); err != nil {
		t.Fatalf("delete session: %v", err)
	}
	got, err := repository.QuotaObservation(ctx, sample.ObservationID)
	if err != nil {
		t.Fatalf("QuotaObservation() error = %v", err)
	}
	if got.SessionID != nil || got.UsedPercent != 12 {
		t.Fatalf("observation after session delete = %#v", got)
	}
}

func TestQuotaObservationRepositoryStoresOnlineProvenanceWithoutSession(t *testing.T) {
	t.Parallel()

	database := openTestDatabase(t)
	repository := NewRepository(database)
	ctx := context.Background()
	if err := repository.EnsureApplicationSchema(ctx); err != nil {
		t.Fatalf("EnsureApplicationSchema() error = %v", err)
	}
	limitID, requestID := "codex", "request-wham-1"
	sample := QuotaObservationSample{
		ObservationID: "quota-wham", AccountScope: QuotaAccountScopeDefault,
		Source: QuotaSourceWham, LimitID: &limitID, WindowKind: QuotaWindowPrimary,
		UsedPercent: 20, WindowMinutes: 300, ResetsAtMS: 1_784_008_800_000,
		ObservedAtMS: 1_783_990_801_000, Validity: QuotaValidityAccepted,
		RequestID: &requestID, SourceGeneration: 0, SourceOffset: 0,
	}
	if err := repository.UpsertFacts(ctx, FactBatch{QuotaObservation: &sample}); err != nil {
		t.Fatalf("UpsertFacts(online quota) error = %v", err)
	}
	got, err := repository.QuotaObservation(ctx, sample.ObservationID)
	if err != nil || got.SessionID != nil || got.RequestID == nil || *got.RequestID != requestID {
		t.Fatalf("QuotaObservation(online) = %#v, %v", got, err)
	}
}

func TestValidateIngestBatchRejectsQuotaOutsideGenerationOrCheckpoint(t *testing.T) {
	t.Parallel()

	repository := openRuntimeRepository(t)
	fingerprint := testSourceFingerprint("source-quota", "/synthetic/session-quota.jsonl", 11, 100, 1_000)
	session, _, checkpoint := testCommittedSessionTurn("session-quota", "turn-quota", 0, 100)
	limitID, sessionID := "codex", session.SessionID
	observation := QuotaObservationSample{
		ObservationID: "quota-ingest-boundary", AccountScope: QuotaAccountScopeDefault,
		Source: QuotaSourceLocalJSONL, LimitID: &limitID, WindowKind: QuotaWindowPrimary,
		UsedPercent: 38, WindowMinutes: 300, ResetsAtMS: 1_784_008_800_000,
		ObservedAtMS: 1_783_990_801_000, Validity: QuotaValidityAccepted,
		SessionID: &sessionID, SourceFileID: &fingerprint.SourceFileID,
		SourceGeneration: 1, SourceOffset: 90,
	}
	batch := IngestBatch{
		SourceFileID: fingerprint.SourceFileID, Generation: 0,
		PreviousCommittedOffset: 0, Fingerprint: fingerprint,
		Facts:      []FactBatch{{Session: &session}, {QuotaObservation: &observation}},
		Checkpoint: checkpoint, EOF: true, AtMS: 20,
	}
	if err := repository.validateIngestBatch(batch); !errors.Is(err, ErrInvalidRecord) {
		t.Fatalf("validateIngestBatch(generation mismatch) error = %v, want ErrInvalidRecord", err)
	}
	observation.SourceGeneration = 0
	observation.SourceOffset = 101
	if err := repository.validateIngestBatch(batch); !errors.Is(err, ErrInvalidRecord) {
		t.Fatalf("validateIngestBatch(offset beyond checkpoint) error = %v, want ErrInvalidRecord", err)
	}
	observation.SourceOffset = 90
	otherSourceFileID := "other-source"
	observation.SourceFileID = &otherSourceFileID
	if err := repository.validateIngestBatch(batch); !errors.Is(err, ErrInvalidRecord) {
		t.Fatalf("validateIngestBatch(source mismatch) error = %v, want ErrInvalidRecord", err)
	}
}

func TestCommitIngestBatchRollsBackCheckpointWhenQuotaWriteConflicts(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	repository := openRuntimeRepository(t)
	fingerprint := testSourceFingerprint("source-quota-rollback", "/synthetic/session-quota.jsonl", 11, 100, 1_000)
	cursor, err := repository.PrepareGeneration(ctx, PrepareGenerationRequest{
		Mode: GenerationModeRebuild, Current: fingerprint,
		ParserVersion: "codex-rollout-v1", AtMS: 10,
	})
	if err != nil {
		t.Fatalf("PrepareGeneration() error = %v", err)
	}
	session, _, checkpoint := testCommittedSessionTurn("session-quota-rollback", "turn-quota", 0, 100)
	limitID, sessionID := "codex", session.SessionID
	first := QuotaObservationSample{
		ObservationID: "quota-write-first", AccountScope: QuotaAccountScopeDefault,
		Source: QuotaSourceLocalJSONL, LimitID: &limitID, WindowKind: QuotaWindowPrimary,
		UsedPercent: 38, WindowMinutes: 300, ResetsAtMS: 3_000,
		ObservedAtMS: 1_500, Validity: QuotaValidityAccepted,
		SessionID: &sessionID, SourceFileID: &fingerprint.SourceFileID,
		SourceGeneration: 0, SourceOffset: 50,
	}
	conflict := first
	conflict.ObservationID = "quota-write-conflict"
	conflict.UsedPercent = 39
	batch := IngestBatch{
		SourceFileID: fingerprint.SourceFileID, Generation: cursor.Generation,
		PreviousCommittedOffset: 0, Fingerprint: fingerprint,
		Facts:      []FactBatch{{Session: &session}, {QuotaObservation: &first}, {QuotaObservation: &conflict}},
		Checkpoint: checkpoint, EOF: true, AtMS: 20,
	}
	if _, err := repository.CommitIngestBatch(ctx, batch); err == nil {
		t.Fatal("CommitIngestBatch(conflicting quota) succeeded, want rollback")
	}
	stored, err := repository.GenerationCursor(ctx, fingerprint.SourceFileID, cursor.Generation)
	if err != nil || stored.State != GenerationBuilding || stored.Checkpoint.CommittedOffset != 0 {
		t.Fatalf("GenerationCursor(after rollback) = %#v, %v", stored, err)
	}
	if _, err := repository.Session(ctx, session.SessionID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Session(after rollback) error = %v, want ErrNotFound", err)
	}
	if _, err := repository.QuotaObservation(ctx, first.ObservationID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("QuotaObservation(after rollback) error = %v, want ErrNotFound", err)
	}

	batch.Facts = batch.Facts[:2]
	if _, err := repository.CommitIngestBatch(ctx, batch); err != nil {
		t.Fatalf("CommitIngestBatch(recovered quota) error = %v", err)
	}
	if got, err := repository.QuotaObservation(ctx, first.ObservationID); err != nil || got.SourceOffset != 50 {
		t.Fatalf("QuotaObservation(recovered) = %#v, %v", got, err)
	}
}

func quotaTestSession(sessionID string) Session {
	return Session{
		SessionID: sessionID, Provider: "codex", SourceKind: "session",
		CreatedAtMS: 1_783_990_800_000, FirstSeenAtMS: 1_783_990_800_000,
		LastSeenAtMS: 1_783_990_800_000,
	}
}

func prepareQuotaTestSource(
	t *testing.T,
	repository *Repository,
	ctx context.Context,
	sourceFileID string,
	inode int64,
) string {
	t.Helper()
	fingerprint := testSourceFingerprint(
		sourceFileID, "/synthetic/"+sourceFileID+".jsonl", inode, 1_000, 1_000,
	)
	if _, err := repository.PrepareGeneration(ctx, PrepareGenerationRequest{
		Mode: GenerationModeRebuild, Current: fingerprint,
		ParserVersion: "codex-rollout-v1", AtMS: 10,
	}); err != nil {
		t.Fatalf("PrepareGeneration(quota source) error = %v", err)
	}
	return sourceFileID
}
