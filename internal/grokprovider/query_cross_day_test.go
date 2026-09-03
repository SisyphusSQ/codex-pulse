package grokprovider

import (
	"context"
	"testing"
	"time"

	basequery "github.com/SisyphusSQ/codex-pulse/internal/query"
	"github.com/SisyphusSQ/codex-pulse/internal/query/usagecost"
	"github.com/SisyphusSQ/codex-pulse/internal/store"
)

func TestGrokCrossDayUsageFollowsEventTimeNotSessionCreatedAt(t *testing.T) {
	t.Parallel()

	shanghai := time.FixedZone("CST", 8*3600)
	d1Start := time.Date(2026, time.September, 2, 0, 0, 0, 0, shanghai)
	d2Start := time.Date(2026, time.September, 3, 0, 0, 0, 0, shanghai)
	d3Start := time.Date(2026, time.September, 4, 0, 0, 0, 0, shanghai)
	d1Event := time.Date(2026, time.September, 2, 23, 0, 0, 0, shanghai)
	d2Event := time.Date(2026, time.September, 3, 1, 0, 0, 0, shanghai)
	model := "grok-4.6"
	periodStart := time.Date(2026, time.August, 31, 0, 0, 0, 0, time.UTC).UnixMilli()
	periodEnd := time.Date(2026, time.September, 7, 0, 0, 0, 0, time.UTC).UnixMilli()
	snapshot := store.GrokSnapshot{
		Generation: 8, CollectedAtMS: d2Event.UnixMilli(),
		Sources: []store.CursorSourceStatus{{
			Provider: "grok", SourceKey: SourceSummary, SourceType: "filesystem_scan",
			State: "available", CoverageState: "exact", CheckpointKind: "filesystem_scan",
			RowCount: 1, LastAttemptAtMS: d2Event.UnixMilli(), LastSuccessAtMS: pointerInt64(d2Event.UnixMilli()),
			UpdatedAtMS: d2Event.UnixMilli(),
		}},
		Sessions: []store.GrokSession{{
			ExternalSessionID: "cross-day", DisplayTitle: "跨日会话", TitleSource: "grok_summary",
			ProjectKey:         "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			ProjectDisplayName: "demo", CreatedAtMS: d1Start.UnixMilli(), LastActivityAtMS: d2Event.UnixMilli(),
			ModelKey: &model, RequestCount: 2, CoverageState: "exact", UpdatedAtMS: d2Event.UnixMilli(),
		}},
		UsageEvents: []store.GrokUsageEvent{
			{
				EventID: "d1", ExternalSessionID: "cross-day", OccurredAtMS: d1Event.UnixMilli(),
				ModelKey: &model, InputTokens: 100, OutputTokens: 20, TotalTokens: 120, UpdatedAtMS: d2Event.UnixMilli(),
			},
			{
				EventID: "d2", ExternalSessionID: "cross-day", OccurredAtMS: d2Event.UnixMilli(),
				ModelKey: &model, InputTokens: 40, OutputTokens: 10, TotalTokens: 50, UpdatedAtMS: d2Event.UnixMilli(),
			},
		},
		Billing: &store.GrokBillingSnapshot{
			Generation: d2Event.UnixMilli(), CollectedAtMS: d2Event.UnixMilli(),
			PeriodType: "weekly", PeriodStartMS: periodStart, PeriodEndMS: periodEnd, UsedPercent: 44,
		},
	}
	collector, err := NewCollector(&snapshotCapture{snapshot: snapshot}, Config{
		SessionsRoot: t.TempDir(), Now: func() time.Time { return d2Event },
		MinimumRefresh: time.Hour,
	})
	if err != nil {
		t.Fatal(err)
	}
	service, err := NewQueryService(collector, staticSnapshot{snapshot: snapshot})
	if err != nil {
		t.Fatal(err)
	}
	today := basequery.UTCTimeRange{StartAtMS: d2Start.UnixMilli(), EndAtMS: d3Start.UnixMilli(), TimeZone: "Asia/Shanghai"}
	usage, err := service.UsageCost(context.Background(), usagecost.UsageCostRequest{
		ExactRange: &today, Granularity: usagecost.TrendDay,
	})
	if err != nil {
		t.Fatalf("UsageCost() error = %v", err)
	}
	if usage.Totals.TotalTokens.Value == nil || *usage.Totals.TotalTokens.Value != 50 {
		t.Fatalf("today usage = %#v, want D2 event tokens only", usage.Totals)
	}
	if len(usage.Trend) != 1 || usage.Trend[0].Totals.TotalTokens.Value == nil ||
		*usage.Trend[0].Totals.TotalTokens.Value != 50 {
		t.Fatalf("today trend = %#v, want D2 bucket only", usage.Trend)
	}

	sessions, err := service.ListSessions(context.Background(), basequery.Request{
		Page: basequery.PageRequest{Limit: 10}, ExactTimeRange: &today,
		Sort: []basequery.SortTerm{{Field: "lastActivityAt", Direction: basequery.SortDescending}},
	})
	if err != nil || len(sessions.Items) != 1 {
		t.Fatalf("ListSessions() = %#v, %v", sessions, err)
	}
	if sessions.Items[0].Totals.TotalTokens.Value == nil || *sessions.Items[0].Totals.TotalTokens.Value != 170 {
		t.Fatalf("session lifecycle totals = %#v, want D1+D2", sessions.Items[0].Totals)
	}

	projects, err := service.ListProjects(context.Background(), basequery.Request{
		Page: basequery.PageRequest{Limit: 10}, ExactTimeRange: &today,
		Sort: []basequery.SortTerm{{Field: "lastActivityAt", Direction: basequery.SortDescending}},
	})
	if err != nil || len(projects.Items) != 1 {
		t.Fatalf("ListProjects() = %#v, %v", projects, err)
	}
	if projects.Items[0].Totals.TotalTokens.Value == nil || *projects.Items[0].Totals.TotalTokens.Value != 50 {
		t.Fatalf("project today totals = %#v, want attributable D2 events", projects.Items[0].Totals)
	}

	quota, err := service.QuotaCurrent(context.Background(), d2Event.UnixMilli())
	if err != nil || len(quota.Current.Windows) == 0 {
		t.Fatalf("QuotaCurrent() = %#v, %v", quota, err)
	}
	window := quota.Current.Windows[0]
	if window.ResetsAtMS == nil || *window.ResetsAtMS != periodEnd {
		t.Fatalf("billing window followed today query: %#v", window)
	}
}

func TestGrokPartialAndProviderErrorsStayIsolated(t *testing.T) {
	t.Parallel()

	shanghai := time.FixedZone("CST", 8*3600)
	d2Start := time.Date(2026, time.September, 3, 0, 0, 0, 0, shanghai)
	d3Start := time.Date(2026, time.September, 4, 0, 0, 0, 0, shanghai)
	d2Event := time.Date(2026, time.September, 3, 1, 0, 0, 0, shanghai)
	snapshot := store.GrokSnapshot{
		Generation: 9, CollectedAtMS: d2Event.UnixMilli(), BillingStale: true,
		Sources: []store.CursorSourceStatus{{
			Provider: "grok", SourceKey: SourceSummary, SourceType: "filesystem_scan",
			State: "unavailable", CoverageState: "unknown", CheckpointKind: "filesystem_scan",
			UpdatedAtMS: d2Event.UnixMilli(),
		}},
		Billing: &store.GrokBillingSnapshot{
			Generation: d2Event.UnixMilli(), CollectedAtMS: d2Event.UnixMilli(),
			PeriodType: "weekly", PeriodStartMS: d2Start.UnixMilli(), PeriodEndMS: d3Start.UnixMilli(),
			UsedPercent: 12,
		},
	}
	collector, err := NewCollector(&snapshotCapture{snapshot: snapshot}, Config{
		SessionsRoot: t.TempDir(), Now: func() time.Time { return d2Event }, MinimumRefresh: time.Hour,
	})
	if err != nil {
		t.Fatal(err)
	}
	service, err := NewQueryService(collector, staticSnapshot{snapshot: snapshot})
	if err != nil {
		t.Fatal(err)
	}
	usage, err := service.UsageCost(context.Background(), usagecost.UsageCostRequest{
		ExactRange:  &basequery.UTCTimeRange{StartAtMS: d2Start.UnixMilli(), EndAtMS: d3Start.UnixMilli(), TimeZone: "Asia/Shanghai"},
		Granularity: usagecost.TrendDay,
	})
	if err != nil {
		t.Fatalf("UsageCost() error = %v", err)
	}
	if usage.Meta.Status != basequery.ResponsePartial {
		t.Fatalf("stale billing must stay partial: %#v", usage.Meta)
	}
	if usage.ProviderContext.EffectiveProvider != "grok" {
		t.Fatalf("provider context leaked: %#v", usage.ProviderContext)
	}
}
