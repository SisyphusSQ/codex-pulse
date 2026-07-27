package usagecost

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/SisyphusSQ/codex-pulse/internal/pricing"
	basequery "github.com/SisyphusSQ/codex-pulse/internal/query"
	"github.com/SisyphusSQ/codex-pulse/internal/store"
	storesqlite "github.com/SisyphusSQ/codex-pulse/internal/store/sqlite"
)

func TestListSessionsValidatesMapsAndRoundTripsOpaqueCursor(t *testing.T) {
	t.Parallel()

	zero, thirty, cost := int64(0), int64(30), int64(100)
	last := int64(300)
	page := store.SessionAnalyticsPage{
		Mode: store.AnalyticsReadActiveRollup,
		Generation: &store.CostRollupGeneration{
			GenerationID: "private-session-generation", ReportingTimezone: "UTC",
			PricingSource: "test", Currency: "USD", RollupVersion: 1,
		},
		Records: []store.SessionAnalyticsRecord{{
			SessionID: "session-alpha", DisplayTitle: "Alpha safe title",
			TitleConfidence: store.AttributionConfidenceHigh,
			TitleSource:     store.AttributionSourceSessionIDFallback,
			TitleReason:     store.AttributionReasonStableIdentity,
			Project: store.ProjectAttribution{
				ProjectID: pointerToString("project-a"), DisplayName: pointerToString("Project A"),
				Confidence: store.AttributionConfidenceHigh,
				Source:     store.AttributionSourceRegisteredRoot, Reason: store.AttributionReasonRootMatched,
			},
			Model: store.ModelAttribution{
				ModelKey: pointerToString("model-a"), DisplayName: pointerToString("Model A"),
				Confidence: store.AttributionConfidenceHigh,
				Source:     store.AttributionSourceModelCanonical, Reason: store.AttributionReasonObserved,
			},
			Activity: store.SessionActivityActive, LastActivityAtMS: &last,
			Rollup: &store.RollupTotals{
				TurnCount: 1, InputTokens: &thirty, CachedInputTokens: &zero,
				OutputTokens: &zero, ReasoningTokens: &zero, TotalTokens: &thirty,
				EstimatedUSDMicros: &cost, PricedTurnCount: 1,
				FirstActivityAtMS: 300, LastActivityAtMS: 300, UpdatedAtMS: 400,
			},
		}},
		MatchedCount: 2,
		MatchedTotals: &store.RollupTotals{
			TurnCount: 1, InputTokens: &thirty, CachedInputTokens: &zero,
			OutputTokens: &zero, ReasoningTokens: &zero, TotalTokens: &thirty,
			EstimatedUSDMicros: &cost, PricedTurnCount: 1,
			FirstActivityAtMS: 300, LastActivityAtMS: 300, UpdatedAtMS: 400,
		},
		PageTotals: &store.RollupTotals{
			TurnCount: 1, InputTokens: &thirty, CachedInputTokens: &zero,
			OutputTokens: &zero, ReasoningTokens: &zero, TotalTokens: &thirty,
			EstimatedUSDMicros: &cost, PricedTurnCount: 1,
			FirstActivityAtMS: 300, LastActivityAtMS: 300, UpdatedAtMS: 400,
		},
		NextCursor: &store.SessionAnalyticsCursor{
			SessionID: "session-alpha", Value: &thirty,
		},
	}
	var filters []store.SessionAnalyticsFilter
	reader := &sessionReaderStub{list: func(
		_ context.Context, filter store.SessionAnalyticsFilter,
	) (store.SessionAnalyticsPage, error) {
		filters = append(filters, filter)
		return page, nil
	}}
	service := newUsageService(t, reader)
	request := basequery.Request{
		Page: basequery.PageRequest{Limit: 1},
		Sort: []basequery.SortTerm{{Field: "totalTokens", Direction: basequery.SortDescending}},
		Filters: []basequery.FilterTerm{
			{Field: "projectId", Operator: basequery.FilterIn, Values: []string{"project-a", "project-b"}},
			{Field: "modelKey", Operator: basequery.FilterEqual, Values: []string{"model-a"}},
			{Field: "activity", Operator: basequery.FilterEqual, Values: []string{"active"}},
		},
		TimeRange: &basequery.LocalDateRange{
			StartDate: "1970-01-01", EndDateExclusive: "1970-01-02", TimeZone: "UTC",
		},
	}
	response, err := service.ListSessions(context.Background(), request)
	if err != nil {
		t.Fatalf("ListSessions() error = %v", err)
	}
	if len(filters) != 1 || filters[0].Limit != 1 ||
		filters[0].SortField != store.SessionAnalyticsSortTotalTokens ||
		filters[0].SortDirection != store.AnalyticsSortDescending ||
		!reflect.DeepEqual(filters[0].ProjectIDs, []string{"project-a", "project-b"}) ||
		!reflect.DeepEqual(filters[0].ModelKeys, []string{"model-a"}) ||
		filters[0].Activity == nil || *filters[0].Activity != store.SessionActivityActive ||
		filters[0].ReportingTimezone == nil || *filters[0].ReportingTimezone != "UTC" ||
		filters[0].LastActivityAtOrAfterMS == nil || *filters[0].LastActivityAtOrAfterMS != 0 ||
		filters[0].LastActivityBeforeMS == nil || *filters[0].LastActivityBeforeMS != 86_400_000 {
		t.Fatalf("reader filter = %#v", filters)
	}
	if response.Meta.Status != basequery.ResponseComplete || response.Meta.Page == nil ||
		!response.Meta.Page.HasMore || response.Meta.Page.NextCursor == nil || len(response.Items) != 1 ||
		response.MatchedCount.Value == nil || *response.MatchedCount.Value != 2 ||
		response.Items[0].SessionID != "session-alpha" || response.Items[0].Activity != "active" ||
		response.Items[0].Project.ID == nil || *response.Items[0].Project.ID != "project-a" ||
		response.Items[0].Model.ID == nil || *response.Items[0].Model.ID != "model-a" {
		t.Fatalf("ListSessions() response = %#v", response)
	}
	assertKnownNumeric(t, response.Items[0].Totals.TotalTokens, 30, basequery.NumericTokens)
	assertKnownNumeric(t, response.Items[0].Totals.EstimatedUSDMicros, 100, basequery.NumericMicroUSD)
	encoded, _ := json.Marshal(response)
	if strings.Contains(string(encoded), "private-session-generation") || strings.Contains(string(encoded), "cwd") {
		t.Fatalf("session response leaked private identity: %s", encoded)
	}

	request.Page.Cursor = response.Meta.Page.NextCursor
	if _, err := service.ListSessions(context.Background(), request); err != nil {
		t.Fatalf("ListSessions(cursor) error = %v", err)
	}
	if len(filters) != 2 || filters[1].Cursor == nil ||
		!reflect.DeepEqual(filters[1].Cursor, page.NextCursor) {
		t.Fatalf("decoded cursor = %#v, want %#v", filters, page.NextCursor)
	}
}

func TestListSessionsRejectsUnknownSortFilterAndTamperedOrCrossSortCursor(t *testing.T) {
	t.Parallel()

	calls := 0
	reader := &sessionReaderStub{list: func(
		context.Context, store.SessionAnalyticsFilter,
	) (store.SessionAnalyticsPage, error) {
		calls++
		return store.SessionAnalyticsPage{
			Mode: store.AnalyticsReadDetailFallback,
			Records: []store.SessionAnalyticsRecord{
				safeFallbackSessionRecord("session-a", "Session A"),
			},
			MatchedCount: 2,
			NextCursor:   &store.SessionAnalyticsCursor{SessionID: "session-a", Null: true},
		}, nil
	}}
	service := newUsageService(t, reader)
	invalid := []basequery.Request{
		{Page: basequery.PageRequest{Limit: 1}, Sort: []basequery.SortTerm{{Field: "raw_sql", Direction: basequery.SortDescending}}},
		{Page: basequery.PageRequest{Limit: 1}, Filters: []basequery.FilterTerm{{Field: "cwd", Operator: basequery.FilterEqual, Values: []string{"/private"}}}},
		{Page: basequery.PageRequest{Limit: 1}, Filters: []basequery.FilterTerm{{Field: "activity", Operator: basequery.FilterEqual, Values: []string{"running"}}}},
		{Page: basequery.PageRequest{Limit: 1}, Sort: []basequery.SortTerm{{Field: "lastActivityAt", Direction: basequery.SortDescending}, {Field: "totalTokens", Direction: basequery.SortDescending}}},
	}
	for _, request := range invalid {
		if _, err := service.ListSessions(context.Background(), request); !errors.Is(err, basequery.ErrValidation) {
			t.Fatalf("ListSessions(%#v) error = %v, want validation", request, err)
		}
	}
	if calls != 0 {
		t.Fatalf("invalid requests reached reader %d times", calls)
	}
	first, err := service.ListSessions(context.Background(), basequery.Request{Page: basequery.PageRequest{Limit: 1}})
	if err != nil {
		t.Fatalf("ListSessions(first) error = %v", err)
	}
	if first.Meta.Page == nil || first.Meta.Page.NextCursor == nil {
		t.Fatalf("first cursor = %#v", first)
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
		{Page: basequery.PageRequest{Limit: 1, Cursor: &tampered}},
		{Page: basequery.PageRequest{Limit: 1, Cursor: &validCursor}, Sort: []basequery.SortTerm{{Field: "estimatedCost", Direction: basequery.SortDescending}}},
	} {
		if _, err := service.ListSessions(context.Background(), request); !errors.Is(err, basequery.ErrValidation) {
			t.Fatalf("ListSessions(cursor=%#v) error = %v, want validation", request, err)
		}
	}
	if calls != 1 {
		t.Fatalf("invalid cursors reached reader; calls=%d", calls)
	}
}

func TestListSessionsMissingLedgerReturnsPartialUnknownTotals(t *testing.T) {
	t.Parallel()

	reader := &sessionReaderStub{list: func(
		context.Context, store.SessionAnalyticsFilter,
	) (store.SessionAnalyticsPage, error) {
		return store.SessionAnalyticsPage{
			Mode: store.AnalyticsReadDetailFallback,
			Records: []store.SessionAnalyticsRecord{
				safeFallbackSessionRecord("session-fallback", "Fallback safe title"),
			},
			MatchedCount: 1,
		}, nil
	}}
	service := newUsageService(t, reader)
	response, err := service.ListSessions(context.Background(), basequery.Request{})
	if err != nil {
		t.Fatalf("ListSessions(fallback) error = %v", err)
	}
	if response.Meta.Status != basequery.ResponsePartial || response.DegradedReason == nil ||
		*response.DegradedReason != DegradedRollupMissing || len(response.Items) != 1 {
		t.Fatalf("fallback response = %#v", response)
	}
	assertUnknownNumeric(t, response.Items[0].Totals.TurnCount, basequery.UnknownUnavailable)
	assertUnknownNumeric(t, response.Items[0].Totals.TotalTokens, basequery.UnknownUnavailable)
	assertUnknownNumeric(t, response.Items[0].Totals.EstimatedUSDMicros, basequery.UnknownUnavailable)
}

func TestListSessionsLightIndexReturnsKnownTokensAndUnknownTurnCost(t *testing.T) {
	t.Parallel()

	input, cached, output, reasoning, total := int64(100), int64(20), int64(10), int64(2), int64(112)
	record := safeFallbackSessionRecord("session-light", "真实标题")
	record.Rollup = &store.RollupTotals{
		InputTokens: &input, CachedInputTokens: &cached, OutputTokens: &output,
		ReasoningTokens: &reasoning, TotalTokens: &total,
	}
	reader := &sessionReaderStub{list: func(
		context.Context, store.SessionAnalyticsFilter,
	) (store.SessionAnalyticsPage, error) {
		return store.SessionAnalyticsPage{
			Mode: store.AnalyticsReadLightIndex, Records: []store.SessionAnalyticsRecord{record},
			MatchedCount: 1, MatchedTotals: record.Rollup, PageTotals: record.Rollup,
		}, nil
	}}
	service := newUsageService(t, reader)
	response, err := service.ListSessions(context.Background(), basequery.Request{})
	if err != nil {
		t.Fatalf("ListSessions(light) error = %v", err)
	}
	if response.Meta.Status != basequery.ResponsePartial || len(response.Items) != 1 {
		t.Fatalf("response = %#v", response)
	}
	assertKnownNumeric(t, response.Items[0].Totals.InputTokens, input, basequery.NumericTokens)
	assertKnownNumeric(t, response.Items[0].Totals.TotalTokens, total, basequery.NumericTokens)
	assertUnknownNumeric(t, response.Items[0].Totals.TurnCount, basequery.UnknownUnavailable)
	assertUnknownNumeric(t, response.Items[0].Totals.EstimatedUSDMicros, basequery.UnknownNotComputed)
}

func TestListSessionsLightIndexReturnsKnownSessionCostAndPricingEvidence(t *testing.T) {
	t.Parallel()

	input, cached, output, reasoning, total := int64(100), int64(20), int64(10), int64(2), int64(112)
	cost := int64(1_250_000)
	record := safeFallbackSessionRecord("session-light-priced", "已定价会话")
	record.Rollup = &store.RollupTotals{
		InputTokens: &input, CachedInputTokens: &cached, OutputTokens: &output,
		ReasoningTokens: &reasoning, TotalTokens: &total, EstimatedUSDMicros: &cost,
	}
	reader := &sessionReaderStub{list: func(
		context.Context, store.SessionAnalyticsFilter,
	) (store.SessionAnalyticsPage, error) {
		return store.SessionAnalyticsPage{
			Mode: store.AnalyticsReadLightIndex, PricingSource: "openai-api", Currency: "USD",
			Records: []store.SessionAnalyticsRecord{record}, MatchedCount: 1,
			MatchedTotals: record.Rollup, PageTotals: record.Rollup,
		}, nil
	}}
	service := newUsageService(t, reader)
	response, err := service.ListSessions(context.Background(), basequery.Request{})
	if err != nil {
		t.Fatalf("ListSessions(light priced) error = %v", err)
	}
	if response.Meta.Status != basequery.ResponsePartial || response.PricingSource == nil ||
		*response.PricingSource != "openai-api" || response.Currency == nil || *response.Currency != "USD" ||
		len(response.Items) != 1 {
		t.Fatalf("priced light response = %#v", response)
	}
	assertKnownNumeric(t, response.Items[0].Totals.EstimatedUSDMicros, cost, basequery.NumericMicroUSD)
}

func TestListSessionsAmbiguousLedgerReturnsSafePartial(t *testing.T) {
	t.Parallel()

	reader := &sessionReaderStub{list: func(
		context.Context, store.SessionAnalyticsFilter,
	) (store.SessionAnalyticsPage, error) {
		return store.SessionAnalyticsPage{
			Mode: store.AnalyticsReadAmbiguousFallback,
			Records: []store.SessionAnalyticsRecord{
				safeFallbackSessionRecord("session-ambiguous", "Ambiguous safe title"),
			},
			MatchedCount: 1,
		}, nil
	}}
	service := newUsageService(t, reader)
	response, err := service.ListSessions(context.Background(), basequery.Request{})
	if err != nil {
		t.Fatalf("ListSessions(ambiguous) error = %v", err)
	}
	if response.Meta.Status != basequery.ResponsePartial || response.DegradedReason == nil ||
		*response.DegradedReason != DegradedRollupAmbiguous || len(response.Items) != 1 {
		t.Fatalf("ambiguous response = %#v", response)
	}
	assertUnknownNumeric(t, response.Items[0].Totals.EstimatedUSDMicros, basequery.UnknownUnavailable)
}

func TestListSessionsMixedPricedAndUnpricedTurnsIsPartial(t *testing.T) {
	t.Parallel()

	zero, tokens, cost := int64(0), int64(30), int64(100)
	totals := store.RollupTotals{
		TurnCount: 2, InputTokens: &tokens, CachedInputTokens: &zero,
		OutputTokens: &zero, ReasoningTokens: &zero, TotalTokens: &tokens,
		EstimatedUSDMicros: &cost, PricedTurnCount: 1, UnpricedTurnCount: 1,
		FirstActivityAtMS: 1, LastActivityAtMS: 1, UpdatedAtMS: 1,
	}
	record := safeFallbackSessionRecord("session-mixed", "Mixed pricing")
	record.Rollup = &totals
	reader := &sessionReaderStub{list: func(
		context.Context, store.SessionAnalyticsFilter,
	) (store.SessionAnalyticsPage, error) {
		return store.SessionAnalyticsPage{
			Mode: store.AnalyticsReadActiveRollup,
			Generation: &store.CostRollupGeneration{
				GenerationID: "generation", ReportingTimezone: "UTC",
				PricingSource: "test", Currency: "USD", RollupVersion: 1,
			},
			Records: []store.SessionAnalyticsRecord{record}, MatchedCount: 1,
			MatchedTotals: &totals, PageTotals: &totals,
		}, nil
	}}
	service := newUsageService(t, reader)
	response, err := service.ListSessions(context.Background(), basequery.Request{})
	if err != nil {
		t.Fatalf("ListSessions(mixed pricing) error = %v", err)
	}
	if response.Meta.Status != basequery.ResponsePartial {
		t.Fatalf("ListSessions(mixed pricing) status = %q, want partial", response.Meta.Status)
	}
}

func TestSessionDetailRejectsInconsistentPricingEvidence(t *testing.T) {
	t.Parallel()

	zero, tokens, cost := int64(0), int64(30), int64(100)
	validSnapshot := func() store.SessionAnalyticsSnapshot {
		record := safeFallbackSessionRecord("session-evidence", "Evidence")
		record.Rollup = &store.RollupTotals{
			TurnCount: 1, InputTokens: &tokens, CachedInputTokens: &zero,
			OutputTokens: &zero, ReasoningTokens: &zero, TotalTokens: &tokens,
			EstimatedUSDMicros: &cost, PricedTurnCount: 1,
			FirstActivityAtMS: 1, LastActivityAtMS: 1, UpdatedAtMS: 1,
		}
		return store.SessionAnalyticsSnapshot{
			Mode: store.AnalyticsReadActiveRollup,
			Generation: &store.CostRollupGeneration{
				GenerationID: "generation", ReportingTimezone: "UTC",
				PricingSource: "test", Currency: "USD", RollupVersion: 1,
			},
			Record: record, Turns: make([]store.SessionTurnAnalyticsRecord, 0),
			PricingVersions: []string{"pricing-v1"},
			UnpricedReasons: make([]store.CostReasonCount, 0),
		}
	}
	tests := []struct {
		name   string
		mutate func(*store.SessionAnalyticsSnapshot)
	}{
		{name: "priced turns without pricing version", mutate: func(snapshot *store.SessionAnalyticsSnapshot) {
			snapshot.PricingVersions = make([]string, 0)
		}},
		{name: "unpriced reason count mismatch", mutate: func(snapshot *store.SessionAnalyticsSnapshot) {
			snapshot.Record.Rollup.PricedTurnCount = 0
			snapshot.Record.Rollup.UnpricedTurnCount = 1
			snapshot.Record.Rollup.EstimatedUSDMicros = nil
		}},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			snapshot := validSnapshot()
			test.mutate(&snapshot)
			service := newUsageService(t, &sessionReaderStub{detail: func(
				context.Context, store.SessionAnalyticsDetailFilter,
			) (store.SessionAnalyticsSnapshot, error) {
				return snapshot, nil
			}})
			_, err := service.SessionDetail(context.Background(), SessionDetailRequest{
				SessionID: "session-evidence", ReportingTimezone: pointerToString("UTC"),
			})
			if !errors.Is(err, basequery.ErrUnavailable) {
				t.Fatalf("SessionDetail() error = %v, want unavailable", err)
			}
		})
	}
}

func TestListSessionsRejectsInvalidStoredAttributionAndCursorShape(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		page store.SessionAnalyticsPage
	}{
		{
			name: "invalid attribution enum",
			page: store.SessionAnalyticsPage{
				Mode: store.AnalyticsReadDetailFallback,
				Records: []store.SessionAnalyticsRecord{{
					SessionID: "session-invalid", DisplayTitle: "Invalid",
					Activity: store.SessionActivityIdle,
				}},
				MatchedCount: 1,
			},
		},
		{
			name: "cursor does not describe last row",
			page: store.SessionAnalyticsPage{
				Mode: store.AnalyticsReadDetailFallback,
				Records: []store.SessionAnalyticsRecord{
					safeFallbackSessionRecord("session-last", "Last"),
				},
				MatchedCount: 2,
				NextCursor:   &store.SessionAnalyticsCursor{SessionID: "session-other", Null: true},
			},
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			service := newUsageService(t, &sessionReaderStub{list: func(
				context.Context, store.SessionAnalyticsFilter,
			) (store.SessionAnalyticsPage, error) {
				return test.page, nil
			}})
			_, err := service.ListSessions(context.Background(), basequery.Request{
				Page: basequery.PageRequest{Limit: 1},
			})
			if !errors.Is(err, basequery.ErrUnavailable) {
				t.Fatalf("ListSessions() error = %v, want unavailable", err)
			}
		})
	}
}

func TestSessionDetailMapsNotFoundAndContentFreeFailure(t *testing.T) {
	t.Parallel()

	privateCause := errors.New("private-session-driver-cause")
	reader := &sessionReaderStub{detail: func(
		_ context.Context, filter store.SessionAnalyticsDetailFilter,
	) (store.SessionAnalyticsSnapshot, error) {
		switch filter.SessionID {
		case "missing":
			return store.SessionAnalyticsSnapshot{}, store.ErrNotFound
		case "driver-failure":
			return store.SessionAnalyticsSnapshot{}, privateCause
		default:
			return store.SessionAnalyticsSnapshot{
				Mode:            store.AnalyticsReadDetailFallback,
				Record:          safeFallbackSessionRecord(filter.SessionID, "Safe detail"),
				Turns:           make([]store.SessionTurnAnalyticsRecord, 0),
				PricingVersions: make([]string, 0), UnpricedReasons: make([]store.CostReasonCount, 0),
			}, nil
		}
	}}
	service := newUsageService(t, reader)
	detail, err := service.SessionDetail(context.Background(), SessionDetailRequest{
		SessionID: "session-safe", ReportingTimezone: pointerToString("UTC"),
	})
	if err != nil || detail.Item.SessionID != "session-safe" ||
		detail.Meta.Status != basequery.ResponsePartial {
		t.Fatalf("SessionDetail() = %#v, %v", detail, err)
	}
	if _, err := service.SessionDetail(context.Background(), SessionDetailRequest{SessionID: "missing"}); !errors.Is(err, basequery.ErrNotFound) {
		t.Fatalf("SessionDetail(missing) error = %v", err)
	}
	_, err = service.SessionDetail(context.Background(), SessionDetailRequest{SessionID: "driver-failure"})
	if !errors.Is(err, basequery.ErrUnavailable) || strings.Contains(err.Error(), privateCause.Error()) {
		t.Fatalf("SessionDetail(driver) error = %q", err)
	}
	if _, err := service.SessionDetail(context.Background(), SessionDetailRequest{SessionID: ""}); !errors.Is(err, basequery.ErrValidation) {
		t.Fatalf("SessionDetail(empty) error = %v", err)
	}
}

func TestSessionDetailExposesBoundedTurnRequestResponseContract(t *testing.T) {
	t.Parallel()

	request := reflect.TypeOf(SessionDetailRequest{})
	if _, found := request.FieldByName("TurnPage"); !found {
		t.Fatal("SessionDetailRequest missing TurnPage")
	}
	response := reflect.TypeOf(SessionDetailResponse{})
	for _, field := range []string{"TurnPage", "Turns"} {
		if _, found := response.FieldByName(field); !found {
			t.Fatalf("SessionDetailResponse missing %s", field)
		}
	}
}

func TestSessionDetailMapsBoundedTurnPageAndRoundTripsOpaqueCursor(t *testing.T) {
	t.Parallel()

	zero, input, cost := int64(0), int64(30), int64(100)
	completedAt := int64(120)
	turn := store.SessionTurnAnalyticsRecord{
		TurnID: "private-turn-id", StartedAtMS: 100, CompletedAtMS: &completedAt,
		Model: store.ModelAttribution{
			ModelKey: pointerToString("model-safe"), DisplayName: pointerToString("Model Safe"),
			Confidence: store.AttributionConfidenceHigh,
			Source:     store.AttributionSourceModelCanonical, Reason: store.AttributionReasonObserved,
		},
		Usage: &store.SessionTurnUsageAnalytics{
			ObservedAtMS: 120, IsFinal: true, InputTokens: &input,
			CachedInputTokens: &zero, OutputTokens: &zero, ReasoningTokens: &zero,
		},
		Cost: &store.SessionTurnCostAnalytics{
			PricingVersion: pointerToString("pricing-v1"), EstimatedUSDMicros: &cost,
			Status: pricing.CostStatusPriced, Reason: pricing.CostReasonPriced,
		},
	}
	record := safeFallbackSessionRecord("session-safe", "Safe detail")
	record.Rollup = &store.RollupTotals{
		TurnCount: 1, InputTokens: &input, CachedInputTokens: &zero,
		OutputTokens: &zero, ReasoningTokens: &zero, TotalTokens: &input,
		EstimatedUSDMicros: &cost, PricedTurnCount: 1,
		FirstActivityAtMS: 120, LastActivityAtMS: 120, UpdatedAtMS: 120,
	}
	var filters []store.SessionAnalyticsDetailFilter
	reader := &sessionReaderStub{detail: func(
		_ context.Context,
		filter store.SessionAnalyticsDetailFilter,
	) (store.SessionAnalyticsSnapshot, error) {
		filters = append(filters, filter)
		return store.SessionAnalyticsSnapshot{
			Mode: store.AnalyticsReadActiveRollup,
			Generation: &store.CostRollupGeneration{
				GenerationID: "generation-v1", ReportingTimezone: "UTC",
				PricingSource: "test", Currency: "USD", RollupVersion: 1,
			},
			ReportingTimezone: "UTC",
			Record:            record,
			Daily: []store.UsageDaily{{
				GenerationID: "generation-v1", BucketStartMS: 0, ReportingTimezone: "UTC",
				RollupTotals: *record.Rollup,
			}},
			Turns: []store.SessionTurnAnalyticsRecord{turn},
			NextTurnCursor: &store.SessionTurnAnalyticsCursor{
				SessionID: filter.SessionID, TurnID: turn.TurnID, StartedAtMS: turn.StartedAtMS,
			},
			PricingVersions: []string{"pricing-v1"},
			UnpricedReasons: make([]store.CostReasonCount, 0),
		}, nil
	}}
	service := newUsageService(t, reader)
	first, err := service.SessionDetail(context.Background(), SessionDetailRequest{
		SessionID: "session-safe", ReportingTimezone: pointerToString("UTC"),
		TurnPage: basequery.PageRequest{Limit: 1},
	})
	if err != nil {
		t.Fatalf("SessionDetail(first turn page) error = %v", err)
	}
	if len(filters) != 1 || filters[0].TurnLimit != 1 || filters[0].TurnCursor != nil ||
		len(first.Turns) != 1 || !first.TurnPage.HasMore || first.TurnPage.NextCursor == nil {
		t.Fatalf("first SessionDetail() = %#v, filters=%#v", first, filters)
	}
	if first.ReportingTimeZone != "UTC" || first.TrendGranularity != TrendHour ||
		len(first.Trend) != 1 || first.Trend[0].Key != "1970-01-01T00:00+00:00" {
		t.Fatalf("first SessionDetail() trend = %#v", first)
	}
	assertKnownNumeric(t, first.Trend[0].Totals.TotalTokens, 30, basequery.NumericTokens)
	mapped := first.Turns[0]
	if mapped.TimelineKey == "" || strings.Contains(mapped.TimelineKey, turn.TurnID) ||
		mapped.State != SessionTurnComplete || mapped.Model.DisplayName == nil ||
		*mapped.Model.DisplayName != "Model Safe" || mapped.PricingStatus != SessionTurnPricingPriced ||
		mapped.PricingVersion == nil || *mapped.PricingVersion != "pricing-v1" {
		t.Fatalf("mapped turn = %#v", mapped)
	}
	assertKnownNumeric(t, mapped.Totals.TurnCount, 1, basequery.NumericCount)
	assertKnownNumeric(t, mapped.Totals.TotalTokens, 30, basequery.NumericTokens)
	assertKnownNumeric(t, mapped.Totals.EstimatedUSDMicros, 100, basequery.NumericMicroUSD)
	encoded, err := json.Marshal(first)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	if strings.Contains(string(encoded), turn.TurnID) {
		t.Fatalf("SessionDetail leaked raw turn identity: %s", encoded)
	}

	_, err = service.SessionDetail(context.Background(), SessionDetailRequest{
		SessionID: "session-safe", ReportingTimezone: pointerToString("UTC"),
		TurnPage: basequery.PageRequest{Limit: 1, Cursor: first.TurnPage.NextCursor},
	})
	if err != nil {
		t.Fatalf("SessionDetail(second turn page) error = %v", err)
	}
	if len(filters) != 2 || filters[1].TurnCursor == nil ||
		filters[1].TurnCursor.SessionID != "session-safe" ||
		filters[1].TurnCursor.TurnID != turn.TurnID ||
		filters[1].TurnCursor.StartedAtMS != turn.StartedAtMS {
		t.Fatalf("decoded turn cursor filters = %#v", filters)
	}
}

// 测试 Session detail 在全部用量位于同一本地自然日时返回小时趋势。
func TestSessionDetailUsesHourlyTrendWithinOneReportingDate(t *testing.T) {
	t.Parallel()

	firstTokens, secondTokens := int64(10), int64(20)
	zero := int64(0)
	record := safeFallbackSessionRecord("session-hourly", "Hourly trend")
	record.Rollup = &store.RollupTotals{
		InputTokens: &firstTokens, CachedInputTokens: &zero, OutputTokens: &secondTokens,
		ReasoningTokens: &zero, TotalTokens: int64Ptr(firstTokens + secondTokens),
	}
	reader := &sessionReaderStub{detail: func(
		context.Context,
		store.SessionAnalyticsDetailFilter,
	) (store.SessionAnalyticsSnapshot, error) {
		return store.SessionAnalyticsSnapshot{
			Mode:              store.AnalyticsReadLightIndex,
			ReportingTimezone: "Asia/Shanghai",
			Record:            record,
			Daily: []store.UsageDaily{
				{
					BucketStartMS:     mustUsageTime(t, "2026-07-27T01:00:00Z"),
					ReportingTimezone: "Asia/Shanghai",
					RollupTotals: store.RollupTotals{
						InputTokens: &firstTokens, CachedInputTokens: &zero,
						OutputTokens: &zero, ReasoningTokens: &zero, TotalTokens: &firstTokens,
					},
				},
				{
					BucketStartMS:     mustUsageTime(t, "2026-07-27T02:00:00Z"),
					ReportingTimezone: "Asia/Shanghai",
					RollupTotals: store.RollupTotals{
						InputTokens: &zero, CachedInputTokens: &zero,
						OutputTokens: &secondTokens, ReasoningTokens: &zero, TotalTokens: &secondTokens,
					},
				},
			},
			Turns:           make([]store.SessionTurnAnalyticsRecord, 0),
			PricingVersions: make([]string, 0),
			UnpricedReasons: make([]store.CostReasonCount, 0),
		}, nil
	}}

	response, err := newUsageService(t, reader).SessionDetail(
		context.Background(),
		SessionDetailRequest{
			SessionID: "session-hourly", ReportingTimezone: pointerToString("Asia/Shanghai"),
		},
	)
	if err != nil {
		t.Fatalf("SessionDetail(hourly trend) error = %v", err)
	}
	if len(response.Trend) != 2 {
		t.Fatalf("SessionDetail(hourly trend) points = %#v", response.Trend)
	}
	if got := []string{response.Trend[0].Key, response.Trend[1].Key}; !reflect.DeepEqual(
		got,
		[]string{"2026-07-27T09:00+08:00", "2026-07-27T10:00+08:00"},
	) {
		t.Fatalf("SessionDetail(hourly trend) keys = %#v", got)
	}
	encoded, err := json.Marshal(response)
	if err != nil {
		t.Fatalf("json.Marshal(hourly trend) error = %v", err)
	}
	if !strings.Contains(string(encoded), `"trendGranularity":"hour"`) ||
		!strings.Contains(string(encoded), `"trend":[`) ||
		strings.Contains(string(encoded), `"daily"`) {
		t.Fatalf("SessionDetail(hourly trend) contract = %s", encoded)
	}
}

// 测试 Session detail 的用量跨越请求时区自然日时保持每日趋势。
func TestSessionDetailUsesDailyTrendAcrossReportingDates(t *testing.T) {
	t.Parallel()

	firstTokens, secondTokens := int64(10), int64(20)
	zero := int64(0)
	record := safeFallbackSessionRecord("session-daily", "Daily trend")
	record.Rollup = &store.RollupTotals{
		InputTokens: &firstTokens, CachedInputTokens: &zero, OutputTokens: &secondTokens,
		ReasoningTokens: &zero, TotalTokens: int64Ptr(firstTokens + secondTokens),
	}
	reader := &sessionReaderStub{detail: func(
		context.Context,
		store.SessionAnalyticsDetailFilter,
	) (store.SessionAnalyticsSnapshot, error) {
		return store.SessionAnalyticsSnapshot{
			Mode:              store.AnalyticsReadLightIndex,
			ReportingTimezone: "Asia/Shanghai",
			Record:            record,
			Daily: []store.UsageDaily{
				{
					BucketStartMS:     mustUsageTime(t, "2026-07-26T15:00:00Z"),
					ReportingTimezone: "Asia/Shanghai",
					RollupTotals: store.RollupTotals{
						InputTokens: &firstTokens, CachedInputTokens: &zero,
						OutputTokens: &zero, ReasoningTokens: &zero, TotalTokens: &firstTokens,
					},
				},
				{
					BucketStartMS:     mustUsageTime(t, "2026-07-26T16:00:00Z"),
					ReportingTimezone: "Asia/Shanghai",
					RollupTotals: store.RollupTotals{
						InputTokens: &zero, CachedInputTokens: &zero,
						OutputTokens: &secondTokens, ReasoningTokens: &zero, TotalTokens: &secondTokens,
					},
				},
			},
			Turns:           make([]store.SessionTurnAnalyticsRecord, 0),
			PricingVersions: make([]string, 0),
			UnpricedReasons: make([]store.CostReasonCount, 0),
		}, nil
	}}

	response, err := newUsageService(t, reader).SessionDetail(
		context.Background(),
		SessionDetailRequest{
			SessionID: "session-daily", ReportingTimezone: pointerToString("Asia/Shanghai"),
		},
	)
	if err != nil {
		t.Fatalf("SessionDetail(daily trend) error = %v", err)
	}
	if len(response.Trend) != 2 {
		t.Fatalf("SessionDetail(daily trend) points = %#v", response.Trend)
	}
	if got := []string{response.Trend[0].Key, response.Trend[1].Key}; !reflect.DeepEqual(
		got,
		[]string{"2026-07-26", "2026-07-27"},
	) {
		t.Fatalf("SessionDetail(daily trend) keys = %#v", got)
	}
	encoded, err := json.Marshal(response)
	if err != nil {
		t.Fatalf("json.Marshal(daily trend) error = %v", err)
	}
	if !strings.Contains(string(encoded), `"trendGranularity":"day"`) ||
		!strings.Contains(string(encoded), `"trend":[`) ||
		strings.Contains(string(encoded), `"daily"`) {
		t.Fatalf("SessionDetail(daily trend) contract = %s", encoded)
	}
}

func TestSessionDetailRejectsFallbackTrendBypassAndUsesUnavailableGranularity(t *testing.T) {
	t.Parallel()

	zero := int64(0)
	record := safeFallbackSessionRecord("session-fallback-trend", "Fallback trend")
	snapshot := store.SessionAnalyticsSnapshot{
		Mode:              store.AnalyticsReadDetailFallback,
		ReportingTimezone: "UTC",
		Record:            record,
		Daily: []store.UsageDaily{{
			BucketStartMS:     0,
			ReportingTimezone: "UTC",
			RollupTotals: store.RollupTotals{
				InputTokens: &zero, CachedInputTokens: &zero,
				OutputTokens: &zero, ReasoningTokens: &zero, TotalTokens: &zero,
			},
		}},
		Turns:           make([]store.SessionTurnAnalyticsRecord, 0),
		PricingVersions: make([]string, 0),
		UnpricedReasons: make([]store.CostReasonCount, 0),
	}
	reader := &sessionReaderStub{detail: func(
		context.Context,
		store.SessionAnalyticsDetailFilter,
	) (store.SessionAnalyticsSnapshot, error) {
		return snapshot, nil
	}}
	service := newUsageService(t, reader)
	_, err := service.SessionDetail(context.Background(), SessionDetailRequest{
		SessionID: "session-fallback-trend",
	})
	if !errors.Is(err, basequery.ErrUnavailable) {
		t.Fatalf("SessionDetail(fallback trend bypass) error = %v, want unavailable", err)
	}

	snapshot.Daily = make([]store.UsageDaily, 0)
	response, err := service.SessionDetail(context.Background(), SessionDetailRequest{
		SessionID: "session-fallback-trend",
	})
	if err != nil {
		t.Fatalf("SessionDetail(empty fallback trend) error = %v", err)
	}
	if response.TrendGranularity != "" || len(response.Trend) != 0 ||
		response.DegradedReason == nil ||
		*response.DegradedReason != DegradedRollupMissing {
		t.Fatalf("SessionDetail(empty fallback trend) = %#v", response)
	}
}

func TestSessionDetailStoreToQueryFallbackDoesNotExposeAggregateTrend(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	dataDirectory := filepath.Join(t.TempDir(), "private")
	if err := os.Mkdir(dataDirectory, 0o700); err != nil {
		t.Fatalf("os.Mkdir(private data directory) error = %v", err)
	}
	database, err := storesqlite.Open(ctx, storesqlite.Config{
		Path: filepath.Join(dataDirectory, "session-fallback.db"),
	})
	if err != nil {
		t.Fatalf("sqlite.Open() error = %v", err)
	}
	t.Cleanup(func() {
		if closeErr := database.Close(context.Background()); closeErr != nil {
			t.Errorf("sqlite.Close() error = %v", closeErr)
		}
	})
	repository := store.NewRepository(database)
	if err := repository.EnsureApplicationSchema(ctx); err != nil {
		t.Fatalf("EnsureApplicationSchema() error = %v", err)
	}
	zero, input := int64(0), int64(42)
	completedAtMS := int64(120)
	if err := repository.UpsertFacts(ctx, store.FactBatch{
		Session: &store.Session{
			SessionID: "session-real-fallback", Provider: "codex", SourceKind: "session",
			CreatedAtMS: 90, FirstSeenAtMS: 90, LastSeenAtMS: completedAtMS,
		},
		Turn: &store.Turn{
			TurnID: "turn-real-fallback", SessionID: "session-real-fallback",
			StartedAtMS: 100, CompletedAtMS: &completedAtMS, Outcome: pointerToString("completed"),
			Model: pointerToString("gpt-5.2-codex"), SourceGeneration: 0,
			StartOffset: 10, CompleteOffset: int64Ptr(20),
		},
		Usage: &store.TurnUsage{
			TurnID: "turn-real-fallback", ObservedAtMS: completedAtMS, IsFinal: true,
			InputTokens: &input, CachedInputTokens: &zero,
			OutputTokens: &zero, ReasoningTokens: &zero,
			SourceGeneration: 0, SourceOffset: 20,
			Confidence: "exact", UpdatedAtMS: completedAtMS,
		},
	}); err != nil {
		t.Fatalf("UpsertFacts() error = %v", err)
	}

	response, err := newUsageService(t, repository).SessionDetail(
		ctx,
		SessionDetailRequest{
			SessionID: "session-real-fallback",
			TurnPage:  basequery.PageRequest{Limit: 20},
		},
	)
	if err != nil {
		t.Fatalf("SessionDetail(Store→Query fallback) error = %v", err)
	}
	if response.Meta.Status != basequery.ResponsePartial ||
		response.DegradedReason == nil ||
		*response.DegradedReason != DegradedRollupMissing ||
		response.TrendGranularity != "" ||
		len(response.Trend) != 0 {
		t.Fatalf("SessionDetail(Store→Query fallback) = %#v", response)
	}
	for _, value := range []basequery.NumericValue{
		response.Item.Totals.InputTokens,
		response.Item.Totals.CachedInputTokens,
		response.Item.Totals.OutputTokens,
		response.Item.Totals.ReasoningTokens,
		response.Item.Totals.TotalTokens,
		response.Item.Totals.EstimatedUSDMicros,
	} {
		assertUnknownNumeric(t, value, basequery.UnknownUnavailable)
	}
}

func TestMapSessionTrendDistinguishesRepeatedDSTHour(t *testing.T) {
	t.Parallel()

	zero, firstTokens, secondTokens := int64(0), int64(10), int64(20)
	firstStart := mustUsageTime(t, "2026-11-01T05:00:00Z")
	secondStart := mustUsageTime(t, "2026-11-01T06:00:00Z")
	snapshot := store.SessionAnalyticsSnapshot{
		Mode:              store.AnalyticsReadLightIndex,
		ReportingTimezone: "America/New_York",
		Daily: []store.UsageDaily{
			{
				BucketStartMS:     firstStart,
				ReportingTimezone: "America/New_York",
				RollupTotals: store.RollupTotals{
					InputTokens: &firstTokens, CachedInputTokens: &zero,
					OutputTokens: &zero, ReasoningTokens: &zero, TotalTokens: &firstTokens,
				},
			},
			{
				BucketStartMS:     secondStart,
				ReportingTimezone: "America/New_York",
				RollupTotals: store.RollupTotals{
					InputTokens: &zero, CachedInputTokens: &zero,
					OutputTokens: &secondTokens, ReasoningTokens: &zero, TotalTokens: &secondTokens,
				},
			},
		},
	}
	granularity, points, err := mapSessionTrend(snapshot)
	if err != nil {
		t.Fatalf("mapSessionTrend(DST fallback hour) error = %v", err)
	}
	if granularity != TrendHour || len(points) != 2 {
		t.Fatalf("mapSessionTrend(DST fallback hour) = %q, %#v", granularity, points)
	}
	gotKeys := []string{points[0].Key, points[1].Key}
	wantKeys := []string{
		"2026-11-01T01:00-04:00",
		"2026-11-01T01:00-05:00",
	}
	if !reflect.DeepEqual(gotKeys, wantKeys) {
		t.Fatalf("mapSessionTrend(DST fallback hour) keys = %#v, want %#v", gotKeys, wantKeys)
	}
	wantBounds := [][2]int64{
		{firstStart, mustUsageTime(t, "2026-11-01T06:00:00Z")},
		{secondStart, mustUsageTime(t, "2026-11-01T07:00:00Z")},
	}
	for index, point := range points {
		if point.StartAtMS.Value == nil || point.EndAtMS.Value == nil ||
			*point.StartAtMS.Value != wantBounds[index][0] ||
			*point.EndAtMS.Value != wantBounds[index][1] {
			t.Fatalf("mapSessionTrend(DST fallback hour) point[%d] = %#v", index, point)
		}
	}
}

func TestSessionDetailStoreToQueryPreservesRepeatedDSTHour(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	dataDirectory := filepath.Join(t.TempDir(), "private")
	if err := os.Mkdir(dataDirectory, 0o700); err != nil {
		t.Fatalf("os.Mkdir(private data directory) error = %v", err)
	}
	database, err := storesqlite.Open(ctx, storesqlite.Config{
		Path: filepath.Join(dataDirectory, "session-dst.db"),
	})
	if err != nil {
		t.Fatalf("sqlite.Open() error = %v", err)
	}
	t.Cleanup(func() {
		if closeErr := database.Close(context.Background()); closeErr != nil {
			t.Errorf("sqlite.Close() error = %v", closeErr)
		}
	})
	repository := store.NewRepository(database)
	if err := repository.EnsureApplicationSchema(ctx); err != nil {
		t.Fatalf("EnsureApplicationSchema() error = %v", err)
	}
	home := store.LightHomeIdentity{Path: "/confirmed-home", DeviceID: "1", Inode: 2}
	rolloutPath := "/confirmed-home/sessions/repeated-hour.jsonl"
	if err := repository.ReplaceLightMetadata(ctx, store.LightMetadataSnapshot{
		Home: home, Generation: 1, ReadyAtMS: 1_000,
		Sessions: []store.LightSessionMetadata{{
			SessionID: "session-dst-real", CWD: "/workspace",
			RolloutPath: &rolloutPath, CreatedAtMS: 100, UpdatedAtMS: 200,
		}},
	}); err != nil {
		t.Fatalf("ReplaceLightMetadata() error = %v", err)
	}
	identity := store.LightRolloutIdentity{
		Path: rolloutPath, SourceFileID: "source-dst-real", Home: home,
		DeviceID: "2", Inode: 3, SizeBytes: 8_192, MTimeNS: 1,
		PrefixBytes:       4_096,
		PrefixSHA256:      strings.Repeat("a", 64),
		FingerprintSHA256: strings.Repeat("b", 64),
	}
	generation, err := repository.StartLightTokenRebuild(
		ctx,
		"session-dst-real",
		identity,
		"parser-v1",
		2_000,
	)
	if err != nil {
		t.Fatalf("StartLightTokenRebuild() error = %v", err)
	}
	firstStart := mustUsageTime(t, "2026-11-01T05:00:00Z")
	secondStart := mustUsageTime(t, "2026-11-01T06:00:00Z")
	if err := repository.CommitLightTokenBatch(ctx, store.LightTokenBatch{
		SessionID: "session-dst-real", Generation: generation,
		UpdatedAtMS: 2_100, Activate: true,
		Checkpoint: store.LightTokenCheckpoint{
			DurableOffset: identity.SizeBytes, Complete: true,
			InputTokens: 10, OutputTokens: 20,
		},
		TimedDeltas: []store.LightTokenTimedDelta{
			{
				SourceOffset: 4_000,
				ObservedAtMS: mustUsageTime(t, "2026-11-01T05:30:00Z"),
				InputTokens:  10,
			},
			{
				SourceOffset: 5_000,
				ObservedAtMS: mustUsageTime(t, "2026-11-01T06:45:00Z"),
				OutputTokens: 20,
			},
		},
	}); err != nil {
		t.Fatalf("CommitLightTokenBatch() error = %v", err)
	}

	response, err := newUsageService(t, repository).SessionDetail(
		ctx,
		SessionDetailRequest{
			SessionID:         "session-dst-real",
			ReportingTimezone: pointerToString("America/New_York"),
			TurnPage:          basequery.PageRequest{Limit: 20},
		},
	)
	if err != nil {
		t.Fatalf("SessionDetail(Store→Query DST) error = %v", err)
	}
	if response.TrendGranularity != TrendHour || len(response.Trend) != 2 {
		t.Fatalf("SessionDetail(Store→Query DST) = %#v", response)
	}
	wantKeys := []string{
		"2026-11-01T01:00-04:00",
		"2026-11-01T01:00-05:00",
	}
	gotKeys := []string{response.Trend[0].Key, response.Trend[1].Key}
	if !reflect.DeepEqual(gotKeys, wantKeys) {
		t.Fatalf("SessionDetail(Store→Query DST) keys = %#v, want %#v", gotKeys, wantKeys)
	}
	wantBounds := [][2]int64{
		{firstStart, secondStart},
		{secondStart, mustUsageTime(t, "2026-11-01T07:00:00Z")},
	}
	for index, point := range response.Trend {
		if point.StartAtMS.Value == nil || point.EndAtMS.Value == nil ||
			*point.StartAtMS.Value != wantBounds[index][0] ||
			*point.EndAtMS.Value != wantBounds[index][1] {
			t.Fatalf("SessionDetail(Store→Query DST) point[%d] = %#v", index, point)
		}
	}
}

func TestSessionTurnCursorHidesIdentityAndRejectsClientResigning(t *testing.T) {
	t.Parallel()

	cursor := &store.SessionTurnAnalyticsCursor{
		SessionID: "private-session-id", TurnID: "private-turn-id", StartedAtMS: 100,
	}
	key := testSessionTurnCursorKey()
	encoded, err := encodeSessionTurnCursor(key, cursor)
	if err != nil || encoded == nil {
		t.Fatalf("encodeSessionTurnCursor() = %v, %v", encoded, err)
	}
	wire, err := base64.RawURLEncoding.DecodeString(*encoded)
	if err != nil {
		t.Fatalf("decode cursor wire: %v", err)
	}
	for _, marker := range [][]byte{[]byte(cursor.SessionID), []byte(cursor.TurnID)} {
		if bytes.Contains(wire, marker) {
			t.Fatalf("cursor wire leaked raw identity %q: %s", marker, wire)
		}
	}

	startedAt := int64(99)
	sessionID := cursor.SessionID
	resigned, err := encodeCursorUnsigned(cursorUnsigned{
		Version: basequery.ContractVersion, Endpoint: sessionTurnCursorEndpoint,
		SortField: "startedAt", Direction: basequery.SortDescending,
		Value: &startedAt, TextValue: &sessionID, Identity: "attacker-selected-turn",
	})
	if err != nil || resigned == nil {
		t.Fatalf("encode attacker cursor = %v, %v", resigned, err)
	}
	if _, err := decodeSessionTurnCursor(key, *resigned, sessionID); !errors.Is(err, basequery.ErrValidation) {
		t.Fatalf("decodeSessionTurnCursor(resigned) error = %v, want validation", err)
	}
}

func TestSessionDetailRejectsTurnPageAggregateDrift(t *testing.T) {
	t.Parallel()

	zero, input, cost := int64(0), int64(30), int64(100)
	completedAt := int64(120)
	turn := store.SessionTurnAnalyticsRecord{
		TurnID: "turn-drift", StartedAtMS: 100, CompletedAtMS: &completedAt,
		Model: store.ModelAttribution{
			Confidence: store.AttributionConfidenceUnknown,
			Source:     store.AttributionSourceMissing, Reason: store.AttributionReasonMissing,
		},
		Usage: &store.SessionTurnUsageAnalytics{
			ObservedAtMS: 120, IsFinal: true, InputTokens: &input,
			CachedInputTokens: &zero, OutputTokens: &zero, ReasoningTokens: &zero,
		},
		Cost: &store.SessionTurnCostAnalytics{
			PricingVersion: pointerToString("pricing-v1"), EstimatedUSDMicros: &cost,
			Status: pricing.CostStatusPriced, Reason: pricing.CostReasonPriced,
		},
	}
	for name, mutate := range map[string]func(*store.SessionAnalyticsSnapshot){
		"complete first page exact drift": func(storeSnapshot *store.SessionAnalyticsSnapshot) {},
		"truncated page lower bound drift": func(storeSnapshot *store.SessionAnalyticsSnapshot) {
			storeSnapshot.NextTurnCursor = &store.SessionTurnAnalyticsCursor{
				SessionID: storeSnapshot.Record.SessionID,
				TurnID:    turn.TurnID, StartedAtMS: turn.StartedAtMS,
			}
		},
		"page pricing version absent from aggregate evidence": func(storeSnapshot *store.SessionAnalyticsSnapshot) {
			storeSnapshot.Record.Rollup = &store.RollupTotals{
				TurnCount: 1, InputTokens: &input, CachedInputTokens: &zero,
				OutputTokens: &zero, ReasoningTokens: &zero, TotalTokens: &input,
				EstimatedUSDMicros: &cost, PricedTurnCount: 1,
				FirstActivityAtMS: 120, LastActivityAtMS: 120, UpdatedAtMS: 120,
			}
			storeSnapshot.PricingVersions = []string{"pricing-other"}
		},
		"complete page missing aggregate pricing version": func(storeSnapshot *store.SessionAnalyticsSnapshot) {
			storeSnapshot.Record.Rollup = &store.RollupTotals{
				TurnCount: 1, InputTokens: &input, CachedInputTokens: &zero,
				OutputTokens: &zero, ReasoningTokens: &zero, TotalTokens: &input,
				EstimatedUSDMicros: &cost, PricedTurnCount: 1,
				FirstActivityAtMS: 120, LastActivityAtMS: 120, UpdatedAtMS: 120,
			}
			storeSnapshot.PricingVersions = []string{"pricing-v1", "ghost-version"}
		},
	} {
		t.Run(name, func(t *testing.T) {
			record := safeFallbackSessionRecord("session-drift", "Safe detail")
			record.Rollup = &store.RollupTotals{
				InputTokens: &zero, CachedInputTokens: &zero, OutputTokens: &zero,
				ReasoningTokens: &zero, TotalTokens: &zero, EstimatedUSDMicros: &zero,
			}
			snapshot := store.SessionAnalyticsSnapshot{
				Mode: store.AnalyticsReadActiveRollup,
				Generation: &store.CostRollupGeneration{
					GenerationID: "generation", ReportingTimezone: "UTC",
					PricingSource: "test", Currency: "USD", RollupVersion: 1,
				},
				Record: record, Turns: []store.SessionTurnAnalyticsRecord{turn},
				PricingVersions: []string{"pricing-v1"},
				UnpricedReasons: make([]store.CostReasonCount, 0),
			}
			mutate(&snapshot)
			service := newUsageService(t, &sessionReaderStub{detail: func(
				context.Context,
				store.SessionAnalyticsDetailFilter,
			) (store.SessionAnalyticsSnapshot, error) {
				return snapshot, nil
			}})
			_, err := service.SessionDetail(context.Background(), SessionDetailRequest{
				SessionID: "session-drift", TurnPage: basequery.PageRequest{Limit: 1},
			})
			if !errors.Is(err, basequery.ErrUnavailable) {
				t.Fatalf("SessionDetail(turn aggregate drift) error = %v, want unavailable", err)
			}
		})
	}
}

func TestSessionDetailRejectsTurnUsageObservedBeforeStart(t *testing.T) {
	t.Parallel()

	zero := int64(0)
	snapshot := store.SessionAnalyticsSnapshot{
		Mode:   store.AnalyticsReadDetailFallback,
		Record: safeFallbackSessionRecord("session-invalid-turn", "Safe detail"),
		Turns: []store.SessionTurnAnalyticsRecord{{
			TurnID: "turn-invalid", StartedAtMS: 100,
			Model: store.ModelAttribution{
				Confidence: store.AttributionConfidenceUnknown,
				Source:     store.AttributionSourceMissing, Reason: store.AttributionReasonMissing,
			},
			Usage: &store.SessionTurnUsageAnalytics{
				ObservedAtMS: 99, InputTokens: &zero, CachedInputTokens: &zero,
				OutputTokens: &zero, ReasoningTokens: &zero,
			},
		}},
		PricingVersions: make([]string, 0),
		UnpricedReasons: make([]store.CostReasonCount, 0),
	}
	service := newUsageService(t, &sessionReaderStub{detail: func(
		context.Context,
		store.SessionAnalyticsDetailFilter,
	) (store.SessionAnalyticsSnapshot, error) {
		return snapshot, nil
	}})
	_, err := service.SessionDetail(context.Background(), SessionDetailRequest{
		SessionID: "session-invalid-turn", TurnPage: basequery.PageRequest{Limit: 20},
	})
	if !errors.Is(err, basequery.ErrUnavailable) {
		t.Fatalf("SessionDetail(invalid turn usage time) error = %v, want unavailable", err)
	}
}

func TestSessionDetailPreservesFallbackTurnZeroAndUnavailableCost(t *testing.T) {
	t.Parallel()

	zero := int64(0)
	snapshot := store.SessionAnalyticsSnapshot{
		Mode:   store.AnalyticsReadDetailFallback,
		Record: safeFallbackSessionRecord("session-fallback-turn", "Safe detail"),
		Turns: []store.SessionTurnAnalyticsRecord{{
			TurnID: "turn-fallback-active", StartedAtMS: 100,
			Model: store.ModelAttribution{
				Confidence: store.AttributionConfidenceUnknown,
				Source:     store.AttributionSourceMissing, Reason: store.AttributionReasonMissing,
			},
			Usage: &store.SessionTurnUsageAnalytics{
				ObservedAtMS: 110, InputTokens: &zero, CachedInputTokens: &zero,
				OutputTokens: &zero, ReasoningTokens: &zero,
			},
		}},
		PricingVersions: make([]string, 0),
		UnpricedReasons: make([]store.CostReasonCount, 0),
	}
	service := newUsageService(t, &sessionReaderStub{detail: func(
		context.Context,
		store.SessionAnalyticsDetailFilter,
	) (store.SessionAnalyticsSnapshot, error) {
		return snapshot, nil
	}})
	response, err := service.SessionDetail(context.Background(), SessionDetailRequest{
		SessionID: "session-fallback-turn",
	})
	if err != nil {
		t.Fatalf("SessionDetail(fallback turn) error = %v", err)
	}
	if response.Meta.Status != basequery.ResponsePartial || response.TurnPage.Limit != 20 ||
		response.TurnPage.HasMore || response.TurnPage.NextCursor != nil || len(response.Turns) != 1 ||
		response.Turns[0].State != SessionTurnActive ||
		response.Turns[0].PricingStatus != SessionTurnPricingUnknown {
		t.Fatalf("fallback turn response = %#v", response)
	}
	assertKnownNumeric(t, response.Turns[0].Totals.TotalTokens, 0, basequery.NumericTokens)
	assertUnknownNumeric(
		t, response.Turns[0].Totals.EstimatedUSDMicros, basequery.UnknownUnavailable,
	)
	assertUnknownNumeric(t, response.Turns[0].CompletedAt, basequery.UnknownNotApplicable)
}

func TestSessionDetailPreservesUnpricedTurnEvidence(t *testing.T) {
	t.Parallel()

	zero := int64(0)
	completedAt := int64(120)
	record := safeFallbackSessionRecord("session-unpriced-turn", "Safe detail")
	record.Rollup = &store.RollupTotals{
		TurnCount: 1, InputTokens: &zero, CachedInputTokens: &zero,
		OutputTokens: &zero, ReasoningTokens: &zero, TotalTokens: &zero,
		UnpricedTurnCount: 1, FirstActivityAtMS: 120, LastActivityAtMS: 120,
		UpdatedAtMS: 120,
	}
	snapshot := store.SessionAnalyticsSnapshot{
		Mode: store.AnalyticsReadActiveRollup,
		Generation: &store.CostRollupGeneration{
			GenerationID: "generation", ReportingTimezone: "UTC",
			PricingSource: "test", Currency: "USD", RollupVersion: 1,
		},
		Record: record,
		Turns: []store.SessionTurnAnalyticsRecord{{
			TurnID: "turn-unpriced", StartedAtMS: 100, CompletedAtMS: &completedAt,
			Model: store.ModelAttribution{
				Confidence: store.AttributionConfidenceUnknown,
				Source:     store.AttributionSourceMissing, Reason: store.AttributionReasonMissing,
			},
			Usage: &store.SessionTurnUsageAnalytics{
				ObservedAtMS: 120, IsFinal: true, InputTokens: &zero,
				CachedInputTokens: &zero, OutputTokens: &zero, ReasoningTokens: &zero,
			},
			Cost: &store.SessionTurnCostAnalytics{
				Status: pricing.CostStatusUnpriced, Reason: pricing.CostReasonModelNotListed,
			},
		}},
		PricingVersions: make([]string, 0),
		UnpricedReasons: []store.CostReasonCount{{
			Reason: pricing.CostReasonModelNotListed, Count: 1,
		}},
	}
	service := newUsageService(t, &sessionReaderStub{detail: func(
		context.Context,
		store.SessionAnalyticsDetailFilter,
	) (store.SessionAnalyticsSnapshot, error) {
		return snapshot, nil
	}})
	response, err := service.SessionDetail(context.Background(), SessionDetailRequest{
		SessionID: "session-unpriced-turn",
	})
	if err != nil {
		t.Fatalf("SessionDetail(unpriced turn) error = %v", err)
	}
	turn := response.Turns[0]
	if response.Meta.Status != basequery.ResponsePartial ||
		turn.PricingStatus != SessionTurnPricingUnpriced || turn.PricingVersion != nil ||
		turn.UnpricedReason == nil || *turn.UnpricedReason != pricing.CostReasonModelNotListed {
		t.Fatalf("unpriced turn response = %#v", response)
	}
	assertUnknownNumeric(t, turn.Totals.EstimatedUSDMicros, basequery.UnknownNotComputed)
	assertKnownNumeric(t, turn.Totals.PricedTurnCount, 0, basequery.NumericCount)
	assertKnownNumeric(t, turn.Totals.UnpricedTurnCount, 1, basequery.NumericCount)
}

func TestSessionDetailRejectsMalformedTurnPricingEvidence(t *testing.T) {
	t.Parallel()

	zero := int64(0)
	completedAt := int64(120)
	baseRecord := safeFallbackSessionRecord("session-malformed-pricing", "Safe detail")
	baseTurn := store.SessionTurnAnalyticsRecord{
		TurnID: "turn-malformed-pricing", StartedAtMS: 100, CompletedAtMS: &completedAt,
		Model: store.ModelAttribution{
			Confidence: store.AttributionConfidenceUnknown,
			Source:     store.AttributionSourceMissing, Reason: store.AttributionReasonMissing,
		},
		Usage: &store.SessionTurnUsageAnalytics{
			ObservedAtMS: 120, IsFinal: true, InputTokens: &zero,
			CachedInputTokens: &zero, OutputTokens: &zero, ReasoningTokens: &zero,
		},
	}
	for name, mutate := range map[string]func(*store.SessionAnalyticsSnapshot){
		"unknown unpriced reason": func(snapshot *store.SessionAnalyticsSnapshot) {
			snapshot.Record.Rollup = &store.RollupTotals{
				TurnCount: 1, InputTokens: &zero, CachedInputTokens: &zero,
				OutputTokens: &zero, ReasoningTokens: &zero, TotalTokens: &zero,
				UnpricedTurnCount: 1, FirstActivityAtMS: 120, LastActivityAtMS: 120,
				UpdatedAtMS: 120,
			}
			snapshot.Turns[0].Cost = &store.SessionTurnCostAnalytics{
				Status: pricing.CostStatusUnpriced, Reason: pricing.CostReason("private_reason"),
			}
			snapshot.UnpricedReasons = []store.CostReasonCount{{
				Reason: pricing.CostReasonModelNotListed, Count: 1,
			}}
		},
		"empty priced version": func(snapshot *store.SessionAnalyticsSnapshot) {
			snapshot.Record.Rollup = &store.RollupTotals{
				TurnCount: 1, InputTokens: &zero, CachedInputTokens: &zero,
				OutputTokens: &zero, ReasoningTokens: &zero, TotalTokens: &zero,
				EstimatedUSDMicros: &zero, PricedTurnCount: 1,
				FirstActivityAtMS: 120, LastActivityAtMS: 120, UpdatedAtMS: 120,
			}
			snapshot.Turns[0].Cost = &store.SessionTurnCostAnalytics{
				PricingVersion: pointerToString(""), EstimatedUSDMicros: &zero,
				Status: pricing.CostStatusPriced, Reason: pricing.CostReasonPriced,
			}
			snapshot.PricingVersions = []string{"pricing-v1"}
		},
	} {
		t.Run(name, func(t *testing.T) {
			snapshot := store.SessionAnalyticsSnapshot{
				Mode: store.AnalyticsReadActiveRollup,
				Generation: &store.CostRollupGeneration{
					GenerationID: "generation", ReportingTimezone: "UTC",
					PricingSource: "test", Currency: "USD", RollupVersion: 1,
				},
				Record:          baseRecord,
				Turns:           []store.SessionTurnAnalyticsRecord{baseTurn},
				PricingVersions: make([]string, 0),
				UnpricedReasons: make([]store.CostReasonCount, 0),
			}
			mutate(&snapshot)
			service := newUsageService(t, &sessionReaderStub{detail: func(
				context.Context,
				store.SessionAnalyticsDetailFilter,
			) (store.SessionAnalyticsSnapshot, error) {
				return snapshot, nil
			}})
			_, err := service.SessionDetail(context.Background(), SessionDetailRequest{
				SessionID: "session-malformed-pricing",
			})
			if !errors.Is(err, basequery.ErrUnavailable) {
				t.Fatalf("SessionDetail(malformed pricing) error = %v, want unavailable", err)
			}
		})
	}
}

func TestSessionDetailRejectsInvalidLimitTamperedAndCrossSessionTurnCursor(t *testing.T) {
	t.Parallel()

	calls := 0
	service := newUsageService(t, &sessionReaderStub{detail: func(
		context.Context,
		store.SessionAnalyticsDetailFilter,
	) (store.SessionAnalyticsSnapshot, error) {
		calls++
		return store.SessionAnalyticsSnapshot{}, nil
	}})
	validCursor, err := encodeSessionTurnCursor(
		service.sessionTurnCursorKey,
		&store.SessionTurnAnalyticsCursor{
			SessionID: "session-a", TurnID: "turn-a", StartedAtMS: 100,
		},
	)
	if err != nil || validCursor == nil {
		t.Fatalf("encodeSessionTurnCursor() = %v, %v", validCursor, err)
	}
	tamperedBytes := []byte(*validCursor)
	if tamperedBytes[0] == 'A' {
		tamperedBytes[0] = 'B'
	} else {
		tamperedBytes[0] = 'A'
	}
	tampered := string(tamperedBytes)
	invalid := []SessionDetailRequest{
		{SessionID: "session-a", TurnPage: basequery.PageRequest{Limit: -1}},
		{SessionID: "session-a", TurnPage: basequery.PageRequest{Limit: 51}},
		{SessionID: "session-a", TurnPage: basequery.PageRequest{Limit: 1, Cursor: &tampered}},
		{SessionID: "session-b", TurnPage: basequery.PageRequest{Limit: 1, Cursor: validCursor}},
	}
	for _, request := range invalid {
		if _, err := service.SessionDetail(context.Background(), request); !errors.Is(err, basequery.ErrValidation) {
			t.Fatalf("SessionDetail(%#v) error = %v, want validation", request, err)
		}
	}
	if calls != 0 {
		t.Fatalf("invalid turn requests reached reader %d times", calls)
	}
}

type sessionReaderStub struct {
	list   func(context.Context, store.SessionAnalyticsFilter) (store.SessionAnalyticsPage, error)
	detail func(context.Context, store.SessionAnalyticsDetailFilter) (store.SessionAnalyticsSnapshot, error)
}

func (reader *sessionReaderStub) UsageCostRange(
	context.Context,
	store.AnalyticsRange,
) (store.UsageCostRangeSnapshot, error) {
	return store.UsageCostRangeSnapshot{}, errors.New("usage reader not configured")
}

func (reader *sessionReaderStub) ListSessionAnalytics(
	ctx context.Context,
	filter store.SessionAnalyticsFilter,
) (store.SessionAnalyticsPage, error) {
	if reader.list == nil {
		return store.SessionAnalyticsPage{}, errors.New("session list reader not configured")
	}
	return reader.list(ctx, filter)
}

func (reader *sessionReaderStub) SessionAnalytics(
	ctx context.Context,
	filter store.SessionAnalyticsDetailFilter,
) (store.SessionAnalyticsSnapshot, error) {
	if reader.detail == nil {
		return store.SessionAnalyticsSnapshot{}, errors.New("session detail reader not configured")
	}
	return reader.detail(ctx, filter)
}

func pointerToString(value string) *string { return &value }

func testSessionTurnCursorKey() sessionTurnCursorKey {
	return sessionTurnCursorKey{
		0x10, 0x11, 0x12, 0x13, 0x14, 0x15, 0x16, 0x17,
		0x18, 0x19, 0x1a, 0x1b, 0x1c, 0x1d, 0x1e, 0x1f,
		0x20, 0x21, 0x22, 0x23, 0x24, 0x25, 0x26, 0x27,
		0x28, 0x29, 0x2a, 0x2b, 0x2c, 0x2d, 0x2e, 0x2f,
	}
}

func safeFallbackSessionRecord(sessionID, title string) store.SessionAnalyticsRecord {
	return store.SessionAnalyticsRecord{
		SessionID: sessionID, DisplayTitle: title,
		TitleConfidence: store.AttributionConfidenceHigh,
		TitleSource:     store.AttributionSourceSessionIDFallback,
		TitleReason:     store.AttributionReasonStableIdentity,
		Project: store.ProjectAttribution{
			Confidence: store.AttributionConfidenceUnknown,
			Source:     store.AttributionSourceMissing, Reason: store.AttributionReasonMissing,
		},
		Model: store.ModelAttribution{
			Confidence: store.AttributionConfidenceUnknown,
			Source:     store.AttributionSourceMissing, Reason: store.AttributionReasonMissing,
		},
		Activity: store.SessionActivityIdle,
	}
}
