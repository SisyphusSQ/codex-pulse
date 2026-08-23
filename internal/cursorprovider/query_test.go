package cursorprovider

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	quotaquery "github.com/SisyphusSQ/codex-pulse/internal/codex/quota"
	basequery "github.com/SisyphusSQ/codex-pulse/internal/query"
	"github.com/SisyphusSQ/codex-pulse/internal/query/invocationusage"
	"github.com/SisyphusSQ/codex-pulse/internal/query/usagecost"
	"github.com/SisyphusSQ/codex-pulse/internal/store"
)

type discardSnapshotWriter struct{}

func (discardSnapshotWriter) ReplaceCursorSnapshot(context.Context, store.CursorSnapshot) error {
	return nil
}

type blockingSnapshotWriter struct {
	started chan struct{}
	release chan struct{}
	once    sync.Once
	commit  func()
}

func (writer *blockingSnapshotWriter) ReplaceCursorSnapshot(ctx context.Context, _ store.CursorSnapshot) error {
	writer.once.Do(func() { close(writer.started) })
	select {
	case <-writer.release:
		if writer.commit != nil {
			writer.commit()
		}
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

type fixedSnapshotReader struct{ snapshot store.CursorSnapshot }

func (reader fixedSnapshotReader) CursorSnapshot(context.Context) (store.CursorSnapshot, error) {
	return reader.snapshot, nil
}

type mutableSnapshotReader struct {
	mu       sync.RWMutex
	snapshot store.CursorSnapshot
}

func (reader *mutableSnapshotReader) CursorSnapshot(context.Context) (store.CursorSnapshot, error) {
	reader.mu.RLock()
	defer reader.mu.RUnlock()
	return reader.snapshot, nil
}

func (reader *mutableSnapshotReader) setGeneration(generation int64) {
	reader.mu.Lock()
	defer reader.mu.Unlock()
	reader.snapshot.Generation = generation
}

func (reader *mutableSnapshotReader) appendSource(source store.CursorSourceStatus) {
	reader.mu.Lock()
	defer reader.mu.Unlock()
	sources := append([]store.CursorSourceStatus(nil), reader.snapshot.Sources...)
	reader.snapshot.Sources = append(sources, source)
}

type countingSnapshotReader struct {
	snapshot store.CursorSnapshot
	reads    atomic.Int64
}

type blockingMutableSnapshotReader struct {
	mu       sync.Mutex
	snapshot store.CursorSnapshot
	reads    int
	started  chan struct{}
	release  chan struct{}
}

func (reader *blockingMutableSnapshotReader) CursorSnapshot(ctx context.Context) (store.CursorSnapshot, error) {
	reader.mu.Lock()
	reader.reads++
	reads := reader.reads
	snapshot := reader.snapshot
	reader.mu.Unlock()
	if reads == 1 {
		close(reader.started)
		select {
		case <-reader.release:
		case <-ctx.Done():
			return store.CursorSnapshot{}, ctx.Err()
		}
	}
	return snapshot, nil
}

func (reader *blockingMutableSnapshotReader) update(snapshot store.CursorSnapshot) {
	reader.mu.Lock()
	reader.snapshot = snapshot
	reader.mu.Unlock()
}

func (reader *blockingMutableSnapshotReader) readCount() int {
	reader.mu.Lock()
	defer reader.mu.Unlock()
	return reader.reads
}

func (reader *countingSnapshotReader) CursorSnapshot(context.Context) (store.CursorSnapshot, error) {
	reader.reads.Add(1)
	return reader.snapshot, nil
}

type snapshotPublishingRefresher struct{ reader *mutableSnapshotReader }

func (refresher snapshotPublishingRefresher) Refresh(context.Context) error {
	refresher.reader.appendSource(store.CursorSourceStatus{
		Provider: "cursor", SourceKey: SourceDashboard, SourceType: "desktop_authenticated_rpc",
		State: "available", CoverageState: "exact", LastSuccessAtMS: pointerInt64ForQueryTest(5_000),
	})
	return nil
}

type failingDashboardRefresher struct{}

func (failingDashboardRefresher) Refresh(context.Context) error { return ErrDesktopAuthExpired }

type blockingDashboardRefresher struct {
	started chan struct{}
	release chan struct{}
}

func (refresher *blockingDashboardRefresher) Refresh(ctx context.Context) error {
	close(refresher.started)
	select {
	case <-refresher.release:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func TestQueryServiceReturnsExistingSnapshotBeforeLocalRefreshCompletes(t *testing.T) {
	root := t.TempDir()
	writer := &blockingSnapshotWriter{started: make(chan struct{}), release: make(chan struct{})}
	t.Cleanup(func() {
		select {
		case <-writer.release:
		default:
			close(writer.release)
		}
	})
	collector, err := NewCollector(writer, Config{
		ProjectsRoot: filepath.Join(root, "projects"), StateDatabase: filepath.Join(root, "state.vscdb"),
		ConversationDatabase: filepath.Join(root, "conversation.db"), AITrackingDatabase: filepath.Join(root, "tracking.db"),
		MinimumRefresh: time.Hour, Now: func() time.Time { return time.UnixMilli(5_000) },
	})
	if err != nil {
		t.Fatalf("NewCollector() error = %v", err)
	}
	service, err := NewQueryService(collector, fixedSnapshotReader{snapshot: cursorQuerySnapshot("partial")})
	if err != nil {
		t.Fatalf("NewQueryService() error = %v", err)
	}

	rangeValue := basequery.UTCTimeRange{StartAtMS: 1_000, EndAtMS: 5_000, TimeZone: "UTC"}
	completed := make(chan error, 1)
	go func() {
		_, queryErr := service.UsageCost(context.Background(), usagecost.UsageCostRequest{
			ExactRange: &rangeValue, Granularity: usagecost.TrendDay,
		})
		completed <- queryErr
	}()

	select {
	case err := <-completed:
		if err != nil {
			t.Fatalf("UsageCost() error = %v", err)
		}
	case <-time.After(250 * time.Millisecond):
		t.Fatal("UsageCost() waited for the local Cursor refresh despite an existing snapshot")
	}
	select {
	case <-writer.started:
	case <-time.After(time.Second):
		t.Fatal("local Cursor refresh did not start in the background")
	}
}

func TestQueryServiceReturnsExistingQuotaBeforeLocalRefreshCompletes(t *testing.T) {
	root := t.TempDir()
	writer := &blockingSnapshotWriter{started: make(chan struct{}), release: make(chan struct{})}
	t.Cleanup(func() {
		select {
		case <-writer.release:
		default:
			close(writer.release)
		}
	})
	collector, err := NewCollector(writer, Config{
		ProjectsRoot: filepath.Join(root, "projects"), StateDatabase: filepath.Join(root, "state.vscdb"),
		ConversationDatabase: filepath.Join(root, "conversation.db"), AITrackingDatabase: filepath.Join(root, "tracking.db"),
		MinimumRefresh: time.Hour, Now: func() time.Time { return time.UnixMilli(5_000) },
	})
	if err != nil {
		t.Fatalf("NewCollector() error = %v", err)
	}
	service, err := NewQueryService(collector, fixedSnapshotReader{snapshot: cursorQuerySnapshot("partial")})
	if err != nil {
		t.Fatalf("NewQueryService() error = %v", err)
	}

	completed := make(chan error, 1)
	go func() {
		_, queryErr := service.QuotaCurrent(context.Background(), 5_000)
		completed <- queryErr
	}()
	select {
	case err := <-completed:
		if err != nil {
			t.Fatalf("QuotaCurrent() error = %v", err)
		}
	case <-time.After(250 * time.Millisecond):
		t.Fatal("QuotaCurrent() waited for the local Cursor refresh despite an existing snapshot")
	}
	select {
	case <-writer.started:
	case <-time.After(time.Second):
		t.Fatal("local Cursor refresh did not start in the background")
	}
}

func TestQueryServiceConcurrentQueriesHydrateSnapshotOnce(t *testing.T) {
	root := t.TempDir()
	collector, err := NewCollector(discardSnapshotWriter{}, Config{
		ProjectsRoot: filepath.Join(root, "projects"), StateDatabase: filepath.Join(root, "state.vscdb"),
		ConversationDatabase: filepath.Join(root, "conversation.db"), AITrackingDatabase: filepath.Join(root, "tracking.db"),
		MinimumRefresh: time.Hour, Now: func() time.Time { return time.UnixMilli(5_000) },
	})
	if err != nil {
		t.Fatalf("NewCollector() error = %v", err)
	}
	if err := collector.Refresh(context.Background()); err != nil {
		t.Fatalf("Refresh(test fixture) error = %v", err)
	}
	reader := &countingSnapshotReader{snapshot: cursorQuerySnapshot("partial")}
	service, err := NewQueryService(collector, reader)
	if err != nil {
		t.Fatalf("NewQueryService() error = %v", err)
	}

	rangeValue := basequery.UTCTimeRange{StartAtMS: 1_000, EndAtMS: 5_000, TimeZone: "UTC"}
	start := make(chan struct{})
	errors := make(chan error, 8)
	for range 8 {
		go func() {
			<-start
			_, queryErr := service.UsageCost(context.Background(), usagecost.UsageCostRequest{
				ExactRange: &rangeValue, Granularity: usagecost.TrendDay,
			})
			errors <- queryErr
		}()
	}
	close(start)
	for range 8 {
		if err := <-errors; err != nil {
			t.Fatalf("UsageCost() error = %v", err)
		}
	}
	if got := reader.reads.Load(); got != 1 {
		t.Fatalf("CursorSnapshot() reads = %d, want 1 shared hydration", got)
	}
}

func TestQueryServiceDoesNotCacheSnapshotLoadedAcrossInvalidation(t *testing.T) {
	root := t.TempDir()
	collector, err := NewCollector(discardSnapshotWriter{}, Config{
		ProjectsRoot: filepath.Join(root, "projects"), StateDatabase: filepath.Join(root, "state.vscdb"),
		ConversationDatabase: filepath.Join(root, "conversation.db"), AITrackingDatabase: filepath.Join(root, "tracking.db"),
		MinimumRefresh: time.Hour, Now: func() time.Time { return time.UnixMilli(5_000) },
	})
	if err != nil {
		t.Fatalf("NewCollector() error = %v", err)
	}
	first := cursorQuerySnapshot("partial")
	first.Generation = 1
	reader := &blockingMutableSnapshotReader{
		snapshot: first,
		started:  make(chan struct{}),
		release:  make(chan struct{}),
	}
	service, err := NewQueryService(collector, reader)
	if err != nil {
		t.Fatalf("NewQueryService() error = %v", err)
	}

	loaded := make(chan error, 1)
	go func() {
		_, loadErr := service.loadSnapshot(context.Background())
		loaded <- loadErr
	}()
	<-reader.started
	service.invalidateSnapshot()
	close(reader.release)
	if err := <-loaded; err != nil {
		t.Fatalf("loadSnapshot() error = %v", err)
	}
	second := first
	second.Generation = 2
	reader.update(second)

	snapshot, err := service.loadSnapshot(context.Background())
	if err != nil {
		t.Fatalf("loadSnapshot(after invalidation) error = %v", err)
	}
	if snapshot.Generation != 2 || reader.readCount() != 2 {
		t.Fatalf(
			"snapshot generation = %d, reads = %d; want generation 2 from a second hydration",
			snapshot.Generation,
			reader.readCount(),
		)
	}
}

func TestQueryServiceExplicitRefreshInvalidatesCachedSnapshot(t *testing.T) {
	root := t.TempDir()
	reader := &mutableSnapshotReader{snapshot: cursorQuerySnapshot("partial")}
	reader.setGeneration(1)
	release := make(chan struct{})
	close(release)
	writer := &blockingSnapshotWriter{
		started: make(chan struct{}),
		release: release,
		commit: func() {
			reader.setGeneration(2)
		},
	}
	collector, err := NewCollector(writer, Config{
		ProjectsRoot: filepath.Join(root, "projects"), StateDatabase: filepath.Join(root, "state.vscdb"),
		ConversationDatabase: filepath.Join(root, "conversation.db"), AITrackingDatabase: filepath.Join(root, "tracking.db"),
		MinimumRefresh: time.Hour, Now: func() time.Time { return time.UnixMilli(5_000) },
	})
	if err != nil {
		t.Fatalf("NewCollector() error = %v", err)
	}
	service, err := NewQueryService(collector, reader)
	if err != nil {
		t.Fatalf("NewQueryService() error = %v", err)
	}
	initial, err := service.loadSnapshot(context.Background())
	if err != nil || initial.Generation != 1 {
		t.Fatalf("loadSnapshot(initial) = (%d, %v), want generation 1", initial.Generation, err)
	}

	if err := service.Refresh(context.Background()); err != nil {
		t.Fatalf("Refresh() error = %v", err)
	}
	refreshed, err := service.loadSnapshot(context.Background())
	if err != nil {
		t.Fatalf("loadSnapshot(after Refresh) error = %v", err)
	}
	if refreshed.Generation != 2 {
		t.Fatalf("snapshot generation after Refresh = %d, want 2", refreshed.Generation)
	}
}

func TestQueryServiceInvalidatesCachedSnapshotAfterLocalRefresh(t *testing.T) {
	root := t.TempDir()
	reader := &mutableSnapshotReader{snapshot: cursorQuerySnapshot("partial")}
	writer := &blockingSnapshotWriter{
		started: make(chan struct{}),
		release: make(chan struct{}),
		commit: func() {
			reader.appendSource(store.CursorSourceStatus{
				Provider: "cursor", SourceKey: SourceHooks, SourceType: "hooks",
				State: "available", CoverageState: "exact", LastSuccessAtMS: pointerInt64ForQueryTest(5_000),
			})
		},
	}
	t.Cleanup(func() {
		select {
		case <-writer.release:
		default:
			close(writer.release)
		}
	})
	collector, err := NewCollector(writer, Config{
		ProjectsRoot: filepath.Join(root, "projects"), StateDatabase: filepath.Join(root, "state.vscdb"),
		ConversationDatabase: filepath.Join(root, "conversation.db"), AITrackingDatabase: filepath.Join(root, "tracking.db"),
		MinimumRefresh: time.Hour, Now: func() time.Time { return time.UnixMilli(5_000) },
	})
	if err != nil {
		t.Fatalf("NewCollector() error = %v", err)
	}
	service, err := NewQueryService(collector, reader)
	if err != nil {
		t.Fatalf("NewQueryService() error = %v", err)
	}
	invalidated := make(chan struct{}, 1)
	service.SetRefreshNotifier(func() { invalidated <- struct{}{} })

	rangeValue := basequery.UTCTimeRange{StartAtMS: 1_000, EndAtMS: 5_000, TimeZone: "UTC"}
	response, err := service.UsageCost(context.Background(), usagecost.UsageCostRequest{
		ExactRange: &rangeValue, Granularity: usagecost.TrendDay,
	})
	if err != nil {
		t.Fatalf("UsageCost() error = %v", err)
	}
	if testContainsString(response.ProviderContext.Sources, SourceHooks) {
		t.Fatalf("initial provider sources = %v, want committed snapshot before refresh", response.ProviderContext.Sources)
	}
	select {
	case <-writer.started:
	case <-time.After(time.Second):
		t.Fatal("local Cursor refresh did not start")
	}
	close(writer.release)
	select {
	case <-invalidated:
	case <-time.After(time.Second):
		t.Fatal("local Cursor refresh did not publish an invalidation")
	}
	response, err = service.UsageCost(context.Background(), usagecost.UsageCostRequest{
		ExactRange: &rangeValue, Granularity: usagecost.TrendDay,
	})
	if err != nil {
		t.Fatalf("UsageCost(after refresh) error = %v", err)
	}
	if !testContainsString(response.ProviderContext.Sources, SourceHooks) {
		t.Fatalf("refreshed provider sources = %v", response.ProviderContext.Sources)
	}
}

func TestQueryServiceReturnsLocalSnapshotBeforeDashboardRefreshCompletes(t *testing.T) {
	root := t.TempDir()
	collector, err := NewCollector(discardSnapshotWriter{}, Config{
		ProjectsRoot: filepath.Join(root, "projects"), StateDatabase: filepath.Join(root, "state.vscdb"),
		ConversationDatabase: filepath.Join(root, "conversation.db"), AITrackingDatabase: filepath.Join(root, "tracking.db"),
		MinimumRefresh: time.Hour, Now: func() time.Time { return time.UnixMilli(5_000) },
	})
	if err != nil {
		t.Fatalf("NewCollector() error = %v", err)
	}
	refresher := &blockingDashboardRefresher{started: make(chan struct{}), release: make(chan struct{})}
	service, err := NewQueryService(collector, fixedSnapshotReader{snapshot: cursorQuerySnapshot("partial")}, refresher)
	if err != nil {
		t.Fatalf("NewQueryService() error = %v", err)
	}
	var invalidations atomic.Int64
	service.SetRefreshNotifier(func() { invalidations.Add(1) })

	rangeValue := basequery.UTCTimeRange{StartAtMS: 1_000, EndAtMS: 5_000, TimeZone: "UTC"}
	completed := make(chan error, 1)
	go func() {
		_, queryErr := service.UsageCost(context.Background(), usagecost.UsageCostRequest{
			ExactRange: &rangeValue, Granularity: usagecost.TrendDay,
		})
		completed <- queryErr
	}()

	select {
	case err := <-completed:
		if err != nil {
			t.Fatalf("UsageCost() error = %v", err)
		}
	case <-time.After(250 * time.Millisecond):
		t.Fatal("UsageCost() waited for the remote dashboard refresh")
	}
	select {
	case <-refresher.started:
	case <-time.After(time.Second):
		t.Fatal("dashboard refresh did not start in the background")
	}
	close(refresher.release)
	deadline := time.Now().Add(time.Second)
	for invalidations.Load() != 2 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if invalidations.Load() != 2 {
		t.Fatalf("local and dashboard completion invalidations = %d, want 2", invalidations.Load())
	}
}

func TestQueryServiceGrokBotRefreshDoesNotShareDashboardSingleFlight(t *testing.T) {
	root := t.TempDir()
	collector, err := NewCollector(discardSnapshotWriter{}, Config{
		ProjectsRoot: filepath.Join(root, "projects"), StateDatabase: filepath.Join(root, "state.vscdb"),
		ConversationDatabase: filepath.Join(root, "conversation.db"), AITrackingDatabase: filepath.Join(root, "tracking.db"),
		MinimumRefresh: time.Hour, Now: func() time.Time { return time.UnixMilli(5_000) },
	})
	if err != nil {
		t.Fatalf("NewCollector() error = %v", err)
	}
	dashboard := &blockingDashboardRefresher{started: make(chan struct{}), release: make(chan struct{})}
	service, err := NewQueryService(collector, fixedSnapshotReader{snapshot: cursorQuerySnapshot("partial")}, dashboard)
	if err != nil {
		t.Fatalf("NewQueryService() error = %v", err)
	}
	grokBot := &countingRefresher{}
	service.SetGrokBotRefresher(grokBot)
	rangeValue := basequery.UTCTimeRange{StartAtMS: 1_000, EndAtMS: 5_000, TimeZone: "UTC"}
	go func() {
		_, _ = service.UsageCost(context.Background(), usagecost.UsageCostRequest{
			ExactRange: &rangeValue, Granularity: usagecost.TrendDay,
		})
	}()
	select {
	case <-dashboard.started:
	case <-time.After(time.Second):
		t.Fatal("dashboard refresh did not start")
	}
	deadline := time.Now().Add(time.Second)
	for grokBot.calls.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if grokBot.calls.Load() == 0 {
		t.Fatal("grok bot refresh waited for the dashboard single-flight")
	}
	close(dashboard.release)
}

type countingRefresher struct{ calls atomic.Int64 }

func (refresher *countingRefresher) Refresh(context.Context) error {
	refresher.calls.Add(1)
	return nil
}

func TestQueryServiceRefreshQuotaWaitsForDashboardAndPublishesInvalidation(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	collector, err := NewCollector(discardSnapshotWriter{}, Config{
		ProjectsRoot: filepath.Join(root, "projects"), StateDatabase: filepath.Join(root, "state.vscdb"),
		ConversationDatabase: filepath.Join(root, "conversation.db"), AITrackingDatabase: filepath.Join(root, "tracking.db"),
		MinimumRefresh: time.Hour, Now: func() time.Time { return time.UnixMilli(5_000) },
	})
	if err != nil {
		t.Fatalf("NewCollector() error = %v", err)
	}
	refresher := &countingRefresher{}
	service, err := NewQueryService(collector, fixedSnapshotReader{snapshot: cursorQuerySnapshot("partial")}, refresher)
	if err != nil {
		t.Fatalf("NewQueryService() error = %v", err)
	}
	invalidated := make(chan struct{}, 1)
	service.SetRefreshNotifier(func() { invalidated <- struct{}{} })
	if err := service.RefreshQuota(context.Background()); err != nil {
		t.Fatalf("RefreshQuota() error = %v", err)
	}
	if refresher.calls.Load() != 1 {
		t.Fatalf("dashboard refreshes = %d, want 1", refresher.calls.Load())
	}
	select {
	case <-invalidated:
	default:
		t.Fatal("RefreshQuota() did not publish an invalidation")
	}
}

func TestQueryServicePublishesDashboardSourceAfterBackgroundRefresh(t *testing.T) {
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
	refreshed := make(chan struct{}, 1)
	service.SetRefreshNotifier(func() { refreshed <- struct{}{} })
	response, err := service.UsageCost(context.Background(), usagecost.UsageCostRequest{
		ExactRange: &rangeValue, Granularity: usagecost.TrendDay,
	})
	if err != nil {
		t.Fatalf("UsageCost() error = %v", err)
	}
	if testContainsString(response.ProviderContext.Sources, SourceDashboard) {
		t.Fatalf("initial provider sources = %v, remote refresh must not delay the local response", response.ProviderContext.Sources)
	}
	select {
	case <-refreshed:
	case <-time.After(time.Second):
		t.Fatal("dashboard refresh did not publish an invalidation")
	}
	response, err = service.UsageCost(context.Background(), usagecost.UsageCostRequest{
		ExactRange: &rangeValue, Granularity: usagecost.TrendDay,
	})
	if err != nil {
		t.Fatalf("UsageCost(after refresh) error = %v", err)
	}
	if !testContainsString(response.ProviderContext.Sources, SourceDashboard) {
		t.Fatalf("refreshed provider sources = %v", response.ProviderContext.Sources)
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

func TestCursorDashboardUsageIncludesPerModelTrendBuckets(t *testing.T) {
	snapshot := cursorQuerySnapshot("partial")
	snapshot.DashboardGeneration = 9
	snapshot.DashboardCollectedAtMS = 172_800_000
	snapshot.DashboardWindowStartMS = 0
	snapshot.DashboardWindowEndMS = 172_800_000
	firstModel, secondModel := "cursor-grok-4.6-xhigh", "cursor-grok-4.6-xhigh-fast"
	snapshot.DashboardUsageEvents = []store.CursorDashboardUsageEvent{
		{EventFingerprint: "first", OccurrenceCount: 1, OccurredAtMS: 10_000, ModelKey: &firstModel, InputTokens: 10, OutputTokens: 20},
		{EventFingerprint: "second", OccurrenceCount: 1, OccurredAtMS: 86_410_000, ModelKey: &secondModel, InputTokens: 30, OutputTokens: 40},
	}
	service := cursorQueryFixture(t, snapshot)
	rangeValue := basequery.UTCTimeRange{StartAtMS: 0, EndAtMS: 172_800_000, TimeZone: "UTC"}
	response, err := service.UsageCost(context.Background(), usagecost.UsageCostRequest{
		ExactRange: &rangeValue, Granularity: usagecost.TrendDay,
	})
	if err != nil {
		t.Fatalf("UsageCost() error = %v", err)
	}
	if len(response.Models) != 2 {
		t.Fatalf("models = %#v, want two model groups", response.Models)
	}
	for _, model := range response.Models {
		if len(model.Trend) != 1 || model.Trend[0].Totals.TotalTokens.Value == nil {
			t.Fatalf("model %q trend = %#v, want one real daily bucket", model.DimensionKey, model.Trend)
		}
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
	if len(current.Current.Windows) != 3 {
		t.Fatalf("current windows = %#v", current.Current.Windows)
	}
	if current.ProviderContext.EffectiveProvider != "cursor" ||
		!testContainsString(current.ProviderContext.Sources, SourceDashboard) ||
		!testContainsString(current.ProviderContext.Capabilities, "quota") {
		t.Fatalf("quota provider context = %#v", current.ProviderContext)
	}
	models, other, grokBot := current.Current.Windows[0], current.Current.Windows[1], current.Current.Windows[2]
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
	if grokBot.LimitID != grokBotLimitID || grokBot.UsedPercent != nil ||
		grokBot.UnknownReason == nil || *grokBot.UnknownReason != quotaquery.CurrentUnknownNeverLoaded {
		t.Fatalf("Grok Bot placeholder = %#v", grokBot)
	}

	pace, err := service.QuotaPace(context.Background(), evaluatedAt)
	if err != nil {
		t.Fatalf("QuotaPace() error = %v", err)
	}
	if len(pace.Pace.Windows) != 3 || pace.Pace.Windows[0].UsedPercent == nil ||
		*pace.Pace.Windows[0].UsedPercent != 7 || pace.Pace.Windows[0].ElapsedPercent == nil ||
		*pace.Pace.Windows[0].ElapsedPercent != 50 || pace.Pace.Windows[1].UsedPercent == nil ||
		*pace.Pace.Windows[1].UsedPercent != 0 {
		t.Fatalf("monthly quota pace = %#v", pace.Pace.Windows)
	}
}

func TestCursorQuotaReturnsLastKnownPercentagesBeforeDashboardRefreshCompletes(t *testing.T) {
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
	if response.Meta.Status != basequery.ResponseComplete || len(response.Current.Windows) != 3 ||
		response.Current.Windows[0].UsedPercent == nil || *response.Current.Windows[0].UsedPercent != 7 ||
		response.Current.Windows[0].Freshness != store.QuotaCurrentFresh {
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
	if len(response.Pace.Windows) != 3 || response.Pace.Windows[0].PreviousCycle == nil ||
		response.Pace.Windows[0].PreviousCycle.WindowStartAtMS != previousStart {
		t.Fatalf("calendar-month history = %#v", response.Pace.Windows)
	}
}

func TestCursorQuotaKeepsGrokBotWeeklyCycleAndNearestReset(t *testing.T) {
	t.Parallel()
	const day = int64(24 * time.Hour / time.Millisecond)
	monthStart := int64(1_780_000_000_000)
	monthEnd := monthStart + 31*day
	weekStart := monthStart + 20*day
	weekEnd := weekStart + 6*day + 12*time.Hour.Milliseconds()
	evaluatedAt := weekStart + day
	snapshot := cursorQuerySnapshot("partial")
	snapshot.DashboardQuotaObservations = []store.CursorDashboardQuotaObservation{
		{
			Generation: 1, LimitID: "cursor.models", UsedPercent: 40,
			CycleStartAtMS: monthStart, CycleEndAtMS: monthEnd, ObservedAtMS: evaluatedAt,
		},
		{
			Generation: 1, LimitID: "cursor.other_models", UsedPercent: 10,
			CycleStartAtMS: monthStart, CycleEndAtMS: monthEnd, ObservedAtMS: evaluatedAt,
		},
		{
			Generation: 2, LimitID: grokBotLimitID, UsedPercent: 0,
			CycleStartAtMS: weekStart, CycleEndAtMS: weekEnd, ObservedAtMS: evaluatedAt,
		},
	}
	snapshot.Sources = append(snapshot.Sources,
		store.CursorSourceStatus{
			Provider: "cursor", SourceKey: SourceDashboard, SourceType: "desktop_authenticated_rpc",
			State: "available", CoverageState: "exact", LastSuccessAtMS: pointerInt64ForQueryTest(evaluatedAt),
			LastAttemptAtMS: evaluatedAt,
		},
		store.CursorSourceStatus{
			Provider: "cursor", SourceKey: SourceDashboardGrokBot, SourceType: "desktop_authenticated_rpc",
			State: "available", CoverageState: "exact", RowCount: 1,
			LastSuccessAtMS: pointerInt64ForQueryTest(evaluatedAt),
			LastAttemptAtMS: evaluatedAt,
		},
	)
	service := cursorQueryFixture(t, snapshot)
	current, err := service.QuotaCurrent(context.Background(), evaluatedAt)
	if err != nil {
		t.Fatalf("QuotaCurrent() error = %v", err)
	}
	if len(current.Current.Windows) != 3 || current.Current.Windows[2].LimitID != grokBotLimitID ||
		current.Current.Windows[2].UsedPercent == nil || *current.Current.Windows[2].UsedPercent != 0 ||
		current.Current.Windows[2].WindowMinutes == nil ||
		*current.Current.Windows[2].WindowMinutes != (weekEnd-weekStart)/60_000 ||
		*current.Current.Windows[2].WindowMinutes == 10_080 {
		t.Fatalf("weekly grok bot window = %#v", current.Current.Windows[2])
	}
	if current.Current.NextReset.AtMS == nil || *current.Current.NextReset.AtMS != weekEnd ||
		current.Current.NextReset.TrustedWindowCount != 3 {
		t.Fatalf("nearest reset = %#v", current.Current.NextReset)
	}
	if len(current.Current.Sources) != 2 || current.Meta.Status != basequery.ResponseComplete {
		t.Fatalf("quota sources = %#v meta=%#v", current.Current.Sources, current.Meta)
	}

	previousWeekStart := weekStart - 7*day
	snapshot.DashboardQuotaObservations = append(snapshot.DashboardQuotaObservations, store.CursorDashboardQuotaObservation{
		Generation: 1, LimitID: grokBotLimitID, UsedPercent: 88,
		CycleStartAtMS: previousWeekStart, CycleEndAtMS: weekStart, ObservedAtMS: previousWeekStart + day,
	})
	service = cursorQueryFixture(t, snapshot)
	pace, err := service.QuotaPace(context.Background(), evaluatedAt)
	if err != nil {
		t.Fatalf("QuotaPace() error = %v", err)
	}
	if len(pace.Pace.Windows) != 3 || pace.Pace.Windows[2].UsedPercent == nil ||
		*pace.Pace.Windows[2].UsedPercent != 0 || pace.Pace.Windows[2].PreviousCycle == nil ||
		pace.Pace.Windows[2].PreviousCycle.WindowStartAtMS != previousWeekStart {
		t.Fatalf("grok bot weekly pace = %#v", pace.Pace.Windows[2])
	}
}

func TestCursorQuotaMarksGrokBotNotApplicableAndPartialFailureIndependently(t *testing.T) {
	t.Parallel()
	evaluatedAt := int64(2_000)
	snapshot := cursorQuerySnapshot("partial")
	snapshot.DashboardQuotaObservations = []store.CursorDashboardQuotaObservation{{
		Generation: 1, LimitID: "cursor.models", UsedPercent: 7,
		CycleStartAtMS: 1_000, CycleEndAtMS: 9_000, ObservedAtMS: 1_500,
	}}
	snapshot.Sources = append(snapshot.Sources,
		store.CursorSourceStatus{
			Provider: "cursor", SourceKey: SourceDashboard, SourceType: "desktop_authenticated_rpc",
			State: "available", CoverageState: "exact", LastSuccessAtMS: pointerInt64ForQueryTest(1_500),
			LastAttemptAtMS: 1_500,
		},
		store.CursorSourceStatus{
			Provider: "cursor", SourceKey: SourceDashboardGrokBot, SourceType: "desktop_authenticated_rpc",
			State: "available", CoverageState: "exact", RowCount: 0,
			LastSuccessAtMS: pointerInt64ForQueryTest(1_800),
			LastAttemptAtMS: 1_800,
		},
	)
	service := cursorQueryFixture(t, snapshot)
	current, err := service.QuotaCurrent(context.Background(), evaluatedAt)
	if err != nil {
		t.Fatalf("QuotaCurrent() error = %v", err)
	}
	grokBot := current.Current.Windows[2]
	if grokBot.UsedPercent != nil || grokBot.UnknownReason == nil ||
		*grokBot.UnknownReason != quotaquery.CurrentUnknownNotApplicable ||
		grokBot.Freshness != store.QuotaCurrentFresh {
		t.Fatalf("not_applicable grok bot = %#v", grokBot)
	}

	failure := "schema_incompatible"
	snapshot.Sources[len(snapshot.Sources)-1].State = "unavailable"
	snapshot.Sources[len(snapshot.Sources)-1].FailureCode = &failure
	snapshot.Sources[len(snapshot.Sources)-1].LastAttemptAtMS = 2_000
	service = cursorQueryFixture(t, snapshot)
	partial, err := service.QuotaCurrent(context.Background(), evaluatedAt)
	if err != nil {
		t.Fatalf("QuotaCurrent(partial) error = %v", err)
	}
	if partial.Meta.Status != basequery.ResponsePartial ||
		partial.Current.Windows[0].UsedPercent == nil || *partial.Current.Windows[0].UsedPercent != 7 ||
		partial.Current.Sources[1].FailureCode == nil ||
		string(*partial.Current.Sources[1].FailureCode) != failure {
		t.Fatalf("partial grok bot failure = meta=%#v current=%#v", partial.Meta, partial.Current)
	}
}

func TestCursorQuotaNotApplicableOverridesCurrentCycleAndSurvivesFailure(t *testing.T) {
	t.Parallel()
	const day = int64(24 * time.Hour / time.Millisecond)
	monthStart := int64(1_780_000_000_000)
	monthEnd := monthStart + 31*day
	weekStart := monthStart + 20*day
	weekEnd := weekStart + 6*day + 12*time.Hour.Milliseconds()
	evaluatedAt := weekStart + day
	snapshot := cursorQuerySnapshot("partial")
	snapshot.DashboardQuotaObservations = []store.CursorDashboardQuotaObservation{
		{
			Generation: 1, LimitID: "cursor.models", UsedPercent: 40,
			CycleStartAtMS: monthStart, CycleEndAtMS: monthEnd, ObservedAtMS: evaluatedAt,
		},
		{
			Generation: 1, LimitID: "cursor.other_models", UsedPercent: 10,
			CycleStartAtMS: monthStart, CycleEndAtMS: monthEnd, ObservedAtMS: evaluatedAt,
		},
		{
			Generation: 2, LimitID: grokBotLimitID, UsedPercent: 40,
			CycleStartAtMS: weekStart, CycleEndAtMS: weekEnd, ObservedAtMS: evaluatedAt,
		},
	}
	snapshot.Sources = append(snapshot.Sources,
		store.CursorSourceStatus{
			Provider: "cursor", SourceKey: SourceDashboard, SourceType: "desktop_authenticated_rpc",
			State: "available", CoverageState: "exact", RowCount: 2,
			LastSuccessAtMS: pointerInt64ForQueryTest(evaluatedAt), LastAttemptAtMS: evaluatedAt,
		},
		store.CursorSourceStatus{
			Provider: "cursor", SourceKey: SourceDashboardGrokBot, SourceType: "desktop_authenticated_rpc",
			State: "available", CoverageState: "exact", RowCount: 0,
			LastSuccessAtMS: pointerInt64ForQueryTest(evaluatedAt), LastAttemptAtMS: evaluatedAt,
		},
	)
	service := cursorQueryFixture(t, snapshot)
	current, err := service.QuotaCurrent(context.Background(), evaluatedAt)
	if err != nil {
		t.Fatalf("QuotaCurrent() error = %v", err)
	}
	grokBot := current.Current.Windows[2]
	if grokBot.UsedPercent != nil || grokBot.UnknownReason == nil ||
		*grokBot.UnknownReason != quotaquery.CurrentUnknownNotApplicable ||
		grokBot.Freshness != store.QuotaCurrentFresh {
		t.Fatalf("not_applicable must beat current-cycle observation = %#v", grokBot)
	}
	if current.Current.NextReset.AtMS == nil || *current.Current.NextReset.AtMS != monthEnd ||
		current.Current.NextReset.TrustedWindowCount != 2 {
		t.Fatalf("not_applicable must not own nearest reset = %#v", current.Current.NextReset)
	}
	if current.Current.Sources[1].UnknownReason == nil ||
		*current.Current.Sources[1].UnknownReason != quotaquery.CurrentUnknownNotApplicable {
		t.Fatalf("grok bot source = %#v", current.Current.Sources[1])
	}

	pace, err := service.QuotaPace(context.Background(), evaluatedAt)
	if err != nil {
		t.Fatalf("QuotaPace() error = %v", err)
	}
	if len(pace.Pace.Windows) != 3 || pace.Pace.Windows[2].UsedPercent != nil ||
		pace.Pace.Windows[2].PreviousCycle != nil || pace.Pace.Windows[2].UnknownReason == nil ||
		*pace.Pace.Windows[2].UnknownReason != quotaquery.PaceUnknownWindowUnavailable {
		t.Fatalf("not_applicable pace must drop old curve = %#v", pace.Pace.Windows[2])
	}

	failure := "read_failed"
	snapshot.Sources[len(snapshot.Sources)-1].State = "unavailable"
	snapshot.Sources[len(snapshot.Sources)-1].FailureCode = &failure
	snapshot.Sources[len(snapshot.Sources)-1].LastAttemptAtMS = evaluatedAt + 1
	service = cursorQueryFixture(t, snapshot)
	partial, err := service.QuotaCurrent(context.Background(), evaluatedAt)
	if err != nil {
		t.Fatalf("QuotaCurrent(partial) error = %v", err)
	}
	if partial.Meta.Status != basequery.ResponsePartial ||
		partial.Current.Windows[2].UsedPercent != nil ||
		partial.Current.Windows[2].UnknownReason == nil ||
		*partial.Current.Windows[2].UnknownReason != quotaquery.CurrentUnknownNotApplicable ||
		partial.Current.Windows[2].Freshness != store.QuotaCurrentStale ||
		partial.Current.NextReset.AtMS == nil || *partial.Current.NextReset.AtMS != monthEnd ||
		partial.Current.Sources[1].FailureCode == nil ||
		string(*partial.Current.Sources[1].FailureCode) != failure {
		t.Fatalf("failure must not revive old grok bot percent = meta=%#v current=%#v", partial.Meta, partial.Current)
	}
}

func TestCursorQuotaNotApplicableOverridesExpiredGrokBotHistory(t *testing.T) {
	t.Parallel()
	evaluatedAt := int64(2_000)
	snapshot := cursorQuerySnapshot("partial")
	snapshot.DashboardQuotaObservations = []store.CursorDashboardQuotaObservation{
		{
			Generation: 1, LimitID: "cursor.models", UsedPercent: 7,
			CycleStartAtMS: 1_000, CycleEndAtMS: 9_000, ObservedAtMS: 1_500,
		},
		{
			Generation: 2, LimitID: grokBotLimitID, UsedPercent: 88,
			CycleStartAtMS: 100, CycleEndAtMS: 900, ObservedAtMS: 800,
		},
	}
	snapshot.Sources = append(snapshot.Sources,
		store.CursorSourceStatus{
			Provider: "cursor", SourceKey: SourceDashboard, SourceType: "desktop_authenticated_rpc",
			State: "available", CoverageState: "exact", RowCount: 1,
			LastSuccessAtMS: pointerInt64ForQueryTest(1_500), LastAttemptAtMS: 1_500,
		},
		store.CursorSourceStatus{
			Provider: "cursor", SourceKey: SourceDashboardGrokBot, SourceType: "desktop_authenticated_rpc",
			State: "available", CoverageState: "exact", RowCount: 0,
			LastSuccessAtMS: pointerInt64ForQueryTest(1_800), LastAttemptAtMS: 1_800,
		},
	)
	service := cursorQueryFixture(t, snapshot)
	current, err := service.QuotaCurrent(context.Background(), evaluatedAt)
	if err != nil {
		t.Fatalf("QuotaCurrent() error = %v", err)
	}
	grokBot := current.Current.Windows[2]
	if grokBot.UsedPercent != nil || grokBot.UnknownReason == nil ||
		*grokBot.UnknownReason != quotaquery.CurrentUnknownNotApplicable ||
		grokBot.Freshness != store.QuotaCurrentFresh {
		t.Fatalf("expired history after not_applicable = %#v", grokBot)
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
