package store

import (
	"context"
	"testing"
	"time"
)

func TestListQuotaCurrentReadsAllWindowsFromOneVerifiedSnapshot(t *testing.T) {
	t.Parallel()

	repository := NewRepository(openTestDatabase(t))
	if err := repository.EnsureApplicationSchema(context.Background()); err != nil {
		t.Fatalf("EnsureApplicationSchema() error = %v", err)
	}
	const observedAt = int64(1_784_000_000_000)
	repository.quotaNow = func() time.Time { return time.UnixMilli(observedAt) }
	record := successfulQuotaFetchRecord("quota-current-list", observedAt, 42, 8)
	if err := repository.RecordQuotaFetch(context.Background(), record); err != nil {
		t.Fatalf("RecordQuotaFetch() error = %v", err)
	}

	currents, err := repository.ListQuotaCurrent(
		context.Background(), QuotaAccountScopeDefault, observedAt+1,
	)
	if err != nil {
		t.Fatalf("ListQuotaCurrent() error = %v", err)
	}
	if len(currents) != 2 || currents[0].WindowKind != QuotaWindowPrimary ||
		currents[1].WindowKind != QuotaWindowSecondary ||
		currents[0].EffectiveUsedPercent == nil || *currents[0].EffectiveUsedPercent != 42 ||
		currents[1].EffectiveUsedPercent == nil || *currents[1].EffectiveUsedPercent != 8 ||
		currents[0].EvaluatedAtMS != observedAt+1 || currents[1].EvaluatedAtMS != observedAt+1 {
		t.Fatalf("currents = %#v", currents)
	}
}
