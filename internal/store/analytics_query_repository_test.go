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
		SessionID: "session-alpha", ReportingTimezone: pointerTo("UTC"),
	})
	if err != nil {
		t.Fatalf("SessionAnalytics() error = %v", err)
	}
	if !reflect.DeepEqual(detail.Record, page.Records[0]) || detail.Generation == nil ||
		detail.PricingVersions == nil || detail.UnpricedReasons == nil {
		t.Fatalf("detail/list mismatch:\ndetail=%#v\nlist=%#v", detail, page.Records[0])
	}
	if _, err := repository.SessionAnalytics(context.Background(), SessionAnalyticsDetailFilter{
		SessionID: "missing", ReportingTimezone: pointerTo("UTC"),
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
		SessionID: "session-alpha",
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
