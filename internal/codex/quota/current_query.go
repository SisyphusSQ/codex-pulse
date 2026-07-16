package quota

import (
	"context"
	"errors"
	"fmt"
	"sort"

	"github.com/SisyphusSQ/codex-pulse/internal/runtimeclock"
	"github.com/SisyphusSQ/codex-pulse/internal/store"
)

const CurrentContractVersion = "quota-current-v1"

var (
	ErrInvalidCurrentQuery     = errors.New("quota current query is invalid")
	ErrQuotaCurrentUnavailable = errors.New("quota current projection is unavailable")
)

type CurrentUnknownReason string

const (
	CurrentUnknownNeverLoaded       CurrentUnknownReason = "never_loaded"
	CurrentUnknownNoTrustedReset    CurrentUnknownReason = "no_trusted_reset"
	CurrentUnknownSourceUnavailable CurrentUnknownReason = "source_unavailable"
	CurrentUnknownScheduleMissing   CurrentUnknownReason = "schedule_unavailable"
)

type CurrentSourceKind string

const (
	CurrentSourceLocal CurrentSourceKind = "local_jsonl"
	CurrentSourceWham  CurrentSourceKind = "wham"
)

type CurrentRefreshState string

const (
	CurrentRefreshUnknown   CurrentRefreshState = "unknown"
	CurrentRefreshScheduled CurrentRefreshState = "scheduled"
	CurrentRefreshPaused    CurrentRefreshState = "paused"
	CurrentRefreshInFlight  CurrentRefreshState = "in_flight"
	CurrentRefreshDisabled  CurrentRefreshState = "disabled"
)

type CurrentResponse struct {
	Version       string              `json:"version"`
	AccountScope  string              `json:"accountScope"`
	EvaluatedAtMS int64               `json:"evaluatedAtMs"`
	Windows       []CurrentWindow     `json:"windows"`
	Sources       []CurrentSource     `json:"sources"`
	NextReset     CurrentNextReset    `json:"nextReset"`
	ResetCredits  CurrentResetCredits `json:"resetCredits"`
	Refresh       CurrentRefresh      `json:"refresh"`
}

type CurrentWindow struct {
	WindowKind       store.QuotaWindowKind       `json:"windowKind"`
	LimitID          string                      `json:"limitId"`
	UsedPercent      *float64                    `json:"usedPercent"`
	RemainingPercent *float64                    `json:"remainingPercent"`
	WindowMinutes    *int64                      `json:"windowMinutes"`
	ResetsAtMS       *int64                      `json:"resetsAtMs"`
	ResetRemainingMS *int64                      `json:"resetRemainingMs"`
	WindowGeneration *int64                      `json:"windowGeneration"`
	SelectedSource   *store.QuotaSource          `json:"selectedSource"`
	Freshness        store.QuotaCurrentFreshness `json:"freshness"`
	Conflict         store.QuotaConflictState    `json:"conflict"`
	ExplanationCode  store.QuotaExplanationCode  `json:"explanationCode"`
	UnknownReason    *CurrentUnknownReason       `json:"unknownReason"`
	LastSuccessAtMS  *int64                      `json:"lastSuccessAtMs"`
	LastAttemptAtMS  *int64                      `json:"lastAttemptAtMs"`
	Explanations     []CurrentExplanation        `json:"explanations"`
}

type CurrentExplanation struct {
	ObservationID    string                         `json:"observationId"`
	Source           store.QuotaSource              `json:"source"`
	UsedPercent      *float64                       `json:"usedPercent"`
	RemainingPercent *float64                       `json:"remainingPercent"`
	WindowMinutes    *int64                         `json:"windowMinutes"`
	ResetsAtMS       *int64                         `json:"resetsAtMs"`
	WindowGeneration *int64                         `json:"windowGeneration"`
	ObservedAtMS     int64                          `json:"observedAtMs"`
	Validity         store.QuotaValidity            `json:"validity"`
	Disposition      store.QuotaEvidenceDisposition `json:"disposition"`
	Reason           *store.QuotaRejectionReason    `json:"reason"`
	ExplanationCode  store.QuotaExplanationCode     `json:"explanationCode"`
}

type CurrentSource struct {
	Source              CurrentSourceKind        `json:"source"`
	LastObservedAtMS    *int64                   `json:"lastObservedAtMs"`
	LastSuccessAtMS     *int64                   `json:"lastSuccessAtMs"`
	LastAttemptAtMS     *int64                   `json:"lastAttemptAtMs"`
	Freshness           store.SourceFreshness    `json:"freshness"`
	FailureCode         *store.SourceFailureCode `json:"failureCode"`
	SelectedWindowCount int64                    `json:"selectedWindowCount"`
	ConflictWindowCount int64                    `json:"conflictWindowCount"`
	UnknownReason       *CurrentUnknownReason    `json:"unknownReason"`
}

type CurrentNextReset struct {
	AtMS               *int64                `json:"atMs"`
	RemainingMS        *int64                `json:"remainingMs"`
	TrustedWindowCount int64                 `json:"trustedWindowCount"`
	UnknownReason      *CurrentUnknownReason `json:"unknownReason"`
}

type CurrentResetCredits struct {
	AvailableCount        *int64                   `json:"availableCount"`
	TotalCount            *int64                   `json:"totalCount"`
	RedeemedCount         *int64                   `json:"redeemedCount"`
	CumulativeRemainingMS *int64                   `json:"cumulativeRemainingMs"`
	NextExpiresAtMS       *int64                   `json:"nextExpiresAtMs"`
	LastSuccessAtMS       *int64                   `json:"lastSuccessAtMs"`
	LastAttemptAtMS       *int64                   `json:"lastAttemptAtMs"`
	Freshness             store.SourceFreshness    `json:"freshness"`
	FailureCode           *store.SourceFailureCode `json:"failureCode"`
	UnknownReason         *CurrentUnknownReason    `json:"unknownReason"`
}

type CurrentRefresh struct {
	Quota        CurrentRefreshStatus `json:"quota"`
	ResetCredits CurrentRefreshStatus `json:"resetCredits"`
}

type CurrentRefreshStatus struct {
	State            CurrentRefreshState         `json:"state"`
	NextDueAtMS      *int64                      `json:"nextDueAtMs"`
	Reason           *store.SourceRefreshReason  `json:"reason"`
	LastManualAtMS   *int64                      `json:"lastManualAtMs"`
	ActiveTrigger    *store.SourceRefreshTrigger `json:"activeTrigger"`
	ClaimStartedAtMS *int64                      `json:"claimStartedAtMs"`
	ClaimExpiresAtMS *int64                      `json:"claimExpiresAtMs"`
	UnknownReason    *CurrentUnknownReason       `json:"unknownReason"`
}

type CurrentSnapshotReader interface {
	QuotaCurrentSnapshot(context.Context, string, int64) (store.QuotaCurrentSnapshot, error)
}

type CurrentQueryService struct {
	reader CurrentSnapshotReader
}

func NewCurrentQueryService(reader CurrentSnapshotReader) (*CurrentQueryService, error) {
	if reader == nil {
		return nil, ErrInvalidCurrentQuery
	}
	return &CurrentQueryService{reader: reader}, nil
}

func (service *CurrentQueryService) Query(ctx context.Context, evaluatedAtMS int64) (CurrentResponse, error) {
	if service == nil || service.reader == nil || evaluatedAtMS < 0 ||
		evaluatedAtMS > runtimeclock.MaxTimestampMS {
		return CurrentResponse{}, ErrInvalidCurrentQuery
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return CurrentResponse{}, err
	}
	snapshot, err := service.reader.QuotaCurrentSnapshot(
		ctx, store.QuotaAccountScopeDefault, evaluatedAtMS,
	)
	if errors.Is(err, store.ErrNotFound) || errors.Is(err, store.ErrInvalidRecord) {
		return CurrentResponse{}, fmt.Errorf(
			"%w: stored facts are missing or invalid: %w", ErrQuotaCurrentUnavailable, err,
		)
	}
	if err != nil {
		return CurrentResponse{}, err
	}
	return mapCurrentResponse(snapshot, evaluatedAtMS)
}

func mapCurrentResponse(snapshot store.QuotaCurrentSnapshot, evaluatedAtMS int64) (CurrentResponse, error) {
	if snapshot.AccountScope != store.QuotaAccountScopeDefault || snapshot.EvaluatedAtMS != evaluatedAtMS {
		return CurrentResponse{}, fmt.Errorf("%w: snapshot identity is inconsistent", ErrInvalidCurrentQuery)
	}
	windows := append([]store.QuotaCurrentWindowSnapshot(nil), snapshot.Windows...)
	sort.Slice(windows, func(left, right int) bool {
		leftRank := currentWindowRank(windows[left].Current.WindowKind)
		rightRank := currentWindowRank(windows[right].Current.WindowKind)
		if leftRank != rightRank {
			return leftRank < rightRank
		}
		return windows[left].Current.LimitID < windows[right].Current.LimitID
	})
	response := CurrentResponse{
		Version: CurrentContractVersion, AccountScope: snapshot.AccountScope,
		EvaluatedAtMS: evaluatedAtMS, Windows: make([]CurrentWindow, 0, len(windows)),
	}
	currents := make([]store.QuotaCurrent, 0, len(windows))
	for _, facts := range windows {
		window, err := mapCurrentWindow(facts, evaluatedAtMS)
		if err != nil {
			return CurrentResponse{}, err
		}
		response.Windows = append(response.Windows, window)
		currents = append(currents, facts.Current)
	}
	response.Sources = mapCurrentSources(response.Windows, snapshot.WhamSourceState, evaluatedAtMS)
	resetSummary, err := CalculateQuotaResetSummary(currents, evaluatedAtMS)
	if err != nil {
		return CurrentResponse{}, err
	}
	response.NextReset = CurrentNextReset{
		AtMS: cloneInt64(resetSummary.NextResetAtMS), RemainingMS: cloneInt64(resetSummary.RemainingMS),
		TrustedWindowCount: resetSummary.TrustedWindowCount,
	}
	if response.NextReset.AtMS == nil {
		response.NextReset.UnknownReason = currentUnknownPointer(CurrentUnknownNoTrustedReset)
	}
	response.ResetCredits, err = mapCurrentResetCredits(snapshot.ResetCredits, evaluatedAtMS)
	if err != nil {
		return CurrentResponse{}, err
	}
	quotaRefresh, err := mapCurrentRefreshStatus(
		snapshot.QuotaRefresh, store.QuotaSourceInstanceWhamDefault, store.QuotaSourceTypeWham,
	)
	if err != nil {
		return CurrentResponse{}, err
	}
	resetRefresh, err := mapCurrentRefreshStatus(
		snapshot.ResetCreditsRefresh, store.ResetCreditsSourceInstanceWhamDefault,
		store.ResetCreditsSourceTypeWham,
	)
	if err != nil {
		return CurrentResponse{}, err
	}
	response.Refresh = CurrentRefresh{Quota: quotaRefresh, ResetCredits: resetRefresh}
	return response, nil
}

func mapCurrentWindow(
	facts store.QuotaCurrentWindowSnapshot,
	evaluatedAtMS int64,
) (CurrentWindow, error) {
	current := facts.Current
	if current.AccountScope != store.QuotaAccountScopeDefault || current.LimitID == "" ||
		current.EvaluatedAtMS != evaluatedAtMS {
		return CurrentWindow{}, fmt.Errorf("%w: window identity is inconsistent", ErrInvalidCurrentQuery)
	}
	window := CurrentWindow{
		WindowKind: current.WindowKind, LimitID: current.LimitID,
		UsedPercent:   cloneFloat64(current.EffectiveUsedPercent),
		WindowMinutes: cloneInt64(current.WindowMinutes), ResetsAtMS: cloneInt64(current.ResetsAtMS),
		WindowGeneration: cloneInt64(current.WindowGeneration), SelectedSource: cloneQuotaSource(current.SelectedSource),
		Freshness: current.FreshnessState, Conflict: current.ConflictState,
		ExplanationCode: current.ExplanationCode, LastSuccessAtMS: cloneInt64(current.LastSuccessAtMS),
		LastAttemptAtMS: cloneInt64(current.LastAttemptAtMS),
	}
	if window.UsedPercent == nil {
		if current.ObservationID != nil || current.FreshnessState != store.QuotaCurrentNeverLoaded {
			return CurrentWindow{}, fmt.Errorf("%w: unknown window shape is inconsistent", ErrInvalidCurrentQuery)
		}
		window.UnknownReason = currentUnknownPointer(CurrentUnknownNeverLoaded)
	} else {
		if *window.UsedPercent < 0 || *window.UsedPercent > 100 || current.ObservationID == nil ||
			window.WindowMinutes == nil || window.ResetsAtMS == nil || window.WindowGeneration == nil ||
			window.SelectedSource == nil {
			return CurrentWindow{}, fmt.Errorf("%w: selected window shape is inconsistent", ErrInvalidCurrentQuery)
		}
		remaining := 100 - *window.UsedPercent
		window.RemainingPercent = &remaining
		if currentWindowResetIsTrusted(current.FreshnessState) && *window.ResetsAtMS > evaluatedAtMS {
			remainingMS := *window.ResetsAtMS - evaluatedAtMS
			window.ResetRemainingMS = &remainingMS
		}
	}
	explanations, err := mapCurrentExplanations(facts, current.ObservationID)
	if err != nil {
		return CurrentWindow{}, err
	}
	window.Explanations = explanations
	return window, nil
}

func mapCurrentExplanations(
	facts store.QuotaCurrentWindowSnapshot,
	selectedID *string,
) ([]CurrentExplanation, error) {
	observations := make(map[string]store.QuotaObservation, len(facts.Observations))
	for _, observation := range facts.Observations {
		if observation.ObservationID == "" {
			return nil, fmt.Errorf("%w: observation identity is missing", ErrInvalidCurrentQuery)
		}
		if _, duplicate := observations[observation.ObservationID]; duplicate {
			return nil, fmt.Errorf("%w: observation identity is duplicated", ErrInvalidCurrentQuery)
		}
		observations[observation.ObservationID] = observation
	}
	evidence := append([]store.QuotaArbitrationEvidence(nil), facts.Evidence...)
	sort.Slice(evidence, func(left, right int) bool {
		return evidence[left].ObservationID < evidence[right].ObservationID
	})
	result := make([]CurrentExplanation, 0, len(evidence))
	selectedFound := selectedID == nil
	for _, item := range evidence {
		observation, found := observations[item.ObservationID]
		if !found || observation.LimitID == nil || *observation.LimitID != facts.Current.LimitID ||
			observation.WindowKind != facts.Current.WindowKind || observation.AccountScope != facts.Current.AccountScope {
			return nil, fmt.Errorf("%w: explanation observation is inconsistent", ErrInvalidCurrentQuery)
		}
		used := observation.UsedPercent
		remaining := 100 - used
		minutes := observation.WindowMinutes
		reset := observation.ResetsAtMS
		result = append(result, CurrentExplanation{
			ObservationID: item.ObservationID, Source: observation.Source,
			UsedPercent: &used, RemainingPercent: &remaining, WindowMinutes: &minutes,
			ResetsAtMS: &reset, WindowGeneration: cloneInt64(item.WindowGeneration),
			ObservedAtMS: observation.LastObservedAtMS, Validity: observation.Validity,
			Disposition: item.Disposition, Reason: cloneQuotaRejectionReason(item.Reason),
			ExplanationCode: item.ExplanationCode,
		})
		if selectedID != nil && item.ObservationID == *selectedID && item.Disposition == store.QuotaEvidenceSelected {
			selectedFound = true
		}
	}
	if len(observations) != len(evidence) || !selectedFound {
		return nil, fmt.Errorf("%w: explanation set is incomplete", ErrInvalidCurrentQuery)
	}
	return result, nil
}

func mapCurrentSources(
	windows []CurrentWindow,
	wham *store.SourceState,
	evaluatedAtMS int64,
) []CurrentSource {
	local := CurrentSource{
		Source: CurrentSourceLocal, Freshness: store.SourceFreshnessUnknown,
		UnknownReason: currentUnknownPointer(CurrentUnknownSourceUnavailable),
	}
	online := CurrentSource{
		Source: CurrentSourceWham, Freshness: store.SourceFreshnessUnknown,
		UnknownReason: currentUnknownPointer(CurrentUnknownSourceUnavailable),
	}
	localHasAccepted := false
	localHasFresh := false
	for _, window := range windows {
		for _, explanation := range window.Explanations {
			target := &local
			if explanation.Source == store.QuotaSourceWham {
				target = &online
			}
			if target.LastObservedAtMS == nil || explanation.ObservedAtMS > *target.LastObservedAtMS {
				observedAt := explanation.ObservedAtMS
				target.LastObservedAtMS = &observedAt
			}
			if explanation.Disposition == store.QuotaEvidenceSelected {
				target.SelectedWindowCount++
			}
			if explanation.Source == store.QuotaSourceLocalJSONL &&
				explanation.Validity == store.QuotaValidityAccepted {
				localHasAccepted = true
				if currentLocalObservationIsFresh(explanation, evaluatedAtMS) {
					localHasFresh = true
				}
			}
		}
		if window.Conflict == store.QuotaConflictPresent {
			for _, explanation := range window.Explanations {
				if explanation.Source == store.QuotaSourceLocalJSONL {
					local.ConflictWindowCount++
					break
				}
			}
			for _, explanation := range window.Explanations {
				if explanation.Source == store.QuotaSourceWham {
					online.ConflictWindowCount++
					break
				}
			}
		}
	}
	if localHasAccepted {
		local.Freshness = store.SourceFreshnessStale
		if localHasFresh {
			local.Freshness = store.SourceFreshnessCurrent
		}
		local.UnknownReason = nil
	}
	if wham != nil {
		online.LastSuccessAtMS = cloneInt64(wham.LastSuccessAtMS)
		online.LastAttemptAtMS = cloneInt64(wham.LastAttemptAtMS)
		online.Freshness = wham.FreshnessState
		online.FailureCode = cloneSourceFailureCode(wham.LastFailureCode)
		online.UnknownReason = nil
	}
	return []CurrentSource{local, online}
}

func currentWindowResetIsTrusted(freshness store.QuotaCurrentFreshness) bool {
	return freshness == store.QuotaCurrentFresh || freshness == store.QuotaCurrentStale
}

func currentLocalObservationIsFresh(explanation CurrentExplanation, evaluatedAtMS int64) bool {
	if explanation.ResetsAtMS == nil || *explanation.ResetsAtMS <= evaluatedAtMS {
		return false
	}
	freshForMS := store.DefaultQuotaArbitrationRule().FreshForMS
	freshUntilMS := *explanation.ResetsAtMS
	if explanation.ObservedAtMS <= runtimeclock.MaxTimestampMS-freshForMS &&
		explanation.ObservedAtMS+freshForMS < freshUntilMS {
		freshUntilMS = explanation.ObservedAtMS + freshForMS
	}
	return evaluatedAtMS <= freshUntilMS
}

func mapCurrentResetCredits(
	summary store.ResetCreditsSummary,
	evaluatedAtMS int64,
) (CurrentResetCredits, error) {
	if summary.AccountScope != store.QuotaAccountScopeDefault ||
		summary.EvaluationAtMS != evaluatedAtMS || !validCurrentSourceFreshness(summary.FreshnessState) ||
		!validCurrentOptionalTimestamp(summary.LastSuccessAtMS) ||
		!validCurrentOptionalTimestamp(summary.LastAttemptAtMS) {
		return CurrentResetCredits{}, fmt.Errorf(
			"%w: reset credits identity or source state is inconsistent", ErrInvalidCurrentQuery,
		)
	}
	loaded := summary.SnapshotID != nil
	countsComplete := summary.AvailableCount != nil && summary.TotalCount != nil &&
		summary.RedeemedCount != nil && summary.CumulativeRemainingMS != nil
	if loaded != countsComplete {
		return CurrentResetCredits{}, fmt.Errorf(
			"%w: reset credits value shape is inconsistent", ErrInvalidCurrentQuery,
		)
	}
	if !loaded {
		if summary.NextExpiresAtMS != nil {
			return CurrentResetCredits{}, fmt.Errorf(
				"%w: reset credits unknown shape is inconsistent", ErrInvalidCurrentQuery,
			)
		}
	} else if *summary.SnapshotID == "" || summary.LastSuccessAtMS == nil ||
		summary.LastAttemptAtMS == nil || *summary.AvailableCount < 0 || *summary.TotalCount < 0 ||
		*summary.RedeemedCount < 0 || *summary.CumulativeRemainingMS < 0 ||
		*summary.AvailableCount > *summary.TotalCount || *summary.RedeemedCount > *summary.TotalCount ||
		*summary.AvailableCount > *summary.TotalCount-*summary.RedeemedCount ||
		!validCurrentOptionalTimestamp(summary.NextExpiresAtMS) ||
		(summary.NextExpiresAtMS != nil && (*summary.NextExpiresAtMS <= evaluatedAtMS ||
			*summary.AvailableCount == 0)) {
		return CurrentResetCredits{}, fmt.Errorf(
			"%w: reset credits values are inconsistent", ErrInvalidCurrentQuery,
		)
	}
	result := CurrentResetCredits{
		AvailableCount: cloneInt64(summary.AvailableCount), TotalCount: cloneInt64(summary.TotalCount),
		RedeemedCount:         cloneInt64(summary.RedeemedCount),
		CumulativeRemainingMS: cloneInt64(summary.CumulativeRemainingMS),
		NextExpiresAtMS:       cloneInt64(summary.NextExpiresAtMS), LastSuccessAtMS: cloneInt64(summary.LastSuccessAtMS),
		LastAttemptAtMS: cloneInt64(summary.LastAttemptAtMS), Freshness: summary.FreshnessState,
		FailureCode: cloneSourceFailureCode(summary.LastFailureCode),
	}
	if result.AvailableCount == nil {
		result.UnknownReason = currentUnknownPointer(CurrentUnknownNeverLoaded)
	}
	return result, nil
}

func validCurrentSourceFreshness(value store.SourceFreshness) bool {
	switch value {
	case store.SourceFreshnessUnknown, store.SourceFreshnessCurrent,
		store.SourceFreshnessStale, store.SourceFreshnessUnavailable:
		return true
	default:
		return false
	}
}

func validCurrentOptionalTimestamp(value *int64) bool {
	return value == nil || (*value >= 0 && *value <= runtimeclock.MaxTimestampMS)
}

func mapCurrentRefreshStatus(
	schedule *store.SourceRefreshSchedule,
	expectedInstanceID string,
	expectedSourceType string,
) (CurrentRefreshStatus, error) {
	if schedule == nil {
		return CurrentRefreshStatus{
			State: CurrentRefreshUnknown, UnknownReason: currentUnknownPointer(CurrentUnknownScheduleMissing),
		}, nil
	}
	if schedule.SourceInstanceID != expectedInstanceID || schedule.SourceType != expectedSourceType ||
		schedule.ScopeKey != store.QuotaAccountScopeDefault {
		return CurrentRefreshStatus{}, fmt.Errorf("%w: refresh identity is inconsistent", ErrInvalidCurrentQuery)
	}
	reason := schedule.Reason
	result := CurrentRefreshStatus{
		NextDueAtMS: cloneInt64(schedule.NextDueAtMS), Reason: &reason,
		LastManualAtMS:   cloneInt64(schedule.LastManualAtMS),
		ActiveTrigger:    cloneSourceRefreshTrigger(schedule.ActiveTrigger),
		ClaimStartedAtMS: cloneInt64(schedule.ClaimStartedAtMS),
		ClaimExpiresAtMS: cloneInt64(schedule.ClaimExpiresAtMS),
	}
	switch {
	case schedule.ActiveClaimID != nil:
		result.State = CurrentRefreshInFlight
	case schedule.Reason == store.RefreshReasonDisabled:
		result.State = CurrentRefreshDisabled
	case schedule.NextDueAtMS != nil:
		result.State = CurrentRefreshScheduled
	default:
		result.State = CurrentRefreshPaused
	}
	return result, nil
}

func currentWindowRank(kind store.QuotaWindowKind) int {
	switch kind {
	case store.QuotaWindowPrimary:
		return 0
	case store.QuotaWindowSecondary:
		return 1
	default:
		return 2
	}
}

func currentUnknownPointer(value CurrentUnknownReason) *CurrentUnknownReason {
	return &value
}

func cloneFloat64(value *float64) *float64 {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}

func cloneQuotaSource(value *store.QuotaSource) *store.QuotaSource {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}

func cloneQuotaRejectionReason(value *store.QuotaRejectionReason) *store.QuotaRejectionReason {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}

func cloneSourceFailureCode(value *store.SourceFailureCode) *store.SourceFailureCode {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}

func cloneSourceRefreshTrigger(value *store.SourceRefreshTrigger) *store.SourceRefreshTrigger {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}
