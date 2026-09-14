package store

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"gorm.io/gorm"
)

func TestQuotaCurrentSnapshotReadsVerifiedQueryFacts(t *testing.T) {
	t.Parallel()

	repository := openRuntimeRepository(t)
	const nowMS = int64(1_784_300_000_000)
	repository.quotaNow = func() time.Time { return time.UnixMilli(nowMS) }
	_, scope, generation := confirmSyntheticCodexAccount(t, repository, "acct-test-a", nowMS)
	if err := repository.RecordQuotaFetch(
		context.Background(),
		appServerQuotaFetchRecordWithUsage("query-snapshot-quota", scope, generation, nowMS, 40, 12),
	); err != nil {
		t.Fatalf("RecordQuotaFetch() error = %v", err)
	}
	if err := repository.RecordResetCreditsFetch(
		context.Background(), boundCompleteResetCreditsFetchRecord("query-snapshot-reset", scope, generation, nowMS),
	); err != nil {
		t.Fatalf("RecordResetCreditsFetch() error = %v", err)
	}
	quotaDueAtMS := nowMS + 300_000
	quotaSchedule, err := repository.UpsertSourceRefreshSchedule(
		context.Background(), SourceRefreshScheduleUpdate{
			SourceInstanceID: QuotaSourceInstanceAppServer(scope), SourceType: QuotaSourceTypeAppServerRateLimits,
			ScopeKey: scope, BindingGeneration: generation, NextDueAtMS: &quotaDueAtMS,
			Reason: RefreshReasonNormalInterval, AtMS: nowMS,
		},
	)
	if err != nil {
		t.Fatalf("UpsertSourceRefreshSchedule(quota) error = %v", err)
	}
	resetDueAtMS := nowMS + 1_800_000
	resetSchedule, err := repository.UpsertSourceRefreshSchedule(
		context.Background(), SourceRefreshScheduleUpdate{
			SourceInstanceID: ResetCreditsSourceInstanceAppServer(scope),
			SourceType:       ResetCreditsSourceTypeAppServer, ScopeKey: scope,
			BindingGeneration: generation, NextDueAtMS: &resetDueAtMS,
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
	if snapshot.Binding.State != CodexAccountBindingConfirmed || snapshot.AccountScope != scope ||
		snapshot.BindingGeneration != generation || snapshot.EvaluatedAtMS != nowMS+60_000 ||
		len(snapshot.Windows) != 2 || snapshot.Windows[0].Current.WindowKind != QuotaWindowPrimary ||
		snapshot.Windows[1].Current.WindowKind != QuotaWindowSecondary {
		t.Fatalf("snapshot identity/windows = %#v", snapshot)
	}
	for _, window := range snapshot.Windows {
		if window.Current.AccountScope != scope || len(window.Observations) != 1 || len(window.Evidence) != 1 ||
			window.Current.ObservationID == nil ||
			window.Observations[0].ObservationID != *window.Current.ObservationID ||
			window.Evidence[0].ObservationID != *window.Current.ObservationID ||
			window.Observations[0].LastObservedAtMS != nowMS {
			t.Fatalf("window facts = %#v", window)
		}
	}
	if snapshot.OnlineSourceState == nil || snapshot.OnlineSourceState.LastSuccessAtMS == nil ||
		*snapshot.OnlineSourceState.LastSuccessAtMS != nowMS || snapshot.QuotaRefresh == nil ||
		snapshot.QuotaRefresh.Revision != quotaSchedule.Revision || snapshot.ResetCreditsRefresh == nil ||
		snapshot.ResetCreditsRefresh.Revision != resetSchedule.Revision {
		t.Fatalf("source/refresh facts = %#v", snapshot)
	}
	if snapshot.ResetCredits.AvailableCount == nil || *snapshot.ResetCredits.AvailableCount != 2 ||
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
	key, scope, generation := confirmSyntheticCodexAccount(t, writer, "acct-test-a", nowMS)
	if stored, err := reader.EnsureCodexAccountScopeKey(context.Background(), key, nowMS); err != nil || stored != key {
		t.Fatalf("EnsureCodexAccountScopeKey(reader) = %x, %v", stored, err)
	}
	if err := writer.RecordQuotaFetch(
		context.Background(),
		appServerQuotaFetchRecordWithUsage("snapshot-old-quota", scope, generation, nowMS, 40, 10),
	); err != nil {
		t.Fatalf("RecordQuotaFetch(old) error = %v", err)
	}
	if err := writer.RecordResetCreditsFetch(
		context.Background(), boundCompleteResetCreditsFetchRecord("snapshot-old-reset", scope, generation, nowMS),
	); err != nil {
		t.Fatalf("RecordResetCreditsFetch(old) error = %v", err)
	}
	oldQuotaDueAtMS := nowMS + 300_000
	oldQuotaSchedule, err := writer.UpsertSourceRefreshSchedule(
		context.Background(), SourceRefreshScheduleUpdate{
			SourceInstanceID: QuotaSourceInstanceAppServer(scope), SourceType: QuotaSourceTypeAppServerRateLimits,
			ScopeKey: scope, BindingGeneration: generation, NextDueAtMS: &oldQuotaDueAtMS,
			Reason: RefreshReasonNormalInterval, AtMS: nowMS,
		},
	)
	if err != nil {
		t.Fatalf("seed quota schedule: %v", err)
	}
	oldResetDueAtMS := nowMS + 1_800_000
	oldResetSchedule, err := writer.UpsertSourceRefreshSchedule(
		context.Background(), SourceRefreshScheduleUpdate{
			SourceInstanceID: ResetCreditsSourceInstanceAppServer(scope),
			SourceType:       ResetCreditsSourceTypeAppServer, ScopeKey: scope,
			BindingGeneration: generation, NextDueAtMS: &oldResetDueAtMS,
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
		context.Background(),
		appServerQuotaFetchRecordWithUsage("snapshot-new-quota", scope, generation, newAtMS, 55, 20),
	); err != nil {
		t.Fatalf("RecordQuotaFetch(new) error = %v", err)
	}
	if err := writer.RecordResetCreditsFetch(
		context.Background(), boundCompleteResetCreditsFetchRecord("snapshot-new-reset", scope, generation, newAtMS),
	); err != nil {
		t.Fatalf("RecordResetCreditsFetch(new) error = %v", err)
	}
	newQuotaDueAtMS := nowMS + 600_000
	if _, err := writer.UpsertSourceRefreshSchedule(context.Background(), SourceRefreshScheduleUpdate{
		SourceInstanceID: QuotaSourceInstanceAppServer(scope), SourceType: QuotaSourceTypeAppServerRateLimits,
		ScopeKey: scope, BindingGeneration: generation, ExpectedRevision: oldQuotaSchedule.Revision,
		NextDueAtMS: &newQuotaDueAtMS, Reason: RefreshReasonLowRemaining, AtMS: newAtMS,
	}); err != nil {
		t.Fatalf("update quota schedule: %v", err)
	}
	newResetDueAtMS := nowMS + 2_400_000
	if _, err := writer.UpsertSourceRefreshSchedule(context.Background(), SourceRefreshScheduleUpdate{
		SourceInstanceID: ResetCreditsSourceInstanceAppServer(scope),
		SourceType:       ResetCreditsSourceTypeAppServer, ScopeKey: scope,
		BindingGeneration: generation, ExpectedRevision: oldResetSchedule.Revision,
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
		read.snapshot.OnlineSourceState == nil || read.snapshot.OnlineSourceState.LastSuccessAtMS == nil ||
		*read.snapshot.OnlineSourceState.LastSuccessAtMS != nowMS || read.snapshot.QuotaRefresh == nil ||
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
	_, scope, generation := confirmSyntheticCodexAccount(t, repository, "acct-test-a", nowMS)
	if err := repository.RecordQuotaFetch(
		context.Background(),
		appServerQuotaFetchRecordWithUsage("query-missing-projection", scope, generation, nowMS, 38, 9),
	); err != nil {
		t.Fatalf("RecordQuotaFetch() error = %v", err)
	}
	if err := repository.database.Write(context.Background(), func(ctx context.Context, transaction *gorm.DB) error {
		if err := transaction.WithContext(ctx).Where(
			"account_scope = ? AND window_kind = ?", scope, string(QuotaWindowPrimary),
		).Delete(&quotaArbitrationEvidenceModel{}).Error; err != nil {
			return err
		}
		return transaction.WithContext(ctx).Where(
			"account_scope = ? AND window_kind = ?", scope, string(QuotaWindowPrimary),
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
	if err := repository.database.View(context.Background(), func(ctx context.Context, connection *gorm.DB) error {
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

func TestQuotaCurrentSnapshotRejectsWrongAppServerSourceIdentity(t *testing.T) {
	t.Parallel()

	repository := openRuntimeRepository(t)
	const nowMS = int64(1_784_315_000_000)
	repository.quotaNow = func() time.Time { return time.UnixMilli(nowMS) }
	_, scope, generation := confirmSyntheticCodexAccount(t, repository, "acct-test-a", nowMS)
	if err := repository.RecordQuotaFetch(
		context.Background(),
		appServerQuotaFetchRecordWithUsage("query-wrong-source-identity", scope, generation, nowMS, 38, 9),
	); err != nil {
		t.Fatalf("RecordQuotaFetch() error = %v", err)
	}
	if err := repository.database.Write(context.Background(), func(ctx context.Context, transaction *gorm.DB) error {
		return transaction.WithContext(ctx).Model(&sourceStateModel{}).
			Where("source_instance_id = ?", QuotaSourceInstanceAppServer(scope)).
			Update("source_type", ResetCreditsSourceTypeAppServer).Error
	}); err != nil {
		t.Fatalf("tamper source identity: %v", err)
	}
	if _, err := repository.QuotaCurrentSnapshot(
		context.Background(), QuotaAccountScopeDefault, nowMS,
	); !errors.Is(err, ErrInvalidRecord) {
		t.Fatalf("QuotaCurrentSnapshot(tampered source identity) error = %v, want ErrInvalidRecord", err)
	}
}

func TestQuotaCurrentSnapshotHidesUnconfirmedAndLegacyDefault(t *testing.T) {
	t.Parallel()

	repository := openRuntimeRepository(t)
	const nowMS = int64(1_784_320_000_000)
	repository.quotaNow = func() time.Time { return time.UnixMilli(nowMS) }
	legacy := successfulQuotaFetchRecord("legacy-default-quota", nowMS, 88, 11)
	if err := repository.RecordQuotaFetch(context.Background(), legacy); err != nil {
		t.Fatalf("RecordQuotaFetch(legacy default) error = %v", err)
	}
	legacyReset := successfulResetCreditsFetchRecord("legacy-default-reset", nowMS)
	if err := repository.RecordResetCreditsFetch(context.Background(), legacyReset); err != nil {
		t.Fatalf("RecordResetCreditsFetch(legacy default) error = %v", err)
	}

	for _, state := range []struct {
		name   string
		mutate func()
	}{
		{name: "unknown", mutate: func() {}},
		{name: "pending", mutate: func() {
			if _, err := repository.MarkCodexAccountBindingPending(
				context.Background(), nowMS+1, CodexAccountBindingReasonStartup,
			); err != nil {
				t.Fatalf("MarkCodexAccountBindingPending() error = %v", err)
			}
		}},
		{name: "signed_out", mutate: func() {
			if _, _, err := repository.SetCodexAccountBindingUnavailable(
				context.Background(), CodexAccountBindingSignedOut, nowMS+2, CodexAccountBindingReasonSignedOut,
			); err != nil {
				t.Fatalf("SetCodexAccountBindingUnavailable(signed_out) error = %v", err)
			}
		}},
		{name: "identity_unavailable", mutate: func() {
			if _, _, err := repository.SetCodexAccountBindingUnavailable(
				context.Background(), CodexAccountBindingIdentityUnavailable, nowMS+3,
				CodexAccountBindingReasonMissingAccountID,
			); err != nil {
				t.Fatalf("SetCodexAccountBindingUnavailable(identity_unavailable) error = %v", err)
			}
		}},
	} {
		state.mutate()
		snapshot, err := repository.QuotaCurrentSnapshot(context.Background(), QuotaAccountScopeDefault, nowMS+10)
		if err != nil {
			t.Fatalf("QuotaCurrentSnapshot(%s) error = %v", state.name, err)
		}
		if snapshot.Binding.State == CodexAccountBindingConfirmed || len(snapshot.Windows) != 0 ||
			snapshot.AccountScope != "" || snapshot.OnlineSourceState != nil ||
			snapshot.ResetCredits.AvailableCount != nil || snapshot.ResetCredits.AccountScope != "" {
			t.Fatalf("unconfirmed %s leaked online facts: %#v", state.name, snapshot)
		}
		if snapshot.Windows == nil {
			t.Fatalf("unconfirmed %s windows = nil, want empty slice", state.name)
		}
	}

	key, scope, generation := confirmSyntheticCodexAccount(t, repository, "acct-test-a", nowMS+4)
	_ = key
	snapshot, err := repository.QuotaCurrentSnapshot(context.Background(), QuotaAccountScopeDefault, nowMS+10)
	if err != nil {
		t.Fatalf("QuotaCurrentSnapshot(confirmed empty) error = %v", err)
	}
	if snapshot.Binding.State != CodexAccountBindingConfirmed || snapshot.AccountScope != scope ||
		snapshot.BindingGeneration != generation || len(snapshot.Windows) != 0 ||
		snapshot.ResetCredits.AvailableCount != nil {
		t.Fatalf("first confirmed snapshot reused legacy default: %#v", snapshot)
	}
}

func TestQuotaCurrentSnapshotAccountSwitchABARestoresOriginalTimestamps(t *testing.T) {
	t.Parallel()

	repository := openRuntimeRepository(t)
	const (
		timeA  = int64(1_784_330_000_000)
		timeB  = int64(1_784_330_060_000)
		timeA2 = int64(1_784_330_120_000)
	)
	repository.quotaNow = func() time.Time { return time.UnixMilli(timeA) }
	key, scopeA, generationA := confirmSyntheticCodexAccount(t, repository, "acct-test-a", timeA)
	if err := repository.RecordQuotaFetch(
		context.Background(),
		appServerQuotaFetchRecordWithUsage("aba-a", scopeA, generationA, timeA, 40, 10),
	); err != nil {
		t.Fatalf("RecordQuotaFetch(A) error = %v", err)
	}
	firstA, err := repository.QuotaCurrentSnapshot(context.Background(), QuotaAccountScopeDefault, timeA)
	if err != nil || len(firstA.Windows) != 2 || firstA.Windows[0].Current.EffectiveUsedPercent == nil ||
		*firstA.Windows[0].Current.EffectiveUsedPercent != 40 ||
		firstA.Windows[0].Observations[0].LastObservedAtMS != timeA {
		t.Fatalf("first A snapshot = %#v, %v", firstA, err)
	}

	repository.quotaNow = func() time.Time { return time.UnixMilli(timeB) }
	scopeB, generationB := confirmSyntheticCodexAccountWithKey(t, repository, key, "acct-test-b", timeB)
	if scopeB == scopeA || generationB <= generationA {
		t.Fatalf("B binding = scope %s gen %d, A scope %s gen %d", scopeB, generationB, scopeA, generationA)
	}
	if err := repository.RecordQuotaFetch(
		context.Background(),
		appServerQuotaFetchRecordWithUsage("aba-b", scopeB, generationB, timeB, 77, 22),
	); err != nil {
		t.Fatalf("RecordQuotaFetch(B) error = %v", err)
	}
	activeB, err := repository.QuotaCurrentSnapshot(context.Background(), QuotaAccountScopeDefault, timeB)
	if err != nil || activeB.AccountScope != scopeB || len(activeB.Windows) != 2 ||
		activeB.Windows[0].Current.EffectiveUsedPercent == nil ||
		*activeB.Windows[0].Current.EffectiveUsedPercent != 77 ||
		activeB.Windows[0].Observations[0].LastObservedAtMS != timeB {
		t.Fatalf("B snapshot leaked A: %#v, %v", activeB, err)
	}

	repository.quotaNow = func() time.Time { return time.UnixMilli(timeA2) }
	scopeA2, generationA2 := confirmSyntheticCodexAccountWithKey(t, repository, key, "acct-test-a", timeA2)
	if scopeA2 != scopeA || generationA2 <= generationB {
		t.Fatalf("return A binding = scope %s gen %d", scopeA2, generationA2)
	}
	restored, err := repository.QuotaCurrentSnapshot(context.Background(), QuotaAccountScopeDefault, timeA2)
	if err != nil || restored.AccountScope != scopeA || restored.BindingGeneration != generationA2 ||
		len(restored.Windows) != 2 || restored.Windows[0].Current.EffectiveUsedPercent == nil ||
		*restored.Windows[0].Current.EffectiveUsedPercent != 40 ||
		restored.Windows[0].Observations[0].LastObservedAtMS != timeA {
		t.Fatalf("restored A snapshot = %#v, %v", restored, err)
	}
}

func TestQuotaCurrentSnapshotShowsStaleAfterFreshWindow(t *testing.T) {
	t.Parallel()

	repository := openRuntimeRepository(t)
	const nowMS = int64(1_784_340_000_000)
	repository.quotaNow = func() time.Time { return time.UnixMilli(nowMS) }
	_, scope, generation := confirmSyntheticCodexAccount(t, repository, "acct-test-a", nowMS)
	if err := repository.RecordQuotaFetch(
		context.Background(),
		appServerQuotaFetchRecordWithUsage("stale-history", scope, generation, nowMS, 40, 10),
	); err != nil {
		t.Fatalf("RecordQuotaFetch() error = %v", err)
	}
	staleAtMS := nowMS + defaultQuotaArbitrationRule().FreshForMS + 1
	snapshot, err := repository.QuotaCurrentSnapshot(context.Background(), QuotaAccountScopeDefault, staleAtMS)
	if err != nil || len(snapshot.Windows) == 0 {
		t.Fatalf("QuotaCurrentSnapshot(stale) = %#v, %v", snapshot, err)
	}
	if snapshot.Windows[0].Current.FreshnessState != QuotaCurrentStale ||
		snapshot.Windows[0].Observations[0].LastObservedAtMS != nowMS {
		t.Fatalf("stale history was rewritten: %#v", snapshot.Windows[0])
	}
}

func TestQuotaCurrentSnapshotKeepsCountOnlyResetCreditsWithoutInventingRemaining(t *testing.T) {
	t.Parallel()

	repository := openRuntimeRepository(t)
	const nowMS = int64(1_784_350_000_000)
	repository.quotaNow = func() time.Time { return time.UnixMilli(nowMS) }
	_, scope, generation := confirmSyntheticCodexAccount(t, repository, "acct-test-a", nowMS)
	status := int64(200)
	record := ResetCreditsFetchRecord{
		AccountScope: scope, BindingGeneration: generation,
		SourceInstanceID: ResetCreditsSourceInstanceAppServer(scope),
		SourceType:       ResetCreditsSourceTypeAppServer, ScopeKey: scope,
		Attempt: SourceAttempt{
			RequestID: "count-only-reset", SourceInstanceID: ResetCreditsSourceInstanceAppServer(scope),
			StartedAtMS: nowMS, FinishedAtMS: nowMS, Outcome: SourceAttemptSucceeded,
			HTTPStatus: &status, AttemptCount: 1, ResponseBytes: 40,
		},
		Snapshot: &ResetCreditsSnapshot{
			SnapshotID: "count-only-reset-snapshot", RequestID: "count-only-reset",
			AccountScope: scope, AvailableCount: 3, ObservedAtMS: nowMS,
			DetailsStatus: ResetCreditDetailsUnavailable,
		},
	}
	if err := repository.RecordResetCreditsFetch(context.Background(), record); err != nil {
		t.Fatalf("RecordResetCreditsFetch(count-only) error = %v", err)
	}
	snapshot, err := repository.QuotaCurrentSnapshot(context.Background(), QuotaAccountScopeDefault, nowMS)
	if err != nil {
		t.Fatalf("QuotaCurrentSnapshot() error = %v", err)
	}
	if snapshot.ResetCredits.AvailableCount == nil || *snapshot.ResetCredits.AvailableCount != 3 ||
		snapshot.ResetCredits.CumulativeRemainingMS != nil || snapshot.ResetCredits.NextExpiresAtMS != nil ||
		len(snapshot.ResetCredits.Credits) != 0 {
		t.Fatalf("count-only reset credits invented remaining: %#v", snapshot.ResetCredits)
	}
}

func boundCompleteResetCreditsFetchRecord(
	requestID, scope string, generation, observedAt int64,
) ResetCreditsFetchRecord {
	record := successfulResetCreditsFetchRecord(requestID, observedAt)
	record.AccountScope = scope
	record.BindingGeneration = generation
	record.SourceInstanceID = ResetCreditsSourceInstanceAppServer(scope)
	record.SourceType = ResetCreditsSourceTypeAppServer
	record.ScopeKey = scope
	record.Attempt.SourceInstanceID = record.SourceInstanceID
	record.Snapshot.AccountScope = scope
	return record
}
