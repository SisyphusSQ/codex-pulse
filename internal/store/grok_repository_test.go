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
