package usagecost

import (
	"testing"

	basequery "github.com/SisyphusSQ/codex-pulse/internal/query"
	"github.com/SisyphusSQ/codex-pulse/internal/store"
)

func TestCacheHitRateUsesWeightedInputTotalsAndPreservesUnknown(t *testing.T) {
	tests := []struct {
		name   string
		input  *int64
		cached *int64
		want   *int64
		reason basequery.UnknownReason
	}{
		{name: "weighted total", input: new(int64(1_000)), cached: new(int64(900)), want: new(int64(9_000))},
		{name: "zero hits", input: new(int64(100)), cached: new(int64(0)), want: new(int64(0))},
		{name: "all hits", input: new(int64(100)), cached: new(int64(100)), want: new(int64(10_000))},
		{name: "round down", input: new(int64(3)), cached: new(int64(1)), want: new(int64(3_333))},
		{name: "round up", input: new(int64(3)), cached: new(int64(2)), want: new(int64(6_667))},
		{name: "round half up", input: new(int64(32)), cached: new(int64(1)), want: new(int64(313))},
		{name: "large count multiplication", input: new(basequery.JavaScriptMaxSafeInteger), cached: new(basequery.JavaScriptMaxSafeInteger - 1), want: new(int64(10_000))},
		{name: "zero input", input: new(int64(0)), cached: new(int64(0)), reason: basequery.UnknownNotApplicable},
		{name: "missing input", cached: new(int64(0)), reason: basequery.UnknownNeverLoaded},
		{name: "missing cached", input: new(int64(100)), reason: basequery.UnknownNeverLoaded},
		{name: "cached exceeds input", input: new(int64(100)), cached: new(int64(101)), reason: basequery.UnknownUnavailable},
		{name: "zero input with cached", input: new(int64(0)), cached: new(int64(1)), reason: basequery.UnknownUnavailable},
		{name: "negative input", input: new(int64(-1)), cached: new(int64(0)), reason: basequery.UnknownUnavailable},
		{name: "negative cached", input: new(int64(100)), cached: new(int64(-1)), reason: basequery.UnknownUnavailable},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			totals := UsageTotals{
				InputTokens:       cacheTestTokens(test.input),
				CachedInputTokens: cacheTestTokens(test.cached),
			}
			got, err := mapCacheHitRate(totals)
			if err != nil {
				t.Fatal(err)
			}
			if got.Unit != basequery.NumericBasisPoints {
				t.Fatalf("unit = %q", got.Unit)
			}
			if test.want != nil {
				assertKnownNumeric(t, got, *test.want, basequery.NumericBasisPoints)
			} else {
				assertUnknownNumeric(t, got, test.reason)
			}
		})
	}
}

func TestCacheHitRateSessionMapperWorksWithoutPricingAndFailsLocally(t *testing.T) {
	record := safeFallbackSessionRecord("cache-session", "Safe cache session")
	record.Rollup = &store.RollupTotals{
		InputTokens: new(int64(1_000)), CachedInputTokens: new(int64(900)),
	}
	item, err := mapSessionItem(record, store.AnalyticsReadLightIndex)
	if err != nil {
		t.Fatal(err)
	}
	assertKnownNumeric(t, *item.CacheHitRate, 9_000, basequery.NumericBasisPoints)
	assertUnknownNumeric(t, item.Totals.EstimatedUSDMicros, basequery.UnknownNotComputed)

	record.Rollup.CachedInputTokens = new(int64(1_001))
	item, err = mapSessionItem(record, store.AnalyticsReadLightIndex)
	if err != nil {
		t.Fatal(err)
	}
	assertUnknownNumeric(t, *item.CacheHitRate, basequery.UnknownUnavailable)
	assertKnownNumeric(t, item.Totals.InputTokens, 1_000, basequery.NumericTokens)
	assertKnownNumeric(t, item.Totals.CachedInputTokens, 1_001, basequery.NumericTokens)

	for _, mode := range []store.AnalyticsReadMode{store.AnalyticsReadDetailFallback, store.AnalyticsReadAmbiguousFallback} {
		record.Rollup = nil
		item, err = mapSessionItem(record, mode)
		if err != nil {
			t.Fatal(err)
		}
		assertUnknownNumeric(t, *item.CacheHitRate, basequery.UnknownUnavailable)
	}
}

func cacheTestTokens(value *int64) basequery.NumericValue {
	if value == nil {
		return basequery.NumericValue{Unit: basequery.NumericTokens, UnknownReason: new(basequery.UnknownNeverLoaded)}
	}
	return basequery.NumericValue{Unit: basequery.NumericTokens, Value: value}
}
