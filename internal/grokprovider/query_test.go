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

func TestGrokQuotaPaceUsesPersistedBillingObservationTimes(t *testing.T) {
	t.Parallel()
	cycleStart := int64(1_780_000_000_000)
	cycleEnd := cycleStart + int64(7*24*60*60*1_000)
	consumedAt := cycleStart + int64(2*60*60*1_000)
	evaluatedAt := cycleStart + int64(3*24*60*60*1_000)
	snapshot := store.GrokSnapshot{
		Generation: evaluatedAt, CollectedAtMS: evaluatedAt,
		Billing: &store.GrokBillingSnapshot{
			Generation: evaluatedAt, CollectedAtMS: evaluatedAt,
			PeriodType: "weekly", PeriodStartMS: cycleStart, PeriodEndMS: cycleEnd,
			UsedPercent: 44,
			QuotaObservations: []store.GrokBillingQuotaObservation{
				{
					Generation: consumedAt, LimitID: grokCreditsLimitID, UsedPercent: 44,
					CycleStartAtMS: cycleStart, CycleEndAtMS: cycleEnd, ObservedAtMS: consumedAt,
				},
				{
					Generation: evaluatedAt, LimitID: grokCreditsLimitID, UsedPercent: 44,
					CycleStartAtMS: cycleStart, CycleEndAtMS: cycleEnd, ObservedAtMS: evaluatedAt,
				},
			},
		},
	}
	collector, err := NewCollector(&snapshotCapture{snapshot: snapshot}, Config{
		SessionsRoot: t.TempDir(), Now: func() time.Time { return time.UnixMilli(evaluatedAt) },
		MinimumRefresh: time.Hour,
	})
	if err != nil {
		t.Fatal(err)
	}
	service, err := NewQueryService(collector, staticSnapshot{snapshot: snapshot})
	if err != nil {
		t.Fatal(err)
	}

	response, err := service.QuotaPace(context.Background(), evaluatedAt)
	if err != nil {
		t.Fatalf("QuotaPace() error = %v", err)
	}
	if len(response.Pace.Windows) != 1 {
		t.Fatalf("pace windows = %#v", response.Pace.Windows)
	}
	points := response.Pace.Windows[0].CurrentPoints
	if len(points) != 2 {
		t.Fatalf("current pace points = %#v, want first and latest 44%% samples", points)
	}
	if points[0].ObservedAtMS != consumedAt || points[0].RemainingPercent != 56 ||
		points[1].ObservedAtMS != evaluatedAt || points[1].RemainingPercent != 56 {
		t.Fatalf("current pace points = %#v", points)
	}
}

func TestGrokActivityDistributionClipsPartialHourBucketsToExactRange(t *testing.T) {
	t.Parallel()
	rangeStart := time.Date(2026, time.August, 18, 10, 39, 0, 0, time.UTC).UnixMilli()
	rangeEnd := time.Date(2026, time.August, 18, 11, 15, 0, 0, time.UTC).UnixMilli()
	distribution := activityDistribution([]store.GrokUsageEvent{
		{
			ExternalSessionID: "session-1",
			OccurredAtMS:      time.Date(2026, time.August, 18, 10, 42, 0, 0, time.UTC).UnixMilli(),
			TotalTokens:       10,
		},
		{
			ExternalSessionID: "session-2",
			OccurredAtMS:      time.Date(2026, time.August, 18, 11, 5, 0, 0, time.UTC).UnixMilli(),
			TotalTokens:       20,
		},
	}, basequery.UTCTimeRange{
		StartAtMS: rangeStart, EndAtMS: rangeEnd, TimeZone: "UTC",
	})
	if distribution == nil || len(distribution.Timeline) != 2 {
		t.Fatalf("activity distribution = %#v, want two timeline buckets", distribution)
	}
	first, second := distribution.Timeline[0], distribution.Timeline[1]
	if first.StartAtMS.Value == nil || *first.StartAtMS.Value != rangeStart ||
		first.EndAtMS.Value == nil || *first.EndAtMS.Value != time.Date(2026, time.August, 18, 11, 0, 0, 0, time.UTC).UnixMilli() {
		t.Fatalf("first bucket = %#v, want clipped start at range boundary", first)
	}
	if second.StartAtMS.Value == nil || *second.StartAtMS.Value != time.Date(2026, time.August, 18, 11, 0, 0, 0, time.UTC).UnixMilli() ||
		second.EndAtMS.Value == nil || *second.EndAtMS.Value != rangeEnd {
		t.Fatalf("second bucket = %#v, want clipped end at range boundary", second)
	}
	if first.Metrics.TotalTokens.Value == nil || *first.Metrics.TotalTokens.Value != 10 ||
		first.Metrics.SessionCount.Value == nil || *first.Metrics.SessionCount.Value != 1 ||
		second.Metrics.TotalTokens.Value == nil || *second.Metrics.TotalTokens.Value != 20 ||
		second.Metrics.SessionCount.Value == nil || *second.Metrics.SessionCount.Value != 1 {
		t.Fatalf("timeline metrics = %#v, want reconciled token and session counts", distribution.Timeline)
	}
	if len(distribution.WeekdayHours) != 2 {
		t.Fatalf("weekday-hour cells = %#v, want both populated UTC hours", distribution.WeekdayHours)
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
