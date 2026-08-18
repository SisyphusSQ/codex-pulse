package grokprovider

import (
	"context"
	"testing"
	"time"

	"github.com/SisyphusSQ/codex-pulse/internal/agentprovider"
	basequery "github.com/SisyphusSQ/codex-pulse/internal/query"
	"github.com/SisyphusSQ/codex-pulse/internal/query/invocationusage"
	"github.com/SisyphusSQ/codex-pulse/internal/query/usagecost"
	"github.com/SisyphusSQ/codex-pulse/internal/store"
)

type staticSnapshot struct {
	snapshot store.GrokSnapshot
}

func (reader staticSnapshot) GrokSnapshot(context.Context) (store.GrokSnapshot, error) {
	return reader.snapshot, nil
}

func TestQueryServiceReturnsGrokProviderContextAndSeparateCosts(t *testing.T) {
	t.Parallel()
	model := "grok-4.6"
	reported := int64(1_500)
	snapshot := store.GrokSnapshot{
		Generation: 10, CollectedAtMS: 2_000,
		Sources: []store.CursorSourceStatus{{
			Provider: "grok", SourceKey: SourceSummary, SourceType: "filesystem_scan",
			State: "available", CoverageState: "exact", CheckpointKind: "filesystem_scan",
			RowCount: 1, LastAttemptAtMS: 2_000, LastSuccessAtMS: pointerInt64(2_000), UpdatedAtMS: 2_000,
		}},
		Sessions: []store.GrokSession{{
			ExternalSessionID: "session-1", DisplayTitle: "Plan", TitleSource: "grok_summary",
			ProjectKey:         "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			ProjectDisplayName: "demo", CreatedAtMS: 1_000, LastActivityAtMS: 1_500,
			ModelKey: &model, RequestCount: 1, ToolCallCount: 1, CoverageState: "exact", UpdatedAtMS: 2_000,
		}},
		UsageEvents: []store.GrokUsageEvent{{
			EventID: "prompt-1", ExternalSessionID: "session-1", OccurredAtMS: 1_200,
			ModelKey: &model, InputTokens: 100, OutputTokens: 20, CachedReadTokens: 40,
			TotalTokens: 120, ReportedCostMicros: &reported, UpdatedAtMS: 2_000,
		}},
		ToolEvents: []store.GrokToolEvent{{
			EventID:           "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
			ExternalSessionID: "session-1", OccurredAtMS: 1_300, ToolName: "read_file",
			Outcome: "succeeded", UpdatedAtMS: 2_000,
		}},
	}
	collector, err := NewCollector(&snapshotCapture{snapshot: snapshot}, Config{
		SessionsRoot: t.TempDir(), Now: func() time.Time { return time.UnixMilli(3_000) },
		MinimumRefresh: time.Hour,
	})
	if err != nil {
		t.Fatal(err)
	}
	service, err := NewQueryService(collector, staticSnapshot{snapshot: snapshot})
	if err != nil {
		t.Fatal(err)
	}
	usage, err := service.UsageCost(context.Background(), usagecost.UsageCostRequest{
		ExactRange:  &basequery.UTCTimeRange{StartAtMS: 0, EndAtMS: 10_000, TimeZone: "UTC"},
		Granularity: usagecost.TrendDay,
	})
	if err != nil || usage.ProviderContext.EffectiveProvider != agentprovider.Grok {
		t.Fatalf("usage = %#v, %v", usage, err)
	}
	if usage.ReportedUSDMicros == nil || usage.ReportedUSDMicros.Value == nil || *usage.ReportedUSDMicros.Value != 1_500 {
		t.Fatalf("reported = %#v", usage.ReportedUSDMicros)
	}
	if usage.Totals.EstimatedUSDMicros.Value == nil || *usage.Totals.EstimatedUSDMicros.Value == 1_500 {
		t.Fatalf("estimated should be independent of reported: %#v", usage.Totals.EstimatedUSDMicros)
	}
	sessions, err := service.ListSessions(context.Background(), basequery.Request{
		ExactTimeRange: &basequery.UTCTimeRange{StartAtMS: 0, EndAtMS: 10_000, TimeZone: "UTC"},
	})
	if err != nil || len(sessions.Items) != 1 || sessions.Items[0].DisplayTitle != "Plan" {
		t.Fatalf("sessions = %#v, %v", sessions, err)
	}
	invocation, err := service.InvocationUsage(context.Background(), invocationusage.InvocationUsageRequest{
		Range:       basequery.UTCTimeRange{StartAtMS: 0, EndAtMS: 10_000, TimeZone: "UTC"},
		Granularity: invocationusage.GranularityDay, SourceClass: invocationusage.SourceClassAll,
	})
	if err != nil || invocation.ProviderContext.EffectiveProvider != agentprovider.Grok ||
		len(invocation.Tools) != 1 || invocation.Tools[0].Name != "read_file" {
		t.Fatalf("invocation = %#v, %v", invocation, err)
	}
}

func TestBillingDecoderFailClosedOnDrift(t *testing.T) {
	t.Parallel()
	if _, err := decodeBillingCredits([]byte(`{"creditUsagePercent":-1}`)); err == nil {
		t.Fatal("negative percent should fail closed")
	}
	if _, err := decodeBillingCredits([]byte(`{"used":1,"monthlyLimit":0}`)); err == nil {
		t.Fatal("zero monthly limit should fail closed")
	}
	credits, err := decodeBillingCredits([]byte(`{
		"creditUsagePercent": 37.5,
		"currentPeriod": {"type":"USAGE_PERIOD_TYPE_WEEKLY","start":1787011200,"end":1787616000},
		"subscriptionTier": "SuperGrok"
	}`))
	if err != nil || credits.PeriodType != "weekly" || credits.UsedPercent != 37.5 ||
		credits.SubscriptionTier == nil || *credits.SubscriptionTier != "SuperGrok" {
		t.Fatalf("credits = %#v, %v", credits, err)
	}
}

func TestBillingDecoderAcceptsOfficialConfigEnvelope(t *testing.T) {
	t.Parallel()
	credits, err := decodeBillingCredits([]byte(`{
		"config": {
			"creditUsagePercent": 42.5,
			"currentPeriod": {
				"type": "USAGE_PERIOD_TYPE_MONTHLY",
				"start": "2026-08-01T00:00:00Z",
				"end": "2026-09-01T00:00:00Z"
			},
			"onDemandCap": {"val": 5000},
			"onDemandUsed": {"val": 1250},
			"prepaidBalance": {"val": 300},
			"isUnifiedBillingUser": true
		},
		"subscriptionTier": "SuperGrok Heavy"
	}`))
	if err != nil {
		t.Fatal(err)
	}
	if credits.PeriodType != "monthly" || credits.UsedPercent != 42.5 ||
		credits.OnDemandCap == nil || *credits.OnDemandCap != 5000 ||
		credits.OnDemandUsed == nil || *credits.OnDemandUsed != 1250 ||
		credits.PrepaidBalance == nil || *credits.PrepaidBalance != 300 ||
		!credits.IsUnifiedBilling || credits.SubscriptionTier == nil ||
		*credits.SubscriptionTier != "SuperGrok Heavy" {
		t.Fatalf("credits = %#v", credits)
	}
	percent, ok := onDemandUsedPercent(credits)
	if !ok || percent != 25 {
		t.Fatalf("on-demand percent = %v, %t", percent, ok)
	}
}

func pointerInt64(value int64) *int64 { return &value }
