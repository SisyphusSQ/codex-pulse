package usagecost

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	basequery "github.com/SisyphusSQ/codex-pulse/internal/query"
	"github.com/SisyphusSQ/codex-pulse/internal/store"
)

func TestListProjectsValidatesMapsAndRoundTripsOpaqueCursor(t *testing.T) {
	t.Parallel()

	thirty := int64(30)
	record := safeProjectRecord("project-a", pointerToString("project-a"), pointerToString("Project A"), 30, 100)
	record.Trend[0].GenerationID = "private-project-generation"
	page := store.ProjectAnalyticsPage{
		Generation: store.CostRollupGeneration{
			GenerationID: "private-project-generation", ReportingTimezone: "UTC",
			PricingSource: "test", Currency: "USD", RollupVersion: 1,
		},
		Records: []store.ProjectAnalyticsRecord{record}, MatchedCount: 2,
		GlobalTotals: record.Totals, MatchedTotals: record.Totals, PageTotals: record.Totals,
		PricingVersions: []string{"pricing-v1"},
		NextCursor: &store.ProjectAnalyticsCursor{
			DimensionKey: "project-a", NumericValue: &thirty,
		},
	}
	var filters []store.ProjectAnalyticsFilter
	reader := &projectReaderStub{list: func(
		_ context.Context, filter store.ProjectAnalyticsFilter,
	) (store.ProjectAnalyticsPage, error) {
		filters = append(filters, filter)
		return page, nil
	}}
	service := newUsageService(t, reader)
	request := basequery.Request{
		Page: basequery.PageRequest{Limit: 1},
		Sort: []basequery.SortTerm{{Field: "totalTokens", Direction: basequery.SortDescending}},
		Filters: []basequery.FilterTerm{
			{Field: "projectId", Operator: basequery.FilterIn, Values: []string{"project-a", "project-b"}},
			{Field: "confidence", Operator: basequery.FilterEqual, Values: []string{"medium"}},
		},
		TimeRange: &basequery.LocalDateRange{
			StartDate: "1970-01-01", EndDateExclusive: "1970-01-03", TimeZone: "UTC",
		},
	}
	response, err := service.ListProjects(context.Background(), request)
	if err != nil {
		t.Fatalf("ListProjects() error = %v", err)
	}
	if len(filters) != 1 || filters[0].Range.StartAtMS != 0 ||
		filters[0].Range.EndAtMS != 2*86_400_000 || filters[0].Limit != 1 ||
		filters[0].SortField != store.ProjectAnalyticsSortTotalTokens ||
		filters[0].SortDirection != store.AnalyticsSortDescending ||
		!reflect.DeepEqual(filters[0].ProjectIDs, []string{"project-a", "project-b"}) ||
		!reflect.DeepEqual(filters[0].Confidences, []string{"medium"}) {
		t.Fatalf("project reader filter = %#v", filters)
	}
	if response.Meta.Status != basequery.ResponseComplete || response.Meta.Page == nil ||
		!response.Meta.Page.HasMore || response.Meta.Page.NextCursor == nil ||
		response.ReportingTimeZone != "UTC" || len(response.Items) != 1 ||
		response.Items[0].Project.ID == nil || *response.Items[0].Project.ID != "project-a" ||
		response.PricingSource == nil || *response.PricingSource != "test" ||
		!reflect.DeepEqual(response.PricingVersions, []string{"pricing-v1"}) {
		t.Fatalf("ListProjects() response = %#v", response)
	}
	assertKnownNumeric(t, response.Items[0].Totals.TotalTokens, 30, basequery.NumericTokens)
	encoded, _ := json.Marshal(response)
	if strings.Contains(string(encoded), "private-project-generation") {
		t.Fatalf("project response leaked generation: %s", encoded)
	}

	request.Page.Cursor = response.Meta.Page.NextCursor
	if _, err := service.ListProjects(context.Background(), request); err != nil {
		t.Fatalf("ListProjects(cursor) error = %v", err)
	}
	if len(filters) != 2 || filters[1].Cursor == nil ||
		!reflect.DeepEqual(filters[1].Cursor, page.NextCursor) {
		t.Fatalf("decoded project cursor = %#v, want %#v", filters, page.NextCursor)
	}
}

func TestListProjectsPreservesExactRangeForStore(t *testing.T) {
	t.Parallel()

	var captured store.ProjectAnalyticsFilter
	reader := &projectReaderStub{list: func(
		_ context.Context, filter store.ProjectAnalyticsFilter,
	) (store.ProjectAnalyticsPage, error) {
		captured = filter
		return store.ProjectAnalyticsPage{}, errors.New("stop after capture")
	}}
	service := newUsageService(t, reader)
	_, _ = service.ListProjects(context.Background(), basequery.Request{
		ExactTimeRange: &basequery.UTCTimeRange{
			StartAtMS: 3_600_000, EndAtMS: 7_200_000, TimeZone: "UTC",
		},
	})
	if !captured.Range.Exact || captured.Range.StartAtMS != 3_600_000 ||
		captured.Range.EndAtMS != 7_200_000 {
		t.Fatalf("project exact range = %#v", captured.Range)
	}
}

func TestProjectDetailPreservesExactRangeForStore(t *testing.T) {
	t.Parallel()

	var captured store.ProjectAnalyticsDetailFilter
	reader := &projectReaderStub{detail: func(
		_ context.Context, filter store.ProjectAnalyticsDetailFilter,
	) (store.ProjectAnalyticsSnapshot, error) {
		captured = filter
		return store.ProjectAnalyticsSnapshot{}, errors.New("stop after capture")
	}}
	service := newUsageService(t, reader)
	_, _ = service.ProjectDetail(context.Background(), ProjectDetailRequest{
		DimensionKey: "project-a",
		ExactRange: &basequery.UTCTimeRange{
			StartAtMS: 3_600_000, EndAtMS: 7_200_000, TimeZone: "UTC",
		},
	})
	if !captured.Range.Exact || captured.Range.StartAtMS != 3_600_000 ||
		captured.Range.EndAtMS != 7_200_000 {
		t.Fatalf("project detail exact range = %#v", captured.Range)
	}
}

func TestListProjectsPreservesUnknownDimensionAndPartialCost(t *testing.T) {
	t.Parallel()

	unknown := safeProjectRecord(
		"unknown|unknown|missing|missing", nil, nil, 7, -1,
	)
	unknown.AttributionConfidence = "unknown"
	unknown.AttributionSource = "missing"
	unknown.AttributionReason = "missing"
	unknown.Trend[0].AttributionConfidence = "unknown"
	unknown.Trend[0].AttributionSource = "missing"
	unknown.Trend[0].AttributionReason = "missing"
	reader := &projectReaderStub{list: func(
		context.Context, store.ProjectAnalyticsFilter,
	) (store.ProjectAnalyticsPage, error) {
		return store.ProjectAnalyticsPage{
			Generation: store.CostRollupGeneration{
				GenerationID: "generation", ReportingTimezone: "UTC",
				PricingSource: "test", Currency: "USD", RollupVersion: 1,
			},
			Records: []store.ProjectAnalyticsRecord{unknown}, MatchedCount: 1,
			GlobalTotals: unknown.Totals, MatchedTotals: unknown.Totals,
			PageTotals: unknown.Totals, PricingVersions: make([]string, 0),
		}, nil
	}}
	service := newUsageService(t, reader)
	response, err := service.ListProjects(context.Background(), basequery.Request{
		TimeRange: &basequery.LocalDateRange{
			StartDate: "1970-01-01", EndDateExclusive: "1970-01-02", TimeZone: "UTC",
		},
	})
	if err != nil {
		t.Fatalf("ListProjects(unknown) error = %v", err)
	}
	if response.Meta.Status != basequery.ResponsePartial || len(response.Items) != 1 ||
		response.Items[0].Project.ID != nil || response.Items[0].Project.DisplayName != nil ||
		response.Items[0].DimensionKey != "unknown|unknown|missing|missing" {
		t.Fatalf("unknown project response = %#v", response)
	}
	assertUnknownNumeric(t, response.Items[0].Totals.EstimatedUSDMicros, basequery.UnknownNotComputed)
}

func TestListProjectsLightIndexKeepsProjectTokensCostAndModels(t *testing.T) {
	t.Parallel()

	input, cached, output, reasoning, total, cost := int64(100), int64(20), int64(10), int64(2), int64(112), int64(129)
	record := safeProjectRecord("project-light", pointerToString("project-light"), pointerToString("Project Light"), 0, -1)
	record.AttributionConfidence = "medium"
	record.AttributionSource = "cwd_path_digest"
	record.AttributionReason = "path_derived"
	record.SessionCount = 2
	record.Totals = store.RollupTotals{
		InputTokens: &input, CachedInputTokens: &cached, OutputTokens: &output,
		ReasoningTokens: &reasoning, TotalTokens: &total, EstimatedUSDMicros: &cost,
		FirstActivityAtMS: 1, LastActivityAtMS: 1, UpdatedAtMS: 1,
	}
	record.Trend[0].GenerationID = ""
	record.Trend[0].AttributionConfidence = record.AttributionConfidence
	record.Trend[0].AttributionSource = record.AttributionSource
	record.Trend[0].AttributionReason = record.AttributionReason
	record.Trend[0].RollupTotals = record.Totals
	reader := &projectReaderStub{
		list: func(context.Context, store.ProjectAnalyticsFilter) (store.ProjectAnalyticsPage, error) {
			return store.ProjectAnalyticsPage{
				Mode: store.AnalyticsReadLightIndex, PricingSource: "openai-api", Currency: "USD",
				Records: []store.ProjectAnalyticsRecord{record}, MatchedCount: 1,
				GlobalTotals: record.Totals, MatchedTotals: record.Totals, PageTotals: record.Totals,
				PricingVersions: make([]string, 0),
			}, nil
		},
		detail: func(context.Context, store.ProjectAnalyticsDetailFilter) (store.ProjectAnalyticsSnapshot, error) {
			return store.ProjectAnalyticsSnapshot{
				Mode: store.AnalyticsReadLightIndex, PricingSource: "openai-api", Currency: "USD",
				Record: record,
				Daily:  append([]store.ProjectUsageDaily(nil), record.Trend...),
				Sessions: []store.ProjectSessionAnalyticsRecord{{
					SessionID: "session-light", DisplayTitle: "真实标题",
					TitleConfidence: store.AttributionConfidenceHigh,
					TitleSource:     store.AttributionSourceSessionIDFallback,
					TitleReason:     store.AttributionReasonObserved,
					Model: store.ModelAttribution{
						Confidence: store.AttributionConfidenceUnknown,
						Source:     store.AttributionSourceMissing, Reason: store.AttributionReasonMissing,
					},
					Activity: store.SessionActivityIdle, LastActivityAtMS: 1, Totals: record.Totals,
				}},
				NextSessionCursor: &store.ProjectSessionAnalyticsCursor{
					GenerationID: "light:1:3", DimensionKey: "project-light",
					SessionID: "session-light", LastActivityAtMS: 1,
				},
				Models: []store.ProjectModelAnalyticsRecord{{
					DimensionKey: "gpt-5.4-mini",
					Model: store.ModelAttribution{
						ModelKey: pointerToString("gpt-5.4-mini"), DisplayName: pointerToString("GPT-5.4 Mini"),
						Confidence: store.AttributionConfidenceHigh,
						Source:     store.AttributionSourceModelCanonical, Reason: store.AttributionReasonObserved,
					},
					Totals: record.Totals,
				}},
				NextModelCursor: &store.ProjectModelAnalyticsCursor{
					GenerationID: "light:1:3", DimensionKey: "project-light",
					ModelDimensionKey: "gpt-5.4-mini", TotalTokens: &total,
				},
				GlobalTotals: record.Totals, PricingVersions: []string{"pricing-v1"},
			}, nil
		},
	}
	service := newUsageService(t, reader)
	response, err := service.ListProjects(context.Background(), basequery.Request{
		TimeRange: &basequery.LocalDateRange{StartDate: "1970-01-01", EndDateExclusive: "1970-01-02", TimeZone: "UTC"},
	})
	if err != nil {
		t.Fatalf("ListProjects(light) error = %v", err)
	}
	if response.Meta.Status != basequery.ResponsePartial || len(response.Items) != 1 ||
		response.PricingSource == nil || *response.PricingSource != "openai-api" ||
		response.Currency == nil || *response.Currency != "USD" {
		t.Fatalf("response = %#v", response)
	}
	assertKnownNumeric(t, response.Items[0].Totals.TotalTokens, total, basequery.NumericTokens)
	assertUnknownNumeric(t, response.Items[0].Totals.TurnCount, basequery.UnknownUnavailable)
	assertKnownNumeric(t, response.Items[0].Totals.EstimatedUSDMicros, cost, basequery.NumericMicroUSD)
	detail, err := service.ProjectDetail(context.Background(), ProjectDetailRequest{
		DimensionKey: "project-light",
		Range:        basequery.LocalDateRange{StartDate: "1970-01-01", EndDateExclusive: "1970-01-02", TimeZone: "UTC"},
		SessionPage:  basequery.PageRequest{Limit: 1},
		ModelPage:    basequery.PageRequest{Limit: 1},
	})
	if err != nil {
		t.Fatalf("ProjectDetail(light) error = %v", err)
	}
	if detail.Meta.Status != basequery.ResponsePartial || len(detail.Sessions) != 1 || len(detail.Models) != 1 ||
		detail.SessionPage.NextCursor == nil || detail.PricingSource == nil ||
		detail.ModelPage.NextCursor == nil || *detail.PricingSource != "openai-api" ||
		detail.Currency == nil || *detail.Currency != "USD" {
		t.Fatalf("detail = %#v", detail)
	}
	assertKnownNumeric(t, detail.Models[0].Totals.EstimatedUSDMicros, cost, basequery.NumericMicroUSD)
	assertUnknownNumeric(t, detail.Models[0].Totals.TurnCount, basequery.UnknownUnavailable)
}

func TestListProjectsKnownEmptyIsComplete(t *testing.T) {
	t.Parallel()

	zero := int64(0)
	emptyTotals := store.RollupTotals{
		InputTokens: &zero, CachedInputTokens: &zero, OutputTokens: &zero,
		ReasoningTokens: &zero, TotalTokens: &zero,
	}
	service := newUsageService(t, &projectReaderStub{list: func(
		context.Context, store.ProjectAnalyticsFilter,
	) (store.ProjectAnalyticsPage, error) {
		return store.ProjectAnalyticsPage{
			Generation: store.CostRollupGeneration{
				GenerationID: "generation", ReportingTimezone: "UTC",
				PricingSource: "test", Currency: "USD", RollupVersion: 1,
			},
			Records: make([]store.ProjectAnalyticsRecord, 0), GlobalTotals: emptyTotals,
			MatchedTotals: emptyTotals, PageTotals: emptyTotals,
			PricingVersions: make([]string, 0),
		}, nil
	}})
	response, err := service.ListProjects(context.Background(), basequery.Request{
		TimeRange: &basequery.LocalDateRange{
			StartDate: "1970-01-01", EndDateExclusive: "1970-01-02", TimeZone: "UTC",
		},
	})
	if err != nil {
		t.Fatalf("ListProjects(empty) error = %v", err)
	}
	if response.Meta.Status != basequery.ResponseComplete || response.Items == nil {
		t.Fatalf("empty project response = %#v", response)
	}
	assertKnownNumeric(t, response.GlobalTotals.EstimatedUSDMicros, 0, basequery.NumericMicroUSD)
}

func TestListProjectsMixedPricedAndUnpricedTurnsIsPartial(t *testing.T) {
	t.Parallel()

	record := safeProjectRecord(
		"project-mixed", pointerToString("project-mixed"), pointerToString("Project Mixed"), 7, 11,
	)
	record.Totals.TurnCount = 2
	record.Totals.UnpricedTurnCount = 1
	service := newUsageService(t, &projectReaderStub{list: func(
		context.Context, store.ProjectAnalyticsFilter,
	) (store.ProjectAnalyticsPage, error) {
		return store.ProjectAnalyticsPage{
			Generation: store.CostRollupGeneration{
				GenerationID: "generation", ReportingTimezone: "UTC",
				PricingSource: "test", Currency: "USD", RollupVersion: 1,
			},
			Records: []store.ProjectAnalyticsRecord{record}, MatchedCount: 1,
			GlobalTotals: record.Totals, MatchedTotals: record.Totals, PageTotals: record.Totals,
			PricingVersions: []string{"pricing-v1"},
		}, nil
	}})
	response, err := service.ListProjects(context.Background(), basequery.Request{
		TimeRange: &basequery.LocalDateRange{
			StartDate: "1970-01-01", EndDateExclusive: "1970-01-02", TimeZone: "UTC",
		},
	})
	if err != nil {
		t.Fatalf("ListProjects(mixed pricing) error = %v", err)
	}
	if response.Meta.Status != basequery.ResponsePartial {
		t.Fatalf("ListProjects(mixed pricing) status = %q, want partial", response.Meta.Status)
	}
}

func TestListProjectsRejectsTrendIdentityDrift(t *testing.T) {
	t.Parallel()

	record := safeProjectRecord(
		"project-a", pointerToString("project-a"), pointerToString("Project A"), 7, 11,
	)
	record.Trend[0].ProjectID = pointerToString("project-b")
	record.Trend[0].ProjectDisplayName = pointerToString("Project B")
	service := newUsageService(t, &projectReaderStub{list: func(
		context.Context, store.ProjectAnalyticsFilter,
	) (store.ProjectAnalyticsPage, error) {
		return store.ProjectAnalyticsPage{
			Generation: store.CostRollupGeneration{
				GenerationID: "generation", ReportingTimezone: "UTC",
				PricingSource: "test", Currency: "USD", RollupVersion: 1,
			},
			Records: []store.ProjectAnalyticsRecord{record}, MatchedCount: 1,
			GlobalTotals: record.Totals, MatchedTotals: record.Totals,
			PageTotals: record.Totals, PricingVersions: []string{"pricing-v1"},
		}, nil
	}})
	_, err := service.ListProjects(context.Background(), basequery.Request{
		TimeRange: &basequery.LocalDateRange{
			StartDate: "1970-01-01", EndDateExclusive: "1970-01-02", TimeZone: "UTC",
		},
	})
	if !errors.Is(err, basequery.ErrUnavailable) {
		t.Fatalf("ListProjects(trend identity drift) error = %v, want unavailable", err)
	}
}

func TestListProjectsRejectsMissingRangeInvalidFiltersAndCursorReplay(t *testing.T) {
	t.Parallel()

	calls := 0
	record := safeProjectRecord("project-a", pointerToString("project-a"), pointerToString("Project A"), 1, 1)
	reader := &projectReaderStub{list: func(
		context.Context, store.ProjectAnalyticsFilter,
	) (store.ProjectAnalyticsPage, error) {
		calls++
		return store.ProjectAnalyticsPage{
			Generation: store.CostRollupGeneration{
				GenerationID: "generation", ReportingTimezone: "UTC",
				PricingSource: "test", Currency: "USD", RollupVersion: 1,
			},
			Records: []store.ProjectAnalyticsRecord{record}, MatchedCount: 2,
			GlobalTotals: record.Totals, MatchedTotals: record.Totals, PageTotals: record.Totals,
			PricingVersions: []string{"pricing-v1"},
			NextCursor: &store.ProjectAnalyticsCursor{
				DimensionKey: "project-a", NumericValue: pointerToInt64(1),
			},
		}, nil
	}}
	service := newUsageService(t, reader)
	validRange := &basequery.LocalDateRange{
		StartDate: "1970-01-01", EndDateExclusive: "1970-01-02", TimeZone: "UTC",
	}
	invalid := []basequery.Request{
		{},
		{TimeRange: validRange, Sort: []basequery.SortTerm{{Field: "sessionId", Direction: basequery.SortDescending}}},
		{TimeRange: validRange, Filters: []basequery.FilterTerm{{Field: "cwd", Operator: basequery.FilterEqual, Values: []string{"private"}}}},
		{TimeRange: validRange, Filters: []basequery.FilterTerm{{Field: "confidence", Operator: basequery.FilterEqual, Values: []string{"certain"}}}},
		{TimeRange: validRange, Sort: []basequery.SortTerm{{Field: "totalTokens", Direction: basequery.SortDescending}, {Field: "displayName", Direction: basequery.SortAscending}}},
	}
	for _, request := range invalid {
		if _, err := service.ListProjects(context.Background(), request); !errors.Is(err, basequery.ErrValidation) {
			t.Fatalf("ListProjects(%#v) error = %v, want validation", request, err)
		}
	}
	if calls != 0 {
		t.Fatalf("invalid project requests reached reader %d times", calls)
	}
	first, err := service.ListProjects(context.Background(), basequery.Request{
		Page: basequery.PageRequest{Limit: 1}, TimeRange: validRange,
	})
	if err != nil {
		t.Fatalf("ListProjects(first) error = %v", err)
	}
	validCursor := *first.Meta.Page.NextCursor
	tamperedBytes := []byte(validCursor)
	if tamperedBytes[0] == 'A' {
		tamperedBytes[0] = 'B'
	} else {
		tamperedBytes[0] = 'A'
	}
	tampered := string(tamperedBytes)
	for _, request := range []basequery.Request{
		{Page: basequery.PageRequest{Limit: 1, Cursor: &tampered}, TimeRange: validRange},
		{Page: basequery.PageRequest{Limit: 1, Cursor: &validCursor}, TimeRange: validRange,
			Sort: []basequery.SortTerm{{Field: "displayName", Direction: basequery.SortAscending}}},
	} {
		if _, err := service.ListProjects(context.Background(), request); !errors.Is(err, basequery.ErrValidation) {
			t.Fatalf("ListProjects(cursor=%#v) error = %v, want validation", request, err)
		}
	}
	if calls != 1 {
		t.Fatalf("invalid project cursors reached reader; calls=%d", calls)
	}
}

func TestProjectDetailMapsDailyNotFoundAndUnavailable(t *testing.T) {
	t.Parallel()

	record := safeProjectRecord("project-a", pointerToString("project-a"), pointerToString("Project A"), 10, 20)
	record.SessionCount = 1
	record.Trend[0].GenerationID = "private-detail-generation"
	privateCause := errors.New("private-project-driver-cause")
	reader := &projectReaderStub{detail: func(
		_ context.Context, filter store.ProjectAnalyticsDetailFilter,
	) (store.ProjectAnalyticsSnapshot, error) {
		switch filter.DimensionKey {
		case "missing":
			return store.ProjectAnalyticsSnapshot{}, store.ErrNotFound
		case "unavailable":
			return store.ProjectAnalyticsSnapshot{}, store.ErrAnalyticsUnavailable
		case "driver":
			return store.ProjectAnalyticsSnapshot{}, privateCause
		default:
			return store.ProjectAnalyticsSnapshot{
				Generation: store.CostRollupGeneration{
					GenerationID: "private-detail-generation", ReportingTimezone: "UTC",
					PricingSource: "test", Currency: "USD", RollupVersion: 1,
				},
				Record: record, GlobalTotals: record.Totals,
				Daily: []store.ProjectUsageDaily{{
					GenerationID: "private-detail-generation", BucketStartMS: 0,
					ReportingTimezone: "UTC", DimensionKey: "project-a",
					ProjectID: pointerToString("project-a"), ProjectDisplayName: pointerToString("Project A"),
					AttributionConfidence: "high", AttributionSource: "registered_root",
					AttributionReason: "root_matched", RollupTotals: record.Totals,
				}},
				Sessions: []store.ProjectSessionAnalyticsRecord{{
					SessionID: "session-a", DisplayTitle: "Safe title",
					TitleConfidence: store.AttributionConfidenceHigh,
					TitleSource:     store.AttributionSourceSessionIDFallback,
					TitleReason:     store.AttributionReasonStableIdentity,
					Model: store.ModelAttribution{
						ModelKey: pointerToString("model-a"), DisplayName: pointerToString("Model A"),
						Confidence: store.AttributionConfidenceHigh,
						Source:     store.AttributionSourceModelCanonical,
						Reason:     store.AttributionReasonObserved,
					},
					Activity: store.SessionActivityIdle, LastActivityAtMS: 10,
					Totals: safeProjectContributionTotals(10, 20, 10),
				}},
				Models: make([]store.ProjectModelAnalyticsRecord, 0), PricingVersions: []string{"pricing-v1"},
			}, nil
		}
	}}
	service := newUsageService(t, reader)
	request := ProjectDetailRequest{
		DimensionKey: "project-a",
		Range: basequery.LocalDateRange{
			StartDate: "1970-01-01", EndDateExclusive: "1970-01-02", TimeZone: "UTC",
		},
	}
	response, err := service.ProjectDetail(context.Background(), request)
	if err != nil || len(response.Daily) != 1 || response.Item.DimensionKey != "project-a" {
		t.Fatalf("ProjectDetail() = %#v, %v", response, err)
	}
	encoded, _ := json.Marshal(response)
	if strings.Contains(string(encoded), "private-detail-generation") {
		t.Fatalf("project detail leaked generation: %s", encoded)
	}
	for dimension, category := range map[string]error{
		"missing": store.ErrNotFound, "unavailable": store.ErrAnalyticsUnavailable, "driver": privateCause,
	} {
		request.DimensionKey = dimension
		_, err := service.ProjectDetail(context.Background(), request)
		if category == store.ErrNotFound && !errors.Is(err, basequery.ErrNotFound) {
			t.Fatalf("ProjectDetail(%s) error = %v, want not found", dimension, err)
		}
		if category != store.ErrNotFound && (!errors.Is(err, basequery.ErrUnavailable) ||
			strings.Contains(err.Error(), category.Error())) {
			t.Fatalf("ProjectDetail(%s) error = %q, want content-free unavailable", dimension, err)
		}
	}
}

func TestProjectDetailNormalizesPageLimitsAndRejectsInvalidPagesBeforeReader(t *testing.T) {
	t.Parallel()

	var filters []store.ProjectAnalyticsDetailFilter
	reader := &projectReaderStub{detail: func(
		_ context.Context, filter store.ProjectAnalyticsDetailFilter,
	) (store.ProjectAnalyticsSnapshot, error) {
		filters = append(filters, filter)
		return safeProjectDetailSnapshot(), nil
	}}
	service := newUsageService(t, reader)
	request := ProjectDetailRequest{
		DimensionKey: "project-a",
		Range: basequery.LocalDateRange{
			StartDate: "1970-01-01", EndDateExclusive: "1970-01-02", TimeZone: "UTC",
		},
	}
	response, err := service.ProjectDetail(context.Background(), request)
	if err != nil {
		t.Fatalf("ProjectDetail(default pages) error = %v", err)
	}
	if len(filters) != 1 || filters[0].SessionLimit != 20 || filters[0].ModelLimit != 20 ||
		response.SessionPage.Limit != 20 || response.ModelPage.Limit != 20 {
		t.Fatalf("ProjectDetail(default pages) response/filters = %#v / %#v", response, filters)
	}

	emptyCursor := ""
	for name, pages := range map[string]struct {
		session basequery.PageRequest
		model   basequery.PageRequest
	}{
		"negative session limit":  {session: basequery.PageRequest{Limit: -1}},
		"oversized session limit": {session: basequery.PageRequest{Limit: 51}},
		"empty session cursor": {
			session: basequery.PageRequest{Limit: 1, Cursor: &emptyCursor},
		},
		"negative model limit":  {model: basequery.PageRequest{Limit: -1}},
		"oversized model limit": {model: basequery.PageRequest{Limit: 51}},
		"empty model cursor": {
			model: basequery.PageRequest{Limit: 1, Cursor: &emptyCursor},
		},
	} {
		invalid := request
		invalid.SessionPage = pages.session
		invalid.ModelPage = pages.model
		if _, err := service.ProjectDetail(context.Background(), invalid); !errors.Is(err, basequery.ErrValidation) {
			t.Fatalf("ProjectDetail(%s) error = %v, want validation", name, err)
		}
	}
	if len(filters) != 1 {
		t.Fatalf("invalid Project detail pages reached reader: %#v", filters)
	}
}

func TestProjectDetailMapsContributionPagesAndRoundTripsBoundCursors(t *testing.T) {
	t.Parallel()

	record := safeProjectRecord(
		"project-a", pointerToString("project-a"), pointerToString("Project A"), 30, 300,
	)
	record.SessionCount = 2
	record.Trend = []store.ProjectUsageDaily{{
		GenerationID: "private-detail-generation", BucketStartMS: 0, ReportingTimezone: "UTC",
		DimensionKey: "project-a", ProjectID: pointerToString("project-a"),
		ProjectDisplayName: pointerToString("Project A"), AttributionConfidence: "high",
		AttributionSource: "registered_root", AttributionReason: "root_matched",
		RollupTotals: record.Totals,
	}}
	twenty, ten := int64(20), int64(10)
	sessionCursor := &store.ProjectSessionAnalyticsCursor{
		GenerationID: "private-detail-generation", DimensionKey: "project-a",
		SessionID: "session-beta", LastActivityAtMS: 20,
	}
	modelCursor := &store.ProjectModelAnalyticsCursor{
		GenerationID: "private-detail-generation", DimensionKey: "project-a",
		ModelDimensionKey: "model-b", TotalTokens: &twenty,
	}
	var filters []store.ProjectAnalyticsDetailFilter
	reader := &projectReaderStub{detail: func(
		_ context.Context, filter store.ProjectAnalyticsDetailFilter,
	) (store.ProjectAnalyticsSnapshot, error) {
		filters = append(filters, filter)
		snapshot := store.ProjectAnalyticsSnapshot{
			Generation: store.CostRollupGeneration{
				GenerationID: "private-detail-generation", ReportingTimezone: "UTC",
				PricingSource: "test", Currency: "USD", RollupVersion: 1,
			},
			Record: record, GlobalTotals: record.Totals, Daily: append([]store.ProjectUsageDaily(nil), record.Trend...),
			PricingVersions: []string{"pricing-v1"},
		}
		if filter.SessionCursor == nil {
			snapshot.Sessions = []store.ProjectSessionAnalyticsRecord{{
				SessionID: "session-beta", DisplayTitle: "Beta safe title",
				TitleConfidence: store.AttributionConfidenceHigh,
				TitleSource:     store.AttributionSourceSessionIDFallback,
				TitleReason:     store.AttributionReasonStableIdentity,
				Model: store.ModelAttribution{
					ModelKey: pointerToString("model-b"), DisplayName: pointerToString("Model B"),
					Confidence: store.AttributionConfidenceHigh,
					Source:     store.AttributionSourceModelCanonical, Reason: store.AttributionReasonObserved,
				},
				Activity: store.SessionActivityIdle, LastActivityAtMS: 20,
				Totals: safeProjectContributionTotals(twenty, 200, 20),
			}}
			snapshot.NextSessionCursor = sessionCursor
			snapshot.Models = []store.ProjectModelAnalyticsRecord{{
				DimensionKey: "model-b", Model: store.ModelAttribution{
					ModelKey: pointerToString("model-b"), DisplayName: pointerToString("Model B"),
					Confidence: store.AttributionConfidenceHigh,
					Source:     store.AttributionSourceModelCanonical, Reason: store.AttributionReasonObserved,
				},
				Totals: safeProjectRecord("ignored", nil, nil, twenty, 200).Totals,
			}}
			snapshot.NextModelCursor = modelCursor
			return snapshot, nil
		}
		snapshot.Sessions = []store.ProjectSessionAnalyticsRecord{{
			SessionID: "session-alpha", DisplayTitle: "Alpha safe title",
			TitleConfidence: store.AttributionConfidenceHigh,
			TitleSource:     store.AttributionSourceSessionIDFallback,
			TitleReason:     store.AttributionReasonStableIdentity,
			Model: store.ModelAttribution{
				ModelKey: pointerToString("model-a"), DisplayName: pointerToString("Model A"),
				Confidence: store.AttributionConfidenceHigh,
				Source:     store.AttributionSourceModelCanonical, Reason: store.AttributionReasonObserved,
			},
			Activity: store.SessionActivityIdle, LastActivityAtMS: 10,
			Totals: safeProjectContributionTotals(ten, 100, 10),
		}}
		snapshot.Models = []store.ProjectModelAnalyticsRecord{{
			DimensionKey: "model-a", Model: store.ModelAttribution{
				ModelKey: pointerToString("model-a"), DisplayName: pointerToString("Model A"),
				Confidence: store.AttributionConfidenceHigh,
				Source:     store.AttributionSourceModelCanonical, Reason: store.AttributionReasonObserved,
			},
			Totals: safeProjectRecord("ignored", nil, nil, ten, 100).Totals,
		}}
		return snapshot, nil
	}}
	service := newUsageService(t, reader)
	rangeValue := basequery.LocalDateRange{
		StartDate: "1970-01-01", EndDateExclusive: "1970-01-02", TimeZone: "UTC",
	}
	first, err := service.ProjectDetail(context.Background(), ProjectDetailRequest{
		DimensionKey: "project-a", Range: rangeValue,
		SessionPage: basequery.PageRequest{Limit: 1}, ModelPage: basequery.PageRequest{Limit: 1},
	})
	if err != nil {
		t.Fatalf("ProjectDetail(first) error = %v", err)
	}
	if len(filters) != 1 || filters[0].SessionLimit != 1 || filters[0].ModelLimit != 1 ||
		len(first.Sessions) != 1 || len(first.Models) != 1 ||
		first.SessionPage.NextCursor == nil || first.ModelPage.NextCursor == nil ||
		first.Item.SessionCount.Value == nil || *first.Item.SessionCount.Value != 2 ||
		len(first.Item.Trend) != 1 {
		t.Fatalf("ProjectDetail(first) = %#v; filters=%#v", first, filters)
	}
	if first.Sessions[0].DisplayTitle != "Beta safe title" ||
		first.Sessions[0].Totals.TotalTokens.Value == nil ||
		*first.Sessions[0].Totals.TotalTokens.Value != 20 ||
		first.Models[0].DimensionKey != "model-b" {
		t.Fatalf("ProjectDetail(first items) = %#v / %#v", first.Sessions, first.Models)
	}

	second, err := service.ProjectDetail(context.Background(), ProjectDetailRequest{
		DimensionKey: "project-a", Range: rangeValue,
		SessionPage: basequery.PageRequest{Limit: 1, Cursor: first.SessionPage.NextCursor},
		ModelPage:   basequery.PageRequest{Limit: 1, Cursor: first.ModelPage.NextCursor},
	})
	if err != nil {
		t.Fatalf("ProjectDetail(second) error = %v", err)
	}
	if len(filters) != 2 || !reflect.DeepEqual(filters[1].SessionCursor, sessionCursor) ||
		!reflect.DeepEqual(filters[1].ModelCursor, modelCursor) || len(second.Sessions) != 1 ||
		second.Sessions[0].DisplayTitle != "Alpha safe title" ||
		second.SessionPage.NextCursor != nil || second.ModelPage.NextCursor != nil {
		t.Fatalf("ProjectDetail(second) = %#v; filters=%#v", second, filters)
	}
	tamperedCursorBytes := []byte(*first.SessionPage.NextCursor)
	if tamperedCursorBytes[0] == 'A' {
		tamperedCursorBytes[0] = 'B'
	} else {
		tamperedCursorBytes[0] = 'A'
	}
	tamperedCursor := string(tamperedCursorBytes)

	for name, request := range map[string]ProjectDetailRequest{
		"tampered": {
			DimensionKey: "project-a", Range: rangeValue,
			SessionPage: basequery.PageRequest{Limit: 1, Cursor: &tamperedCursor},
			ModelPage:   basequery.PageRequest{Limit: 1},
		},
		"cross endpoint": {
			DimensionKey: "project-a", Range: rangeValue,
			SessionPage: basequery.PageRequest{Limit: 1, Cursor: first.ModelPage.NextCursor},
			ModelPage:   basequery.PageRequest{Limit: 1},
		},
		"cross project": {
			DimensionKey: "project-b", Range: rangeValue,
			SessionPage: basequery.PageRequest{Limit: 1, Cursor: first.SessionPage.NextCursor},
			ModelPage:   basequery.PageRequest{Limit: 1},
		},
		"cross range": {
			DimensionKey: "project-a",
			Range: basequery.LocalDateRange{
				StartDate: "1970-01-02", EndDateExclusive: "1970-01-03", TimeZone: "UTC",
			},
			SessionPage: basequery.PageRequest{Limit: 1, Cursor: first.SessionPage.NextCursor},
			ModelPage:   basequery.PageRequest{Limit: 1},
		},
	} {
		if _, err := service.ProjectDetail(context.Background(), request); !errors.Is(err, basequery.ErrValidation) {
			t.Fatalf("ProjectDetail(%s) error = %v, want validation", name, err)
		}
	}
	restartedService := newUsageService(t, reader)
	if _, err := restartedService.ProjectDetail(context.Background(), ProjectDetailRequest{
		DimensionKey: "project-a", Range: rangeValue,
		SessionPage: basequery.PageRequest{Limit: 1, Cursor: first.SessionPage.NextCursor},
		ModelPage:   basequery.PageRequest{Limit: 1},
	}); !errors.Is(err, basequery.ErrValidation) {
		t.Fatalf("ProjectDetail(old process cursor) error = %v, want validation", err)
	}
	if len(filters) != 2 {
		t.Fatalf("invalid detail cursors reached reader: %#v", filters)
	}
}

func TestProjectDetailRejectsOutOfOrderSessionPage(t *testing.T) {
	t.Parallel()

	record := safeProjectRecord(
		"project-a", pointerToString("project-a"), pointerToString("Project A"), 30, 300,
	)
	record.SessionCount = 2
	record.Trend[0].GenerationID = "private-detail-generation"
	makeSession := func(id string, atMS int64) store.ProjectSessionAnalyticsRecord {
		return store.ProjectSessionAnalyticsRecord{
			SessionID: id, DisplayTitle: "Safe title",
			TitleConfidence: store.AttributionConfidenceHigh,
			TitleSource:     store.AttributionSourceSessionIDFallback,
			TitleReason:     store.AttributionReasonStableIdentity,
			Model: store.ModelAttribution{
				ModelKey: pointerToString("model-a"), DisplayName: pointerToString("Model A"),
				Confidence: store.AttributionConfidenceHigh,
				Source:     store.AttributionSourceModelCanonical, Reason: store.AttributionReasonObserved,
			},
			Activity: store.SessionActivityIdle, LastActivityAtMS: atMS,
			Totals: safeProjectContributionTotals(15, 150, atMS),
		}
	}
	snapshot := store.ProjectAnalyticsSnapshot{
		Generation: store.CostRollupGeneration{
			GenerationID: "private-detail-generation", ReportingTimezone: "UTC",
			PricingSource: "test", Currency: "USD", RollupVersion: 1,
		},
		Record: record, Daily: append([]store.ProjectUsageDaily(nil), record.Trend...),
		Sessions: []store.ProjectSessionAnalyticsRecord{
			makeSession("session-older", 10), makeSession("session-newer", 20),
		},
		Models:       make([]store.ProjectModelAnalyticsRecord, 0),
		GlobalTotals: record.Totals, PricingVersions: []string{"pricing-v1"},
	}
	rangeValue := basequery.UTCTimeRange{StartAtMS: 0, EndAtMS: 86_400_000, TimeZone: "UTC"}
	if err := validateProjectSnapshotShape(snapshot, rangeValue, "project-a", 2, true, 2); err == nil {
		t.Fatal("validateProjectSnapshotShape(out-of-order sessions) error = nil")
	}
	snapshot.Sessions = make([]store.ProjectSessionAnalyticsRecord, 0)
	makeModel := func(key string, tokens int64) store.ProjectModelAnalyticsRecord {
		return store.ProjectModelAnalyticsRecord{
			DimensionKey: key,
			Model: store.ModelAttribution{
				ModelKey: pointerToString(key), DisplayName: pointerToString(key),
				Confidence: store.AttributionConfidenceHigh,
				Source:     store.AttributionSourceModelCanonical,
				Reason:     store.AttributionReasonObserved,
			},
			Totals: safeProjectRecord("ignored", nil, nil, tokens, tokens*10).Totals,
		}
	}
	snapshot.Models = []store.ProjectModelAnalyticsRecord{
		makeModel("model-low", 10), makeModel("model-high", 20),
	}
	if err := validateProjectSnapshotShape(snapshot, rangeValue, "project-a", 2, true, 2); err == nil {
		t.Fatal("validateProjectSnapshotShape(out-of-order models) error = nil")
	}
}

func TestProjectDetailRejectsTruncatedFirstSessionPageWithoutCursor(t *testing.T) {
	t.Parallel()

	record := safeProjectRecord(
		"project-a", pointerToString("project-a"), pointerToString("Project A"), 30, 300,
	)
	record.SessionCount = 2
	record.Trend[0].GenerationID = "private-detail-generation"
	snapshot := store.ProjectAnalyticsSnapshot{
		Generation: store.CostRollupGeneration{
			GenerationID: "private-detail-generation", ReportingTimezone: "UTC",
			PricingSource: "test", Currency: "USD", RollupVersion: 1,
		},
		Record: record, Daily: append([]store.ProjectUsageDaily(nil), record.Trend...),
		Sessions: []store.ProjectSessionAnalyticsRecord{{
			SessionID: "session-only", DisplayTitle: "Safe title",
			TitleConfidence: store.AttributionConfidenceHigh,
			TitleSource:     store.AttributionSourceSessionIDFallback,
			TitleReason:     store.AttributionReasonStableIdentity,
			Model: store.ModelAttribution{
				ModelKey: pointerToString("model-a"), DisplayName: pointerToString("Model A"),
				Confidence: store.AttributionConfidenceHigh,
				Source:     store.AttributionSourceModelCanonical,
				Reason:     store.AttributionReasonObserved,
			},
			Activity: store.SessionActivityIdle, LastActivityAtMS: 10,
			Totals: safeProjectContributionTotals(30, 300, 10),
		}},
		Models:       make([]store.ProjectModelAnalyticsRecord, 0),
		GlobalTotals: record.Totals, PricingVersions: []string{"pricing-v1"},
	}
	rangeValue := basequery.UTCTimeRange{StartAtMS: 0, EndAtMS: 86_400_000, TimeZone: "UTC"}
	if err := validateProjectSnapshotShape(snapshot, rangeValue, "project-a", 2, true, 2); err == nil {
		t.Fatal("validateProjectSnapshotShape(truncated first session page) error = nil")
	}
}

func TestProjectCursorSupportsTextAndRejectsCrossEndpoint(t *testing.T) {
	t.Parallel()

	display := "Project A"
	cursor := &store.ProjectAnalyticsCursor{
		DimensionKey: "project-a", TextValue: &display,
	}
	encoded, err := encodeProjectCursor(cursor, "displayName", basequery.SortAscending)
	if err != nil || encoded == nil {
		t.Fatalf("encodeProjectCursor() = %v, %v", encoded, err)
	}
	decoded, err := decodeProjectCursor(*encoded, "displayName", basequery.SortAscending)
	if err != nil || !reflect.DeepEqual(decoded, cursor) {
		t.Fatalf("decodeProjectCursor() = %#v, %v, want %#v", decoded, err, cursor)
	}
	if _, err := decodeSessionCursor(*encoded, "displayName", basequery.SortAscending); !errors.Is(err, basequery.ErrValidation) {
		t.Fatalf("decodeSessionCursor(project cursor) error = %v, want validation", err)
	}
}

type projectReaderStub struct {
	list   func(context.Context, store.ProjectAnalyticsFilter) (store.ProjectAnalyticsPage, error)
	detail func(context.Context, store.ProjectAnalyticsDetailFilter) (store.ProjectAnalyticsSnapshot, error)
}

func (reader *projectReaderStub) UsageCostRange(
	context.Context,
	store.AnalyticsRange,
) (store.UsageCostRangeSnapshot, error) {
	return store.UsageCostRangeSnapshot{}, errors.New("usage reader not configured")
}

func (reader *projectReaderStub) ListProjectAnalytics(
	ctx context.Context,
	filter store.ProjectAnalyticsFilter,
) (store.ProjectAnalyticsPage, error) {
	if reader.list == nil {
		return store.ProjectAnalyticsPage{}, errors.New("project list reader not configured")
	}
	return reader.list(ctx, filter)
}

func (reader *projectReaderStub) ProjectAnalytics(
	ctx context.Context,
	filter store.ProjectAnalyticsDetailFilter,
) (store.ProjectAnalyticsSnapshot, error) {
	if reader.detail == nil {
		return store.ProjectAnalyticsSnapshot{}, errors.New("project detail reader not configured")
	}
	return reader.detail(ctx, filter)
}

func safeProjectRecord(
	dimensionKey string,
	projectID, display *string,
	tokens int64,
	cost int64,
) store.ProjectAnalyticsRecord {
	zero := int64(0)
	totals := store.RollupTotals{
		TurnCount: 1, InputTokens: &tokens, CachedInputTokens: &zero,
		OutputTokens: &zero, ReasoningTokens: &zero, TotalTokens: &tokens,
		FirstActivityAtMS: 1, LastActivityAtMS: 1, UpdatedAtMS: 1,
	}
	if cost >= 0 {
		totals.EstimatedUSDMicros = &cost
		totals.PricedTurnCount = 1
	} else {
		totals.UnpricedTurnCount = 1
	}
	return store.ProjectAnalyticsRecord{
		DimensionKey: dimensionKey, ProjectID: projectID, ProjectDisplayName: display,
		AttributionConfidence: "high", AttributionSource: "registered_root",
		AttributionReason: "root_matched", SessionCount: 1,
		Trend: []store.ProjectUsageDaily{{
			GenerationID: "generation", BucketStartMS: 0, ReportingTimezone: "UTC",
			DimensionKey: dimensionKey, ProjectID: projectID, ProjectDisplayName: display,
			AttributionConfidence: "high", AttributionSource: "registered_root",
			AttributionReason: "root_matched", RollupTotals: totals,
		}},
		Totals: totals,
	}
}

func safeProjectContributionTotals(tokens int64, cost int64, atMS int64) store.RollupTotals {
	totals := safeProjectRecord("ignored", nil, nil, tokens, cost).Totals
	totals.FirstActivityAtMS = atMS
	totals.LastActivityAtMS = atMS
	totals.UpdatedAtMS = atMS
	return totals
}

func safeProjectDetailSnapshot() store.ProjectAnalyticsSnapshot {
	record := safeProjectRecord(
		"project-a", pointerToString("project-a"), pointerToString("Project A"), 10, 20,
	)
	return store.ProjectAnalyticsSnapshot{
		Generation: store.CostRollupGeneration{
			GenerationID: "generation", ReportingTimezone: "UTC",
			PricingSource: "test", Currency: "USD", RollupVersion: 1,
		},
		Record: record, Daily: append([]store.ProjectUsageDaily(nil), record.Trend...),
		Sessions: []store.ProjectSessionAnalyticsRecord{{
			SessionID: "session-a", DisplayTitle: "Safe title",
			TitleConfidence: store.AttributionConfidenceHigh,
			TitleSource:     store.AttributionSourceSessionIDFallback,
			TitleReason:     store.AttributionReasonStableIdentity,
			Model: store.ModelAttribution{
				ModelKey: pointerToString("model-a"), DisplayName: pointerToString("Model A"),
				Confidence: store.AttributionConfidenceHigh,
				Source:     store.AttributionSourceModelCanonical,
				Reason:     store.AttributionReasonObserved,
			},
			Activity: store.SessionActivityIdle, LastActivityAtMS: 1,
			Totals: safeProjectContributionTotals(10, 20, 1),
		}},
		Models: []store.ProjectModelAnalyticsRecord{{
			DimensionKey: "model-a",
			Model: store.ModelAttribution{
				ModelKey: pointerToString("model-a"), DisplayName: pointerToString("Model A"),
				Confidence: store.AttributionConfidenceHigh,
				Source:     store.AttributionSourceModelCanonical,
				Reason:     store.AttributionReasonObserved,
			},
			Totals: safeProjectContributionTotals(10, 20, 1),
		}},
		GlobalTotals: record.Totals, PricingVersions: []string{"pricing-v1"},
	}
}

func pointerToInt64(value int64) *int64 { return &value }
