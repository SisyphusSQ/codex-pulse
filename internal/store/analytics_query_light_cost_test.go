package store

import (
	"errors"
	"math"
	"testing"

	"github.com/SisyphusSQ/codex-pulse/internal/pricing"
)

func TestCalculateLightGroupCostKeepsIndecomposableCachedDeltaUnknown(t *testing.T) {
	t.Parallel()

	identity := "gpt-5.4-mini"
	catalogs := map[string]lightPricingCatalog{
		"v1": {models: map[string]modelPriceModel{
			identity: {
				InputMicrosPerMillion:       pointerTo(int64(1_000_000)),
				CachedInputMicrosPerMillion: pointerTo(int64(100_000)),
				OutputMicrosPerMillion:      pointerTo(int64(1_000_000)),
			},
		}},
	}
	group := &lightCostGroup{
		dimension: safeDimension{identity: &identity},
		input:     10, cached: 20,
	}
	cost, err := calculateLightGroupCost(group, catalogs, "v1")
	if err != nil {
		t.Fatalf("calculateLightGroupCost(indecomposable) error = %v", err)
	}
	if !cost.incomplete || cost.estimated != nil {
		t.Fatalf("indecomposable cost = %#v, want unknown incomplete", cost)
	}

	priced := *group
	priced.input = 20
	priced.cached = 10
	cost, err = calculateLightGroupCost(&priced, catalogs, "v1")
	if err != nil {
		t.Fatalf("calculateLightGroupCost(legal) error = %v", err)
	}
	if cost.incomplete || cost.estimated == nil || *cost.estimated != 11 {
		t.Fatalf("legal cost = %#v, want 11 micros", cost)
	}
}

func TestCalculateLightGroupCostStillFailsOverflowAndNegatives(t *testing.T) {
	t.Parallel()

	identity := "gpt-5.4-mini"
	catalogs := map[string]lightPricingCatalog{
		"v1": {models: map[string]modelPriceModel{
			identity: {
				InputMicrosPerMillion:       pointerTo(int64(math.MaxInt64)),
				CachedInputMicrosPerMillion: pointerTo(int64(1)),
				OutputMicrosPerMillion:      pointerTo(int64(1)),
			},
		}},
	}

	overflow := &lightCostGroup{
		dimension: safeDimension{identity: &identity},
		input:     math.MaxInt64,
	}
	if _, err := calculateLightGroupCost(overflow, catalogs, "v1"); !errors.Is(err, pricing.ErrCostOverflow) {
		t.Fatalf("overflow error = %v, want ErrCostOverflow", err)
	}

	negative := &lightCostGroup{
		dimension: safeDimension{identity: &identity},
		input:     -1,
	}
	if _, err := calculateLightGroupCost(negative, catalogs, "v1"); !errors.Is(err, pricing.ErrInvalidCalculation) {
		t.Fatalf("negative error = %v, want ErrInvalidCalculation", err)
	}
}

func TestLightRollupAccumulatorDoesNotKeepPricedSubtotalWhenBucketIsIndecomposable(t *testing.T) {
	t.Parallel()

	priced := lightCalculatedCost{estimated: pointerTo(int64(75))}
	incomplete := lightCalculatedCost{incomplete: true}
	orders := [][]lightCalculatedCost{
		{priced, incomplete},
		{incomplete, priced},
	}
	for _, order := range orders {
		aggregate := &lightRollupAccumulator{}
		if err := aggregate.add(&lightCostGroup{input: 100}, order[0]); err != nil {
			t.Fatal(err)
		}
		if err := aggregate.add(&lightCostGroup{input: 10, cached: 20}, order[1]); err != nil {
			t.Fatal(err)
		}
		totals := aggregate.totals()
		if totals.EstimatedUSDMicros != nil {
			t.Fatalf("mixed totals cost = %v, want unknown", *totals.EstimatedUSDMicros)
		}
		if totals.InputTokens == nil || *totals.InputTokens != 110 ||
			totals.CachedInputTokens == nil || *totals.CachedInputTokens != 20 {
			t.Fatalf("mixed totals tokens = %#v", totals)
		}
	}

	onlyPriced := &lightRollupAccumulator{}
	if err := onlyPriced.add(&lightCostGroup{input: 100}, priced); err != nil {
		t.Fatal(err)
	}
	totals := onlyPriced.totals()
	if totals.EstimatedUSDMicros == nil || *totals.EstimatedUSDMicros != 75 {
		t.Fatalf("priced-only totals = %#v", totals)
	}
}

func TestLightProjectTotalsPropagateOnlyIndecomposableCost(t *testing.T) {
	t.Parallel()

	records := []ProjectAnalyticsRecord{
		{
			DimensionKey: "priced",
			Totals: RollupTotals{
				InputTokens: pointerTo(int64(100)), TotalTokens: pointerTo(int64(100)),
				EstimatedUSDMicros: pointerTo(int64(75)),
			},
		},
		{
			DimensionKey: "unknown",
			Totals: RollupTotals{
				InputTokens: pointerTo(int64(10)), CachedInputTokens: pointerTo(int64(20)),
				TotalTokens: pointerTo(int64(10)),
			},
		},
	}

	partial, err := sumLightProjectRecords(records, nil)
	if err != nil {
		t.Fatal(err)
	}
	if partial.EstimatedUSDMicros == nil || *partial.EstimatedUSDMicros != 75 {
		t.Fatalf("ordinary unpriced subtotal = %#v, want existing 75 micros", partial)
	}

	incomplete, err := sumLightProjectRecords(records, map[string]bool{"unknown": true})
	if err != nil {
		t.Fatal(err)
	}
	if incomplete.EstimatedUSDMicros != nil || incomplete.InputTokens == nil || *incomplete.InputTokens != 110 {
		t.Fatalf("indecomposable project totals = %#v, want unknown cost and exact tokens", incomplete)
	}
}

func TestLightSessionTotalsPropagateOnlyIndecomposableCost(t *testing.T) {
	t.Parallel()

	records := []SessionAnalyticsRecord{
		{
			SessionID: "priced",
			Rollup: &RollupTotals{
				InputTokens: pointerTo(int64(100)), CachedInputTokens: pointerTo(int64(0)),
				OutputTokens: pointerTo(int64(0)), ReasoningTokens: pointerTo(int64(0)),
				TotalTokens: pointerTo(int64(100)), EstimatedUSDMicros: pointerTo(int64(75)),
			},
		},
		{
			SessionID: "unknown",
			Rollup: &RollupTotals{
				InputTokens: pointerTo(int64(10)), CachedInputTokens: pointerTo(int64(20)),
				OutputTokens: pointerTo(int64(0)), ReasoningTokens: pointerTo(int64(0)),
				TotalTokens: pointerTo(int64(10)),
			},
		},
	}

	partial := lightTotalsForRecords(records, nil)
	if partial.EstimatedUSDMicros == nil || *partial.EstimatedUSDMicros != 75 {
		t.Fatalf("ordinary unpriced subtotal = %#v, want existing 75 micros", partial)
	}
	incomplete := lightTotalsForRecords(records, map[string]bool{"unknown": true})
	if incomplete.EstimatedUSDMicros != nil || incomplete.InputTokens == nil || *incomplete.InputTokens != 110 {
		t.Fatalf("indecomposable session totals = %#v, want unknown cost and exact tokens", incomplete)
	}
}
