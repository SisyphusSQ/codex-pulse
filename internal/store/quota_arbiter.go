package store

import (
	"math"
	"sort"
)

const (
	quotaArbitrationRuleV5       = "quota-arbiter-v5"
	quotaFreshForMS              = int64(10 * 60 * 1000)
	quotaMaxClockSkewMS          = int64(2 * 60 * 1000)
	quotaMaxRuleClockSkewMS      = int64(24 * 60 * 60 * 1000)
	quotaResetJitterMS           = int64(5_000)
	quotaWhamWeeklyResetJitterMS = int64(2 * 60 * 1000)
	quotaWeeklyWindowMinutes     = int64(7 * 24 * 60)
	quotaMinuteMS                = int64(60 * 1000)
)

type quotaWindowProjection struct {
	Current  QuotaCurrent
	Evidence []QuotaArbitrationEvidence
}

type quotaArbiterCandidate struct {
	observation   QuotaObservation
	rawResetsAtMS int64
	disposition   QuotaEvidenceDisposition
	reason        *QuotaRejectionReason
	basicValid    bool
	eligible      bool
}

type quotaResetGeneration struct {
	anchorReset    int64
	canonicalReset int64
}

func defaultQuotaArbitrationRule() QuotaArbitrationRule {
	return DefaultQuotaArbitrationRule()
}

// DefaultQuotaArbitrationRule returns the current versioned production rule.
func DefaultQuotaArbitrationRule() QuotaArbitrationRule {
	return QuotaArbitrationRule{
		Version: quotaArbitrationRuleV5, FreshForMS: quotaFreshForMS, MaxClockSkewMS: quotaMaxClockSkewMS,
	}
}

// QuotaResetsEquivalent reports whether two reset instants identify the same
// logical quota generation after accounting for bounded source timestamp jitter.
func QuotaResetsEquivalent(leftAtMS, rightAtMS int64) bool {
	return quotaResetsEquivalentWithin(leftAtMS, rightAtMS, quotaResetJitterMS)
}

// QuotaResetsEquivalentForWindow reports whether two reset instants identify
// one logical generation under the source and window-specific jitter policy.
func QuotaResetsEquivalentForWindow(
	source QuotaSource,
	windowMinutes int64,
	leftAtMS int64,
	rightAtMS int64,
) bool {
	jitterMS := quotaResetJitterMS
	if source == QuotaSourceWham && windowMinutes == quotaWeeklyWindowMinutes {
		jitterMS = quotaWhamWeeklyResetJitterMS
	}
	return quotaResetsEquivalentWithin(leftAtMS, rightAtMS, jitterMS)
}

func quotaResetsEquivalentWithin(leftAtMS, rightAtMS, jitterMS int64) bool {
	if leftAtMS < 0 || rightAtMS < 0 {
		return false
	}
	if leftAtMS < rightAtMS {
		leftAtMS, rightAtMS = rightAtMS, leftAtMS
	}
	return leftAtMS-rightAtMS <= jitterMS
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
	for index, observation := range ordered {
		candidate := quotaArbiterCandidate{
			observation: observation, rawResetsAtMS: observation.ResetsAtMS,
		}
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
		candidates[index].basicValid = true
		candidates[index].eligible = true
		candidates[index].disposition = QuotaEvidenceSuperseded
	}

	excludedIndexes := make(map[int]struct{})
	validByReset, validResets := classifyQuotaGenerations(candidates, excludedIndexes, rule)
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
			excludedIndexes[index] = struct{}{}
		}
		validByReset, validResets = classifyQuotaGenerations(candidates, excludedIndexes, rule)
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
	excludedIndexes map[int]struct{},
	rule QuotaArbitrationRule,
) (map[int64][]int, []int64) {
	activeAnchorReset := int64(-1)
	activeReset := int64(-1)
	activeMinutes := int64(0)
	activeFirstObserved := int64(0)
	activeLatestFirstObserved := int64(0)
	var activeGeneration *quotaResetGeneration
	activeIndexes := make([]int, 0)
	knownGenerationsByMinutes := make(map[int64][]*quotaResetGeneration)
	validIndexes := make([]int, 0, len(candidates))
	orderedIndexes := make([]int, 0, len(candidates))
	for index := range candidates {
		candidates[index].observation.ResetsAtMS = candidates[index].rawResetsAtMS
		if !candidates[index].basicValid {
			continue
		}
		if _, excluded := excludedIndexes[index]; excluded {
			continue
		}
		orderedIndexes = append(orderedIndexes, index)
	}
	sort.Slice(orderedIndexes, func(i, j int) bool {
		return quotaGenerationObservationLess(
			candidates[orderedIndexes[i]].observation,
			candidates[orderedIndexes[j]].observation,
		)
	})
	for _, index := range orderedIndexes {
		candidate := &candidates[index]
		candidate.eligible = true
		candidate.disposition = QuotaEvidenceSuperseded
		candidate.reason = nil
		observation := candidate.observation
		belongsToActiveGeneration := false
		switch {
		case activeReset < 0:
			activeAnchorReset = observation.ResetsAtMS
			activeReset = observation.ResetsAtMS
			activeMinutes = observation.WindowMinutes
			activeFirstObserved = observation.FirstObservedAtMS
			activeLatestFirstObserved = observation.FirstObservedAtMS
			activeGeneration = &quotaResetGeneration{
				anchorReset: observation.ResetsAtMS, canonicalReset: observation.ResetsAtMS,
			}
			knownGenerationsByMinutes[observation.WindowMinutes] = append(
				knownGenerationsByMinutes[observation.WindowMinutes], activeGeneration,
			)
			activeIndexes = activeIndexes[:0]
			belongsToActiveGeneration = true
		case observation.WindowMinutes != activeMinutes:
			if knownReset, found := quotaKnownGenerationResetForWindow(
				knownGenerationsByMinutes,
				observation.Source,
				observation.WindowMinutes,
				observation.ResetsAtMS,
			); found {
				// 已退役时长的既有 generation 晚到时只补充历史证据，不能重新夺回 current。
				candidate.observation.ResetsAtMS = knownReset
				candidate.eligible = false
				continue
			}
			if latestKnownReset, found := quotaLatestKnownGenerationReset(
				knownGenerationsByMinutes, observation.WindowMinutes,
			); found && observation.ResetsAtMS < latestKnownReset {
				// 角色即使暂时退役，其同一时长 reset 代际仍必须单调，超过来源窗口容差的回退继续隔离。
				setQuotaCandidateReason(candidate, QuotaEvidenceSuspicious, QuotaReasonResetRegression)
				candidate.eligible = false
				continue
			}
			if observation.FirstObservedAtMS <= activeLatestFirstObserved {
				// 同一观测时刻出现互斥时长时没有可靠的新旧顺序，保持 fail closed。
				setQuotaCandidateReason(candidate, QuotaEvidenceSuspicious, QuotaReasonInvalidWindowMinutes)
				candidate.eligible = false
				continue
			}
			// 服务端可在同一 logical key 上切换窗口时长角色。新角色从自己的 reset 代际重新开始，
			// 不与退役角色更远的 reset_at 比较；全部旧角色 observation 仍保留为 superseded 证据。
			activeAnchorReset = observation.ResetsAtMS
			activeReset = observation.ResetsAtMS
			activeMinutes = observation.WindowMinutes
			activeFirstObserved = observation.FirstObservedAtMS
			activeLatestFirstObserved = observation.FirstObservedAtMS
			activeGeneration = &quotaResetGeneration{
				anchorReset: observation.ResetsAtMS, canonicalReset: observation.ResetsAtMS,
			}
			knownGenerationsByMinutes[observation.WindowMinutes] = append(
				knownGenerationsByMinutes[observation.WindowMinutes], activeGeneration,
			)
			activeIndexes = activeIndexes[:0]
			validIndexes = validIndexes[:0]
			belongsToActiveGeneration = true
		case QuotaResetsEquivalentForWindow(
			observation.Source,
			observation.WindowMinutes,
			activeAnchorReset,
			observation.ResetsAtMS,
		):
			if observation.ResetsAtMS > activeReset {
				activeReset = observation.ResetsAtMS
				activeGeneration.canonicalReset = activeReset
				for _, activeIndex := range activeIndexes {
					candidates[activeIndex].observation.ResetsAtMS = activeReset
				}
			}
			observation.ResetsAtMS = activeReset
			candidate.observation.ResetsAtMS = activeReset
			if observation.FirstObservedAtMS > activeLatestFirstObserved {
				activeLatestFirstObserved = observation.FirstObservedAtMS
			}
			belongsToActiveGeneration = true
		case observation.ResetsAtMS > activeReset:
			// 滑动窗口可在旧 reset 尚未到达时提前重置，并给出更晚的新 reset。
			// 基础校验已保证新 reset 位于观测后的窗口时长内，不能再用旧 reset 作为准入门槛。
			activeAnchorReset = observation.ResetsAtMS
			activeReset = observation.ResetsAtMS
			activeMinutes = observation.WindowMinutes
			activeFirstObserved = observation.FirstObservedAtMS
			activeLatestFirstObserved = observation.FirstObservedAtMS
			activeGeneration = &quotaResetGeneration{
				anchorReset: observation.ResetsAtMS, canonicalReset: observation.ResetsAtMS,
			}
			knownGenerationsByMinutes[observation.WindowMinutes] = append(
				knownGenerationsByMinutes[observation.WindowMinutes], activeGeneration,
			)
			activeIndexes = activeIndexes[:0]
			belongsToActiveGeneration = true
		case observation.ResetsAtMS < activeReset:
			if observation.FirstObservedAtMS >= activeFirstObserved {
				setQuotaCandidateReason(candidate, QuotaEvidenceSuspicious, QuotaReasonResetRegression)
				candidate.eligible = false
				continue
			}
		}
		validIndexes = append(validIndexes, index)
		if belongsToActiveGeneration {
			activeIndexes = append(activeIndexes, index)
		}
	}
	validByReset := make(map[int64][]int)
	validResets := make([]int64, 0)
	seenReset := make(map[int64]struct{})
	for _, index := range validIndexes {
		resetAtMS := candidates[index].observation.ResetsAtMS
		validByReset[resetAtMS] = append(validByReset[resetAtMS], index)
		if _, found := seenReset[resetAtMS]; !found {
			validResets = append(validResets, resetAtMS)
			seenReset[resetAtMS] = struct{}{}
		}
	}
	sort.Slice(validResets, func(i, j int) bool { return validResets[i] < validResets[j] })
	return validByReset, validResets
}

func quotaGenerationObservationLess(left, right QuotaObservation) bool {
	if left.FirstObservedAtMS != right.FirstObservedAtMS {
		return left.FirstObservedAtMS < right.FirstObservedAtMS
	}
	return quotaObservationLess(left, right)
}

func quotaKnownGenerationResetForWindow(
	knownGenerationsByMinutes map[int64][]*quotaResetGeneration,
	source QuotaSource,
	windowMinutes int64,
	resetAtMS int64,
) (int64, bool) {
	canonical := int64(-1)
	for _, generation := range knownGenerationsByMinutes[windowMinutes] {
		if !QuotaResetsEquivalentForWindow(source, windowMinutes, generation.anchorReset, resetAtMS) {
			continue
		}
		if generation.canonicalReset > canonical {
			canonical = generation.canonicalReset
		}
	}
	return canonical, canonical >= 0
}

func quotaLatestKnownGenerationReset(
	knownGenerationsByMinutes map[int64][]*quotaResetGeneration,
	windowMinutes int64,
) (int64, bool) {
	latest := int64(-1)
	for _, generation := range knownGenerationsByMinutes[windowMinutes] {
		if generation.canonicalReset > latest {
			latest = generation.canonicalReset
		}
	}
	return latest, latest >= 0
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
