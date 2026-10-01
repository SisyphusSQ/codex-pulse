package statistics_srv

import (
	"net/url"
	"testing"

	reportingv1 "github.com/SisyphusSQ/codex-pulse/api/codexpulse/reporting/v1"
	centerfixture "github.com/SisyphusSQ/codex-pulse/server/internal/testsupport/center"
)

func TestCacheHitRateUsesLifetimeNotRangeCopiesOrPricing(t *testing.T) {
	stats, reporting, admin, clients := statisticsFixture(t)
	s := centerfixture.Snapshot()
	s.Contributions = nil
	for _, values := range [][3]int64{{2000, 900, 900}, {3602000, 100, 0}} {
		c := centerfixture.Contribution(values[1], values[0])
		c.CachedTokens = new(values[2])
		c.CostStatus = "unpriced"
		c.Rates = nil
		c.CostMicroUSD = nil
		c.ID = reportingv1.ContributionID(s.Provider, s.SessionID, c, 0)
		s.Contributions = append(s.Contributions, c)
	}
	s.CacheUsage = &reportingv1.CacheUsageCapsule{Version: 1, Basis: "lifetime_cached_input", InputTokens: new(int64(1000)), CachedInputTokens: new(int64(900))}
	for _, p := range clients {
		centerfixture.SendSnapshot(t, reporting, p, s)
	}
	q := statisticsTestQuery(t, url.Values{"start_at_ms": {"3600000"}, "end_at_ms": {"3800000"}})
	list, err := stats.Sessions(t.Context(), admin, q)
	if err != nil || len(list.Items) != 1 {
		t.Fatal(err)
	}
	v := list.Items[0].CacheHitRate
	if v == nil {
		t.Fatal("cache metric missing")
	}
	decimal(t, v.BasisPoints, "9000")
	decimal(t, v.InputTokens, "1000")
	decimal(t, v.CachedInputTokens, "900")
	for _, p := range clients {
		q.ClientID = p.ID
		detail, err := stats.Session(t.Context(), admin, q, list.Items[0].ID)
		if err != nil {
			t.Fatal(err)
		}
		decimal(t, detail.Session.CacheHitRate.BasisPoints, "9000")
	}
}

func TestCacheHitRateBoundariesAndUnknownKeepOtherMetrics(t *testing.T) {
	for _, tt := range []struct {
		input, cached int64
		want, reason  string
	}{{1000, 0, "0", ""}, {1000, 1000, "10000", ""}, {32, 1, "313", ""}, {9007199254740991, 4503599627370496, "5000", ""}, {0, 0, "", "not_applicable"}, {1, 2, "", "unavailable"}} {
		s := centerfixture.Snapshot()
		s.CacheUsage = &reportingv1.CacheUsageCapsule{Version: 1, Basis: "lifetime_cached_input", InputTokens: new(tt.input), CachedInputTokens: new(tt.cached)}
		v := cacheHitRateView(s, "source", "codex", nil)
		if tt.want != "" {
			decimal(t, v.BasisPoints, tt.want)
		} else if v.BasisPoints != nil || v.Reason != tt.reason {
			t.Fatal("unknown became zero", tt, v)
		}
	}
	for _, provider := range []string{"codex", "cursor", "grok"} {
		v := cacheHitRateView(reportingv1.SessionSnapshot{}, "", provider, nil)
		if v.BasisPoints != nil {
			t.Fatal("unsupported became zero")
		}
	}
}
