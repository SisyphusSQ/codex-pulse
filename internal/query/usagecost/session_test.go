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
			Record: record, PricingVersions: []string{"pricing-v1"},
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
