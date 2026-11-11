package store

import (
	"context"
	"errors"
	"testing"

	"gorm.io/gorm"

	"github.com/SisyphusSQ/codex-pulse/internal/creditbalance"
)

func TestPaidCreditsPersistenceReplayAndAccountFence(t *testing.T) {
	r := openAccountBindingRepository(t)
	_, scope, generation := confirmSyntheticCodexAccount(t, r, "credit-account-a", 1_784_000_000_000)
	record := appServerQuotaFetchRecord("paid-credit-test", scope, generation, 1_784_000_000_100)
	record.PaidCredits = []creditbalance.Snapshot{{LimitID: "codex", State: "known", Balance: new("62500"), HasCredits: new(true), Unlimited: new(false), ObservedAtMS: record.Attempt.FinishedAtMS}}
	if err := r.RecordQuotaFetch(t.Context(), record); err != nil {
		t.Fatal(err)
	}
	if err := r.RecordQuotaFetch(t.Context(), record); err != nil {
		t.Fatal(err)
	}
	snapshot, err := r.QuotaCurrentSnapshot(t.Context(), scope, record.Attempt.FinishedAtMS)
	if err != nil || len(snapshot.PaidCredits) != 1 || snapshot.PaidCredits[0].Balance == nil || *snapshot.PaidCredits[0].Balance != "62500" {
		t.Fatalf("snapshot=%+v error=%v", snapshot.PaidCredits, err)
	}
	changed := record
	changed.PaidCredits = nil
	if err := r.RecordQuotaFetch(t.Context(), changed); !errors.Is(err, ErrInvalidRecord) {
		t.Fatalf("removed replay facts: %v", err)
	}
	changed.PaidCredits = []creditbalance.Snapshot{record.PaidCredits[0]}
	changed.PaidCredits[0].Balance = new("1")
	if err := r.RecordQuotaFetch(t.Context(), changed); !errors.Is(err, ErrInvalidRecord) {
		t.Fatalf("changed balance replay: %v", err)
	}
	if _, err := r.MarkCodexAccountBindingPending(t.Context(), record.Attempt.FinishedAtMS+1, CodexAccountBindingReasonAccountChanged); err != nil {
		t.Fatal(err)
	}
	if err := r.RecordQuotaFetch(t.Context(), record); !errors.Is(err, ErrCodexAccountBindingChanged) {
		t.Fatalf("late account write: %v", err)
	}
	snapshot, err = r.QuotaCurrentSnapshot(t.Context(), "", record.Attempt.FinishedAtMS+2)
	if err != nil || len(snapshot.PaidCredits) != 0 {
		t.Fatalf("pending binding leaked balance: %+v %v", snapshot.PaidCredits, err)
	}
}

func TestPaidCreditsHistoricalPurgeCountsBalanceOnlyAccount(t *testing.T) {
	r := openAccountBindingRepository(t)
	key, scope, generation := confirmSyntheticCodexAccount(t, r, "credit-history-a", 1_784_000_000_000)
	record := appServerQuotaFetchRecord("paid-history-test", scope, generation, 1_784_000_000_100)
	record.PaidCredits = []creditbalance.Snapshot{{LimitID: "codex", State: "known", Balance: new("0"), HasCredits: new(false), Unlimited: new(false), ObservedAtMS: record.Attempt.FinishedAtMS}}
	if err := r.RecordQuotaFetch(t.Context(), record); err != nil {
		t.Fatal(err)
	}
	confirmSyntheticCodexAccountWithKey(t, r, key, "credit-history-b", 1_784_000_000_300)
	if err := r.database.Write(t.Context(), func(ctx context.Context, db *gorm.DB) error {
		if err := db.WithContext(ctx).Where("account_scope = ?", scope).Delete(&quotaCurrentModel{}).Error; err != nil {
			return err
		}
		return db.WithContext(ctx).Where("account_scope = ?", scope).Delete(&quotaObservationModel{}).Error
	}); err != nil {
		t.Fatal(err)
	}

	purged, err := r.ClearHistoricalCodexAccountQuota(t.Context())
	if err != nil || purged.AccountCount != 1 || purged.PaidCreditCount != 1 {
		t.Fatalf("receipt=%+v error=%v", purged, err)
	}
	err = r.database.ViewSnapshot(t.Context(), func(ctx context.Context, db *gorm.DB) error {
		samples, err := readPaidCredits(ctx, db, scope, 1_784_000_000_400)
		if len(samples) != 0 {
			t.Fatalf("historical credits retained: %+v", samples)
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
}
