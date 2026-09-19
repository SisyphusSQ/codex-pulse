package store

import (
	"context"
	"testing"
	"time"
)

func TestLegacyQuotaHistoryAssociationIsExplicitExclusiveAndReversible(t *testing.T) {
	t.Parallel()

	repository := openRuntimeRepository(t)
	const nowMS = int64(1_784_400_000_000)
	repository.quotaNow = func() time.Time { return time.UnixMilli(nowMS) }
	if err := repository.RecordQuotaFetch(
		context.Background(), successfulQuotaFetchRecord("legacy-cycle-1", nowMS-8*24*60*60*1_000, 20, 30),
	); err != nil {
		t.Fatalf("RecordQuotaFetch(legacy cycle 1) error = %v", err)
	}
	if err := repository.RecordQuotaFetch(
		context.Background(), successfulQuotaFetchRecord("legacy-cycle-2", nowMS-24*60*60*1_000, 40, 60),
	); err != nil {
		t.Fatalf("RecordQuotaFetch(legacy cycle 2) error = %v", err)
	}

	_, scopeA, _ := confirmSyntheticCodexAccount(t, repository, "acct-history-a", nowMS)
	detectedA := detectedAccountForScope(t, repository, scopeA)
	available, err := repository.LegacyQuotaHistoryStatus(context.Background(), scopeA)
	if err != nil || available.State != LegacyQuotaHistoryAvailable ||
		available.ObservationCount != 4 || available.CycleCount != 4 ||
		available.FirstObservedAtMS == nil || available.LastObservedAtMS == nil {
		t.Fatalf("LegacyQuotaHistoryStatus(A available) = %#v, %v", available, err)
	}

	linked, err := repository.LinkLegacyQuotaHistory(context.Background(), LegacyQuotaHistoryLinkRequest{
		DetectedAccountID:        detectedA.DetectedAccountID,
		ExpectedDetectedRevision: detectedA.Revision,
		NowMS:                    nowMS + 1,
	})
	if err != nil || linked.Result != LegacyQuotaHistoryMutationApplied {
		t.Fatalf("LinkLegacyQuotaHistory(A) = %#v, %v", linked, err)
	}
	linkedStatus, err := repository.LegacyQuotaHistoryStatus(context.Background(), scopeA)
	if err != nil || linkedStatus.State != LegacyQuotaHistoryLinked ||
		linkedStatus.AssociationRevision == nil || *linkedStatus.AssociationRevision != 1 {
		t.Fatalf("LegacyQuotaHistoryStatus(A linked) = %#v, %v", linkedStatus, err)
	}

	_, scopeB, _ := confirmSyntheticCodexAccount(t, repository, "acct-history-b", nowMS+2)
	detectedB := detectedAccountForScope(t, repository, scopeB)
	blocked, err := repository.LegacyQuotaHistoryStatus(context.Background(), scopeB)
	if err != nil || blocked.State != LegacyQuotaHistoryLinkedElsewhere ||
		blocked.ObservationCount != 0 || blocked.CycleCount != 0 {
		t.Fatalf("LegacyQuotaHistoryStatus(B blocked) = %#v, %v", blocked, err)
	}
	conflict, err := repository.LinkLegacyQuotaHistory(context.Background(), LegacyQuotaHistoryLinkRequest{
		DetectedAccountID:        detectedB.DetectedAccountID,
		ExpectedDetectedRevision: detectedB.Revision,
		NowMS:                    nowMS + 3,
	})
	if err != nil || conflict.Result != LegacyQuotaHistoryMutationConflict ||
		conflict.Reason == nil || *conflict.Reason != LegacyQuotaHistoryReasonAlreadyLinked {
		t.Fatalf("LinkLegacyQuotaHistory(B conflict) = %#v, %v", conflict, err)
	}

	revoked, err := repository.UnlinkLegacyQuotaHistory(context.Background(), LegacyQuotaHistoryUnlinkRequest{
		DetectedAccountID:           detectedA.DetectedAccountID,
		ExpectedDetectedRevision:    detectedA.Revision,
		ExpectedAssociationRevision: *linkedStatus.AssociationRevision,
	})
	if err != nil || revoked.Result != LegacyQuotaHistoryMutationApplied {
		t.Fatalf("UnlinkLegacyQuotaHistory(A) = %#v, %v", revoked, err)
	}
	availableB, err := repository.LegacyQuotaHistoryStatus(context.Background(), scopeB)
	if err != nil || availableB.State != LegacyQuotaHistoryAvailable || availableB.ObservationCount != 4 {
		t.Fatalf("LegacyQuotaHistoryStatus(B after revoke) = %#v, %v", availableB, err)
	}
	relinked, err := repository.LinkLegacyQuotaHistory(context.Background(), LegacyQuotaHistoryLinkRequest{
		DetectedAccountID:        detectedB.DetectedAccountID,
		ExpectedDetectedRevision: detectedB.Revision,
		NowMS:                    nowMS + 4,
	})
	if err != nil || relinked.Result != LegacyQuotaHistoryMutationApplied {
		t.Fatalf("LinkLegacyQuotaHistory(B after revoke) = %#v, %v", relinked, err)
	}
	relinkedStatus, err := repository.LegacyQuotaHistoryStatus(context.Background(), scopeB)
	if err != nil || relinkedStatus.AssociationRevision == nil ||
		*relinkedStatus.AssociationRevision != 2 {
		t.Fatalf("LegacyQuotaHistoryStatus(B relinked) = %#v, %v", relinkedStatus, err)
	}
	staleRevoke, err := repository.UnlinkLegacyQuotaHistory(
		context.Background(), LegacyQuotaHistoryUnlinkRequest{
			DetectedAccountID:           detectedB.DetectedAccountID,
			ExpectedDetectedRevision:    detectedB.Revision,
			ExpectedAssociationRevision: *linkedStatus.AssociationRevision,
		},
	)
	if err != nil || staleRevoke.Result != LegacyQuotaHistoryMutationConflict ||
		staleRevoke.Reason == nil || *staleRevoke.Reason != LegacyQuotaHistoryReasonRevisionChanged {
		t.Fatalf("UnlinkLegacyQuotaHistory(B stale generation) = %#v, %v", staleRevoke, err)
	}

	defaultScope := QuotaAccountScopeDefault
	observations, err := repository.ListQuotaObservations(context.Background(), QuotaObservationFilter{
		AccountScope: &defaultScope, Limit: 10,
	})
	if err != nil || len(observations) != 4 {
		t.Fatalf("legacy observations changed by association lifecycle = %#v, %v", observations, err)
	}
}

func detectedAccountForScope(
	t *testing.T,
	repository *Repository,
	accountScope string,
) CodexSubscriptionDetectedAccount {
	t.Helper()
	records, err := repository.ListCodexSubscriptionRecords(context.Background())
	if err != nil {
		t.Fatalf("ListCodexSubscriptionRecords() error = %v", err)
	}
	for _, account := range records.Detected {
		if account.AccountScope == accountScope {
			return account
		}
	}
	t.Fatalf("detected account for scope %s is missing", accountScope)
	return CodexSubscriptionDetectedAccount{}
}
