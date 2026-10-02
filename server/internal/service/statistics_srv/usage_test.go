package statistics_srv

import (
	"errors"
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
