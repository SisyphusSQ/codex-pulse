package store

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	storesqlite "github.com/SisyphusSQ/codex-pulse/internal/store/sqlite"
)

func TestQuotaProjectionIntegratesWhamAndLocalAtomically(t *testing.T) {
	t.Parallel()

	database := openTestDatabase(t)
	repository := NewRepository(database)
	ctx := context.Background()
	if err := repository.EnsureApplicationSchema(ctx); err != nil {
		t.Fatalf("EnsureApplicationSchema() error = %v", err)
	}
	observed := int64(50 * quotaTestHourMS)
	reset := observed + 5*quotaTestHourMS
	repository.quotaNow = func() time.Time { return time.UnixMilli(observed) }
	wham := successfulQuotaFetchRecord("projection-wham", observed, 41, 12)
	wham.Observations = wham.Observations[:1]
	wham.Observations[0].ResetsAtMS = reset
	if err := repository.RecordQuotaFetch(ctx, wham); err != nil {
		t.Fatalf("RecordQuotaFetch() error = %v", err)
	}
	assertQuotaCurrentValue(t, repository, observed, 41, QuotaCurrentFresh, QuotaConflictNone)

	repository.quotaNow = func() time.Time { return time.UnixMilli(observed + 1) }
	local := quotaProjectionLocalFixture(t, repository, ctx, observed+1, reset, 45)
	if err := repository.UpsertFacts(ctx, FactBatch{QuotaObservation: &local}); err != nil {
		t.Fatalf("UpsertFacts(local) error = %v", err)
	}
	current := assertQuotaCurrentValue(t, repository, observed+1, 45, QuotaCurrentFresh, QuotaConflictPresent)
	if current.SelectedSource == nil || *current.SelectedSource != QuotaSourceLocalJSONL {
		t.Fatalf("selected source = %#v", current.SelectedSource)
	}
	evidence, err := repository.ListQuotaArbitrationEvidence(
		ctx, QuotaAccountScopeDefault, QuotaWindowPrimary, "codex",
	)
	if err != nil || len(evidence) != 2 {
		t.Fatalf("ListQuotaArbitrationEvidence() = %#v, %v", evidence, err)
	}
}

func TestQuotaProjectionLocalUsesTrustedClockAndMaintenanceRepairsFutureEvaluation(t *testing.T) {
	t.Parallel()

	database := openTestDatabase(t)
	repository := NewRepository(database)
	ctx := context.Background()
	if err := repository.EnsureApplicationSchema(ctx); err != nil {
		t.Fatalf("EnsureApplicationSchema() error = %v", err)
	}
	now := int64(70 * quotaTestHourMS)
	repository.quotaNow = func() time.Time { return time.UnixMilli(now) }
	observed := now + defaultQuotaArbitrationRule().MaxClockSkewMS + 1
	reset := observed + 5*quotaTestHourMS
	local := quotaProjectionLocalFixture(t, repository, ctx, observed, reset, 42)
	if err := repository.UpsertFacts(ctx, FactBatch{QuotaObservation: &local}); err != nil {
		t.Fatalf("UpsertFacts(future local) error = %v", err)
	}
	assertQuotaCurrentNeverLoaded(t, repository, now)

	if err := repository.RebuildQuotaProjection(ctx, defaultQuotaArbitrationRule()); err != nil {
		t.Fatalf("RebuildQuotaProjection(repair future evaluation) error = %v", err)
	}
	assertQuotaCurrentNeverLoaded(t, repository, now)
	evidence, err := repository.ListQuotaArbitrationEvidence(
		ctx, QuotaAccountScopeDefault, QuotaWindowPrimary, "codex",
	)
	if err != nil {
		t.Fatalf("ListQuotaArbitrationEvidence() error = %v", err)
	}
	assertQuotaEvidence(t, evidence, local.ObservationID, QuotaEvidenceSuspicious, QuotaReasonObservedRegression)
}

func TestQuotaProjectionWhamUsesTrustedClockForFutureAndLateAttempts(t *testing.T) {
	t.Parallel()

	t.Run("future attempt cannot raise evaluation clock", func(t *testing.T) {
		database := openTestDatabase(t)
		repository := NewRepository(database)
		ctx := context.Background()
		if err := repository.EnsureApplicationSchema(ctx); err != nil {
			t.Fatalf("EnsureApplicationSchema() error = %v", err)
		}
		now := int64(75 * quotaTestHourMS)
		repository.quotaNow = func() time.Time { return time.UnixMilli(now) }
		futureAt := now + defaultQuotaArbitrationRule().MaxClockSkewMS + 1
		future := successfulQuotaFetchRecord("projection-future-wham", futureAt, 42, 9)
		future.Observations = future.Observations[:1]
		if err := repository.RecordQuotaFetch(ctx, future); err != nil {
			t.Fatalf("RecordQuotaFetch(future) error = %v", err)
		}
		assertQuotaCurrentNeverLoaded(t, repository, now)
		evidence, err := repository.ListQuotaArbitrationEvidence(
			ctx, QuotaAccountScopeDefault, QuotaWindowPrimary, "codex",
		)
		if err != nil {
			t.Fatalf("ListQuotaArbitrationEvidence() error = %v", err)
		}
		assertQuotaEvidence(
			t, evidence, future.Observations[0].ObservationID,
			QuotaEvidenceSuspicious, QuotaReasonObservedRegression,
		)
	})

	t.Run("late attempt cannot lower evaluation clock", func(t *testing.T) {
		database := openTestDatabase(t)
		repository := NewRepository(database)
		ctx := context.Background()
		if err := repository.EnsureApplicationSchema(ctx); err != nil {
			t.Fatalf("EnsureApplicationSchema() error = %v", err)
		}
		now := int64(76 * quotaTestHourMS)
		reset := now + 4*quotaTestHourMS
		repository.quotaNow = func() time.Time { return time.UnixMilli(now) }
		newer := successfulQuotaFetchRecord("projection-newer-wham", now, 40, 9)
		newer.Observations = newer.Observations[:1]
		newer.Observations[0].ResetsAtMS = reset
		if err := repository.RecordQuotaFetch(ctx, newer); err != nil {
			t.Fatalf("RecordQuotaFetch(newer) error = %v", err)
		}
		beforeState, err := repository.SourceState(ctx, QuotaSourceInstanceWhamDefault)
		if err != nil {
			t.Fatalf("SourceState(before late) error = %v", err)
		}
		assertQuotaCurrentValue(t, repository, now, 40, QuotaCurrentFresh, QuotaConflictNone)

		lateAt := now - defaultQuotaArbitrationRule().MaxClockSkewMS - quotaTestMinuteMS
		late := successfulQuotaFetchRecord("projection-late-wham", lateAt, 39, 9)
		late.Observations = late.Observations[:1]
		late.Observations[0].ResetsAtMS = reset
		if err := repository.RecordQuotaFetch(ctx, late); err != nil {
			t.Fatalf("RecordQuotaFetch(late) error = %v", err)
		}
		afterState, err := repository.SourceState(ctx, QuotaSourceInstanceWhamDefault)
		if err != nil {
			t.Fatalf("SourceState(after late) error = %v", err)
		}
		if !sourceStatesEqual(beforeState, afterState) {
			t.Fatalf("late attempt regressed source state: before=%#v after=%#v", beforeState, afterState)
		}
		assertQuotaCurrentValue(t, repository, now, 40, QuotaCurrentFresh, QuotaConflictNone)
		evidence, err := repository.ListQuotaArbitrationEvidence(
			ctx, QuotaAccountScopeDefault, QuotaWindowPrimary, "codex",
		)
		if err != nil {
			t.Fatalf("ListQuotaArbitrationEvidence() error = %v", err)
		}
		assertQuotaEvidence(t, evidence, newer.Observations[0].ObservationID, QuotaEvidenceSelected, "")
		assertQuotaEvidence(t, evidence, late.Observations[0].ObservationID, QuotaEvidenceEligible, "")
	})

	t.Run("invalid trusted clock rolls back the complete fetch", func(t *testing.T) {
		database := openTestDatabase(t)
		repository := NewRepository(database)
		ctx := context.Background()
		if err := repository.EnsureApplicationSchema(ctx); err != nil {
			t.Fatalf("EnsureApplicationSchema() error = %v", err)
		}
		repository.quotaNow = func() time.Time { return time.UnixMilli(-1) }
		record := successfulQuotaFetchRecord("projection-invalid-trusted-clock", 77*quotaTestHourMS, 42, 9)
		record.Observations = record.Observations[:1]
		if err := repository.RecordQuotaFetch(ctx, record); !errors.Is(err, ErrInvalidRecord) {
			t.Fatalf("RecordQuotaFetch(invalid trusted clock) error = %v, want ErrInvalidRecord", err)
		}
		if _, err := repository.SourceState(ctx, record.SourceInstanceID); !errors.Is(err, ErrNotFound) {
			t.Fatalf("SourceState(after rollback) error = %v, want ErrNotFound", err)
		}
		attempts, err := repository.ListSourceAttempts(ctx, record.SourceInstanceID, 10)
		if err != nil || len(attempts) != 0 {
			t.Fatalf("ListSourceAttempts(after rollback) = %#v, %v", attempts, err)
		}
		if _, err := repository.QuotaObservation(ctx, record.Observations[0].ObservationID); !errors.Is(err, ErrNotFound) {
			t.Fatalf("QuotaObservation(after rollback) error = %v, want ErrNotFound", err)
		}
		if _, err := repository.QuotaCurrent(
			ctx, QuotaAccountScopeDefault, QuotaWindowPrimary, "codex", 0,
		); !errors.Is(err, ErrNotFound) {
			t.Fatalf("QuotaCurrent(after rollback) error = %v, want ErrNotFound", err)
		}
	})
}

func TestQuotaProjectionFailureAndDynamicTimeKeepLastKnownGood(t *testing.T) {
	t.Parallel()

	database := openTestDatabase(t)
	repository := NewRepository(database)
	ctx := context.Background()
	if err := repository.EnsureApplicationSchema(ctx); err != nil {
		t.Fatalf("EnsureApplicationSchema() error = %v", err)
	}
	observed := int64(80 * quotaTestHourMS)
	reset := observed + 5*quotaTestHourMS
	success := successfulQuotaFetchRecord("projection-success", observed, 38, 9)
	success.Observations = success.Observations[:1]
	success.Observations[0].ResetsAtMS = reset
	repository.quotaNow = func() time.Time { return time.UnixMilli(observed) }
	if err := repository.RecordQuotaFetch(ctx, success); err != nil {
		t.Fatalf("RecordQuotaFetch(success) error = %v", err)
	}
	failureAt := observed + quotaTestMinuteMS
	failure := failedQuotaFetchRecord(
		"projection-failure", failureAt, SourceAttemptFailed, RuntimeErrorUnavailable,
		SourceFailureNetworkUnavailable, nil,
	)
	repository.quotaNow = func() time.Time { return time.UnixMilli(failureAt) }
	if err := repository.RecordQuotaFetch(ctx, failure); err != nil {
		t.Fatalf("RecordQuotaFetch(failure) error = %v", err)
	}
	current := assertQuotaCurrentValue(t, repository, failureAt, 38, QuotaCurrentStale, QuotaConflictNone)
	if current.LastAttemptAtMS == nil || *current.LastAttemptAtMS != failureAt || current.ObservationID == nil {
		t.Fatalf("failed-refresh current = %#v", current)
	}
	assertQuotaCurrentValue(
		t, repository, observed+defaultQuotaArbitrationRule().FreshForMS+1,
		38, QuotaCurrentStale, QuotaConflictNone,
	)
	assertQuotaCurrentValue(t, repository, reset, 38, QuotaCurrentExpiredUnknown, QuotaConflictNone)
}

func TestQuotaProjectionGenericWhamStateUpdateRebuildsAtomically(t *testing.T) {
	t.Parallel()

	database := openTestDatabase(t)
	repository := NewRepository(database)
	ctx := context.Background()
	if err := repository.EnsureApplicationSchema(ctx); err != nil {
		t.Fatalf("EnsureApplicationSchema() error = %v", err)
	}
	observed := int64(95 * quotaTestHourMS)
	record := successfulQuotaFetchRecord("projection-generic-state", observed, 38, 9)
	record.Observations = record.Observations[:1]
	repository.quotaNow = func() time.Time { return time.UnixMilli(observed) }
	if err := repository.RecordQuotaFetch(ctx, record); err != nil {
		t.Fatalf("RecordQuotaFetch() error = %v", err)
	}
	state, err := repository.SourceState(ctx, QuotaSourceInstanceWhamDefault)
	if err != nil {
		t.Fatalf("SourceState() error = %v", err)
	}
	failureAt := observed + quotaTestMinuteMS
	failureClass := RuntimeErrorUnavailable
	failureCode := SourceFailureNetworkUnavailable
	state.LastAttemptAtMS = &failureAt
	state.ConsecutiveFailures = 1
	state.LastErrorClass = &failureClass
	state.LastFailureCode = &failureCode
	state.FreshnessState = SourceFreshnessStale
	state.CursorVersion++
	state.UpdatedAtMS = failureAt
	repository.quotaNow = func() time.Time { return time.UnixMilli(failureAt) }
	if err := repository.UpsertSourceState(ctx, state); err != nil {
		t.Fatalf("UpsertSourceState(quota) error = %v", err)
	}
	assertQuotaCurrentValue(t, repository, failureAt, 38, QuotaCurrentStale, QuotaConflictNone)
}

func TestQuotaProjectionGenericWhamStateUpdateRollsBackWithProjection(t *testing.T) {
	t.Parallel()

	database := openTestDatabase(t)
	repository := NewRepository(database)
	ctx := context.Background()
	if err := repository.EnsureApplicationSchema(ctx); err != nil {
		t.Fatalf("EnsureApplicationSchema() error = %v", err)
	}
	observed := int64(96 * quotaTestHourMS)
	record := successfulQuotaFetchRecord("projection-generic-state-rollback", observed, 38, 9)
	record.Observations = record.Observations[:1]
	repository.quotaNow = func() time.Time { return time.UnixMilli(observed) }
	if err := repository.RecordQuotaFetch(ctx, record); err != nil {
		t.Fatalf("RecordQuotaFetch() error = %v", err)
	}
	before, err := repository.SourceState(ctx, QuotaSourceInstanceWhamDefault)
	if err != nil {
		t.Fatalf("SourceState(before) error = %v", err)
	}
	updated := before
	failureAt := observed + quotaTestMinuteMS
	failureClass := RuntimeErrorUnavailable
	failureCode := SourceFailureNetworkUnavailable
	updated.LastAttemptAtMS = &failureAt
	updated.ConsecutiveFailures = 1
	updated.LastErrorClass = &failureClass
	updated.LastFailureCode = &failureCode
	updated.FreshnessState = SourceFreshnessStale
	updated.CursorVersion++
	updated.UpdatedAtMS = failureAt
	repository.quotaNow = func() time.Time { return time.UnixMilli(failureAt) }
	want := errors.New("injected generic source-state projection failure")
	repository.quotaProjectionHook = func(stage string) error {
		if stage == "after_delete" {
			return want
		}
		return nil
	}
	if err := repository.UpsertSourceState(ctx, updated); !errors.Is(err, want) {
		t.Fatalf("UpsertSourceState() error = %v, want injected failure", err)
	}
	repository.quotaProjectionHook = nil
	after, err := repository.SourceState(ctx, QuotaSourceInstanceWhamDefault)
	if err != nil {
		t.Fatalf("SourceState(after) error = %v", err)
	}
	if !sourceStatesEqual(before, after) {
		t.Fatalf("source state changed after projection rollback: before=%#v after=%#v", before, after)
	}
	assertQuotaCurrentValue(t, repository, failureAt, 38, QuotaCurrentFresh, QuotaConflictNone)
}

func TestQuotaProjectionReaderUsesSingleSQLiteSnapshot(t *testing.T) {
	t.Parallel()

	database := openTestDatabase(t)
	writer := NewRepository(database)
	reader := NewRepository(database)
	ctx := context.Background()
	if err := writer.EnsureApplicationSchema(ctx); err != nil {
		t.Fatalf("EnsureApplicationSchema() error = %v", err)
	}
	observed := int64(97 * quotaTestHourMS)
	reset := observed + 5*quotaTestHourMS
	initial := successfulQuotaFetchRecord("projection-snapshot-initial", observed, 38, 9)
	initial.Observations = initial.Observations[:1]
	initial.Observations[0].ResetsAtMS = reset
	writer.quotaNow = func() time.Time { return time.UnixMilli(observed) }
	if err := writer.RecordQuotaFetch(ctx, initial); err != nil {
		t.Fatalf("RecordQuotaFetch(initial) error = %v", err)
	}

	entered := make(chan struct{})
	release := make(chan struct{})
	released := false
	defer func() {
		if !released {
			close(release)
		}
	}()
	reader.quotaProjectionReadHook = func(stage string) error {
		if stage == "after_current" {
			close(entered)
			<-release
		}
		return nil
	}
	type readResult struct {
		current QuotaCurrent
		err     error
	}
	result := make(chan readResult, 1)
	updatedAt := observed + quotaTestMinuteMS
	go func() {
		current, err := reader.QuotaCurrent(
			ctx, QuotaAccountScopeDefault, QuotaWindowPrimary, "codex", updatedAt,
		)
		result <- readResult{current: current, err: err}
	}()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("QuotaCurrent did not reach the controlled snapshot boundary")
	}

	updated := successfulQuotaFetchRecord("projection-snapshot-updated", updatedAt, 39, 9)
	updated.Observations = updated.Observations[:1]
	updated.Observations[0].ResetsAtMS = reset
	writer.quotaNow = func() time.Time { return time.UnixMilli(updatedAt) }
	writerDone := make(chan error, 1)
	go func() { writerDone <- writer.RecordQuotaFetch(ctx, updated) }()
	select {
	case err := <-writerDone:
		if err != nil {
			t.Fatalf("RecordQuotaFetch(updated) error = %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("writer could not commit while quota reader held its snapshot")
	}
	close(release)
	released = true
	select {
	case got := <-result:
		if got.err != nil || got.current.EffectiveUsedPercent == nil || *got.current.EffectiveUsedPercent != 38 {
			t.Fatalf("QuotaCurrent(concurrent commit) = %#v, %v; want complete old snapshot used=38", got.current, got.err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("QuotaCurrent did not finish after releasing the snapshot boundary")
	}
	assertQuotaCurrentValue(t, writer, updatedAt, 39, QuotaCurrentFresh, QuotaConflictNone)
}

func TestQuotaProjectionRebuildRollbackAndRestart(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()
	if err := os.Chmod(directory, 0o700); err != nil {
		t.Fatalf("secure temp directory: %v", err)
	}
	path := filepath.Join(directory, "quota-projection.db")
	open := func() *storesqlite.Store {
		database, err := storesqlite.Open(context.Background(), storesqlite.Config{Path: path})
		if err != nil {
			t.Fatalf("sqlite.Open() error = %v", err)
		}
		return database
	}
	database := open()
	repository := NewRepository(database)
	ctx := context.Background()
	if err := repository.EnsureApplicationSchema(ctx); err != nil {
		t.Fatalf("EnsureApplicationSchema() error = %v", err)
	}
	observed := int64(120 * quotaTestHourMS)
	reset := observed + 5*quotaTestHourMS
	record := successfulQuotaFetchRecord("projection-restart", observed, 37, 8)
	record.Observations = record.Observations[:1]
	record.Observations[0].ResetsAtMS = reset
	repository.quotaNow = func() time.Time { return time.UnixMilli(observed) }
	if err := repository.RecordQuotaFetch(ctx, record); err != nil {
		t.Fatalf("RecordQuotaFetch() error = %v", err)
	}
	before := assertQuotaCurrentValue(t, repository, observed, 37, QuotaCurrentFresh, QuotaConflictNone)

	want := errors.New("injected projection failure")
	repository.quotaNow = func() time.Time { return time.UnixMilli(observed + 1) }
	repository.quotaProjectionHook = func(stage string) error {
		if stage == "after_delete" {
			return want
		}
		return nil
	}
	if err := repository.RebuildQuotaProjection(ctx, defaultQuotaArbitrationRule()); !errors.Is(err, want) {
		t.Fatalf("RebuildQuotaProjection(fault) error = %v, want injected", err)
	}
	repository.quotaProjectionHook = nil
	after := assertQuotaCurrentValue(t, repository, observed, 37, QuotaCurrentFresh, QuotaConflictNone)
	if before.ObservationID == nil || after.ObservationID == nil || *before.ObservationID != *after.ObservationID {
		t.Fatalf("projection changed across rollback: before=%#v after=%#v", before, after)
	}
	if err := database.Close(ctx); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	database = open()
	defer func() { _ = database.Close(context.Background()) }()
	repository = NewRepository(database)
	assertQuotaCurrentValue(t, repository, observed+1, 37, QuotaCurrentFresh, QuotaConflictNone)
}

func TestQuotaProjectionBatchesEvidenceBeyondSQLiteVariableLimit(t *testing.T) {
	t.Parallel()

	database := openTestDatabase(t)
	repository := NewRepository(database)
	ctx := context.Background()
	if err := repository.EnsureApplicationSchema(ctx); err != nil {
		t.Fatalf("EnsureApplicationSchema() error = %v", err)
	}
	base := int64(150 * quotaTestHourMS)
	reset := base + 5*quotaTestHourMS
	const historySize = 4096
	err := database.Write(ctx, func(ctx context.Context, transaction storesqlite.WriteTx) error {
		models := make([]*quotaObservationModel, 0, historySize)
		for index := 0; index < historySize; index++ {
			sample := quotaProjectionWhamSample(
				"history-"+formatQuotaTestIndex(index), 40, base+int64(index), reset,
			)
			models = append(models, quotaObservationModelFromSample(sample))
		}
		return transaction.WithContext(ctx).CreateInBatches(&models, 256).Error
	})
	if err != nil {
		t.Fatalf("seed %d observations: %v", historySize, err)
	}
	repository.quotaNow = func() time.Time { return time.UnixMilli(base + historySize) }
	if err := repository.RebuildQuotaProjection(ctx, defaultQuotaArbitrationRule()); err != nil {
		t.Fatalf("RebuildQuotaProjection() error = %v", err)
	}
	assertQuotaCurrentValue(t, repository, base+historySize, 40, QuotaCurrentFresh, QuotaConflictNone)
	evidence, err := repository.ListQuotaArbitrationEvidence(
		ctx, QuotaAccountScopeDefault, QuotaWindowPrimary, "codex",
	)
	if err != nil || len(evidence) != historySize {
		t.Fatalf("evidence count = %d, %v; want %d", len(evidence), err, historySize)
	}

	record := successfulQuotaFetchRecord("history-after-limit", base+historySize+1, 41, 1)
	record.Observations = record.Observations[:1]
	record.Observations[0].ResetsAtMS = reset
	repository.quotaNow = func() time.Time { return time.UnixMilli(base + historySize + 1) }
	if err := repository.RecordQuotaFetch(ctx, record); err != nil {
		t.Fatalf("RecordQuotaFetch(after variable limit) error = %v", err)
	}
	assertQuotaCurrentValue(t, repository, base+historySize+1, 41, QuotaCurrentFresh, QuotaConflictNone)
	evidence, err = repository.ListQuotaArbitrationEvidence(
		ctx, QuotaAccountScopeDefault, QuotaWindowPrimary, "codex",
	)
	if err != nil || len(evidence) != historySize+1 {
		t.Fatalf("evidence count after write = %d, %v; want %d", len(evidence), err, historySize+1)
	}
}

func TestQuotaProjectionReadersFailClosedOnTypedTampering(t *testing.T) {
	t.Parallel()

	database := openTestDatabase(t)
	repository := NewRepository(database)
	ctx := context.Background()
	if err := repository.EnsureApplicationSchema(ctx); err != nil {
		t.Fatalf("EnsureApplicationSchema() error = %v", err)
	}
	observed := int64(170 * quotaTestHourMS)
	record := successfulQuotaFetchRecord("projection-tamper", observed, 38, 7)
	repository.quotaNow = func() time.Time { return time.UnixMilli(observed) }
	if err := repository.RecordQuotaFetch(ctx, record); err != nil {
		t.Fatalf("RecordQuotaFetch() error = %v", err)
	}
	primaryObservationID := record.Observations[0].ObservationID
	if err := database.Write(ctx, func(ctx context.Context, transaction storesqlite.WriteTx) error {
		return transaction.WithContext(ctx).Model(&quotaCurrentModel{}).Where(
			"account_scope = ? AND window_kind = ? AND limit_id = ?",
			QuotaAccountScopeDefault, string(QuotaWindowPrimary), "codex",
		).Update("window_generation", record.Observations[0].ResetsAtMS+1).Error
	}); err == nil {
		t.Fatal("quota_current accepted window_generation different from resets_at_ms")
	}

	updateCurrent := func(values map[string]any) {
		t.Helper()
		if err := database.Write(ctx, func(ctx context.Context, transaction storesqlite.WriteTx) error {
			return transaction.WithContext(ctx).Model(&quotaCurrentModel{}).Where(
				"account_scope = ? AND window_kind = ? AND limit_id = ?",
				QuotaAccountScopeDefault, string(QuotaWindowPrimary), "codex",
			).Updates(values).Error
		}); err != nil {
			t.Fatalf("tamper quota current: %v", err)
		}
	}
	updateEvidence := func(values map[string]any) {
		t.Helper()
		if err := database.Write(ctx, func(ctx context.Context, transaction storesqlite.WriteTx) error {
			return transaction.WithContext(ctx).Model(&quotaArbitrationEvidenceModel{}).Where(
				"account_scope = ? AND window_kind = ? AND limit_id = ? AND observation_id = ?",
				QuotaAccountScopeDefault, string(QuotaWindowPrimary), "codex", primaryObservationID,
			).Updates(values).Error
		}); err != nil {
			t.Fatalf("tamper quota evidence: %v", err)
		}
	}

	updateCurrent(map[string]any{"effective_used_percent": 0})
	if _, err := repository.QuotaCurrent(
		ctx, QuotaAccountScopeDefault, QuotaWindowPrimary, "codex", observed,
	); !errors.Is(err, ErrInvalidRecord) {
		t.Fatalf("QuotaCurrent(tampered used) error = %v, want ErrInvalidRecord", err)
	}
	updateCurrent(map[string]any{"effective_used_percent": 38, "explanation_code": string(QuotaExplanationStale)})
	if _, err := repository.QuotaCurrent(
		ctx, QuotaAccountScopeDefault, QuotaWindowPrimary, "codex", observed,
	); !errors.Is(err, ErrInvalidRecord) {
		t.Fatalf("QuotaCurrent(tampered explanation) error = %v, want ErrInvalidRecord", err)
	}
	updateCurrent(map[string]any{"explanation_code": string(QuotaExplanationTrusted)})
	updateCurrent(map[string]any{
		"freshness_state":  string(QuotaCurrentStale),
		"explanation_code": string(QuotaExplanationStale),
	})
	if _, err := repository.QuotaCurrent(
		ctx, QuotaAccountScopeDefault, QuotaWindowPrimary, "codex", observed,
	); !errors.Is(err, ErrInvalidRecord) {
		t.Fatalf("QuotaCurrent(self-consistent tampered state) error = %v, want ErrInvalidRecord", err)
	}
	updateCurrent(map[string]any{
		"freshness_state":  string(QuotaCurrentFresh),
		"explanation_code": string(QuotaExplanationTrusted),
	})

	updateEvidence(map[string]any{"window_generation": record.Observations[0].ResetsAtMS + 1})
	if _, err := repository.ListQuotaArbitrationEvidence(
		ctx, QuotaAccountScopeDefault, QuotaWindowPrimary, "codex",
	); !errors.Is(err, ErrInvalidRecord) {
		t.Fatalf("ListQuotaArbitrationEvidence(tampered generation) error = %v, want ErrInvalidRecord", err)
	}
	updateEvidence(map[string]any{
		"window_generation": record.Observations[0].ResetsAtMS,
		"window_kind":       string(QuotaWindowSecondary),
	})
	if _, err := repository.ListQuotaArbitrationEvidence(
		ctx, QuotaAccountScopeDefault, QuotaWindowSecondary, "codex",
	); !errors.Is(err, ErrInvalidRecord) {
		t.Fatalf("ListQuotaArbitrationEvidence(tampered logical window) error = %v, want ErrInvalidRecord", err)
	}
	if err := database.Write(ctx, func(ctx context.Context, transaction storesqlite.WriteTx) error {
		return transaction.WithContext(ctx).Model(&quotaArbitrationEvidenceModel{}).Where(
			"account_scope = ? AND window_kind = ? AND limit_id = ? AND observation_id = ?",
			QuotaAccountScopeDefault, string(QuotaWindowSecondary), "codex", primaryObservationID,
		).Update("window_kind", string(QuotaWindowPrimary)).Error
	}); err != nil {
		t.Fatalf("restore quota evidence logical window: %v", err)
	}
	updateEvidence(map[string]any{
		"window_generation": record.Observations[0].ResetsAtMS,
		"explanation_code":  string(QuotaExplanationUnavailable),
	})
	if _, err := repository.ListQuotaArbitrationEvidence(
		ctx, QuotaAccountScopeDefault, QuotaWindowPrimary, "codex",
	); !errors.Is(err, ErrInvalidRecord) {
		t.Fatalf("ListQuotaArbitrationEvidence(tampered explanation) error = %v, want ErrInvalidRecord", err)
	}
	updateEvidence(map[string]any{
		"disposition":      string(QuotaEvidenceSuspicious),
		"reason":           string(QuotaReasonDefaultFallback),
		"explanation_code": string(QuotaExplanationSuspicious),
	})
	if _, err := repository.ListQuotaArbitrationEvidence(
		ctx, QuotaAccountScopeDefault, QuotaWindowPrimary, "codex",
	); !errors.Is(err, ErrInvalidRecord) {
		t.Fatalf("ListQuotaArbitrationEvidence(self-consistent tamper) error = %v, want ErrInvalidRecord", err)
	}
	updateEvidence(map[string]any{
		"disposition":      string(QuotaEvidenceSelected),
		"reason":           nil,
		"explanation_code": string(QuotaExplanationTrusted),
	})
	if err := database.Write(ctx, func(ctx context.Context, transaction storesqlite.WriteTx) error {
		return transaction.WithContext(ctx).Where(
			"account_scope = ? AND window_kind = ? AND limit_id = ?",
			QuotaAccountScopeDefault, string(QuotaWindowPrimary), "codex",
		).Delete(&quotaArbitrationEvidenceModel{}).Error
	}); err != nil {
		t.Fatalf("delete quota evidence: %v", err)
	}
	if _, err := repository.ListQuotaArbitrationEvidence(
		ctx, QuotaAccountScopeDefault, QuotaWindowPrimary, "codex",
	); !errors.Is(err, ErrInvalidRecord) {
		t.Fatalf("ListQuotaArbitrationEvidence(deleted set) error = %v, want ErrInvalidRecord", err)
	}
	if _, err := repository.QuotaCurrent(
		ctx, QuotaAccountScopeDefault, QuotaWindowPrimary, "codex", observed,
	); !errors.Is(err, ErrInvalidRecord) {
		t.Fatalf("QuotaCurrent(deleted evidence set) error = %v, want ErrInvalidRecord", err)
	}
}

func TestQuotaProjectionReadersRejectMissingAndExtraEvidenceMembers(t *testing.T) {
	t.Parallel()

	database := openTestDatabase(t)
	repository := NewRepository(database)
	ctx := context.Background()
	if err := repository.EnsureApplicationSchema(ctx); err != nil {
		t.Fatalf("EnsureApplicationSchema() error = %v", err)
	}
	observed := int64(175 * quotaTestHourMS)
	initial := successfulQuotaFetchRecord("projection-evidence-membership", observed, 38, 7)
	repository.quotaNow = func() time.Time { return time.UnixMilli(observed) }
	if err := repository.RecordQuotaFetch(ctx, initial); err != nil {
		t.Fatalf("RecordQuotaFetch(initial) error = %v", err)
	}
	updatedAt := observed + quotaTestMinuteMS
	updated := successfulQuotaFetchRecord("projection-evidence-membership-updated", updatedAt, 39, 7)
	updated.Observations = updated.Observations[:1]
	updated.Observations[0].ResetsAtMS = initial.Observations[0].ResetsAtMS
	repository.quotaNow = func() time.Time { return time.UnixMilli(updatedAt) }
	if err := repository.RecordQuotaFetch(ctx, updated); err != nil {
		t.Fatalf("RecordQuotaFetch(updated) error = %v", err)
	}
	if err := database.Write(ctx, func(ctx context.Context, transaction storesqlite.WriteTx) error {
		return transaction.WithContext(ctx).Where(
			"account_scope = ? AND window_kind = ? AND limit_id = ? AND observation_id = ?",
			QuotaAccountScopeDefault, string(QuotaWindowPrimary), "codex", initial.Observations[0].ObservationID,
		).Delete(&quotaArbitrationEvidenceModel{}).Error
	}); err != nil {
		t.Fatalf("delete one quota evidence member: %v", err)
	}
	if _, err := repository.ListQuotaArbitrationEvidence(
		ctx, QuotaAccountScopeDefault, QuotaWindowPrimary, "codex",
	); !errors.Is(err, ErrInvalidRecord) {
		t.Fatalf("ListQuotaArbitrationEvidence(missing member) error = %v, want ErrInvalidRecord", err)
	}
	repository.quotaNow = func() time.Time { return time.UnixMilli(updatedAt) }
	if err := repository.RebuildQuotaProjection(ctx, defaultQuotaArbitrationRule()); err != nil {
		t.Fatalf("RebuildQuotaProjection(restore evidence) error = %v", err)
	}
	extra := quotaArbitrationEvidenceModel{
		AccountScope:     QuotaAccountScopeDefault,
		WindowKind:       string(QuotaWindowPrimary),
		LimitID:          "codex",
		ObservationID:    initial.Observations[1].ObservationID,
		WindowGeneration: initial.Observations[1].ResetsAtMS,
		Disposition:      string(QuotaEvidenceEligible),
		ExplanationCode:  string(QuotaExplanationTrusted),
	}
	if err := database.Write(ctx, func(ctx context.Context, transaction storesqlite.WriteTx) error {
		return transaction.WithContext(ctx).Create(&extra).Error
	}); err != nil {
		t.Fatalf("insert extra quota evidence member: %v", err)
	}
	if _, err := repository.ListQuotaArbitrationEvidence(
		ctx, QuotaAccountScopeDefault, QuotaWindowPrimary, "codex",
	); !errors.Is(err, ErrInvalidRecord) {
		t.Fatalf("ListQuotaArbitrationEvidence(extra member) error = %v, want ErrInvalidRecord", err)
	}
}

func TestQuotaProjectionPartialFetchKeepsSiblingWindow(t *testing.T) {
	t.Parallel()

	database := openTestDatabase(t)
	repository := NewRepository(database)
	ctx := context.Background()
	if err := repository.EnsureApplicationSchema(ctx); err != nil {
		t.Fatalf("EnsureApplicationSchema() error = %v", err)
	}
	observed := int64(180 * quotaTestHourMS)
	initial := successfulQuotaFetchRecord("partial-initial", observed, 30, 60)
	repository.quotaNow = func() time.Time { return time.UnixMilli(observed) }
	if err := repository.RecordQuotaFetch(ctx, initial); err != nil {
		t.Fatalf("RecordQuotaFetch(initial) error = %v", err)
	}
	secondary, err := repository.QuotaCurrent(
		ctx, QuotaAccountScopeDefault, QuotaWindowSecondary, "codex", observed,
	)
	if err != nil || secondary.EffectiveUsedPercent == nil || *secondary.EffectiveUsedPercent != 60 {
		t.Fatalf("secondary before partial = %#v, %v", secondary, err)
	}

	partialAt := observed + quotaTestMinuteMS
	partial := failedQuotaFetchRecord(
		"partial-primary", partialAt, SourceAttemptFailed, RuntimeErrorInvalid,
		SourceFailureSchemaIncompatible, nil,
	)
	primary := quotaProjectionWhamSample("partial-primary-observation", 31, partialAt, initial.Observations[0].ResetsAtMS)
	requestID := partial.Attempt.RequestID
	primary.RequestID = &requestID
	partial.Observations = []QuotaObservationSample{primary}
	repository.quotaNow = func() time.Time { return time.UnixMilli(partialAt) }
	if err := repository.RecordQuotaFetch(ctx, partial); err != nil {
		t.Fatalf("RecordQuotaFetch(partial) error = %v", err)
	}
	assertQuotaCurrentValue(t, repository, partialAt, 31, QuotaCurrentFresh, QuotaConflictNone)
	secondary, err = repository.QuotaCurrent(
		ctx, QuotaAccountScopeDefault, QuotaWindowSecondary, "codex", partialAt,
	)
	if err != nil || secondary.EffectiveUsedPercent == nil || *secondary.EffectiveUsedPercent != 60 ||
		secondary.ObservationID == nil {
		t.Fatalf("secondary after partial = %#v, %v", secondary, err)
	}
}

func TestQuotaProjectionCancelledRebuildKeepsPreviousProjection(t *testing.T) {
	t.Parallel()

	database := openTestDatabase(t)
	repository := NewRepository(database)
	ctx := context.Background()
	if err := repository.EnsureApplicationSchema(ctx); err != nil {
		t.Fatalf("EnsureApplicationSchema() error = %v", err)
	}
	observed := int64(210 * quotaTestHourMS)
	record := successfulQuotaFetchRecord("projection-cancel", observed, 36, 7)
	record.Observations = record.Observations[:1]
	repository.quotaNow = func() time.Time { return time.UnixMilli(observed) }
	if err := repository.RecordQuotaFetch(ctx, record); err != nil {
		t.Fatalf("RecordQuotaFetch() error = %v", err)
	}
	before := assertQuotaCurrentValue(t, repository, observed, 36, QuotaCurrentFresh, QuotaConflictNone)
	repository.quotaNow = func() time.Time { return time.UnixMilli(observed + 1) }
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if err := repository.RebuildQuotaProjection(cancelled, defaultQuotaArbitrationRule()); !errors.Is(err, context.Canceled) {
		t.Fatalf("RebuildQuotaProjection(cancelled) error = %v, want context.Canceled", err)
	}
	after := assertQuotaCurrentValue(t, repository, observed, 36, QuotaCurrentFresh, QuotaConflictNone)
	if before.ObservationID == nil || after.ObservationID == nil || *before.ObservationID != *after.ObservationID {
		t.Fatalf("projection changed after cancellation: before=%#v after=%#v", before, after)
	}
}

func formatQuotaTestIndex(index int) string {
	return fmt.Sprintf("%04d", index)
}

func assertQuotaCurrentNeverLoaded(t *testing.T, repository *Repository, evaluatedAtMS int64) {
	t.Helper()
	current, err := repository.QuotaCurrent(
		context.Background(), QuotaAccountScopeDefault, QuotaWindowPrimary, "codex", evaluatedAtMS,
	)
	if err != nil || current.ObservationID != nil || current.EffectiveUsedPercent != nil ||
		current.FreshnessState != QuotaCurrentNeverLoaded || current.ExplanationCode != QuotaExplanationUnavailable {
		t.Fatalf("QuotaCurrent() = %#v, %v; want never-loaded without selected value", current, err)
	}
}

func assertQuotaCurrentValue(
	t *testing.T,
	repository *Repository,
	evaluatedAtMS int64,
	used float64,
	freshness QuotaCurrentFreshness,
	conflict QuotaConflictState,
) QuotaCurrent {
	t.Helper()
	current, err := repository.QuotaCurrent(
		context.Background(), QuotaAccountScopeDefault, QuotaWindowPrimary, "codex", evaluatedAtMS,
	)
	if err != nil || current.EffectiveUsedPercent == nil || *current.EffectiveUsedPercent != used ||
		current.FreshnessState != freshness || current.ConflictState != conflict {
		t.Fatalf("QuotaCurrent() = %#v, %v; want used=%v freshness=%s conflict=%s", current, err, used, freshness, conflict)
	}
	return current
}

func quotaProjectionLocalFixture(
	t *testing.T,
	repository *Repository,
	ctx context.Context,
	observedAt, reset int64,
	used float64,
) QuotaObservationSample {
	t.Helper()
	session := quotaTestSession("projection-local-session")
	if err := repository.UpsertFacts(ctx, FactBatch{Session: &session}); err != nil {
		t.Fatalf("UpsertFacts(session) error = %v", err)
	}
	sourceFileID := prepareQuotaTestSource(t, repository, ctx, "projection-local-source", 904)
	limitID, sessionID := "codex", session.SessionID
	return QuotaObservationSample{
		ObservationID: "projection-local", AccountScope: QuotaAccountScopeDefault,
		Source: QuotaSourceLocalJSONL, LimitID: &limitID, WindowKind: QuotaWindowPrimary,
		UsedPercent: used, WindowMinutes: 300, ResetsAtMS: reset, ObservedAtMS: observedAt,
		Validity: QuotaValidityAccepted, SessionID: &sessionID, SourceFileID: &sourceFileID,
		SourceGeneration: 1, SourceOffset: observedAt,
	}
}
