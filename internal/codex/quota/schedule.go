package quota

import (
	"errors"
	"math"

	"github.com/SisyphusSQ/codex-pulse/internal/runtimeclock"
	"github.com/SisyphusSQ/codex-pulse/internal/store"
)

const (
	manualRefreshMinimumIntervalMS = int64(60_000)
	foregroundRefreshAgeMS         = int64(60_000)
	quotaFastIntervalMS            = int64(120_000)
	quotaNearResetMS               = int64(600_000)
	quotaResetGraceMS              = int64(3_000)
	maximumScheduleIntervalSeconds = int64(86_400)
)

var ErrInvalidRefreshPolicy = errors.New("quota refresh policy is invalid")

type RefreshSource string

const (
	RefreshSourceQuota        RefreshSource = "quota"
	RefreshSourceResetCredits RefreshSource = "reset_credits"
)

type RefreshPlanInput struct {
	Source          RefreshSource
	Trigger         store.SourceRefreshTrigger
	Enabled         bool
	NowMS           int64
	IntervalSeconds int64
	Schedule        *store.SourceRefreshSchedule
	SourceState     *store.SourceState
	Windows         []store.QuotaCurrent
}

type RefreshDecision struct {
	NextDueAtMS *int64
	Reason      store.SourceRefreshReason
	ShouldFetch bool
}

// QuotaResetSummary is the cross-window reset calculation consumed by the
// scheduler and later query DTOs. Nullable timestamps preserve "no trusted
// reset" independently from Unix epoch or a real zero duration.
type QuotaResetSummary struct {
	NextResetAtMS      *int64
	RemainingMS        *int64
	TrustedWindowCount int64
}

type RefreshPolicy struct {
	jitter func() float64
}

func NewRefreshPolicy(jitter func() float64) (RefreshPolicy, error) {
	if jitter == nil {
		return RefreshPolicy{}, ErrInvalidRefreshPolicy
	}
	return RefreshPolicy{jitter: jitter}, nil
}

func (policy RefreshPolicy) Plan(input RefreshPlanInput) (RefreshDecision, error) {
	if policy.jitter == nil || !validRefreshSource(input.Source) || !validRefreshPlanTrigger(input.Trigger) ||
		input.NowMS < 0 || input.NowMS > runtimeclock.MaxTimestampMS || input.IntervalSeconds < 60 ||
		input.IntervalSeconds > maximumScheduleIntervalSeconds {
		return RefreshDecision{}, ErrInvalidRefreshPolicy
	}
	if !input.Enabled {
		return RefreshDecision{Reason: store.RefreshReasonDisabled}, nil
	}
	if input.Schedule != nil && input.Schedule.ActiveClaimID != nil &&
		input.Trigger != store.RefreshTriggerScheduled {
		return preservedRefreshDecision(input.Schedule), nil
	}

	switch input.Trigger {
	case store.RefreshTriggerManual:
		if input.Schedule != nil {
			if input.Schedule.LastManualAtMS != nil &&
				input.NowMS < addScheduleDuration(*input.Schedule.LastManualAtMS, manualRefreshMinimumIntervalMS) {
				return preservedRefreshDecision(input.Schedule), nil
			}
		}
		if decision, fenced := activeRetryAfterDecision(input.Schedule, input.SourceState, input.NowMS); fenced {
			return decision, nil
		}
		return immediateRefreshDecision(input.NowMS, store.RefreshReasonManual), nil
	case store.RefreshTriggerForeground:
		if decision, fenced := activeRetryAfterDecision(input.Schedule, input.SourceState, input.NowMS); fenced {
			return decision, nil
		}
		if schedulePausesAutomaticRefresh(input.Schedule, input.NowMS) {
			return preservedRefreshDecision(input.Schedule), nil
		}
		if input.SourceState != nil && input.SourceState.LastSuccessAtMS != nil &&
			input.NowMS < addScheduleDuration(*input.SourceState.LastSuccessAtMS, foregroundRefreshAgeMS) {
			return preservedRefreshDecision(input.Schedule), nil
		}
		return immediateRefreshDecision(input.NowMS, store.RefreshReasonForeground), nil
	case store.RefreshTriggerWake:
		if decision, fenced := activeRetryAfterDecision(input.Schedule, input.SourceState, input.NowMS); fenced {
			return decision, nil
		}
		if schedulePausesAutomaticRefresh(input.Schedule, input.NowMS) || input.SourceState == nil ||
			input.SourceState.FreshnessState != store.SourceFreshnessStale {
			return preservedRefreshDecision(input.Schedule), nil
		}
		return immediateRefreshDecision(input.NowMS, store.RefreshReasonWakeStale), nil
	case store.RefreshTriggerStartup, store.RefreshTriggerRecovery:
		if input.SourceState == nil || input.SourceState.LastAttemptAtMS == nil {
			reason := store.RefreshReasonStartup
			if input.Trigger == store.RefreshTriggerRecovery {
				reason = store.RefreshReasonRecovery
			}
			return immediateRefreshDecision(input.NowMS, reason), nil
		}
		decision, durableFailureDue := durableFailureRestartDecision(
			input.Schedule, input.SourceState, input.NowMS,
		)
		if !durableFailureDue {
			var err error
			decision, err = policy.nextDecision(input)
			if err != nil {
				return RefreshDecision{}, err
			}
			if input.Schedule != nil && input.Schedule.NextDueAtMS != nil && decision.NextDueAtMS != nil &&
				*input.Schedule.NextDueAtMS < *decision.NextDueAtMS &&
				!schedulePausesAutomaticRefresh(input.Schedule, input.NowMS) {
				decision.NextDueAtMS = cloneScheduleInt64(input.Schedule.NextDueAtMS)
				decision.Reason = input.Schedule.Reason
			}
		}
		if decision.NextDueAtMS != nil && *decision.NextDueAtMS <= input.NowMS {
			decision.NextDueAtMS = cloneScheduleInt64(&input.NowMS)
			decision.ShouldFetch = true
			if input.Trigger == store.RefreshTriggerRecovery {
				decision.Reason = store.RefreshReasonRecovery
			}
		}
		return decision, nil
	case store.RefreshTriggerScheduled:
		return policy.nextDecision(input)
	default:
		return RefreshDecision{}, ErrInvalidRefreshPolicy
	}
}

func (policy RefreshPolicy) nextDecision(input RefreshPlanInput) (RefreshDecision, error) {
	if input.SourceState != nil && input.SourceState.LastFailureCode != nil {
		switch *input.SourceState.LastFailureCode {
		case store.SourceFailureAuthRequired:
			return RefreshDecision{Reason: store.RefreshReasonAuthRequired}, nil
		case store.SourceFailureSchemaIncompatible:
			return RefreshDecision{Reason: store.RefreshReasonSchemaIncompatible}, nil
		case store.SourceFailureNetworkUnavailable, store.SourceFailureTimeout, store.SourceFailureServerError,
			store.SourceFailureHTTP429:
			backoffAtMS, err := policy.networkBackoffAt(input.NowMS, input.SourceState.ConsecutiveFailures)
			if err != nil {
				return RefreshDecision{}, err
			}
			reason := store.RefreshReasonNetworkBackoff
			if *input.SourceState.LastFailureCode == store.SourceFailureHTTP429 &&
				input.SourceState.NextDueAtMS != nil && *input.SourceState.NextDueAtMS > backoffAtMS {
				backoffAtMS = *input.SourceState.NextDueAtMS
				reason = store.RefreshReasonRetryAfter
			}
			return scheduledRefreshDecision(backoffAtMS, reason), nil
		case store.SourceFailureCancelled:
			dueAtMS, ok := addScheduleDurationChecked(input.NowMS, input.IntervalSeconds*1_000)
			if !ok {
				return RefreshDecision{}, ErrInvalidRefreshPolicy
			}
			return scheduledRefreshDecision(dueAtMS, store.RefreshReasonCancelled), nil
		}
	}
	if input.Source == RefreshSourceResetCredits {
		dueAtMS, ok := addScheduleDurationChecked(input.NowMS, input.IntervalSeconds*1_000)
		if !ok {
			return RefreshDecision{}, ErrInvalidRefreshPolicy
		}
		return scheduledRefreshDecision(dueAtMS, store.RefreshReasonNormalInterval), nil
	}
	return planQuotaCadence(input)
}

func planQuotaCadence(input RefreshPlanInput) (RefreshDecision, error) {
	intervalMS := input.IntervalSeconds * 1_000
	reason := store.RefreshReasonNormalInterval
	lowRemaining := false
	for _, window := range input.Windows {
		if !trustedScheduleWindow(window, input.NowMS) {
			continue
		}
		if window.EffectiveUsedPercent != nil && *window.EffectiveUsedPercent >= 80 {
			lowRemaining = true
		}
	}
	resetSummary, err := CalculateQuotaResetSummary(input.Windows, input.NowMS)
	if err != nil {
		return RefreshDecision{}, err
	}
	if lowRemaining {
		intervalMS = quotaFastIntervalMS
		reason = store.RefreshReasonLowRemaining
	}
	if resetSummary.RemainingMS != nil && *resetSummary.RemainingMS <= quotaNearResetMS {
		intervalMS = quotaFastIntervalMS
		if !lowRemaining {
			reason = store.RefreshReasonNearReset
		}
	}
	dueAtMS, ok := addScheduleDurationChecked(input.NowMS, intervalMS)
	if !ok {
		return RefreshDecision{}, ErrInvalidRefreshPolicy
	}
	if resetSummary.NextResetAtMS != nil {
		resetGraceAtMS, valid := addScheduleDurationChecked(*resetSummary.NextResetAtMS, quotaResetGraceMS)
		if valid && resetGraceAtMS < dueAtMS {
			dueAtMS = resetGraceAtMS
			reason = store.RefreshReasonResetGrace
		}
	}
	return scheduledRefreshDecision(dueAtMS, reason), nil
}

// CalculateQuotaResetSummary ignores suspicious/expired/never-loaded windows
// and returns the nearest reset among all remaining trusted generations.
func CalculateQuotaResetSummary(windows []store.QuotaCurrent, nowMS int64) (QuotaResetSummary, error) {
	if nowMS < 0 || nowMS > runtimeclock.MaxTimestampMS {
		return QuotaResetSummary{}, ErrInvalidRefreshPolicy
	}
	result := QuotaResetSummary{}
	for _, window := range windows {
		if !trustedScheduleWindow(window, nowMS) {
			continue
		}
		result.TrustedWindowCount++
		if result.NextResetAtMS == nil || *window.ResetsAtMS < *result.NextResetAtMS {
			result.NextResetAtMS = cloneScheduleInt64(window.ResetsAtMS)
		}
	}
	if result.NextResetAtMS != nil {
		remaining := *result.NextResetAtMS - nowMS
		result.RemainingMS = &remaining
	}
	return result, nil
}

func (policy RefreshPolicy) networkBackoffAt(nowMS, failures int64) (int64, error) {
	if failures < 1 {
		failures = 1
	}
	minutes := int64(5)
	switch failures {
	case 1:
		minutes = 5
	case 2:
		minutes = 10
	case 3:
		minutes = 20
	default:
		minutes = 30
	}
	delayMS := minutes * 60_000
	sample := policy.jitter()
	if math.IsNaN(sample) || math.IsInf(sample, 0) || sample < 0 || sample >= 1 {
		return 0, ErrInvalidRefreshPolicy
	}
	if delayMS < 1_800_000 {
		delayMS += int64(float64(delayMS) * 0.1 * sample)
		if delayMS > 1_800_000 {
			delayMS = 1_800_000
		}
	}
	dueAtMS, ok := addScheduleDurationChecked(nowMS, delayMS)
	if !ok {
		return 0, ErrInvalidRefreshPolicy
	}
	return dueAtMS, nil
}

func trustedScheduleWindow(window store.QuotaCurrent, nowMS int64) bool {
	if window.ObservationID == nil || window.EffectiveUsedPercent == nil || window.ResetsAtMS == nil ||
		*window.ResetsAtMS <= nowMS {
		return false
	}
	return window.FreshnessState == store.QuotaCurrentFresh || window.FreshnessState == store.QuotaCurrentStale
}

func preservedRefreshDecision(schedule *store.SourceRefreshSchedule) RefreshDecision {
	if schedule == nil {
		return RefreshDecision{Reason: store.RefreshReasonNormalInterval}
	}
	return RefreshDecision{
		NextDueAtMS: cloneScheduleInt64(schedule.NextDueAtMS), Reason: schedule.Reason,
	}
}

func immediateRefreshDecision(nowMS int64, reason store.SourceRefreshReason) RefreshDecision {
	return RefreshDecision{NextDueAtMS: cloneScheduleInt64(&nowMS), Reason: reason, ShouldFetch: true}
}

func scheduledRefreshDecision(dueAtMS int64, reason store.SourceRefreshReason) RefreshDecision {
	return RefreshDecision{NextDueAtMS: cloneScheduleInt64(&dueAtMS), Reason: reason}
}

func activeRetryAfterDecision(
	schedule *store.SourceRefreshSchedule,
	state *store.SourceState,
	nowMS int64,
) (RefreshDecision, bool) {
	var fenceAtMS *int64
	if schedule != nil && schedule.Reason == store.RefreshReasonRetryAfter && schedule.NextDueAtMS != nil &&
		*schedule.NextDueAtMS > nowMS {
		fenceAtMS = schedule.NextDueAtMS
	}
	if state != nil && state.LastFailureCode != nil && *state.LastFailureCode == store.SourceFailureHTTP429 &&
		state.NextDueAtMS != nil && *state.NextDueAtMS > nowMS &&
		(fenceAtMS == nil || *state.NextDueAtMS > *fenceAtMS) {
		fenceAtMS = state.NextDueAtMS
	}
	if fenceAtMS == nil {
		return RefreshDecision{}, false
	}
	if schedule != nil && schedule.NextDueAtMS != nil && *schedule.NextDueAtMS >= *fenceAtMS {
		return preservedRefreshDecision(schedule), true
	}
	return scheduledRefreshDecision(*fenceAtMS, store.RefreshReasonRetryAfter), true
}

func durableFailureRestartDecision(
	schedule *store.SourceRefreshSchedule,
	state *store.SourceState,
	nowMS int64,
) (RefreshDecision, bool) {
	if decision, fenced := activeRetryAfterDecision(schedule, state, nowMS); fenced {
		return decision, true
	}
	if schedule == nil || schedule.NextDueAtMS == nil ||
		(schedule.Reason != store.RefreshReasonNetworkBackoff && schedule.Reason != store.RefreshReasonRetryAfter) {
		return RefreshDecision{}, false
	}
	return preservedRefreshDecision(schedule), true
}

func schedulePausesAutomaticRefresh(schedule *store.SourceRefreshSchedule, nowMS int64) bool {
	return schedule != nil && (schedule.Reason == store.RefreshReasonAuthRequired ||
		schedule.Reason == store.RefreshReasonSchemaIncompatible || schedule.Reason == store.RefreshReasonDisabled ||
		(schedule.Reason == store.RefreshReasonNetworkBackoff || schedule.Reason == store.RefreshReasonRetryAfter) &&
			schedule.NextDueAtMS != nil && *schedule.NextDueAtMS > nowMS)
}

func validRefreshSource(value RefreshSource) bool {
	return value == RefreshSourceQuota || value == RefreshSourceResetCredits
}

func validRefreshPlanTrigger(value store.SourceRefreshTrigger) bool {
	return value == store.RefreshTriggerScheduled || value == store.RefreshTriggerStartup ||
		value == store.RefreshTriggerForeground || value == store.RefreshTriggerWake ||
		value == store.RefreshTriggerManual || value == store.RefreshTriggerRecovery
}

func addScheduleDuration(value, delta int64) int64 {
	result, ok := addScheduleDurationChecked(value, delta)
	if !ok {
		return runtimeclock.MaxTimestampMS
	}
	return result
}

func addScheduleDurationChecked(value, delta int64) (int64, bool) {
	if value < 0 || delta < 0 || value > runtimeclock.MaxTimestampMS-delta {
		return 0, false
	}
	return value + delta, true
}

func cloneScheduleInt64(value *int64) *int64 {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}
