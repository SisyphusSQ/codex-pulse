package cursorprovider

import (
	"context"
	"errors"
	"math"
	"sync"
	"testing"
	"time"

	"github.com/SisyphusSQ/codex-pulse/internal/store"
)

type dashboardClientFixture struct {
	current      CurrentPeriodUsage
	pages        []UsageEventsPage
	err          error
	calls        int
	currentCalls int
}

func (fixture *dashboardClientFixture) GetCurrentPeriodUsage(context.Context) (CurrentPeriodUsage, error) {
	fixture.currentCalls++
	return fixture.current, fixture.err
}

func (fixture *dashboardClientFixture) GetFilteredUsageEvents(_ context.Context, _ UsageEventsRequest) (UsageEventsPage, error) {
	if fixture.err != nil {
		return UsageEventsPage{}, fixture.err
	}
	if fixture.calls >= len(fixture.pages) {
		return UsageEventsPage{}, ErrDashboardProtocol
	}
	page := fixture.pages[fixture.calls]
	fixture.calls++
	return page, nil
}

type dashboardSnapshotCapture struct {
	mu       sync.Mutex
	snapshot store.CursorDashboardSnapshot
	commits  int
	failures []string
}

func (capture *dashboardSnapshotCapture) CommitCursorDashboardSnapshot(_ context.Context, snapshot store.CursorDashboardSnapshot) error {
	capture.mu.Lock()
	defer capture.mu.Unlock()
	capture.snapshot = snapshot
	capture.commits++
	return nil
}

func (capture *dashboardSnapshotCapture) RecordCursorDashboardFailure(_ context.Context, _ int64, failureCode string) error {
	capture.mu.Lock()
	defer capture.mu.Unlock()
	capture.failures = append(capture.failures, failureCode)
	return nil
}

func TestDashboardCollectorCommitsCompleteWindowAndPreservesDuplicateMultiplicity(t *testing.T) {
	t.Parallel()

	event := DashboardUsageEvent{
		OccurredAtMS: 2_000, Model: "cursor-model", Kind: 1, TokenBased: true,
		TokenUsage:   &DashboardTokenUsage{InputTokens: 10, OutputTokens: 20, CacheWriteTokens: 30, CacheReadTokens: 40},
		ChargedCents: 1.5, CursorTokenFeeCents: 0.25, ConversationID: "conversation-a",
	}
	client := &dashboardClientFixture{
		current: CurrentPeriodUsage{
			BillingCycleStartMS: 1_000, BillingCycleEndMS: 9_000, Enabled: true,
			PlanUsage: &CurrentPlanUsage{
				TotalSpendCents: 125, IncludedSpendCents: 2_000, BonusSpendCents: 500,
				RemainingCents: 2_375, LimitCents: 2_500,
				CursorModelsUsedPercent: pointerFloat64ForDashboardTest(7),
				OtherModelsUsedPercent:  pointerFloat64ForDashboardTest(0),
			},
		},
		pages: []UsageEventsPage{{TotalCount: 2, Events: []DashboardUsageEvent{event, event}}},
	}
	capture := &dashboardSnapshotCapture{}
	collector, err := NewDashboardCollector(client, capture, DashboardCollectorConfig{
		MinimumRefresh: 0,
		Now:            func() time.Time { return time.UnixMilli(5_000) },
	})
	if err != nil {
		t.Fatalf("NewDashboardCollector() error = %v", err)
	}
	if err := collector.Refresh(context.Background()); err != nil {
		t.Fatalf("Refresh() error = %v", err)
	}
	if capture.commits != 1 || len(capture.failures) != 0 {
		t.Fatalf("writes = commits:%d failures:%v", capture.commits, capture.failures)
	}
	if capture.snapshot.WindowStartMS != 1_000 || capture.snapshot.WindowEndMS != 5_000 || len(capture.snapshot.Events) != 1 {
		t.Fatalf("snapshot = %#v", capture.snapshot)
	}
	if capture.snapshot.BillingCycleEndMS != 9_000 || capture.snapshot.PlanUsage == nil ||
		capture.snapshot.PlanUsage.TotalSpendMicros != 1_250_000 ||
		capture.snapshot.PlanUsage.RemainingMicros != 23_750_000 {
		t.Fatalf("billing summary = %#v", capture.snapshot.PlanUsage)
	}
	if len(capture.snapshot.QuotaWindows) != 2 ||
		capture.snapshot.QuotaWindows[0] != (store.CursorDashboardQuotaWindow{
			LimitID: "cursor.models", UsedPercent: 7, CycleStartAtMS: 1_000, CycleEndAtMS: 9_000,
		}) ||
		capture.snapshot.QuotaWindows[1] != (store.CursorDashboardQuotaWindow{
			LimitID: "cursor.other_models", UsedPercent: 0, CycleStartAtMS: 1_000, CycleEndAtMS: 9_000,
		}) {
		t.Fatalf("quota windows = %#v", capture.snapshot.QuotaWindows)
	}
	stored := capture.snapshot.Events[0]
	if stored.OccurrenceCount != 2 || !stored.TokenBased || stored.InputTokens != 10 || stored.CacheReadTokens != 40 {
		t.Fatalf("stored usage = %#v", stored)
	}
	if stored.ReportedChargeMicros != 15_000 || stored.CursorTokenFeeMicros != 2_500 {
		t.Fatalf("stored charge = %#v", stored)
	}
	if len(stored.EventFingerprint) != 64 {
		t.Fatalf("event fingerprint = %q", stored.EventFingerprint)
	}
}

func TestDashboardCollectorManualRefreshUsesInteractiveInterval(t *testing.T) {
	t.Parallel()
	now := time.UnixMilli(5_000)
	client := &dashboardClientFixture{
		current: CurrentPeriodUsage{
			BillingCycleStartMS: 1_000,
			BillingCycleEndMS:   time.Unix(1_000, 0).UnixMilli(),
		},
		pages: []UsageEventsPage{{TotalCount: 0}, {TotalCount: 0}},
	}
	collector, err := NewDashboardCollector(client, &dashboardSnapshotCapture{}, DashboardCollectorConfig{
		MinimumRefresh: 5 * time.Minute,
		Now:            func() time.Time { return now },
	})
	if err != nil {
		t.Fatalf("NewDashboardCollector() error = %v", err)
	}
	if performed, err := collector.RefreshIfDue(context.Background()); err != nil || !performed {
		t.Fatalf("first RefreshIfDue() = %v, %v", performed, err)
	}

	now = now.Add(61 * time.Second)
	if performed, err := collector.RefreshIfDue(context.Background()); err != nil || performed {
		t.Fatalf("second RefreshIfDue() = %v, %v", performed, err)
	}
	if err := collector.Refresh(context.Background()); err != nil {
		t.Fatalf("Refresh() error = %v", err)
	}
	if client.currentCalls != 2 {
		t.Fatalf("dashboard requests = %d, want 2", client.currentCalls)
	}
}

func pointerFloat64ForDashboardTest(value float64) *float64 { return &value }

func TestDashboardCollectorManualRefreshReportsAuthFailureWithoutReplacingLastSuccess(t *testing.T) {
	t.Parallel()
	client := &dashboardClientFixture{err: ErrDesktopAuthExpired}
	capture := &dashboardSnapshotCapture{}
	collector, err := NewDashboardCollector(client, capture, DashboardCollectorConfig{
		MinimumRefresh: 0,
		Now:            func() time.Time { return time.UnixMilli(5_000) },
	})
	if err != nil {
		t.Fatalf("NewDashboardCollector() error = %v", err)
	}
	if err := collector.Refresh(context.Background()); !errors.Is(err, ErrDesktopAuthExpired) {
		t.Fatalf("Refresh() error = %v, want auth failure", err)
	}
	if capture.commits != 0 || len(capture.failures) != 1 || capture.failures[0] != "auth_expired" {
		t.Fatalf("writes = commits:%d failures:%v", capture.commits, capture.failures)
	}
	if !errors.Is(client.err, ErrDesktopAuthExpired) {
		t.Fatal("fixture must retain auth failure")
	}
}

func TestDashboardCollectorBackgroundRefreshRecordsAuthFailure(t *testing.T) {
	t.Parallel()
	client := &dashboardClientFixture{err: ErrDesktopAuthExpired}
	capture := &dashboardSnapshotCapture{}
	collector, err := NewDashboardCollector(client, capture, DashboardCollectorConfig{
		MinimumRefresh: 0,
		Now:            func() time.Time { return time.UnixMilli(5_000) },
	})
	if err != nil {
		t.Fatalf("NewDashboardCollector() error = %v", err)
	}
	if performed, err := collector.RefreshIfDue(context.Background()); err != nil || !performed {
		t.Fatalf("RefreshIfDue() = %v, %v", performed, err)
	}
	if capture.commits != 0 || len(capture.failures) != 1 || capture.failures[0] != "auth_expired" {
		t.Fatalf("writes = commits:%d failures:%v", capture.commits, capture.failures)
	}
}

func TestDashboardCollectorRejectsPageThatExceedsOfficialTotal(t *testing.T) {
	t.Parallel()
	event := DashboardUsageEvent{OccurredAtMS: 2_000, Kind: 1}
	client := &dashboardClientFixture{
		current: CurrentPeriodUsage{BillingCycleStartMS: 1_000, BillingCycleEndMS: 9_000},
		pages:   []UsageEventsPage{{TotalCount: 1, Events: []DashboardUsageEvent{event, event}}},
	}
	capture := &dashboardSnapshotCapture{}
	collector, err := NewDashboardCollector(client, capture, DashboardCollectorConfig{
		Now: func() time.Time { return time.UnixMilli(5_000) },
	})
	if err != nil {
		t.Fatalf("NewDashboardCollector() error = %v", err)
	}
	if err := collector.Refresh(context.Background()); !errors.Is(err, ErrDashboardProtocol) {
		t.Fatalf("Refresh() error = %v, want protocol failure", err)
	}
	if capture.commits != 0 || len(capture.failures) != 1 || capture.failures[0] != "schema_incompatible" {
		t.Fatalf("writes = commits:%d failures:%v", capture.commits, capture.failures)
	}
}

func TestDashboardCollectorRejectsAggregateThatCannotCrossRPCExactly(t *testing.T) {
	t.Parallel()
	client := &dashboardClientFixture{
		current: CurrentPeriodUsage{BillingCycleStartMS: 1_000, BillingCycleEndMS: 9_000},
		pages: []UsageEventsPage{{TotalCount: 1, Events: []DashboardUsageEvent{{
			OccurredAtMS: 2_000, Kind: 1, TokenBased: true,
			TokenUsage: &DashboardTokenUsage{InputTokens: math.MaxInt64},
		}}}},
	}
	capture := &dashboardSnapshotCapture{}
	collector, err := NewDashboardCollector(client, capture, DashboardCollectorConfig{
		Now: func() time.Time { return time.UnixMilli(5_000) },
	})
	if err != nil {
		t.Fatalf("NewDashboardCollector() error = %v", err)
	}
	if err := collector.Refresh(context.Background()); !errors.Is(err, ErrDashboardProtocol) {
		t.Fatalf("Refresh() error = %v, want protocol failure", err)
	}
	if capture.commits != 0 || len(capture.failures) != 1 || capture.failures[0] != "schema_incompatible" {
		t.Fatalf("writes = commits:%d failures:%v", capture.commits, capture.failures)
	}
}
