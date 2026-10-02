package statistics_srv

import (
	"errors"
	"math/big"
	"testing"

	reportingv1 "github.com/SisyphusSQ/codex-pulse/api/codexpulse/reporting/v1"
	centerfixture "github.com/SisyphusSQ/codex-pulse/server/internal/testsupport/center"
	"github.com/SisyphusSQ/codex-pulse/server/utils"
)

func TestUsageModelDaysDedupAndHistoricalPrices(t *testing.T) {
	stats, r, admin, clients := statisticsFixture(t)
	snap := centerfixture.Snapshot()
	snap.LastActiveAtMS = new(int64(2000))
	c := centerfixture.Contribution(1000000, 1000)
	c.Model = new("gpt-6.1-sol")
	c.CachedTokens = new(int64(500000))
	c.OutputTokens = new(int64(100000))
	c.ReasoningTokens = new(int64(10000))
	c.TotalTokens = new(int64(1110000))
	c.PricingVersion = new("historical-fixed")
	c.CostStatus = "known"
	c.PricingMode = "codex_model_sum"
	c.Rates = &reportingv1.Rates{InputMicroUSD: new(int64(2000000)), CachedMicroUSD: new(int64(100000)), OutputMicroUSD: new(int64(10000000))}
	c.CostMicroUSD = new(int64(2150000))
	c.ID = reportingv1.ContributionID("codex", "session", c, 0)
	snap.Contributions = []reportingv1.Contribution{c}
	for _, client := range clients {
		centerfixture.SendSnapshot(t, r, client, snap)
	}
	out, err := stats.Usage(t.Context(), admin, statisticsTestQuery(t, nil))
	if err != nil {
		t.Fatal(err)
	}
	decimal(t, out.CacheHitRate.BasisPoints, "5000")
	decimal(t, out.Models[0].CacheHitRate.BasisPoints, "5000")
	decimal(t, out.Totals.TotalTokens, "1110000")
	decimal(t, out.Totals.CostMicroUSD, "2150000")
	if len(out.Models) != 1 || len(out.ModelDays) != 1 || out.Models[0].Provider != "codex" || out.ModelDays[0].Date != "1970-01-01" {
		t.Fatal("model buckets", out)
	}
	decimal(t, out.ModelDays[0].Totals.CostMicroUSD, "2150000")
	if out.Totals.PricingVersions[0] != "historical-fixed" {
		t.Fatal("current price replaced evidence")
	}
	if _, err := stats.Usage(t.Context(), clients[0], statisticsTestQuery(t, nil)); !errors.Is(err, utils.ErrForbidden) {
		t.Fatal("collector usage", err)
	}
}

func TestRangeCacheHitRateUnknownAndExactCounts(t *testing.T) {
	for _, tc := range []struct {
		name, input, cached, provider, want, reason string
		missing                                     bool
	}{
		{name: "zero", input: "0", cached: "0", provider: "codex", reason: "not_applicable"},
		{name: "zero cached", input: "100", cached: "0", provider: "codex", want: "0"},
		{name: "large", input: "100000000000000000001", cached: "33335000000000000000", provider: "codex", want: "3333"},
		{name: "missing denominator", input: "100", cached: "90", provider: "codex", missing: true, reason: "unavailable"},
		{name: "invalid ratio", input: "100", cached: "101", provider: "codex", reason: "unavailable"},
		{name: "cursor", input: "100", cached: "90", provider: "cursor", reason: "unsupported_provider"},
		{name: "mixed", input: "100", cached: "90", reason: "mixed_providers"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			g := newStatisticsAggregate()
			input, _ := new(big.Int).SetString(tc.input, 10)
			cached, _ := new(big.Int).SetString(tc.cached, 10)
			g.input = decimalSum{value: *input, seen: true, missing: tc.missing}
			g.cached = decimalSum{value: *cached, seen: true}
			v := rangeCacheHitRate(g, tc.provider)
			if tc.want == "" {
				if v.BasisPoints != nil || v.Reason != tc.reason {
					t.Fatalf("unknown: %+v", v)
				}
			} else {
				decimal(t, v.BasisPoints, tc.want)
			}
			if g.input.value.String() != tc.input || g.cached.value.String() != tc.cached {
				t.Fatal("mutated totals")
			}
		})
	}
}
