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

func TestQuotaArbiterQuarantinesSameGenerationRegressionAndRecoversOnNewGeneration(t *testing.T) {
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
	if projection.Current.ObservationID == nil || *projection.Current.ObservationID != first.ObservationID ||
		projection.Current.FreshnessState != QuotaCurrentSuspicious {
		t.Fatalf("same-generation current = %#v, want LKG suspicious", projection.Current)
	}
	assertQuotaEvidence(t, projection.Evidence, regressed.ObservationID, QuotaEvidenceSuspicious, QuotaReasonUsedRegression)

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

// 测试 QuotaArbiter 在同一窗口 reset_at 出现有界多秒抖动时继续选择最新可信值。（风险复现用例）
func TestQuotaArbiterToleratesBoundedMultiSecondResetTimestampJitter(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		jitterMS int64
	}{
		{name: "two_seconds_observed_in_real_responses", jitterMS: 2_000},
		{name: "five_seconds_relaxed_boundary", jitterMS: 5_000},
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
	resetAt := observedAt + 7*24*quotaTestHourMS
	peak := quotaArbiterObservation(
		"wham-reset-peak", QuotaSourceWham, 6, observedAt, resetAt+quotaResetJitterMS+1,
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
		projection.Current.RuleVersion != "quota-arbiter-v4" {
		t.Fatalf("sliding-window current = %#v, want trusted reset observation", projection.Current)
	}
	assertQuotaEvidence(t, projection.Evidence, exhausted.ObservationID, QuotaEvidenceSuperseded, "")
	assertQuotaEvidence(t, projection.Evidence, reset.ObservationID, QuotaEvidenceSelected, "")
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
		want.Current.RuleVersion != "quota-arbiter-v4" {
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

// 测试 QuotaArbiter 在退役周角色晚到且 reset 向后超过 5 秒时继续隔离回退。（风险复现用例）
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
		firstObserved+2*quotaTestMinuteMS, temporaryWeekly.ResetsAtMS-quotaResetJitterMS-1,
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
