package usagecost

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/SisyphusSQ/codex-pulse/internal/pricing"
	basequery "github.com/SisyphusSQ/codex-pulse/internal/query"
	"github.com/SisyphusSQ/codex-pulse/internal/store"
)

func TestUsageCostGroupsDayWeekMonthAndPreservesLedgerEvidence(t *testing.T) {
	t.Parallel()

	one, two, three := int64(1), int64(2), int64(3)
	costOne, costTwo := int64(10), int64(20)
	reader := usageReaderFunc(func(context.Context, store.AnalyticsRange) (store.UsageCostRangeSnapshot, error) {
		return store.UsageCostRangeSnapshot{
			Mode: store.AnalyticsReadActiveRollup,
			Generation: &store.CostRollupGeneration{
				GenerationID: "private-generation-id", ReportingTimezone: "Asia/Shanghai",
				PricingSource: "openai-api", Currency: "USD", RollupVersion: 1,
			},
			Daily: []store.UsageDaily{
				{GenerationID: "private-generation-id", BucketStartMS: mustUsageTime(t, "2026-06-30T16:00:00Z"), ReportingTimezone: "Asia/Shanghai", RollupTotals: store.RollupTotals{
					TurnCount: 1, InputTokens: &one, CachedInputTokens: &two,
					OutputTokens: &three, ReasoningTokens: &one, TotalTokens: int64Ptr(7),
					EstimatedUSDMicros: &costOne, PricedTurnCount: 1,
					FirstActivityAtMS: mustUsageTime(t, "2026-07-01T01:00:00Z"), LastActivityAtMS: mustUsageTime(t, "2026-07-01T01:00:00Z"),
				}},
				{GenerationID: "private-generation-id", BucketStartMS: mustUsageTime(t, "2026-07-01T16:00:00Z"), ReportingTimezone: "Asia/Shanghai", RollupTotals: store.RollupTotals{
					TurnCount: 1, InputTokens: &two, CachedInputTokens: &one,
					OutputTokens: &one, ReasoningTokens: &one, TotalTokens: int64Ptr(5),
					EstimatedUSDMicros: &costTwo, PricedTurnCount: 1,
					FirstActivityAtMS: mustUsageTime(t, "2026-07-02T02:00:00Z"), LastActivityAtMS: mustUsageTime(t, "2026-07-02T02:00:00Z"),
				}},
				{GenerationID: "private-generation-id", BucketStartMS: mustUsageTime(t, "2026-07-07T16:00:00Z"), ReportingTimezone: "Asia/Shanghai", RollupTotals: store.RollupTotals{
					TurnCount: 1, InputTokens: &three, CachedInputTokens: &one,
					OutputTokens: &one, ReasoningTokens: &one, TotalTokens: int64Ptr(6),
					UnpricedTurnCount: 1,
					FirstActivityAtMS: mustUsageTime(t, "2026-07-08T03:00:00Z"), LastActivityAtMS: mustUsageTime(t, "2026-07-08T03:00:00Z"),
				}},
			},
			PricingVersions: []string{"openai-api-2026-07-14"},
			UnpricedReasons: []store.CostReasonCount{{
				Reason: pricing.CostReasonModelNotListed, Count: 1,
			}},
		}, nil
	})
	service := newUsageService(t, reader)
	request := UsageCostRequest{
		Range: basequery.LocalDateRange{
			StartDate: "2026-07-01", EndDateExclusive: "2026-07-09", TimeZone: "Asia/Shanghai",
		},
	}

	for _, test := range []struct {
		granularity TrendGranularity
		wantKeys    []string
	}{
		{granularity: TrendDay, wantKeys: []string{"2026-07-01", "2026-07-02", "2026-07-08"}},
		{granularity: TrendWeek, wantKeys: []string{"2026-W27", "2026-W28"}},
		{granularity: TrendMonth, wantKeys: []string{"2026-07"}},
	} {
		request.Granularity = test.granularity
		response, err := service.UsageCost(context.Background(), request)
		if err != nil {
			t.Fatalf("UsageCost(%s) error = %v", test.granularity, err)
		}
		keys := make([]string, 0, len(response.Trend))
		for _, point := range response.Trend {
			keys = append(keys, point.Key)
		}
		if !reflect.DeepEqual(keys, test.wantKeys) {
			t.Fatalf("UsageCost(%s) keys = %#v, want %#v", test.granularity, keys, test.wantKeys)
		}
		if response.Meta.Status != basequery.ResponsePartial || response.Meta.Issues == nil ||
			response.DegradedReason != nil || response.ReportingTimeZone != "Asia/Shanghai" ||
			response.PricingSource == nil || *response.PricingSource != "openai-api" ||
			response.Currency == nil || *response.Currency != "USD" ||
			!reflect.DeepEqual(response.PricingVersions, []string{"openai-api-2026-07-14"}) ||
			len(response.UnpricedReasons) != 1 {
			t.Fatalf("UsageCost(%s) response metadata = %#v", test.granularity, response)
		}
		assertKnownNumeric(t, response.Totals.TurnCount, 3, basequery.NumericCount)
		assertKnownNumeric(t, response.Totals.TotalTokens, 18, basequery.NumericTokens)
		assertKnownNumeric(t, response.Totals.EstimatedUSDMicros, 30, basequery.NumericMicroUSD)
		encoded, err := json.Marshal(response)
		if err != nil {
			t.Fatalf("json.Marshal() error = %v", err)
		}
		if strings.Contains(string(encoded), "private-generation-id") {
			t.Fatalf("response leaked generation identity: %s", encoded)
		}
	}
}

func TestUsageCostFallbackIsPartialAndDoesNotInventUnknownOrZero(t *testing.T) {
	t.Parallel()

	zero := int64(0)
	reader := usageReaderFunc(func(context.Context, store.AnalyticsRange) (store.UsageCostRangeSnapshot, error) {
		return store.UsageCostRangeSnapshot{
			Mode: store.AnalyticsReadDetailFallback,
			Daily: []store.UsageDaily{{
				BucketStartMS:     mustUsageTime(t, "2026-03-08T08:00:00Z"),
				ReportingTimezone: "America/Los_Angeles",
				RollupTotals: store.RollupTotals{
					TurnCount: 1, InputTokens: &zero, CachedInputTokens: &zero,
					OutputTokens: nil, ReasoningTokens: &zero, TotalTokens: nil,
					UnpricedTurnCount: 1,
					FirstActivityAtMS: mustUsageTime(t, "2026-03-08T09:00:00Z"),
					LastActivityAtMS:  mustUsageTime(t, "2026-03-08T09:00:00Z"),
				},
			}},
			PricingVersions: make([]string, 0), UnpricedReasons: make([]store.CostReasonCount, 0),
		}, nil
	})
	service := newUsageService(t, reader)
	request := UsageCostRequest{
		Range: basequery.LocalDateRange{
			StartDate: "2026-03-08", EndDateExclusive: "2026-03-09", TimeZone: "America/Los_Angeles",
		},
		Granularity: TrendDay,
	}
	first, err := service.UsageCost(context.Background(), request)
	if err != nil {
		t.Fatalf("UsageCost(fallback) error = %v", err)
	}
	second, err := service.UsageCost(context.Background(), request)
	if err != nil {
		t.Fatalf("UsageCost(replay) error = %v", err)
	}
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("fallback replay drifted:\nfirst=%#v\nsecond=%#v", first, second)
	}
	if first.Meta.Status != basequery.ResponsePartial || first.DegradedReason == nil ||
		*first.DegradedReason != DegradedRollupMissing || first.PricingSource != nil ||
		first.Currency != nil || first.PricingVersions == nil || first.UnpricedReasons == nil ||
		first.Range.EndAtMS-first.Range.StartAtMS != 23*60*60*1000 {
		t.Fatalf("fallback metadata = %#v", first)
	}
	assertKnownNumeric(t, first.Totals.InputTokens, 0, basequery.NumericTokens)
	assertUnknownNumeric(t, first.Totals.OutputTokens, basequery.UnknownUnavailable)
	assertUnknownNumeric(t, first.Totals.TotalTokens, basequery.UnknownUnavailable)
	assertUnknownNumeric(t, first.Totals.EstimatedUSDMicros, basequery.UnknownUnavailable)
}

func TestUsageCostLightIndexReturnsKnownTokensAndUnknownTurnCost(t *testing.T) {
	t.Parallel()

	input, cached, output, reasoning, total := int64(100), int64(20), int64(10), int64(2), int64(112)
	reader := usageReaderFunc(func(context.Context, store.AnalyticsRange) (store.UsageCostRangeSnapshot, error) {
		return store.UsageCostRangeSnapshot{
			Mode: store.AnalyticsReadLightIndex,
			Daily: []store.UsageDaily{{
				BucketStartMS: 1_721_347_200_000, ReportingTimezone: "UTC",
				RollupTotals: store.RollupTotals{
					InputTokens: &input, CachedInputTokens: &cached, OutputTokens: &output,
					ReasoningTokens: &reasoning, TotalTokens: &total,
				},
			}},
			PricingVersions: []string{}, UnpricedReasons: []store.CostReasonCount{},
		}, nil
	})
	service := newUsageService(t, reader)
	response, err := service.UsageCost(context.Background(), UsageCostRequest{
		Range:       basequery.LocalDateRange{StartDate: "2024-07-19", EndDateExclusive: "2024-07-20", TimeZone: "UTC"},
		Granularity: TrendDay,
	})
	if err != nil {
		t.Fatalf("UsageCost(light) error = %v", err)
	}
	if response.Meta.Status != basequery.ResponsePartial {
		t.Fatalf("response = %#v", response)
	}
	assertKnownNumeric(t, response.Totals.TotalTokens, total, basequery.NumericTokens)
	assertUnknownNumeric(t, response.Totals.TurnCount, basequery.UnknownUnavailable)
	assertUnknownNumeric(t, response.Totals.EstimatedUSDMicros, basequery.UnknownNotComputed)
}

func TestUsageCostLightIndexReturnsKnownCostAndModelBreakdown(t *testing.T) {
	t.Parallel()

	input, cached, output, reasoning, total, cost := int64(1_000_000), int64(200_000), int64(100_000), int64(50_000), int64(1_150_000), int64(1_290_000)
	model := "gpt-5.4-mini"
	display := "GPT-5.4 Mini"
	reader := usageReaderFunc(func(context.Context, store.AnalyticsRange) (store.UsageCostRangeSnapshot, error) {
		return store.UsageCostRangeSnapshot{
			Mode: store.AnalyticsReadLightIndex, PricingSource: "openai-api", Currency: "USD",
			Daily: []store.UsageDaily{{
				BucketStartMS: 1_784_419_200_000, ReportingTimezone: "UTC",
				RollupTotals: store.RollupTotals{
					InputTokens: &input, CachedInputTokens: &cached, OutputTokens: &output,
					ReasoningTokens: &reasoning, TotalTokens: &total, EstimatedUSDMicros: &cost,
				},
			}},
			Models: []store.ModelUsageDaily{{
				BucketStartMS: 1_784_419_200_000, ReportingTimezone: "UTC", DimensionKey: model,
				ModelKey: &model, ModelDisplayName: &display,
				AttributionConfidence: "high", AttributionSource: "model_canonical", AttributionReason: "observed",
				RollupTotals: store.RollupTotals{
					InputTokens: &input, CachedInputTokens: &cached, OutputTokens: &output,
					ReasoningTokens: &reasoning, TotalTokens: &total, EstimatedUSDMicros: &cost,
				},
			}},
			PricingVersions: []string{"openai-api-2026-07-22"}, UnpricedReasons: []store.CostReasonCount{},
		}, nil
	})
	service := newUsageService(t, reader)
	response, err := service.UsageCost(context.Background(), UsageCostRequest{
		Range:       basequery.LocalDateRange{StartDate: "2026-07-19", EndDateExclusive: "2026-07-20", TimeZone: "UTC"},
		Granularity: TrendDay,
	})
	if err != nil {
		t.Fatalf("UsageCost(light priced) error = %v", err)
	}
	if response.PricingSource == nil || *response.PricingSource != "openai-api" ||
		response.Currency == nil || *response.Currency != "USD" || len(response.Models) != 1 ||
		response.Models[0].DimensionKey != model {
		t.Fatalf("priced light response = %#v", response)
	}
	assertKnownNumeric(t, response.Totals.EstimatedUSDMicros, cost, basequery.NumericMicroUSD)
	assertKnownNumeric(t, response.Models[0].Totals.TotalTokens, total, basequery.NumericTokens)
	assertKnownNumeric(t, response.Models[0].Totals.EstimatedUSDMicros, cost, basequery.NumericMicroUSD)
}

func TestUsageCostEmptyFallbackKeepsCostUnknown(t *testing.T) {
	t.Parallel()

	service := newUsageService(t, usageReaderFunc(func(
		context.Context, store.AnalyticsRange,
	) (store.UsageCostRangeSnapshot, error) {
		return store.UsageCostRangeSnapshot{
			Mode:  store.AnalyticsReadDetailFallback,
			Daily: make([]store.UsageDaily, 0), PricingVersions: make([]string, 0),
			UnpricedReasons: make([]store.CostReasonCount, 0),
		}, nil
	}))
	response, err := service.UsageCost(context.Background(), UsageCostRequest{
		Range: basequery.LocalDateRange{
			StartDate: "2026-01-01", EndDateExclusive: "2026-01-02", TimeZone: "UTC",
		},
		Granularity: TrendDay,
	})
	if err != nil {
		t.Fatalf("UsageCost(empty fallback) error = %v", err)
	}
	if response.Meta.Status != basequery.ResponsePartial || response.DegradedReason == nil ||
		*response.DegradedReason != DegradedRollupMissing || response.Trend == nil {
		t.Fatalf("empty fallback response = %#v", response)
	}
	assertKnownNumeric(t, response.Totals.TotalTokens, 0, basequery.NumericTokens)
	assertUnknownNumeric(t, response.Totals.EstimatedUSDMicros, basequery.UnknownUnavailable)
}

func TestUsageCostRejectsInvalidInputCancellationAndUnsafeStoredInteger(t *testing.T) {
	t.Parallel()

	calls := 0
	reader := usageReaderFunc(func(context.Context, store.AnalyticsRange) (store.UsageCostRangeSnapshot, error) {
		calls++
		unsafe := basequery.JavaScriptMaxSafeInteger + 1
		return store.UsageCostRangeSnapshot{
			Mode: store.AnalyticsReadActiveRollup,
			Generation: &store.CostRollupGeneration{
				GenerationID: "unsafe-generation", ReportingTimezone: "UTC",
				PricingSource: "test", Currency: "USD", RollupVersion: 1,
			},
			Daily: []store.UsageDaily{{
				GenerationID: "unsafe-generation", BucketStartMS: 0, ReportingTimezone: "UTC",
				RollupTotals: store.RollupTotals{
					TurnCount: 1, InputTokens: &unsafe, CachedInputTokens: int64Ptr(0),
					OutputTokens: int64Ptr(0), ReasoningTokens: int64Ptr(0), TotalTokens: &unsafe,
					PricedTurnCount: 1,
				},
			}},
			PricingVersions: make([]string, 0), UnpricedReasons: make([]store.CostReasonCount, 0),
		}, nil
	})
	service := newUsageService(t, reader)
	validRange := basequery.LocalDateRange{
		StartDate: "1970-01-01", EndDateExclusive: "1970-01-02", TimeZone: "UTC",
	}
	for _, request := range []UsageCostRequest{
		{Range: validRange, Granularity: "quarter"},
		{Range: basequery.LocalDateRange{StartDate: "2026-01-01", EndDateExclusive: "2027-02-01", TimeZone: "UTC"}, Granularity: TrendDay},
	} {
		if _, err := service.UsageCost(context.Background(), request); !errors.Is(err, basequery.ErrValidation) {
			t.Fatalf("UsageCost(%#v) error = %v, want validation", request, err)
		}
	}
	if calls != 0 {
		t.Fatalf("invalid requests reached reader %d times", calls)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := service.UsageCost(ctx, UsageCostRequest{Range: validRange, Granularity: TrendDay}); !errors.Is(err, context.Canceled) || calls != 0 {
		t.Fatalf("cancelled query calls=%d error=%v", calls, err)
	}
	_, err := service.UsageCost(context.Background(), UsageCostRequest{Range: validRange, Granularity: TrendDay})
	if !errors.Is(err, basequery.ErrUnavailable) || calls != 1 {
		t.Fatalf("unsafe stored value calls=%d error=%v, want unavailable", calls, err)
	}
}

func TestUsageCostRejectsInconsistentStoredTotalsAndEvidence(t *testing.T) {
	t.Parallel()

	zero, one, two := int64(0), int64(1), int64(2)
	validSnapshot := func() store.UsageCostRangeSnapshot {
		return store.UsageCostRangeSnapshot{
			Mode: store.AnalyticsReadActiveRollup,
			Generation: &store.CostRollupGeneration{
				GenerationID: "generation-1", ReportingTimezone: "UTC",
				PricingSource: "test", Currency: "USD", RollupVersion: 1,
			},
			Daily: []store.UsageDaily{{
				GenerationID: "generation-1", BucketStartMS: 0, ReportingTimezone: "UTC",
				RollupTotals: store.RollupTotals{
					TurnCount: 1, InputTokens: &one, CachedInputTokens: &zero,
					OutputTokens: &zero, ReasoningTokens: &zero, TotalTokens: &one,
					UnpricedTurnCount: 1,
				},
			}},
			PricingVersions: make([]string, 0),
			UnpricedReasons: []store.CostReasonCount{{
				Reason: pricing.CostReasonModelNotListed, Count: 1,
			}},
		}
	}
	tests := []struct {
		name   string
		mutate func(*store.UsageCostRangeSnapshot)
	}{
		{name: "total disagrees with components", mutate: func(snapshot *store.UsageCostRangeSnapshot) {
			snapshot.Daily[0].TotalTokens = &two
		}},
		{name: "unknown unpriced reason", mutate: func(snapshot *store.UsageCostRangeSnapshot) {
			snapshot.UnpricedReasons[0].Reason = pricing.CostReason("private-new-reason")
		}},
		{name: "empty pricing source", mutate: func(snapshot *store.UsageCostRangeSnapshot) {
			snapshot.Generation.PricingSource = ""
		}},
		{name: "priced turns without pricing version", mutate: func(snapshot *store.UsageCostRangeSnapshot) {
			snapshot.Daily[0].PricedTurnCount = 1
			snapshot.Daily[0].UnpricedTurnCount = 0
			snapshot.Daily[0].EstimatedUSDMicros = &zero
			snapshot.UnpricedReasons = make([]store.CostReasonCount, 0)
		}},
		{name: "unpriced reason count mismatch", mutate: func(snapshot *store.UsageCostRangeSnapshot) {
			snapshot.UnpricedReasons[0].Count = 2
		}},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			snapshot := validSnapshot()
			test.mutate(&snapshot)
			service := newUsageService(t, usageReaderFunc(func(
				context.Context, store.AnalyticsRange,
			) (store.UsageCostRangeSnapshot, error) {
				return snapshot, nil
			}))
			_, err := service.UsageCost(context.Background(), UsageCostRequest{
				Range: basequery.LocalDateRange{
					StartDate: "1970-01-01", EndDateExclusive: "1970-01-02", TimeZone: "UTC",
				},
				Granularity: TrendDay,
			})
			if !errors.Is(err, basequery.ErrUnavailable) {
				t.Fatalf("UsageCost() error = %v, want unavailable", err)
			}
		})
	}
}

func TestUsageCostMapsReaderCauseToContentFreeUnavailable(t *testing.T) {
	t.Parallel()

	privateCause := errors.New("synthetic-private-driver-cause")
	service := newUsageService(t, usageReaderFunc(func(
		context.Context, store.AnalyticsRange,
	) (store.UsageCostRangeSnapshot, error) {
		return store.UsageCostRangeSnapshot{}, privateCause
	}))
	_, err := service.UsageCost(context.Background(), UsageCostRequest{
		Range: basequery.LocalDateRange{
			StartDate: "2026-01-01", EndDateExclusive: "2026-01-02", TimeZone: "UTC",
		},
		Granularity: TrendDay,
	})
	if !errors.Is(err, basequery.ErrUnavailable) || strings.Contains(err.Error(), privateCause.Error()) {
		t.Fatalf("UsageCost() error = %q, want content-free unavailable", err)
	}
	envelope, ok := basequery.ErrorEnvelopeFrom(err)
	if !ok {
		t.Fatal("ErrorEnvelopeFrom() ok = false")
	}
	encoded, _ := json.Marshal(envelope)
	if strings.Contains(string(encoded), privateCause.Error()) {
		t.Fatalf("error envelope leaked cause: %s", encoded)
	}
}

type usageReaderFunc func(context.Context, store.AnalyticsRange) (store.UsageCostRangeSnapshot, error)

func (function usageReaderFunc) UsageCostRange(
	ctx context.Context,
	filter store.AnalyticsRange,
) (store.UsageCostRangeSnapshot, error) {
	return function(ctx, filter)
}

func newUsageService(t *testing.T, reader UsageReader) *Service {
	t.Helper()
	service, err := NewService(reader)
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	return service
}

func assertKnownNumeric(t testing.TB, value basequery.NumericValue, want int64, unit basequery.NumericUnit) {
	t.Helper()
	if value.Value == nil || *value.Value != want || value.Unit != unit || value.UnknownReason != nil {
		t.Fatalf("numeric = %#v, want known %d %s", value, want, unit)
	}
}

func assertUnknownNumeric(t testing.TB, value basequery.NumericValue, reason basequery.UnknownReason) {
	t.Helper()
	if value.Value != nil || value.UnknownReason == nil || *value.UnknownReason != reason {
		t.Fatalf("numeric = %#v, want unknown %s", value, reason)
	}
}

func int64Ptr(value int64) *int64 { return &value }

func mustUsageTime(t testing.TB, value string) int64 {
	t.Helper()
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		t.Fatalf("time.Parse(%q) error = %v", value, err)
	}
	return parsed.UnixMilli()
}
