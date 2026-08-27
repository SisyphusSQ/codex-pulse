package cursorprovider

import (
	"context"
	"fmt"
	"sort"

	quotaquery "github.com/SisyphusSQ/codex-pulse/internal/codex/quota"
	"github.com/SisyphusSQ/codex-pulse/internal/query/runtimeinfo"
	"github.com/SisyphusSQ/codex-pulse/internal/store"
)

const (
	cursorModelsLimitID        = "cursor.models"
	otherModelsLimitID         = "cursor.other_models"
	grokBotLimitID             = "cursor.grok_bot"
	cursorDashboardRuleVersion = "cursor-dashboard-v2"
)

var cursorQuotaLimitNames = map[string]string{
	cursorModelsLimitID: "Cursor Models",
	otherModelsLimitID:  "Other Models",
	grokBotLimitID:      "Grok Bot",
}

var cursorQuotaLimitOrder = []string{cursorModelsLimitID, otherModelsLimitID, grokBotLimitID}

type cursorQuotaSnapshotReader struct {
	snapshot store.QuotaCurrentSnapshot
}

func (reader cursorQuotaSnapshotReader) QuotaCurrentSnapshot(
	context.Context,
	string,
	int64,
) (store.QuotaCurrentSnapshot, error) {
	return reader.snapshot, nil
}

func (service *QueryService) QuotaCurrent(
	ctx context.Context,
	evaluatedAtMS int64,
) (runtimeinfo.QuotaCurrentResponse, error) {
	snapshot, err := service.snapshot(ctx)
	if err != nil {
		return runtimeinfo.QuotaCurrentResponse{}, err
	}
	facts := cursorQuotaSnapshot(snapshot, evaluatedAtMS)
	current := quotaquery.CurrentResponse{
		Version: quotaquery.CurrentContractVersion, AccountScope: store.QuotaAccountScopeDefault,
		EvaluatedAtMS: evaluatedAtMS, Windows: make([]quotaquery.CurrentWindow, 0, len(facts.Windows)),
		Sources: cursorQuotaSources(snapshot, facts.Windows),
	}
	currents := make([]store.QuotaCurrent, 0, len(facts.Windows))
	for _, window := range facts.Windows {
		current.Windows = append(current.Windows, mapCursorCurrentWindow(window, evaluatedAtMS))
		currents = append(currents, window.Current)
	}
	summary, err := quotaquery.CalculateQuotaResetSummary(currents, evaluatedAtMS)
	if err != nil {
		return runtimeinfo.QuotaCurrentResponse{}, err
	}
	current.NextReset = quotaquery.CurrentNextReset{
		AtMS: cloneCursorInt64(summary.NextResetAtMS), RemainingMS: cloneCursorInt64(summary.RemainingMS),
		TrustedWindowCount: summary.TrustedWindowCount,
	}
	if current.NextReset.AtMS == nil {
		reason := quotaquery.CurrentUnknownNoTrustedReset
		current.NextReset.UnknownReason = &reason
	}
	meta := completeMeta(nil)
	if cursorQuotaIsPartial(snapshot) {
		meta = partialMeta(nil)
	}
	return runtimeinfo.QuotaCurrentResponse{
		Meta: meta, Current: current, ProviderContext: contextFor(snapshot),
	}, nil
}

func (service *QueryService) QuotaPace(
	ctx context.Context,
	evaluatedAtMS int64,
) (runtimeinfo.QuotaPaceResponse, error) {
	snapshot, err := service.snapshot(ctx)
	if err != nil {
		return runtimeinfo.QuotaPaceResponse{}, err
	}
	facts := cursorQuotaSnapshot(snapshot, evaluatedAtMS)
	query, err := quotaquery.NewCurrentQueryService(cursorQuotaSnapshotReader{snapshot: facts})
	if err != nil {
		return runtimeinfo.QuotaPaceResponse{}, err
	}
	pace, err := query.Pace(ctx, evaluatedAtMS)
	if err != nil {
		return runtimeinfo.QuotaPaceResponse{}, err
	}
	meta := completeMeta(nil)
	if cursorQuotaIsPartial(snapshot) {
		meta = partialMeta(nil)
	}
	return runtimeinfo.QuotaPaceResponse{
		Meta: meta, Pace: pace, ProviderContext: contextFor(snapshot),
	}, nil
}

func mapCursorCurrentWindow(
	facts store.QuotaCurrentWindowSnapshot,
	evaluatedAtMS int64,
) quotaquery.CurrentWindow {
	current := facts.Current
	name := cursorQuotaLimitNames[current.LimitID]
	window := quotaquery.CurrentWindow{
		WindowKind: current.WindowKind, LimitID: current.LimitID, LimitName: &name,
		WindowMinutes: cloneCursorInt64(current.WindowMinutes), ResetsAtMS: cloneCursorInt64(current.ResetsAtMS),
		WindowGeneration: cloneCursorInt64(current.WindowGeneration),
		SelectedSource:   cloneCursorQuotaSource(current.SelectedSource),
		Freshness:        current.FreshnessState, Conflict: store.QuotaConflictNone,
		ExplanationCode: current.ExplanationCode,
		LastSuccessAtMS: cloneCursorInt64(current.LastSuccessAtMS),
		LastAttemptAtMS: cloneCursorInt64(current.LastAttemptAtMS),
	}
	if current.EffectiveUsedPercent == nil {
		switch current.FreshnessState {
		case store.QuotaCurrentNeverLoaded:
			reason := quotaquery.CurrentUnknownNeverLoaded
			window.UnknownReason = &reason
		case store.QuotaCurrentFresh, store.QuotaCurrentStale:
			reason := quotaquery.CurrentUnknownNotApplicable
			window.UnknownReason = &reason
		}
		return window
	}
	used := *current.EffectiveUsedPercent
	remaining := 100 - used
	window.UsedPercent = &used
	window.RemainingPercent = &remaining
	if current.ResetsAtMS != nil && *current.ResetsAtMS > evaluatedAtMS &&
		(current.FreshnessState == store.QuotaCurrentFresh || current.FreshnessState == store.QuotaCurrentStale) {
		resetRemaining := *current.ResetsAtMS - evaluatedAtMS
		window.ResetRemainingMS = &resetRemaining
	}
	return window
}

func cursorQuotaSnapshot(
	snapshot store.CursorSnapshot,
	evaluatedAtMS int64,
) store.QuotaCurrentSnapshot {
	result := store.QuotaCurrentSnapshot{
		AccountScope: store.QuotaAccountScopeDefault, EvaluatedAtMS: evaluatedAtMS,
		Windows: make([]store.QuotaCurrentWindowSnapshot, 0, len(cursorQuotaLimitOrder)),
	}
	byLimit := make(map[string][]store.CursorDashboardQuotaObservation)
	for _, observation := range snapshot.DashboardQuotaObservations {
		if _, known := cursorQuotaLimitNames[observation.LimitID]; !known ||
			observation.ObservedAtMS > evaluatedAtMS {
			continue
		}
		byLimit[observation.LimitID] = append(byLimit[observation.LimitID], observation)
	}
	for _, limitID := range cursorQuotaLimitOrder {
		result.Windows = append(result.Windows, cursorQuotaWindowSnapshot(
			snapshot, limitID, byLimit[limitID], evaluatedAtMS,
		))
	}
	return result
}

func cursorQuotaWindowSnapshot(
	snapshot store.CursorSnapshot,
	limitID string,
	observations []store.CursorDashboardQuotaObservation,
	evaluatedAtMS int64,
) store.QuotaCurrentWindowSnapshot {
	sort.Slice(observations, func(left, right int) bool {
		if observations[left].ObservedAtMS != observations[right].ObservedAtMS {
			return observations[left].ObservedAtMS < observations[right].ObservedAtMS
		}
		return observations[left].Generation < observations[right].Generation
	})
	sourceKind := cursorQuotaSource(limitID)
	source := cursorSourceByKey(snapshot, cursorQuotaSourceKey(limitID))
	selected := -1
	mapped := make([]store.QuotaObservation, 0, len(observations))
	// GetSandUsageStatus can move period_start forward after an in-cycle
	// allowance reset while leaving the official weekly reset unchanged.
	// Preserve the earliest observed start for that reset boundary so the
	// usage reset stays inside one pace cycle instead of restarting its x-axis.
	canonicalCycleStartByEnd := make(map[int64]int64)
	if limitID == grokBotLimitID {
		for _, observation := range observations {
			start, found := canonicalCycleStartByEnd[observation.CycleEndAtMS]
			if !found || observation.CycleStartAtMS < start {
				canonicalCycleStartByEnd[observation.CycleEndAtMS] = observation.CycleStartAtMS
			}
		}
	}
	for index, observation := range observations {
		if observation.CycleStartAtMS <= evaluatedAtMS && evaluatedAtMS < observation.CycleEndAtMS {
			selected = index
		}
		cycleStartAtMS := observation.CycleStartAtMS
		if canonicalStart, found := canonicalCycleStartByEnd[observation.CycleEndAtMS]; found {
			cycleStartAtMS = canonicalStart
		}
		limit := observation.LimitID
		name := cursorQuotaLimitNames[limit]
		mapped = append(mapped, store.QuotaObservation{
			ObservationID: fmt.Sprintf("cursor-dashboard:%d:%s", observation.Generation, limit),
			AccountScope:  store.QuotaAccountScopeDefault, Source: sourceKind,
			LimitID: &limit, LimitName: &name, WindowKind: cursorQuotaWindowKind(limit),
			UsedPercent:   observation.UsedPercent,
			WindowMinutes: (observation.CycleEndAtMS - cycleStartAtMS) / 60_000,
			ResetsAtMS:    observation.CycleEndAtMS, Validity: store.QuotaValidityAccepted,
			FirstObservedAtMS: observation.ObservedAtMS, LastObservedAtMS: observation.ObservedAtMS,
			SampleCount: 1, FirstSourceGeneration: observation.Generation,
			SourceGeneration: observation.Generation,
		})
	}
	current := store.QuotaCurrent{
		AccountScope: store.QuotaAccountScopeDefault, WindowKind: cursorQuotaWindowKind(limitID),
		LimitID: limitID, ConflictState: store.QuotaConflictNone,
		RuleVersion: cursorDashboardRuleVersion, EvaluatedAtMS: evaluatedAtMS,
	}
	if source != nil {
		current.LastAttemptAtMS = cloneCursorInt64(&source.LastAttemptAtMS)
		current.LastSuccessAtMS = cloneCursorInt64(source.LastSuccessAtMS)
	}
	if limitID == grokBotLimitID && cursorGrokBotNotApplicable(source) {
		current.FreshnessState = store.QuotaCurrentFresh
		current.ExplanationCode = store.QuotaExplanationTrusted
		if source.State == "unavailable" {
			current.FreshnessState = store.QuotaCurrentStale
			current.ExplanationCode = store.QuotaExplanationStale
		}
		return store.QuotaCurrentWindowSnapshot{Current: current}
	}
	if selected >= 0 {
		chosen := observations[selected]
		selectedID := mapped[selected].ObservationID
		minutes := mapped[selected].WindowMinutes
		reset := chosen.CycleEndAtMS
		used := chosen.UsedPercent
		freshness := store.QuotaCurrentFresh
		explanation := store.QuotaExplanationTrusted
		if source != nil && source.State == "unavailable" {
			freshness = store.QuotaCurrentStale
			explanation = store.QuotaExplanationStale
		}
		current.ObservationID = &selectedID
		current.EffectiveUsedPercent = &used
		current.WindowMinutes = &minutes
		current.ResetsAtMS = &reset
		current.WindowGeneration = &reset
		current.SelectedSource = &sourceKind
		current.FreshnessState = freshness
		current.ExplanationCode = explanation
		current.LastSuccessAtMS = &chosen.ObservedAtMS
		if current.LastAttemptAtMS == nil {
			current.LastAttemptAtMS = &chosen.ObservedAtMS
		}
		return store.QuotaCurrentWindowSnapshot{Current: current, Observations: mapped}
	}
	if len(observations) > 0 {
		latest := observations[len(observations)-1]
		current.FreshnessState = store.QuotaCurrentExpiredUnknown
		current.ExplanationCode = store.QuotaExplanationExpired
		current.LastSuccessAtMS = &latest.ObservedAtMS
		if current.LastAttemptAtMS == nil {
			current.LastAttemptAtMS = &latest.ObservedAtMS
		}
		return store.QuotaCurrentWindowSnapshot{Current: current, Observations: mapped}
	}
	if limitID == grokBotLimitID && source != nil && source.LastSuccessAtMS != nil {
		current.FreshnessState = store.QuotaCurrentFresh
		current.ExplanationCode = store.QuotaExplanationTrusted
		if source.State == "unavailable" {
			current.FreshnessState = store.QuotaCurrentStale
			current.ExplanationCode = store.QuotaExplanationStale
		}
		return store.QuotaCurrentWindowSnapshot{Current: current, Observations: mapped}
	}
	current.FreshnessState = store.QuotaCurrentNeverLoaded
	current.ExplanationCode = store.QuotaExplanationUnavailable
	return store.QuotaCurrentWindowSnapshot{Current: current, Observations: mapped}
}

func cursorQuotaSources(
	snapshot store.CursorSnapshot,
	windows []store.QuotaCurrentWindowSnapshot,
) []quotaquery.CurrentSource {
	return []quotaquery.CurrentSource{
		cursorQuotaSourceStatus(snapshot, SourceDashboard, quotaquery.CurrentSourceCursorDashboard, windows, store.QuotaSourceCursorDashboard),
		cursorQuotaSourceStatus(snapshot, SourceDashboardGrokBot, quotaquery.CurrentSourceCursorDashboardGrokBot, windows, store.QuotaSourceCursorDashboardGrokBot),
	}
}

func cursorQuotaSourceStatus(
	snapshot store.CursorSnapshot,
	sourceKey string,
	kind quotaquery.CurrentSourceKind,
	windows []store.QuotaCurrentWindowSnapshot,
	selected store.QuotaSource,
) quotaquery.CurrentSource {
	result := quotaquery.CurrentSource{Source: kind, Freshness: store.SourceFreshnessUnknown}
	source := cursorSourceByKey(snapshot, sourceKey)
	if source != nil {
		result.LastAttemptAtMS = cloneCursorInt64(&source.LastAttemptAtMS)
		result.LastSuccessAtMS = cloneCursorInt64(source.LastSuccessAtMS)
		result.LastObservedAtMS = cloneCursorInt64(source.LastSuccessAtMS)
		if source.FailureCode != nil {
			code := store.SourceFailureCode(*source.FailureCode)
			result.FailureCode = &code
		}
		switch source.State {
		case "available":
			result.Freshness = store.SourceFreshnessCurrent
		case "unavailable":
			if source.LastSuccessAtMS != nil {
				result.Freshness = store.SourceFreshnessStale
			} else {
				result.Freshness = store.SourceFreshnessUnavailable
			}
		}
	}
	for _, window := range windows {
		if window.Current.EffectiveUsedPercent != nil &&
			window.Current.SelectedSource != nil &&
			*window.Current.SelectedSource == selected {
			result.SelectedWindowCount++
		}
	}
	if sourceKey == SourceDashboardGrokBot && cursorGrokBotNotApplicable(source) {
		reason := quotaquery.CurrentUnknownNotApplicable
		result.UnknownReason = &reason
	}
	if source == nil && sourceKey == SourceDashboardGrokBot {
		reason := quotaquery.CurrentUnknownNeverLoaded
		result.UnknownReason = &reason
	}
	return result
}

func cursorQuotaIsPartial(snapshot store.CursorSnapshot) bool {
	for _, source := range snapshot.Sources {
		if (source.SourceKey == SourceDashboard || source.SourceKey == SourceDashboardGrokBot) &&
			source.State == "unavailable" {
			return true
		}
	}
	return false
}

func cursorQuotaWindowKind(limitID string) store.QuotaWindowKind {
	switch limitID {
	case cursorModelsLimitID:
		return store.QuotaWindowPrimary
	case otherModelsLimitID:
		return store.QuotaWindowSecondary
	default:
		return store.QuotaWindowAdditionalGrokBot
	}
}

func cursorQuotaSource(limitID string) store.QuotaSource {
	if limitID == grokBotLimitID {
		return store.QuotaSourceCursorDashboardGrokBot
	}
	return store.QuotaSourceCursorDashboard
}

func cursorQuotaSourceKey(limitID string) string {
	if limitID == grokBotLimitID {
		return SourceDashboardGrokBot
	}
	return SourceDashboard
}

func cursorSourceByKey(snapshot store.CursorSnapshot, key string) *store.CursorSourceStatus {
	for index := range snapshot.Sources {
		if snapshot.Sources[index].SourceKey == key {
			return &snapshot.Sources[index]
		}
	}
	return nil
}

func cursorGrokBotNotApplicable(source *store.CursorSourceStatus) bool {
	return source != nil && source.LastSuccessAtMS != nil && source.RowCount == 0
}

func cloneCursorInt64(value *int64) *int64 {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}

func cloneCursorQuotaSource(value *store.QuotaSource) *store.QuotaSource {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}
