package quota

import (
	"testing"

	"github.com/SisyphusSQ/codex-pulse/internal/store"
)

func TestRefreshPolicyPlansQuotaCadenceFromTrustedWindows(t *testing.T) {
	t.Parallel()

	const now = int64(1_000_000)
	policy := mustRefreshPolicy(t, func() float64 { return 0 })
	tests := []struct {
		name       string
		used       float64
		resetInMS  int64
		freshness  store.QuotaCurrentFreshness
		wantDue    int64
		wantReason store.SourceRefreshReason
	}{
		{name: "normal", used: 50, resetInMS: 3_600_000, freshness: store.QuotaCurrentFresh, wantDue: now + 300_000, wantReason: store.RefreshReasonNormalInterval},
		{name: "low remaining", used: 80, resetInMS: 3_600_000, freshness: store.QuotaCurrentFresh, wantDue: now + 120_000, wantReason: store.RefreshReasonLowRemaining},
		{name: "near reset", used: 50, resetInMS: 600_000, freshness: store.QuotaCurrentStale, wantDue: now + 120_000, wantReason: store.RefreshReasonNearReset},
		{name: "reset grace wins", used: 90, resetInMS: 30_000, freshness: store.QuotaCurrentFresh, wantDue: now + 33_000, wantReason: store.RefreshReasonResetGrace},
		{name: "untrusted reset ignored", used: 90, resetInMS: 30_000, freshness: store.QuotaCurrentExpiredUnknown, wantDue: now + 300_000, wantReason: store.RefreshReasonNormalInterval},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			window := scheduleWindowFixture(testCase.used, now+testCase.resetInMS, testCase.freshness)
			decision, err := policy.Plan(RefreshPlanInput{
				Source: RefreshSourceQuota, Trigger: store.RefreshTriggerScheduled,
				Enabled: true, NowMS: now, IntervalSeconds: 300,
				Windows: []store.QuotaCurrent{window},
			})
			if err != nil || decision.NextDueAtMS == nil || *decision.NextDueAtMS != testCase.wantDue ||
				decision.Reason != testCase.wantReason {
				t.Fatalf("Plan() = %#v, %v", decision, err)
			}
		})
	}
}

func TestCalculateQuotaResetSummaryUsesOnlyTrustedCrossWindowResets(t *testing.T) {
	t.Parallel()

	const now = int64(1_000_000)
	primary := scheduleWindowFixture(50, now+1_800_000, store.QuotaCurrentFresh)
	secondary := scheduleWindowFixture(20, now+7_200_000, store.QuotaCurrentStale)
	secondary.WindowKind = store.QuotaWindowSecondary
	untrusted := scheduleWindowFixture(90, now+60_000, store.QuotaCurrentSuspicious)
	summary, err := CalculateQuotaResetSummary(
		[]store.QuotaCurrent{secondary, untrusted, primary}, now,
	)
	if err != nil || summary.TrustedWindowCount != 2 || summary.NextResetAtMS == nil ||
		*summary.NextResetAtMS != now+1_800_000 || summary.RemainingMS == nil || *summary.RemainingMS != 1_800_000 {
		t.Fatalf("CalculateQuotaResetSummary() = %#v, %v", summary, err)
	}
	empty, err := CalculateQuotaResetSummary([]store.QuotaCurrent{untrusted}, now)
	if err != nil || empty.TrustedWindowCount != 0 || empty.NextResetAtMS != nil || empty.RemainingMS != nil {
		t.Fatalf("untrusted summary = %#v, %v", empty, err)
	}
}

func TestRefreshPolicyClassifiesBackoffRetryAfterAndStops(t *testing.T) {
	t.Parallel()

	const now = int64(2_000_000)
	policy := mustRefreshPolicy(t, func() float64 { return 0 })
	tests := []struct {
		name       string
		failure    store.SourceFailureCode
		failures   int64
		retryAtMS  *int64
		wantDue    *int64
		wantReason store.SourceRefreshReason
	}{
		{name: "network first", failure: store.SourceFailureNetworkUnavailable, failures: 1, wantDue: int64Pointer(now + 300_000), wantReason: store.RefreshReasonNetworkBackoff},
		{name: "timeout second", failure: store.SourceFailureTimeout, failures: 2, wantDue: int64Pointer(now + 600_000), wantReason: store.RefreshReasonNetworkBackoff},
		{name: "server capped", failure: store.SourceFailureServerError, failures: 9, wantDue: int64Pointer(now + 1_800_000), wantReason: store.RefreshReasonNetworkBackoff},
		{name: "retry after later", failure: store.SourceFailureHTTP429, failures: 1, retryAtMS: int64Pointer(now + 900_000), wantDue: int64Pointer(now + 900_000), wantReason: store.RefreshReasonRetryAfter},
		{name: "retry after missing", failure: store.SourceFailureHTTP429, failures: 1, wantDue: int64Pointer(now + 300_000), wantReason: store.RefreshReasonNetworkBackoff},
		{name: "auth pauses", failure: store.SourceFailureAuthRequired, failures: 1, wantReason: store.RefreshReasonAuthRequired},
		{name: "schema pauses", failure: store.SourceFailureSchemaIncompatible, failures: 1, wantReason: store.RefreshReasonSchemaIncompatible},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			state := store.SourceState{
				SourceInstanceID: store.QuotaSourceInstanceWhamDefault,
				SourceType:       store.QuotaSourceTypeWham, ScopeKey: store.QuotaAccountScopeDefault,
				ConsecutiveFailures: testCase.failures, LastFailureCode: &testCase.failure,
				NextDueAtMS: testCase.retryAtMS, FreshnessState: store.SourceFreshnessStale,
			}
			decision, err := policy.Plan(RefreshPlanInput{
				Source: RefreshSourceQuota, Trigger: store.RefreshTriggerScheduled,
				Enabled: true, NowMS: now, IntervalSeconds: 300, SourceState: &state,
			})
			if err != nil || decision.Reason != testCase.wantReason || !equalInt64Pointer(decision.NextDueAtMS, testCase.wantDue) {
				t.Fatalf("Plan() = %#v, %v", decision, err)
			}
		})
	}
}

func TestRefreshPolicyThrottlesManualForegroundAndWakeTriggers(t *testing.T) {
	t.Parallel()

	const now = int64(3_000_000)
	policy := mustRefreshPolicy(t, func() float64 { return 0 })
	future := now + 600_000
	retryAfterFuture := now + 60_000
	lastManual := now - 30_000
	lastSuccessRecent := now - 30_000
	lastSuccessStale := now - 61_000
	retryAfterElapsed := now - 1
	http429 := store.SourceFailureHTTP429
	tests := []struct {
		name       string
		trigger    store.SourceRefreshTrigger
		schedule   store.SourceRefreshSchedule
		state      *store.SourceState
		wantFetch  bool
		wantReason store.SourceRefreshReason
	}{
		{name: "manual throttled", trigger: store.RefreshTriggerManual, schedule: store.SourceRefreshSchedule{LastManualAtMS: &lastManual, NextDueAtMS: &future, Reason: store.RefreshReasonNormalInterval}, wantReason: store.RefreshReasonNormalInterval},
		{name: "manual respects retry after", trigger: store.RefreshTriggerManual, schedule: store.SourceRefreshSchedule{NextDueAtMS: &future, Reason: store.RefreshReasonRetryAfter}, wantReason: store.RefreshReasonRetryAfter},
		{name: "manual respects retry after hidden by longer backoff", trigger: store.RefreshTriggerManual, schedule: store.SourceRefreshSchedule{NextDueAtMS: &future, Reason: store.RefreshReasonNetworkBackoff}, state: &store.SourceState{LastFailureCode: &http429, NextDueAtMS: &retryAfterFuture}, wantReason: store.RefreshReasonNetworkBackoff},
		{name: "manual may bypass backoff after retry after elapsed", trigger: store.RefreshTriggerManual, schedule: store.SourceRefreshSchedule{NextDueAtMS: &future, Reason: store.RefreshReasonNetworkBackoff}, state: &store.SourceState{LastFailureCode: &http429, NextDueAtMS: &retryAfterElapsed}, wantFetch: true, wantReason: store.RefreshReasonManual},
		{name: "manual allowed", trigger: store.RefreshTriggerManual, schedule: store.SourceRefreshSchedule{NextDueAtMS: &future, Reason: store.RefreshReasonNetworkBackoff}, wantFetch: true, wantReason: store.RefreshReasonManual},
		{name: "foreground recent", trigger: store.RefreshTriggerForeground, schedule: store.SourceRefreshSchedule{NextDueAtMS: &future, Reason: store.RefreshReasonNormalInterval}, state: &store.SourceState{LastSuccessAtMS: &lastSuccessRecent}, wantReason: store.RefreshReasonNormalInterval},
		{name: "foreground stale age", trigger: store.RefreshTriggerForeground, schedule: store.SourceRefreshSchedule{NextDueAtMS: &future, Reason: store.RefreshReasonNormalInterval}, state: &store.SourceState{LastSuccessAtMS: &lastSuccessStale}, wantFetch: true, wantReason: store.RefreshReasonForeground},
		{name: "foreground respects network backoff", trigger: store.RefreshTriggerForeground, schedule: store.SourceRefreshSchedule{NextDueAtMS: &future, Reason: store.RefreshReasonNetworkBackoff}, state: &store.SourceState{LastSuccessAtMS: &lastSuccessStale}, wantReason: store.RefreshReasonNetworkBackoff},
		{name: "foreground after retry-after", trigger: store.RefreshTriggerForeground, schedule: store.SourceRefreshSchedule{NextDueAtMS: &retryAfterElapsed, Reason: store.RefreshReasonRetryAfter}, state: &store.SourceState{LastSuccessAtMS: &lastSuccessStale}, wantFetch: true, wantReason: store.RefreshReasonForeground},
		{name: "wake current ignored", trigger: store.RefreshTriggerWake, schedule: store.SourceRefreshSchedule{NextDueAtMS: &future, Reason: store.RefreshReasonNormalInterval}, state: &store.SourceState{FreshnessState: store.SourceFreshnessCurrent}, wantReason: store.RefreshReasonNormalInterval},
		{name: "wake stale allowed", trigger: store.RefreshTriggerWake, schedule: store.SourceRefreshSchedule{NextDueAtMS: &future, Reason: store.RefreshReasonNormalInterval}, state: &store.SourceState{FreshnessState: store.SourceFreshnessStale}, wantFetch: true, wantReason: store.RefreshReasonWakeStale},
		{name: "wake stale respects network backoff", trigger: store.RefreshTriggerWake, schedule: store.SourceRefreshSchedule{NextDueAtMS: &future, Reason: store.RefreshReasonNetworkBackoff}, state: &store.SourceState{FreshnessState: store.SourceFreshnessStale}, wantReason: store.RefreshReasonNetworkBackoff},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			decision, err := policy.Plan(RefreshPlanInput{
				Source: RefreshSourceQuota, Trigger: testCase.trigger, Enabled: true,
				NowMS: now, IntervalSeconds: 300, Schedule: &testCase.schedule, SourceState: testCase.state,
			})
			if err != nil || decision.ShouldFetch != testCase.wantFetch || decision.Reason != testCase.wantReason {
				t.Fatalf("Plan() = %#v, %v", decision, err)
			}
			if testCase.wantFetch && (decision.NextDueAtMS == nil || *decision.NextDueAtMS != now) {
				t.Fatalf("immediate decision = %#v", decision)
			}
		})
	}
}

func TestRefreshPolicyUsesLaterRetryAfterFence(t *testing.T) {
	t.Parallel()

	const now = int64(3_500_000)
	policy := mustRefreshPolicy(t, func() float64 { return 0 })
	persistedRetryAtMS := now + 60_000
	sourceRetryAtMS := now + 120_000
	http429 := store.SourceFailureHTTP429
	decision, err := policy.Plan(RefreshPlanInput{
		Source: RefreshSourceResetCredits, Trigger: store.RefreshTriggerManual,
		Enabled: true, NowMS: now, IntervalSeconds: 1_800,
		Schedule: &store.SourceRefreshSchedule{
			NextDueAtMS: &persistedRetryAtMS, Reason: store.RefreshReasonRetryAfter,
		},
		SourceState: &store.SourceState{
			LastFailureCode: &http429, NextDueAtMS: &sourceRetryAtMS,
		},
	})
	if err != nil || decision.ShouldFetch || decision.NextDueAtMS == nil ||
		*decision.NextDueAtMS != sourceRetryAtMS || decision.Reason != store.RefreshReasonRetryAfter {
		t.Fatalf("Plan() = %#v, %v", decision, err)
	}
}

func TestRefreshPolicyRevalidatesStartupAndDisabledState(t *testing.T) {
	t.Parallel()

	const now = int64(4_000_000)
	policy := mustRefreshPolicy(t, func() float64 { return 0 })
	decision, err := policy.Plan(RefreshPlanInput{
		Source: RefreshSourceResetCredits, Trigger: store.RefreshTriggerStartup,
		Enabled: true, NowMS: now, IntervalSeconds: 1_800,
	})
	if err != nil || !decision.ShouldFetch || decision.NextDueAtMS == nil ||
		*decision.NextDueAtMS != now || decision.Reason != store.RefreshReasonStartup {
		t.Fatalf("startup never loaded = %#v, %v", decision, err)
	}
	future := now + 3_600_000
	lastAttempt := now - 1_000
	decision, err = policy.Plan(RefreshPlanInput{
		Source: RefreshSourceResetCredits, Trigger: store.RefreshTriggerRecovery,
		Enabled: true, NowMS: now, IntervalSeconds: 1_800,
		Schedule:    &store.SourceRefreshSchedule{NextDueAtMS: &future, Reason: store.RefreshReasonNormalInterval},
		SourceState: &store.SourceState{LastAttemptAtMS: &lastAttempt, FreshnessState: store.SourceFreshnessCurrent},
	})
	if err != nil || decision.NextDueAtMS == nil || *decision.NextDueAtMS != now+1_800_000 ||
		decision.Reason != store.RefreshReasonNormalInterval {
		t.Fatalf("recovery revalidation = %#v, %v", decision, err)
	}
	decision, err = policy.Plan(RefreshPlanInput{
		Source: RefreshSourceQuota, Trigger: store.RefreshTriggerScheduled,
		Enabled: false, NowMS: now, IntervalSeconds: 300,
	})
	if err != nil || decision.NextDueAtMS != nil || decision.ShouldFetch || decision.Reason != store.RefreshReasonDisabled {
		t.Fatalf("disabled = %#v, %v", decision, err)
	}
}

func TestRefreshPolicyPreservesDurableFailureDueAcrossRestart(t *testing.T) {
	t.Parallel()

	const failureAtMS = int64(5_000_000)
	policy := mustRefreshPolicy(t, func() float64 { return 0 })
	lastAttemptAtMS := failureAtMS
	backoffDueAtMS := failureAtMS + 300_000
	networkFailure := store.SourceFailureNetworkUnavailable
	for _, trigger := range []store.SourceRefreshTrigger{
		store.RefreshTriggerStartup,
		store.RefreshTriggerRecovery,
	} {
		decision, err := policy.Plan(RefreshPlanInput{
			Source: RefreshSourceResetCredits, Trigger: trigger,
			Enabled: true, NowMS: failureAtMS + 60_000, IntervalSeconds: 1_800,
			Schedule: &store.SourceRefreshSchedule{
				NextDueAtMS: &backoffDueAtMS, Reason: store.RefreshReasonNetworkBackoff,
			},
			SourceState: &store.SourceState{
				LastAttemptAtMS: &lastAttemptAtMS, ConsecutiveFailures: 1,
				LastFailureCode: &networkFailure,
			},
		})
		if err != nil || decision.ShouldFetch || decision.NextDueAtMS == nil ||
			*decision.NextDueAtMS != backoffDueAtMS || decision.Reason != store.RefreshReasonNetworkBackoff {
			t.Fatalf("Plan(%s) = %#v, %v", trigger, decision, err)
		}
	}

	retryAtMS := failureAtMS + 120_000
	http429 := store.SourceFailureHTTP429
	decision, err := policy.Plan(RefreshPlanInput{
		Source: RefreshSourceResetCredits, Trigger: store.RefreshTriggerRecovery,
		Enabled: true, NowMS: failureAtMS + 60_000, IntervalSeconds: 1_800,
		Schedule: &store.SourceRefreshSchedule{
			NextDueAtMS: &backoffDueAtMS, Reason: store.RefreshReasonNetworkBackoff,
		},
		SourceState: &store.SourceState{
			LastAttemptAtMS: &lastAttemptAtMS, ConsecutiveFailures: 1,
			LastFailureCode: &http429, NextDueAtMS: &retryAtMS,
		},
	})
	if err != nil || decision.ShouldFetch || decision.NextDueAtMS == nil ||
		*decision.NextDueAtMS != backoffDueAtMS || decision.Reason != store.RefreshReasonNetworkBackoff {
		t.Fatalf("Plan(retry-after plus longer backoff) = %#v, %v", decision, err)
	}

	expiredNowMS := backoffDueAtMS + 1
	decision, err = policy.Plan(RefreshPlanInput{
		Source: RefreshSourceResetCredits, Trigger: store.RefreshTriggerRecovery,
		Enabled: true, NowMS: expiredNowMS, IntervalSeconds: 1_800,
		Schedule: &store.SourceRefreshSchedule{
			NextDueAtMS: &backoffDueAtMS, Reason: store.RefreshReasonNetworkBackoff,
		},
		SourceState: &store.SourceState{
			LastAttemptAtMS: &lastAttemptAtMS, ConsecutiveFailures: 1,
			LastFailureCode: &networkFailure,
		},
	})
	if err != nil || !decision.ShouldFetch || decision.NextDueAtMS == nil ||
		*decision.NextDueAtMS != expiredNowMS || decision.Reason != store.RefreshReasonRecovery {
		t.Fatalf("Plan(expired recovery) = %#v, %v", decision, err)
	}
}

func mustRefreshPolicy(t *testing.T, jitter func() float64) RefreshPolicy {
	t.Helper()
	policy, err := NewRefreshPolicy(jitter)
	if err != nil {
		t.Fatalf("NewRefreshPolicy() error = %v", err)
	}
	return policy
}

func scheduleWindowFixture(used float64, resetAt int64, freshness store.QuotaCurrentFreshness) store.QuotaCurrent {
	observationID := "trusted"
	duration := int64(300)
	source := store.QuotaSourceWham
	return store.QuotaCurrent{
		AccountScope: store.QuotaAccountScopeDefault, WindowKind: store.QuotaWindowPrimary,
		LimitID: "codex", ObservationID: &observationID, EffectiveUsedPercent: &used,
		WindowMinutes: &duration, ResetsAtMS: &resetAt, SelectedSource: &source,
		FreshnessState: freshness, ExplanationCode: store.QuotaExplanationTrusted,
	}
}

func int64Pointer(value int64) *int64 { return &value }

func equalInt64Pointer(left, right *int64) bool {
	return left == nil && right == nil || left != nil && right != nil && *left == *right
}
