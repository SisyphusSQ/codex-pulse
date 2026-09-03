package cursorprovider

import (
	"context"
	"testing"
	"time"

	basequery "github.com/SisyphusSQ/codex-pulse/internal/query"
	"github.com/SisyphusSQ/codex-pulse/internal/query/usagecost"
	"github.com/SisyphusSQ/codex-pulse/internal/store"
)

func TestCursorCrossDayUsageFollowsEventTimeNotSessionCreatedAt(t *testing.T) {
	t.Parallel()

	shanghai := time.FixedZone("CST", 8*3600)
	d1Start := time.Date(2026, time.September, 2, 0, 0, 0, 0, shanghai)
	d2Start := time.Date(2026, time.September, 3, 0, 0, 0, 0, shanghai)
	d3Start := time.Date(2026, time.September, 4, 0, 0, 0, 0, shanghai)
	d1Event := time.Date(2026, time.September, 2, 23, 0, 0, 0, shanghai)
	d2Event := time.Date(2026, time.September, 3, 1, 0, 0, 0, shanghai)
	model := "cursor-model"
	sessionID := "cross-day"
	snapshot := store.CursorSnapshot{
		Generation: 3, CollectedAtMS: d2Event.UnixMilli(),
		Sources: []store.CursorSourceStatus{{
			Provider: "cursor", SourceKey: SourceState, SourceType: "sqlite_snapshot",
			State: "available", CoverageState: "exact",
		}},
		Sessions: []store.CursorSession{{
			ExternalSessionID: sessionID, DisplayTitle: "跨日会话", TitleSource: "cursor_composer_header",
			ProjectKey: "project-a", ProjectDisplayName: "Project A",
			CreatedAtMS: d1Start.UnixMilli(), LastActivityAtMS: d2Event.UnixMilli(),
			ModelKey: &model, RequestCount: 2, CoverageState: "exact",
		}},
		RequestEvents: []store.CursorRequestEvent{
			{EventID: "d1", ExternalSessionID: sessionID, OccurredAtMS: d1Event.UnixMilli()},
			{EventID: "d2", ExternalSessionID: sessionID, OccurredAtMS: d2Event.UnixMilli()},
		},
		UsageEvents: []store.CursorUsageEvent{
			{EventID: "d1", ExternalSessionID: sessionID, OccurredAtMS: d1Event.UnixMilli(), ModelKey: &model, InputTokens: 100, OutputTokens: 20},
			{EventID: "d2", ExternalSessionID: sessionID, OccurredAtMS: d2Event.UnixMilli(), ModelKey: &model, InputTokens: 40, OutputTokens: 10},
		},
	}
	service := cursorQueryFixture(t, snapshot)
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
	detail, err := service.SessionDetail(context.Background(), usagecost.SessionDetailRequest{
		SessionID: sessionID, ReportingTimezone: pointer("Asia/Shanghai"), TurnPage: basequery.PageRequest{Limit: 10},
	})
	if err != nil || detail.Item.Totals.TotalTokens.Value == nil || *detail.Item.Totals.TotalTokens.Value != 170 {
		t.Fatalf("session detail lifecycle totals = %#v, %v", detail.Item, err)
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
}

func TestCursorPartialCoverageStaysPartialAcrossDays(t *testing.T) {
	t.Parallel()

	shanghai := time.FixedZone("CST", 8*3600)
	d2Start := time.Date(2026, time.September, 3, 0, 0, 0, 0, shanghai)
	d3Start := time.Date(2026, time.September, 4, 0, 0, 0, 0, shanghai)
	d2Event := time.Date(2026, time.September, 3, 1, 0, 0, 0, shanghai)
	snapshot := store.CursorSnapshot{
		Generation: 4, CollectedAtMS: d2Event.UnixMilli(),
		Sources: []store.CursorSourceStatus{{
			Provider: "cursor", SourceKey: SourceState, SourceType: "sqlite_snapshot",
			State: "available", CoverageState: "partial",
		}},
		Sessions: []store.CursorSession{{
			ExternalSessionID: "partial-session", DisplayTitle: "部分覆盖", TitleSource: "fallback",
			ProjectKey: "project-a", ProjectDisplayName: "Project A",
			CreatedAtMS: d2Start.UnixMilli(), LastActivityAtMS: d2Event.UnixMilli(),
			RequestCount: 1, CoverageState: "partial",
		}},
		UsageEvents: []store.CursorUsageEvent{{
			EventID: "d2", ExternalSessionID: "partial-session", OccurredAtMS: d2Event.UnixMilli(),
			InputTokens: 40, OutputTokens: 10,
		}},
	}
	service := cursorQueryFixture(t, snapshot)
	today := basequery.UTCTimeRange{StartAtMS: d2Start.UnixMilli(), EndAtMS: d3Start.UnixMilli(), TimeZone: "Asia/Shanghai"}
	usage, err := service.UsageCost(context.Background(), usagecost.UsageCostRequest{
		ExactRange: &today, Granularity: usagecost.TrendDay,
	})
	if err != nil {
		t.Fatalf("UsageCost() error = %v", err)
	}
	if usage.Totals.TotalTokens.Value != nil {
		t.Fatalf("partial coverage leaked exact totals: %#v", usage.Totals)
	}
}

func TestCursorLocalAndDashboardEventsAreNotDoubleCountedAcrossDays(t *testing.T) {
	t.Parallel()

	shanghai := time.FixedZone("CST", 8*3600)
	d1Event := time.Date(2026, time.September, 2, 23, 0, 0, 0, shanghai)
	d2Start := time.Date(2026, time.September, 3, 0, 0, 0, 0, shanghai)
	d2Event := time.Date(2026, time.September, 3, 1, 0, 0, 0, shanghai)
	d3Start := time.Date(2026, time.September, 4, 0, 0, 0, 0, shanghai)
	model := "cursor-model"
	sessionID := "dedup-session"
	snapshot := store.CursorSnapshot{
		Generation: 5, CollectedAtMS: d2Event.UnixMilli(),
		DashboardGeneration: 9, DashboardCollectedAtMS: d2Event.UnixMilli(),
		DashboardWindowStartMS: d1Event.UnixMilli(), DashboardWindowEndMS: d3Start.UnixMilli(),
		Sources: []store.CursorSourceStatus{
			{Provider: "cursor", SourceKey: SourceState, SourceType: "sqlite_snapshot", State: "available", CoverageState: "exact"},
			{Provider: "cursor", SourceKey: SourceDashboard, SourceType: "desktop_authenticated_rpc", State: "available", CoverageState: "exact"},
		},
		Sessions: []store.CursorSession{{
			ExternalSessionID: sessionID, DisplayTitle: "去重会话", TitleSource: "cursor_composer_header",
			ProjectKey: "project-a", ProjectDisplayName: "Project A",
			CreatedAtMS: d1Event.UnixMilli(), LastActivityAtMS: d2Event.UnixMilli(),
			ModelKey: &model, RequestCount: 2, CoverageState: "exact",
		}},
		UsageEvents: []store.CursorUsageEvent{
			{EventID: "d1", ExternalSessionID: sessionID, OccurredAtMS: d1Event.UnixMilli(), ModelKey: &model, InputTokens: 100, OutputTokens: 20},
			{EventID: "d2", ExternalSessionID: sessionID, OccurredAtMS: d2Event.UnixMilli(), ModelKey: &model, InputTokens: 40, OutputTokens: 10},
		},
		DashboardUsageEvents: []store.CursorDashboardUsageEvent{
			{EventFingerprint: "d1", OccurrenceCount: 1, ExternalSessionID: &sessionID, OccurredAtMS: d1Event.UnixMilli(), ModelKey: &model, InputTokens: 100, OutputTokens: 20},
			{EventFingerprint: "d2", OccurrenceCount: 1, ExternalSessionID: &sessionID, OccurredAtMS: d2Event.UnixMilli(), ModelKey: &model, InputTokens: 40, OutputTokens: 10},
			{EventFingerprint: "orphan", OccurrenceCount: 1, OccurredAtMS: d2Event.UnixMilli(), ModelKey: &model, InputTokens: 999, OutputTokens: 1},
		},
	}
	service := cursorQueryFixture(t, snapshot)
	today := basequery.UTCTimeRange{StartAtMS: d2Start.UnixMilli(), EndAtMS: d3Start.UnixMilli(), TimeZone: "Asia/Shanghai"}
	usage, err := service.UsageCost(context.Background(), usagecost.UsageCostRequest{
		ExactRange: &today, Granularity: usagecost.TrendDay,
	})
	if err != nil {
		t.Fatalf("UsageCost() error = %v", err)
	}
	if usage.Totals.TotalTokens.Value == nil || *usage.Totals.TotalTokens.Value != 1_050 {
		t.Fatalf("dashboard today usage = %#v, want D2 dashboard tokens without local double count", usage.Totals)
	}

	sessions, err := service.ListSessions(context.Background(), basequery.Request{
		Page: basequery.PageRequest{Limit: 10}, ExactTimeRange: &today,
		Sort: []basequery.SortTerm{{Field: "lastActivityAt", Direction: basequery.SortDescending}},
	})
	if err != nil || len(sessions.Items) != 1 {
		t.Fatalf("ListSessions() = %#v, %v", sessions, err)
	}
	if sessions.Items[0].Totals.TotalTokens.Value == nil || *sessions.Items[0].Totals.TotalTokens.Value != 170 {
		t.Fatalf("session lifecycle totals = %#v, want D1+D2 dashboard tokens", sessions.Items[0].Totals)
	}
	detail, err := service.SessionDetail(context.Background(), usagecost.SessionDetailRequest{
		SessionID: sessionID, ReportingTimezone: pointer("Asia/Shanghai"), TurnPage: basequery.PageRequest{Limit: 10},
	})
	if err != nil || detail.Item.Totals.TotalTokens.Value == nil || *detail.Item.Totals.TotalTokens.Value != 170 {
		t.Fatalf("dashboard session detail lifecycle totals = %#v, %v", detail.Item, err)
	}

	projects, err := service.ListProjects(context.Background(), basequery.Request{
		Page: basequery.PageRequest{Limit: 10}, ExactTimeRange: &today,
		Sort: []basequery.SortTerm{{Field: "lastActivityAt", Direction: basequery.SortDescending}},
	})
	if err != nil || len(projects.Items) != 1 {
		t.Fatalf("ListProjects() = %#v, %v", projects, err)
	}
	if projects.Items[0].Totals.TotalTokens.Value == nil || *projects.Items[0].Totals.TotalTokens.Value != 50 {
		t.Fatalf("project totals = %#v, unattributed dashboard event must not invent a project", projects.Items[0].Totals)
	}
}

func TestCursorSessionTokenSortUsesSamePerSessionSourceAsItems(t *testing.T) {
	t.Parallel()

	model := "cursor-model"
	sessionA, sessionB := "dashboard-session", "local-session"
	snapshot := store.CursorSnapshot{
		Generation: 6, CollectedAtMS: 5_000,
		DashboardGeneration: 9, DashboardCollectedAtMS: 5_000,
		DashboardWindowStartMS: 1_000, DashboardWindowEndMS: 5_000,
		Sources: []store.CursorSourceStatus{
			{Provider: "cursor", SourceKey: SourceState, SourceType: "sqlite_snapshot", State: "available", CoverageState: "exact"},
			{Provider: "cursor", SourceKey: SourceDashboard, SourceType: "desktop_authenticated_rpc", State: "available", CoverageState: "exact"},
		},
		Sessions: []store.CursorSession{
			{ExternalSessionID: sessionA, ProjectKey: "project-a", ProjectDisplayName: "Project A", CreatedAtMS: 1_000, LastActivityAtMS: 4_000, RequestCount: 1, CoverageState: "exact"},
			{ExternalSessionID: sessionB, ProjectKey: "project-b", ProjectDisplayName: "Project B", CreatedAtMS: 1_000, LastActivityAtMS: 3_000, RequestCount: 1, CoverageState: "exact"},
		},
		UsageEvents: []store.CursorUsageEvent{
			{EventID: "local-a", ExternalSessionID: sessionA, OccurredAtMS: 4_000, ModelKey: &model, InputTokens: 10},
			{EventID: "local-b", ExternalSessionID: sessionB, OccurredAtMS: 3_000, ModelKey: &model, InputTokens: 200},
		},
		DashboardUsageEvents: []store.CursorDashboardUsageEvent{{
			EventFingerprint: "dashboard-a", OccurrenceCount: 1, ExternalSessionID: &sessionA,
			OccurredAtMS: 4_000, ModelKey: &model, InputTokens: 10,
		}},
	}
	service := cursorQueryFixture(t, snapshot)
	rangeValue := basequery.UTCTimeRange{StartAtMS: 1_000, EndAtMS: 5_000, TimeZone: "UTC"}
	response, err := service.ListSessions(context.Background(), basequery.Request{
		Page: basequery.PageRequest{Limit: 10}, ExactTimeRange: &rangeValue,
		Sort: []basequery.SortTerm{{Field: "totalTokens", Direction: basequery.SortDescending}},
	})
	if err != nil {
		t.Fatalf("ListSessions() error = %v", err)
	}
	if len(response.Items) != 2 || response.Items[0].SessionID != sessionB ||
		response.Items[0].Totals.TotalTokens.Value == nil || *response.Items[0].Totals.TotalTokens.Value != 200 ||
		response.Items[1].SessionID != sessionA || response.Items[1].Totals.TotalTokens.Value == nil ||
		*response.Items[1].Totals.TotalTokens.Value != 10 {
		t.Fatalf("mixed-source session sort/items = %#v", response.Items)
	}
}

func TestCursorSessionListMarksIncompleteDashboardLifecyclePartial(t *testing.T) {
	t.Parallel()

	sessionID := "older-session"
	snapshot := store.CursorSnapshot{
		Generation: 7, CollectedAtMS: 5_000,
		DashboardGeneration: 9, DashboardCollectedAtMS: 5_000,
		DashboardWindowStartMS: 2_000, DashboardWindowEndMS: 5_000,
		Sources: []store.CursorSourceStatus{
			{Provider: "cursor", SourceKey: SourceState, SourceType: "sqlite_snapshot", State: "available", CoverageState: "exact"},
			{Provider: "cursor", SourceKey: SourceDashboard, SourceType: "desktop_authenticated_rpc", State: "available", CoverageState: "exact"},
		},
		Sessions: []store.CursorSession{{
			ExternalSessionID: sessionID, ProjectKey: "project-a", ProjectDisplayName: "Project A",
			CreatedAtMS: 1_000, LastActivityAtMS: 4_000, RequestCount: 2, CoverageState: "exact",
		}},
		UsageEvents: []store.CursorUsageEvent{
			{EventID: "before-window", ExternalSessionID: sessionID, OccurredAtMS: 1_500, InputTokens: 100},
			{EventID: "in-window", ExternalSessionID: sessionID, OccurredAtMS: 4_000, InputTokens: 20},
		},
		DashboardUsageEvents: []store.CursorDashboardUsageEvent{{
			EventFingerprint: "in-window", OccurrenceCount: 1, ExternalSessionID: &sessionID,
			OccurredAtMS: 4_000, InputTokens: 20,
		}},
	}
	service := cursorQueryFixture(t, snapshot)
	rangeValue := basequery.UTCTimeRange{StartAtMS: 2_000, EndAtMS: 5_000, TimeZone: "UTC"}
	response, err := service.ListSessions(context.Background(), basequery.Request{
		Page: basequery.PageRequest{Limit: 10}, ExactTimeRange: &rangeValue,
		Sort: []basequery.SortTerm{{Field: "lastActivityAt", Direction: basequery.SortDescending}},
	})
	if err != nil {
		t.Fatalf("ListSessions() error = %v", err)
	}
	if response.Meta.Status != basequery.ResponsePartial || len(response.Items) != 1 ||
		response.Items[0].Totals.TotalTokens.Value == nil || *response.Items[0].Totals.TotalTokens.Value != 20 {
		t.Fatalf("incomplete dashboard lifecycle response = %#v", response)
	}
}
