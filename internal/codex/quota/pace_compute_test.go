package quota

import (
	"fmt"
	"testing"

	"github.com/SisyphusSQ/codex-pulse/internal/store"
)

func TestComputePaceWindowBoundsPredictionWithoutDroppingDenseCurve(t *testing.T) {
	now, reset := int64(10800000), int64(18000000)
	source := store.QuotaSourceAppServer
	facts := store.QuotaCurrentWindowSnapshot{Current: store.QuotaCurrent{AccountScope: "default", WindowKind: store.QuotaWindowPrimary, LimitID: "codex", EffectiveUsedPercent: new(70.0), WindowMinutes: new(int64(300)), ResetsAtMS: &reset, WindowGeneration: &reset, SelectedSource: &source, FreshnessState: store.QuotaCurrentFresh, ConflictState: store.QuotaConflictNone, EvaluatedAtMS: now}}
	for i := range 513 {
		facts.Observations = append(facts.Observations, paceObservation(fmt.Sprint(i), source, 20+50*float64(i)/512, now-3600000+int64(i)*7000, reset, 300))
	}
	out, err := ComputePaceWindow(facts, "default", now)
	if err != nil {
		t.Fatal(err)
	}
	if out.Forecast.State != PaceForecastUnavailable || out.Forecast.UnknownReason == nil || *out.Forecast.UnknownReason != PaceUnknownEvidenceBudget || out.Forecast.EvidenceCount != 513 || len(out.CurrentPoints) < 513 {
		t.Fatal("dense prediction escaped budget or lost observations")
	}
}
func TestComputePaceWindowUnknownHasExplicitUnavailableForecast(t *testing.T) {
	facts := store.QuotaCurrentWindowSnapshot{Current: store.QuotaCurrent{AccountScope: "default", LimitID: "codex", EvaluatedAtMS: 1000}}
	out, err := ComputePaceWindow(facts, "default", 1000)
	if err != nil {
		t.Fatal(err)
	}
	if out.Forecast.State != PaceForecastUnavailable || out.Forecast.Method != PaceForecastMethodNone || out.Forecast.UnknownReason == nil {
		t.Fatal("empty forecast semantics")
	}
}

func TestComputePaceWindowKeepsActualLinkedPointAtEvaluationTime(t *testing.T) {
	now, reset := int64(10800000), int64(18000000)
	source := store.QuotaSourceAppServer
	normal := paceObservation("normal", source, 70, now-600000, reset, 300)
	linked := paceObservation("linked", store.QuotaSourceWham, 99, now, reset, 300)
	linked.AccountScope = "linked-scope"
	facts := store.QuotaCurrentWindowSnapshot{Current: store.QuotaCurrent{AccountScope: "default", WindowKind: store.QuotaWindowPrimary, LimitID: "codex", EffectiveUsedPercent: new(70.0), WindowMinutes: new(int64(300)), ResetsAtMS: &reset, WindowGeneration: &reset, SelectedSource: &source, FreshnessState: store.QuotaCurrentFresh, ConflictState: store.QuotaConflictNone, EvaluatedAtMS: now}, Observations: []store.QuotaObservation{normal}, AssociatedHistoryScope: new("linked-scope"), AssociatedHistoryObservations: []store.QuotaObservation{linked}}
	out, err := ComputePaceWindow(facts, "default", now)
	if err != nil {
		t.Fatal(err)
	}
	if len(out.CurrentPoints) != 2 || out.CurrentPoints[1].UsedPercent != 99 || !out.CurrentPoints[1].LinkedHistory || out.CurrentPoints[1].ObservedAtMS != now || out.Forecast.UnknownReason == nil || *out.Forecast.UnknownReason != PaceUnknownEvidenceSparse {
		t.Fatal("display projection replaced real history or refreshed prediction")
	}
}
