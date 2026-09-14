package quota

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sort"

	"github.com/SisyphusSQ/codex-pulse/internal/runtimeclock"
	"github.com/SisyphusSQ/codex-pulse/internal/store"
)

const CurrentContractVersion = "quota-current-v1"
const maxLimitIdentityBytes = 512

var (
	ErrInvalidCurrentQuery     = errors.New("quota current query is invalid")
	ErrQuotaCurrentUnavailable = errors.New("quota current projection is unavailable")
)

type CurrentUnknownReason string

const (
	CurrentUnknownNeverLoaded        CurrentUnknownReason = "never_loaded"
	CurrentUnknownNotApplicable      CurrentUnknownReason = "not_applicable"
	CurrentUnknownNoTrustedReset     CurrentUnknownReason = "no_trusted_reset"
	CurrentUnknownSourceUnavailable  CurrentUnknownReason = "source_unavailable"
	CurrentUnknownScheduleMissing    CurrentUnknownReason = "schedule_unavailable"
	CurrentUnknownBindingUnavailable CurrentUnknownReason = "binding_unavailable"
)

type CurrentSourceKind string

const (
	CurrentSourceLocal                  CurrentSourceKind = "local_jsonl"
	CurrentSourceWham                   CurrentSourceKind = "wham"
	CurrentSourceAppServer              CurrentSourceKind = "app_server"
	CurrentSourceCursorDashboard        CurrentSourceKind = "cursor_dashboard"
	CurrentSourceCursorDashboardGrokBot CurrentSourceKind = "cursor.dashboard.grok_bot"
	CurrentSourceGrokBilling            CurrentSourceKind = "grok_billing"
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
	Version       string                     `json:"version"`
	AccountScope  string                     `json:"accountScope"`
	EvaluatedAtMS int64                      `json:"evaluatedAtMs"`
	Binding       *store.CodexAccountBinding `json:"binding,omitempty"`
	Windows       []CurrentWindow            `json:"windows"`
	Sources       []CurrentSource            `json:"sources"`
	NextReset     CurrentNextReset           `json:"nextReset"`
	ResetCredits  CurrentResetCredits        `json:"resetCredits"`
	Refresh       CurrentRefresh             `json:"refresh"`
}

func publishedBinding(binding store.CodexAccountBinding) *store.CodexAccountBinding {
	if binding.State == "" {
		return nil
	}
	published := binding
	return &published
}

type CurrentWindow struct {
	WindowKind       store.QuotaWindowKind       `json:"windowKind"`
	LimitID          string                      `json:"limitId"`
	LimitName        *string                     `json:"limitName"`
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
	AvailableCount        *int64                         `json:"availableCount"`
	CumulativeRemainingMS *int64                         `json:"cumulativeRemainingMs"`
	NextExpiresAtMS       *int64                         `json:"nextExpiresAtMs"`
	LastSuccessAtMS       *int64                         `json:"lastSuccessAtMs"`
	LastAttemptAtMS       *int64                         `json:"lastAttemptAtMs"`
	Freshness             store.SourceFreshness          `json:"freshness"`
	FailureCode           *store.SourceFailureCode       `json:"failureCode"`
	DetailsState          store.ResetCreditDetailsStatus `json:"detailsState,omitempty"`
	UnknownReason         *CurrentUnknownReason          `json:"unknownReason"`
	Items                 []CurrentResetCreditItem       `json:"items"`
}

type CurrentResetCreditItem struct {
	Status       store.ResetCreditStatus `json:"status"`
	Type         store.ResetCreditType   `json:"type"`
	GrantedAtMS  int64                   `json:"grantedAtMs"`
	ExpiresAtMS  *int64                  `json:"expiresAtMs,omitempty"`
	RedeemedAtMS *int64                  `json:"redeemedAtMs"`
	RemainingMS  *int64                  `json:"remainingMs"`
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
	if snapshot.Binding.State != "" {
		return mapCodexBoundCurrentResponse(snapshot, evaluatedAtMS)
	}
	if snapshot.AccountScope != store.QuotaAccountScopeDefault || snapshot.EvaluatedAtMS != evaluatedAtMS {
		return CurrentResponse{}, fmt.Errorf("%w: snapshot identity is inconsistent", ErrInvalidCurrentQuery)
	}
	return mapBoundCurrentResponse(snapshot, evaluatedAtMS, store.QuotaAccountScopeDefault, store.QuotaSourceInstanceWhamDefault, store.QuotaSourceTypeWham, store.ResetCreditsSourceInstanceWhamDefault, store.ResetCreditsSourceTypeWham)
}

func mapCodexBoundCurrentResponse(snapshot store.QuotaCurrentSnapshot, evaluatedAtMS int64) (CurrentResponse, error) {
	if snapshot.EvaluatedAtMS != evaluatedAtMS {
		return CurrentResponse{}, fmt.Errorf("%w: snapshot identity is inconsistent", ErrInvalidCurrentQuery)
	}
	if snapshot.Binding.State != store.CodexAccountBindingConfirmed || snapshot.Binding.AccountScope == nil {
		reason := CurrentUnknownBindingUnavailable
		return CurrentResponse{
			Version: CurrentContractVersion, EvaluatedAtMS: evaluatedAtMS, Binding: publishedBinding(snapshot.Binding),
			Windows: []CurrentWindow{},
			Sources: []CurrentSource{{
				Source: CurrentSourceAppServer, Freshness: store.SourceFreshnessUnknown,
				UnknownReason: &reason,
			}},
			NextReset:    CurrentNextReset{UnknownReason: &reason},
			ResetCredits: CurrentResetCredits{Freshness: store.SourceFreshnessUnknown, UnknownReason: &reason},
			Refresh: CurrentRefresh{
				Quota:        CurrentRefreshStatus{State: CurrentRefreshUnknown, UnknownReason: &reason},
				ResetCredits: CurrentRefreshStatus{State: CurrentRefreshUnknown, UnknownReason: &reason},
			},
		}, nil
	}
	scope := *snapshot.Binding.AccountScope
	if snapshot.AccountScope != scope || snapshot.BindingGeneration != snapshot.Binding.BindingGeneration {
		return CurrentResponse{}, fmt.Errorf("%w: snapshot identity is inconsistent", ErrInvalidCurrentQuery)
	}
	return mapBoundCurrentResponse(
		snapshot, evaluatedAtMS, scope,
		store.QuotaSourceInstanceAppServer(scope), store.QuotaSourceTypeAppServerRateLimits,
		store.ResetCreditsSourceInstanceAppServer(scope), store.ResetCreditsSourceTypeAppServer,
	)
}

func mapBoundCurrentResponse(
	snapshot store.QuotaCurrentSnapshot,
	evaluatedAtMS int64,
	expectedScope string,
	quotaInstanceID string,
	quotaSourceType string,
	resetInstanceID string,
	resetSourceType string,
) (CurrentResponse, error) {
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
		EvaluatedAtMS: evaluatedAtMS, Binding: publishedBinding(snapshot.Binding),
		Windows: make([]CurrentWindow, 0, len(windows)),
	}
	currents := make([]store.QuotaCurrent, 0, len(windows))
	for _, facts := range windows {
		window, err := mapCurrentWindow(facts, expectedScope, evaluatedAtMS)
		if err != nil {
			return CurrentResponse{}, err
		}
		response.Windows = append(response.Windows, window)
		currents = append(currents, facts.Current)
	}
	response.Sources = mapCurrentSources(
		response.Windows, snapshot.OnlineSourceState, evaluatedAtMS, quotaSourceType,
	)
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
	response.ResetCredits, err = mapCurrentResetCredits(snapshot.ResetCredits, expectedScope, evaluatedAtMS)
	if err != nil {
		return CurrentResponse{}, err
	}
	quotaRefresh, err := mapCurrentRefreshStatus(
		snapshot.QuotaRefresh, quotaInstanceID, quotaSourceType, expectedScope, snapshot.BindingGeneration,
	)
	if err != nil {
		return CurrentResponse{}, err
	}
	resetRefresh, err := mapCurrentRefreshStatus(
		snapshot.ResetCreditsRefresh, resetInstanceID, resetSourceType, expectedScope, snapshot.BindingGeneration,
	)
	if err != nil {
		return CurrentResponse{}, err
	}
	response.Refresh = CurrentRefresh{Quota: quotaRefresh, ResetCredits: resetRefresh}
	if snapshot.Binding.State != "" {
		filtered := make([]CurrentSource, 0, len(response.Sources))
		for _, source := range response.Sources {
			if source.Source == CurrentSourceLocal {
				continue
			}
			if source.Source == CurrentSourceWham {
				source.Source = CurrentSourceAppServer
			}
			filtered = append(filtered, source)
		}
		response.Sources = filtered
	}
	return response, nil
}

func mapCurrentWindow(
	facts store.QuotaCurrentWindowSnapshot,
	expectedScope string,
	evaluatedAtMS int64,
) (CurrentWindow, error) {
	current := facts.Current
	if current.AccountScope != expectedScope || current.LimitID == "" ||
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
	limitName, err := currentLimitName(facts.Observations, current.ObservationID)
	if err != nil {
		return CurrentWindow{}, err
	}
	window.LimitName = limitName
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

func currentLimitName(
	observations []store.QuotaObservation,
	selectedID *string,
) (*string, error) {
	if selectedID == nil {
		return nil, nil
	}
	selectedFound := false
	var latestName *string
	var latestObservedAtMS int64
	latestNameConflict := false
	// 展示名不参与额度数值仲裁；选中记录缺名时，可由同一逻辑额度的最新 accepted 事实补全。
	for _, observation := range observations {
		if observation.ObservationID == *selectedID {
			selectedFound = true
			if observation.LimitName != nil {
				if *observation.LimitName == "" || len(*observation.LimitName) > maxLimitIdentityBytes {
					return nil, fmt.Errorf("%w: selected limit name is invalid", ErrInvalidCurrentQuery)
				}
				value := *observation.LimitName
				return &value, nil
			}
		}
		if observation.Validity != store.QuotaValidityAccepted || observation.LimitName == nil {
			continue
		}
		if *observation.LimitName == "" || len(*observation.LimitName) > maxLimitIdentityBytes {
			return nil, fmt.Errorf("%w: supplemental limit name is invalid", ErrInvalidCurrentQuery)
		}
		switch {
		case latestName == nil || observation.LastObservedAtMS > latestObservedAtMS:
			value := *observation.LimitName
			latestName = &value
			latestObservedAtMS = observation.LastObservedAtMS
			latestNameConflict = false
		case observation.LastObservedAtMS == latestObservedAtMS &&
			*observation.LimitName != *latestName:
			latestNameConflict = true
		}
	}
	if !selectedFound {
		return nil, fmt.Errorf("%w: selected limit name observation is missing", ErrInvalidCurrentQuery)
	}
	if latestNameConflict {
		return nil, nil
	}
	return latestName, nil
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
	onlineState *store.SourceState,
	evaluatedAtMS int64,
	onlineSourceType string,
) []CurrentSource {
	local := CurrentSource{
		Source: CurrentSourceLocal, Freshness: store.SourceFreshnessUnknown,
		UnknownReason: currentUnknownPointer(CurrentUnknownSourceUnavailable),
	}
	online := CurrentSource{
		Source: CurrentSourceWham, Freshness: store.SourceFreshnessUnknown,
		UnknownReason: currentUnknownPointer(CurrentUnknownSourceUnavailable),
	}
	if onlineSourceType == store.QuotaSourceTypeAppServerRateLimits {
		online.Source = CurrentSourceAppServer
	}
	localHasAccepted := false
	localHasFresh := false
	for _, window := range windows {
		for _, explanation := range window.Explanations {
			target := &local
			if explanation.Source == store.QuotaSourceWham || explanation.Source == store.QuotaSourceAppServer {
				target = &online
			}
			if explanation.Source == store.QuotaSourceAppServer {
				online.Source = CurrentSourceAppServer
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
				if explanation.Source == store.QuotaSourceWham || explanation.Source == store.QuotaSourceAppServer {
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
	if onlineState != nil {
		online.LastSuccessAtMS = cloneInt64(onlineState.LastSuccessAtMS)
		online.LastAttemptAtMS = cloneInt64(onlineState.LastAttemptAtMS)
		online.Freshness = onlineState.FreshnessState
		online.FailureCode = cloneSourceFailureCode(onlineState.LastFailureCode)
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
	expectedScope string,
	evaluatedAtMS int64,
) (CurrentResetCredits, error) {
	if summary.AccountScope != expectedScope ||
		summary.EvaluationAtMS != evaluatedAtMS || !validCurrentSourceFreshness(summary.FreshnessState) ||
		!validCurrentOptionalTimestamp(summary.LastSuccessAtMS) ||
		!validCurrentOptionalTimestamp(summary.LastAttemptAtMS) {
		return CurrentResetCredits{}, fmt.Errorf(
			"%w: reset credits identity or source state is inconsistent", ErrInvalidCurrentQuery,
		)
	}
	loaded := summary.SnapshotID != nil
	if !loaded {
		if summary.AvailableCount != nil || summary.CumulativeRemainingMS != nil ||
			summary.NextExpiresAtMS != nil || len(summary.Credits) != 0 {
			return CurrentResetCredits{}, fmt.Errorf(
				"%w: reset credits unknown shape is inconsistent", ErrInvalidCurrentQuery,
			)
		}
		return CurrentResetCredits{
			LastSuccessAtMS: cloneInt64(summary.LastSuccessAtMS),
			LastAttemptAtMS: cloneInt64(summary.LastAttemptAtMS), Freshness: summary.FreshnessState,
			FailureCode:   cloneSourceFailureCode(summary.LastFailureCode),
			UnknownReason: currentUnknownPointer(CurrentUnknownNeverLoaded),
		}, nil
	}
	if *summary.SnapshotID == "" || summary.AvailableCount == nil || *summary.AvailableCount < 0 ||
		summary.LastSuccessAtMS == nil || summary.LastAttemptAtMS == nil {
		return CurrentResetCredits{}, fmt.Errorf(
			"%w: reset credits values are inconsistent", ErrInvalidCurrentQuery,
		)
	}
	result := CurrentResetCredits{
		AvailableCount:  cloneInt64(summary.AvailableCount),
		LastSuccessAtMS: cloneInt64(summary.LastSuccessAtMS),
		LastAttemptAtMS: cloneInt64(summary.LastAttemptAtMS), Freshness: summary.FreshnessState,
		FailureCode: cloneSourceFailureCode(summary.LastFailureCode),
		Items:       mapCurrentResetCreditItems(summary.Credits, evaluatedAtMS),
	}
	if summary.Credits == nil && *summary.AvailableCount > 0 && summary.CumulativeRemainingMS == nil {
		if summary.NextExpiresAtMS != nil {
			return CurrentResetCredits{}, fmt.Errorf(
				"%w: reset credits count-only shape is inconsistent", ErrInvalidCurrentQuery,
			)
		}
		result.DetailsState = store.ResetCreditDetailsUnavailable
		return result, nil
	}
	if summary.CumulativeRemainingMS == nil {
		if len(summary.Credits) == 0 {
			return CurrentResetCredits{}, fmt.Errorf(
				"%w: reset credits value shape is inconsistent", ErrInvalidCurrentQuery,
			)
		}
		if summary.NextExpiresAtMS != nil {
			return CurrentResetCredits{}, fmt.Errorf(
				"%w: reset credits partial shape is inconsistent", ErrInvalidCurrentQuery,
			)
		}
		availableItems := resetCreditAvailableItemCount(summary.Credits, evaluatedAtMS)
		if availableItems < *summary.AvailableCount {
			result.DetailsState = store.ResetCreditDetailsPartial
		} else {
			result.DetailsState = store.ResetCreditDetailsComplete
		}
		return result, nil
	}
	if *summary.CumulativeRemainingMS < 0 ||
		!validCurrentOptionalTimestamp(summary.NextExpiresAtMS) ||
		!resetCreditInventorySummaryIsValid(summary, evaluatedAtMS) {
		return CurrentResetCredits{}, fmt.Errorf(
			"%w: reset credits values are inconsistent", ErrInvalidCurrentQuery,
		)
	}
	result.DetailsState = store.ResetCreditDetailsComplete
	result.CumulativeRemainingMS = cloneInt64(summary.CumulativeRemainingMS)
	result.NextExpiresAtMS = cloneInt64(summary.NextExpiresAtMS)
	return result, nil
}

func resetCreditAvailableItemCount(credits []store.ResetCredit, evaluatedAtMS int64) int64 {
	available := int64(0)
	for _, credit := range credits {
		if credit.Status != store.ResetCreditAvailable {
			continue
		}
		if credit.ExpiresAtMS != nil && *credit.ExpiresAtMS <= evaluatedAtMS {
			continue
		}
		available++
	}
	return available
}

func resetCreditInventorySummaryIsValid(summary store.ResetCreditsSummary, evaluatedAtMS int64) bool {
	available, cumulative := int64(0), int64(0)
	var next *int64
	for _, credit := range summary.Credits {
		if credit.Status != store.ResetCreditAvailable || credit.ExpiresAtMS == nil || *credit.ExpiresAtMS <= evaluatedAtMS {
			continue
		}
		available++
		remaining := *credit.ExpiresAtMS - evaluatedAtMS
		if cumulative <= math.MaxInt64-remaining {
			cumulative += remaining
		} else {
			cumulative = math.MaxInt64
		}
		if next == nil || *credit.ExpiresAtMS < *next {
			value := *credit.ExpiresAtMS
			next = &value
		}
	}
	if summary.AvailableCount == nil || *summary.AvailableCount != available ||
		summary.CumulativeRemainingMS == nil || *summary.CumulativeRemainingMS != cumulative {
		return false
	}
	if next == nil {
		return summary.NextExpiresAtMS == nil
	}
	return summary.NextExpiresAtMS != nil && *summary.NextExpiresAtMS == *next
}

func mapCurrentResetCreditItems(
	credits []store.ResetCredit,
	evaluatedAtMS int64,
) []CurrentResetCreditItem {
	items := make([]CurrentResetCreditItem, 0, len(credits))
	for _, credit := range credits {
		status := credit.Status
		var remaining *int64
		if status == store.ResetCreditAvailable {
			if credit.ExpiresAtMS != nil && *credit.ExpiresAtMS <= evaluatedAtMS {
				status = store.ResetCreditExpired
			} else if credit.ExpiresAtMS != nil {
				value := *credit.ExpiresAtMS - evaluatedAtMS
				remaining = &value
			}
		}
		items = append(items, CurrentResetCreditItem{
			Status: status, Type: credit.Type, GrantedAtMS: credit.GrantedAtMS,
			ExpiresAtMS: cloneInt64(credit.ExpiresAtMS), RedeemedAtMS: cloneInt64(credit.RedeemedAtMS),
			RemainingMS: remaining,
		})
	}
	sort.Slice(items, func(left, right int) bool {
		leftExpires := resetCreditExpiresAtMS(items[left].ExpiresAtMS)
		rightExpires := resetCreditExpiresAtMS(items[right].ExpiresAtMS)
		if leftExpires != rightExpires {
			return leftExpires < rightExpires
		}
		if items[left].GrantedAtMS != items[right].GrantedAtMS {
			return items[left].GrantedAtMS < items[right].GrantedAtMS
		}
		return items[left].Status < items[right].Status
	})
	return items
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
	expectedScope string,
	expectedGeneration int64,
) (CurrentRefreshStatus, error) {
	if schedule == nil {
		return CurrentRefreshStatus{
			State: CurrentRefreshUnknown, UnknownReason: currentUnknownPointer(CurrentUnknownScheduleMissing),
		}, nil
	}
	if schedule.SourceInstanceID != expectedInstanceID || schedule.SourceType != expectedSourceType ||
		schedule.ScopeKey != expectedScope {
		return CurrentRefreshStatus{}, fmt.Errorf("%w: refresh identity is inconsistent", ErrInvalidCurrentQuery)
	}
	if schedule.BindingGeneration != expectedGeneration {
		return CurrentRefreshStatus{
			State: CurrentRefreshUnknown, UnknownReason: currentUnknownPointer(CurrentUnknownScheduleMissing),
		}, nil
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

func resetCreditExpiresAtMS(value *int64) int64 {
	if value == nil {
		return 0
	}
	return *value
}

func cloneSourceRefreshTrigger(value *store.SourceRefreshTrigger) *store.SourceRefreshTrigger {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}
