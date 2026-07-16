package store

import (
	"context"
	"errors"
	"testing"
)

func TestSourceRefreshScheduleClaimsDueWorkOnceAndRejectsStaleCompletion(t *testing.T) {
	t.Parallel()

	repository := NewRepository(openTestDatabase(t))
	if err := repository.EnsureApplicationSchema(context.Background()); err != nil {
		t.Fatalf("EnsureApplicationSchema() error = %v", err)
	}
	dueAt := int64(1_000)
	schedule, err := repository.UpsertSourceRefreshSchedule(context.Background(), SourceRefreshScheduleUpdate{
		SourceInstanceID: QuotaSourceInstanceWhamDefault, SourceType: QuotaSourceTypeWham,
		ScopeKey: QuotaAccountScopeDefault, ExpectedRevision: 0,
		NextDueAtMS: &dueAt, Reason: RefreshReasonStartup, AtMS: 900,
	})
	if err != nil || schedule.Revision != 1 {
		t.Fatalf("UpsertSourceRefreshSchedule() = %#v, %v", schedule, err)
	}
	if _, claimed, err := repository.ClaimSourceRefresh(
		context.Background(), QuotaSourceInstanceWhamDefault, schedule.Revision,
		"claim-too-early", RefreshTriggerScheduled, 999, 30_000,
	); err != nil || claimed {
		t.Fatalf("early claim = %v, %v", claimed, err)
	}
	claimedSchedule, claimed, err := repository.ClaimSourceRefresh(
		context.Background(), QuotaSourceInstanceWhamDefault, schedule.Revision,
		"claim-1", RefreshTriggerScheduled, 1_000, 30_000,
	)
	if err != nil || !claimed || claimedSchedule.ActiveClaimID == nil ||
		*claimedSchedule.ActiveClaimID != "claim-1" || claimedSchedule.Revision != 2 {
		t.Fatalf("due claim = %#v, %v, %v", claimedSchedule, claimed, err)
	}
	if _, claimed, err := repository.ClaimSourceRefresh(
		context.Background(), QuotaSourceInstanceWhamDefault, claimedSchedule.Revision,
		"claim-2", RefreshTriggerScheduled, 1_001, 30_000,
	); err != nil || claimed {
		t.Fatalf("overlapping claim = %v, %v", claimed, err)
	}

	nextDue := int64(301_000)
	completed, err := repository.CompleteSourceRefresh(context.Background(), SourceRefreshCompletion{
		SourceInstanceID: QuotaSourceInstanceWhamDefault, ClaimID: "claim-1",
		ExpectedRevision: claimedSchedule.Revision, NextDueAtMS: &nextDue,
		Reason: RefreshReasonNormalInterval, AtMS: 1_010,
	})
	if err != nil || completed.ActiveClaimID != nil || completed.Revision != 3 ||
		completed.NextDueAtMS == nil || *completed.NextDueAtMS != nextDue {
		t.Fatalf("CompleteSourceRefresh() = %#v, %v", completed, err)
	}
	if _, err := repository.CompleteSourceRefresh(context.Background(), SourceRefreshCompletion{
		SourceInstanceID: QuotaSourceInstanceWhamDefault, ClaimID: "claim-1",
		ExpectedRevision: claimedSchedule.Revision, NextDueAtMS: &nextDue,
		Reason: RefreshReasonNormalInterval, AtMS: 1_011,
	}); !errors.Is(err, ErrSourceRefreshConflict) {
		t.Fatalf("stale completion error = %v, want ErrSourceRefreshConflict", err)
	}
}

func TestReleaseExpiredSourceRefreshClaimRevalidatesDueAtCurrentClock(t *testing.T) {
	t.Parallel()

	repository := NewRepository(openTestDatabase(t))
	if err := repository.EnsureApplicationSchema(context.Background()); err != nil {
		t.Fatalf("EnsureApplicationSchema() error = %v", err)
	}
	dueAt := int64(100)
	schedule, err := repository.UpsertSourceRefreshSchedule(context.Background(), SourceRefreshScheduleUpdate{
		SourceInstanceID: ResetCreditsSourceInstanceWhamDefault, SourceType: ResetCreditsSourceTypeWham,
		ScopeKey: QuotaAccountScopeDefault, ExpectedRevision: 0,
		NextDueAtMS: &dueAt, Reason: RefreshReasonStartup, AtMS: 90,
	})
	if err != nil {
		t.Fatalf("create schedule: %v", err)
	}
	claimed, ok, err := repository.ClaimSourceRefresh(
		context.Background(), ResetCreditsSourceInstanceWhamDefault, schedule.Revision,
		"claim-crashed", RefreshTriggerScheduled, 100, 50,
	)
	if err != nil || !ok {
		t.Fatalf("claim = %#v, %v, %v", claimed, ok, err)
	}
	if expired, err := repository.ListExpiredSourceRefreshClaims(context.Background(), 149, 10); err != nil || len(expired) != 0 {
		t.Fatalf("early expired claims = %#v, %v", expired, err)
	}
	expired, err := repository.ListExpiredSourceRefreshClaims(context.Background(), 150, 10)
	if err != nil || len(expired) != 1 || expired[0].ActiveClaimID == nil ||
		*expired[0].ActiveClaimID != "claim-crashed" {
		t.Fatalf("expired claims = %#v, %v", expired, err)
	}
	recovered, released, err := repository.ReleaseExpiredSourceRefreshClaim(context.Background(), SourceRefreshClaimRecovery{
		SourceInstanceID: ResetCreditsSourceInstanceWhamDefault, ClaimID: "claim-crashed",
		ExpectedRevision: claimed.Revision, AtMS: 150,
	})
	if err != nil || !released || recovered.ActiveClaimID != nil ||
		recovered.NextDueAtMS == nil || *recovered.NextDueAtMS != 150 ||
		recovered.Reason != RefreshReasonRecovery || recovered.Revision != claimed.Revision+1 {
		t.Fatalf("recovered = %#v, %v", recovered, err)
	}
	if _, _, err := repository.ReleaseExpiredSourceRefreshClaim(context.Background(), SourceRefreshClaimRecovery{
		SourceInstanceID: ResetCreditsSourceInstanceWhamDefault, ClaimID: "claim-crashed",
		ExpectedRevision: claimed.Revision, AtMS: 150,
	}); !errors.Is(err, ErrSourceRefreshConflict) {
		t.Fatalf("stale release error = %v, want ErrSourceRefreshConflict", err)
	}
}

func TestAbandonedSourceRefreshClaimRejectsLateRetryAfterAttempt(t *testing.T) {
	t.Parallel()

	repository := NewRepository(openTestDatabase(t))
	if err := repository.EnsureApplicationSchema(context.Background()); err != nil {
		t.Fatalf("EnsureApplicationSchema() error = %v", err)
	}
	const claimID = "late-rate-limit-claim"
	dueAtMS := int64(100)
	schedule, err := repository.UpsertSourceRefreshSchedule(context.Background(), SourceRefreshScheduleUpdate{
		SourceInstanceID: ResetCreditsSourceInstanceWhamDefault, SourceType: ResetCreditsSourceTypeWham,
		ScopeKey: QuotaAccountScopeDefault, NextDueAtMS: &dueAtMS,
		Reason: RefreshReasonStartup, AtMS: 90,
	})
	if err != nil {
		t.Fatalf("create schedule: %v", err)
	}
	claimed, ok, err := repository.ClaimSourceRefresh(
		context.Background(), ResetCreditsSourceInstanceWhamDefault, schedule.Revision,
		claimID, RefreshTriggerScheduled, 100, 50,
	)
	if err != nil || !ok {
		t.Fatalf("claim = %#v, %v, %v", claimed, ok, err)
	}
	if _, released, err := repository.ReleaseExpiredSourceRefreshClaim(
		context.Background(), SourceRefreshClaimRecovery{
			SourceInstanceID: ResetCreditsSourceInstanceWhamDefault, ClaimID: claimID,
			ExpectedRevision: claimed.Revision, AtMS: 150,
		},
	); err != nil || !released {
		t.Fatalf("release = %v, %v", released, err)
	}
	status := int64(429)
	failure := SourceFailureHTTP429
	class := RuntimeErrorUnavailable
	retryAtMS := int64(10_000)
	err = repository.RecordResetCreditsFetch(context.Background(), ResetCreditsFetchRecord{
		SourceInstanceID: ResetCreditsSourceInstanceWhamDefault,
		SourceType:       ResetCreditsSourceTypeWham, ScopeKey: QuotaAccountScopeDefault,
		Attempt: SourceAttempt{
			RequestID: claimID, SourceInstanceID: ResetCreditsSourceInstanceWhamDefault,
			StartedAtMS: 100, FinishedAtMS: 151, Outcome: SourceAttemptFailed,
			HTTPStatus: &status, ErrorClass: &class, FailureCode: &failure,
			AttemptCount: 1, RetryAtMS: &retryAtMS,
		},
	})
	if !errors.Is(err, ErrSourceRefreshConflict) {
		t.Fatalf("late attempt error = %v, want ErrSourceRefreshConflict", err)
	}
	if _, err := repository.SourceState(context.Background(), ResetCreditsSourceInstanceWhamDefault); !errors.Is(err, ErrNotFound) {
		t.Fatalf("late attempt created source state: %v", err)
	}
}

func TestSourceRefreshScheduleManualClaimPersistsThrottleTimestamp(t *testing.T) {
	t.Parallel()

	repository := NewRepository(openTestDatabase(t))
	if err := repository.EnsureApplicationSchema(context.Background()); err != nil {
		t.Fatalf("EnsureApplicationSchema() error = %v", err)
	}
	dueAt := int64(1_000)
	schedule, err := repository.UpsertSourceRefreshSchedule(context.Background(), SourceRefreshScheduleUpdate{
		SourceInstanceID: QuotaSourceInstanceWhamDefault, SourceType: QuotaSourceTypeWham,
		ScopeKey: QuotaAccountScopeDefault, ExpectedRevision: 0,
		NextDueAtMS: &dueAt, Reason: RefreshReasonManual, AtMS: 1_000,
	})
	if err != nil {
		t.Fatalf("create schedule: %v", err)
	}
	claimed, ok, err := repository.ClaimSourceRefresh(
		context.Background(), QuotaSourceInstanceWhamDefault, schedule.Revision,
		"manual-claim", RefreshTriggerManual, 1_000, 30_000,
	)
	if err != nil || !ok || claimed.LastManualAtMS == nil || *claimed.LastManualAtMS != 1_000 {
		t.Fatalf("manual claim = %#v, %v, %v", claimed, ok, err)
	}
	nextDue := int64(301_000)
	completed, err := repository.CompleteSourceRefresh(context.Background(), SourceRefreshCompletion{
		SourceInstanceID: QuotaSourceInstanceWhamDefault, ClaimID: "manual-claim",
		ExpectedRevision: claimed.Revision, NextDueAtMS: &nextDue,
		Reason: RefreshReasonNormalInterval, AtMS: 1_001,
	})
	if err != nil {
		t.Fatalf("complete manual claim: %v", err)
	}
	earlyDue := int64(31_000)
	early, err := repository.UpsertSourceRefreshSchedule(context.Background(), SourceRefreshScheduleUpdate{
		SourceInstanceID: QuotaSourceInstanceWhamDefault, SourceType: QuotaSourceTypeWham,
		ScopeKey: QuotaAccountScopeDefault, ExpectedRevision: completed.Revision,
		NextDueAtMS: &earlyDue, Reason: RefreshReasonManual, AtMS: 31_000,
	})
	if err != nil {
		t.Fatalf("schedule early manual: %v", err)
	}
	if _, ok, err := repository.ClaimSourceRefresh(
		context.Background(), QuotaSourceInstanceWhamDefault, early.Revision,
		"manual-too-soon", RefreshTriggerManual, 31_000, 30_000,
	); err != nil || ok {
		t.Fatalf("manual claim inside durable throttle = %v, %v", ok, err)
	}
}

func TestSourceRefreshScheduleRejectsUnregisteredSourceIdentity(t *testing.T) {
	t.Parallel()

	repository := NewRepository(openTestDatabase(t))
	if err := repository.EnsureApplicationSchema(context.Background()); err != nil {
		t.Fatalf("EnsureApplicationSchema() error = %v", err)
	}
	dueAt := int64(100)
	if _, err := repository.UpsertSourceRefreshSchedule(context.Background(), SourceRefreshScheduleUpdate{
		SourceInstanceID: "private:future:source", SourceType: "private_future",
		ScopeKey: QuotaAccountScopeDefault, NextDueAtMS: &dueAt,
		Reason: RefreshReasonStartup, AtMS: 100,
	}); !errors.Is(err, ErrInvalidRecord) {
		t.Fatalf("unregistered source error = %v, want ErrInvalidRecord", err)
	}
}
