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

func TestDSHCodexHistoricalCostRevisionAndThreeDeviceCopies(t *testing.T) {
	stats, reporting, admin, clients := statisticsFixture(t)
	snapshot := throughputSnapshot()
	snapshot.Provider, snapshot.SourceKind = "dsh", "dsh_local"
	for i := range snapshot.Contributions {
		c := &snapshot.Contributions[i]
		c.Model = new("gpt-6.1-sol")
		c.PricingMode, c.CostStatus = "event_cost", "unpriced"
		c.CostMicroUSD, c.Rates, c.PricingVersion = nil, nil, nil
	}
	fixStatisticsIDs(&snapshot)
	for _, client := range clients {
		centerfixture.SendSnapshot(t, reporting, client, snapshot)
	}
	q := statisticsTestQuery(t, url.Values{"provider": {"dsh"}, "start_at_ms": {"0"}, "end_at_ms": {"4000000"}})
	before, err := stats.Usage(t.Context(), admin, q)
	if err != nil {
		t.Fatal(err)
	}
	decimal(t, before.Totals.TotalTokens, "2000")
	snapshot.Revision++
	for i := range snapshot.Contributions {
		c := &snapshot.Contributions[i]
		id := reportingv1.ContributionID(snapshot.Provider, snapshot.SessionID, *c, 0)
		c.CostStatus = "known"
		c.CostMicroUSD = new(int64(10000))
		c.PricingVersion = new("openai-api-2026-09-29")
		if c.ID != id {
			t.Fatal("cost changed identity")
		}
	}
	// 同一设备先补费用，其他两台仍是未计价副本；canonical 应采用更完整的价格证据。
	for _, client := range clients {
		centerfixture.SendSnapshot(t, reporting, client, snapshot)
		centerfixture.SendSnapshot(t, reporting, client, snapshot)
		after, err := stats.Usage(t.Context(), admin, q)
		if err != nil {
			t.Fatal(err)
		}
		decimal(t, after.Totals.TotalTokens, "2000")
		decimal(t, after.Totals.CostMicroUSD, "20000")
		if len(after.Models) != 1 || after.Models[0].Provider != "dsh" || len(after.Totals.PricingVersions) != 1 || after.Totals.PricingVersions[0] != "openai-api-2026-09-29" {
			t.Fatal("historical cost or DSH identity lost", after)
		}
	}
	list, err := stats.Sessions(t.Context(), admin, q)
	if err != nil || len(list.Items) != 1 {
		t.Fatal("copies duplicated the session", err)
	}
}
