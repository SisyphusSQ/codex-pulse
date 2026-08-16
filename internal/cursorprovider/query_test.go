package cursorprovider

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	basequery "github.com/SisyphusSQ/codex-pulse/internal/query"
	"github.com/SisyphusSQ/codex-pulse/internal/query/invocationusage"
	"github.com/SisyphusSQ/codex-pulse/internal/query/usagecost"
	"github.com/SisyphusSQ/codex-pulse/internal/store"
)

type discardSnapshotWriter struct{}

func (discardSnapshotWriter) ReplaceCursorSnapshot(context.Context, store.CursorSnapshot) error {
	return nil
}

type fixedSnapshotReader struct{ snapshot store.CursorSnapshot }

func (reader fixedSnapshotReader) CursorSnapshot(context.Context) (store.CursorSnapshot, error) {
	return reader.snapshot, nil
}

type mutableSnapshotReader struct{ snapshot store.CursorSnapshot }

func (reader *mutableSnapshotReader) CursorSnapshot(context.Context) (store.CursorSnapshot, error) {
	return reader.snapshot, nil
}

type snapshotPublishingRefresher struct{ reader *mutableSnapshotReader }

func (refresher snapshotPublishingRefresher) Refresh(context.Context) error {
	refresher.reader.snapshot.Sources = append(refresher.reader.snapshot.Sources, store.CursorSourceStatus{
		Provider: "cursor", SourceKey: SourceDashboard, SourceType: "desktop_authenticated_rpc",
		State: "available", CoverageState: "exact", LastSuccessAtMS: pointerInt64ForQueryTest(5_000),
	})
	return nil
}

type failingDashboardRefresher struct{}

func (failingDashboardRefresher) Refresh(context.Context) error { return ErrDesktopAuthExpired }

func TestQueryServiceRefreshesDashboardBeforeReadingProviderContext(t *testing.T) {
	root := t.TempDir()
	collector, err := NewCollector(discardSnapshotWriter{}, Config{
		ProjectsRoot: filepath.Join(root, "projects"), StateDatabase: filepath.Join(root, "state.vscdb"),
		ConversationDatabase: filepath.Join(root, "conversation.db"), AITrackingDatabase: filepath.Join(root, "tracking.db"),
		MinimumRefresh: time.Hour, Now: func() time.Time { return time.UnixMilli(5_000) },
	})
	if err != nil {
		t.Fatalf("NewCollector() error = %v", err)
	}
	reader := &mutableSnapshotReader{snapshot: cursorQuerySnapshot("partial")}
	service, err := NewQueryService(collector, reader, snapshotPublishingRefresher{reader: reader})
	if err != nil {
		t.Fatalf("NewQueryService() error = %v", err)
	}
	rangeValue := basequery.UTCTimeRange{StartAtMS: 1_000, EndAtMS: 5_000, TimeZone: "UTC"}
	response, err := service.UsageCost(context.Background(), usagecost.UsageCostRequest{
		ExactRange: &rangeValue, Granularity: usagecost.TrendDay,
	})
	if err != nil {
		t.Fatalf("UsageCost() error = %v", err)
	}
	if !testContainsString(response.ProviderContext.Sources, SourceDashboard) {
		t.Fatalf("provider sources = %v", response.ProviderContext.Sources)
	}
}

func TestCursorUsageKeepsRequestsExactWhenTokenCoverageIsPartial(t *testing.T) {
	service := cursorQueryFixture(t, cursorQuerySnapshot("partial"))
	rangeValue := basequery.UTCTimeRange{StartAtMS: 1_000, EndAtMS: 5_000, TimeZone: "UTC"}
	response, err := service.UsageCost(context.Background(), usagecost.UsageCostRequest{
		ExactRange: &rangeValue, Granularity: usagecost.TrendDay,
	})
	if err != nil {
		t.Fatalf("UsageCost() error = %v", err)
	}
	if response.Totals.TurnCount.Value == nil || *response.Totals.TurnCount.Value != 2 {
		t.Fatalf("request count = %#v, want exact generation count", response.Totals.TurnCount)
	}
	if response.Totals.TotalTokens.Value != nil || response.Totals.TotalTokens.UnknownReason == nil ||
		*response.Totals.TotalTokens.UnknownReason != basequery.UnknownUnavailable {
		t.Fatalf("total tokens = %#v, want unavailable instead of partial sum", response.Totals.TotalTokens)
	}
	if len(response.Trend) != 1 || response.Trend[0].Totals.TotalTokens.Value != nil {
		t.Fatalf("trend = %#v, partial source must not expose an exact-looking bucket", response.Trend)
	}
}

func TestCursorUsagePrefersDashboardWindowOverPartialLocalUsage(t *testing.T) {
	snapshot := cursorQuerySnapshot("partial")
	snapshot.Sources = append(snapshot.Sources, store.CursorSourceStatus{
		Provider: "cursor", SourceKey: SourceDashboard, SourceType: "desktop_authenticated_rpc",
		State: "available", CoverageState: "exact", RowCount: 2,
		LastSuccessAtMS: pointerInt64ForQueryTest(5_000),
	})
	snapshot.DashboardGeneration = 9
	snapshot.DashboardCollectedAtMS = 5_000
	snapshot.DashboardWindowStartMS = 1_000
	snapshot.DashboardWindowEndMS = 5_000
	snapshot.DashboardBillingCycleEndMS = 9_000
	snapshot.DashboardPlanUsage = &store.CursorDashboardPlanUsage{
		TotalSpendMicros: 1_250_000, IncludedSpendMicros: 20_000_000,
		BonusSpendMicros: 5_000_000, RemainingMicros: 23_750_000, LimitMicros: 25_000_000,
	}
	model := "cursor-model"
	sessionID := "session-a"
	snapshot.DashboardUsageEvents = []store.CursorDashboardUsageEvent{{
		EventFingerprint: "dashboard-event", OccurrenceCount: 2, ExternalSessionID: &sessionID,
		OccurredAtMS: 2_000, ModelKey: &model, InputTokens: 10, OutputTokens: 20,
		CacheWriteTokens: 30, CacheReadTokens: 40, ReportedChargeMicros: 15_000, CursorTokenFeeMicros: 2_500,
	}}
	service := cursorQueryFixture(t, snapshot)
	rangeValue := basequery.UTCTimeRange{StartAtMS: 1_000, EndAtMS: 5_000, TimeZone: "UTC"}
	response, err := service.UsageCost(context.Background(), usagecost.UsageCostRequest{
		ExactRange: &rangeValue, Granularity: usagecost.TrendDay,
	})
	if err != nil {
		t.Fatalf("UsageCost() error = %v", err)
	}
	if response.Totals.TurnCount.Value == nil || *response.Totals.TurnCount.Value != 2 {
		t.Fatalf("dashboard request count = %#v", response.Totals.TurnCount)
	}
	if response.Totals.TotalTokens.Value == nil || *response.Totals.TotalTokens.Value != 200 {
		t.Fatalf("dashboard total tokens = %#v", response.Totals.TotalTokens)
	}
	if response.Totals.InputTokens.Value == nil || *response.Totals.InputTokens.Value != 80 {
		t.Fatalf("dashboard input tokens = %#v, want input plus cache-write tokens", response.Totals.InputTokens)
	}
	if response.Totals.CachedInputTokens.Value == nil || *response.Totals.CachedInputTokens.Value != 80 {
		t.Fatalf("dashboard cache-read tokens = %#v", response.Totals.CachedInputTokens)
	}
	if response.ReportedUSDMicros == nil || response.ReportedUSDMicros.Value == nil ||
		*response.ReportedUSDMicros.Value != 30_000 {
		t.Fatalf("dashboard reported charge = %#v", response.ReportedUSDMicros)
	}
	if response.DataAsOfMS == nil || response.DataAsOfMS.Value == nil || *response.DataAsOfMS.Value != 5_000 {
		t.Fatalf("dashboard data as of = %#v", response.DataAsOfMS)
	}
	if response.CursorTokenFeeUSDMicros == nil || response.CursorTokenFeeUSDMicros.Value == nil ||
		*response.CursorTokenFeeUSDMicros.Value != 5_000 {
		t.Fatalf("dashboard Cursor token fee = %#v", response.CursorTokenFeeUSDMicros)
	}
	if response.CursorBilling == nil || response.CursorBilling.TotalSpendUSDMicros.Value == nil ||
		*response.CursorBilling.TotalSpendUSDMicros.Value != 1_250_000 ||
		response.CursorBilling.BillingCycleEndAtMS.Value == nil || *response.CursorBilling.BillingCycleEndAtMS.Value != 9_000 {
		t.Fatalf("dashboard billing summary = %#v", response.CursorBilling)
	}
	if !testContainsString(response.ProviderContext.Capabilities, "reported_cost") {
		t.Fatalf("dashboard capabilities = %v", response.ProviderContext.Capabilities)
	}
}

func TestCursorQuotaUsesOfficialModelPercentagesAndMonthlyCycle(t *testing.T) {
	const month = int64(30 * 24 * time.Hour / time.Millisecond)
	cycleStart := int64(1_780_000_000_000)
	cycleEnd := cycleStart + month
	evaluatedAt := cycleStart + month/2
	snapshot := cursorQuerySnapshot("partial")
	snapshot.CollectedAtMS = evaluatedAt
	snapshot.DashboardCollectedAtMS = evaluatedAt
	snapshot.DashboardWindowStartMS = cycleStart
	snapshot.DashboardWindowEndMS = evaluatedAt
	snapshot.DashboardBillingCycleEndMS = cycleEnd
	snapshot.DashboardPlanUsage = &store.CursorDashboardPlanUsage{
		TotalSpendMicros: 99_000_000, IncludedSpendMicros: 70_000_000,
		RemainingMicros: 1_000_000, LimitMicros: 100_000_000,
	}
	snapshot.DashboardQuotaObservations = []store.CursorDashboardQuotaObservation{
		{
			Generation: 1, LimitID: "cursor.models", UsedPercent: 7,
			CycleStartAtMS: cycleStart, CycleEndAtMS: cycleEnd, ObservedAtMS: evaluatedAt,
		},
		{
			Generation: 1, LimitID: "cursor.other_models", UsedPercent: 0,
			CycleStartAtMS: cycleStart, CycleEndAtMS: cycleEnd, ObservedAtMS: evaluatedAt,
		},
	}
	snapshot.Sources = append(snapshot.Sources, store.CursorSourceStatus{
		Provider: "cursor", SourceKey: SourceDashboard, SourceType: "desktop_authenticated_rpc",
		State: "available", CoverageState: "exact", LastSuccessAtMS: &evaluatedAt,
	})
	service := cursorQueryFixture(t, snapshot)

	current, err := service.QuotaCurrent(context.Background(), evaluatedAt)
	if err != nil {
		t.Fatalf("QuotaCurrent() error = %v", err)
	}
	if len(current.Current.Windows) != 2 {
		t.Fatalf("current windows = %#v", current.Current.Windows)
	}
	if current.ProviderContext.EffectiveProvider != "cursor" ||
		!testContainsString(current.ProviderContext.Sources, SourceDashboard) ||
		!testContainsString(current.ProviderContext.Capabilities, "quota") {
		t.Fatalf("quota provider context = %#v", current.ProviderContext)
	}
	models, other := current.Current.Windows[0], current.Current.Windows[1]
	if models.LimitName == nil || *models.LimitName != "Cursor Models" ||
		models.UsedPercent == nil || *models.UsedPercent != 7 ||
		models.RemainingPercent == nil || *models.RemainingPercent != 93 {
		t.Fatalf("Cursor Models quota = %#v", models)
	}
	if other.LimitName == nil || *other.LimitName != "Other Models" ||
		other.UsedPercent == nil || *other.UsedPercent != 0 ||
		other.ResetsAtMS == nil || *other.ResetsAtMS != cycleEnd {
		t.Fatalf("Other Models quota = %#v", other)
	}

	pace, err := service.QuotaPace(context.Background(), evaluatedAt)
	if err != nil {
		t.Fatalf("QuotaPace() error = %v", err)
	}
	if len(pace.Pace.Windows) != 2 || pace.Pace.Windows[0].UsedPercent == nil ||
		*pace.Pace.Windows[0].UsedPercent != 7 || pace.Pace.Windows[0].ElapsedPercent == nil ||
		*pace.Pace.Windows[0].ElapsedPercent != 50 || pace.Pace.Windows[1].UsedPercent == nil ||
		*pace.Pace.Windows[1].UsedPercent != 0 {
		t.Fatalf("monthly quota pace = %#v", pace.Pace.Windows)
	}
}

func TestCursorQuotaKeepsLastKnownPercentagesWhenDashboardRefreshFails(t *testing.T) {
	cycleStart, evaluatedAt, cycleEnd := int64(1_000), int64(2_000), int64(3_000)
	snapshot := cursorQuerySnapshot("partial")
	snapshot.DashboardQuotaObservations = []store.CursorDashboardQuotaObservation{{
		Generation: 1, LimitID: "cursor.models", UsedPercent: 7,
		CycleStartAtMS: cycleStart, CycleEndAtMS: cycleEnd, ObservedAtMS: evaluatedAt,
	}}
	root := t.TempDir()
	collector, err := NewCollector(discardSnapshotWriter{}, Config{
		ProjectsRoot: filepath.Join(root, "projects"), StateDatabase: filepath.Join(root, "state.vscdb"),
		ConversationDatabase: filepath.Join(root, "conversation.db"), AITrackingDatabase: filepath.Join(root, "tracking.db"),
		MinimumRefresh: time.Hour, Now: func() time.Time { return time.UnixMilli(evaluatedAt) },
	})
	if err != nil {
		t.Fatalf("NewCollector() error = %v", err)
	}
	service, err := NewQueryService(collector, fixedSnapshotReader{snapshot: snapshot}, failingDashboardRefresher{})
	if err != nil {
		t.Fatalf("NewQueryService() error = %v", err)
	}
	response, err := service.QuotaCurrent(context.Background(), evaluatedAt)
	if err != nil {
		t.Fatalf("QuotaCurrent() error = %v", err)
	}
	if response.Meta.Status != basequery.ResponsePartial || len(response.Current.Windows) != 1 ||
		response.Current.Windows[0].UsedPercent == nil || *response.Current.Windows[0].UsedPercent != 7 ||
		response.Current.Windows[0].Freshness != store.QuotaCurrentStale {
		t.Fatalf("last-known quota = %#v", response)
	}
}

func TestCursorQuotaPaceKeepsCalendarMonthsWithDifferentDurations(t *testing.T) {
	const day = int64(24 * time.Hour / time.Millisecond)
	currentStart := int64(1_780_000_000_000)
	currentEnd := currentStart + 31*day
	previousStart := currentStart - 30*day
	evaluatedAt := currentStart + 10*day
	snapshot := cursorQuerySnapshot("partial")
	snapshot.DashboardQuotaObservations = []store.CursorDashboardQuotaObservation{
		{
			Generation: 1, LimitID: "cursor.models", UsedPercent: 50,
			CycleStartAtMS: previousStart, CycleEndAtMS: currentStart,
			ObservedAtMS: previousStart + 15*day,
		},
		{
			Generation: 2, LimitID: "cursor.models", UsedPercent: 9,
			CycleStartAtMS: currentStart, CycleEndAtMS: currentEnd, ObservedAtMS: evaluatedAt,
		},
	}
	service := cursorQueryFixture(t, snapshot)
	response, err := service.QuotaPace(context.Background(), evaluatedAt)
	if err != nil {
		t.Fatalf("QuotaPace() error = %v", err)
	}
	if len(response.Pace.Windows) != 1 || response.Pace.Windows[0].PreviousCycle == nil ||
		response.Pace.Windows[0].PreviousCycle.WindowStartAtMS != previousStart {
		t.Fatalf("calendar-month history = %#v", response.Pace.Windows)
	}
}

func TestCursorSessionsUseDashboardModelEvidenceAndExposeDetailBreakdown(t *testing.T) {
	t.Parallel()

	snapshot := cursorQuerySnapshot("partial")
	snapshot.Sessions[1].ModelKey = nil
	snapshot.DashboardGeneration = 9
	snapshot.DashboardCollectedAtMS = 5_000
	snapshot.DashboardWindowStartMS = 1_000
	snapshot.DashboardWindowEndMS = 5_000
	snapshot.Sources = append(snapshot.Sources, store.CursorSourceStatus{
		Provider: "cursor", SourceKey: SourceDashboard, SourceType: "desktop_authenticated_rpc",
		State: "available", CoverageState: "exact", LastSuccessAtMS: pointerInt64ForQueryTest(5_000),
	})
	sessionID := "session-b"
	model := "cursor-dashboard-model"
	snapshot.DashboardUsageEvents = []store.CursorDashboardUsageEvent{
		{
			EventFingerprint: "dashboard-session-b", OccurrenceCount: 2, ExternalSessionID: &sessionID,
			OccurredAtMS: 4_500, ModelKey: &model, InputTokens: 10, OutputTokens: 5,
		},
		{
			EventFingerprint: "dashboard-unattributed", OccurrenceCount: 1,
			OccurredAtMS: 3_500, ModelKey: &model, InputTokens: 1, OutputTokens: 1,
		},
	}
	service := cursorQueryFixture(t, snapshot)
	rangeValue := basequery.UTCTimeRange{StartAtMS: 1_000, EndAtMS: 5_000, TimeZone: "UTC"}
	list, err := service.ListSessions(context.Background(), basequery.Request{
		Page: basequery.PageRequest{Limit: 10}, ExactTimeRange: &rangeValue,
		Sort: []basequery.SortTerm{{Field: "lastActivityAt", Direction: basequery.SortDescending}},
	})
	if err != nil {
		t.Fatalf("ListSessions() error = %v", err)
	}
	var item *usagecost.SessionItem
	for index := range list.Items {
		if list.Items[index].SessionID == sessionID {
			item = &list.Items[index]
		}
	}
	if item == nil || item.Model.DisplayName == nil || *item.Model.DisplayName != model ||
		item.Model.Source != SourceDashboard || item.Totals.TotalTokens.Value == nil ||
		*item.Totals.TotalTokens.Value != 30 {
		t.Fatalf("dashboard-backed session item = %#v", item)
	}

	detail, err := service.SessionDetail(context.Background(), usagecost.SessionDetailRequest{
		SessionID: sessionID, ReportingTimezone: pointer("UTC"), TurnPage: basequery.PageRequest{Limit: 10},
	})
	if err != nil {
		t.Fatalf("SessionDetail() error = %v", err)
	}
	if len(detail.Models) != 1 || detail.Models[0].Model.DisplayName == nil ||
		*detail.Models[0].Model.DisplayName != model || detail.Item.Model.Source != SourceDashboard ||
		detail.Item.Totals.TotalTokens.Value == nil || *detail.Item.Totals.TotalTokens.Value != 30 {
		t.Fatalf("dashboard-backed session detail = %#v", detail)
	}
}

func TestCursorUsageBuildsDashboardActivityDistributionInReportingTimezone(t *testing.T) {
	location, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Fatalf("LoadLocation() error = %v", err)
	}
	start := time.Date(2026, 8, 10, 0, 0, 0, 0, location)
	end := start.AddDate(0, 0, 7)
	sessionA, sessionB, sessionC := "session-a", "session-b", "session-c"
	snapshot := cursorQuerySnapshot("partial")
	snapshot.Sources = append(snapshot.Sources, store.CursorSourceStatus{
		Provider: "cursor", SourceKey: SourceDashboard, SourceType: "desktop_authenticated_rpc",
		State: "available", CoverageState: "exact", LastSuccessAtMS: pointerInt64ForQueryTest(end.UnixMilli()),
	})
	snapshot.DashboardGeneration = 9
	snapshot.DashboardCollectedAtMS = end.UnixMilli()
	snapshot.DashboardWindowStartMS = start.UnixMilli()
	snapshot.DashboardWindowEndMS = end.UnixMilli()
	snapshot.DashboardUsageEvents = []store.CursorDashboardUsageEvent{
		{
			EventFingerprint: "monday-early", OccurrenceCount: 2, ExternalSessionID: &sessionA,
			OccurredAtMS: time.Date(2026, 8, 10, 1, 30, 0, 0, location).UnixMilli(),
			InputTokens:  10, OutputTokens: 20, CacheWriteTokens: 30, CacheReadTokens: 40,
		},
		{
			EventFingerprint: "monday-late", OccurrenceCount: 1, ExternalSessionID: &sessionA,
			OccurredAtMS: time.Date(2026, 8, 10, 5, 45, 0, 0, location).UnixMilli(),
			InputTokens:  2, OutputTokens: 3,
		},
		{
			EventFingerprint: "tuesday", OccurrenceCount: 3, ExternalSessionID: &sessionB,
			OccurredAtMS: time.Date(2026, 8, 11, 8, 15, 0, 0, location).UnixMilli(),
			InputTokens:  12, OutputTokens: 8,
		},
		{
			EventFingerprint: "sunday", OccurrenceCount: 1, ExternalSessionID: &sessionC,
			OccurredAtMS: time.Date(2026, 8, 16, 23, 30, 0, 0, location).UnixMilli(),
			InputTokens:  4, OutputTokens: 5,
		},
	}

	service := cursorQueryFixture(t, snapshot)
	rangeValue := basequery.UTCTimeRange{
		StartAtMS: start.UnixMilli(), EndAtMS: end.UnixMilli(), TimeZone: "Asia/Shanghai",
	}
	response, err := service.UsageCost(context.Background(), usagecost.UsageCostRequest{
		ExactRange: &rangeValue, Granularity: usagecost.TrendDay, IncludeActivityDistribution: true,
	})
	if err != nil {
		t.Fatalf("UsageCost() error = %v", err)
	}
	activity := response.ActivityDistribution
	if activity == nil {
		t.Fatal("activity distribution is nil")
	}
	trendKeys := make([]string, 0, len(response.Trend))
	for _, point := range response.Trend {
		trendKeys = append(trendKeys, point.Key)
	}
	if !reflect.DeepEqual(trendKeys, []string{"2026-08-10", "2026-08-11", "2026-08-16"}) {
		t.Fatalf("daily trend keys = %v, want local calendar dates for annual activity", trendKeys)
	}
	if activity.TimelineGranularity != usagecost.TrendHour || activity.TimelineBucketMinutes != 360 {
		t.Fatalf("activity timeline = (%q, %d minutes), want (hour, 360 minutes)",
			activity.TimelineGranularity, activity.TimelineBucketMinutes)
	}
	if len(activity.Timeline) != 3 {
		t.Fatalf("timeline = %#v, want 3 populated buckets", activity.Timeline)
	}
	assertActivityPoint := func(index int, wantStart time.Time, wantTokens, wantSessions int64) {
		t.Helper()
		point := activity.Timeline[index]
		if point.StartAtMS.Value == nil || *point.StartAtMS.Value != wantStart.UnixMilli() ||
			point.EndAtMS.Value == nil || *point.EndAtMS.Value != wantStart.Add(6*time.Hour).UnixMilli() ||
			point.Metrics.TotalTokens.Value == nil || *point.Metrics.TotalTokens.Value != wantTokens ||
			point.Metrics.SessionCount.Value == nil || *point.Metrics.SessionCount.Value != wantSessions {
			t.Fatalf("timeline[%d] = %#v", index, point)
		}
	}
	assertActivityPoint(0, time.Date(2026, 8, 10, 0, 0, 0, 0, location), 205, 1)
	assertActivityPoint(1, time.Date(2026, 8, 11, 6, 0, 0, 0, location), 60, 1)
	assertActivityPoint(2, time.Date(2026, 8, 16, 18, 0, 0, 0, location), 9, 1)

	wantWeekdayHours := []struct {
		weekday int
		hour    int
		tokens  int64
	}{{1, 1, 200}, {1, 5, 5}, {2, 8, 60}, {7, 23, 9}}
	if len(activity.WeekdayHours) != len(wantWeekdayHours) {
		t.Fatalf("weekday hours = %#v", activity.WeekdayHours)
	}
	for index, want := range wantWeekdayHours {
		point := activity.WeekdayHours[index]
		if point.Weekday != want.weekday || point.Hour != want.hour ||
			point.Metrics.TotalTokens.Value == nil || *point.Metrics.TotalTokens.Value != want.tokens ||
			point.Metrics.SessionCount.Value == nil || *point.Metrics.SessionCount.Value != 1 {
			t.Fatalf("weekdayHours[%d] = %#v, want weekday=%d hour=%d tokens=%d sessions=1",
				index, point, want.weekday, want.hour, want.tokens)
		}
	}
}

func TestCursorUsageDoesNotTreatDashboardRequestsAsActivitySessions(t *testing.T) {
	location := time.FixedZone("UTC", 0)
	start := time.Date(2026, 8, 10, 0, 0, 0, 0, location)
	end := start.AddDate(0, 0, 1)
	snapshot := cursorQuerySnapshot("partial")
	snapshot.Sources = append(snapshot.Sources, store.CursorSourceStatus{
		Provider: "cursor", SourceKey: SourceDashboard, SourceType: "desktop_authenticated_rpc",
		State: "available", CoverageState: "exact", LastSuccessAtMS: pointerInt64ForQueryTest(end.UnixMilli()),
	})
	snapshot.DashboardGeneration = 9
	snapshot.DashboardCollectedAtMS = end.UnixMilli()
	snapshot.DashboardWindowStartMS = start.UnixMilli()
	snapshot.DashboardWindowEndMS = end.UnixMilli()
	snapshot.DashboardUsageEvents = []store.CursorDashboardUsageEvent{{
		EventFingerprint: "request-without-session", OccurrenceCount: 7,
		OccurredAtMS: start.Add(2 * time.Hour).UnixMilli(), InputTokens: 10, OutputTokens: 5,
	}}

	service := cursorQueryFixture(t, snapshot)
	rangeValue := basequery.UTCTimeRange{
		StartAtMS: start.UnixMilli(), EndAtMS: end.UnixMilli(), TimeZone: "UTC",
	}
	response, err := service.UsageCost(context.Background(), usagecost.UsageCostRequest{
		ExactRange: &rangeValue, Granularity: usagecost.TrendHour, IncludeActivityDistribution: true,
	})
	if err != nil {
		t.Fatalf("UsageCost() error = %v", err)
	}
	if response.ActivityDistribution == nil || len(response.ActivityDistribution.Timeline) != 1 {
		t.Fatalf("activity distribution = %#v", response.ActivityDistribution)
	}
	metrics := response.ActivityDistribution.Timeline[0].Metrics
	if metrics.TotalTokens.Value == nil || *metrics.TotalTokens.Value != 105 {
		t.Fatalf("activity tokens = %#v, want exact dashboard total", metrics.TotalTokens)
	}
	if metrics.SessionCount.Value != nil || metrics.SessionCount.UnknownReason == nil ||
		*metrics.SessionCount.UnknownReason != basequery.UnknownUnavailable {
		t.Fatalf("activity sessions = %#v, want unavailable without session identity", metrics.SessionCount)
	}
}

func TestCursorUsageKeepsEmptyDashboardActivityTimeUnknown(t *testing.T) {
	snapshot := cursorQuerySnapshot("partial")
	snapshot.Sources = append(snapshot.Sources, store.CursorSourceStatus{
		Provider: "cursor", SourceKey: SourceDashboard, SourceType: "desktop_authenticated_rpc",
		State: "available", CoverageState: "exact", LastSuccessAtMS: pointerInt64ForQueryTest(20_000),
	})
	snapshot.DashboardGeneration = 9
	snapshot.DashboardCollectedAtMS = 20_000
	snapshot.DashboardWindowStartMS = 10_000
	snapshot.DashboardWindowEndMS = 20_000
	service := cursorQueryFixture(t, snapshot)
	rangeValue := basequery.UTCTimeRange{StartAtMS: 10_000, EndAtMS: 20_000, TimeZone: "UTC"}
	response, err := service.UsageCost(context.Background(), usagecost.UsageCostRequest{
		ExactRange: &rangeValue, Granularity: usagecost.TrendDay,
	})
	if err != nil {
		t.Fatalf("UsageCost() error = %v", err)
	}
	if response.Totals.TurnCount.Value == nil || *response.Totals.TurnCount.Value != 0 ||
		response.Totals.TotalTokens.Value == nil || *response.Totals.TotalTokens.Value != 0 {
		t.Fatalf("empty dashboard totals = %#v, want exact zero counts", response.Totals)
	}
	if response.Totals.LastActivityAtMS.Value != nil ||
		response.Totals.LastActivityAtMS.UnknownReason == nil ||
		*response.Totals.LastActivityAtMS.UnknownReason != basequery.UnknownNotApplicable {
		t.Fatalf("empty dashboard last activity = %#v, want not_applicable instead of Unix epoch", response.Totals.LastActivityAtMS)
	}
}

func TestCursorListsUseFullyAttributableDashboardTokens(t *testing.T) {
	snapshot := cursorQuerySnapshot("partial")
	snapshot.Sources = append(snapshot.Sources, store.CursorSourceStatus{
		Provider: "cursor", SourceKey: SourceDashboard, SourceType: "desktop_authenticated_rpc",
		State: "available", CoverageState: "exact", LastSuccessAtMS: pointerInt64ForQueryTest(5_000),
	})
	snapshot.DashboardGeneration = 9
	snapshot.DashboardCollectedAtMS = 5_000
	snapshot.DashboardWindowStartMS = 1_000
	snapshot.DashboardWindowEndMS = 5_000
	sessionA, sessionB := "session-a", "session-b"
	snapshot.DashboardUsageEvents = []store.CursorDashboardUsageEvent{
		{EventFingerprint: "dashboard-a", OccurrenceCount: 1, ExternalSessionID: &sessionA,
			OccurredAtMS: 2_000, InputTokens: 5, OutputTokens: 5},
		{EventFingerprint: "dashboard-b", OccurrenceCount: 2, ExternalSessionID: &sessionB,
			OccurredAtMS: 3_000, InputTokens: 10, OutputTokens: 20,
			CacheWriteTokens: 30, CacheReadTokens: 40},
	}
	service := cursorQueryFixture(t, snapshot)
	rangeValue := basequery.UTCTimeRange{StartAtMS: 1_000, EndAtMS: 5_000, TimeZone: "UTC"}
	request := basequery.Request{
		ExactTimeRange: &rangeValue,
		Page:           basequery.PageRequest{Limit: 10},
		Sort:           []basequery.SortTerm{{Field: "totalTokens", Direction: basequery.SortDescending}},
	}
	sessions, err := service.ListSessions(context.Background(), request)
	if err != nil {
		t.Fatalf("ListSessions() error = %v", err)
	}
	if len(sessions.Items) != 2 || sessions.Items[0].SessionID != sessionB ||
		sessions.Items[0].Totals.TotalTokens.Value == nil || *sessions.Items[0].Totals.TotalTokens.Value != 200 {
		t.Fatalf("dashboard-attributed sessions = %#v, want session-b with 200 tokens first", sessions.Items)
	}
	projects, err := service.ListProjects(context.Background(), request)
	if err != nil {
		t.Fatalf("ListProjects() error = %v", err)
	}
	if len(projects.Items) != 2 || projects.Items[0].DimensionKey != "project-b" ||
		projects.Items[0].Totals.TotalTokens.Value == nil || *projects.Items[0].Totals.TotalTokens.Value != 200 ||
		projects.Items[0].Totals.TurnCount.Value == nil || *projects.Items[0].Totals.TurnCount.Value != 2 {
		t.Fatalf("dashboard-attributed projects = %#v, want project-b with exact official totals first", projects.Items)
	}
	detail, err := service.ProjectDetail(context.Background(), usagecost.ProjectDetailRequest{
		DimensionKey: "project-b", ExactRange: &rangeValue,
		SessionPage: basequery.PageRequest{Limit: 10}, ModelPage: basequery.PageRequest{Limit: 10},
	})
	if err != nil {
		t.Fatalf("ProjectDetail() error = %v", err)
	}
	if detail.Item.Totals.TotalTokens.Value == nil || *detail.Item.Totals.TotalTokens.Value != 200 ||
		len(detail.Sessions) != 1 || detail.Sessions[0].Totals.TotalTokens.Value == nil ||
		*detail.Sessions[0].Totals.TotalTokens.Value != 200 {
		t.Fatalf("dashboard-attributed project detail = %#v, want exact item and session totals", detail)
	}
}

func TestCursorListsDoNotPresentPartialDashboardAttributionAsExact(t *testing.T) {
	snapshot := cursorQuerySnapshot("partial")
	snapshot.Sources = append(snapshot.Sources, store.CursorSourceStatus{
		Provider: "cursor", SourceKey: SourceDashboard, SourceType: "desktop_authenticated_rpc",
		State: "available", CoverageState: "exact", LastSuccessAtMS: pointerInt64ForQueryTest(5_000),
	})
	snapshot.DashboardGeneration = 9
	snapshot.DashboardCollectedAtMS = 5_000
	snapshot.DashboardWindowStartMS = 1_000
	snapshot.DashboardWindowEndMS = 5_000
	unmatched := "session-not-collected-locally"
	snapshot.DashboardUsageEvents = []store.CursorDashboardUsageEvent{{
		EventFingerprint: "unmatched", OccurrenceCount: 1, ExternalSessionID: &unmatched,
		OccurredAtMS: 2_000, InputTokens: 10, OutputTokens: 20,
	}}
	service := cursorQueryFixture(t, snapshot)
	rangeValue := basequery.UTCTimeRange{StartAtMS: 1_000, EndAtMS: 5_000, TimeZone: "UTC"}
	projects, err := service.ListProjects(context.Background(), basequery.Request{
		ExactTimeRange: &rangeValue, Page: basequery.PageRequest{Limit: 10},
		Sort: []basequery.SortTerm{{Field: "totalTokens", Direction: basequery.SortDescending}},
	})
	if err != nil {
		t.Fatalf("ListProjects() error = %v", err)
	}
	for _, project := range projects.Items {
		if project.Totals.TotalTokens.Value != nil {
			t.Fatalf("partially attributable dashboard tokens leaked into project %q: %#v", project.DimensionKey, project.Totals.TotalTokens)
		}
	}
}

func TestCursorUsageReturnsLastKnownDashboardSnapshotAsPartial(t *testing.T) {
	snapshot := cursorQuerySnapshot("partial")
	lastSuccess := int64(4_000)
	snapshot.Sources = append(snapshot.Sources, store.CursorSourceStatus{
		Provider: "cursor", SourceKey: SourceDashboard, SourceType: "desktop_authenticated_rpc",
		State: "unavailable", CoverageState: "read_failed", LastSuccessAtMS: &lastSuccess,
	})
	snapshot.DashboardGeneration = 9
	snapshot.DashboardCollectedAtMS = 4_000
	snapshot.DashboardWindowStartMS = 1_000
	snapshot.DashboardWindowEndMS = 4_000
	snapshot.DashboardUsageEvents = []store.CursorDashboardUsageEvent{{
		EventFingerprint: "last-known", OccurrenceCount: 3, OccurredAtMS: 2_000,
		InputTokens: 10, OutputTokens: 20, ReportedChargeMicros: 2_000,
	}}
	service := cursorQueryFixture(t, snapshot)
	rangeValue := basequery.UTCTimeRange{StartAtMS: 1_000, EndAtMS: 5_000, TimeZone: "UTC"}
	response, err := service.UsageCost(context.Background(), usagecost.UsageCostRequest{
		ExactRange: &rangeValue, Granularity: usagecost.TrendDay,
	})
	if err != nil {
		t.Fatalf("UsageCost() error = %v", err)
	}
	if response.Meta.Status != basequery.ResponsePartial {
		t.Fatalf("response status = %q, want partial", response.Meta.Status)
	}
	if response.Totals.TurnCount.Value == nil || *response.Totals.TurnCount.Value != 3 ||
		response.Totals.TotalTokens.Value == nil || *response.Totals.TotalTokens.Value != 90 {
		t.Fatalf("last-known dashboard totals = %#v", response.Totals)
	}
	if response.ReportedUSDMicros == nil || response.ReportedUSDMicros.Value == nil ||
		*response.ReportedUSDMicros.Value != 6_000 {
		t.Fatalf("last-known reported charge = %#v", response.ReportedUSDMicros)
	}
	if response.DataAsOfMS == nil || response.DataAsOfMS.Value == nil || *response.DataAsOfMS.Value != 4_000 {
		t.Fatalf("last-known data as of = %#v", response.DataAsOfMS)
	}
}

func TestCursorUsageUsesOverlappingDashboardWindowAsPartial(t *testing.T) {
	snapshot := cursorQuerySnapshot("partial")
	snapshot.DashboardGeneration = 9
	snapshot.DashboardCollectedAtMS = 5_000
	snapshot.DashboardWindowStartMS = 2_000
	snapshot.DashboardWindowEndMS = 5_000
	snapshot.DashboardUsageEvents = []store.CursorDashboardUsageEvent{{
		EventFingerprint: "overlapping-window", OccurrenceCount: 2, OccurredAtMS: 3_000,
		InputTokens: 10, OutputTokens: 20, ReportedChargeMicros: 2_000,
	}}
	service := cursorQueryFixture(t, snapshot)
	rangeValue := basequery.UTCTimeRange{StartAtMS: 1_000, EndAtMS: 6_000, TimeZone: "UTC"}
	response, err := service.UsageCost(context.Background(), usagecost.UsageCostRequest{
		ExactRange: &rangeValue, Granularity: usagecost.TrendDay,
	})
	if err != nil {
		t.Fatalf("UsageCost() error = %v", err)
	}
	if response.Meta.Status != basequery.ResponsePartial {
		t.Fatalf("response status = %q, want partial for a shorter overlapping dashboard window", response.Meta.Status)
	}
	if response.Totals.TurnCount.Value == nil || *response.Totals.TurnCount.Value != 2 ||
		response.Totals.TotalTokens.Value == nil || *response.Totals.TotalTokens.Value != 60 {
		t.Fatalf("overlapping dashboard totals = %#v", response.Totals)
	}
}

func TestCursorInvocationUsageCountsAIEditsInsideRequestedRange(t *testing.T) {
	snapshot := cursorQuerySnapshot("partial")
	snapshot.Sources = append(snapshot.Sources, store.CursorSourceStatus{
		Provider: "cursor", SourceKey: SourceAITracking, SourceType: "sqlite_snapshot",
		State: "available", CoverageState: "exact", RowCount: 3,
	})
	snapshot.AIEditEvents = []store.CursorAIEditEvent{
		{EventID: "before", OccurredAtMS: 500, EditCount: 11},
		{EventID: "inside-a", OccurredAtMS: 2_000, EditCount: 2},
		{EventID: "inside-b", OccurredAtMS: 4_000, EditCount: 3},
		{EventID: "after", OccurredAtMS: 5_000, EditCount: 13},
	}
	service := cursorQueryFixture(t, snapshot)
	response, err := service.InvocationUsage(context.Background(), invocationusage.InvocationUsageRequest{
		Range:       basequery.UTCTimeRange{StartAtMS: 1_000, EndAtMS: 5_000, TimeZone: "UTC"},
		Granularity: invocationusage.GranularityDay, SourceClass: invocationusage.SourceClassAll,
	})
	if err != nil {
		t.Fatalf("InvocationUsage() error = %v", err)
	}
	encoded, err := json.Marshal(response)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	var wire struct {
		Totals struct {
			AIEditCount basequery.NumericValue `json:"aiEditCount"`
		} `json:"totals"`
	}
	if err := json.Unmarshal(encoded, &wire); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if wire.Totals.AIEditCount.Value == nil || *wire.Totals.AIEditCount.Value != 5 {
		t.Fatalf("AI edit count = %#v, want 5 edits inside the requested range", wire.Totals.AIEditCount)
	}
}

func TestCursorInvocationUsageMarksAIEditsUnavailableWithoutHealthySource(t *testing.T) {
	service := cursorQueryFixture(t, cursorQuerySnapshot("partial"))
	response, err := service.InvocationUsage(context.Background(), invocationusage.InvocationUsageRequest{
		Range:       basequery.UTCTimeRange{StartAtMS: 1_000, EndAtMS: 5_000, TimeZone: "UTC"},
		Granularity: invocationusage.GranularityDay, SourceClass: invocationusage.SourceClassAll,
	})
	if err != nil {
		t.Fatalf("InvocationUsage() error = %v", err)
	}
	encoded, err := json.Marshal(response)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	var wire struct {
		Totals struct {
			AIEditCount basequery.NumericValue `json:"aiEditCount"`
		} `json:"totals"`
	}
	if err := json.Unmarshal(encoded, &wire); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if wire.Totals.AIEditCount.Value != nil || wire.Totals.AIEditCount.UnknownReason == nil ||
		*wire.Totals.AIEditCount.UnknownReason != basequery.UnknownUnavailable {
		t.Fatalf("AI edit count = %#v, want unavailable without a healthy source", wire.Totals.AIEditCount)
	}
}

func pointerInt64ForQueryTest(value int64) *int64 { return &value }

func testContainsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func TestCursorSessionPaginationBindsCursorToFilterAndIncludesUnknownTokenTurns(t *testing.T) {
	service := cursorQueryFixture(t, cursorQuerySnapshot("partial"))
	rangeValue := basequery.UTCTimeRange{StartAtMS: 1_000, EndAtMS: 5_000, TimeZone: "UTC"}
	request := basequery.Request{
		Page:           basequery.PageRequest{Limit: 1},
		Sort:           []basequery.SortTerm{{Field: "lastActivityAt", Direction: basequery.SortDescending}},
		ExactTimeRange: &rangeValue,
	}
	first, err := service.ListSessions(context.Background(), request)
	if err != nil {
		t.Fatalf("ListSessions(first) error = %v", err)
	}
	if len(first.Items) != 1 || first.Meta.Page == nil || first.Meta.Page.NextCursor == nil {
		t.Fatalf("first page = %#v", first)
	}
	replayed := request
	replayed.Page.Cursor = first.Meta.Page.NextCursor
	replayed.Filters = []basequery.FilterTerm{{
		Field: "projectId", Operator: basequery.FilterEqual, Values: []string{"Project A"},
	}}
	if _, err := service.ListSessions(context.Background(), replayed); !errors.Is(err, basequery.ErrValidation) {
		t.Fatalf("cross-filter cursor error = %v, want validation", err)
	}

	detail, err := service.SessionDetail(context.Background(), usagecost.SessionDetailRequest{
		SessionID: "session-a", TurnPage: basequery.PageRequest{Limit: 10},
	})
	if err != nil {
		t.Fatalf("SessionDetail() error = %v", err)
	}
	if len(detail.Turns) != 2 || detail.Item.Totals.TurnCount.Value == nil ||
		*detail.Item.Totals.TurnCount.Value != 2 {
		t.Fatalf("detail = %#v, want both generation IDs", detail)
	}
	if detail.Item.DisplayTitle != "Real Cursor title" || detail.Item.TitleSource != "cursor_composer_header" ||
		detail.Item.TitleConfidence != "high" {
		t.Fatalf("detail title attribution = %#v", detail.Item)
	}
	if detail.Turns[0].Totals.TotalTokens.Value != nil || detail.Turns[1].Totals.TotalTokens.Value != nil {
		t.Fatalf("partial token coverage leaked into turns: %#v", detail.Turns)
	}
}

func cursorQueryFixture(t *testing.T, snapshot store.CursorSnapshot) *QueryService {
	t.Helper()
	root := t.TempDir()
	collector, err := NewCollector(discardSnapshotWriter{}, Config{
		ProjectsRoot:         filepath.Join(root, "projects"),
		StateDatabase:        filepath.Join(root, "state.vscdb"),
		ConversationDatabase: filepath.Join(root, "conversation.db"),
		AITrackingDatabase:   filepath.Join(root, "tracking.db"),
		MinimumRefresh:       time.Hour,
		Now:                  func() time.Time { return time.UnixMilli(snapshot.CollectedAtMS) },
	})
	if err != nil {
		t.Fatalf("NewCollector() error = %v", err)
	}
	service, err := NewQueryService(collector, fixedSnapshotReader{snapshot: snapshot})
	if err != nil {
		t.Fatalf("NewQueryService() error = %v", err)
	}
	return service
}

func TestProjectGroupsUnifiesUnrecognizedProjectsAsOther(t *testing.T) {
	t.Parallel()

	sessions := []store.CursorSession{
		{ExternalSessionID: "unknown-a", ProjectKey: "unknown-project-a", ProjectDisplayName: "未识别项目"},
		{ExternalSessionID: "unknown-b", ProjectKey: "unknown-project-b", ProjectDisplayName: "未识别项目"},
		{ExternalSessionID: "known-other", ProjectKey: "known-other-project", ProjectDisplayName: "其他"},
	}
	events := []store.CursorUsageEvent{
		{EventID: "a", ExternalSessionID: "unknown-a", InputTokens: 10},
		{EventID: "b", ExternalSessionID: "unknown-b", OutputTokens: 20},
	}
	groups := projectGroups(sessions, events, nil, nil)
	if len(groups) != 2 {
		t.Fatalf("projectGroups() count = %d, want 2: %#v", len(groups), groups)
	}

	var unknown, known *cursorProjectGroup
	for index := range groups {
		switch groups[index].key {
		case cursorOtherProjectKey:
			unknown = &groups[index]
		case "known-other-project":
			known = &groups[index]
		}
	}
	if unknown == nil || unknown.name != "其他" || len(unknown.sessions) != 2 || len(unknown.events) != 2 {
		t.Fatalf("unrecognized project group = %#v", unknown)
	}
	if known == nil || known.name != "其他" || len(known.sessions) != 1 {
		t.Fatalf("known project named other = %#v", known)
	}
	item := projectItem(*unknown, true, false)
	if item.Project.Confidence != "low" || item.Project.Source != "cursor_local" ||
		item.Project.Reason != "workspace_unavailable" {
		t.Fatalf("other project attribution = %#v", item.Project)
	}
}

func cursorQuerySnapshot(stateCoverage string) store.CursorSnapshot {
	model := "cursor-model"
	return store.CursorSnapshot{
		Generation: 7, CollectedAtMS: 5_000,
		Sources: []store.CursorSourceStatus{{
			Provider: "cursor", SourceKey: SourceState, SourceType: "sqlite_snapshot",
			State: stateCoverage, CoverageState: stateCoverage, RowCount: 2,
		}},
		Sessions: []store.CursorSession{
			{ExternalSessionID: "session-a", DisplayTitle: "Real Cursor title", TitleSource: "cursor_composer_header", ProjectKey: "project-a", ProjectDisplayName: "Project A", CreatedAtMS: 1_000, LastActivityAtMS: 4_000, ModelKey: &model, RequestCount: 2, CoverageState: "partial"},
			{ExternalSessionID: "session-b", ProjectKey: "project-b", ProjectDisplayName: "Project B", CreatedAtMS: 1_000, LastActivityAtMS: 3_000, RequestCount: 1, CoverageState: "partial"},
		},
		RequestEvents: []store.CursorRequestEvent{
			{EventID: "generation-a", ExternalSessionID: "session-a", OccurredAtMS: 2_000},
			{EventID: "generation-b", ExternalSessionID: "session-a", OccurredAtMS: 3_000},
		},
		UsageEvents: []store.CursorUsageEvent{{
			EventID: "generation-a", ExternalSessionID: "session-a", OccurredAtMS: 2_000,
			ModelKey: &model, InputTokens: 100, OutputTokens: 20,
		}},
	}
}
