package store

import (
	"fmt"
	"math"
	"sort"
)

const (
	quotaArbitrationRuleV2  = "quota-arbiter-v2"
	quotaFreshForMS         = int64(10 * 60 * 1000)
	quotaMaxClockSkewMS     = int64(2 * 60 * 1000)
	quotaMaxRuleClockSkewMS = int64(24 * 60 * 60 * 1000)
	quotaResetJitterMS      = int64(1_000)
	quotaMinuteMS           = int64(60 * 1000)
)

type quotaWindowProjection struct {
	Current  QuotaCurrent
	Evidence []QuotaArbitrationEvidence
}

type quotaArbiterCandidate struct {
	observation QuotaObservation
	disposition QuotaEvidenceDisposition
	reason      *QuotaRejectionReason
	basicValid  bool
	eligible    bool
}

func defaultQuotaArbitrationRule() QuotaArbitrationRule {
	return DefaultQuotaArbitrationRule()
}

// DefaultQuotaArbitrationRule returns the current versioned production rule.
func DefaultQuotaArbitrationRule() QuotaArbitrationRule {
	return QuotaArbitrationRule{
		Version: quotaArbitrationRuleV2, FreshForMS: quotaFreshForMS, MaxClockSkewMS: quotaMaxClockSkewMS,
	}
}

func arbitrateQuotaWindowWithSourceStates(
	observations []QuotaObservation,
	evaluatedAtMS int64,
	rule QuotaArbitrationRule,
	sourceStates map[QuotaSource]SourceState,
) (quotaWindowProjection, error) {
	projection, err := arbitrateQuotaWindow(observations, evaluatedAtMS, rule)
	if err != nil {
		return quotaWindowProjection{}, err
	}
	current := &projection.Current
	for _, state := range sourceStates {
		if state.LastAttemptAtMS != nil &&
			(current.LastAttemptAtMS == nil || *state.LastAttemptAtMS > *current.LastAttemptAtMS) {
			current.LastAttemptAtMS = cloneQuotaInt64Pointer(state.LastAttemptAtMS)
		}
	}
	if current.SelectedSource != nil {
		if state, found := sourceStates[*current.SelectedSource]; found && state.LastAttemptAtMS != nil &&
			current.LastSuccessAtMS != nil && *state.LastAttemptAtMS > *current.LastSuccessAtMS &&
			(state.FreshnessState == SourceFreshnessStale || state.FreshnessState == SourceFreshnessUnavailable) &&
			current.FreshnessState == QuotaCurrentFresh {
			current.FreshnessState = QuotaCurrentStale
		}
	}
	current.ExplanationCode = quotaCurrentExplanation(current.FreshnessState, current.ConflictState)
	return projection, nil
}

func arbitrateQuotaWindow(
	observations []QuotaObservation,
	evaluatedAtMS int64,
	rule QuotaArbitrationRule,
) (quotaWindowProjection, error) {
	if evaluatedAtMS < 0 || rule.Version == "" || len(rule.Version) > 128 ||
		rule.FreshForMS <= 0 || rule.FreshForMS > maxQuotaWindowMinutes*quotaMinuteMS ||
		rule.MaxClockSkewMS < 0 || rule.MaxClockSkewMS > quotaMaxRuleClockSkewMS {
		return quotaWindowProjection{}, invalidRecord("quota arbitration rule is invalid")
	}
	if len(observations) == 0 {
		return quotaWindowProjection{}, invalidRecord("quota arbitration window is empty")
	}
	ordered := append([]QuotaObservation(nil), observations...)
	sort.Slice(ordered, func(i, j int) bool { return quotaObservationLess(ordered[i], ordered[j]) })
	first := ordered[0]
	if first.LimitID == nil || *first.LimitID == "" {
		return quotaWindowProjection{}, invalidRecord("quota arbitration logical key is incomplete")
	}
	limitID := *first.LimitID
	for _, observation := range ordered {
		if observation.AccountScope != first.AccountScope || observation.WindowKind != first.WindowKind ||
			observation.LimitID == nil || *observation.LimitID != limitID {
			return quotaWindowProjection{}, invalidRecord("quota arbitration mixes logical windows")
		}
	}

	candidates := make([]quotaArbiterCandidate, len(ordered))
	lastUsed := make(map[string]float64)
	for index, observation := range ordered {
		candidate := quotaArbiterCandidate{observation: observation}
		candidates[index] = candidate
		if observation.Validity != QuotaValidityAccepted {
			disposition := QuotaEvidenceSuspicious
			if observation.Validity == QuotaValidityRejected {
				disposition = QuotaEvidenceRejected
			}
			setQuotaCandidateFinding(&candidates[index], disposition, observation.RejectionReason)
			continue
		}
		if quotaTimestampBeyondSkew(observation.LastObservedAtMS, evaluatedAtMS, rule.MaxClockSkewMS) {
			setQuotaCandidateReason(&candidates[index], QuotaEvidenceSuspicious, QuotaReasonObservedRegression)
			continue
		}
		if observation.ResetsAtMS <= observation.LastObservedAtMS {
			setQuotaCandidateReason(&candidates[index], QuotaEvidenceSuspicious, QuotaReasonResetNotFuture)
			continue
		}
		windowMS, valid := quotaWindowDurationMS(observation.WindowMinutes)
		if !valid || observation.ResetsAtMS-observation.LastObservedAtMS > windowMS+rule.MaxClockSkewMS {
			setQuotaCandidateReason(&candidates[index], QuotaEvidenceSuspicious, QuotaReasonInvalidResetsAt)
			continue
		}
		monotonicKey := fmt.Sprintf("%s\x00%d\x00%d", observation.Source, observation.ResetsAtMS, observation.WindowMinutes)
		if used, found := lastUsed[monotonicKey]; found && observation.UsedPercent < used {
			setQuotaCandidateReason(&candidates[index], QuotaEvidenceSuspicious, QuotaReasonUsedRegression)
			continue
		}
		if observation.UsedPercent > lastUsed[monotonicKey] {
			lastUsed[monotonicKey] = observation.UsedPercent
		}
		candidates[index].basicValid = true
		candidates[index].eligible = true
		candidates[index].disposition = QuotaEvidenceSuperseded
	}

	excludedResets := make(map[int64]struct{})
	validByReset, validResets := classifyQuotaGenerations(candidates, excludedResets, rule)
	if len(validResets) == 0 {
		return quotaNeverLoadedProjection(candidates, first, limitID, evaluatedAtMS, rule), nil
	}
	blockedZeroGeneration := false
	selectedReset, selectedIndexes := latestQuotaGeneration(validByReset, validResets)
	for quotaGenerationIsBlockedZero(candidates, selectedIndexes, selectedReset) {
		blockedZeroGeneration = true
		for _, index := range selectedIndexes {
			setQuotaCandidateReason(&candidates[index], QuotaEvidenceSuspicious, QuotaReasonDefaultFallback)
			candidates[index].eligible = false
		}
		excludedResets[selectedReset] = struct{}{}
		validByReset, validResets = classifyQuotaGenerations(candidates, excludedResets, rule)
		if len(validResets) == 0 {
			return quotaNeverLoadedProjection(candidates, first, limitID, evaluatedAtMS, rule), nil
		}
		selectedReset, selectedIndexes = latestQuotaGeneration(validByReset, validResets)
	}

	latestBySource := make(map[QuotaSource]int)
	for _, index := range selectedIndexes {
		candidate := candidates[index]
		if !candidate.eligible {
			continue
		}
		previous, found := latestBySource[candidate.observation.Source]
		if !found || quotaCandidateMoreCurrent(candidate.observation, candidates[previous].observation) {
			latestBySource[candidate.observation.Source] = index
		}
	}
	if len(latestBySource) == 0 {
		return quotaNeverLoadedProjection(candidates, first, limitID, evaluatedAtMS, rule), nil
	}
	perSource := make([]int, 0, len(latestBySource))
	for _, index := range latestBySource {
		perSource = append(perSource, index)
	}
	sort.Slice(perSource, func(i, j int) bool {
		return quotaObservationLess(candidates[perSource[i]].observation, candidates[perSource[j]].observation)
	})
	selectedIndex := perSource[0]
	conflict := false
	firstUsed := candidates[perSource[0]].observation.UsedPercent
	for _, index := range perSource {
		observation := candidates[index].observation
		if observation.UsedPercent != firstUsed {
			conflict = true
		}
		if observation.UsedPercent > candidates[selectedIndex].observation.UsedPercent ||
			observation.UsedPercent == candidates[selectedIndex].observation.UsedPercent &&
				quotaCandidateMoreCurrent(observation, candidates[selectedIndex].observation) {
			selectedIndex = index
		}
	}
	for _, index := range selectedIndexes {
		if !candidates[index].eligible {
			continue
		}
		candidates[index].disposition = QuotaEvidenceEligible
		if conflict {
			reason := QuotaReasonSourceConflict
			candidates[index].reason = &reason
		}
	}
	candidates[selectedIndex].disposition = QuotaEvidenceSelected
	selected := candidates[selectedIndex].observation
	freshUntil := quotaFreshUntil(selected.LastObservedAtMS, selected.ResetsAtMS, rule.FreshForMS)
	freshness := QuotaCurrentFresh
	if evaluatedAtMS >= selected.ResetsAtMS {
		freshness = QuotaCurrentExpiredUnknown
	} else if blockedZeroGeneration || quotaHasNewerSuspiciousCandidate(candidates, selected.LastObservedAtMS) {
		freshness = QuotaCurrentSuspicious
	} else if evaluatedAtMS > freshUntil {
		freshness = QuotaCurrentStale
	}
	conflictState := QuotaConflictNone
	if conflict {
		conflictState = QuotaConflictPresent
	}
	explanation := quotaCurrentExplanation(freshness, conflictState)
	lastAttempt := quotaLastObservationTime(candidates)
	selectedID, used, minutes, reset, generation, source := selected.ObservationID, selected.UsedPercent,
		selected.WindowMinutes, selected.ResetsAtMS, selected.ResetsAtMS, selected.Source
	lastSuccess := selected.LastObservedAtMS
	current := QuotaCurrent{
		AccountScope: first.AccountScope, WindowKind: first.WindowKind, LimitID: limitID,
		ObservationID: &selectedID, EffectiveUsedPercent: &used, WindowMinutes: &minutes,
		ResetsAtMS: &reset, WindowGeneration: &generation, SelectedSource: &source,
		FreshnessState: freshness, ConflictState: conflictState, FreshUntilMS: &freshUntil,
		LastSuccessAtMS: &lastSuccess, LastAttemptAtMS: &lastAttempt, RuleVersion: rule.Version,
		ExplanationCode: explanation, EvaluatedAtMS: evaluatedAtMS,
	}
	return quotaWindowProjection{Current: current, Evidence: quotaEvidenceFromCandidates(candidates, first, limitID)}, nil
}

func quotaNeverLoadedProjection(
	candidates []quotaArbiterCandidate,
	first QuotaObservation,
	limitID string,
	evaluatedAtMS int64,
	rule QuotaArbitrationRule,
) quotaWindowProjection {
	lastAttempt := quotaLastObservationTime(candidates)
	return quotaWindowProjection{
		Current: QuotaCurrent{
			AccountScope: first.AccountScope, WindowKind: first.WindowKind, LimitID: limitID,
			FreshnessState: QuotaCurrentNeverLoaded, ConflictState: QuotaConflictNone,
			LastAttemptAtMS: &lastAttempt, RuleVersion: rule.Version,
			ExplanationCode: QuotaExplanationUnavailable, EvaluatedAtMS: evaluatedAtMS,
		},
		Evidence: quotaEvidenceFromCandidates(candidates, first, limitID),
	}
}

func quotaObservationLess(left, right QuotaObservation) bool {
	if left.LastObservedAtMS != right.LastObservedAtMS {
		return left.LastObservedAtMS < right.LastObservedAtMS
	}
	if left.FirstObservedAtMS != right.FirstObservedAtMS {
		return left.FirstObservedAtMS < right.FirstObservedAtMS
	}
	if left.Source != right.Source {
		return left.Source < right.Source
	}
	if left.SourceGeneration != right.SourceGeneration {
		return left.SourceGeneration < right.SourceGeneration
	}
	if left.SourceOffset != right.SourceOffset {
		return left.SourceOffset < right.SourceOffset
	}
	return left.ObservationID < right.ObservationID
}

func quotaCandidateMoreCurrent(left, right QuotaObservation) bool {
	return quotaObservationLess(right, left)
}

func classifyQuotaGenerations(
	candidates []quotaArbiterCandidate,
	excludedResets map[int64]struct{},
	rule QuotaArbitrationRule,
) (map[int64][]int, []int64) {
	activeReset := int64(-1)
	activeMinutes := int64(0)
	activeFirstObserved := int64(0)
	validByReset := make(map[int64][]int)
	validResets := make([]int64, 0)
	seenReset := make(map[int64]struct{})
	for index := range candidates {
		candidate := &candidates[index]
		if !candidate.basicValid {
			continue
		}
		if _, excluded := excludedResets[candidate.observation.ResetsAtMS]; excluded {
			continue
		}
		candidate.eligible = true
		candidate.disposition = QuotaEvidenceSuperseded
		candidate.reason = nil
		observation := candidate.observation
		switch {
		case activeReset < 0:
			activeReset = observation.ResetsAtMS
			activeMinutes = observation.WindowMinutes
			activeFirstObserved = observation.LastObservedAtMS
		case observation.ResetsAtMS == activeReset && observation.WindowMinutes != activeMinutes:
			setQuotaCandidateReason(candidate, QuotaEvidenceSuspicious, QuotaReasonInvalidWindowMinutes)
			candidate.eligible = false
			continue
		case observation.ResetsAtMS > activeReset:
			// 滑动窗口可在旧 reset 尚未到达时提前重置，并给出更晚的新 reset。
			// 基础校验已保证新 reset 位于观测后的窗口时长内，不能再用旧 reset 作为准入门槛。
			activeReset = observation.ResetsAtMS
			activeMinutes = observation.WindowMinutes
			activeFirstObserved = observation.LastObservedAtMS
		case observation.ResetsAtMS < activeReset:
			if observation.WindowMinutes == activeMinutes &&
				activeReset-observation.ResetsAtMS <= quotaResetJitterMS {
				// Wham 的滑动窗口 reset_at 会在相邻采样间出现 1 秒取整抖动。
				// 把它归入已知的同一窗口，避免把可信倒计时反复降级为 suspicious。
				observation.ResetsAtMS = activeReset
				candidate.observation.ResetsAtMS = activeReset
				break
			}
			if observation.LastObservedAtMS >= activeFirstObserved {
				setQuotaCandidateReason(candidate, QuotaEvidenceSuspicious, QuotaReasonResetRegression)
				candidate.eligible = false
				continue
			}
		}
		validByReset[observation.ResetsAtMS] = append(validByReset[observation.ResetsAtMS], index)
		if _, found := seenReset[observation.ResetsAtMS]; !found {
			validResets = append(validResets, observation.ResetsAtMS)
			seenReset[observation.ResetsAtMS] = struct{}{}
		}
	}
	sort.Slice(validResets, func(i, j int) bool { return validResets[i] < validResets[j] })
	return validByReset, validResets
}

func latestQuotaGeneration(validByReset map[int64][]int, validResets []int64) (int64, []int) {
	selectedReset := validResets[len(validResets)-1]
	return selectedReset, validByReset[selectedReset]
}

func quotaWindowDurationMS(minutes int64) (int64, bool) {
	if minutes <= 0 || minutes > maxQuotaWindowMinutes || minutes > math.MaxInt64/quotaMinuteMS {
		return 0, false
	}
	return minutes * quotaMinuteMS, true
}

func quotaTimestampBeyondSkew(observedAtMS, evaluatedAtMS, skewMS int64) bool {
	if evaluatedAtMS > math.MaxInt64-skewMS {
		return false
	}
	return observedAtMS > evaluatedAtMS+skewMS
}

func quotaFreshUntil(observedAtMS, resetAtMS, freshForMS int64) int64 {
	until := resetAtMS
	if observedAtMS <= math.MaxInt64-freshForMS && observedAtMS+freshForMS < until {
		until = observedAtMS + freshForMS
	}
	return until
}

func quotaGenerationIsBlockedZero(
	candidates []quotaArbiterCandidate,
	selectedIndexes []int,
	selectedReset int64,
) bool {
	maxUsed := float64(0)
	latestSelected := int64(0)
	for _, index := range selectedIndexes {
		if !candidates[index].eligible {
			continue
		}
		observation := candidates[index].observation
		if observation.UsedPercent > maxUsed {
			maxUsed = observation.UsedPercent
		}
		if observation.LastObservedAtMS > latestSelected {
			latestSelected = observation.LastObservedAtMS
		}
	}
	if maxUsed != 0 {
		return false
	}
	for _, candidate := range candidates {
		observation := candidate.observation
		if candidate.basicValid && observation.Source == QuotaSourceLocalJSONL &&
			observation.ResetsAtMS < selectedReset && observation.LastObservedAtMS > latestSelected {
			return true
		}
	}
	return false
}

func quotaHasNewerSuspiciousCandidate(candidates []quotaArbiterCandidate, selectedAtMS int64) bool {
	for _, candidate := range candidates {
		if candidate.disposition == QuotaEvidenceSuspicious && candidate.observation.LastObservedAtMS >= selectedAtMS {
			return true
		}
	}
	return false
}

func quotaLastObservationTime(candidates []quotaArbiterCandidate) int64 {
	last := int64(0)
	for _, candidate := range candidates {
		if candidate.observation.LastObservedAtMS > last {
			last = candidate.observation.LastObservedAtMS
		}
	}
	return last
}

func quotaCurrentExplanation(freshness QuotaCurrentFreshness, conflict QuotaConflictState) QuotaExplanationCode {
	if conflict == QuotaConflictPresent {
		return QuotaExplanationSourceConflict
	}
	switch freshness {
	case QuotaCurrentFresh:
		return QuotaExplanationTrusted
	case QuotaCurrentStale:
		return QuotaExplanationStale
	case QuotaCurrentExpiredUnknown:
		return QuotaExplanationExpired
	case QuotaCurrentSuspicious:
		return QuotaExplanationSuspicious
	default:
		return QuotaExplanationUnavailable
	}
}

func quotaEvidenceFromCandidates(
	candidates []quotaArbiterCandidate,
	first QuotaObservation,
	limitID string,
) []QuotaArbitrationEvidence {
	evidence := make([]QuotaArbitrationEvidence, 0, len(candidates))
	for _, candidate := range candidates {
		generation := candidate.observation.ResetsAtMS
		explanation := quotaEvidenceExplanation(candidate)
		evidence = append(evidence, QuotaArbitrationEvidence{
			AccountScope: first.AccountScope, WindowKind: first.WindowKind, LimitID: limitID,
			ObservationID: candidate.observation.ObservationID, WindowGeneration: &generation,
			Disposition: candidate.disposition, Reason: cloneQuotaReason(candidate.reason),
			ExplanationCode: explanation,
		})
	}
	sort.Slice(evidence, func(i, j int) bool { return evidence[i].ObservationID < evidence[j].ObservationID })
	return evidence
}

func quotaEvidenceExplanation(candidate quotaArbiterCandidate) QuotaExplanationCode {
	if candidate.reason != nil && *candidate.reason == QuotaReasonSourceConflict {
		return QuotaExplanationSourceConflict
	}
	switch candidate.disposition {
	case QuotaEvidenceSelected, QuotaEvidenceEligible, QuotaEvidenceSuperseded:
		return QuotaExplanationTrusted
	case QuotaEvidenceSuspicious:
		return QuotaExplanationSuspicious
	default:
		return QuotaExplanationUnavailable
	}
}

func setQuotaCandidateReason(
	candidate *quotaArbiterCandidate,
	disposition QuotaEvidenceDisposition,
	reason QuotaRejectionReason,
) {
	candidate.disposition = disposition
	candidate.reason = &reason
}

func setQuotaCandidateFinding(
	candidate *quotaArbiterCandidate,
	disposition QuotaEvidenceDisposition,
	reason *QuotaRejectionReason,
) {
	candidate.disposition = disposition
	candidate.reason = cloneQuotaReason(reason)
}

func cloneQuotaReason(reason *QuotaRejectionReason) *QuotaRejectionReason {
	if reason == nil {
		return nil
	}
	cloned := *reason
	return &cloned
}
