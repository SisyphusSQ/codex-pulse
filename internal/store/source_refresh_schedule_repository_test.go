package store

import (
	"context"
	"testing"
)

const (
	testAccountScopeA = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	testAccountScopeB = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
)

func TestSourceRefreshScheduleIsolatesAccountsAndGenerations(t *testing.T) {
	t.Parallel()

	repository := NewRepository(openTestDatabase(t))
	if err := repository.EnsureApplicationSchema(context.Background()); err != nil {
		t.Fatalf("EnsureApplicationSchema() error = %v", err)
	}
	dueA := int64(1_000)
	dueB := int64(2_000)
	scheduleA, err := repository.UpsertSourceRefreshSchedule(context.Background(), SourceRefreshScheduleUpdate{
		SourceInstanceID: QuotaSourceInstanceAppServer(testAccountScopeA),
		SourceType:       QuotaSourceTypeAppServerRateLimits, ScopeKey: testAccountScopeA,
		BindingGeneration: 1, ExpectedRevision: 0, NextDueAtMS: &dueA,
		Reason: RefreshReasonStartup, AtMS: 900,
	})
	if err != nil {
		t.Fatalf("Upsert(A) error = %v", err)
	}
	scheduleB, err := repository.UpsertSourceRefreshSchedule(context.Background(), SourceRefreshScheduleUpdate{
		SourceInstanceID: QuotaSourceInstanceAppServer(testAccountScopeB),
		SourceType:       QuotaSourceTypeAppServerRateLimits, ScopeKey: testAccountScopeB,
		BindingGeneration: 1, ExpectedRevision: 0, NextDueAtMS: &dueB,
		Reason: RefreshReasonStartup, AtMS: 900,
	})
	if err != nil {
		t.Fatalf("Upsert(B) error = %v", err)
	}
	if scheduleA.SourceInstanceID == scheduleB.SourceInstanceID {
		t.Fatal("A and B share source instance ID")
	}

	updatedA, err := repository.UpsertSourceRefreshSchedule(context.Background(), SourceRefreshScheduleUpdate{
		SourceInstanceID: scheduleA.SourceInstanceID, SourceType: scheduleA.SourceType,
		ScopeKey: scheduleA.ScopeKey, BindingGeneration: 3, ExpectedRevision: scheduleA.Revision,
		NextDueAtMS: &dueA, Reason: RefreshReasonStartup, AtMS: 950,
	})
	if err != nil || updatedA.BindingGeneration != 3 {
		t.Fatalf("update A generation = %#v, %v", updatedA, err)
	}
	if _, claimed, err := repository.ClaimSourceRefresh(
		context.Background(), updatedA.SourceInstanceID, updatedA.Revision,
		"stale-generation", RefreshTriggerScheduled, 1_000, 30_000, 1,
	); err != nil || claimed {
		t.Fatalf("stale generation claim = %v, %v", claimed, err)
	}
	claimed, ok, err := repository.ClaimSourceRefresh(
		context.Background(), updatedA.SourceInstanceID, updatedA.Revision,
		"current-generation", RefreshTriggerScheduled, 1_000, 30_000, 3,
	)
	if err != nil || !ok || claimed.BindingGeneration != 3 {
		t.Fatalf("current generation claim = %#v, %v, %v", claimed, ok, err)
	}

	paused, err := repository.UpsertSourceRefreshSchedule(context.Background(), SourceRefreshScheduleUpdate{
		SourceInstanceID: scheduleB.SourceInstanceID, SourceType: scheduleB.SourceType,
		ScopeKey: scheduleB.ScopeKey, BindingGeneration: 1, ExpectedRevision: scheduleB.Revision,
		Reason: RefreshReasonInactiveAccount, AtMS: 1_100,
	})
	if err != nil || paused.NextDueAtMS != nil || paused.Reason != RefreshReasonInactiveAccount {
		t.Fatalf("pause B = %#v, %v", paused, err)
	}

	if _, err := repository.AbandonSourceRefreshClaim(context.Background(), SourceRefreshClaimRecovery{
		SourceInstanceID: claimed.SourceInstanceID, ClaimID: "current-generation",
		ExpectedRevision: claimed.Revision, AtMS: 1_100,
	}); err != nil {
		t.Fatalf("abandon A error = %v", err)
	}
	restoredDue := int64(1_200)
	abandoned, err := repository.SourceRefreshSchedule(context.Background(), claimed.SourceInstanceID)
	if err != nil {
		t.Fatalf("load abandoned A error = %v", err)
	}
	restored, err := repository.UpsertSourceRefreshSchedule(context.Background(), SourceRefreshScheduleUpdate{
		SourceInstanceID: abandoned.SourceInstanceID, SourceType: abandoned.SourceType,
		ScopeKey: abandoned.ScopeKey, BindingGeneration: 5, ExpectedRevision: abandoned.Revision,
		NextDueAtMS: &restoredDue, Reason: RefreshReasonRecovery, AtMS: 1_150,
	})
	if err != nil || restored.BindingGeneration != 5 || restored.NextDueAtMS == nil || *restored.NextDueAtMS != restoredDue {
		t.Fatalf("restore A = %#v, %v", restored, err)
	}
}

func TestSourceRefreshScheduleSharesManualFenceAcrossAccounts(t *testing.T) {
	t.Parallel()

	repository := NewRepository(openTestDatabase(t))
	if err := repository.EnsureApplicationSchema(context.Background()); err != nil {
		t.Fatalf("EnsureApplicationSchema() error = %v", err)
	}
	dueAt := int64(1_000)
	quotaA, err := repository.UpsertSourceRefreshSchedule(context.Background(), SourceRefreshScheduleUpdate{
		SourceInstanceID: QuotaSourceInstanceAppServer(testAccountScopeA),
		SourceType:       QuotaSourceTypeAppServerRateLimits, ScopeKey: testAccountScopeA,
		BindingGeneration: 1, ExpectedRevision: 0, NextDueAtMS: &dueAt,
		Reason: RefreshReasonStartup, AtMS: 900,
	})
	if err != nil {
		t.Fatalf("Upsert(quota A) error = %v", err)
	}
	resetB, err := repository.UpsertSourceRefreshSchedule(context.Background(), SourceRefreshScheduleUpdate{
		SourceInstanceID: ResetCreditsSourceInstanceAppServer(testAccountScopeB),
		SourceType:       ResetCreditsSourceTypeAppServer, ScopeKey: testAccountScopeB,
		BindingGeneration: 1, ExpectedRevision: 0, NextDueAtMS: &dueAt,
		Reason: RefreshReasonStartup, AtMS: 900,
	})
	if err != nil {
		t.Fatalf("Upsert(reset B) error = %v", err)
	}
	if _, ok, err := repository.ClaimSourceRefresh(
		context.Background(), quotaA.SourceInstanceID, quotaA.Revision,
		"manual-a", RefreshTriggerManual, 1_000, 30_000, 1,
	); err != nil || !ok {
		t.Fatalf("manual A claim = %v, %v", ok, err)
	}
	if _, ok, err := repository.ClaimSourceRefresh(
		context.Background(), resetB.SourceInstanceID, resetB.Revision,
		"manual-b-too-soon", RefreshTriggerScheduled, 1_030, 30_000, 1,
	); err != nil || ok {
		t.Fatalf("shared fence allowed B claim = %v, %v", ok, err)
	}
	claimed, ok, err := repository.ClaimSourceRefresh(
		context.Background(), resetB.SourceInstanceID, resetB.Revision,
		"manual-b-after-fence", RefreshTriggerScheduled, 61_000, 30_000, 1,
	)
	if err != nil || !ok || claimed.SourceInstanceID != resetB.SourceInstanceID {
		t.Fatalf("B claim after fence = %#v, %v, %v", claimed, ok, err)
	}
}

func TestSourceRefreshScheduleAllowsSameAccountManualCompanion(t *testing.T) {
	t.Parallel()

	repository := NewRepository(openTestDatabase(t))
	if err := repository.EnsureApplicationSchema(context.Background()); err != nil {
		t.Fatalf("EnsureApplicationSchema() error = %v", err)
	}
	dueAt := int64(1_000)
	quotaA, err := repository.UpsertSourceRefreshSchedule(context.Background(), SourceRefreshScheduleUpdate{
		SourceInstanceID: QuotaSourceInstanceAppServer(testAccountScopeA),
		SourceType:       QuotaSourceTypeAppServerRateLimits, ScopeKey: testAccountScopeA,
		BindingGeneration: 2, ExpectedRevision: 0, NextDueAtMS: &dueAt,
		Reason: RefreshReasonStartup, AtMS: 900,
	})
	if err != nil {
		t.Fatalf("Upsert(quota A) error = %v", err)
	}
	resetA, err := repository.UpsertSourceRefreshSchedule(context.Background(), SourceRefreshScheduleUpdate{
		SourceInstanceID: ResetCreditsSourceInstanceAppServer(testAccountScopeA),
		SourceType:       ResetCreditsSourceTypeAppServer, ScopeKey: testAccountScopeA,
		BindingGeneration: 2, ExpectedRevision: 0, NextDueAtMS: &dueAt,
		Reason: RefreshReasonStartup, AtMS: 900,
	})
	if err != nil {
		t.Fatalf("Upsert(reset A) error = %v", err)
	}
	if _, ok, err := repository.ClaimSourceRefresh(
		context.Background(), quotaA.SourceInstanceID, quotaA.Revision,
		"manual-quota-a", RefreshTriggerManual, 1_000, 30_000, 2,
	); err != nil || !ok {
		t.Fatalf("manual quota A claim = %v, %v", ok, err)
	}
	claimed, ok, err := repository.ClaimSourceRefresh(
		context.Background(), resetA.SourceInstanceID, resetA.Revision,
		"manual-reset-a", RefreshTriggerManual, 1_000, 30_000, 2,
	)
	if err != nil || !ok || claimed.SourceInstanceID != resetA.SourceInstanceID {
		t.Fatalf("same-account reset companion claim = %#v, %v, %v", claimed, ok, err)
	}
}

func TestSourceRefreshScheduleClearGlobalFenceUnblocksHomeRearm(t *testing.T) {
	t.Parallel()

	repository := NewRepository(openTestDatabase(t))
	if err := repository.EnsureApplicationSchema(context.Background()); err != nil {
		t.Fatalf("EnsureApplicationSchema() error = %v", err)
	}
	dueAt := int64(1_000)
	schedule, err := repository.UpsertSourceRefreshSchedule(context.Background(), SourceRefreshScheduleUpdate{
		SourceInstanceID: QuotaSourceInstanceAppServer(testAccountScopeA),
		SourceType:       QuotaSourceTypeAppServerRateLimits, ScopeKey: testAccountScopeA,
		BindingGeneration: 1, ExpectedRevision: 0, NextDueAtMS: &dueAt,
		Reason: RefreshReasonNetworkBackoff, AtMS: 900,
	})
	if err != nil {
		t.Fatalf("Upsert() error = %v", err)
	}
	if err := repository.RaiseSourceRefreshGlobalFence(
		context.Background(), 330_000, "network_backoff", 1_000,
	); err != nil {
		t.Fatalf("RaiseSourceRefreshGlobalFence() error = %v", err)
	}
	if _, ok, err := repository.ClaimSourceRefresh(
		context.Background(), schedule.SourceInstanceID, schedule.Revision,
		"blocked-by-backoff", RefreshTriggerRecovery, 1_000, 30_000, 1,
	); err != nil || ok {
		t.Fatalf("backoff fence claim = %v, %v", ok, err)
	}
	if err := repository.ClearSourceRefreshGlobalFence(context.Background()); err != nil {
		t.Fatalf("ClearSourceRefreshGlobalFence() error = %v", err)
	}
	claimed, ok, err := repository.ClaimSourceRefresh(
		context.Background(), schedule.SourceInstanceID, schedule.Revision,
		"after-clear", RefreshTriggerRecovery, 1_000, 30_000, 1,
	)
	if err != nil || !ok || claimed.SourceInstanceID != schedule.SourceInstanceID {
		t.Fatalf("claim after clearing fence = %#v, %v, %v", claimed, ok, err)
	}
}
