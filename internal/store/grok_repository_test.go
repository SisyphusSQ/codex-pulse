package store

import (
	"context"
	"strings"
	"testing"
)

func TestGrokLocalReplacementPreservesLastBillingSnapshot(t *testing.T) {
	t.Parallel()
	repository := openRuntimeRepository(t)
	ctx := context.Background()
	tier := "SuperGrok"
	if err := repository.CommitGrokBillingSnapshot(ctx, GrokBillingSnapshot{
		Generation: 10, CollectedAtMS: 2_000, PeriodType: "weekly",
		PeriodStartMS: 1_000, PeriodEndMS: 8_000, UsedPercent: 37.5,
		SubscriptionTier: &tier,
		QuotaObservations: []GrokBillingQuotaObservation{{
			Generation: 10, LimitID: "grok.included_credits", UsedPercent: 37.5,
			CycleStartAtMS: 1_000, CycleEndAtMS: 8_000, ObservedAtMS: 2_000,
		}},
	}); err != nil {
		t.Fatalf("CommitGrokBillingSnapshot() error = %v", err)
	}
	local := GrokSnapshot{
		Generation: 11, CollectedAtMS: 2_100,
		Sources: []CursorSourceStatus{{
			Provider: "grok", SourceKey: "grok.summary", SourceType: "filesystem_scan",
			State: "available", CoverageState: "exact", CheckpointKind: "filesystem_scan",
			RowCount: 0, LastAttemptAtMS: 2_100, LastSuccessAtMS: pointerInt64ForCursorTest(2_100), UpdatedAtMS: 2_100,
		}},
		Sessions: []GrokSession{{
			ExternalSessionID: "session-1", DisplayTitle: "Plan", TitleSource: "grok_summary",
			ProjectKey: strings.Repeat("a", 64), ProjectDisplayName: "demo",
			CreatedAtMS: 1_000, LastActivityAtMS: 1_500, RequestCount: 1, CoverageState: "exact", UpdatedAtMS: 2_100,
		}},
	}
	if err := repository.ReplaceGrokSnapshot(ctx, local); err != nil {
		t.Fatalf("ReplaceGrokSnapshot() error = %v", err)
	}
	readback, err := repository.GrokSnapshot(ctx)
	if err != nil {
		t.Fatalf("GrokSnapshot() error = %v", err)
	}
	if readback.Billing == nil || readback.Billing.UsedPercent != 37.5 ||
		readback.Billing.SubscriptionTier == nil || *readback.Billing.SubscriptionTier != "SuperGrok" {
		t.Fatalf("billing after local replacement = %#v", readback.Billing)
	}
	var billingSource *CursorSourceStatus
	for index := range readback.Sources {
		if readback.Sources[index].SourceKey == "grok.billing" {
			billingSource = &readback.Sources[index]
		}
	}
	if billingSource == nil || billingSource.State != "available" {
		t.Fatalf("billing source after local replacement = %#v", billingSource)
	}
}

func TestGrokBillingFailurePreservesLastSuccess(t *testing.T) {
	t.Parallel()
	repository := openRuntimeRepository(t)
	ctx := context.Background()
	if err := repository.ReplaceGrokSnapshot(ctx, GrokSnapshot{Generation: 1, CollectedAtMS: 2_000}); err != nil {
		t.Fatalf("ReplaceGrokSnapshot() error = %v", err)
	}
	if err := repository.CommitGrokBillingSnapshot(ctx, GrokBillingSnapshot{
		Generation: 1, CollectedAtMS: 2_000, PeriodType: "monthly",
		PeriodStartMS: 1_000, PeriodEndMS: 9_000, UsedPercent: 12,
	}); err != nil {
		t.Fatalf("CommitGrokBillingSnapshot() error = %v", err)
	}
	if err := repository.RecordGrokBillingFailure(ctx, 3_000, "auth_expired"); err != nil {
		t.Fatalf("RecordGrokBillingFailure() error = %v", err)
	}
	readback, err := repository.GrokSnapshot(ctx)
	if err != nil {
		t.Fatalf("GrokSnapshot() error = %v", err)
	}
	if readback.Billing == nil || readback.Billing.UsedPercent != 12 {
		t.Fatalf("billing after failure = %#v", readback.Billing)
	}
	if !readback.BillingStale || readback.BillingFailureCode == nil || *readback.BillingFailureCode != "auth_expired" {
		t.Fatalf("failure flags = stale=%v code=%v", readback.BillingStale, readback.BillingFailureCode)
	}
}

func TestGrokBillingRefreshPreservesEarlierQuotaObservations(t *testing.T) {
	t.Parallel()
	repository := openRuntimeRepository(t)
	ctx := context.Background()
	cycleStart := int64(1_780_000_000_000)
	cycleEnd := cycleStart + int64(7*24*60*60*1_000)
	firstObservedAt := cycleStart + int64(2*60*60*1_000)
	latestObservedAt := cycleStart + int64(3*24*60*60*1_000)
	if err := repository.ReplaceGrokSnapshot(ctx, GrokSnapshot{
		Generation: 1, CollectedAtMS: cycleStart,
	}); err != nil {
		t.Fatalf("ReplaceGrokSnapshot() error = %v", err)
	}

	for _, snapshot := range []GrokBillingSnapshot{
		{
			Generation: firstObservedAt, CollectedAtMS: firstObservedAt,
			PeriodType: "weekly", PeriodStartMS: cycleStart, PeriodEndMS: cycleEnd,
			UsedPercent: 44,
			QuotaObservations: []GrokBillingQuotaObservation{{
				Generation: firstObservedAt, LimitID: "grok.included_credits", UsedPercent: 44,
				CycleStartAtMS: cycleStart, CycleEndAtMS: cycleEnd, ObservedAtMS: firstObservedAt,
			}},
		},
		{
			Generation: latestObservedAt, CollectedAtMS: latestObservedAt,
			PeriodType: "weekly", PeriodStartMS: cycleStart, PeriodEndMS: cycleEnd,
			UsedPercent: 44,
			QuotaObservations: []GrokBillingQuotaObservation{{
				Generation: latestObservedAt, LimitID: "grok.included_credits", UsedPercent: 44,
				CycleStartAtMS: cycleStart, CycleEndAtMS: cycleEnd, ObservedAtMS: latestObservedAt,
			}},
		},
	} {
		if err := repository.CommitGrokBillingSnapshot(ctx, snapshot); err != nil {
			t.Fatalf("CommitGrokBillingSnapshot() error = %v", err)
		}
	}

	readback, err := repository.GrokSnapshot(ctx)
	if err != nil {
		t.Fatalf("GrokSnapshot() error = %v", err)
	}
	if readback.Billing == nil || len(readback.Billing.QuotaObservations) != 2 {
		t.Fatalf("quota observations = %#v, want both refresh samples", readback.Billing)
	}
	if readback.Billing.QuotaObservations[0].ObservedAtMS != firstObservedAt ||
		readback.Billing.QuotaObservations[1].ObservedAtMS != latestObservedAt {
		t.Fatalf("quota observation times = %#v", readback.Billing.QuotaObservations)
	}
}

func TestGrokBillingRefreshRetainsOnlyFiveQuotaCycles(t *testing.T) {
	t.Parallel()
	repository := openRuntimeRepository(t)
	ctx := context.Background()
	firstCycleStart := int64(1_780_000_000_000)
	weekMS := int64(7 * 24 * 60 * 60 * 1_000)
	if err := repository.ReplaceGrokSnapshot(ctx, GrokSnapshot{
		Generation: 1, CollectedAtMS: firstCycleStart,
	}); err != nil {
		t.Fatalf("ReplaceGrokSnapshot() error = %v", err)
	}

	for cycle := int64(0); cycle < 6; cycle++ {
		cycleStart := firstCycleStart + cycle*weekMS
		cycleEnd := cycleStart + weekMS
		observedAt := cycleStart + int64(60*60*1_000)
		snapshot := GrokBillingSnapshot{
			Generation: observedAt, CollectedAtMS: observedAt,
			PeriodType: "weekly", PeriodStartMS: cycleStart, PeriodEndMS: cycleEnd,
			UsedPercent: float64(cycle),
			QuotaObservations: []GrokBillingQuotaObservation{{
				Generation: observedAt, LimitID: "grok.included_credits", UsedPercent: float64(cycle),
				CycleStartAtMS: cycleStart, CycleEndAtMS: cycleEnd, ObservedAtMS: observedAt,
			}},
		}
		if err := repository.CommitGrokBillingSnapshot(ctx, snapshot); err != nil {
			t.Fatalf("CommitGrokBillingSnapshot() error = %v", err)
		}
	}

	readback, err := repository.GrokSnapshot(ctx)
	if err != nil {
		t.Fatalf("GrokSnapshot() error = %v", err)
	}
	if readback.Billing == nil || len(readback.Billing.QuotaObservations) != 5 {
		t.Fatalf("quota observations = %#v, want five retained cycles", readback.Billing)
	}
	if readback.Billing.QuotaObservations[0].CycleStartAtMS != firstCycleStart+weekMS ||
		readback.Billing.QuotaObservations[4].CycleStartAtMS != firstCycleStart+5*weekMS {
		t.Fatalf("retained quota cycles = %#v", readback.Billing.QuotaObservations)
	}
}
