package store

import (
	"context"
	"errors"
	"testing"
	"time"

	storesqlite "github.com/SisyphusSQ/codex-pulse/internal/store/sqlite"
)

func TestRecordResetCreditsFetchPersistsReplaySafeSummary(t *testing.T) {
	t.Parallel()

	repository := NewRepository(openTestDatabase(t))
	if err := repository.EnsureApplicationSchema(context.Background()); err != nil {
		t.Fatalf("EnsureApplicationSchema() error = %v", err)
	}
	const observedAt = int64(1_784_000_000_000)
	record := successfulResetCreditsFetchRecord("reset-request-1", observedAt)
	for attempt := 0; attempt < 2; attempt++ {
		if err := repository.RecordResetCreditsFetch(context.Background(), record); err != nil {
			t.Fatalf("RecordResetCreditsFetch(%d) error = %v", attempt+1, err)
		}
	}

	summary, err := repository.ResetCreditsSummary(
		context.Background(), QuotaAccountScopeDefault, observedAt+60_000,
	)
	if err != nil {
		t.Fatalf("ResetCreditsSummary() error = %v", err)
	}
	if summary.SnapshotID == nil || *summary.SnapshotID != record.Snapshot.SnapshotID ||
		summary.AvailableCount == nil || *summary.AvailableCount != 2 ||
		summary.TotalCount == nil || *summary.TotalCount != 3 ||
		summary.RedeemedCount == nil || *summary.RedeemedCount != 1 ||
		summary.CumulativeRemainingMS == nil || *summary.CumulativeRemainingMS != 10_680_000 ||
		summary.NextExpiresAtMS == nil || *summary.NextExpiresAtMS != observedAt+3_600_000 ||
		summary.LastSuccessAtMS == nil || *summary.LastSuccessAtMS != observedAt ||
		summary.FreshnessState != SourceFreshnessCurrent {
		t.Fatalf("summary = %#v", summary)
	}
	attempts, err := repository.ListSourceAttempts(
		context.Background(), ResetCreditsSourceInstanceWhamDefault, 10,
	)
	if err != nil || len(attempts) != 1 || attempts[0].RequestID != "reset-request-1" {
		t.Fatalf("attempts = %#v, %v", attempts, err)
	}
}

func TestResetCreditsSummaryRejectsSnapshotAttachedToAnotherSourceAttempt(t *testing.T) {
	t.Parallel()

	repository := NewRepository(openTestDatabase(t))
	if err := repository.EnsureApplicationSchema(context.Background()); err != nil {
		t.Fatalf("EnsureApplicationSchema() error = %v", err)
	}
	const observedAt = int64(1_784_000_000_000)
	repository.quotaNow = func() time.Time { return time.UnixMilli(observedAt) }
	resetRecord := successfulResetCreditsFetchRecord("reset-provenance", observedAt)
	if err := repository.RecordResetCreditsFetch(context.Background(), resetRecord); err != nil {
		t.Fatalf("record reset credits: %v", err)
	}
	quotaRecord := successfulQuotaFetchRecord("quota-provenance", observedAt, 42, 8)
	if err := repository.RecordQuotaFetch(context.Background(), quotaRecord); err != nil {
		t.Fatalf("record quota: %v", err)
	}
	if err := repository.database.Write(context.Background(), func(ctx context.Context, transaction storesqlite.WriteTx) error {
		return transaction.WithContext(ctx).Model(&resetCreditsSnapshotModel{}).
			Where("snapshot_id = ?", resetRecord.Snapshot.SnapshotID).
			Update("request_id", quotaRecord.Attempt.RequestID).Error
	}); err != nil {
		t.Fatalf("tamper snapshot request: %v", err)
	}
	if _, err := repository.ResetCreditsSummary(
		context.Background(), QuotaAccountScopeDefault, observedAt,
	); !errors.Is(err, ErrInvalidRecord) {
		t.Fatalf("ResetCreditsSummary(tampered) error = %v, want ErrInvalidRecord", err)
	}
}

func TestRecordResetCreditsFetchRejectsConflictingReplay(t *testing.T) {
	t.Parallel()

	repository := NewRepository(openTestDatabase(t))
	if err := repository.EnsureApplicationSchema(context.Background()); err != nil {
		t.Fatalf("EnsureApplicationSchema() error = %v", err)
	}
	record := successfulResetCreditsFetchRecord("reset-request-conflict", 1_784_000_000_000)
	if err := repository.RecordResetCreditsFetch(context.Background(), record); err != nil {
		t.Fatalf("RecordResetCreditsFetch() error = %v", err)
	}
	conflict := record
	conflict.Snapshot = cloneResetCreditsSnapshot(record.Snapshot)
	conflict.Snapshot.AvailableCount = 1
	if err := repository.RecordResetCreditsFetch(context.Background(), conflict); !errors.Is(err, ErrInvalidRecord) {
		t.Fatalf("conflicting replay error = %v, want ErrInvalidRecord", err)
	}
}

func TestResetCreditsFailurePreservesLastKnownGoodAndDynamicExpiry(t *testing.T) {
	t.Parallel()

	repository := NewRepository(openTestDatabase(t))
	if err := repository.EnsureApplicationSchema(context.Background()); err != nil {
		t.Fatalf("EnsureApplicationSchema() error = %v", err)
	}
	const observedAt = int64(1_784_000_000_000)
	if err := repository.RecordResetCreditsFetch(
		context.Background(), successfulResetCreditsFetchRecord("reset-success", observedAt),
	); err != nil {
		t.Fatalf("record success: %v", err)
	}
	failureCode := SourceFailureNetworkUnavailable
	errorClass := RuntimeErrorUnavailable
	failureAt := observedAt + 120_000
	if err := repository.RecordResetCreditsFetch(context.Background(), ResetCreditsFetchRecord{
		SourceInstanceID: ResetCreditsSourceInstanceWhamDefault,
		SourceType:       ResetCreditsSourceTypeWham,
		ScopeKey:         QuotaAccountScopeDefault,
		Attempt: SourceAttempt{
			RequestID: "reset-failure", SourceInstanceID: ResetCreditsSourceInstanceWhamDefault,
			StartedAtMS: failureAt, FinishedAtMS: failureAt, Outcome: SourceAttemptFailed,
			ErrorClass: &errorClass, FailureCode: &failureCode, AttemptCount: 1,
		},
	}); err != nil {
		t.Fatalf("record failure: %v", err)
	}

	summary, err := repository.ResetCreditsSummary(
		context.Background(), QuotaAccountScopeDefault, observedAt+4_000_000,
	)
	if err != nil {
		t.Fatalf("ResetCreditsSummary() error = %v", err)
	}
	if summary.AvailableCount == nil || *summary.AvailableCount != 1 ||
		summary.CumulativeRemainingMS == nil || *summary.CumulativeRemainingMS != 3_200_000 ||
		summary.NextExpiresAtMS == nil || *summary.NextExpiresAtMS != observedAt+7_200_000 ||
		summary.FreshnessState != SourceFreshnessStale || summary.LastAttemptAtMS == nil ||
		*summary.LastAttemptAtMS != failureAt || summary.LastFailureCode == nil ||
		*summary.LastFailureCode != SourceFailureNetworkUnavailable {
		t.Fatalf("summary after failure/expiry = %#v", summary)
	}
}

func successfulResetCreditsFetchRecord(requestID string, observedAt int64) ResetCreditsFetchRecord {
	statusAvailable := ResetCreditAvailable
	statusRedeemed := ResetCreditRedeemed
	typeCodex := ResetCreditTypeCodexRateLimits
	redeemedAt := observedAt - 60_000
	return ResetCreditsFetchRecord{
		SourceInstanceID: ResetCreditsSourceInstanceWhamDefault,
		SourceType:       ResetCreditsSourceTypeWham,
		ScopeKey:         QuotaAccountScopeDefault,
		Attempt: SourceAttempt{
			RequestID: requestID, SourceInstanceID: ResetCreditsSourceInstanceWhamDefault,
			StartedAtMS: observedAt, FinishedAtMS: observedAt, Outcome: SourceAttemptSucceeded,
			HTTPStatus: pointerTo(int64(200)), AttemptCount: 1, ResponseBytes: 256,
		},
		Snapshot: &ResetCreditsSnapshot{
			SnapshotID: "reset-snapshot-" + requestID, RequestID: requestID,
			AccountScope: QuotaAccountScopeDefault, AvailableCount: 2, ObservedAtMS: observedAt,
			Credits: []ResetCredit{
				{CreditIDHash: SHA256DigestOf([]byte("credit-a")), Status: statusAvailable, Type: typeCodex,
					GrantedAtMS: observedAt - 3_600_000, ExpiresAtMS: observedAt + 3_600_000},
				{CreditIDHash: SHA256DigestOf([]byte("credit-b")), Status: statusAvailable, Type: typeCodex,
					GrantedAtMS: observedAt - 3_600_000, ExpiresAtMS: observedAt + 7_200_000},
				{CreditIDHash: SHA256DigestOf([]byte("credit-c")), Status: statusRedeemed, Type: typeCodex,
					GrantedAtMS: observedAt - 3_600_000, ExpiresAtMS: observedAt + 8_000_000, RedeemedAtMS: &redeemedAt},
			},
		},
	}
}
