package quota

import (
	"context"
	"fmt"
	"testing"

	"github.com/SisyphusSQ/codex-pulse/internal/store"
)

func TestBuildPaceWindowComparesUsedWithElapsedTime(t *testing.T) {
	t.Parallel()

	const (
		evaluatedAtMS = int64(2_520_000)
		resetAtMS     = int64(6_000_000)
		windowMinutes = int64(100)
	)
	usedPercent := 61.0
	source := store.QuotaSourceWham
	observationID := "observation-current"
	facts := store.QuotaCurrentWindowSnapshot{
		Current: store.QuotaCurrent{
			AccountScope:         store.QuotaAccountScopeDefault,
			WindowKind:           store.QuotaWindowSecondary,
			LimitID:              "codex",
			ObservationID:        &observationID,
			EffectiveUsedPercent: &usedPercent,
			WindowMinutes:        int64Pointer(windowMinutes),
			ResetsAtMS:           int64Pointer(resetAtMS),
			WindowGeneration:     int64Pointer(resetAtMS),
			SelectedSource:       &source,
			FreshnessState:       store.QuotaCurrentFresh,
			ConflictState:        store.QuotaConflictNone,
			ExplanationCode:      store.QuotaExplanationTrusted,
			EvaluatedAtMS:        evaluatedAtMS,
		},
	}

	got, err := buildPaceWindow(facts, evaluatedAtMS)
	if err != nil {
		t.Fatalf("buildPaceWindow() error = %v", err)
	}
	if got.WindowKind != store.QuotaWindowSecondary || got.LimitID != "codex" ||
		got.WindowStartAtMS == nil || *got.WindowStartAtMS != 0 ||
		got.UsedPercent == nil || *got.UsedPercent != 61 ||
		got.RemainingPercent == nil || *got.RemainingPercent != 39 ||
		got.ElapsedPercent == nil || *got.ElapsedPercent != 42 ||
		got.PaceDeltaPP == nil || *got.PaceDeltaPP != 19 {
		t.Fatalf("buildPaceWindow() = %#v", got)
	}
}

func TestCurrentPacePointsEndsAtCurrentProjection(t *testing.T) {
	t.Parallel()

	const (
		evaluatedAtMS = int64(2_520_000)
		resetAtMS     = int64(6_000_000)
		windowMinutes = int64(100)
	)
	source := store.QuotaSourceWham
	usedPercent := 85.0
	current := store.QuotaCurrent{
		AccountScope:         store.QuotaAccountScopeDefault,
		WindowKind:           store.QuotaWindowPrimary,
		LimitID:              "codex",
		EffectiveUsedPercent: &usedPercent,
		WindowMinutes:        int64Pointer(windowMinutes),
		ResetsAtMS:           int64Pointer(resetAtMS),
		WindowGeneration:     int64Pointer(resetAtMS),
		SelectedSource:       &source,
		EvaluatedAtMS:        evaluatedAtMS,
	}
	observations := []store.QuotaObservation{
		paceObservation("earlier", source, 35, 900_000, resetAtMS, windowMinutes),
	}

	got := currentPacePoints(current, observations)
	if len(got) != 2 {
		t.Fatalf("currentPacePoints() count = %d, want 2", len(got))
	}
	last := got[len(got)-1]
	if last.ObservedAtMS != evaluatedAtMS ||
		last.ElapsedPercent != 42 ||
		last.UsedPercent != 85 ||
		last.RemainingPercent != 15 {
		t.Fatalf("currentPacePoints() last = %#v", last)
	}
}

func TestBuildPaceWindowForecastsEarlyExhaustionFromRecentAcceptedEvidence(t *testing.T) {
	t.Parallel()

	const (
		evaluatedAtMS = int64(10_800_000)
		resetAtMS     = int64(18_000_000)
		windowMinutes = int64(300)
	)
	usedPercent := 70.0
	source := store.QuotaSourceWham
	observationID := "observation-current"
	facts := store.QuotaCurrentWindowSnapshot{
		Current: store.QuotaCurrent{
			AccountScope:         store.QuotaAccountScopeDefault,
			WindowKind:           store.QuotaWindowPrimary,
			LimitID:              "codex",
			ObservationID:        &observationID,
			EffectiveUsedPercent: &usedPercent,
			WindowMinutes:        int64Pointer(windowMinutes),
			ResetsAtMS:           int64Pointer(resetAtMS),
			WindowGeneration:     int64Pointer(resetAtMS),
			SelectedSource:       &source,
			FreshnessState:       store.QuotaCurrentFresh,
			ConflictState:        store.QuotaConflictNone,
			ExplanationCode:      store.QuotaExplanationTrusted,
			EvaluatedAtMS:        evaluatedAtMS,
		},
		Observations: []store.QuotaObservation{
			paceObservation("pace-1", source, 32.5, 6_300_000, resetAtMS, windowMinutes),
			paceObservation("pace-2", source, 45, 7_600_000, resetAtMS, windowMinutes),
			paceObservation("pace-3", source, 57.5, 8_900_000, resetAtMS, windowMinutes),
			paceObservation(observationID, source, 70, 10_200_000, resetAtMS, windowMinutes),
		},
	}

	got, err := buildPaceWindow(facts, evaluatedAtMS)
	if err != nil {
		t.Fatalf("buildPaceWindow() error = %v", err)
	}
	if got.Forecast.State != PaceForecastAtRisk ||
		got.Forecast.Method != PaceForecastMethodRecentTheilSen ||
		got.Forecast.ExhaustAtMS == nil || *got.Forecast.ExhaustAtMS != 13_320_000 ||
		got.Forecast.LeadBeforeResetMS == nil || *got.Forecast.LeadBeforeResetMS != 4_680_000 ||
		got.Forecast.EvidenceCount != 4 || got.Forecast.EvidenceSpanMS != 3_900_000 {
		t.Fatalf("buildPaceWindow().Forecast = %#v", got.Forecast)
	}
}

func TestBuildPaceWindowExcludesArbitrationRejectedRegression(t *testing.T) {
	t.Parallel()

	const (
		evaluatedAtMS = int64(10_800_000)
		resetAtMS     = int64(18_000_000)
		windowMinutes = int64(300)
	)
	usedPercent := 70.0
	source := store.QuotaSourceWham
	observationID := "pace-selected"
	facts := store.QuotaCurrentWindowSnapshot{
		Current: store.QuotaCurrent{
			AccountScope:         store.QuotaAccountScopeDefault,
			WindowKind:           store.QuotaWindowPrimary,
			LimitID:              "codex",
			ObservationID:        &observationID,
			EffectiveUsedPercent: &usedPercent,
			WindowMinutes:        int64Pointer(windowMinutes),
			ResetsAtMS:           int64Pointer(resetAtMS),
			WindowGeneration:     int64Pointer(resetAtMS),
			SelectedSource:       &source,
			FreshnessState:       store.QuotaCurrentFresh,
			ConflictState:        store.QuotaConflictNone,
			ExplanationCode:      store.QuotaExplanationTrusted,
			EvaluatedAtMS:        evaluatedAtMS,
		},
		Observations: []store.QuotaObservation{
			paceObservation("pace-1", source, 45, 7_200_000, resetAtMS, windowMinutes),
			paceObservation("pace-2", source, 55, 9_000_000, resetAtMS, windowMinutes),
			paceObservation("pace-regression", source, 54, 9_300_000, resetAtMS, windowMinutes),
			paceObservation(observationID, source, 70, evaluatedAtMS, resetAtMS, windowMinutes),
		},
		Evidence: []store.QuotaArbitrationEvidence{
			{ObservationID: "pace-1", Disposition: store.QuotaEvidenceEligible},
			{ObservationID: "pace-2", Disposition: store.QuotaEvidenceEligible},
			{ObservationID: "pace-regression", Disposition: store.QuotaEvidenceSuspicious},
			{ObservationID: observationID, Disposition: store.QuotaEvidenceSelected},
		},
	}

	got, err := buildPaceWindow(facts, evaluatedAtMS)
	if err != nil {
		t.Fatalf("buildPaceWindow() error = %v", err)
	}
	if len(got.CurrentPoints) != 3 {
		t.Fatalf("buildPaceWindow().CurrentPoints count = %d, want 3", len(got.CurrentPoints))
	}
	if got.Forecast.State != PaceForecastAtRisk ||
		got.Forecast.Method != PaceForecastMethodRecentTheilSen ||
		got.Forecast.ExhaustAtMS == nil || *got.Forecast.ExhaustAtMS != 15_120_000 ||
		got.Forecast.LeadBeforeResetMS == nil || *got.Forecast.LeadBeforeResetMS != 2_880_000 ||
		got.Forecast.EvidenceCount != 3 || got.Forecast.EvidenceSpanMS != 3_600_000 {
		t.Fatalf("buildPaceWindow().Forecast = %#v", got.Forecast)
	}
}

func TestBuildPaceWindowMergesBoundedResetJitterWithoutMixingLimits(t *testing.T) {
	t.Parallel()

	const (
		evaluatedAtMS = int64(10_800_000)
		resetAtMS     = int64(18_000_000)
		windowMinutes = int64(300)
		jitteredReset = resetAtMS - 5_000
	)
	usedPercent := 70.0
	source := store.QuotaSourceWham
	observationID := "observation-current"
	sparkLimitID := "codex_bengalfox"
	sparkObservation := paceObservation(
		"spark-must-stay-isolated",
		source,
		99,
		8_000_000,
		resetAtMS,
		windowMinutes,
	)
	sparkObservation.LimitID = &sparkLimitID
	facts := store.QuotaCurrentWindowSnapshot{
		Current: store.QuotaCurrent{
			AccountScope:         store.QuotaAccountScopeDefault,
			WindowKind:           store.QuotaWindowPrimary,
			LimitID:              "codex",
			ObservationID:        &observationID,
			EffectiveUsedPercent: &usedPercent,
			WindowMinutes:        int64Pointer(windowMinutes),
			ResetsAtMS:           int64Pointer(resetAtMS),
			WindowGeneration:     int64Pointer(resetAtMS),
			SelectedSource:       &source,
			FreshnessState:       store.QuotaCurrentFresh,
			ConflictState:        store.QuotaConflictNone,
			ExplanationCode:      store.QuotaExplanationTrusted,
			EvaluatedAtMS:        evaluatedAtMS,
		},
		Observations: []store.QuotaObservation{
			paceObservation("pace-1", source, 32.5, 6_300_000, jitteredReset, windowMinutes),
			paceObservation("pace-2", source, 45, 7_600_000, jitteredReset, windowMinutes),
			sparkObservation,
			paceObservation("pace-3", source, 57.5, 8_900_000, jitteredReset, windowMinutes),
			paceObservation(observationID, source, 70, 10_200_000, jitteredReset, windowMinutes),
		},
	}

	got, err := buildPaceWindow(facts, evaluatedAtMS)
	if err != nil {
		t.Fatalf("buildPaceWindow() error = %v", err)
	}
	if len(got.CurrentPoints) != 5 {
		t.Fatalf("buildPaceWindow().CurrentPoints count = %d, want 5", len(got.CurrentPoints))
	}
	if got.Forecast.State != PaceForecastAtRisk ||
		got.Forecast.Method != PaceForecastMethodRecentTheilSen ||
		got.Forecast.ExhaustAtMS == nil || *got.Forecast.ExhaustAtMS != 13_320_000 ||
		got.Forecast.LeadBeforeResetMS == nil || *got.Forecast.LeadBeforeResetMS != 4_680_000 ||
		got.Forecast.EvidenceCount != 4 || got.Forecast.EvidenceSpanMS != 3_900_000 {
		t.Fatalf("buildPaceWindow().Forecast = %#v", got.Forecast)
	}
}

func TestBuildPaceWindowRejectsResetJitterBeyondBoundary(t *testing.T) {
	t.Parallel()

	const (
		evaluatedAtMS = int64(10_800_000)
		resetAtMS     = int64(18_000_000)
		windowMinutes = int64(300)
		jitteredReset = resetAtMS - 5_001
	)
	usedPercent := 70.0
	source := store.QuotaSourceWham
	current := store.QuotaCurrent{
		AccountScope:         store.QuotaAccountScopeDefault,
		WindowKind:           store.QuotaWindowPrimary,
		LimitID:              "codex",
		EffectiveUsedPercent: &usedPercent,
		WindowMinutes:        int64Pointer(windowMinutes),
		ResetsAtMS:           int64Pointer(resetAtMS),
		WindowGeneration:     int64Pointer(resetAtMS),
		SelectedSource:       &source,
		FreshnessState:       store.QuotaCurrentFresh,
		ConflictState:        store.QuotaConflictNone,
		EvaluatedAtMS:        evaluatedAtMS,
	}
	observations := []store.QuotaObservation{
		paceObservation("pace-1", source, 45, 7_200_000, jitteredReset, windowMinutes),
		paceObservation("pace-2", source, 55, 9_000_000, jitteredReset, windowMinutes),
		paceObservation("pace-3", source, 70, evaluatedAtMS, jitteredReset, windowMinutes),
	}

	got := forecastPaceWindow(
		current,
		observations,
		resetAtMS-windowMinutes*60_000,
		windowMinutes*60_000,
		evaluatedAtMS,
	)
	if got.State != PaceForecastUnavailable ||
		got.Method != PaceForecastMethodNone ||
		got.UnknownReason == nil ||
		*got.UnknownReason != PaceUnknownEvidenceSparse {
		t.Fatalf("forecastPaceWindow() = %#v", got)
	}
}

func TestBuildPaceWindowComparesPreviousAndFourHistoricalCyclesAtSameProgress(t *testing.T) {
	t.Parallel()

	const (
		evaluatedAtMS = int64(26_520_000)
		resetAtMS     = int64(30_000_000)
		windowMinutes = int64(100)
		durationMS    = int64(6_000_000)
	)
	usedPercent := 61.0
	source := store.QuotaSourceWham
	observationID := "current-42"
	facts := store.QuotaCurrentWindowSnapshot{
		Current: store.QuotaCurrent{
			AccountScope:         store.QuotaAccountScopeDefault,
			WindowKind:           store.QuotaWindowPrimary,
			LimitID:              "codex",
			ObservationID:        &observationID,
			EffectiveUsedPercent: &usedPercent,
			WindowMinutes:        int64Pointer(windowMinutes),
			ResetsAtMS:           int64Pointer(resetAtMS),
			WindowGeneration:     int64Pointer(resetAtMS),
			SelectedSource:       &source,
			FreshnessState:       store.QuotaCurrentFresh,
			ConflictState:        store.QuotaConflictNone,
			ExplanationCode:      store.QuotaExplanationTrusted,
			EvaluatedAtMS:        evaluatedAtMS,
		},
		Observations: []store.QuotaObservation{
			paceObservation(observationID, source, 61, evaluatedAtMS, resetAtMS, windowMinutes),
		},
	}
	historicalUsedAt42Percent := []float64{45, 50, 55, 60}
	for index, usedAt42Percent := range historicalUsedAt42Percent {
		generation := resetAtMS - int64(index+1)*durationMS
		startAtMS := generation - durationMS
		facts.Observations = append(
			facts.Observations,
			paceObservation(fmt.Sprintf("history-%d-start", index), source, 0, startAtMS, generation, windowMinutes),
			paceObservation(
				fmt.Sprintf("history-%d-progress", index), source, usedAt42Percent,
				startAtMS+2_520_000, generation, windowMinutes,
			),
			paceObservation(
				fmt.Sprintf("history-%d-end", index), source, usedAt42Percent+20,
				generation-1, generation, windowMinutes,
			),
		)
	}

	got, err := buildPaceWindow(facts, evaluatedAtMS)
	if err != nil {
		t.Fatalf("buildPaceWindow() error = %v", err)
	}
	if got.PreviousCycle == nil || got.PreviousCycle.WindowGeneration != 24_000_000 ||
		len(got.HistoricalCycles) != 4 || got.HistoryCycleCount != 4 ||
		got.PreviousRemainingAtElapsed == nil || *got.PreviousRemainingAtElapsed != 55 ||
		got.HistoryMedianRemainingAtElapsed == nil || *got.HistoryMedianRemainingAtElapsed != 47.5 {
		t.Fatalf("buildPaceWindow() history = %#v", got)
	}
}

func TestHistoricalPaceCyclesMergeJitterWithoutTreatingCurrentAsPrevious(t *testing.T) {
	t.Parallel()

	const (
		evaluatedAtMS       = int64(26_520_000)
		resetAtMS           = int64(30_000_000)
		windowMinutes       = int64(100)
		durationMS          = int64(6_000_000)
		previousGeneration  = resetAtMS - durationMS
		previousWindowStart = previousGeneration - durationMS
	)
	usedPercent := 61.0
	source := store.QuotaSourceWham
	current := store.QuotaCurrent{
		AccountScope:         store.QuotaAccountScopeDefault,
		WindowKind:           store.QuotaWindowPrimary,
		LimitID:              "codex",
		EffectiveUsedPercent: &usedPercent,
		WindowMinutes:        int64Pointer(windowMinutes),
		ResetsAtMS:           int64Pointer(resetAtMS),
		WindowGeneration:     int64Pointer(resetAtMS),
		SelectedSource:       &source,
		FreshnessState:       store.QuotaCurrentFresh,
		ConflictState:        store.QuotaConflictNone,
		EvaluatedAtMS:        evaluatedAtMS,
	}
	observations := []store.QuotaObservation{
		paceObservation(
			"current-jitter",
			source,
			usedPercent,
			evaluatedAtMS,
			resetAtMS-1_000,
			windowMinutes,
		),
		paceObservation(
			"previous-start",
			source,
			0,
			previousWindowStart,
			previousGeneration,
			windowMinutes,
		),
		paceObservation(
			"previous-end-jitter",
			source,
			95,
			previousGeneration-2_000,
			previousGeneration-1_000,
			windowMinutes,
		),
	}

	previous, historical := historicalPaceCycles(current, observations)
	if previous == nil ||
		previous.WindowGeneration != previousGeneration ||
		!previous.Complete ||
		len(previous.Points) != 2 {
		t.Fatalf("historicalPaceCycles() previous = %#v", previous)
	}
	if len(historical) != 1 ||
		historical[0].WindowGeneration != previousGeneration ||
		!historical[0].Complete {
		t.Fatalf("historicalPaceCycles() historical = %#v", historical)
	}
}

func TestHistoricalPaceCyclesDoNotChainJitterBeyondBoundary(t *testing.T) {
	t.Parallel()

	const (
		evaluatedAtMS       = int64(26_520_000)
		resetAtMS           = int64(30_000_000)
		windowMinutes       = int64(100)
		durationMS          = int64(6_000_000)
		previousGeneration  = resetAtMS - durationMS
		previousWindowStart = previousGeneration - durationMS
	)
	usedPercent := 61.0
	source := store.QuotaSourceWham
	current := store.QuotaCurrent{
		AccountScope:         store.QuotaAccountScopeDefault,
		WindowKind:           store.QuotaWindowPrimary,
		LimitID:              "codex",
		EffectiveUsedPercent: &usedPercent,
		WindowMinutes:        int64Pointer(windowMinutes),
		ResetsAtMS:           int64Pointer(resetAtMS),
		WindowGeneration:     int64Pointer(resetAtMS),
		SelectedSource:       &source,
		FreshnessState:       store.QuotaCurrentFresh,
		ConflictState:        store.QuotaConflictNone,
		EvaluatedAtMS:        evaluatedAtMS,
	}
	observations := []store.QuotaObservation{
		paceObservation(
			"previous-start",
			source,
			0,
			previousWindowStart,
			previousGeneration,
			windowMinutes,
		),
		paceObservation(
			"previous-middle-jitter",
			source,
			50,
			previousWindowStart+durationMS/2,
			previousGeneration-4_000,
			windowMinutes,
		),
		paceObservation(
			"separate-end-after-chain",
			source,
			95,
			previousGeneration-10_000,
			previousGeneration-8_000,
			windowMinutes,
		),
	}

	previous, historical := historicalPaceCycles(current, observations)
	if previous == nil ||
		previous.WindowGeneration != previousGeneration ||
		previous.Complete ||
		len(previous.Points) != 2 {
		t.Fatalf("historicalPaceCycles() previous = %#v", previous)
	}
	if len(historical) != 0 {
		t.Fatalf("historicalPaceCycles() historical = %#v, want no complete cycle", historical)
	}
}

func TestCurrentQueryServiceReturnsVersionedPaceWindowsFromOneSnapshot(t *testing.T) {
	t.Parallel()

	const (
		evaluatedAtMS = int64(2_520_000)
		resetAtMS     = int64(6_000_000)
		windowMinutes = int64(100)
	)
	usedPercent := 61.0
	source := store.QuotaSourceWham
	observationID := "observation-current"
	reader := currentSnapshotReaderFunc(func(
		_ context.Context,
		accountScope string,
		atMS int64,
	) (store.QuotaCurrentSnapshot, error) {
		return store.QuotaCurrentSnapshot{
			AccountScope:  accountScope,
			EvaluatedAtMS: atMS,
			Windows: []store.QuotaCurrentWindowSnapshot{{
				Current: store.QuotaCurrent{
					AccountScope:         accountScope,
					WindowKind:           store.QuotaWindowSecondary,
					LimitID:              "codex",
					ObservationID:        &observationID,
					EffectiveUsedPercent: &usedPercent,
					WindowMinutes:        int64Pointer(windowMinutes),
					ResetsAtMS:           int64Pointer(resetAtMS),
					WindowGeneration:     int64Pointer(resetAtMS),
					SelectedSource:       &source,
					FreshnessState:       store.QuotaCurrentFresh,
					ConflictState:        store.QuotaConflictNone,
					ExplanationCode:      store.QuotaExplanationTrusted,
					EvaluatedAtMS:        atMS,
				},
			}},
		}, nil
	})
	service, err := NewCurrentQueryService(reader)
	if err != nil {
		t.Fatalf("NewCurrentQueryService() error = %v", err)
	}

	response, err := service.Pace(context.Background(), evaluatedAtMS)
	if err != nil {
		t.Fatalf("Pace() error = %v", err)
	}
	if response.Version != PaceContractVersion ||
		response.AccountScope != store.QuotaAccountScopeDefault ||
		response.EvaluatedAtMS != evaluatedAtMS ||
		len(response.Windows) != 1 ||
		response.Windows[0].PaceDeltaPP == nil ||
		*response.Windows[0].PaceDeltaPP != 19 {
		t.Fatalf("Pace() = %#v", response)
	}
}

func TestDownsamplePacePointsPreservesBoundsAndOrder(t *testing.T) {
	t.Parallel()

	points := make([]PacePoint, 200)
	for index := range points {
		points[index] = PacePoint{
			ObservedAtMS:     int64(index),
			ElapsedPercent:   float64(index) / 2,
			UsedPercent:      float64(index) / 2,
			RemainingPercent: 100 - float64(index)/2,
		}
	}

	got := downsamplePacePoints(points, maximumPacePointsPerCycle)
	if len(got) != maximumPacePointsPerCycle {
		t.Fatalf("downsamplePacePoints() count = %d, want %d", len(got), maximumPacePointsPerCycle)
	}
	if got[0] != points[0] || got[len(got)-1] != points[len(points)-1] {
		t.Fatalf("downsamplePacePoints() did not preserve endpoints")
	}
	for index := 1; index < len(got); index++ {
		if got[index].ObservedAtMS <= got[index-1].ObservedAtMS {
			t.Fatalf(
				"downsamplePacePoints() is not strictly ordered at %d: %#v",
				index,
				got[index-1:index+1],
			)
		}
	}
}

func TestForecastPaceWindowFailsClosedForUntrustedOrInsufficientEvidence(t *testing.T) {
	t.Parallel()

	const (
		evaluatedAtMS = int64(10_800_000)
		resetAtMS     = int64(18_000_000)
		windowMinutes = int64(300)
	)
	source := store.QuotaSourceWham
	usedPercent := 70.0
	current := store.QuotaCurrent{
		AccountScope:         store.QuotaAccountScopeDefault,
		WindowKind:           store.QuotaWindowPrimary,
		LimitID:              "codex",
		EffectiveUsedPercent: &usedPercent,
		WindowMinutes:        int64Pointer(windowMinutes),
		ResetsAtMS:           int64Pointer(resetAtMS),
		WindowGeneration:     int64Pointer(resetAtMS),
		SelectedSource:       &source,
		FreshnessState:       store.QuotaCurrentFresh,
		ConflictState:        store.QuotaConflictNone,
		EvaluatedAtMS:        evaluatedAtMS,
	}
	evidence := []store.QuotaObservation{
		paceObservation("pace-1", source, 45, 7_200_000, resetAtMS, windowMinutes),
		paceObservation("pace-2", source, 55, 9_000_000, resetAtMS, windowMinutes),
		paceObservation("pace-3", source, 70, evaluatedAtMS, resetAtMS, windowMinutes),
	}
	windowStartAtMS := resetAtMS - windowMinutes*60_000
	durationMS := windowMinutes * 60_000

	tests := []struct {
		name   string
		mutate func(*store.QuotaCurrent, *[]store.QuotaObservation)
		reason PaceUnknownReason
	}{
		{
			name: "stale",
			mutate: func(current *store.QuotaCurrent, _ *[]store.QuotaObservation) {
				current.FreshnessState = store.QuotaCurrentStale
			},
			reason: PaceUnknownEvidenceStale,
		},
		{
			name: "conflict",
			mutate: func(current *store.QuotaCurrent, _ *[]store.QuotaObservation) {
				current.ConflictState = store.QuotaConflictPresent
			},
			reason: PaceUnknownSourceConflict,
		},
		{
			name: "sparse",
			mutate: func(_ *store.QuotaCurrent, observations *[]store.QuotaObservation) {
				*observations = (*observations)[:2]
			},
			reason: PaceUnknownEvidenceSparse,
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			testCurrent := current
			testEvidence := append([]store.QuotaObservation(nil), evidence...)
			test.mutate(&testCurrent, &testEvidence)

			got := forecastPaceWindow(
				testCurrent,
				testEvidence,
				windowStartAtMS,
				durationMS,
				evaluatedAtMS,
			)
			if got.State != PaceForecastUnavailable ||
				got.Method != PaceForecastMethodNone ||
				got.ExhaustAtMS != nil ||
				got.LeadBeforeResetMS != nil ||
				got.UnknownReason == nil ||
				*got.UnknownReason != test.reason {
				t.Fatalf("forecastPaceWindow() = %#v", got)
			}
		})
	}
}

func TestForecastPaceWindowDoesNotProjectPastReset(t *testing.T) {
	t.Parallel()

	const (
		evaluatedAtMS = int64(10_800_000)
		resetAtMS     = int64(18_000_000)
		windowMinutes = int64(300)
	)
	source := store.QuotaSourceWham
	usedPercent := 40.0
	current := store.QuotaCurrent{
		AccountScope:         store.QuotaAccountScopeDefault,
		WindowKind:           store.QuotaWindowPrimary,
		LimitID:              "codex",
		EffectiveUsedPercent: &usedPercent,
		WindowMinutes:        int64Pointer(windowMinutes),
		ResetsAtMS:           int64Pointer(resetAtMS),
		WindowGeneration:     int64Pointer(resetAtMS),
		SelectedSource:       &source,
		FreshnessState:       store.QuotaCurrentFresh,
		ConflictState:        store.QuotaConflictNone,
		EvaluatedAtMS:        evaluatedAtMS,
	}
	evidence := []store.QuotaObservation{
		paceObservation("pace-1", source, 37, 7_200_000, resetAtMS, windowMinutes),
		paceObservation("pace-2", source, 38.5, 9_000_000, resetAtMS, windowMinutes),
		paceObservation("pace-3", source, 40, evaluatedAtMS, resetAtMS, windowMinutes),
	}

	got := forecastPaceWindow(
		current,
		evidence,
		resetAtMS-windowMinutes*60_000,
		windowMinutes*60_000,
		evaluatedAtMS,
	)
	if got.State != PaceForecastOnTrack ||
		got.Method != PaceForecastMethodRecentTheilSen ||
		got.ExhaustAtMS != nil ||
		got.LeadBeforeResetMS != nil ||
		got.UnknownReason != nil {
		t.Fatalf("forecastPaceWindow() = %#v", got)
	}
}

func paceObservation(
	observationID string,
	source store.QuotaSource,
	usedPercent float64,
	observedAtMS int64,
	resetAtMS int64,
	windowMinutes int64,
) store.QuotaObservation {
	limitID := "codex"
	return store.QuotaObservation{
		ObservationID:     observationID,
		AccountScope:      store.QuotaAccountScopeDefault,
		Source:            source,
		LimitID:           &limitID,
		WindowKind:        store.QuotaWindowPrimary,
		UsedPercent:       usedPercent,
		WindowMinutes:     windowMinutes,
		ResetsAtMS:        resetAtMS,
		Validity:          store.QuotaValidityAccepted,
		FirstObservedAtMS: observedAtMS,
		LastObservedAtMS:  observedAtMS,
		SampleCount:       1,
	}
}
