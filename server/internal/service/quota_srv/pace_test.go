package quota_srv

import (
	"testing"
	"time"

	reportingv1 "github.com/SisyphusSQ/codex-pulse/api/codexpulse/reporting/v1"
	quota_dto "github.com/SisyphusSQ/codex-pulse/server/internal/models/dto/quota_dto"
)

func TestCenterPaceMatchesNativeForecastAndLegacyBaselineWithoutSyntheticSamples(t *testing.T) {
	s, reporting, admin, clients := quotaFixture(t)
	base := quotaNow - 10800000
	reset := base + 18000000
	b := accountFacts("raw-a")
	for i, sample := range []struct {
		at   int64
		used float64
	}{{6300000, 32.5}, {7600000, 45}, {8900000, 57.5}, {10200000, 70}} {
		b.Quotas = append(b.Quotas, quotaFact(string(rune('a'+i)), "raw-a", sample.used, base+sample.at, reset))
	}
	for i, sample := range []struct {
		at   int64
		used float64
	}{{base - 17000000, 10}, {base - 1000000, 90}, {base + 10200000, 99}} {
		q := quotaFact("linked-"+string(rune('a'+i)), "raw-a", sample.used, sample.at, base)
		if i == 2 {
			q.ResetsAtMS = new(reset)
		}
		q.LocalScope, q.AssociationScope, q.HistoryOrigin, q.Source = "default", new("scope-raw-a"), "linked_history", "legacy_wham"
		b.Quotas = append(b.Quotas, q)
	}
	sendQuota(t, reporting, clients[0], b)
	out, err := s.Pace(t.Context(), admin, quota_dto.Query{})
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Windows) != 1 {
		t.Fatal("window missing")
	}
	p := out.Windows[0]
	if p.Forecast.State != "at_risk" || p.Forecast.Method != "recent_theil_sen" || p.Forecast.ExhaustAtMS == nil || *p.Forecast.ExhaustAtMS != base+13320000 || *p.Forecast.LeadBeforeResetMS != 4680000 || p.Forecast.EvidenceCount != 4 {
		t.Fatalf("native forecast differs: %#v", p.Forecast)
	}
	if p.PreviousCycle == nil || !p.PreviousCycle.Complete || p.HistoryCycleCount != 1 || len(p.HistoryBand) == 0 || !p.PreviousCycle.Points[0].LinkedHistory {
		t.Fatal("allowed legacy baseline missing")
	}
	if len(p.CurrentPoints) != 4 || p.CurrentPoints[3].ObservedAtMS != base+10200000 || p.CurrentPoints[3].UsedPercent != 70 {
		t.Fatal("legacy overrode confirmed point or invented now sample")
	}
	s.now = func() time.Time { return time.UnixMilli(quotaNow + 1) }
	stale, err := s.Pace(t.Context(), admin, quota_dto.Query{})
	if err != nil {
		t.Fatal(err)
	}
	if stale.Windows[0].Forecast.State != "unavailable" || stale.Windows[0].Forecast.UnknownReason == nil || *stale.Windows[0].Forecast.UnknownReason != "evidence_stale" || len(stale.Windows[0].CurrentPoints) != 4 {
		t.Fatal("stale forecast or erased curve")
	}
}
func TestCenterPaceSparseConflictAndDecreasingCurve(t *testing.T) {
	s, reporting, admin, clients := quotaFixture(t)
	reset := quotaNow + 7200000
	b := accountFacts("raw-a")
	b.Quotas = []reportingv1.QuotaObservation{quotaFact("sparse", "raw-a", 40, quotaNow-60000, reset)}
	sendQuota(t, reporting, clients[0], b)
	out, err := s.Pace(t.Context(), admin, quota_dto.Query{})
	if err != nil {
		t.Fatal(err)
	}
	if out.Windows[0].Forecast.UnknownReason == nil || *out.Windows[0].Forecast.UnknownReason != "evidence_sparse" {
		t.Fatal("sparse fabricated forecast")
	}
	for _, q := range []reportingv1.QuotaObservation{quotaFact("down", "raw-a", 20, quotaNow-30000, reset), quotaFact("rise", "raw-a", 50, quotaNow-10000, reset)} {
		sendQuota(t, reporting, clients[0], reportingv1.Batch{Quotas: []reportingv1.QuotaObservation{q}})
	}
	out, err = s.Pace(t.Context(), admin, quota_dto.Query{})
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Windows[0].CurrentPoints) != 3 || out.Windows[0].CurrentPoints[1].UsedPercent != 20 {
		t.Fatal("decrease flattened")
	}
	b = accountFacts("raw-a")
	b.Quotas = []reportingv1.QuotaObservation{quotaFact("disagree", "raw-a", 80, quotaNow-10000, reset)}
	sendQuota(t, reporting, clients[1], b)
	out, err = s.Pace(t.Context(), admin, quota_dto.Query{})
	if err != nil {
		t.Fatal(err)
	}
	if out.Windows[0].Forecast.UnknownReason == nil || *out.Windows[0].Forecast.UnknownReason != "source_conflict" || out.Windows[0].Forecast.ExhaustAtMS != nil {
		t.Fatal("conflict forecast accepted")
	}
}
