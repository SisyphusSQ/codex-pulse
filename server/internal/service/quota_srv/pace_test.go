package quota_srv

import (
	"reflect"
	"testing"
	"time"

	reportingv1 "github.com/SisyphusSQ/codex-pulse/api/codexpulse/reporting/v1"
	quota_dto "github.com/SisyphusSQ/codex-pulse/server/internal/models/dto/quota_dto"
	quota_vo "github.com/SisyphusSQ/codex-pulse/server/internal/models/vo/quota_vo"
)

func TestCenterPaceWithoutConfirmedCurrentDoesNotInventSnapshot(t *testing.T) {
	for _, identity := range []string{"confirmed", "unassigned"} {
		window := quota_vo.Window{Key: "synthetic-window", IdentityState: identity, LimitID: "codex", WindowKind: "primary", Current: quota_vo.Current{Freshness: "never_loaded"}}
		p, err := paceWindow(window, quotaNow)
		if err != nil {
			t.Fatal(err)
		}
		if p.SnapshotAtMS != nil || p.Current.UsedPercent != nil || p.ElapsedPercent != nil || p.Forecast.State != "unavailable" || p.Forecast.UnknownReason == nil || len(p.CurrentPoints) != 0 {
			t.Fatalf("missing quota became a snapshot: %#v", p)
		}
	}
}

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
	if p.SnapshotAtMS == nil || *p.SnapshotAtMS != base+10200000 || *p.ElapsedPercent != 10200000.0/18000000*100 {
		t.Fatal("pace did not use the last observation time")
	}
	for _, at := range []int64{quotaNow + 1, reset + 1, reset + 30*86400000} {
		s.now = func() time.Time { return time.UnixMilli(at) }
		snapshot, err := s.Pace(t.Context(), admin, quota_dto.Query{})
		if err != nil {
			t.Fatal(err)
		}
		got := snapshot.Windows[0]
		if snapshot.EvaluatedAtMS != at || *got.SnapshotAtMS != *p.SnapshotAtMS ||
			*got.ElapsedPercent != *p.ElapsedPercent || *got.PaceDeltaPP != *p.PaceDeltaPP ||
			!reflect.DeepEqual(got.Forecast, p.Forecast) || !reflect.DeepEqual(got.CurrentPoints, p.CurrentPoints) ||
			!reflect.DeepEqual(got.PreviousRemainingAtElapsed, p.PreviousRemainingAtElapsed) ||
			!reflect.DeepEqual(got.HistoryMedianRemainingAtElapsed, p.HistoryMedianRemainingAtElapsed) {
			t.Fatal("polling time changed the last observed snapshot")
		}
		if got.Current.Freshness == "fresh" || got.Current.ResetRemainingMS != nil {
			t.Fatal("snapshot was incorrectly upgraded to a fresh current observation")
		}
	}
	s.now = func() time.Time { return time.UnixMilli(quotaNow) }
	newObservation := quotaFact("next-update", "raw-a", 75, quotaNow-300000, reset)
	sendQuota(t, reporting, clients[0], reportingv1.Batch{Quotas: []reportingv1.QuotaObservation{newObservation}})
	next, err := s.Pace(t.Context(), admin, quota_dto.Query{})
	if err != nil {
		t.Fatal(err)
	}
	if *next.Windows[0].SnapshotAtMS != newObservation.ObservedAtMS || *next.Windows[0].Current.UsedPercent != 75 || len(next.Windows[0].CurrentPoints) != 5 {
		t.Fatal("new observation did not advance the snapshot")
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
