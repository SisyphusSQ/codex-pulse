package statistics_srv

import (
	"net/url"
	"testing"

	reportingv1 "github.com/SisyphusSQ/codex-pulse/api/codexpulse/reporting/v1"
	centerfixture "github.com/SisyphusSQ/codex-pulse/server/internal/testsupport/center"
)

func TestDSHReportingUSDCacheThroughputAndProviderIsolation(t *testing.T) {
	stats, reporting, admin, clients := statisticsFixture(t)
	snapshot := throughputSnapshot()
	snapshot.Provider, snapshot.SourceKind = "dsh", "dsh_local"
	snapshot.CacheUsage = &reportingv1.CacheUsageCapsule{Version: 1, Basis: "lifetime_cached_input", InputTokens: new(int64(400)), CachedInputTokens: new(int64(300))}
	for i := range snapshot.Contributions {
		c := &snapshot.Contributions[i]
		c.InputTokens, c.CachedTokens = new(int64(200)), new(int64(150))
		c.TotalTokens = new(int64(1200))
		c.Model = new("deepseek-flash")
		c.PricingMode, c.CostStatus = "event_cost", "known"
		c.PricingVersion = new("deepseek-usd-2026-10-06:off_peak")
		c.CostMicroUSD = new(int64(608))
	}
	fixStatisticsIDs(&snapshot)
	for _, client := range clients {
		centerfixture.SendSnapshot(t, reporting, client, snapshot)
	}
	q := statisticsTestQuery(t, url.Values{"provider": {"dsh"}, "start_at_ms": {"0"}, "end_at_ms": {"4000000"}})
	usage, err := stats.Usage(t.Context(), admin, q)
	if err != nil {
		t.Fatal(err)
	}
	decimal(t, usage.Totals.TotalTokens, "2400")
	decimal(t, usage.Totals.CostMicroUSD, "1216")
	decimal(t, usage.CacheHitRate.BasisPoints, "7500")
	if len(usage.Models) != 1 || usage.Models[0].Provider != "dsh" || usage.Totals.PricingVersions[0] != "deepseek-usd-2026-10-06:off_peak" {
		t.Fatal("DSH route or historical price was lost")
	}
	list, err := stats.Sessions(t.Context(), admin, q)
	if err != nil || len(list.Items) != 1 {
		t.Fatal("DSH copies were double counted", err)
	}
	decimal(t, list.Items[0].CacheHitRate.BasisPoints, "7500")
	decimal(t, list.Items[0].Throughput.AverageOutputMilliTPS, "18182")
	q.Provider = "codex"
	other, err := stats.Sessions(t.Context(), admin, q)
	if err != nil || len(other.Items) != 0 {
		t.Fatal("DSH appeared in Codex scope", err)
	}
}
