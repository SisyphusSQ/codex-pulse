package store

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	storesqlite "github.com/SisyphusSQ/codex-pulse/internal/store/sqlite"
)

func TestQuotaCurrentSnapshotReadsVerifiedQueryFacts(t *testing.T) {
	t.Parallel()

	repository := openRuntimeRepository(t)
	const nowMS = int64(1_784_300_000_000)
	repository.quotaNow = func() time.Time { return time.UnixMilli(nowMS) }
	quotaRecord := successfulQuotaFetchRecord("query-snapshot-quota", nowMS, 40, 12)
	if err := repository.RecordQuotaFetch(context.Background(), quotaRecord); err != nil {
		t.Fatalf("RecordQuotaFetch() error = %v", err)
	}
	resetRecord := successfulResetCreditsFetchRecord("query-snapshot-reset", nowMS)
	if err := repository.RecordResetCreditsFetch(context.Background(), resetRecord); err != nil {
		t.Fatalf("RecordResetCreditsFetch() error = %v", err)
	}
	quotaDueAtMS := nowMS + 300_000
	quotaSchedule, err := repository.UpsertSourceRefreshSchedule(
		context.Background(), SourceRefreshScheduleUpdate{
			SourceInstanceID: QuotaSourceInstanceWhamDefault, SourceType: QuotaSourceTypeWham,
			ScopeKey: QuotaAccountScopeDefault, NextDueAtMS: &quotaDueAtMS,
			Reason: RefreshReasonNormalInterval, AtMS: nowMS,
		},
	)
	if err != nil {
		t.Fatalf("UpsertSourceRefreshSchedule(quota) error = %v", err)
	}
	resetDueAtMS := nowMS + 1_800_000
	resetSchedule, err := repository.UpsertSourceRefreshSchedule(
		context.Background(), SourceRefreshScheduleUpdate{
			SourceInstanceID: ResetCreditsSourceInstanceWhamDefault, SourceType: ResetCreditsSourceTypeWham,
			ScopeKey: QuotaAccountScopeDefault, NextDueAtMS: &resetDueAtMS,
			Reason: RefreshReasonNormalInterval, AtMS: nowMS,
		},
	)
	if err != nil {
		t.Fatalf("UpsertSourceRefreshSchedule(reset) error = %v", err)
	}

	snapshot, err := repository.QuotaCurrentSnapshot(
		context.Background(), QuotaAccountScopeDefault, nowMS+60_000,
	)
	if err != nil {
		t.Fatalf("QuotaCurrentSnapshot() error = %v", err)
	}
	if snapshot.AccountScope != QuotaAccountScopeDefault || snapshot.EvaluatedAtMS != nowMS+60_000 ||
		len(snapshot.Windows) != 2 || snapshot.Windows[0].Current.WindowKind != QuotaWindowPrimary ||
		snapshot.Windows[1].Current.WindowKind != QuotaWindowSecondary {
		t.Fatalf("snapshot identity/windows = %#v", snapshot)
	}
	for _, window := range snapshot.Windows {
		if len(window.Observations) != 1 || len(window.Evidence) != 1 ||
			window.Current.ObservationID == nil ||
			window.Observations[0].ObservationID != *window.Current.ObservationID ||
			window.Evidence[0].ObservationID != *window.Current.ObservationID {
			t.Fatalf("window facts = %#v", window)
		}
	}
	if snapshot.WhamSourceState == nil || snapshot.WhamSourceState.LastSuccessAtMS == nil ||
		*snapshot.WhamSourceState.LastSuccessAtMS != nowMS || snapshot.QuotaRefresh == nil ||
		snapshot.QuotaRefresh.Revision != quotaSchedule.Revision || snapshot.ResetCreditsRefresh == nil ||
		snapshot.ResetCreditsRefresh.Revision != resetSchedule.Revision {
		t.Fatalf("source/refresh facts = %#v", snapshot)
	}
	if snapshot.ResetCredits.AvailableCount == nil || *snapshot.ResetCredits.AvailableCount != 2 ||
		snapshot.ResetCredits.TotalCount == nil || *snapshot.ResetCredits.TotalCount != 3 ||
		snapshot.ResetCredits.CumulativeRemainingMS == nil ||
		*snapshot.ResetCredits.CumulativeRemainingMS != 10_680_000 {
		t.Fatalf("reset credits = %#v", snapshot.ResetCredits)
	}
}

func TestQuotaCurrentSnapshotUsesOneSQLiteReadSnapshot(t *testing.T) {
	t.Parallel()

	database := openTestDatabase(t)
	reader := NewRepository(database)
	writer := NewRepository(database)
	if err := reader.EnsureApplicationSchema(context.Background()); err != nil {
		t.Fatalf("EnsureApplicationSchema() error = %v", err)
	}
	const nowMS = int64(1_784_305_000_000)
	reader.quotaNow = func() time.Time { return time.UnixMilli(nowMS) }
	writer.quotaNow = func() time.Time { return time.UnixMilli(nowMS + 1_000) }
	if err := writer.RecordQuotaFetch(
		context.Background(), successfulQuotaFetchRecord("snapshot-old-quota", nowMS, 40, 10),
	); err != nil {
		t.Fatalf("RecordQuotaFetch(old) error = %v", err)
	}
	if err := writer.RecordResetCreditsFetch(
		context.Background(), successfulResetCreditsFetchRecord("snapshot-old-reset", nowMS),
	); err != nil {
		t.Fatalf("RecordResetCreditsFetch(old) error = %v", err)
	}
	oldQuotaDueAtMS := nowMS + 300_000
	oldQuotaSchedule, err := writer.UpsertSourceRefreshSchedule(
		context.Background(), SourceRefreshScheduleUpdate{
			SourceInstanceID: QuotaSourceInstanceWhamDefault, SourceType: QuotaSourceTypeWham,
			ScopeKey: QuotaAccountScopeDefault, NextDueAtMS: &oldQuotaDueAtMS,
			Reason: RefreshReasonNormalInterval, AtMS: nowMS,
		},
	)
	if err != nil {
		t.Fatalf("seed quota schedule: %v", err)
	}
	oldResetDueAtMS := nowMS + 1_800_000
	oldResetSchedule, err := writer.UpsertSourceRefreshSchedule(
		context.Background(), SourceRefreshScheduleUpdate{
			SourceInstanceID: ResetCreditsSourceInstanceWhamDefault, SourceType: ResetCreditsSourceTypeWham,
			ScopeKey: QuotaAccountScopeDefault, NextDueAtMS: &oldResetDueAtMS,
			Reason: RefreshReasonNormalInterval, AtMS: nowMS,
		},
	)
	if err != nil {
		t.Fatalf("seed reset schedule: %v", err)
	}

	readReached := make(chan struct{})
	releaseRead := make(chan struct{})
	var pauseOnce sync.Once
	reader.quotaProjectionReadHook = func(stage string) error {
		if stage == "after_current" {
			pauseOnce.Do(func() {
				close(readReached)
				<-releaseRead
			})
		}
		return nil
	}
	type snapshotResult struct {
		snapshot QuotaCurrentSnapshot
		err      error
	}
	result := make(chan snapshotResult, 1)
	go func() {
		snapshot, err := reader.QuotaCurrentSnapshot(
			context.Background(), QuotaAccountScopeDefault, nowMS+60_000,
		)
		result <- snapshotResult{snapshot: snapshot, err: err}
	}()
	select {
	case <-readReached:
	case <-time.After(time.Second):
		t.Fatal("snapshot reader did not reach controlled boundary")
	}

	newAtMS := nowMS + 1_000
	if err := writer.RecordQuotaFetch(
		context.Background(), successfulQuotaFetchRecord("snapshot-new-quota", newAtMS, 55, 20),
	); err != nil {
		t.Fatalf("RecordQuotaFetch(new) error = %v", err)
	}
	if err := writer.RecordResetCreditsFetch(
		context.Background(), successfulResetCreditsFetchRecord("snapshot-new-reset", newAtMS),
	); err != nil {
		t.Fatalf("RecordResetCreditsFetch(new) error = %v", err)
	}
	newQuotaDueAtMS := nowMS + 600_000
	if _, err := writer.UpsertSourceRefreshSchedule(context.Background(), SourceRefreshScheduleUpdate{
		SourceInstanceID: QuotaSourceInstanceWhamDefault, SourceType: QuotaSourceTypeWham,
		ScopeKey: QuotaAccountScopeDefault, ExpectedRevision: oldQuotaSchedule.Revision,
		NextDueAtMS: &newQuotaDueAtMS, Reason: RefreshReasonLowRemaining, AtMS: newAtMS,
	}); err != nil {
		t.Fatalf("update quota schedule: %v", err)
	}
	newResetDueAtMS := nowMS + 2_400_000
	if _, err := writer.UpsertSourceRefreshSchedule(context.Background(), SourceRefreshScheduleUpdate{
		SourceInstanceID: ResetCreditsSourceInstanceWhamDefault, SourceType: ResetCreditsSourceTypeWham,
		ScopeKey: QuotaAccountScopeDefault, ExpectedRevision: oldResetSchedule.Revision,
		NextDueAtMS: &newResetDueAtMS, Reason: RefreshReasonNormalInterval, AtMS: newAtMS,
	}); err != nil {
		t.Fatalf("update reset schedule: %v", err)
	}
	close(releaseRead)
	read := <-result
	if read.err != nil {
		t.Fatalf("QuotaCurrentSnapshot() error = %v", read.err)
	}
	if len(read.snapshot.Windows) != 2 || read.snapshot.Windows[0].Current.EffectiveUsedPercent == nil ||
		*read.snapshot.Windows[0].Current.EffectiveUsedPercent != 40 ||
		read.snapshot.WhamSourceState == nil || read.snapshot.WhamSourceState.LastSuccessAtMS == nil ||
		*read.snapshot.WhamSourceState.LastSuccessAtMS != nowMS || read.snapshot.QuotaRefresh == nil ||
		read.snapshot.QuotaRefresh.NextDueAtMS == nil || *read.snapshot.QuotaRefresh.NextDueAtMS != oldQuotaDueAtMS ||
		read.snapshot.ResetCredits.LastSuccessAtMS == nil || *read.snapshot.ResetCredits.LastSuccessAtMS != nowMS ||
		read.snapshot.ResetCreditsRefresh == nil || read.snapshot.ResetCreditsRefresh.NextDueAtMS == nil ||
		*read.snapshot.ResetCreditsRefresh.NextDueAtMS != oldResetDueAtMS {
		t.Fatalf("mixed old/new SQLite snapshot = %#v", read.snapshot)
	}
}

func TestQuotaCurrentSnapshotFailsClosedUntilProjectionIsRebuilt(t *testing.T) {
	t.Parallel()

	repository := openRuntimeRepository(t)
	const nowMS = int64(1_784_310_000_000)
	repository.quotaNow = func() time.Time { return time.UnixMilli(nowMS) }
	if err := repository.RecordQuotaFetch(
		context.Background(), successfulQuotaFetchRecord("query-missing-projection", nowMS, 38, 9),
	); err != nil {
		t.Fatalf("RecordQuotaFetch() error = %v", err)
	}
	if err := repository.database.Write(context.Background(), func(ctx context.Context, transaction storesqlite.WriteTx) error {
		if err := transaction.WithContext(ctx).Where(
			"account_scope = ? AND window_kind = ?", QuotaAccountScopeDefault, string(QuotaWindowPrimary),
		).Delete(&quotaArbitrationEvidenceModel{}).Error; err != nil {
			return err
		}
		return transaction.WithContext(ctx).Where(
			"account_scope = ? AND window_kind = ?", QuotaAccountScopeDefault, string(QuotaWindowPrimary),
		).Delete(&quotaCurrentModel{}).Error
	}); err != nil {
		t.Fatalf("delete projection fixture: %v", err)
	}

	if _, err := repository.QuotaCurrentSnapshot(
		context.Background(), QuotaAccountScopeDefault, nowMS,
	); !errors.Is(err, ErrNotFound) {
		t.Fatalf("QuotaCurrentSnapshot(missing projection) error = %v, want ErrNotFound", err)
	}
	var currentCount int64
	if err := repository.database.View(context.Background(), func(ctx context.Context, connection storesqlite.ReadConn) error {
		return connection.WithContext(ctx).Model(&quotaCurrentModel{}).Count(&currentCount).Error
	}); err != nil {
		t.Fatalf("count current rows: %v", err)
	}
	if currentCount != 1 {
		t.Fatalf("query mutated projection rows: count=%d, want 1", currentCount)
	}
	if err := repository.RebuildQuotaProjection(context.Background(), defaultQuotaArbitrationRule()); err != nil {
		t.Fatalf("RebuildQuotaProjection() error = %v", err)
	}
	if snapshot, err := repository.QuotaCurrentSnapshot(
		context.Background(), QuotaAccountScopeDefault, nowMS,
	); err != nil || len(snapshot.Windows) != 2 {
		t.Fatalf("QuotaCurrentSnapshot(after rebuild) = %#v, %v", snapshot, err)
	}
}

func TestQuotaCurrentSnapshotRejectsWrongWhamSourceIdentity(t *testing.T) {
	t.Parallel()

	repository := openRuntimeRepository(t)
	const nowMS = int64(1_784_315_000_000)
	repository.quotaNow = func() time.Time { return time.UnixMilli(nowMS) }
	if err := repository.RecordQuotaFetch(
		context.Background(), successfulQuotaFetchRecord("query-wrong-source-identity", nowMS, 38, 9),
	); err != nil {
		t.Fatalf("RecordQuotaFetch() error = %v", err)
	}
	if err := repository.database.Write(context.Background(), func(ctx context.Context, transaction storesqlite.WriteTx) error {
		return transaction.WithContext(ctx).Model(&sourceStateModel{}).
			Where("source_instance_id = ?", QuotaSourceInstanceWhamDefault).
			Update("source_type", ResetCreditsSourceTypeWham).Error
	}); err != nil {
		t.Fatalf("tamper source identity: %v", err)
	}
	if _, err := repository.QuotaCurrentSnapshot(
		context.Background(), QuotaAccountScopeDefault, nowMS,
	); !errors.Is(err, ErrInvalidRecord) {
		t.Fatalf("QuotaCurrentSnapshot(tampered source identity) error = %v, want ErrInvalidRecord", err)
	}
}
