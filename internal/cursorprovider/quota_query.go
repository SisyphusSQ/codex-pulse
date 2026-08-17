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
	cursorModelsLimitID = "cursor.models"
	otherModelsLimitID  = "cursor.other_models"
)

var cursorQuotaLimitNames = map[string]string{
	cursorModelsLimitID: "Cursor Models",
	otherModelsLimitID:  "Other Models",
}

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
	snapshot, stale, err := service.quotaSnapshot(ctx)
	if err != nil {
		return runtimeinfo.QuotaCurrentResponse{}, err
	}
	facts := cursorQuotaSnapshot(snapshot, evaluatedAtMS, stale)
	sourceFreshness := store.SourceFreshnessCurrent
	meta := completeMeta(nil)
	if stale {
		sourceFreshness = store.SourceFreshnessStale
		meta = partialMeta(nil)
	}
	current := quotaquery.CurrentResponse{
		Version: quotaquery.CurrentContractVersion, AccountScope: store.QuotaAccountScopeDefault,
		EvaluatedAtMS: evaluatedAtMS, Windows: make([]quotaquery.CurrentWindow, 0, len(facts.Windows)),
		Sources: []quotaquery.CurrentSource{{
			Source:    quotaquery.CurrentSourceCursorDashboard,
			Freshness: sourceFreshness,
		}},
	}
	for _, window := range facts.Windows {
		used := *window.Current.EffectiveUsedPercent
		remaining := 100 - used
		resetRemaining := *window.Current.ResetsAtMS - evaluatedAtMS
		name := cursorQuotaLimitNames[window.Current.LimitID]
		current.Windows = append(current.Windows, quotaquery.CurrentWindow{
			WindowKind: window.Current.WindowKind, LimitID: window.Current.LimitID, LimitName: &name,
			UsedPercent: &used, RemainingPercent: &remaining,
			WindowMinutes: window.Current.WindowMinutes, ResetsAtMS: window.Current.ResetsAtMS,
			ResetRemainingMS: &resetRemaining, WindowGeneration: window.Current.WindowGeneration,
			SelectedSource: window.Current.SelectedSource, Freshness: window.Current.FreshnessState,
			Conflict: store.QuotaConflictNone, ExplanationCode: store.QuotaExplanationTrusted,
			LastSuccessAtMS: window.Current.LastSuccessAtMS,
		})
	}
	if len(current.Windows) > 0 {
		reset := *current.Windows[0].ResetsAtMS
		remaining := reset - evaluatedAtMS
		current.NextReset = quotaquery.CurrentNextReset{
			AtMS: &reset, RemainingMS: &remaining, TrustedWindowCount: int64(len(current.Windows)),
		}
	}
	return runtimeinfo.QuotaCurrentResponse{
		Meta: meta, Current: current, ProviderContext: contextFor(snapshot),
	}, nil
}

func (service *QueryService) QuotaPace(
	ctx context.Context,
	evaluatedAtMS int64,
) (runtimeinfo.QuotaPaceResponse, error) {
	snapshot, stale, err := service.quotaSnapshot(ctx)
	if err != nil {
		return runtimeinfo.QuotaPaceResponse{}, err
	}
	facts := cursorQuotaSnapshot(snapshot, evaluatedAtMS, stale)
	query, err := quotaquery.NewCurrentQueryService(cursorQuotaSnapshotReader{snapshot: facts})
	if err != nil {
		return runtimeinfo.QuotaPaceResponse{}, err
	}
	pace, err := query.Pace(ctx, evaluatedAtMS)
	if err != nil {
		return runtimeinfo.QuotaPaceResponse{}, err
	}
	meta := completeMeta(nil)
	if stale {
		meta = partialMeta(nil)
	}
	return runtimeinfo.QuotaPaceResponse{
		Meta: meta, Pace: pace, ProviderContext: contextFor(snapshot),
	}, nil
}

func (service *QueryService) quotaSnapshot(
	ctx context.Context,
) (store.CursorSnapshot, bool, error) {
	snapshot, err := service.snapshot(ctx)
	if err != nil {
		return store.CursorSnapshot{}, false, err
	}
	return snapshot, false, nil
}

func cursorQuotaSnapshot(
	snapshot store.CursorSnapshot,
	evaluatedAtMS int64,
	stale bool,
) store.QuotaCurrentSnapshot {
	result := store.QuotaCurrentSnapshot{
		AccountScope: store.QuotaAccountScopeDefault, EvaluatedAtMS: evaluatedAtMS,
		Windows: []store.QuotaCurrentWindowSnapshot{},
	}
	byLimit := make(map[string][]store.CursorDashboardQuotaObservation)
	for _, observation := range snapshot.DashboardQuotaObservations {
		if _, known := cursorQuotaLimitNames[observation.LimitID]; !known ||
			observation.ObservedAtMS > evaluatedAtMS {
			continue
		}
		byLimit[observation.LimitID] = append(byLimit[observation.LimitID], observation)
	}
	for _, limitID := range []string{cursorModelsLimitID, otherModelsLimitID} {
		observations := byLimit[limitID]
		sort.Slice(observations, func(left, right int) bool {
			if observations[left].ObservedAtMS != observations[right].ObservedAtMS {
				return observations[left].ObservedAtMS < observations[right].ObservedAtMS
			}
			return observations[left].Generation < observations[right].Generation
		})
		selected := -1
		mapped := make([]store.QuotaObservation, 0, len(observations))
		for index, observation := range observations {
			if observation.CycleStartAtMS <= evaluatedAtMS && evaluatedAtMS < observation.CycleEndAtMS {
				selected = index
			}
			limit := observation.LimitID
			name := cursorQuotaLimitNames[limit]
			mapped = append(mapped, store.QuotaObservation{
				ObservationID: fmt.Sprintf("cursor-dashboard:%d:%s", observation.Generation, limit),
				AccountScope:  store.QuotaAccountScopeDefault, Source: store.QuotaSourceCursorDashboard,
				LimitID: &limit, LimitName: &name, WindowKind: cursorQuotaWindowKind(limit),
				UsedPercent:   observation.UsedPercent,
				WindowMinutes: (observation.CycleEndAtMS - observation.CycleStartAtMS) / 60_000,
				ResetsAtMS:    observation.CycleEndAtMS, Validity: store.QuotaValidityAccepted,
				FirstObservedAtMS: observation.ObservedAtMS, LastObservedAtMS: observation.ObservedAtMS,
				SampleCount: 1, FirstSourceGeneration: observation.Generation,
				SourceGeneration: observation.Generation,
			})
		}
		if selected < 0 {
			continue
		}
		chosen := observations[selected]
		selectedID := mapped[selected].ObservationID
		minutes := mapped[selected].WindowMinutes
		reset := chosen.CycleEndAtMS
		used := chosen.UsedPercent
		source := store.QuotaSourceCursorDashboard
		freshness := store.QuotaCurrentFresh
		if stale {
			freshness = store.QuotaCurrentStale
		}
		result.Windows = append(result.Windows, store.QuotaCurrentWindowSnapshot{
			Current: store.QuotaCurrent{
				AccountScope: store.QuotaAccountScopeDefault, WindowKind: cursorQuotaWindowKind(limitID),
				LimitID: limitID, ObservationID: &selectedID, EffectiveUsedPercent: &used,
				WindowMinutes: &minutes, ResetsAtMS: &reset, WindowGeneration: &reset,
				SelectedSource: &source, FreshnessState: freshness,
				ConflictState: store.QuotaConflictNone, LastSuccessAtMS: &chosen.ObservedAtMS,
				LastAttemptAtMS: &chosen.ObservedAtMS, RuleVersion: "cursor-dashboard-v1",
				ExplanationCode: store.QuotaExplanationTrusted, EvaluatedAtMS: evaluatedAtMS,
			},
			Observations: mapped, Evidence: []store.QuotaArbitrationEvidence{},
		})
	}
	return result
}

func cursorQuotaWindowKind(limitID string) store.QuotaWindowKind {
	if limitID == cursorModelsLimitID {
		return store.QuotaWindowPrimary
	}
	return store.QuotaWindowSecondary
}
