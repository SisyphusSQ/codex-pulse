package store

import (
	"math"
	"math/rand"
	"reflect"
	"testing"
)

const (
	quotaTestHourMS   int64 = 60 * 60 * 1000
	quotaTestMinuteMS int64 = 60 * 1000
)

func TestQuotaArbiterSelectsConservativeCurrentAndKeepsConflictEvidence(t *testing.T) {
	t.Parallel()

	now := int64(10 * quotaTestHourMS)
	reset := now + 5*quotaTestHourMS
	local := quotaArbiterObservation("local-45", QuotaSourceLocalJSONL, 45, now-2*quotaTestMinuteMS, reset)
	wham := quotaArbiterObservation("wham-41", QuotaSourceWham, 41, now-quotaTestMinuteMS, reset)

	projection, err := arbitrateQuotaWindow([]QuotaObservation{wham, local}, now, defaultQuotaArbitrationRule())
	if err != nil {
		t.Fatalf("arbitrateQuotaWindow() error = %v", err)
	}
	if projection.Current.ObservationID == nil || *projection.Current.ObservationID != local.ObservationID ||
		projection.Current.EffectiveUsedPercent == nil || *projection.Current.EffectiveUsedPercent != 45 ||
		projection.Current.WindowGeneration == nil || *projection.Current.WindowGeneration != reset {
		t.Fatalf("current = %#v, want conservative local 45", projection.Current)
	}
	if projection.Current.FreshnessState != QuotaCurrentFresh ||
		projection.Current.ConflictState != QuotaConflictPresent ||
		projection.Current.ExplanationCode != QuotaExplanationSourceConflict {
		t.Fatalf("current states = %#v", projection.Current)
	}
	assertQuotaEvidence(t, projection.Evidence, local.ObservationID, QuotaEvidenceSelected, QuotaReasonSourceConflict)
	assertQuotaEvidence(t, projection.Evidence, wham.ObservationID, QuotaEvidenceEligible, QuotaReasonSourceConflict)
}

func TestQuotaArbiterDoesNotOverflowFutureClockBoundary(t *testing.T) {
	t.Parallel()

	evaluatedAt := int64(math.MaxInt64)
	observedAt := evaluatedAt - quotaTestHourMS
	reset := evaluatedAt - 1
	observation := quotaArbiterObservation("near-max-clock", QuotaSourceWham, 12, observedAt, reset)
	projection, err := arbitrateQuotaWindow(
		[]QuotaObservation{observation}, evaluatedAt, defaultQuotaArbitrationRule(),
	)
	if err != nil {
		t.Fatalf("arbitrateQuotaWindow() error = %v", err)
	}
	if projection.Current.ObservationID == nil || *projection.Current.ObservationID != observation.ObservationID ||
		projection.Current.FreshnessState != QuotaCurrentExpiredUnknown {
		t.Fatalf("near-max current = %#v", projection.Current)
	}
}

func TestQuotaArbiterRejectsZeroGenerationWhenLaterLocalSnapshotConflicts(t *testing.T) {
	t.Parallel()

	rule := defaultQuotaArbitrationRule()
	previousReset := int64(10 * quotaTestHourMS)
	previous := quotaArbiterObservation(
		"previous-local", QuotaSourceLocalJSONL, 52, previousReset-quotaTestHourMS, previousReset,
	)
	newObserved := previousReset - rule.MaxClockSkewMS/2
	newReset := newObserved + 5*quotaTestHourMS
	zero := quotaArbiterObservation("new-zero", QuotaSourceWham, 0, newObserved, newReset)
	laterLocal := quotaArbiterObservation(
		"later-old-local", QuotaSourceLocalJSONL, 53, newObserved+1, previousReset,
	)

	projection, err := arbitrateQuotaWindow(
		[]QuotaObservation{previous, zero, laterLocal}, newObserved+2, rule,
	)
	if err != nil {
		t.Fatalf("arbitrateQuotaWindow() error = %v", err)
	}
	if projection.Current.ObservationID == nil || *projection.Current.ObservationID != laterLocal.ObservationID ||
		projection.Current.EffectiveUsedPercent == nil || *projection.Current.EffectiveUsedPercent != 53 ||
		projection.Current.FreshnessState != QuotaCurrentSuspicious {
		t.Fatalf("current = %#v, want latest Local LKG after conflicting zero", projection.Current)
	}
	assertQuotaEvidence(t, projection.Evidence, zero.ObservationID, QuotaEvidenceSuspicious, QuotaReasonDefaultFallback)
}

func TestQuotaArbiterRejectsFirstSeenZeroGenerationInFavorOfLaterLocalWindow(t *testing.T) {
	t.Parallel()

	rule := defaultQuotaArbitrationRule()
	localReset := int64(10 * quotaTestHourMS)
	zeroObserved := localReset - rule.MaxClockSkewMS/2
	zeroReset := zeroObserved + 5*quotaTestHourMS
	zero := quotaArbiterObservation("first-seen-zero", QuotaSourceWham, 0, zeroObserved, zeroReset)
	laterLocal := quotaArbiterObservation(
		"first-seen-local", QuotaSourceLocalJSONL, 53, zeroObserved+1, localReset,
	)

	projection, err := arbitrateQuotaWindow([]QuotaObservation{zero, laterLocal}, zeroObserved+2, rule)
	if err != nil {
		t.Fatalf("arbitrateQuotaWindow() error = %v", err)
	}
	if projection.Current.ObservationID == nil || *projection.Current.ObservationID != laterLocal.ObservationID ||
		projection.Current.EffectiveUsedPercent == nil || *projection.Current.EffectiveUsedPercent != 53 ||
		projection.Current.FreshnessState != QuotaCurrentSuspicious {
		t.Fatalf("current = %#v, want later Local LKG and no false zero", projection.Current)
	}
	assertQuotaEvidence(t, projection.Evidence, zero.ObservationID, QuotaEvidenceSuspicious, QuotaReasonDefaultFallback)
}

func TestQuotaArbiterSelectsLatestSameGenerationUsageDecrease(t *testing.T) {
	t.Parallel()

	rule := defaultQuotaArbitrationRule()
	firstReset := int64(6 * quotaTestHourMS)
	first := quotaArbiterObservation("wham-45", QuotaSourceWham, 45, 5*quotaTestHourMS, firstReset)
	regressed := quotaArbiterObservation("wham-0-same", QuotaSourceWham, 0, 5*quotaTestHourMS+quotaTestMinuteMS, firstReset)

	projection, err := arbitrateQuotaWindow(
		[]QuotaObservation{first, regressed}, 5*quotaTestHourMS+2*quotaTestMinuteMS, rule,
	)
	if err != nil {
		t.Fatalf("same-generation arbitration error = %v", err)
	}
	if projection.Current.ObservationID == nil || *projection.Current.ObservationID != regressed.ObservationID ||
		projection.Current.EffectiveUsedPercent == nil || *projection.Current.EffectiveUsedPercent != 0 ||
		projection.Current.FreshnessState != QuotaCurrentFresh {
		t.Fatalf("same-generation current = %#v, want latest observed value", projection.Current)
	}
	assertQuotaEvidence(t, projection.Evidence, first.ObservationID, QuotaEvidenceEligible, "")
	assertQuotaEvidence(t, projection.Evidence, regressed.ObservationID, QuotaEvidenceSelected, "")

	secondObserved := firstReset + 3_000
	secondReset := secondObserved + 5*quotaTestHourMS
	newGeneration := quotaArbiterObservation("wham-0-new", QuotaSourceWham, 0, secondObserved, secondReset)
	projection, err = arbitrateQuotaWindow(
		[]QuotaObservation{first, regressed, newGeneration}, secondObserved+quotaTestMinuteMS, rule,
	)
	if err != nil {
		t.Fatalf("new-generation arbitration error = %v", err)
	}
	if projection.Current.ObservationID == nil || *projection.Current.ObservationID != newGeneration.ObservationID ||
		projection.Current.EffectiveUsedPercent == nil || *projection.Current.EffectiveUsedPercent != 0 ||
		projection.Current.WindowGeneration == nil || *projection.Current.WindowGeneration != secondReset ||
		projection.Current.FreshnessState != QuotaCurrentFresh {
		t.Fatalf("new-generation current = %#v, want trusted zero", projection.Current)
	}
}

func TestQuotaArbiterKeepsEverySameGenerationUsageChangeEligible(t *testing.T) {
	t.Parallel()

	rule := defaultQuotaArbitrationRule()
	observedAt := int64(260 * quotaTestHourMS)
	resetAt := observedAt + 7*24*quotaTestHourMS
	peak := quotaArbiterObservation("wham-peak-61", QuotaSourceWham, 61, observedAt, resetAt)
	onePoint := quotaArbiterObservation(
		"wham-rounded-60", QuotaSourceWham, 60, observedAt+quotaTestMinuteMS, resetAt,
	)
	boundary := quotaArbiterObservation(
		"wham-rounded-59", QuotaSourceWham, 59, observedAt+2*quotaTestMinuteMS, resetAt,
	)
	beyond := quotaArbiterObservation(
		"wham-regressed-58", QuotaSourceWham, 58, observedAt+3*quotaTestMinuteMS, resetAt,
	)
	for _, observation := range []*QuotaObservation{&peak, &onePoint, &boundary, &beyond} {
		observation.WindowMinutes = quotaWeeklyWindowMinutes
	}

	projection, err := arbitrateQuotaWindow(
		[]QuotaObservation{peak, onePoint, boundary, beyond},
		observedAt+4*quotaTestMinuteMS,
		rule,
	)
	if err != nil {
		t.Fatalf("bounded used regression arbitration error = %v", err)
	}
	if projection.Current.ObservationID == nil ||
		*projection.Current.ObservationID != beyond.ObservationID ||
		projection.Current.EffectiveUsedPercent == nil ||
		*projection.Current.EffectiveUsedPercent != beyond.UsedPercent ||
		projection.Current.FreshnessState != QuotaCurrentFresh ||
		projection.Current.RuleVersion != "quota-arbiter-v6" {
		t.Fatalf("usage change current = %#v, want latest observed value", projection.Current)
	}
	assertQuotaEvidence(t, projection.Evidence, peak.ObservationID, QuotaEvidenceEligible, "")
	assertQuotaEvidence(t, projection.Evidence, onePoint.ObservationID, QuotaEvidenceEligible, "")
	assertQuotaEvidence(t, projection.Evidence, boundary.ObservationID, QuotaEvidenceEligible, "")
	assertQuotaEvidence(t, projection.Evidence, beyond.ObservationID, QuotaEvidenceSelected, "")
}

func TestQuotaArbiterToleratesOneSecondResetTimestampJitter(t *testing.T) {
	t.Parallel()

	rule := defaultQuotaArbitrationRule()
	observedAt := int64(300 * quotaTestHourMS)
	resetAt := observedAt + 7*24*quotaTestHourMS + quotaTestMinuteMS
	first := quotaArbiterObservation("wham-reset-19", QuotaSourceWham, 5, observedAt, resetAt)
	first.WindowMinutes = 7 * 24 * 60
	jittered := quotaArbiterObservation(
		"wham-reset-18", QuotaSourceWham, 6, observedAt+quotaTestMinuteMS, resetAt-1_000,
	)
	jittered.WindowMinutes = 7 * 24 * 60

	projection, err := arbitrateQuotaWindow(
		[]QuotaObservation{first, jittered}, observedAt+2*quotaTestMinuteMS, rule,
	)
	if err != nil {
		t.Fatalf("reset jitter arbitration error = %v", err)
	}
	if projection.Current.ObservationID == nil ||
		*projection.Current.ObservationID != jittered.ObservationID ||
		projection.Current.EffectiveUsedPercent == nil ||
		*projection.Current.EffectiveUsedPercent != jittered.UsedPercent ||
		projection.Current.ResetsAtMS == nil ||
		*projection.Current.ResetsAtMS != resetAt ||
		projection.Current.FreshnessState != QuotaCurrentFresh {
		t.Fatalf("reset jitter current = %#v, want latest trusted observation", projection.Current)
	}
	assertQuotaEvidence(t, projection.Evidence, jittered.ObservationID, QuotaEvidenceSelected, "")
}

func TestQuotaResetsEquivalentUsesBoundedJitter(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		left  int64
		right int64
		want  bool
	}{
		{name: "same_timestamp", left: 10_000, right: 10_000, want: true},
		{name: "one_second", left: 10_000, right: 9_000, want: true},
		{name: "five_second_boundary", left: 10_000, right: 5_000, want: true},
		{name: "five_second_boundary_reversed", left: 5_000, right: 10_000, want: true},
		{name: "beyond_boundary", left: 10_000, right: 4_999, want: false},
		{name: "negative_timestamp", left: -1, right: -1, want: false},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			if got := QuotaResetsEquivalent(test.left, test.right); got != test.want {
				t.Fatalf(
					"QuotaResetsEquivalent(%d, %d) = %t, want %t",
					test.left,
					test.right,
					got,
					test.want,
				)
			}
		})
	}
}

func TestQuotaResetsEquivalentForWindowUsesWhamWeeklyBoundaryOnly(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		source        QuotaSource
		windowMinutes int64
		differenceMS  int64
		want          bool
	}{
		{
			name: "wham_weekly_120_second_boundary", source: QuotaSourceWham,
			windowMinutes: quotaWeeklyWindowMinutes, differenceMS: quotaWhamWeeklyResetJitterMS, want: true,
		},
		{
			name: "wham_weekly_beyond_120_seconds", source: QuotaSourceWham,
			windowMinutes: quotaWeeklyWindowMinutes, differenceMS: quotaWhamWeeklyResetJitterMS + 1, want: false,
		},
		{
			name: "wham_five_hour_keeps_default_boundary", source: QuotaSourceWham,
			windowMinutes: 300, differenceMS: quotaResetJitterMS + 1, want: false,
		},
		{
			name: "local_weekly_keeps_default_boundary", source: QuotaSourceLocalJSONL,
			windowMinutes: quotaWeeklyWindowMinutes, differenceMS: quotaResetJitterMS + 1, want: false,
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			if got := QuotaResetsEquivalentForWindow(
				test.source,
				test.windowMinutes,
				1_000_000,
				1_000_000-test.differenceMS,
			); got != test.want {
				t.Fatalf("QuotaResetsEquivalentForWindow() = %t, want %t", got, test.want)
			}
		})
	}
}

// 测试 QuotaArbiter 在同一窗口 reset_at 出现有界多秒抖动时继续选择最新可信值。（风险复现用例）
func TestQuotaArbiterToleratesBoundedMultiSecondResetTimestampJitter(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		jitterMS int64
	}{
		{name: "two_seconds_observed_in_real_responses", jitterMS: 2_000},
		{name: "five_seconds_default_boundary", jitterMS: 5_000},
		{name: "110_second_weekly_correction", jitterMS: 110_000},
		{name: "120_second_weekly_boundary", jitterMS: quotaWhamWeeklyResetJitterMS},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			rule := defaultQuotaArbitrationRule()
			observedAt := int64(320 * quotaTestHourMS)
			resetAt := observedAt + 7*24*quotaTestHourMS
			first := quotaArbiterObservation("wham-reset-base", QuotaSourceWham, 5, observedAt, resetAt)
			first.WindowMinutes = 7 * 24 * 60
			peak := quotaArbiterObservation(
				"wham-reset-peak",
				QuotaSourceWham,
				6,
				observedAt+quotaTestMinuteMS,
				resetAt+test.jitterMS,
			)
			peak.WindowMinutes = 7 * 24 * 60
			latest := quotaArbiterObservation(
				"wham-reset-latest",
				QuotaSourceWham,
				7,
				observedAt+2*quotaTestMinuteMS,
				resetAt,
			)
			latest.WindowMinutes = 7 * 24 * 60

			projection, err := arbitrateQuotaWindow(
				[]QuotaObservation{first, peak, latest},
				observedAt+3*quotaTestMinuteMS,
				rule,
			)
			if err != nil {
				t.Fatalf("bounded reset jitter arbitration error = %v", err)
			}
			if projection.Current.ObservationID == nil ||
				*projection.Current.ObservationID != latest.ObservationID ||
				projection.Current.EffectiveUsedPercent == nil ||
				*projection.Current.EffectiveUsedPercent != latest.UsedPercent ||
				projection.Current.ResetsAtMS == nil ||
				*projection.Current.ResetsAtMS != resetAt+test.jitterMS ||
				projection.Current.FreshnessState != QuotaCurrentFresh {
				t.Fatalf("bounded reset jitter current = %#v, want latest trusted observation", projection.Current)
			}
			assertQuotaEvidence(t, projection.Evidence, latest.ObservationID, QuotaEvidenceSelected, "")
		})
	}
}

// 测试 QuotaArbiter 不会把超过放宽边界的 reset_at 回退误归为同一窗口。（风险复现用例）
func TestQuotaArbiterRejectsResetTimestampRegressionBeyondRelaxedBoundary(t *testing.T) {
	t.Parallel()

	rule := defaultQuotaArbitrationRule()
	observedAt := int64(340 * quotaTestHourMS)
	resetAt := observedAt + 7*24*quotaTestHourMS - quotaTestMinuteMS
	peak := quotaArbiterObservation(
		"wham-reset-peak", QuotaSourceWham, 6, observedAt, resetAt+quotaWhamWeeklyResetJitterMS+1,
	)
	peak.WindowMinutes = 7 * 24 * 60
	regressed := quotaArbiterObservation(
		"wham-reset-regressed", QuotaSourceWham, 7, observedAt+quotaTestMinuteMS, resetAt,
	)
	regressed.WindowMinutes = 7 * 24 * 60

	projection, err := arbitrateQuotaWindow(
		[]QuotaObservation{peak, regressed}, observedAt+2*quotaTestMinuteMS, rule,
	)
	if err != nil {
		t.Fatalf("reset regression arbitration error = %v", err)
	}
	if projection.Current.ObservationID == nil ||
		*projection.Current.ObservationID != peak.ObservationID ||
		projection.Current.ResetsAtMS == nil ||
		*projection.Current.ResetsAtMS != peak.ResetsAtMS ||
		projection.Current.FreshnessState != QuotaCurrentSuspicious {
		t.Fatalf("reset regression current = %#v, want last known good marked suspicious", projection.Current)
	}
	assertQuotaEvidence(
		t,
		projection.Evidence,
		regressed.ObservationID,
		QuotaEvidenceSuspicious,
		QuotaReasonResetRegression,
	)
}

func TestQuotaArbiterDoesNotChainWhamWeeklyResetJitter(t *testing.T) {
	t.Parallel()

	rule := defaultQuotaArbitrationRule()
	observedAt := int64(360 * quotaTestHourMS)
	resetAt := observedAt + 7*24*quotaTestHourMS
	first := quotaArbiterObservation("wham-anchor", QuotaSourceWham, 10, observedAt, resetAt)
	within := quotaArbiterObservation(
		"wham-within-anchor", QuotaSourceWham, 11,
		observedAt+quotaTestMinuteMS, resetAt+100_000,
	)
	beyondAnchor := quotaArbiterObservation(
		"wham-beyond-anchor", QuotaSourceWham, 0,
		observedAt+2*quotaTestMinuteMS, resetAt+180_000,
	)
	for _, observation := range []*QuotaObservation{&first, &within, &beyondAnchor} {
		observation.WindowMinutes = quotaWeeklyWindowMinutes
	}

	projection, err := arbitrateQuotaWindow(
		[]QuotaObservation{first, within, beyondAnchor}, observedAt+3*quotaTestMinuteMS, rule,
	)
	if err != nil {
		t.Fatalf("non-chained reset jitter arbitration error = %v", err)
	}
	if projection.Current.ObservationID == nil ||
		*projection.Current.ObservationID != beyondAnchor.ObservationID ||
		projection.Current.EffectiveUsedPercent == nil ||
		*projection.Current.EffectiveUsedPercent != 0 ||
		projection.Current.WindowGeneration == nil ||
		*projection.Current.WindowGeneration != beyondAnchor.ResetsAtMS {
		t.Fatalf("non-chained reset jitter current = %#v, want distinct generation", projection.Current)
	}
}

// 测试 QuotaArbiter 在 7 天滑动窗口提前刷新 reset_at 场景下接受服务端已重置的新事实。（风险复现用例）
func TestQuotaArbiterAcceptsEarlySlidingWindowReset(t *testing.T) {
	t.Parallel()

	rule := defaultQuotaArbitrationRule()
	firstObserved := int64(200 * quotaTestHourMS)
	firstReset := firstObserved + 7*24*quotaTestHourMS
	exhausted := quotaArbiterObservation(
		"wham-exhausted-sliding-window", QuotaSourceWham, 100, firstObserved, firstReset,
	)
	exhausted.WindowMinutes = 7 * 24 * 60
	resetObserved := firstObserved + 4*quotaTestHourMS
	resetAt := resetObserved + 7*24*quotaTestHourMS
	reset := quotaArbiterObservation(
		"wham-reset-sliding-window", QuotaSourceWham, 0, resetObserved, resetAt,
	)
	reset.WindowMinutes = 7 * 24 * 60

	projection, err := arbitrateQuotaWindow(
		[]QuotaObservation{exhausted, reset}, resetObserved+quotaTestMinuteMS, rule,
	)
	if err != nil {
		t.Fatalf("sliding-window arbitration error = %v", err)
	}
	if projection.Current.ObservationID == nil || *projection.Current.ObservationID != reset.ObservationID ||
		projection.Current.EffectiveUsedPercent == nil || *projection.Current.EffectiveUsedPercent != 0 ||
		projection.Current.WindowGeneration == nil || *projection.Current.WindowGeneration != resetAt ||
		projection.Current.FreshnessState != QuotaCurrentFresh ||
		projection.Current.RuleVersion != "quota-arbiter-v6" {
		t.Fatalf("sliding-window current = %#v, want trusted reset observation", projection.Current)
	}
	assertQuotaEvidence(t, projection.Evidence, exhausted.ObservationID, QuotaEvidenceSuperseded, "")
	assertQuotaEvidence(t, projection.Evidence, reset.ObservationID, QuotaEvidenceSelected, "")
}

// 测试 QuotaArbiter 在重置卡触发周窗口换代后，接受服务端对临时 reset_at 的稳定回拨。
func TestQuotaArbiterReanchorsProvisionalWeeklyResetAfterStableCorrection(t *testing.T) {
	t.Parallel()

	rule := defaultQuotaArbitrationRule()
	firstObserved := int64(700 * quotaTestHourMS)
	exhausted := quotaArbiterObservation(
		"wham-exhausted-before-reset-credit",
		QuotaSourceWham,
		100,
		firstObserved,
		firstObserved+3*24*quotaTestHourMS,
	)
	exhausted.WindowMinutes = quotaWeeklyWindowMinutes

	resetObserved := firstObserved + 4*quotaTestHourMS
	provisionalReset := resetObserved + 7*24*quotaTestHourMS
	provisional := quotaArbiterObservation(
		"wham-reset-credit-provisional",
		QuotaSourceWham,
		0,
		resetObserved,
		provisionalReset,
	)
	provisional.WindowMinutes = quotaWeeklyWindowMinutes

	stableReset := provisionalReset - 44*quotaTestMinuteMS - 8_000
	firstCorrection := quotaArbiterObservation(
		"wham-reset-credit-correction-first",
		QuotaSourceWham,
		0,
		resetObserved+4*quotaTestMinuteMS+21_000,
		stableReset,
	)
	secondCorrection := quotaArbiterObservation(
		"wham-reset-credit-correction-confirmed",
		QuotaSourceWham,
		0,
		resetObserved+9*quotaTestMinuteMS+23_000,
		stableReset,
	)
	firstCorrection.WindowMinutes = quotaWeeklyWindowMinutes
	secondCorrection.WindowMinutes = quotaWeeklyWindowMinutes

	observations := []QuotaObservation{exhausted, provisional, firstCorrection, secondCorrection}
	projection, err := arbitrateQuotaWindow(
		observations,
		resetObserved+10*quotaTestMinuteMS,
		rule,
	)
	if err != nil {
		t.Fatalf("provisional weekly reset correction arbitration error = %v", err)
	}
	if projection.Current.ObservationID == nil ||
		*projection.Current.ObservationID != secondCorrection.ObservationID ||
		projection.Current.EffectiveUsedPercent == nil ||
		*projection.Current.EffectiveUsedPercent != 0 ||
		projection.Current.ResetsAtMS == nil ||
		*projection.Current.ResetsAtMS != stableReset ||
		projection.Current.FreshnessState != QuotaCurrentFresh ||
		projection.Current.RuleVersion != "quota-arbiter-v6" {
		t.Fatalf("corrected weekly reset current = %#v, want latest confirmed stable reset", projection.Current)
	}
	assertQuotaEvidence(t, projection.Evidence, exhausted.ObservationID, QuotaEvidenceSuperseded, "")
	assertQuotaEvidence(t, projection.Evidence, provisional.ObservationID, QuotaEvidenceEligible, "")
	assertQuotaEvidence(t, projection.Evidence, firstCorrection.ObservationID, QuotaEvidenceEligible, "")
	assertQuotaEvidence(t, projection.Evidence, secondCorrection.ObservationID, QuotaEvidenceSelected, "")

	for seed := int64(0); seed < 50; seed++ {
		shuffled := append([]QuotaObservation(nil), observations...)
		rand.New(rand.NewSource(seed)).Shuffle(len(shuffled), func(left, right int) {
			shuffled[left], shuffled[right] = shuffled[right], shuffled[left]
		})
		got, shuffleErr := arbitrateQuotaWindow(
			shuffled,
			resetObserved+10*quotaTestMinuteMS,
			rule,
		)
		if shuffleErr != nil {
			t.Fatalf("seed %d corrected weekly reset arbitration error = %v", seed, shuffleErr)
		}
		if !reflect.DeepEqual(got, projection) {
			t.Fatalf("seed %d corrected weekly reset projection differs\n got=%#v\nwant=%#v", seed, got, projection)
		}
	}
}

func TestQuotaArbiterReanchorsFirstObservedProvisionalWeeklyReset(t *testing.T) {
	t.Parallel()

	rule := defaultQuotaArbitrationRule()
	resetObserved := int64(720 * quotaTestHourMS)
	provisionalReset := resetObserved + 7*24*quotaTestHourMS
	stableReset := provisionalReset - 44*quotaTestMinuteMS
	provisional := quotaArbiterObservation(
		"wham-first-weekly-provisional", QuotaSourceWham, 0, resetObserved, provisionalReset,
	)
	firstCorrection := quotaArbiterObservation(
		"wham-first-weekly-correction-first", QuotaSourceWham, 0,
		resetObserved+4*quotaTestMinuteMS, stableReset,
	)
	confirmedCorrection := quotaArbiterObservation(
		"wham-first-weekly-correction-confirmed", QuotaSourceWham, 0,
		resetObserved+9*quotaTestMinuteMS, stableReset,
	)
	for _, observation := range []*QuotaObservation{&provisional, &firstCorrection, &confirmedCorrection} {
		observation.WindowMinutes = quotaWeeklyWindowMinutes
	}

	projection, err := arbitrateQuotaWindow(
		[]QuotaObservation{provisional, firstCorrection, confirmedCorrection},
		resetObserved+10*quotaTestMinuteMS,
		rule,
	)
	if err != nil {
		t.Fatalf("first weekly provisional correction arbitration error = %v", err)
	}
	if projection.Current.ObservationID == nil ||
		*projection.Current.ObservationID != confirmedCorrection.ObservationID ||
		projection.Current.ResetsAtMS == nil || *projection.Current.ResetsAtMS != stableReset ||
		projection.Current.FreshnessState != QuotaCurrentFresh {
		t.Fatalf("first weekly corrected current = %#v, want confirmed stable reset", projection.Current)
	}
}

func TestQuotaArbiterReanchorsProvisionalWeeklyResetAfterWindowRoleSwitch(t *testing.T) {
	t.Parallel()

	rule := defaultQuotaArbitrationRule()
	firstObserved := int64(740 * quotaTestHourMS)
	fiveHour := quotaArbiterObservation(
		"wham-role-switch-five-hour", QuotaSourceWham, 35,
		firstObserved, firstObserved+5*quotaTestHourMS,
	)
	resetObserved := firstObserved + quotaTestMinuteMS
	provisionalReset := resetObserved + 7*24*quotaTestHourMS
	stableReset := provisionalReset - 44*quotaTestMinuteMS
	provisional := quotaArbiterObservation(
		"wham-role-switch-weekly-provisional", QuotaSourceWham, 0,
		resetObserved, provisionalReset,
	)
	firstCorrection := quotaArbiterObservation(
		"wham-role-switch-weekly-correction-first", QuotaSourceWham, 0,
		resetObserved+4*quotaTestMinuteMS, stableReset,
	)
	confirmedCorrection := quotaArbiterObservation(
		"wham-role-switch-weekly-correction-confirmed", QuotaSourceWham, 0,
		resetObserved+9*quotaTestMinuteMS, stableReset,
	)
	for _, observation := range []*QuotaObservation{&provisional, &firstCorrection, &confirmedCorrection} {
		observation.WindowMinutes = quotaWeeklyWindowMinutes
	}

	projection, err := arbitrateQuotaWindow(
		[]QuotaObservation{fiveHour, provisional, firstCorrection, confirmedCorrection},
		resetObserved+10*quotaTestMinuteMS,
		rule,
	)
	if err != nil {
		t.Fatalf("role-switch weekly provisional correction arbitration error = %v", err)
	}
	if projection.Current.ObservationID == nil ||
		*projection.Current.ObservationID != confirmedCorrection.ObservationID ||
		projection.Current.WindowMinutes == nil ||
		*projection.Current.WindowMinutes != quotaWeeklyWindowMinutes ||
		projection.Current.ResetsAtMS == nil || *projection.Current.ResetsAtMS != stableReset ||
		projection.Current.FreshnessState != QuotaCurrentFresh {
		t.Fatalf("role-switch corrected current = %#v, want confirmed weekly stable reset", projection.Current)
	}
}

// 测试 QuotaArbiter 不会仅凭一次大幅回拨改写刚建立的周窗口代际。
func TestQuotaArbiterKeepsProvisionalWeeklyResetAfterSingleCorrection(t *testing.T) {
	t.Parallel()

	rule := defaultQuotaArbitrationRule()
	firstObserved := int64(740 * quotaTestHourMS)
	exhausted := quotaArbiterObservation(
		"wham-single-correction-exhausted",
		QuotaSourceWham,
		100,
		firstObserved,
		firstObserved+3*24*quotaTestHourMS,
	)
	exhausted.WindowMinutes = quotaWeeklyWindowMinutes

	resetObserved := firstObserved + 4*quotaTestHourMS
	provisionalReset := resetObserved + 7*24*quotaTestHourMS
	provisional := quotaArbiterObservation(
		"wham-single-correction-provisional",
		QuotaSourceWham,
		0,
		resetObserved,
		provisionalReset,
	)
	provisional.WindowMinutes = quotaWeeklyWindowMinutes
	correction := quotaArbiterObservation(
		"wham-single-correction-outlier",
		QuotaSourceWham,
		0,
		resetObserved+5*quotaTestMinuteMS,
		provisionalReset-45*quotaTestMinuteMS,
	)
	correction.WindowMinutes = quotaWeeklyWindowMinutes

	projection, err := arbitrateQuotaWindow(
		[]QuotaObservation{exhausted, provisional, correction},
		resetObserved+6*quotaTestMinuteMS,
		rule,
	)
	if err != nil {
		t.Fatalf("single weekly reset correction arbitration error = %v", err)
	}
	if projection.Current.ObservationID == nil ||
		*projection.Current.ObservationID != provisional.ObservationID ||
		projection.Current.ResetsAtMS == nil ||
		*projection.Current.ResetsAtMS != provisionalReset ||
		projection.Current.FreshnessState != QuotaCurrentSuspicious {
		t.Fatalf("single correction current = %#v, want provisional LKG marked suspicious", projection.Current)
	}
	assertQuotaEvidence(
		t,
		projection.Evidence,
		correction.ObservationID,
		QuotaEvidenceSuspicious,
		QuotaReasonResetRegression,
	)
}

// 测试 QuotaArbiter 要求稳定回拨连续出现，不能跨过另一个不一致 reset 拼出确认次数。
func TestQuotaArbiterRequiresConsecutiveStableWeeklyResetCorrections(t *testing.T) {
	t.Parallel()

	rule := defaultQuotaArbitrationRule()
	firstObserved := int64(760 * quotaTestHourMS)
	previousReset := firstObserved + 3*24*quotaTestHourMS
	exhausted := quotaArbiterObservation(
		"wham-nonconsecutive-correction-exhausted",
		QuotaSourceWham,
		100,
		firstObserved,
		previousReset,
	)
	exhausted.WindowMinutes = quotaWeeklyWindowMinutes

	resetObserved := firstObserved + 4*quotaTestHourMS
	provisionalReset := resetObserved + 7*24*quotaTestHourMS
	provisional := quotaArbiterObservation(
		"wham-nonconsecutive-correction-provisional",
		QuotaSourceWham,
		0,
		resetObserved,
		provisionalReset,
	)
	provisional.WindowMinutes = quotaWeeklyWindowMinutes
	stableReset := provisionalReset - 44*quotaTestMinuteMS
	firstCorrection := quotaArbiterObservation(
		"wham-nonconsecutive-correction-first",
		QuotaSourceWham,
		0,
		resetObserved+4*quotaTestMinuteMS,
		stableReset,
	)
	inconsistent := quotaArbiterObservation(
		"wham-nonconsecutive-correction-inconsistent",
		QuotaSourceWham,
		0,
		resetObserved+6*quotaTestMinuteMS,
		stableReset-10*quotaTestMinuteMS,
	)
	repeated := quotaArbiterObservation(
		"wham-nonconsecutive-correction-repeated",
		QuotaSourceWham,
		0,
		resetObserved+9*quotaTestMinuteMS,
		stableReset,
	)
	for _, observation := range []*QuotaObservation{&firstCorrection, &inconsistent, &repeated} {
		observation.WindowMinutes = quotaWeeklyWindowMinutes
	}

	projection, err := arbitrateQuotaWindow(
		[]QuotaObservation{exhausted, provisional, firstCorrection, inconsistent, repeated},
		resetObserved+10*quotaTestMinuteMS,
		rule,
	)
	if err != nil {
		t.Fatalf("nonconsecutive weekly reset correction arbitration error = %v", err)
	}
	if projection.Current.ObservationID == nil ||
		*projection.Current.ObservationID != provisional.ObservationID ||
		projection.Current.ResetsAtMS == nil ||
		*projection.Current.ResetsAtMS != provisionalReset ||
		projection.Current.FreshnessState != QuotaCurrentSuspicious {
		t.Fatalf("nonconsecutive correction current = %#v, want provisional LKG", projection.Current)
	}
	assertQuotaEvidence(
		t,
		projection.Evidence,
		repeated.ObservationID,
		QuotaEvidenceSuspicious,
		QuotaReasonResetRegression,
	)
}

// 测试 QuotaArbiter 不会把重复到达的上一代 reset_at 当成新代际的稳定纠偏。
func TestQuotaArbiterRejectsRepeatedPreviousGenerationResetAsCorrection(t *testing.T) {
	t.Parallel()

	rule := defaultQuotaArbitrationRule()
	firstObserved := int64(780 * quotaTestHourMS)
	previousReset := firstObserved + 3*24*quotaTestHourMS
	exhausted := quotaArbiterObservation(
		"wham-previous-generation-exhausted",
		QuotaSourceWham,
		100,
		firstObserved,
		previousReset,
	)
	exhausted.WindowMinutes = quotaWeeklyWindowMinutes

	resetObserved := firstObserved + 4*quotaTestHourMS
	provisionalReset := resetObserved + 7*24*quotaTestHourMS
	provisional := quotaArbiterObservation(
		"wham-previous-generation-provisional",
		QuotaSourceWham,
		0,
		resetObserved,
		provisionalReset,
	)
	provisional.WindowMinutes = quotaWeeklyWindowMinutes
	firstLate := quotaArbiterObservation(
		"wham-previous-generation-late-first",
		QuotaSourceWham,
		100,
		resetObserved+4*quotaTestMinuteMS,
		previousReset,
	)
	secondLate := quotaArbiterObservation(
		"wham-previous-generation-late-second",
		QuotaSourceWham,
		100,
		resetObserved+9*quotaTestMinuteMS,
		previousReset,
	)
	firstLate.WindowMinutes = quotaWeeklyWindowMinutes
	secondLate.WindowMinutes = quotaWeeklyWindowMinutes

	projection, err := arbitrateQuotaWindow(
		[]QuotaObservation{exhausted, provisional, firstLate, secondLate},
		resetObserved+10*quotaTestMinuteMS,
		rule,
	)
	if err != nil {
		t.Fatalf("previous-generation correction arbitration error = %v", err)
	}
	if projection.Current.ObservationID == nil ||
		*projection.Current.ObservationID != provisional.ObservationID ||
		projection.Current.ResetsAtMS == nil ||
		*projection.Current.ResetsAtMS != provisionalReset ||
		projection.Current.FreshnessState != QuotaCurrentSuspicious {
		t.Fatalf("previous-generation correction current = %#v, want provisional LKG", projection.Current)
	}
	assertQuotaEvidence(
		t,
		projection.Evidence,
		secondLate.ObservationID,
		QuotaEvidenceSuspicious,
		QuotaReasonResetRegression,
	)
}

func TestQuotaArbiterRejectsPreviousGenerationResetJitterAsCorrection(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		firstJitterMS  int64
		secondJitterMS int64
		wantReanchor   bool
	}{
		{name: "one second stays previous", firstJitterMS: 1_000, secondJitterMS: 1_000},
		{
			name:           "weekly jitter boundary stays previous",
			firstJitterMS:  quotaWhamWeeklyResetJitterMS,
			secondJitterMS: quotaWhamWeeklyResetJitterMS,
		},
		{
			name:           "mixed boundary cannot confirm previous jitter",
			firstJitterMS:  quotaWhamWeeklyResetJitterMS + 1,
			secondJitterMS: 1,
		},
		{
			name:           "outside weekly jitter is distinct",
			firstJitterMS:  quotaWhamWeeklyResetJitterMS + 1,
			secondJitterMS: quotaWhamWeeklyResetJitterMS + 1,
			wantReanchor:   true,
		},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			rule := defaultQuotaArbitrationRule()
			firstObserved := int64(800 * quotaTestHourMS)
			previousReset := firstObserved + 3*24*quotaTestHourMS
			exhausted := quotaArbiterObservation(
				"wham-previous-jitter-exhausted-"+testCase.name,
				QuotaSourceWham,
				100,
				firstObserved,
				previousReset,
			)
			exhausted.WindowMinutes = quotaWeeklyWindowMinutes

			resetObserved := firstObserved + 4*quotaTestHourMS
			provisionalReset := resetObserved + 7*24*quotaTestHourMS
			provisional := quotaArbiterObservation(
				"wham-previous-jitter-provisional-"+testCase.name,
				QuotaSourceWham,
				0,
				resetObserved,
				provisionalReset,
			)
			provisional.WindowMinutes = quotaWeeklyWindowMinutes
			firstLateReset := previousReset + testCase.firstJitterMS
			secondLateReset := previousReset + testCase.secondJitterMS
			firstLate := quotaArbiterObservation(
				"wham-previous-jitter-first-"+testCase.name,
				QuotaSourceWham,
				100,
				resetObserved+4*quotaTestMinuteMS,
				firstLateReset,
			)
			secondLate := quotaArbiterObservation(
				"wham-previous-jitter-second-"+testCase.name,
				QuotaSourceWham,
				100,
				resetObserved+9*quotaTestMinuteMS,
				secondLateReset,
			)
			firstLate.WindowMinutes = quotaWeeklyWindowMinutes
			secondLate.WindowMinutes = quotaWeeklyWindowMinutes

			projection, err := arbitrateQuotaWindow(
				[]QuotaObservation{exhausted, provisional, firstLate, secondLate},
				resetObserved+10*quotaTestMinuteMS,
				rule,
			)
			if err != nil {
				t.Fatalf("previous jitter correction arbitration error = %v", err)
			}
			if testCase.wantReanchor {
				if projection.Current.ObservationID == nil ||
					*projection.Current.ObservationID != secondLate.ObservationID ||
					projection.Current.ResetsAtMS == nil || *projection.Current.ResetsAtMS != secondLateReset ||
					projection.Current.FreshnessState != QuotaCurrentFresh {
					t.Fatalf("distinct correction current = %#v, want confirmed reset", projection.Current)
				}
				return
			}
			if projection.Current.ObservationID == nil ||
				*projection.Current.ObservationID != provisional.ObservationID ||
				projection.Current.ResetsAtMS == nil ||
				*projection.Current.ResetsAtMS != provisionalReset ||
				projection.Current.FreshnessState != QuotaCurrentSuspicious {
				t.Fatalf("previous jitter current = %#v, want provisional LKG", projection.Current)
			}
		})
	}
}

func TestQuotaArbiterRejectsReappearingProvisionalAliasAfterStableCorrection(t *testing.T) {
	t.Parallel()

	rule := defaultQuotaArbitrationRule()
	firstObserved := int64(840 * quotaTestHourMS)
	previousReset := firstObserved + 3*24*quotaTestHourMS
	exhausted := quotaArbiterObservation(
		"wham-provisional-alias-exhausted", QuotaSourceWham, 100, firstObserved, previousReset,
	)
	exhausted.WindowMinutes = quotaWeeklyWindowMinutes
	resetObserved := firstObserved + 4*quotaTestHourMS
	provisionalReset := resetObserved + 7*24*quotaTestHourMS
	stableReset := provisionalReset - 44*quotaTestMinuteMS
	provisional := quotaArbiterObservation(
		"wham-provisional-alias-initial", QuotaSourceWham, 0, resetObserved, provisionalReset,
	)
	firstCorrection := quotaArbiterObservation(
		"wham-provisional-alias-correction-first", QuotaSourceWham, 0,
		resetObserved+4*quotaTestMinuteMS, stableReset,
	)
	confirmedCorrection := quotaArbiterObservation(
		"wham-provisional-alias-correction-confirmed", QuotaSourceWham, 0,
		resetObserved+9*quotaTestMinuteMS, stableReset,
	)
	reappeared := quotaArbiterObservation(
		"wham-provisional-alias-reappeared", QuotaSourceWham, 0,
		resetObserved+12*quotaTestMinuteMS, provisionalReset,
	)
	stableAfterAlias := quotaArbiterObservation(
		"wham-provisional-alias-stable-after", QuotaSourceWham, 1,
		resetObserved+15*quotaTestMinuteMS, stableReset,
	)
	latestStable := quotaArbiterObservation(
		"wham-provisional-alias-stable-latest", QuotaSourceWham, 1,
		resetObserved+18*quotaTestMinuteMS, stableReset,
	)
	for _, observation := range []*QuotaObservation{
		&provisional,
		&firstCorrection,
		&confirmedCorrection,
		&reappeared,
		&stableAfterAlias,
		&latestStable,
	} {
		observation.WindowMinutes = quotaWeeklyWindowMinutes
	}

	projection, err := arbitrateQuotaWindow(
		[]QuotaObservation{
			exhausted,
			provisional,
			firstCorrection,
			confirmedCorrection,
			reappeared,
			stableAfterAlias,
			latestStable,
		},
		resetObserved+19*quotaTestMinuteMS,
		rule,
	)
	if err != nil {
		t.Fatalf("reappearing provisional alias arbitration error = %v", err)
	}
	if projection.Current.ObservationID == nil ||
		*projection.Current.ObservationID != latestStable.ObservationID ||
		projection.Current.EffectiveUsedPercent == nil ||
		*projection.Current.EffectiveUsedPercent != latestStable.UsedPercent ||
		projection.Current.ResetsAtMS == nil || *projection.Current.ResetsAtMS != stableReset ||
		projection.Current.FreshnessState != QuotaCurrentFresh {
		t.Fatalf("post-alias current = %#v, want latest stable correction", projection.Current)
	}
	assertQuotaEvidence(
		t,
		projection.Evidence,
		reappeared.ObservationID,
		QuotaEvidenceSuspicious,
		QuotaReasonResetRegression,
	)
}

func TestQuotaArbiterRejectsProvisionalAliasFromAnotherSource(t *testing.T) {
	t.Parallel()

	rule := defaultQuotaArbitrationRule()
	firstObserved := int64(880 * quotaTestHourMS)
	previousReset := firstObserved + 3*24*quotaTestHourMS
	exhausted := quotaArbiterObservation(
		"cross-source-alias-exhausted", QuotaSourceWham, 100, firstObserved, previousReset,
	)
	exhausted.WindowMinutes = quotaWeeklyWindowMinutes
	resetObserved := firstObserved + 4*quotaTestHourMS
	provisionalReset := resetObserved + 7*24*quotaTestHourMS
	stableReset := provisionalReset - 44*quotaTestMinuteMS
	provisional := quotaArbiterObservation(
		"cross-source-alias-provisional", QuotaSourceWham, 0, resetObserved, provisionalReset,
	)
	firstCorrection := quotaArbiterObservation(
		"cross-source-alias-correction-first", QuotaSourceWham, 0,
		resetObserved+4*quotaTestMinuteMS, stableReset,
	)
	confirmedCorrection := quotaArbiterObservation(
		"cross-source-alias-correction-confirmed", QuotaSourceWham, 0,
		resetObserved+9*quotaTestMinuteMS, stableReset,
	)
	reappeared := quotaArbiterObservation(
		"cross-source-alias-reappeared", QuotaSourceLocalJSONL, 0,
		resetObserved+12*quotaTestMinuteMS, provisionalReset,
	)
	latestStable := quotaArbiterObservation(
		"cross-source-alias-stable-latest", QuotaSourceWham, 1,
		resetObserved+15*quotaTestMinuteMS, stableReset,
	)
	for _, observation := range []*QuotaObservation{
		&provisional,
		&firstCorrection,
		&confirmedCorrection,
		&reappeared,
		&latestStable,
	} {
		observation.WindowMinutes = quotaWeeklyWindowMinutes
	}

	projection, err := arbitrateQuotaWindow(
		[]QuotaObservation{exhausted, provisional, firstCorrection, confirmedCorrection, reappeared, latestStable},
		resetObserved+16*quotaTestMinuteMS,
		rule,
	)
	if err != nil {
		t.Fatalf("cross-source provisional alias arbitration error = %v", err)
	}
	if projection.Current.ObservationID == nil ||
		*projection.Current.ObservationID != latestStable.ObservationID ||
		projection.Current.ResetsAtMS == nil || *projection.Current.ResetsAtMS != stableReset ||
		projection.Current.FreshnessState != QuotaCurrentFresh {
		t.Fatalf("cross-source post-alias current = %#v, want latest stable reset", projection.Current)
	}
	assertQuotaEvidence(
		t,
		projection.Evidence,
		reappeared.ObservationID,
		QuotaEvidenceSuspicious,
		QuotaReasonResetRegression,
	)
}

func TestQuotaArbiterKeepsProvisionalAliasRetiredWithWindowRole(t *testing.T) {
	t.Parallel()

	rule := defaultQuotaArbitrationRule()
	firstObserved := int64(920 * quotaTestHourMS)
	previousReset := firstObserved + 3*24*quotaTestHourMS
	exhausted := quotaArbiterObservation(
		"retired-alias-exhausted", QuotaSourceWham, 100, firstObserved, previousReset,
	)
	exhausted.WindowMinutes = quotaWeeklyWindowMinutes
	resetObserved := firstObserved + 4*quotaTestHourMS
	provisionalReset := resetObserved + 7*24*quotaTestHourMS
	stableReset := provisionalReset - 44*quotaTestMinuteMS
	provisional := quotaArbiterObservation(
		"retired-alias-provisional", QuotaSourceWham, 0, resetObserved, provisionalReset,
	)
	firstCorrection := quotaArbiterObservation(
		"retired-alias-correction-first", QuotaSourceWham, 0,
		resetObserved+4*quotaTestMinuteMS, stableReset,
	)
	confirmedCorrection := quotaArbiterObservation(
		"retired-alias-correction-confirmed", QuotaSourceWham, 0,
		resetObserved+9*quotaTestMinuteMS, stableReset,
	)
	for _, observation := range []*QuotaObservation{&provisional, &firstCorrection, &confirmedCorrection} {
		observation.WindowMinutes = quotaWeeklyWindowMinutes
	}
	fiveHourObserved := resetObserved + 12*quotaTestMinuteMS
	fiveHour := quotaArbiterObservation(
		"retired-alias-five-hour", QuotaSourceWham, 4,
		fiveHourObserved, fiveHourObserved+5*quotaTestHourMS,
	)
	lateAlias := quotaArbiterObservation(
		"retired-alias-reappeared", QuotaSourceWham, 0,
		resetObserved+15*quotaTestMinuteMS, provisionalReset,
	)
	lateAlias.WindowMinutes = quotaWeeklyWindowMinutes

	projection, err := arbitrateQuotaWindow(
		[]QuotaObservation{exhausted, provisional, firstCorrection, confirmedCorrection, fiveHour, lateAlias},
		resetObserved+16*quotaTestMinuteMS,
		rule,
	)
	if err != nil {
		t.Fatalf("retired provisional alias arbitration error = %v", err)
	}
	if projection.Current.ObservationID == nil ||
		*projection.Current.ObservationID != fiveHour.ObservationID ||
		projection.Current.WindowMinutes == nil || *projection.Current.WindowMinutes != fiveHour.WindowMinutes ||
		projection.Current.ResetsAtMS == nil || *projection.Current.ResetsAtMS != fiveHour.ResetsAtMS ||
		projection.Current.FreshnessState != QuotaCurrentFresh {
		t.Fatalf("retired alias current = %#v, want active five-hour role", projection.Current)
	}
	assertQuotaEvidence(t, projection.Evidence, lateAlias.ObservationID, QuotaEvidenceSuperseded, "")
}

// 测试 QuotaArbiter 在 primary/codex 经历 300→10080→300 分钟角色切换后选择恢复的 5 小时窗口，
// 并且不会被随后晚到的旧周窗口重新覆盖。（风险复现用例）
func TestQuotaArbiterRestoresFiveHourRoleWithoutRevivingRetiredWeeklyGeneration(t *testing.T) {
	t.Parallel()

	rule := defaultQuotaArbitrationRule()
	firstObserved := int64(400 * quotaTestHourMS)
	historicalFiveHour := quotaArbiterObservation(
		"historical-five-hour", QuotaSourceWham, 82,
		firstObserved, firstObserved+5*quotaTestHourMS,
	)
	temporaryWeekly := quotaArbiterObservation(
		"temporary-weekly-primary", QuotaSourceWham, 46,
		firstObserved+quotaTestMinuteMS, firstObserved+quotaTestMinuteMS+7*24*quotaTestHourMS,
	)
	temporaryWeekly.WindowMinutes = 7 * 24 * 60
	temporaryWeekly.LastObservedAtMS = firstObserved + 3*quotaTestMinuteMS
	temporaryWeekly.SampleCount = 3
	restoredFiveHour := quotaArbiterObservation(
		"restored-five-hour", QuotaSourceWham, 4,
		firstObserved+2*quotaTestMinuteMS, firstObserved+2*quotaTestMinuteMS+5*quotaTestHourMS,
	)
	lateTemporaryWeekly := quotaArbiterObservation(
		"late-temporary-weekly", QuotaSourceWham, 47,
		firstObserved+4*quotaTestMinuteMS, temporaryWeekly.ResetsAtMS,
	)
	lateTemporaryWeekly.WindowMinutes = temporaryWeekly.WindowMinutes
	observations := []QuotaObservation{
		historicalFiveHour,
		temporaryWeekly,
		restoredFiveHour,
		lateTemporaryWeekly,
	}

	want, err := arbitrateQuotaWindow(
		observations, firstObserved+5*quotaTestMinuteMS, rule,
	)
	if err != nil {
		t.Fatalf("role-switch arbitration error = %v", err)
	}
	if want.Current.ObservationID == nil ||
		*want.Current.ObservationID != restoredFiveHour.ObservationID ||
		want.Current.WindowMinutes == nil ||
		*want.Current.WindowMinutes != restoredFiveHour.WindowMinutes ||
		want.Current.ResetsAtMS == nil ||
		*want.Current.ResetsAtMS != restoredFiveHour.ResetsAtMS ||
		want.Current.FreshnessState != QuotaCurrentFresh ||
		want.Current.RuleVersion != "quota-arbiter-v6" {
		t.Fatalf("role-switch current = %#v, want restored five-hour observation", want.Current)
	}
	assertQuotaEvidence(
		t, want.Evidence, temporaryWeekly.ObservationID, QuotaEvidenceSuperseded, "",
	)
	assertQuotaEvidence(
		t, want.Evidence, restoredFiveHour.ObservationID, QuotaEvidenceSelected, "",
	)
	assertQuotaEvidence(
		t, want.Evidence, lateTemporaryWeekly.ObservationID, QuotaEvidenceSuperseded, "",
	)

	for seed := int64(0); seed < 100; seed++ {
		shuffled := append([]QuotaObservation(nil), observations...)
		rand.New(rand.NewSource(seed)).Shuffle(len(shuffled), func(i, j int) {
			shuffled[i], shuffled[j] = shuffled[j], shuffled[i]
		})
		got, err := arbitrateQuotaWindow(
			shuffled, firstObserved+5*quotaTestMinuteMS, rule,
		)
		if err != nil {
			t.Fatalf("seed %d role-switch arbitration error = %v", seed, err)
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("seed %d role-switch projection differs\n got=%#v\nwant=%#v", seed, got, want)
		}
	}
}

// 测试 QuotaArbiter 在退役周角色晚到且 reset 向后超过 120 秒时继续隔离回退。（风险复现用例）
func TestQuotaArbiterQuarantinesResetRegressionFromRetiredWindowRole(t *testing.T) {
	t.Parallel()

	rule := defaultQuotaArbitrationRule()
	firstObserved := int64(500 * quotaTestHourMS)
	temporaryWeekly := quotaArbiterObservation(
		"retired-weekly", QuotaSourceWham, 46,
		firstObserved, firstObserved+7*24*quotaTestHourMS,
	)
	temporaryWeekly.WindowMinutes = 7 * 24 * 60
	restoredFiveHour := quotaArbiterObservation(
		"five-hour-after-retired-weekly", QuotaSourceWham, 4,
		firstObserved+quotaTestMinuteMS, firstObserved+quotaTestMinuteMS+5*quotaTestHourMS,
	)
	regressedWeekly := quotaArbiterObservation(
		"regressed-retired-weekly", QuotaSourceWham, 47,
		firstObserved+2*quotaTestMinuteMS, temporaryWeekly.ResetsAtMS-quotaWhamWeeklyResetJitterMS-1,
	)
	regressedWeekly.WindowMinutes = temporaryWeekly.WindowMinutes

	projection, err := arbitrateQuotaWindow(
		[]QuotaObservation{temporaryWeekly, restoredFiveHour, regressedWeekly},
		firstObserved+3*quotaTestMinuteMS,
		rule,
	)
	if err != nil {
		t.Fatalf("retired-role reset regression arbitration error = %v", err)
	}
	if projection.Current.ObservationID == nil ||
		*projection.Current.ObservationID != restoredFiveHour.ObservationID ||
		projection.Current.WindowMinutes == nil ||
		*projection.Current.WindowMinutes != restoredFiveHour.WindowMinutes ||
		projection.Current.FreshnessState != QuotaCurrentSuspicious {
		t.Fatalf("retired-role reset regression current = %#v, want suspicious five-hour LKG", projection.Current)
	}
	assertQuotaEvidence(
		t,
		projection.Evidence,
		regressedWeekly.ObservationID,
		QuotaEvidenceSuspicious,
		QuotaReasonResetRegression,
	)
}

// 测试 QuotaArbiter 在 reset 相同但 observation 更新、且新时长自身合法时接受窗口角色切换。
func TestQuotaArbiterAcceptsNewerWindowRoleWithSharedReset(t *testing.T) {
	t.Parallel()

	rule := defaultQuotaArbitrationRule()
	firstObserved := int64(600 * quotaTestHourMS)
	sharedReset := firstObserved + 4*quotaTestHourMS
	weekly := quotaArbiterObservation(
		"weekly-before-shared-reset", QuotaSourceWham, 46, firstObserved, sharedReset,
	)
	weekly.WindowMinutes = 7 * 24 * 60
	fiveHour := quotaArbiterObservation(
		"five-hour-with-shared-reset", QuotaSourceWham, 4,
		firstObserved+quotaTestMinuteMS, sharedReset,
	)

	projection, err := arbitrateQuotaWindow(
		[]QuotaObservation{weekly, fiveHour}, firstObserved+2*quotaTestMinuteMS, rule,
	)
	if err != nil {
		t.Fatalf("shared-reset role-switch arbitration error = %v", err)
	}
	if projection.Current.ObservationID == nil ||
		*projection.Current.ObservationID != fiveHour.ObservationID ||
		projection.Current.WindowMinutes == nil ||
		*projection.Current.WindowMinutes != fiveHour.WindowMinutes ||
		projection.Current.FreshnessState != QuotaCurrentFresh {
		t.Fatalf("shared-reset role-switch current = %#v, want newer five-hour role", projection.Current)
	}
}

func TestQuotaArbiterRejectsClockAndResetAnomaliesWithoutLosingLastKnownGood(t *testing.T) {
	t.Parallel()

	rule := defaultQuotaArbitrationRule()
	now := int64(20 * quotaTestHourMS)
	reset := now + 5*quotaTestHourMS
	trusted := quotaArbiterObservation("local-trusted", QuotaSourceLocalJSONL, 38, now-quotaTestMinuteMS, reset)
	future := quotaArbiterObservation(
		"wham-future", QuotaSourceWham, 10, now+rule.MaxClockSkewMS+1, reset+5*quotaTestHourMS,
	)
	lateOld := quotaArbiterObservation("local-old-late", QuotaSourceLocalJSONL, 39, now+1, reset-quotaTestHourMS)

	projection, err := arbitrateQuotaWindow([]QuotaObservation{trusted, future, lateOld}, now, rule)
	if err != nil {
		t.Fatalf("arbitrateQuotaWindow() error = %v", err)
	}
	if projection.Current.ObservationID == nil || *projection.Current.ObservationID != trusted.ObservationID ||
		projection.Current.FreshnessState != QuotaCurrentSuspicious {
		t.Fatalf("current = %#v, want trusted LKG degraded to suspicious", projection.Current)
	}
	assertQuotaEvidence(t, projection.Evidence, future.ObservationID, QuotaEvidenceSuspicious, QuotaReasonObservedRegression)
	assertQuotaEvidence(t, projection.Evidence, lateOld.ObservationID, QuotaEvidenceSuspicious, QuotaReasonResetRegression)
}

func TestQuotaArbiterFreshnessTransitionsKeepLastKnownGoodAndConflictAxis(t *testing.T) {
	t.Parallel()

	rule := defaultQuotaArbitrationRule()
	observed := int64(100 * quotaTestHourMS)
	reset := observed + 5*quotaTestHourMS
	local := quotaArbiterObservation("local", QuotaSourceLocalJSONL, 45, observed, reset)
	wham := quotaArbiterObservation("wham", QuotaSourceWham, 41, observed+1, reset)

	tests := []struct {
		name      string
		evaluated int64
		freshness QuotaCurrentFreshness
	}{
		{name: "fresh boundary", evaluated: observed + rule.FreshForMS, freshness: QuotaCurrentFresh},
		{name: "stale", evaluated: observed + rule.FreshForMS + 1, freshness: QuotaCurrentStale},
		{name: "expired unknown", evaluated: reset, freshness: QuotaCurrentExpiredUnknown},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			projection, err := arbitrateQuotaWindow([]QuotaObservation{local, wham}, test.evaluated, rule)
			if err != nil {
				t.Fatalf("arbitrateQuotaWindow() error = %v", err)
			}
			if projection.Current.FreshnessState != test.freshness ||
				projection.Current.ConflictState != QuotaConflictPresent ||
				projection.Current.ObservationID == nil {
				t.Fatalf("current = %#v, want freshness=%s + conflict + LKG", projection.Current, test.freshness)
			}
		})
	}
}

func TestQuotaArbiterIsInputOrderIndependentAndNeverLoadsWithoutAcceptedFact(t *testing.T) {
	t.Parallel()

	rule := defaultQuotaArbitrationRule()
	now := int64(30 * quotaTestHourMS)
	reset := now + 5*quotaTestHourMS
	values := []QuotaObservation{
		quotaArbiterObservation("local-10", QuotaSourceLocalJSONL, 10, now-4, reset),
		quotaArbiterObservation("local-20", QuotaSourceLocalJSONL, 20, now-3, reset),
		quotaArbiterObservation("wham-19", QuotaSourceWham, 19, now-2, reset),
	}
	want, err := arbitrateQuotaWindow(values, now, rule)
	if err != nil {
		t.Fatalf("arbitrateQuotaWindow(want) error = %v", err)
	}
	for seed := int64(0); seed < 100; seed++ {
		shuffled := append([]QuotaObservation(nil), values...)
		rand.New(rand.NewSource(seed)).Shuffle(len(shuffled), func(i, j int) {
			shuffled[i], shuffled[j] = shuffled[j], shuffled[i]
		})
		got, err := arbitrateQuotaWindow(shuffled, now, rule)
		if err != nil {
			t.Fatalf("seed %d error = %v", seed, err)
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("seed %d projection differs\n got=%#v\nwant=%#v", seed, got, want)
		}
	}

	reason := QuotaReasonUnknownPlanType
	suspicious := quotaArbiterObservation("unknown-plan", QuotaSourceWham, 0, now, reset)
	suspicious.Validity = QuotaValiditySuspicious
	suspicious.RejectionReason = &reason
	projection, err := arbitrateQuotaWindow([]QuotaObservation{suspicious}, now, rule)
	if err != nil {
		t.Fatalf("arbitrateQuotaWindow(suspicious only) error = %v", err)
	}
	if projection.Current.ObservationID != nil || projection.Current.EffectiveUsedPercent != nil ||
		projection.Current.FreshnessState != QuotaCurrentNeverLoaded ||
		projection.Current.ExplanationCode != QuotaExplanationUnavailable {
		t.Fatalf("never-loaded current = %#v", projection.Current)
	}
	assertQuotaEvidence(t, projection.Evidence, suspicious.ObservationID, QuotaEvidenceSuspicious, reason)
}

func quotaArbiterObservation(id string, source QuotaSource, used float64, observedAt, reset int64) QuotaObservation {
	limitID := "codex"
	requestID := "request-" + id
	sourceFileID := "source-1"
	value := QuotaObservation{
		ObservationID: id, AccountScope: QuotaAccountScopeDefault, Source: source,
		LimitID: &limitID, WindowKind: QuotaWindowPrimary, UsedPercent: used,
		WindowMinutes: 300, ResetsAtMS: reset, Validity: QuotaValidityAccepted,
		FirstObservedAtMS: observedAt, LastObservedAtMS: observedAt, SampleCount: 1,
		FirstSourceGeneration: 1, SourceGeneration: 1, FirstSourceOffset: observedAt, SourceOffset: observedAt,
	}
	if source == QuotaSourceWham {
		value.RequestID = &requestID
	} else {
		value.SourceFileID = &sourceFileID
	}
	return value
}

func assertQuotaEvidence(
	t *testing.T,
	evidence []QuotaArbitrationEvidence,
	observationID string,
	disposition QuotaEvidenceDisposition,
	reason QuotaRejectionReason,
) {
	t.Helper()
	for _, item := range evidence {
		if item.ObservationID != observationID {
			continue
		}
		if item.Disposition != disposition ||
			(reason == "" && item.Reason != nil) ||
			(reason != "" && (item.Reason == nil || *item.Reason != reason)) {
			t.Fatalf("evidence[%s] = %#v, want %s/%s", observationID, item, disposition, reason)
		}
		return
	}
	t.Fatalf("evidence[%s] missing in %#v", observationID, evidence)
}
