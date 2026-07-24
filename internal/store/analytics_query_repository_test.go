package store

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/SisyphusSQ/codex-pulse/internal/pricing"
	storesqlite "github.com/SisyphusSQ/codex-pulse/internal/store/sqlite"
)

func TestUsageCostRangeReadsBoundedActiveGenerationEvidence(t *testing.T) {
	t.Parallel()

	repository := openRuntimeRepository(t)
	ctx := context.Background()
	if err := repository.AddPricingVersion(ctx, pricing.BuiltinOpenAI20260714()); err != nil {
		t.Fatalf("AddPricingVersion() error = %v", err)
	}
	million, zero := int64(1_000_000), int64(0)
	seedCostTurn(t, repository, "analytics-priced", pointerTo("gpt-5.2-codex"),
		mustParseCostTime(t, "2026-07-01T01:00:00Z"), true, pricing.Usage{
			InputTokens: &million, CachedInputTokens: &zero,
			OutputTokens: &zero, ReasoningTokens: &zero,
		})
	seedCostTurn(t, repository, "analytics-unpriced", pointerTo("gpt-future"),
		mustParseCostTime(t, "2026-07-01T02:00:00Z"), true, pricing.Usage{
			InputTokens: &zero, CachedInputTokens: &zero,
			OutputTokens: &zero, ReasoningTokens: &zero,
		})
	seedCostTurn(t, repository, "analytics-exclusive-end", pointerTo("gpt-5.2-codex"),
		mustParseCostTime(t, "2026-07-02T16:00:00Z"), true, pricing.Usage{
			InputTokens: &million, CachedInputTokens: &zero,
			OutputTokens: &zero, ReasoningTokens: &zero,
		})
	_, err := repository.RebuildCostLedger(ctx, RebuildCostLedgerRequest{
		GenerationID: "analytics-active-v1", ReportingTimezone: "Asia/Shanghai",
		PricingSource: "openai-api", Currency: "USD", RollupVersion: 1,
		CalculatedAtMS: mustParseCostTime(t, "2026-07-03T00:00:00Z"),
	})
	if err != nil {
		t.Fatalf("RebuildCostLedger() error = %v", err)
	}

	snapshot, err := repository.UsageCostRange(ctx, AnalyticsRange{
		ReportingTimezone: "Asia/Shanghai",
		StartAtMS:         mustParseCostTime(t, "2026-06-30T16:00:00Z"),
		EndAtMS:           mustParseCostTime(t, "2026-07-02T16:00:00Z"),
	})
	if err != nil {
		t.Fatalf("UsageCostRange() error = %v", err)
	}
	if snapshot.Mode != AnalyticsReadActiveRollup || snapshot.Generation == nil ||
		snapshot.Generation.GenerationID != "analytics-active-v1" || len(snapshot.Daily) != 1 {
		t.Fatalf("active snapshot identity = %#v", snapshot)
	}
	daily := snapshot.Daily[0]
	if daily.BucketStartMS != mustParseCostTime(t, "2026-06-30T16:00:00Z") ||
		daily.TurnCount != 2 || daily.PricedTurnCount != 1 || daily.UnpricedTurnCount != 1 ||
		daily.EstimatedUSDMicros == nil || *daily.EstimatedUSDMicros <= 0 {
		t.Fatalf("active daily = %#v", daily)
	}
	if !reflect.DeepEqual(snapshot.PricingVersions, []string{"openai-api-2026-07-14"}) {
		t.Fatalf("pricing versions = %#v", snapshot.PricingVersions)
	}
	wantReasons := []CostReasonCount{{Reason: pricing.CostReasonModelNotListed, Count: 1}}
	if !reflect.DeepEqual(snapshot.UnpricedReasons, wantReasons) {
		t.Fatalf("unpriced reasons = %#v, want %#v", snapshot.UnpricedReasons, wantReasons)
	}
}

func TestUsageCostRangeBucketsActiveGenerationByHour(t *testing.T) {
	t.Parallel()

	repository := openRuntimeRepository(t)
	ctx := context.Background()
	if err := repository.AddPricingVersion(ctx, pricing.BuiltinOpenAI20260714()); err != nil {
		t.Fatalf("AddPricingVersion() error = %v", err)
	}
	one, zero := int64(1), int64(0)
	first := mustParseCostTime(t, "2026-07-22T01:15:00Z")
	second := mustParseCostTime(t, "2026-07-22T02:45:00Z")
	seedCostTurn(t, repository, "hour-one", pointerTo("gpt-5.2-codex"), first, true, pricing.Usage{
		InputTokens: &one, CachedInputTokens: &zero, OutputTokens: &zero, ReasoningTokens: &zero,
	})
	seedCostTurn(t, repository, "hour-two", pointerTo("gpt-5.2-codex"), second, true, pricing.Usage{
		InputTokens: &one, CachedInputTokens: &zero, OutputTokens: &zero, ReasoningTokens: &zero,
	})
	_, err := repository.RebuildCostLedger(ctx, RebuildCostLedgerRequest{
		GenerationID: "analytics-hour-v1", ReportingTimezone: "UTC",
		PricingSource: "openai-api", Currency: "USD", RollupVersion: 1,
		CalculatedAtMS: mustParseCostTime(t, "2026-07-22T03:00:00Z"),
	})
	if err != nil {
		t.Fatalf("RebuildCostLedger() error = %v", err)
	}
	filter := AnalyticsRange{
		ReportingTimezone: "UTC",
		StartAtMS:         mustParseCostTime(t, "2026-07-22T00:00:00Z"),
		EndAtMS:           mustParseCostTime(t, "2026-07-23T00:00:00Z"),
	}
	granularity := reflect.ValueOf(&filter).Elem().FieldByName("Granularity")
	if !granularity.IsValid() || granularity.Kind() != reflect.String || !granularity.CanSet() {
		t.Fatal("AnalyticsRange must expose a settable string Granularity field")
	}
	granularity.SetString("hour")

	snapshot, err := repository.UsageCostRange(ctx, filter)
	if err != nil {
		t.Fatalf("UsageCostRange(hour) error = %v", err)
	}
	wantBuckets := []int64{
		mustParseCostTime(t, "2026-07-22T01:00:00Z"),
		mustParseCostTime(t, "2026-07-22T02:00:00Z"),
	}
	buckets := make([]int64, 0, len(snapshot.Daily))
	for _, row := range snapshot.Daily {
		buckets = append(buckets, row.BucketStartMS)
		if row.EstimatedUSDMicros == nil {
			t.Fatalf("hourly row lost active pricing evidence: %#v", row)
		}
	}
	if !reflect.DeepEqual(buckets, wantBuckets) {
		t.Fatalf("hourly buckets = %#v, want %#v", buckets, wantBuckets)
	}
}

func TestUsageCostRangeExactPartialDayPreservesActivePricing(t *testing.T) {
	t.Parallel()

	repository := openRuntimeRepository(t)
	ctx := context.Background()
	if err := repository.AddPricingVersion(ctx, pricing.BuiltinOpenAI20260714()); err != nil {
		t.Fatalf("AddPricingVersion() error = %v", err)
	}
	million, zero := int64(1_000_000), int64(0)
	seedCostTurn(t, repository, "exact-before", pointerTo("gpt-5.2-codex"),
		mustParseCostTime(t, "2026-07-22T01:00:00Z"), true, pricing.Usage{
			InputTokens: &million, CachedInputTokens: &zero, OutputTokens: &zero, ReasoningTokens: &zero,
		})
	seedCostTurn(t, repository, "exact-inside", pointerTo("gpt-5.2-codex"),
		mustParseCostTime(t, "2026-07-22T02:00:00Z"), true, pricing.Usage{
			InputTokens: &million, CachedInputTokens: &zero, OutputTokens: &zero, ReasoningTokens: &zero,
		})
	_, err := repository.RebuildCostLedger(ctx, RebuildCostLedgerRequest{
		GenerationID: "analytics-exact-v1", ReportingTimezone: "UTC",
		PricingSource: "openai-api", Currency: "USD", RollupVersion: 1,
		CalculatedAtMS: mustParseCostTime(t, "2026-07-22T03:00:00Z"),
	})
	if err != nil {
		t.Fatalf("RebuildCostLedger() error = %v", err)
	}
	start := mustParseCostTime(t, "2026-07-22T01:30:00Z")
	snapshot, err := repository.UsageCostRange(ctx, AnalyticsRange{
		ReportingTimezone: "UTC", StartAtMS: start,
		EndAtMS: mustParseCostTime(t, "2026-07-22T03:00:00Z"), Exact: true,
	})
	if err != nil {
		t.Fatalf("UsageCostRange(exact) error = %v", err)
	}
	if snapshot.Mode != AnalyticsReadActiveRollup || snapshot.Generation == nil ||
		len(snapshot.Daily) != 1 || snapshot.Daily[0].BucketStartMS != start ||
		snapshot.Daily[0].TurnCount != 1 || snapshot.Daily[0].EstimatedUSDMicros == nil {
		t.Fatalf("exact active snapshot = %#v", snapshot)
	}
}

func TestUsageCostRangeFallsBackToFinalDetailWithoutInventingCost(t *testing.T) {
	t.Parallel()

	repository := openRuntimeRepository(t)
	ctx := context.Background()
	one, zero := int64(1), int64(0)
	seedCostTurn(t, repository, "analytics-fallback", pointerTo("gpt-5.2-codex"),
		mustParseCostTime(t, "2026-07-01T03:00:00Z"), true, pricing.Usage{
			InputTokens: &one, CachedInputTokens: &zero,
			OutputTokens: nil, ReasoningTokens: &zero,
		})

	first, err := repository.UsageCostRange(ctx, AnalyticsRange{
		ReportingTimezone: "Asia/Shanghai",
		StartAtMS:         mustParseCostTime(t, "2026-06-30T16:00:00Z"),
		EndAtMS:           mustParseCostTime(t, "2026-07-01T16:00:00Z"),
	})
	if err != nil {
		t.Fatalf("UsageCostRange(fallback) error = %v", err)
	}
	second, err := repository.UsageCostRange(ctx, AnalyticsRange{
		ReportingTimezone: "Asia/Shanghai",
		StartAtMS:         mustParseCostTime(t, "2026-06-30T16:00:00Z"),
		EndAtMS:           mustParseCostTime(t, "2026-07-01T16:00:00Z"),
	})
	if err != nil {
		t.Fatalf("UsageCostRange(replay) error = %v", err)
	}
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("fallback replay drifted:\nfirst=%#v\nsecond=%#v", first, second)
	}
	if first.Mode != AnalyticsReadDetailFallback || first.Generation != nil ||
		first.PricingVersions == nil || first.UnpricedReasons == nil || len(first.Daily) != 1 {
		t.Fatalf("fallback snapshot shape = %#v", first)
	}
	daily := first.Daily[0]
	if daily.TurnCount != 1 || daily.PricedTurnCount != 0 || daily.UnpricedTurnCount != 1 ||
		daily.InputTokens == nil || *daily.InputTokens != 1 ||
		daily.CachedInputTokens == nil || *daily.CachedInputTokens != 0 ||
		daily.OutputTokens != nil || daily.TotalTokens != nil || daily.EstimatedUSDMicros != nil {
		t.Fatalf("fallback daily invented or lost facts = %#v", daily)
	}
}

func TestUsageCostRangeRejectsInvalidRangeAndHonorsCancellation(t *testing.T) {
	t.Parallel()

	repository := openRuntimeRepository(t)
	for _, filter := range []AnalyticsRange{
		{ReportingTimezone: "", StartAtMS: 1, EndAtMS: 2},
		{ReportingTimezone: "Local", StartAtMS: 1, EndAtMS: 2},
		{ReportingTimezone: "Mars/Olympus", StartAtMS: 1, EndAtMS: 2},
		{ReportingTimezone: "UTC", StartAtMS: -1, EndAtMS: 2},
		{ReportingTimezone: "UTC", StartAtMS: 2, EndAtMS: 2},
	} {
		if _, err := repository.UsageCostRange(context.Background(), filter); !errors.Is(err, ErrInvalidRecord) {
			t.Fatalf("UsageCostRange(%#v) error = %v, want ErrInvalidRecord", filter, err)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := repository.UsageCostRange(ctx, AnalyticsRange{
		ReportingTimezone: "UTC", StartAtMS: 1, EndAtMS: 2,
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("UsageCostRange(cancelled) error = %v, want context.Canceled", err)
	}
}

func TestValidateAnalyticsRangeEnforcesCalendarDayLimit(t *testing.T) {
	t.Parallel()

	accepted := []AnalyticsRange{
		{
			ReportingTimezone: "UTC",
			StartAtMS:         mustParseCostTime(t, "2024-01-01T00:00:00Z"),
			EndAtMS:           mustParseCostTime(t, "2025-01-01T00:00:00Z"),
		},
		{
			ReportingTimezone: "America/Los_Angeles",
			StartAtMS:         mustParseCostTime(t, "2026-03-08T08:00:00Z"),
			EndAtMS:           mustParseCostTime(t, "2026-03-09T07:00:00Z"),
		},
		{
			ReportingTimezone: "America/Los_Angeles",
			StartAtMS:         mustParseCostTime(t, "2026-11-01T07:00:00Z"),
			EndAtMS:           mustParseCostTime(t, "2026-11-02T08:00:00Z"),
		},
	}
	for _, filter := range accepted {
		if _, err := validateAnalyticsRange(filter); err != nil {
			t.Fatalf("validateAnalyticsRange(%#v) error = %v", filter, err)
		}
	}
	rejected := AnalyticsRange{
		ReportingTimezone: "UTC",
		StartAtMS:         mustParseCostTime(t, "2024-01-01T00:00:00Z"),
		EndAtMS:           mustParseCostTime(t, "2025-01-02T00:00:00Z"),
	}
	if _, err := validateAnalyticsRange(rejected); !errors.Is(err, ErrInvalidRecord) {
		t.Fatalf("validateAnalyticsRange(367 days) error = %v, want ErrInvalidRecord", err)
	}
}

func TestUsageCostRangeSurvivesStoreRestart(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()
	if err := os.Chmod(directory, 0o700); err != nil {
		t.Fatalf("Chmod(temp dir) error = %v", err)
	}
	databasePath := filepath.Join(directory, "analytics-restart.db")
	open := func() (*storesqlite.Store, *Repository) {
		database, err := storesqlite.Open(context.Background(), storesqlite.Config{Path: databasePath})
		if err != nil {
			t.Fatalf("sqlite.Open() error = %v", err)
		}
		repository := NewRepository(database)
		if err := repository.EnsureApplicationSchema(context.Background()); err != nil {
			t.Fatalf("EnsureApplicationSchema() error = %v", err)
		}
		return database, repository
	}

	database, repository := open()
	if err := repository.AddPricingVersion(context.Background(), pricing.BuiltinOpenAI20260714()); err != nil {
		t.Fatalf("AddPricingVersion() error = %v", err)
	}
	zero := int64(0)
	seedCostTurn(t, repository, "analytics-restart", pointerTo("gpt-5.2-codex"),
		mustParseCostTime(t, "2026-07-01T04:00:00Z"), true, pricing.Usage{
			InputTokens: &zero, CachedInputTokens: &zero,
			OutputTokens: &zero, ReasoningTokens: &zero,
		})
	_, err := repository.RebuildCostLedger(context.Background(), RebuildCostLedgerRequest{
		GenerationID: "analytics-restart-v1", ReportingTimezone: "Asia/Shanghai",
		PricingSource: "openai-api", Currency: "USD", RollupVersion: 1,
		CalculatedAtMS: mustParseCostTime(t, "2026-07-02T00:00:00Z"),
	})
	if err != nil {
		t.Fatalf("RebuildCostLedger() error = %v", err)
	}
	filter := AnalyticsRange{
		ReportingTimezone: "Asia/Shanghai",
		StartAtMS:         mustParseCostTime(t, "2026-06-30T16:00:00Z"),
		EndAtMS:           mustParseCostTime(t, "2026-07-01T16:00:00Z"),
	}
	before, err := repository.UsageCostRange(context.Background(), filter)
	if err != nil {
		t.Fatalf("UsageCostRange(before restart) error = %v", err)
	}
	if err := database.Close(context.Background()); err != nil {
		t.Fatalf("Close(before restart) error = %v", err)
	}

	reopened, reopenedRepository := open()
	t.Cleanup(func() {
		if err := reopened.Close(context.Background()); err != nil {
			t.Errorf("Close(reopened) error = %v", err)
		}
	})
	after, err := reopenedRepository.UsageCostRange(context.Background(), filter)
	if err != nil {
		t.Fatalf("UsageCostRange(after restart) error = %v", err)
	}
	if !reflect.DeepEqual(after, before) {
		t.Fatalf("restart changed range snapshot:\nbefore=%#v\nafter=%#v", before, after)
	}
}

func TestListSessionAnalyticsFiltersSortsAndPaginatesSafeFacts(t *testing.T) {
	t.Parallel()

	repository := openRuntimeRepository(t)
	seedSessionAnalyticsFixture(t, repository, true)
	filter := SessionAnalyticsFilter{
		ReportingTimezone: pointerTo("UTC"), Limit: 2,
		SortField: SessionAnalyticsSortLastActivity, SortDirection: AnalyticsSortDescending,
	}
	first, err := repository.ListSessionAnalytics(context.Background(), filter)
	if err != nil {
		t.Fatalf("ListSessionAnalytics(first) error = %v", err)
	}
	if first.Mode != AnalyticsReadActiveRollup || first.Generation == nil ||
		first.Generation.GenerationID != "session-analytics-v1" || first.MatchedCount != 4 ||
		len(first.Records) != 2 || first.NextCursor == nil || first.MatchedTotals == nil ||
		first.PageTotals == nil {
		t.Fatalf("first page shape = %#v", first)
	}
	if got := []string{first.Records[0].SessionID, first.Records[1].SessionID}; !reflect.DeepEqual(got, []string{"session-alpha", "session-beta"}) {
		t.Fatalf("first page IDs = %#v", got)
	}
	if first.Records[0].DisplayTitle != "Alpha safe title" ||
		first.Records[0].Project.ProjectID == nil || *first.Records[0].Project.ProjectID != "project-a" ||
		first.Records[0].Model.ModelKey == nil || *first.Records[0].Model.ModelKey != "model-a" ||
		first.Records[0].Activity != SessionActivityActive || first.Records[0].Rollup == nil {
		t.Fatalf("safe active record = %#v", first.Records[0])
	}
	secondFilter := filter
	secondFilter.Cursor = first.NextCursor
	second, err := repository.ListSessionAnalytics(context.Background(), secondFilter)
	if err != nil {
		t.Fatalf("ListSessionAnalytics(second) error = %v", err)
	}
	if got := []string{second.Records[0].SessionID, second.Records[1].SessionID}; !reflect.DeepEqual(got, []string{"session-delta", "session-gamma"}) || second.NextCursor != nil {
		t.Fatalf("second page = %#v", second)
	}
	for _, record := range append(append([]SessionAnalyticsRecord{}, first.Records...), second.Records...) {
		if record.SessionID == "session-gamma" && record.LastActivityAtMS != nil {
			t.Fatalf("null last activity was invented: %#v", record)
		}
	}

	startAt, endAt := int64(250), int64(400)
	activity := SessionActivityActive
	filtered, err := repository.ListSessionAnalytics(context.Background(), SessionAnalyticsFilter{
		ReportingTimezone: pointerTo("UTC"), Limit: 10,
		SortField: SessionAnalyticsSortTotalTokens, SortDirection: AnalyticsSortDescending,
		ProjectIDs: []string{"project-a"}, ModelKeys: []string{"model-a"}, Activity: &activity,
		LastActivityAtOrAfterMS: &startAt, LastActivityBeforeMS: &endAt,
	})
	if err != nil {
		t.Fatalf("ListSessionAnalytics(filtered) error = %v", err)
	}
	if filtered.MatchedCount != 1 || len(filtered.Records) != 1 ||
		filtered.Records[0].SessionID != "session-alpha" || filtered.PageTotals == nil ||
		filtered.PageTotals.TotalTokens == nil || *filtered.PageTotals.TotalTokens != 30 {
		t.Fatalf("filtered page = %#v", filtered)
	}

	costSorted, err := repository.ListSessionAnalytics(context.Background(), SessionAnalyticsFilter{
		ReportingTimezone: pointerTo("UTC"), Limit: 10,
		SortField: SessionAnalyticsSortEstimatedCost, SortDirection: AnalyticsSortDescending,
	})
	if err != nil {
		t.Fatalf("ListSessionAnalytics(cost) error = %v", err)
	}
	wantCostOrder := []string{"session-alpha", "session-delta", "session-gamma", "session-beta"}
	gotCostOrder := make([]string, 0, len(costSorted.Records))
	for _, record := range costSorted.Records {
		gotCostOrder = append(gotCostOrder, record.SessionID)
	}
	if !reflect.DeepEqual(gotCostOrder, wantCostOrder) {
		t.Fatalf("cost order = %#v, want %#v", gotCostOrder, wantCostOrder)
	}

	totalSorted, err := repository.ListSessionAnalytics(context.Background(), SessionAnalyticsFilter{
		ReportingTimezone: pointerTo("UTC"), Limit: 10,
		SortField: SessionAnalyticsSortTotalTokens, SortDirection: AnalyticsSortDescending,
	})
	if err != nil {
		t.Fatalf("ListSessionAnalytics(total) error = %v", err)
	}
	wantTotalOrder := []string{"session-alpha", "session-delta", "session-gamma", "session-beta"}
	gotTotalOrder := make([]string, 0, len(totalSorted.Records))
	for _, record := range totalSorted.Records {
		gotTotalOrder = append(gotTotalOrder, record.SessionID)
	}
	if !reflect.DeepEqual(gotTotalOrder, wantTotalOrder) {
		t.Fatalf("total order = %#v, want %#v", gotTotalOrder, wantTotalOrder)
	}

	ascending := SessionAnalyticsFilter{
		ReportingTimezone: pointerTo("UTC"), Limit: 1,
		SortField: SessionAnalyticsSortEstimatedCost, SortDirection: AnalyticsSortAscending,
	}
	gotAscending := make([]string, 0, 4)
	for {
		page, err := repository.ListSessionAnalytics(context.Background(), ascending)
		if err != nil {
			t.Fatalf("ListSessionAnalytics(ascending) error = %v", err)
		}
		if len(page.Records) != 1 {
			t.Fatalf("ascending page = %#v", page)
		}
		gotAscending = append(gotAscending, page.Records[0].SessionID)
		if page.NextCursor == nil {
			break
		}
		ascending.Cursor = page.NextCursor
	}
	wantAscending := []string{"session-delta", "session-alpha", "session-gamma", "session-beta"}
	if !reflect.DeepEqual(gotAscending, wantAscending) {
		t.Fatalf("ascending keyset = %#v, want %#v", gotAscending, wantAscending)
	}
}

func TestListSessionAnalyticsUsesExactContributionRange(t *testing.T) {
	t.Parallel()

	repository := openRuntimeRepository(t)
	seedProjectAnalyticsFixture(t, repository, false)
	page, err := repository.ListSessionAnalytics(context.Background(), SessionAnalyticsFilter{
		ReportingTimezone:       pointerTo("UTC"),
		LastActivityAtOrAfterMS: pointerTo(int64(5)), LastActivityBeforeMS: pointerTo(int64(25)),
		RangeExact: true, Limit: 10, SortField: SessionAnalyticsSortTotalTokens,
		SortDirection: AnalyticsSortDescending,
	})
	if err != nil {
		t.Fatalf("ListSessionAnalytics(exact) error = %v", err)
	}
	if len(page.Records) != 2 || page.Records[0].SessionID != "project-session-alpha" ||
		page.Records[1].SessionID != "project-session-delta" ||
		page.Records[0].Rollup == nil || page.Records[0].Rollup.TotalTokens == nil ||
		*page.Records[0].Rollup.TotalTokens != 10 || page.MatchedTotals == nil ||
		page.MatchedTotals.TotalTokens == nil || *page.MatchedTotals.TotalTokens != 15 {
		t.Fatalf("exact session page = %#v", page)
	}
}

func TestSessionAnalyticsDetailMatchesListAndMissingRollupDegrades(t *testing.T) {
	t.Parallel()

	repository := openRuntimeRepository(t)
	seedSessionAnalyticsFixture(t, repository, true)
	page, err := repository.ListSessionAnalytics(context.Background(), SessionAnalyticsFilter{
		ReportingTimezone: pointerTo("UTC"), Limit: 10,
		SortField: SessionAnalyticsSortLastActivity, SortDirection: AnalyticsSortDescending,
	})
	if err != nil {
		t.Fatalf("ListSessionAnalytics() error = %v", err)
	}
	detail, err := repository.SessionAnalytics(context.Background(), SessionAnalyticsDetailFilter{
		SessionID: "session-alpha", ReportingTimezone: pointerTo("UTC"), TurnLimit: 20,
	})
	if err != nil {
		t.Fatalf("SessionAnalytics() error = %v", err)
	}
	if !reflect.DeepEqual(detail.Record, page.Records[0]) || detail.Generation == nil ||
		detail.PricingVersions == nil || detail.UnpricedReasons == nil {
		t.Fatalf("detail/list mismatch:\ndetail=%#v\nlist=%#v", detail, page.Records[0])
	}
	if _, err := repository.SessionAnalytics(context.Background(), SessionAnalyticsDetailFilter{
		SessionID: "missing", ReportingTimezone: pointerTo("UTC"), TurnLimit: 20,
	}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("SessionAnalytics(missing) error = %v, want ErrNotFound", err)
	}

	withoutLedger := openRuntimeRepository(t)
	seedSessionAnalyticsFixture(t, withoutLedger, false)
	fallback, err := withoutLedger.ListSessionAnalytics(context.Background(), SessionAnalyticsFilter{
		Limit: 10, SortField: SessionAnalyticsSortLastActivity,
		SortDirection: AnalyticsSortDescending,
	})
	if err != nil {
		t.Fatalf("ListSessionAnalytics(fallback) error = %v", err)
	}
	if fallback.Mode != AnalyticsReadDetailFallback || fallback.Generation != nil ||
		fallback.MatchedTotals != nil || fallback.PageTotals != nil || len(fallback.Records) != 4 {
		t.Fatalf("fallback page shape = %#v", fallback)
	}
	for _, record := range fallback.Records {
		if record.Rollup != nil {
			t.Fatalf("fallback invented rollup: %#v", record)
		}
	}
}

func TestSessionAnalyticsDetailExposesBoundedTurnContract(t *testing.T) {
	t.Parallel()

	detailFilter := reflect.TypeOf(SessionAnalyticsDetailFilter{})
	for _, field := range []string{"TurnLimit", "TurnCursor"} {
		if _, found := detailFilter.FieldByName(field); !found {
			t.Fatalf("SessionAnalyticsDetailFilter missing %s", field)
		}
	}
	snapshot := reflect.TypeOf(SessionAnalyticsSnapshot{})
	for _, field := range []string{"Turns", "NextTurnCursor"} {
		if _, found := snapshot.FieldByName(field); !found {
			t.Fatalf("SessionAnalyticsSnapshot missing %s", field)
		}
	}
}

func TestSessionAnalyticsDetailReadsBoundedContentFreeTurnTimeline(t *testing.T) {
	t.Parallel()

	repository := openRuntimeRepository(t)
	seedSessionTurnTimelineFixture(t, repository, true)
	first, err := repository.SessionAnalytics(context.Background(), SessionAnalyticsDetailFilter{
		SessionID: "session-timeline", ReportingTimezone: pointerTo("UTC"), TurnLimit: 1,
	})
	if err != nil {
		t.Fatalf("SessionAnalytics(first turn page) error = %v", err)
	}
	if first.Mode != AnalyticsReadActiveRollup || len(first.Turns) != 1 ||
		first.NextTurnCursor == nil {
		t.Fatalf("first turn page = %#v", first)
	}
	active := first.Turns[0]
	if active.TurnID != "turn-timeline-active" || active.StartedAtMS != 200 ||
		active.CompletedAtMS != nil || active.Usage == nil || active.Usage.IsFinal ||
		active.Usage.InputTokens == nil || *active.Usage.InputTokens != 0 || active.Cost != nil ||
		active.Model.DisplayName == nil || *active.Model.DisplayName != "Model Safe" {
		t.Fatalf("active turn = %#v", active)
	}
	if first.NextTurnCursor.SessionID != "session-timeline" ||
		first.NextTurnCursor.TurnID != active.TurnID ||
		first.NextTurnCursor.StartedAtMS != active.StartedAtMS {
		t.Fatalf("first turn cursor = %#v, active = %#v", first.NextTurnCursor, active)
	}

	second, err := repository.SessionAnalytics(context.Background(), SessionAnalyticsDetailFilter{
		SessionID: "session-timeline", ReportingTimezone: pointerTo("UTC"), TurnLimit: 1,
		TurnCursor: first.NextTurnCursor,
	})
	if err != nil {
		t.Fatalf("SessionAnalytics(second turn page) error = %v", err)
	}
	if len(second.Turns) != 1 || second.NextTurnCursor != nil {
		t.Fatalf("second turn page = %#v", second)
	}
	completed := second.Turns[0]
	if completed.TurnID != "turn-timeline-complete" || completed.StartedAtMS != 100 ||
		completed.CompletedAtMS == nil || *completed.CompletedAtMS != 120 ||
		completed.Usage == nil || !completed.Usage.IsFinal || completed.Cost == nil ||
		completed.Cost.Status != pricing.CostStatusPriced ||
		completed.Cost.PricingVersion == nil ||
		*completed.Cost.PricingVersion != "openai-api-2026-07-14" ||
		completed.Cost.EstimatedUSDMicros == nil || *completed.Cost.EstimatedUSDMicros != 100 {
		t.Fatalf("completed turn = %#v", completed)
	}

	fallbackRepository := openRuntimeRepository(t)
	seedSessionTurnTimelineFixture(t, fallbackRepository, false)
	fallback, err := fallbackRepository.SessionAnalytics(
		context.Background(),
		SessionAnalyticsDetailFilter{SessionID: "session-timeline", TurnLimit: 20},
	)
	if err != nil {
		t.Fatalf("SessionAnalytics(fallback turn page) error = %v", err)
	}
	if fallback.Mode != AnalyticsReadDetailFallback || len(fallback.Turns) != 2 {
		t.Fatalf("fallback turn page = %#v", fallback)
	}
	for _, turn := range fallback.Turns {
		if turn.Usage == nil || turn.Cost != nil {
			t.Fatalf("fallback invented or lost turn evidence: %#v", turn)
		}
	}
}

func TestSessionAnalyticsDetailReturnsKnownEmptyAndRejectsMissingTurnAttribution(t *testing.T) {
	t.Parallel()

	emptyRepository := openRuntimeRepository(t)
	seedSessionAnalyticsFixture(t, emptyRepository, true)
	empty, err := emptyRepository.SessionAnalytics(context.Background(), SessionAnalyticsDetailFilter{
		SessionID: "session-beta", ReportingTimezone: pointerTo("UTC"), TurnLimit: 20,
	})
	if err != nil {
		t.Fatalf("SessionAnalytics(known empty) error = %v", err)
	}
	if empty.Turns == nil || len(empty.Turns) != 0 || empty.NextTurnCursor != nil {
		t.Fatalf("known-empty turn page = %#v", empty)
	}

	missingRepository := openRuntimeRepository(t)
	seedSessionAnalyticsFixture(t, missingRepository, true)
	if err := missingRepository.database.Write(context.Background(), func(
		ctx context.Context,
		transaction storesqlite.WriteTx,
	) error {
		return transaction.WithContext(ctx).Where("turn_id = ?", "turn-session-alpha").
			Delete(&turnAttributionModel{}).Error
	}); err != nil {
		t.Fatalf("delete turn attribution: %v", err)
	}
	_, err = missingRepository.SessionAnalytics(context.Background(), SessionAnalyticsDetailFilter{
		SessionID: "session-alpha", ReportingTimezone: pointerTo("UTC"), TurnLimit: 20,
	})
	if !errors.Is(err, ErrInvalidRecord) {
		t.Fatalf("SessionAnalytics(missing turn attribution) error = %v, want ErrInvalidRecord", err)
	}
}

func TestSessionAnalyticsRejectsInvalidFiltersAndHonorsCancellation(t *testing.T) {
	t.Parallel()

	repository := openRuntimeRepository(t)
	for _, filter := range []SessionAnalyticsFilter{
		{Limit: 0, SortField: SessionAnalyticsSortLastActivity, SortDirection: AnalyticsSortDescending},
		{Limit: 101, SortField: SessionAnalyticsSortLastActivity, SortDirection: AnalyticsSortDescending},
		{Limit: 1, SortField: "private-field", SortDirection: AnalyticsSortDescending},
		{Limit: 1, SortField: SessionAnalyticsSortLastActivity, SortDirection: "sideways"},
		{Limit: 1, SortField: SessionAnalyticsSortLastActivity, SortDirection: AnalyticsSortDescending,
			ProjectIDs: []string{""}},
		{Limit: 1, SortField: SessionAnalyticsSortLastActivity, SortDirection: AnalyticsSortDescending,
			Cursor: &SessionAnalyticsCursor{SessionID: "session-a", Null: true, Value: pointerTo(int64(1))}},
	} {
		if _, err := repository.ListSessionAnalytics(context.Background(), filter); !errors.Is(err, ErrInvalidRecord) {
			t.Fatalf("ListSessionAnalytics(%#v) error = %v, want ErrInvalidRecord", filter, err)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := repository.ListSessionAnalytics(ctx, SessionAnalyticsFilter{
		Limit: 1, SortField: SessionAnalyticsSortLastActivity, SortDirection: AnalyticsSortDescending,
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("ListSessionAnalytics(cancelled) error = %v", err)
	}
	for _, filter := range []SessionAnalyticsDetailFilter{
		{SessionID: "session-a", TurnLimit: 0},
		{SessionID: "session-a", TurnLimit: 51},
		{SessionID: "session-a", TurnLimit: 1, TurnCursor: &SessionTurnAnalyticsCursor{
			SessionID: "session-b", TurnID: "turn-a", StartedAtMS: 1,
		}},
		{SessionID: "session-a", TurnLimit: 1, TurnCursor: &SessionTurnAnalyticsCursor{
			SessionID: "session-a", TurnID: "", StartedAtMS: 1,
		}},
	} {
		if _, err := repository.SessionAnalytics(context.Background(), filter); !errors.Is(err, ErrInvalidRecord) {
			t.Fatalf("SessionAnalytics(%#v) error = %v, want ErrInvalidRecord", filter, err)
		}
	}
	_, err = repository.SessionAnalytics(ctx, SessionAnalyticsDetailFilter{
		SessionID: "session-a", TurnLimit: 20,
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("SessionAnalytics(cancelled) error = %v", err)
	}
}

func TestSessionAnalyticsSurvivesStoreRestart(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()
	if err := os.Chmod(directory, 0o700); err != nil {
		t.Fatalf("Chmod(temp dir) error = %v", err)
	}
	databasePath := filepath.Join(directory, "session-analytics-restart.db")
	open := func() (*storesqlite.Store, *Repository) {
		database, err := storesqlite.Open(context.Background(), storesqlite.Config{Path: databasePath})
		if err != nil {
			t.Fatalf("sqlite.Open() error = %v", err)
		}
		repository := NewRepository(database)
		if err := repository.EnsureApplicationSchema(context.Background()); err != nil {
			t.Fatalf("EnsureApplicationSchema() error = %v", err)
		}
		return database, repository
	}

	database, repository := open()
	seedSessionAnalyticsFixture(t, repository, true)
	filter := SessionAnalyticsFilter{
		ReportingTimezone: pointerTo("UTC"), Limit: 10,
		SortField: SessionAnalyticsSortLastActivity, SortDirection: AnalyticsSortDescending,
	}
	before, err := repository.ListSessionAnalytics(context.Background(), filter)
	if err != nil {
		t.Fatalf("ListSessionAnalytics(before restart) error = %v", err)
	}
	if err := database.Close(context.Background()); err != nil {
		t.Fatalf("Close(before restart) error = %v", err)
	}

	reopened, reopenedRepository := open()
	t.Cleanup(func() {
		if err := reopened.Close(context.Background()); err != nil {
			t.Errorf("Close(reopened) error = %v", err)
		}
	})
	after, err := reopenedRepository.ListSessionAnalytics(context.Background(), filter)
	if err != nil {
		t.Fatalf("ListSessionAnalytics(after restart) error = %v", err)
	}
	if !reflect.DeepEqual(after, before) {
		t.Fatalf("restart changed session page:\nbefore=%#v\nafter=%#v", before, after)
	}
}

func TestSessionTurnAnalyticsCursorSurvivesStoreRestart(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()
	if err := os.Chmod(directory, 0o700); err != nil {
		t.Fatalf("Chmod(temp dir) error = %v", err)
	}
	databasePath := filepath.Join(directory, "session-turn-analytics-restart.db")
	open := func() (*storesqlite.Store, *Repository) {
		database, err := storesqlite.Open(context.Background(), storesqlite.Config{Path: databasePath})
		if err != nil {
			t.Fatalf("sqlite.Open() error = %v", err)
		}
		repository := NewRepository(database)
		if err := repository.EnsureApplicationSchema(context.Background()); err != nil {
			t.Fatalf("EnsureApplicationSchema() error = %v", err)
		}
		return database, repository
	}

	database, repository := open()
	seedSessionTurnTimelineFixture(t, repository, true)
	first, err := repository.SessionAnalytics(context.Background(), SessionAnalyticsDetailFilter{
		SessionID: "session-timeline", ReportingTimezone: pointerTo("UTC"), TurnLimit: 1,
	})
	if err != nil || len(first.Turns) != 1 || first.NextTurnCursor == nil {
		t.Fatalf("SessionAnalytics(first before restart) = %#v, %v", first, err)
	}
	if err := database.Close(context.Background()); err != nil {
		t.Fatalf("Close(before restart) error = %v", err)
	}

	reopened, reopenedRepository := open()
	t.Cleanup(func() {
		if err := reopened.Close(context.Background()); err != nil {
			t.Errorf("Close(reopened) error = %v", err)
		}
	})
	second, err := reopenedRepository.SessionAnalytics(context.Background(), SessionAnalyticsDetailFilter{
		SessionID: "session-timeline", ReportingTimezone: pointerTo("UTC"), TurnLimit: 1,
		TurnCursor: first.NextTurnCursor,
	})
	if err != nil {
		t.Fatalf("SessionAnalytics(second after restart) error = %v", err)
	}
	if len(second.Turns) != 1 || second.NextTurnCursor != nil ||
		second.Turns[0].TurnID != "turn-timeline-complete" ||
		second.Turns[0].Cost == nil || second.Turns[0].Usage == nil {
		t.Fatalf("SessionAnalytics(second after restart) = %#v", second)
	}
}

func TestSessionAnalyticsAmbiguousGenerationFallsBackWithoutTimezone(t *testing.T) {
	t.Parallel()

	repository := openRuntimeRepository(t)
	seedSessionAnalyticsFixture(t, repository, true)
	second := costRollupGenerationModel{
		GenerationID: "session-analytics-shanghai", ReportingTimezone: "Asia/Shanghai",
		PricingSource: "test", Currency: "USD", RollupVersion: 1,
		State: string(CostRollupGenerationActive), CreatedAtMS: 500,
		CompletedAtMS: pointerTo(int64(500)), UpdatedAtMS: 500,
	}
	if err := repository.database.Write(context.Background(), func(
		ctx context.Context, transaction storesqlite.WriteTx,
	) error {
		return transaction.WithContext(ctx).Create(&second).Error
	}); err != nil {
		t.Fatalf("create second timezone generation: %v", err)
	}

	ambiguous, err := repository.ListSessionAnalytics(context.Background(), SessionAnalyticsFilter{
		Limit: 10, SortField: SessionAnalyticsSortLastActivity,
		SortDirection: AnalyticsSortDescending,
	})
	if err != nil {
		t.Fatalf("ListSessionAnalytics(ambiguous) error = %v", err)
	}
	if ambiguous.Mode != AnalyticsReadAmbiguousFallback || ambiguous.Generation != nil ||
		ambiguous.MatchedTotals != nil || ambiguous.PageTotals != nil || len(ambiguous.Records) != 4 {
		t.Fatalf("ambiguous session page = %#v", ambiguous)
	}
	for _, record := range ambiguous.Records {
		if record.Rollup != nil {
			t.Fatalf("ambiguous session page selected a rollup: %#v", record)
		}
	}

	explicit, err := repository.ListSessionAnalytics(context.Background(), SessionAnalyticsFilter{
		ReportingTimezone: pointerTo("UTC"), Limit: 10,
		SortField: SessionAnalyticsSortLastActivity, SortDirection: AnalyticsSortDescending,
	})
	if err != nil || explicit.Mode != AnalyticsReadActiveRollup || explicit.Generation == nil ||
		explicit.Generation.ReportingTimezone != "UTC" {
		t.Fatalf("ListSessionAnalytics(explicit timezone) = %#v, %v", explicit, err)
	}
	detail, err := repository.SessionAnalytics(context.Background(), SessionAnalyticsDetailFilter{
		SessionID: "session-alpha", TurnLimit: 20,
	})
	if err != nil || detail.Mode != AnalyticsReadAmbiguousFallback || detail.Generation != nil ||
		detail.Record.Rollup != nil {
		t.Fatalf("SessionAnalytics(ambiguous) = %#v, %v", detail, err)
	}
}

func TestListProjectAnalyticsPreservesUnknownFiltersSortsAndPaginates(t *testing.T) {
	t.Parallel()

	repository := openRuntimeRepository(t)
	seedProjectAnalyticsFixture(t, repository, false)
	filter := ProjectAnalyticsFilter{
		Range: AnalyticsRange{ReportingTimezone: "UTC", StartAtMS: 0, EndAtMS: 2 * 86_400_000},
		Limit: 2, SortField: ProjectAnalyticsSortTotalTokens,
		SortDirection: AnalyticsSortDescending,
	}
	first, err := repository.ListProjectAnalytics(context.Background(), filter)
	if err != nil {
		t.Fatalf("ListProjectAnalytics(first) error = %v", err)
	}
	if first.Generation.GenerationID != "project-analytics-v1" || first.MatchedCount != 3 ||
		len(first.Records) != 2 || first.NextCursor == nil ||
		first.GlobalTotals.TotalTokens == nil || *first.GlobalTotals.TotalTokens != 42 ||
		first.MatchedTotals.TotalTokens == nil || *first.MatchedTotals.TotalTokens != 42 ||
		first.PageTotals.TotalTokens == nil || *first.PageTotals.TotalTokens != 37 ||
		first.PricingVersions == nil {
		t.Fatalf("first project page = %#v", first)
	}
	if got := []string{first.Records[0].DimensionKey, first.Records[1].DimensionKey}; !reflect.DeepEqual(got, []string{"project-a", "unknown|unknown|missing|missing"}) {
		t.Fatalf("first project IDs = %#v", got)
	}
	if first.Records[1].ProjectID != nil || first.Records[1].ProjectDisplayName != nil ||
		first.Records[1].AttributionConfidence != string(AttributionConfidenceUnknown) {
		t.Fatalf("unknown project dimension was lost = %#v", first.Records[1])
	}
	secondFilter := filter
	secondFilter.Cursor = first.NextCursor
	second, err := repository.ListProjectAnalytics(context.Background(), secondFilter)
	if err != nil {
		t.Fatalf("ListProjectAnalytics(second) error = %v", err)
	}
	if len(second.Records) != 1 || second.Records[0].DimensionKey != "project-b" ||
		second.NextCursor != nil || second.PageTotals.TotalTokens == nil ||
		*second.PageTotals.TotalTokens != 5 {
		t.Fatalf("second project page = %#v", second)
	}

	confidence := string(AttributionConfidenceUnknown)
	unknownOnly, err := repository.ListProjectAnalytics(context.Background(), ProjectAnalyticsFilter{
		Range: filter.Range, Limit: 10,
		SortField: ProjectAnalyticsSortLastActivity, SortDirection: AnalyticsSortDescending,
		Confidences: []string{confidence},
	})
	if err != nil {
		t.Fatalf("ListProjectAnalytics(confidence) error = %v", err)
	}
	if unknownOnly.MatchedCount != 1 || len(unknownOnly.Records) != 1 ||
		unknownOnly.Records[0].DimensionKey != "unknown|unknown|missing|missing" {
		t.Fatalf("unknown confidence page = %#v", unknownOnly)
	}
	highOnly, err := repository.ListProjectAnalytics(context.Background(), ProjectAnalyticsFilter{
		Range: filter.Range, Limit: 10,
		SortField: ProjectAnalyticsSortLastActivity, SortDirection: AnalyticsSortDescending,
		Confidences: []string{string(AttributionConfidenceHigh)},
	})
	if err != nil {
		t.Fatalf("ListProjectAnalytics(high confidence) error = %v", err)
	}
	if highOnly.MatchedCount != 1 || len(highOnly.Records) != 1 ||
		highOnly.Records[0].DimensionKey != "project-b" ||
		highOnly.MatchedTotals.TotalTokens == nil || *highOnly.MatchedTotals.TotalTokens != 5 {
		t.Fatalf("range confidence filter was applied before aggregation: %#v", highOnly)
	}

	projectOnly, err := repository.ListProjectAnalytics(context.Background(), ProjectAnalyticsFilter{
		Range: filter.Range, Limit: 10,
		SortField: ProjectAnalyticsSortDisplayName, SortDirection: AnalyticsSortAscending,
		ProjectIDs: []string{"project-a", "project-b"},
	})
	if err != nil {
		t.Fatalf("ListProjectAnalytics(project IDs) error = %v", err)
	}
	if got := []string{projectOnly.Records[0].DimensionKey, projectOnly.Records[1].DimensionKey}; !reflect.DeepEqual(got, []string{"project-a", "project-b"}) {
		t.Fatalf("display order = %#v", got)
	}
}

func TestProjectAnalyticsDetailMatchesListAndReconcilesGlobalTotals(t *testing.T) {
	t.Parallel()

	repository := openRuntimeRepository(t)
	seedProjectAnalyticsFixture(t, repository, false)
	rangeFilter := AnalyticsRange{
		ReportingTimezone: "UTC", StartAtMS: 0, EndAtMS: 2 * 86_400_000,
	}
	page, err := repository.ListProjectAnalytics(context.Background(), ProjectAnalyticsFilter{
		Range: rangeFilter, Limit: 10,
		SortField: ProjectAnalyticsSortTotalTokens, SortDirection: AnalyticsSortDescending,
	})
	if err != nil {
		t.Fatalf("ListProjectAnalytics() error = %v", err)
	}
	detail, err := repository.ProjectAnalytics(context.Background(), ProjectAnalyticsDetailFilter{
		Range: rangeFilter, DimensionKey: "project-a",
	})
	if err != nil {
		t.Fatalf("ProjectAnalytics() error = %v", err)
	}
	if !reflect.DeepEqual(detail.Record, page.Records[0]) || len(detail.Daily) != 2 ||
		!reflect.DeepEqual(detail.GlobalTotals, page.GlobalTotals) || detail.PricingVersions == nil {
		t.Fatalf("project detail/list mismatch:\ndetail=%#v\nlist=%#v", detail, page.Records[0])
	}
	if detail.Daily[0].BucketStartMS != 0 || detail.Daily[1].BucketStartMS != 86_400_000 {
		t.Fatalf("project detail daily order = %#v", detail.Daily)
	}
	if _, err := repository.ProjectAnalytics(context.Background(), ProjectAnalyticsDetailFilter{
		Range: rangeFilter, DimensionKey: "missing",
	}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("ProjectAnalytics(missing) error = %v, want ErrNotFound", err)
	}
	if err := repository.database.Write(context.Background(), func(
		ctx context.Context, transaction storesqlite.WriteTx,
	) error {
		return transaction.WithContext(ctx).Model(&projectUsageDailyModel{}).
			Where("generation_id = ? AND dimension_key = ?", "project-analytics-v1", "project-a").
			Update("attribution_source", "private-source-payload").Error
	}); err != nil {
		t.Fatalf("corrupt project attribution fixture: %v", err)
	}
	if _, err := repository.ListProjectAnalytics(context.Background(), ProjectAnalyticsFilter{
		Range: rangeFilter, Limit: 10,
		SortField: ProjectAnalyticsSortTotalTokens, SortDirection: AnalyticsSortDescending,
	}); !errors.Is(err, ErrInvalidRecord) {
		t.Fatalf("ListProjectAnalytics(corrupt attribution) error = %v, want ErrInvalidRecord", err)
	}

	drifted := openRuntimeRepository(t)
	seedProjectAnalyticsFixture(t, drifted, true)
	if _, err := drifted.ListProjectAnalytics(context.Background(), ProjectAnalyticsFilter{
		Range: rangeFilter, Limit: 10,
		SortField: ProjectAnalyticsSortTotalTokens, SortDirection: AnalyticsSortDescending,
	}); !errors.Is(err, ErrInvalidRecord) {
		t.Fatalf("ListProjectAnalytics(drifted) error = %v, want ErrInvalidRecord", err)
	}
}

func TestListProjectAnalyticsDecoratesExactSessionCountsAndBoundedTrend(t *testing.T) {
	t.Parallel()

	repository := openRuntimeRepository(t)
	seedProjectAnalyticsFixture(t, repository, false)
	page, err := repository.ListProjectAnalytics(context.Background(), ProjectAnalyticsFilter{
		Range: AnalyticsRange{
			ReportingTimezone: "UTC", StartAtMS: 0, EndAtMS: 2 * 86_400_000,
		},
		Limit: 10, SortField: ProjectAnalyticsSortTotalTokens,
		SortDirection: AnalyticsSortDescending,
	})
	if err != nil {
		t.Fatalf("ListProjectAnalytics() error = %v", err)
	}
	if len(page.Records) != 3 {
		t.Fatalf("project records = %#v", page.Records)
	}
	wantCounts := map[string]int64{
		"project-a": 2, "project-b": 1, "unknown|unknown|missing|missing": 1,
	}
	for _, record := range page.Records {
		if record.SessionCount != wantCounts[record.DimensionKey] {
			t.Fatalf("project %q session count = %d, want %d", record.DimensionKey,
				record.SessionCount, wantCounts[record.DimensionKey])
		}
		if len(record.Trend) == 0 || len(record.Trend) > 30 {
			t.Fatalf("project %q trend length = %d", record.DimensionKey, len(record.Trend))
		}
		for index, point := range record.Trend {
			if point.DimensionKey != record.DimensionKey ||
				(index > 0 && record.Trend[index-1].BucketStartMS >= point.BucketStartMS) {
				t.Fatalf("project %q trend = %#v", record.DimensionKey, record.Trend)
			}
		}
	}
	if got := []int64{
		page.Records[0].Trend[0].BucketStartMS,
		page.Records[0].Trend[1].BucketStartMS,
	}; !reflect.DeepEqual(got, []int64{0, 86_400_000}) {
		t.Fatalf("project-a trend buckets = %#v", got)
	}
}

func TestListProjectAnalyticsUsesExactContributionRange(t *testing.T) {
	t.Parallel()

	repository := openRuntimeRepository(t)
	seedProjectAnalyticsFixture(t, repository, false)
	page, err := repository.ListProjectAnalytics(context.Background(), ProjectAnalyticsFilter{
		Range: AnalyticsRange{
			ReportingTimezone: "UTC", StartAtMS: 5, EndAtMS: 25, Exact: true,
		},
		Limit: 10, SortField: ProjectAnalyticsSortTotalTokens,
		SortDirection: AnalyticsSortDescending,
	})
	if err != nil {
		t.Fatalf("ListProjectAnalytics(exact) error = %v", err)
	}
	if len(page.Records) != 2 || page.Records[0].DimensionKey != "project-a" ||
		page.Records[1].DimensionKey != "project-b" ||
		page.Records[0].Totals.TotalTokens == nil || *page.Records[0].Totals.TotalTokens != 10 ||
		page.GlobalTotals.TotalTokens == nil || *page.GlobalTotals.TotalTokens != 15 {
		t.Fatalf("exact project page = %#v", page)
	}
	for _, record := range page.Records {
		if len(record.Trend) != 1 || record.Trend[0].BucketStartMS != 5 {
			t.Fatalf("exact project trend = %#v", record.Trend)
		}
	}
}

func TestProjectAnalyticsUsesExactContributionRange(t *testing.T) {
	t.Parallel()

	repository := openRuntimeRepository(t)
	seedProjectAnalyticsFixture(t, repository, false)
	snapshot, err := repository.ProjectAnalytics(context.Background(), ProjectAnalyticsDetailFilter{
		Range: AnalyticsRange{
			ReportingTimezone: "UTC", StartAtMS: 5, EndAtMS: 25, Exact: true,
		},
		DimensionKey: "project-a", SessionLimit: 10, ModelLimit: 10,
	})
	if err != nil {
		t.Fatalf("ProjectAnalytics(exact) error = %v", err)
	}
	if snapshot.Record.Totals.TotalTokens == nil || *snapshot.Record.Totals.TotalTokens != 10 ||
		len(snapshot.Daily) != 1 || snapshot.Daily[0].BucketStartMS != 5 ||
		len(snapshot.Sessions) != 1 || snapshot.Sessions[0].SessionID != "project-session-alpha" {
		t.Fatalf("exact project detail = %#v", snapshot)
	}
}

func TestAppendProjectTrendKeepsLatestThirtyBuckets(t *testing.T) {
	t.Parallel()

	record := ProjectAnalyticsRecord{DimensionKey: "project-a", Trend: make([]ProjectUsageDaily, 0)}
	for bucket := int64(0); bucket < 31; bucket++ {
		appendProjectTrend(&record, ProjectUsageDaily{
			DimensionKey: "project-a", BucketStartMS: bucket,
		})
	}
	if len(record.Trend) != 30 || record.Trend[0].BucketStartMS != 1 ||
		record.Trend[29].BucketStartMS != 30 {
		t.Fatalf("bounded project trend = %#v", record.Trend)
	}
}

func TestProjectAnalyticsPagesProjectContributionSessionsAndModels(t *testing.T) {
	t.Parallel()

	repository := openRuntimeRepository(t)
	seedProjectAnalyticsFixture(t, repository, false)
	rangeFilter := AnalyticsRange{
		ReportingTimezone: "UTC", StartAtMS: 0, EndAtMS: 2 * 86_400_000,
	}
	first, err := repository.ProjectAnalytics(context.Background(), ProjectAnalyticsDetailFilter{
		Range: rangeFilter, DimensionKey: "project-a", SessionLimit: 1, ModelLimit: 1,
	})
	if err != nil {
		t.Fatalf("ProjectAnalytics(first) error = %v", err)
	}
	if first.Record.SessionCount != 2 || len(first.Sessions) != 1 ||
		first.NextSessionCursor == nil || len(first.Models) != 1 || first.NextModelCursor == nil {
		t.Fatalf("first project detail pages = %#v", first)
	}
	if first.Sessions[0].SessionID != "project-session-beta" ||
		first.Sessions[0].DisplayTitle != "Beta safe title" ||
		first.Sessions[0].Model.ModelKey == nil || *first.Sessions[0].Model.ModelKey != "model-b" ||
		first.Sessions[0].Totals.TotalTokens == nil || *first.Sessions[0].Totals.TotalTokens != 20 ||
		first.Sessions[0].Activity != SessionActivityIdle {
		t.Fatalf("first project session = %#v", first.Sessions[0])
	}
	if first.Models[0].DimensionKey != "model-b" || first.Models[0].Model.ModelKey == nil ||
		*first.Models[0].Model.ModelKey != "model-b" || first.Models[0].Totals.TotalTokens == nil ||
		*first.Models[0].Totals.TotalTokens != 20 {
		t.Fatalf("first project model = %#v", first.Models[0])
	}

	second, err := repository.ProjectAnalytics(context.Background(), ProjectAnalyticsDetailFilter{
		Range: rangeFilter, DimensionKey: "project-a", SessionLimit: 1,
		SessionCursor: first.NextSessionCursor, ModelLimit: 1, ModelCursor: first.NextModelCursor,
	})
	if err != nil {
		t.Fatalf("ProjectAnalytics(second) error = %v", err)
	}
	if len(second.Sessions) != 1 || second.Sessions[0].SessionID != "project-session-alpha" ||
		second.NextSessionCursor != nil || len(second.Models) != 1 ||
		second.Models[0].DimensionKey != "model-a" || second.NextModelCursor != nil {
		t.Fatalf("second project detail pages = %#v", second)
	}

	unknown, err := repository.ProjectAnalytics(context.Background(), ProjectAnalyticsDetailFilter{
		Range: rangeFilter, DimensionKey: "unknown|unknown|missing|missing",
		SessionLimit: 10, ModelLimit: 10,
	})
	if err != nil {
		t.Fatalf("ProjectAnalytics(unknown) error = %v", err)
	}
	if unknown.Record.SessionCount != 1 || len(unknown.Sessions) != 1 ||
		unknown.Sessions[0].SessionID != "project-session-gamma" || len(unknown.Models) != 1 ||
		unknown.Models[0].DimensionKey != "unknown|unknown|missing|missing" ||
		unknown.Models[0].Model.ModelKey != nil || unknown.Models[0].Totals.TotalTokens == nil ||
		*unknown.Models[0].Totals.TotalTokens != 7 {
		t.Fatalf("unknown project detail = %#v", unknown)
	}
}

func TestProjectAnalyticsContributionKeysetsCoverEqualNullAndUnknownEdges(t *testing.T) {
	t.Parallel()

	repository := openRuntimeRepository(t)
	seedProjectAnalyticsFixture(t, repository, false)
	seedProjectContributionPaginationEdges(t, repository)
	filter := ProjectAnalyticsDetailFilter{
		Range: AnalyticsRange{
			ReportingTimezone: "UTC", StartAtMS: 0, EndAtMS: 2 * 86_400_000,
		},
		DimensionKey: "project-a", SessionLimit: 1, ModelLimit: 1,
	}
	wantSessions := []string{
		"project-session-omega", "project-session-beta", "project-session-alpha",
	}
	wantModels := []string{
		"model-b", "model-a", "unknown|unknown|missing|missing",
		"unknown|low|invalid_model|invalid",
	}
	var sessions []string
	for pageNumber := 0; pageNumber < 4; pageNumber++ {
		page, err := repository.ProjectAnalytics(context.Background(), filter)
		if err != nil {
			t.Fatalf("ProjectAnalytics(session edge page %d) error = %v", pageNumber, err)
		}
		if page.Record.SessionCount != 3 || len(page.Sessions) != 1 || len(page.Models) != 1 {
			t.Fatalf("ProjectAnalytics(session edge page %d) = %#v", pageNumber, page)
		}
		sessions = append(sessions, page.Sessions[0].SessionID)
		filter.SessionCursor = page.NextSessionCursor
		if page.NextSessionCursor == nil {
			if pageNumber != 2 {
				t.Fatalf("session edge page %d cursor shape = %#v", pageNumber,
					page.NextSessionCursor)
			}
			break
		}
	}
	if !reflect.DeepEqual(sessions, wantSessions) {
		t.Fatalf("edge Session keyset traversal = %#v, want %#v", sessions, wantSessions)
	}

	filter.SessionLimit = 50
	filter.SessionCursor = nil
	filter.ModelCursor = nil
	var models []string
	for pageNumber := 0; pageNumber < 5; pageNumber++ {
		page, err := repository.ProjectAnalytics(context.Background(), filter)
		if err != nil {
			t.Fatalf("ProjectAnalytics(model edge page %d) error = %v", pageNumber, err)
		}
		if page.Record.SessionCount != 3 || len(page.Sessions) != 3 || len(page.Models) != 1 {
			t.Fatalf("ProjectAnalytics(model edge page %d) = %#v", pageNumber, page)
		}
		models = append(models, page.Models[0].DimensionKey)
		filter.ModelCursor = page.NextModelCursor
		if pageNumber == 2 && (page.NextModelCursor == nil || !page.NextModelCursor.Null ||
			page.NextModelCursor.TotalTokens != nil) {
			t.Fatalf("model edge page %d NULL cursor = %#v", pageNumber, page.NextModelCursor)
		}
		if page.NextModelCursor == nil {
			if pageNumber != 3 {
				t.Fatalf("model edge page %d cursor shape = %#v", pageNumber,
					page.NextModelCursor)
			}
			break
		}
	}
	if !reflect.DeepEqual(models, wantModels) {
		t.Fatalf("edge Model keyset traversal = %#v, want %#v", models, wantModels)
	}
	for _, values := range [][]string{sessions, models} {
		seen := make(map[string]struct{}, len(values))
		for _, value := range values {
			if _, found := seen[value]; found {
				t.Fatalf("edge keyset duplicated %q in %#v", value, values)
			}
			seen[value] = struct{}{}
		}
	}
}

func TestProjectAnalyticsContributionCursorsSurviveStoreRestart(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()
	if err := os.Chmod(directory, 0o700); err != nil {
		t.Fatalf("Chmod(temp dir) error = %v", err)
	}
	databasePath := filepath.Join(directory, "project-contribution-restart.db")
	open := func() (*storesqlite.Store, *Repository) {
		database, err := storesqlite.Open(context.Background(), storesqlite.Config{Path: databasePath})
		if err != nil {
			t.Fatalf("sqlite.Open() error = %v", err)
		}
		repository := NewRepository(database)
		if err := repository.EnsureApplicationSchema(context.Background()); err != nil {
			t.Fatalf("EnsureApplicationSchema() error = %v", err)
		}
		return database, repository
	}

	database, repository := open()
	seedProjectAnalyticsFixture(t, repository, false)
	rangeFilter := AnalyticsRange{
		ReportingTimezone: "UTC", StartAtMS: 0, EndAtMS: 2 * 86_400_000,
	}
	first, err := repository.ProjectAnalytics(context.Background(), ProjectAnalyticsDetailFilter{
		Range: rangeFilter, DimensionKey: "project-a", SessionLimit: 1, ModelLimit: 1,
	})
	if err != nil || first.NextSessionCursor == nil || first.NextModelCursor == nil {
		t.Fatalf("ProjectAnalytics(first) = %#v, %v", first, err)
	}
	if first.NextSessionCursor.GenerationID != "project-analytics-v1" ||
		first.NextModelCursor.GenerationID != "project-analytics-v1" {
		t.Fatalf("ProjectAnalytics(first cursor generation) = %#v / %#v",
			first.NextSessionCursor, first.NextModelCursor)
	}
	if err := database.Close(context.Background()); err != nil {
		t.Fatalf("Close(before restart) error = %v", err)
	}

	reopened, reopenedRepository := open()
	t.Cleanup(func() {
		if err := reopened.Close(context.Background()); err != nil {
			t.Errorf("Close(reopened) error = %v", err)
		}
	})
	second, err := reopenedRepository.ProjectAnalytics(
		context.Background(), ProjectAnalyticsDetailFilter{
			Range: rangeFilter, DimensionKey: "project-a", SessionLimit: 1,
			SessionCursor: first.NextSessionCursor, ModelLimit: 1, ModelCursor: first.NextModelCursor,
		},
	)
	if err != nil || len(second.Sessions) != 1 || len(second.Models) != 1 ||
		second.Sessions[0].SessionID != "project-session-alpha" ||
		second.Models[0].DimensionKey != "model-a" {
		t.Fatalf("ProjectAnalytics(after restart) = %#v, %v", second, err)
	}
}

func TestProjectAnalyticsRejectsContributionCursorAfterGenerationRollover(t *testing.T) {
	t.Parallel()

	repository := openRuntimeRepository(t)
	seedProjectAnalyticsFixture(t, repository, false)
	rangeFilter := AnalyticsRange{
		ReportingTimezone: "UTC", StartAtMS: 0, EndAtMS: 2 * 86_400_000,
	}
	first, err := repository.ProjectAnalytics(context.Background(), ProjectAnalyticsDetailFilter{
		Range: rangeFilter, DimensionKey: "project-a", SessionLimit: 1, ModelLimit: 1,
	})
	if err != nil || first.NextSessionCursor == nil || first.NextModelCursor == nil {
		t.Fatalf("ProjectAnalytics(first) = %#v, %v", first, err)
	}
	if err := repository.database.Write(context.Background(), func(
		ctx context.Context,
		transaction storesqlite.WriteTx,
	) error {
		database := transaction.WithContext(ctx)
		if err := database.Model(&costRollupGenerationModel{}).
			Where("generation_id = ?", "project-analytics-v1").
			Update("state", string(CostRollupGenerationSuperseded)).Error; err != nil {
			return err
		}
		return database.Create(&costRollupGenerationModel{
			GenerationID: "project-analytics-v2", ReportingTimezone: "UTC",
			PricingSource: "test", Currency: "USD", RollupVersion: 1,
			State: string(CostRollupGenerationActive), CreatedAtMS: 300_000_000,
			CompletedAtMS: pointerTo(int64(300_000_000)), UpdatedAtMS: 300_000_000,
		}).Error
	}); err != nil {
		t.Fatalf("roll active generation: %v", err)
	}
	for name, filter := range map[string]ProjectAnalyticsDetailFilter{
		"session": {
			Range: rangeFilter, DimensionKey: "project-a", SessionLimit: 1,
			SessionCursor: first.NextSessionCursor, ModelLimit: 1,
		},
		"model": {
			Range: rangeFilter, DimensionKey: "project-a", SessionLimit: 1,
			ModelLimit: 1, ModelCursor: first.NextModelCursor,
		},
	} {
		if _, err := repository.ProjectAnalytics(context.Background(), filter); !errors.Is(err, ErrInvalidRecord) {
			t.Fatalf("ProjectAnalytics(%s cursor after rollover) error = %v, want ErrInvalidRecord", name, err)
		}
	}
}

func TestListProjectAnalyticsRejectsProjectContributionDrift(t *testing.T) {
	t.Parallel()

	repository := openRuntimeRepository(t)
	seedProjectAnalyticsFixture(t, repository, false)
	if err := repository.database.Write(context.Background(), func(
		ctx context.Context,
		transaction storesqlite.WriteTx,
	) error {
		return transaction.WithContext(ctx).Model(&turnAttributionModel{}).
			Where("turn_id = ?", "project-turn-alpha").
			Updates(map[string]any{
				"project_id": "project-b", "project_display_name": "Project B",
			}).Error
	}); err != nil {
		t.Fatalf("drift project contribution fixture: %v", err)
	}
	_, err := repository.ListProjectAnalytics(context.Background(), ProjectAnalyticsFilter{
		Range: AnalyticsRange{
			ReportingTimezone: "UTC", StartAtMS: 0, EndAtMS: 2 * 86_400_000,
		},
		Limit: 10, SortField: ProjectAnalyticsSortTotalTokens,
		SortDirection: AnalyticsSortDescending,
	})
	if !errors.Is(err, ErrInvalidRecord) {
		t.Fatalf("ListProjectAnalytics(contribution drift) error = %v, want ErrInvalidRecord", err)
	}
}

func TestProjectAnalyticsRejectsMissingTurnAttributionForUnknownContribution(t *testing.T) {
	t.Parallel()

	repository := openRuntimeRepository(t)
	seedProjectAnalyticsFixture(t, repository, false)
	if err := repository.database.Write(context.Background(), func(
		ctx context.Context,
		transaction storesqlite.WriteTx,
	) error {
		return transaction.WithContext(ctx).Where(
			"turn_id = ?", "project-turn-gamma",
		).Delete(&turnAttributionModel{}).Error
	}); err != nil {
		t.Fatalf("delete unknown turn attribution fixture: %v", err)
	}
	_, err := repository.ProjectAnalytics(context.Background(), ProjectAnalyticsDetailFilter{
		Range: AnalyticsRange{
			ReportingTimezone: "UTC", StartAtMS: 0, EndAtMS: 2 * 86_400_000,
		},
		DimensionKey: "unknown|unknown|missing|missing",
		SessionLimit: 10, ModelLimit: 10,
	})
	if !errors.Is(err, ErrInvalidRecord) {
		t.Fatalf("ProjectAnalytics(missing turn attribution) error = %v, want ErrInvalidRecord", err)
	}
}

func TestProjectAnalyticsRequiresActiveGenerationAndValidRange(t *testing.T) {
	t.Parallel()

	repository := openRuntimeRepository(t)
	filter := ProjectAnalyticsFilter{
		Range: AnalyticsRange{ReportingTimezone: "UTC", StartAtMS: 0, EndAtMS: 86_400_000},
		Limit: 10, SortField: ProjectAnalyticsSortTotalTokens,
		SortDirection: AnalyticsSortDescending,
	}
	if _, err := repository.ListProjectAnalytics(context.Background(), filter); !errors.Is(err, ErrAnalyticsUnavailable) {
		t.Fatalf("ListProjectAnalytics(no generation) error = %v, want unavailable", err)
	}
	if _, err := repository.ProjectAnalytics(context.Background(), ProjectAnalyticsDetailFilter{
		Range: filter.Range, DimensionKey: "project-a",
	}); !errors.Is(err, ErrAnalyticsUnavailable) {
		t.Fatalf("ProjectAnalytics(no generation) error = %v, want unavailable", err)
	}
	seedProjectAnalyticsFixture(t, repository, false)
	for _, invalid := range []ProjectAnalyticsFilter{
		{Range: AnalyticsRange{ReportingTimezone: "", StartAtMS: 0, EndAtMS: 1}, Limit: 1,
			SortField: ProjectAnalyticsSortTotalTokens, SortDirection: AnalyticsSortDescending},
		{Range: filter.Range, Limit: 0,
			SortField: ProjectAnalyticsSortTotalTokens, SortDirection: AnalyticsSortDescending},
		{Range: filter.Range, Limit: 101,
			SortField: ProjectAnalyticsSortTotalTokens, SortDirection: AnalyticsSortDescending},
		{Range: filter.Range, Limit: 1,
			SortField: "private", SortDirection: AnalyticsSortDescending},
		{Range: filter.Range, Limit: 1,
			SortField: ProjectAnalyticsSortTotalTokens, SortDirection: "sideways"},
		{Range: filter.Range, Limit: 1,
			SortField: ProjectAnalyticsSortTotalTokens, SortDirection: AnalyticsSortDescending,
			Cursor: &ProjectAnalyticsCursor{DimensionKey: "project-a", Null: true, NumericValue: pointerTo(int64(1))}},
	} {
		if _, err := repository.ListProjectAnalytics(context.Background(), invalid); !errors.Is(err, ErrInvalidRecord) {
			t.Fatalf("ListProjectAnalytics(%#v) error = %v, want ErrInvalidRecord", invalid, err)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := repository.ListProjectAnalytics(ctx, filter); !errors.Is(err, context.Canceled) {
		t.Fatalf("ListProjectAnalytics(cancelled) error = %v", err)
	}
	one := int64(1)
	for _, invalid := range []ProjectAnalyticsDetailFilter{
		{Range: filter.Range, DimensionKey: "project-a", SessionLimit: 51, ModelLimit: 1},
		{Range: filter.Range, DimensionKey: "project-a", SessionLimit: 1, ModelLimit: 51},
		{Range: filter.Range, DimensionKey: "project-a", SessionLimit: 1, ModelLimit: 1,
			SessionCursor: &ProjectSessionAnalyticsCursor{
				DimensionKey: "project-b", SessionID: "session", LastActivityAtMS: 1,
			}},
		{Range: filter.Range, DimensionKey: "project-a", SessionLimit: 1, ModelLimit: 1,
			ModelCursor: &ProjectModelAnalyticsCursor{
				DimensionKey: "project-a", ModelDimensionKey: "model", Null: true, TotalTokens: &one,
			}},
	} {
		if _, err := repository.ProjectAnalytics(context.Background(), invalid); !errors.Is(err, ErrInvalidRecord) {
			t.Fatalf("ProjectAnalytics(%#v) error = %v, want ErrInvalidRecord", invalid, err)
		}
	}
	if _, err := repository.ProjectAnalytics(ctx, ProjectAnalyticsDetailFilter{
		Range: filter.Range, DimensionKey: "project-a",
	}); !errors.Is(err, context.Canceled) {
		t.Fatalf("ProjectAnalytics(cancelled) error = %v", err)
	}
}

func seedProjectAnalyticsFixture(t *testing.T, repository *Repository, driftGlobal bool) {
	t.Helper()
	zero := int64(0)
	rollup := func(tokens int64, cost *int64, priced, unpriced int64, atMS int64) rollupTotalsModel {
		return rollupTotalsModel{
			TurnCount: priced + unpriced, InputTokens: pointerTo(tokens),
			CachedInputTokens: &zero, OutputTokens: &zero, ReasoningTokens: &zero,
			TotalTokens: pointerTo(tokens), EstimatedUSDMicros: cost,
			PricedTurnCount: priced, UnpricedTurnCount: unpriced,
			FirstActivityAtMS: atMS, LastActivityAtMS: atMS, UpdatedAtMS: 200_000_000,
		}
	}
	cost100, cost200, free := int64(100), int64(200), int64(0)
	generation := costRollupGenerationModel{
		GenerationID: "project-analytics-v1", ReportingTimezone: "UTC",
		PricingSource: "test", Currency: "USD", RollupVersion: 1,
		State: string(CostRollupGenerationActive), CreatedAtMS: 200_000_000,
		CompletedAtMS: pointerTo(int64(200_000_000)), UpdatedAtMS: 200_000_000,
	}
	projects := []projectUsageDailyModel{
		{GenerationID: generation.GenerationID, BucketStartMS: 0, ReportingTimezone: "UTC",
			DimensionKey: "project-a", ProjectID: pointerTo("project-a"), ProjectDisplayName: pointerTo("Project A"),
			AttributionConfidence: "high", AttributionSource: "registered_root", AttributionReason: "root_matched",
			Totals: rollup(10, &cost100, 1, 0, 10)},
		{GenerationID: generation.GenerationID, BucketStartMS: 86_400_000, ReportingTimezone: "UTC",
			DimensionKey: "project-a", ProjectID: pointerTo("project-a"), ProjectDisplayName: pointerTo("Project A"),
			AttributionConfidence: "medium", AttributionSource: "cwd_path_digest", AttributionReason: "path_derived",
			Totals: rollup(20, &cost200, 1, 0, 86_400_010)},
		{GenerationID: generation.GenerationID, BucketStartMS: 0, ReportingTimezone: "UTC",
			DimensionKey: "project-b", ProjectID: pointerTo("project-b"), ProjectDisplayName: pointerTo("Project B"),
			AttributionConfidence: "high", AttributionSource: "registered_root", AttributionReason: "root_matched",
			Totals: rollup(5, &free, 1, 0, 20)},
		{GenerationID: generation.GenerationID, BucketStartMS: 0, ReportingTimezone: "UTC",
			DimensionKey:          "unknown|unknown|missing|missing",
			AttributionConfidence: "unknown", AttributionSource: "missing", AttributionReason: "missing",
			Totals: rollup(7, nil, 0, 1, 30)},
	}
	dayOneTokens := int64(22)
	if driftGlobal {
		dayOneTokens++
	}
	dayOne := rollup(dayOneTokens, &cost100, 2, 1, 30)
	dayOne.FirstActivityAtMS = 10
	days := []usageDailyModel{
		{GenerationID: generation.GenerationID, BucketStartMS: 0, ReportingTimezone: "UTC",
			Totals: dayOne},
		{GenerationID: generation.GenerationID, BucketStartMS: 86_400_000, ReportingTimezone: "UTC",
			Totals: rollup(20, &cost200, 1, 0, 86_400_010)},
	}
	err := repository.database.Write(context.Background(), func(ctx context.Context, transaction storesqlite.WriteTx) error {
		database := transaction.WithContext(ctx)
		if err := database.Create(&generation).Error; err != nil {
			return err
		}
		if err := database.Create(&projects).Error; err != nil {
			return err
		}
		return database.Create(&days).Error
	})
	if err != nil {
		t.Fatalf("seed project analytics fixture: %v", err)
	}
	seedProjectContributionFacts(t, repository)
}

func seedProjectContributionFacts(t *testing.T, repository *Repository) {
	t.Helper()
	pricingVersion := pricing.BuiltinOpenAI20260714()
	if err := repository.AddPricingVersion(context.Background(), pricingVersion); err != nil {
		t.Fatalf("AddPricingVersion() error = %v", err)
	}
	zero := int64(0)
	type contributionFixture struct {
		sessionID, title, turnID  string
		observedAtMS              int64
		tokens                    *int64
		cost                      *int64
		projectID, projectDisplay *string
		projectConfidence         string
		projectSource             string
		projectReason             string
		modelKey, modelDisplay    *string
		modelConfidence           string
		modelSource               string
		modelReason               string
	}
	fixtures := []contributionFixture{
		{
			sessionID: "project-session-alpha", title: "Alpha safe title", turnID: "project-turn-alpha",
			observedAtMS: 10, tokens: pointerTo(int64(10)), cost: pointerTo(int64(100)),
			projectID: pointerTo("project-a"), projectDisplay: pointerTo("Project A"),
			projectConfidence: "high", projectSource: "registered_root", projectReason: "root_matched",
			modelKey: pointerTo("model-a"), modelDisplay: pointerTo("Model A"),
			modelConfidence: "high", modelSource: "model_canonical", modelReason: "observed",
		},
		{
			sessionID: "project-session-beta", title: "Beta safe title", turnID: "project-turn-beta",
			observedAtMS: 86_400_010, tokens: pointerTo(int64(20)), cost: pointerTo(int64(200)),
			projectID: pointerTo("project-a"), projectDisplay: pointerTo("Project A"),
			projectConfidence: "medium", projectSource: "cwd_path_digest", projectReason: "path_derived",
			modelKey: pointerTo("model-b"), modelDisplay: pointerTo("Model B"),
			modelConfidence: "high", modelSource: "model_canonical", modelReason: "observed",
		},
		{
			sessionID: "project-session-delta", title: "Delta safe title", turnID: "project-turn-delta",
			observedAtMS: 20, tokens: pointerTo(int64(5)), cost: pointerTo(int64(0)),
			projectID: pointerTo("project-b"), projectDisplay: pointerTo("Project B"),
			projectConfidence: "high", projectSource: "registered_root", projectReason: "root_matched",
			modelKey: pointerTo("model-a"), modelDisplay: pointerTo("Model A"),
			modelConfidence: "high", modelSource: "model_canonical", modelReason: "observed",
		},
		{
			sessionID: "project-session-gamma", title: "Gamma safe title", turnID: "project-turn-gamma",
			observedAtMS: 30, tokens: pointerTo(int64(7)), cost: nil,
			projectConfidence: "unknown", projectSource: "missing", projectReason: "missing",
			modelConfidence: "unknown", modelSource: "missing", modelReason: "missing",
		},
	}
	err := repository.database.Write(context.Background(), func(
		ctx context.Context,
		transaction storesqlite.WriteTx,
	) error {
		database := transaction.WithContext(ctx)
		for _, value := range fixtures {
			completedAt := value.observedAtMS
			if err := database.Create(&sessionModel{
				SessionID: value.sessionID, Provider: "codex", SourceKind: "session",
				CreatedAtMS: 1, FirstSeenAtMS: 1, LastSeenAtMS: value.observedAtMS,
			}).Error; err != nil {
				return err
			}
			if err := database.Create(&sessionCurrentModel{
				SessionID: value.sessionID, LastActivityAtMS: pointerTo(value.observedAtMS),
				UpdatedAtMS: value.observedAtMS,
			}).Error; err != nil {
				return err
			}
			if err := database.Create(&sessionAttributionModel{
				SessionID: value.sessionID, DisplayTitle: value.title,
				TitleConfidence: "high", TitleSource: "session_id_fallback", TitleReason: "stable_identity",
				ProjectID: value.projectID, ProjectDisplay: value.projectDisplay,
				ProjectConfidence: value.projectConfidence, ProjectSource: value.projectSource,
				ProjectReason: value.projectReason, ModelKey: value.modelKey, ModelDisplay: value.modelDisplay,
				ModelConfidence: value.modelConfidence, ModelSource: value.modelSource,
				ModelReason: value.modelReason, RuleVersion: 1, UpdatedAtMS: value.observedAtMS,
			}).Error; err != nil {
				return err
			}
			if err := database.Create(&turnModel{
				TurnID: value.turnID, SessionID: value.sessionID,
				StartedAtMS: value.observedAtMS, CompletedAtMS: &completedAt, Outcome: pointerTo("completed"),
				SourceGeneration: 1, StartOffset: 1, CompleteOffset: pointerTo(int64(2)),
			}).Error; err != nil {
				return err
			}
			if err := database.Create(&turnUsageModel{
				TurnID: value.turnID, ObservedAtMS: value.observedAtMS, IsFinal: true,
				InputTokens: value.tokens, CachedInputTokens: &zero, OutputTokens: &zero,
				ReasoningTokens: &zero, SourceGeneration: 1, SourceOffset: 2,
				Confidence: "high", UpdatedAtMS: value.observedAtMS,
			}).Error; err != nil {
				return err
			}
			if err := database.Create(&turnAttributionModel{
				TurnID: value.turnID, ProjectID: value.projectID, ProjectDisplay: value.projectDisplay,
				ProjectConfidence: value.projectConfidence, ProjectSource: value.projectSource,
				ProjectReason: value.projectReason, ModelKey: value.modelKey, ModelDisplay: value.modelDisplay,
				ModelConfidence: value.modelConfidence, ModelSource: value.modelSource,
				ModelReason: value.modelReason, RuleVersion: 1, UpdatedAtMS: value.observedAtMS,
			}).Error; err != nil {
				return err
			}
			cost := turnCostModel{
				GenerationID: "project-analytics-v1", TurnID: value.turnID,
				CalculatedAtMS: 200_000_000,
			}
			if value.cost == nil {
				cost.PricingStatus = string(pricing.CostStatusUnpriced)
				cost.PricingReason = string(pricing.CostReasonModelNotListed)
			} else {
				cost.PricingVersion = pointerTo(pricingVersion.PricingVersion)
				cost.EstimatedUSDMicros = value.cost
				cost.PricingStatus = string(pricing.CostStatusPriced)
				cost.PricingReason = string(pricing.CostReasonPriced)
			}
			if err := database.Create(&cost).Error; err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("seed project contribution facts: %v", err)
	}
}

func seedProjectContributionPaginationEdges(t *testing.T, repository *Repository) {
	t.Helper()

	zero, atMS, updatedAtMS := int64(0), int64(86_400_010), int64(200_000_000)
	projectID, projectDisplay := "project-a", "Project A"
	pricingVersion := pricing.BuiltinOpenAI20260714().PricingVersion
	err := repository.database.Write(context.Background(), func(
		ctx context.Context,
		transaction storesqlite.WriteTx,
	) error {
		database := transaction.WithContext(ctx)
		if err := database.Model(&turnUsageModel{}).Where("turn_id = ?", "project-turn-alpha").
			Updates(map[string]any{
				"observed_at_ms": atMS, "input_tokens": int64(20), "updated_at_ms": atMS,
			}).Error; err != nil {
			return err
		}
		if err := database.Model(&turnModel{}).Where("turn_id = ?", "project-turn-alpha").
			Updates(map[string]any{
				"started_at_ms": atMS, "completed_at_ms": atMS,
			}).Error; err != nil {
			return err
		}
		if err := database.Model(&sessionCurrentModel{}).
			Where("session_id = ?", "project-session-alpha").
			Updates(map[string]any{
				"last_activity_at_ms": atMS, "updated_at_ms": atMS,
			}).Error; err != nil {
			return err
		}
		if err := database.Model(&sessionModel{}).
			Where("session_id = ?", "project-session-alpha").
			Update("last_seen_at_ms", atMS).Error; err != nil {
			return err
		}

		if err := database.Create(&sessionModel{
			SessionID: "project-session-omega", Provider: "codex", SourceKind: "session",
			CreatedAtMS: 1, FirstSeenAtMS: 1, LastSeenAtMS: atMS,
		}).Error; err != nil {
			return err
		}
		if err := database.Create(&sessionCurrentModel{
			SessionID: "project-session-omega", LastActivityAtMS: &atMS, UpdatedAtMS: atMS,
		}).Error; err != nil {
			return err
		}
		if err := database.Create(&sessionAttributionModel{
			SessionID: "project-session-omega", DisplayTitle: "Omega safe title",
			TitleConfidence: "high", TitleSource: "session_id_fallback",
			TitleReason: "stable_identity", ProjectID: &projectID,
			ProjectDisplay: &projectDisplay, ProjectConfidence: "high",
			ProjectSource: "registered_root", ProjectReason: "root_matched",
			ModelConfidence: "unknown", ModelSource: "missing", ModelReason: "missing",
			RuleVersion: 1, UpdatedAtMS: atMS,
		}).Error; err != nil {
			return err
		}
		if err := database.Create(&turnModel{
			TurnID: "project-turn-omega", SessionID: "project-session-omega",
			StartedAtMS: atMS, CompletedAtMS: &atMS, Outcome: pointerTo("completed"),
			SourceGeneration: 1, StartOffset: 1, CompleteOffset: pointerTo(int64(2)),
		}).Error; err != nil {
			return err
		}
		if err := database.Create(&turnUsageModel{
			TurnID: "project-turn-omega", ObservedAtMS: atMS, IsFinal: true,
			CachedInputTokens: &zero, OutputTokens: &zero, ReasoningTokens: &zero,
			SourceGeneration: 1, SourceOffset: 2, Confidence: "high", UpdatedAtMS: atMS,
		}).Error; err != nil {
			return err
		}
		if err := database.Create(&turnAttributionModel{
			TurnID: "project-turn-omega", ProjectID: &projectID, ProjectDisplay: &projectDisplay,
			ProjectConfidence: "high", ProjectSource: "registered_root", ProjectReason: "root_matched",
			ModelConfidence: "unknown", ModelSource: "missing", ModelReason: "missing",
			RuleVersion: 1, UpdatedAtMS: atMS,
		}).Error; err != nil {
			return err
		}
		if err := database.Create(&turnCostModel{
			GenerationID: "project-analytics-v1", TurnID: "project-turn-omega",
			PricingStatus: string(pricing.CostStatusUnpriced),
			PricingReason: string(pricing.CostReasonModelNotListed), CalculatedAtMS: updatedAtMS,
		}).Error; err != nil {
			return err
		}
		if err := database.Create(&turnModel{
			TurnID: "project-turn-omega-invalid", SessionID: "project-session-omega",
			StartedAtMS: atMS, CompletedAtMS: &atMS, Outcome: pointerTo("completed"),
			SourceGeneration: 1, StartOffset: 3, CompleteOffset: pointerTo(int64(4)),
		}).Error; err != nil {
			return err
		}
		if err := database.Create(&turnUsageModel{
			TurnID: "project-turn-omega-invalid", ObservedAtMS: atMS, IsFinal: true,
			CachedInputTokens: &zero, OutputTokens: &zero, ReasoningTokens: &zero,
			SourceGeneration: 1, SourceOffset: 4, Confidence: "high", UpdatedAtMS: atMS,
		}).Error; err != nil {
			return err
		}
		if err := database.Create(&turnAttributionModel{
			TurnID: "project-turn-omega-invalid", ProjectID: &projectID,
			ProjectDisplay: &projectDisplay, ProjectConfidence: "high",
			ProjectSource: "registered_root", ProjectReason: "root_matched",
			ModelConfidence: "low", ModelSource: "invalid_model", ModelReason: "invalid",
			RuleVersion: 1, UpdatedAtMS: atMS,
		}).Error; err != nil {
			return err
		}
		if err := database.Create(&turnCostModel{
			GenerationID: "project-analytics-v1", TurnID: "project-turn-omega-invalid",
			PricingStatus: string(pricing.CostStatusUnpriced),
			PricingReason: string(pricing.CostReasonInvalidModel), CalculatedAtMS: updatedAtMS,
		}).Error; err != nil {
			return err
		}

		if err := database.Where(
			"generation_id = ? AND bucket_start_ms = ? AND dimension_key = ?",
			"project-analytics-v1", int64(0), "project-a",
		).Delete(&projectUsageDailyModel{}).Error; err != nil {
			return err
		}
		if err := database.Model(&projectUsageDailyModel{}).Where(
			"generation_id = ? AND bucket_start_ms = ? AND dimension_key = ?",
			"project-analytics-v1", int64(86_400_000), "project-a",
		).Updates(map[string]any{
			"turn_count": 4, "input_tokens": nil, "cached_input_tokens": zero,
			"output_tokens": zero, "reasoning_tokens": zero, "total_tokens": nil,
			"estimated_usd_micros": int64(300), "priced_turn_count": 2,
			"unpriced_turn_count": 2, "first_activity_at_ms": atMS,
			"last_activity_at_ms": atMS, "updated_at_ms": updatedAtMS,
		}).Error; err != nil {
			return err
		}
		if err := database.Model(&usageDailyModel{}).Where(
			"generation_id = ? AND bucket_start_ms = ?", "project-analytics-v1", int64(0),
		).Updates(map[string]any{
			"turn_count": 2, "input_tokens": int64(12), "cached_input_tokens": zero,
			"output_tokens": zero, "reasoning_tokens": zero, "total_tokens": int64(12),
			"estimated_usd_micros": zero, "priced_turn_count": 1, "unpriced_turn_count": 1,
			"first_activity_at_ms": int64(20), "last_activity_at_ms": int64(30),
			"updated_at_ms": updatedAtMS,
		}).Error; err != nil {
			return err
		}
		return database.Model(&usageDailyModel{}).Where(
			"generation_id = ? AND bucket_start_ms = ?",
			"project-analytics-v1", int64(86_400_000),
		).Updates(map[string]any{
			"turn_count": 4, "input_tokens": nil, "cached_input_tokens": zero,
			"output_tokens": zero, "reasoning_tokens": zero, "total_tokens": nil,
			"estimated_usd_micros": int64(300), "priced_turn_count": 2,
			"unpriced_turn_count": 2, "first_activity_at_ms": atMS,
			"last_activity_at_ms": atMS, "updated_at_ms": updatedAtMS,
		}).Error
	})
	if err != nil {
		t.Fatalf("seed project contribution pagination edges (pricing=%s): %v", pricingVersion, err)
	}
}

func seedSessionAnalyticsFixture(t *testing.T, repository *Repository, withLedger bool) {
	t.Helper()
	type fixture struct {
		id, title                 string
		projectID, projectDisplay *string
		modelKey, modelDisplay    *string
		lastActivity              *int64
		active                    bool
		rollup                    *rollupTotalsModel
	}
	fixtures := []fixture{
		{id: "session-alpha", title: "Alpha safe title", projectID: pointerTo("project-a"),
			projectDisplay: pointerTo("Project A"), modelKey: pointerTo("model-a"),
			modelDisplay: pointerTo("Model A"), lastActivity: pointerTo(int64(300)), active: true,
			rollup: &rollupTotalsModel{TurnCount: 1, InputTokens: pointerTo(int64(30)),
				CachedInputTokens: pointerTo(int64(0)), OutputTokens: pointerTo(int64(0)),
				ReasoningTokens: pointerTo(int64(0)), TotalTokens: pointerTo(int64(30)),
				EstimatedUSDMicros: pointerTo(int64(100)), PricedTurnCount: 1,
				FirstActivityAtMS: 300, LastActivityAtMS: 300, UpdatedAtMS: 400}},
		{id: "session-beta", title: "Beta safe title", projectID: pointerTo("project-a"),
			projectDisplay: pointerTo("Project A"), modelKey: pointerTo("model-b"),
			modelDisplay: pointerTo("Model B"), lastActivity: pointerTo(int64(200)),
			rollup: &rollupTotalsModel{TurnCount: 1, InputTokens: nil,
				CachedInputTokens: pointerTo(int64(0)), OutputTokens: pointerTo(int64(0)),
				ReasoningTokens: pointerTo(int64(0)), TotalTokens: nil,
				UnpricedTurnCount: 1, FirstActivityAtMS: 200, LastActivityAtMS: 200, UpdatedAtMS: 400}},
		{id: "session-gamma", title: "Gamma safe title"},
		{id: "session-delta", title: "Delta safe title", projectID: pointerTo("project-b"),
			projectDisplay: pointerTo("Project B"), modelKey: pointerTo("model-a"),
			modelDisplay: pointerTo("Model A"), lastActivity: pointerTo(int64(100)),
			rollup: &rollupTotalsModel{TurnCount: 1, InputTokens: pointerTo(int64(10)),
				CachedInputTokens: pointerTo(int64(0)), OutputTokens: pointerTo(int64(0)),
				ReasoningTokens: pointerTo(int64(0)), TotalTokens: pointerTo(int64(10)),
				EstimatedUSDMicros: pointerTo(int64(0)), PricedTurnCount: 1,
				FirstActivityAtMS: 100, LastActivityAtMS: 100, UpdatedAtMS: 400}},
	}
	err := repository.database.Write(context.Background(), func(ctx context.Context, transaction storesqlite.WriteTx) error {
		database := transaction.WithContext(ctx)
		if withLedger {
			generation := costRollupGenerationModel{
				GenerationID: "session-analytics-v1", ReportingTimezone: "UTC",
				PricingSource: "test", Currency: "USD", RollupVersion: 1,
				State: string(CostRollupGenerationActive), CreatedAtMS: 400,
				CompletedAtMS: pointerTo(int64(400)), UpdatedAtMS: 400,
			}
			if err := database.Create(&generation).Error; err != nil {
				return err
			}
		}
		for _, value := range fixtures {
			session := sessionModel{
				SessionID: value.id, Provider: "codex", SourceKind: "session",
				CreatedAtMS: 1, FirstSeenAtMS: 1, LastSeenAtMS: 400,
			}
			if err := database.Create(&session).Error; err != nil {
				return err
			}
			if value.active {
				turn := turnModel{
					TurnID: "turn-" + value.id, SessionID: value.id, StartedAtMS: 300,
					SourceGeneration: 0, StartOffset: 1,
				}
				if err := database.Create(&turn).Error; err != nil {
					return err
				}
			}
			current := sessionCurrentModel{
				SessionID: value.id, LastActivityAtMS: value.lastActivity, UpdatedAtMS: 400,
			}
			if err := database.Create(&current).Error; err != nil {
				return err
			}
			attribution := sessionAttributionModel{
				SessionID: value.id, DisplayTitle: value.title,
				TitleConfidence: string(AttributionConfidenceHigh),
				TitleSource:     string(AttributionSourceSessionIDFallback),
				TitleReason:     string(AttributionReasonStableIdentity),
				ProjectID:       value.projectID, ProjectDisplay: value.projectDisplay,
				ProjectConfidence: string(AttributionConfidenceHigh),
				ProjectSource:     string(AttributionSourceRegisteredRoot),
				ProjectReason:     string(AttributionReasonRootMatched),
				ModelKey:          value.modelKey, ModelDisplay: value.modelDisplay,
				ModelConfidence: string(AttributionConfidenceHigh),
				ModelSource:     string(AttributionSourceModelCanonical),
				ModelReason:     string(AttributionReasonObserved), RuleVersion: 1, UpdatedAtMS: 400,
			}
			if value.projectID == nil {
				attribution.ProjectConfidence = string(AttributionConfidenceUnknown)
				attribution.ProjectSource = string(AttributionSourceMissing)
				attribution.ProjectReason = string(AttributionReasonMissing)
			}
			if value.modelKey == nil {
				attribution.ModelConfidence = string(AttributionConfidenceUnknown)
				attribution.ModelSource = string(AttributionSourceMissing)
				attribution.ModelReason = string(AttributionReasonMissing)
			}
			if err := database.Create(&attribution).Error; err != nil {
				return err
			}
			if value.active {
				turnAttribution := turnAttributionModel{
					TurnID:    "turn-" + value.id,
					ProjectID: value.projectID, ProjectDisplay: value.projectDisplay,
					ProjectConfidence: attribution.ProjectConfidence,
					ProjectSource:     attribution.ProjectSource, ProjectReason: attribution.ProjectReason,
					ModelKey: value.modelKey, ModelDisplay: value.modelDisplay,
					ModelConfidence: attribution.ModelConfidence,
					ModelSource:     attribution.ModelSource, ModelReason: attribution.ModelReason,
					RuleVersion: 1, UpdatedAtMS: 400,
				}
				if err := database.Create(&turnAttribution).Error; err != nil {
					return err
				}
			}
			if withLedger && value.rollup != nil {
				rollup := sessionUsageRollupModel{
					GenerationID: "session-analytics-v1", SessionID: value.id, Totals: *value.rollup,
				}
				if err := database.Create(&rollup).Error; err != nil {
					return err
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("seed session analytics fixture: %v", err)
	}
}

func seedSessionTurnTimelineFixture(t *testing.T, repository *Repository, withLedger bool) {
	t.Helper()
	pricingVersion := pricing.BuiltinOpenAI20260714()
	if withLedger {
		if err := repository.AddPricingVersion(context.Background(), pricingVersion); err != nil {
			t.Fatalf("AddPricingVersion() error = %v", err)
		}
	}
	zero, input, cost := int64(0), int64(30), int64(100)
	completedAt := int64(120)
	err := repository.database.Write(context.Background(), func(
		ctx context.Context,
		transaction storesqlite.WriteTx,
	) error {
		database := transaction.WithContext(ctx)
		if err := database.Create(&sessionModel{
			SessionID: "session-timeline", Provider: "codex", SourceKind: "session",
			CreatedAtMS: 1, FirstSeenAtMS: 1, LastSeenAtMS: 220,
		}).Error; err != nil {
			return err
		}
		if err := database.Create(&sessionCurrentModel{
			SessionID: "session-timeline", LastActivityAtMS: pointerTo(int64(220)), UpdatedAtMS: 220,
		}).Error; err != nil {
			return err
		}
		sessionAttribution := sessionAttributionModel{
			SessionID: "session-timeline", DisplayTitle: "Timeline safe title",
			TitleConfidence:   string(AttributionConfidenceHigh),
			TitleSource:       string(AttributionSourceSessionIDFallback),
			TitleReason:       string(AttributionReasonStableIdentity),
			ProjectConfidence: string(AttributionConfidenceUnknown),
			ProjectSource:     string(AttributionSourceMissing),
			ProjectReason:     string(AttributionReasonMissing),
			ModelKey:          pointerTo("model-safe"), ModelDisplay: pointerTo("Model Safe"),
			ModelConfidence: string(AttributionConfidenceHigh),
			ModelSource:     string(AttributionSourceModelCanonical),
			ModelReason:     string(AttributionReasonObserved), RuleVersion: 1, UpdatedAtMS: 220,
		}
		if err := database.Create(&sessionAttribution).Error; err != nil {
			return err
		}
		turns := []turnModel{
			{
				TurnID: "turn-timeline-complete", SessionID: "session-timeline", StartedAtMS: 100,
				CompletedAtMS: &completedAt, Outcome: pointerTo("completed"),
				Model: pointerTo("raw-private-model"), SourceGeneration: 0,
				StartOffset: 10, CompleteOffset: pointerTo(int64(20)),
			},
			{
				TurnID: "turn-timeline-active", SessionID: "session-timeline", StartedAtMS: 200,
				Model: pointerTo("raw-private-model"), SourceGeneration: 0, StartOffset: 30,
			},
		}
		if err := database.Create(&turns).Error; err != nil {
			return err
		}
		usages := []turnUsageModel{
			{
				TurnID: "turn-timeline-complete", ObservedAtMS: 120, IsFinal: true,
				InputTokens: &input, CachedInputTokens: &zero, OutputTokens: &zero,
				ReasoningTokens: &zero, SourceGeneration: 0, SourceOffset: 20,
				Confidence: "exact", UpdatedAtMS: 120,
			},
			{
				TurnID: "turn-timeline-active", ObservedAtMS: 220, IsFinal: false,
				InputTokens: &zero, CachedInputTokens: &zero, OutputTokens: &zero,
				ReasoningTokens: &zero, SourceGeneration: 0, SourceOffset: 40,
				Confidence: "exact", UpdatedAtMS: 220,
			},
		}
		if err := database.Create(&usages).Error; err != nil {
			return err
		}
		attributions := []turnAttributionModel{
			{TurnID: "turn-timeline-complete"},
			{TurnID: "turn-timeline-active"},
		}
		for index := range attributions {
			attributions[index].ProjectConfidence = string(AttributionConfidenceUnknown)
			attributions[index].ProjectSource = string(AttributionSourceMissing)
			attributions[index].ProjectReason = string(AttributionReasonMissing)
			attributions[index].ModelKey = pointerTo("model-safe")
			attributions[index].ModelDisplay = pointerTo("Model Safe")
			attributions[index].ModelConfidence = string(AttributionConfidenceHigh)
			attributions[index].ModelSource = string(AttributionSourceModelCanonical)
			attributions[index].ModelReason = string(AttributionReasonObserved)
			attributions[index].RuleVersion = 1
			attributions[index].UpdatedAtMS = 220
		}
		if err := database.Create(&attributions).Error; err != nil {
			return err
		}
		if !withLedger {
			return nil
		}
		generation := costRollupGenerationModel{
			GenerationID: "session-turn-timeline-v1", ReportingTimezone: "UTC",
			PricingSource: "test", Currency: "USD", RollupVersion: 1,
			State: string(CostRollupGenerationActive), CreatedAtMS: 300,
			CompletedAtMS: pointerTo(int64(300)), UpdatedAtMS: 300,
		}
		if err := database.Create(&generation).Error; err != nil {
			return err
		}
		rollup := sessionUsageRollupModel{
			GenerationID: generation.GenerationID, SessionID: "session-timeline",
			Totals: rollupTotalsModel{
				TurnCount: 1, InputTokens: &input, CachedInputTokens: &zero,
				OutputTokens: &zero, ReasoningTokens: &zero, TotalTokens: &input,
				EstimatedUSDMicros: &cost, PricedTurnCount: 1,
				FirstActivityAtMS: 120, LastActivityAtMS: 120, UpdatedAtMS: 300,
			},
		}
		if err := database.Create(&rollup).Error; err != nil {
			return err
		}
		return database.Create(&turnCostModel{
			GenerationID: generation.GenerationID, TurnID: "turn-timeline-complete",
			PricingVersion: &pricingVersion.PricingVersion, EstimatedUSDMicros: &cost,
			PricingStatus: string(pricing.CostStatusPriced),
			PricingReason: string(pricing.CostReasonPriced), CalculatedAtMS: 300,
		}).Error
	})
	if err != nil {
		t.Fatalf("seed session turn timeline fixture: %v", err)
	}
}
