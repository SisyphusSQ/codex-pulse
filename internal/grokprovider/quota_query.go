package grokprovider

import (
	"context"
	"fmt"

	quotaquery "github.com/SisyphusSQ/codex-pulse/internal/codex/quota"
	"github.com/SisyphusSQ/codex-pulse/internal/query/runtimeinfo"
	"github.com/SisyphusSQ/codex-pulse/internal/store"
)

type grokQuotaSnapshotReader struct {
	snapshot store.QuotaCurrentSnapshot
}

func (reader grokQuotaSnapshotReader) QuotaCurrentSnapshot(
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
	snapshot, stale, err := service.quotaSnapshot(ctx, evaluatedAtMS)
	if err != nil {
		return runtimeinfo.QuotaCurrentResponse{}, err
	}
	facts := grokQuotaSnapshot(snapshot, evaluatedAtMS, stale)
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
			Source:    quotaquery.CurrentSourceGrokBilling,
			Freshness: sourceFreshness,
		}},
	}
	for _, window := range facts.Windows {
		used := *window.Current.EffectiveUsedPercent
		remaining := 100 - used
		resetRemaining := *window.Current.ResetsAtMS - evaluatedAtMS
		name := grokQuotaLimitName(window.Current.LimitID, snapshot)
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
	snapshot, stale, err := service.quotaSnapshot(ctx, evaluatedAtMS)
	if err != nil {
		return runtimeinfo.QuotaPaceResponse{}, err
	}
	facts := grokQuotaSnapshot(snapshot, evaluatedAtMS, stale)
	query, err := quotaquery.NewCurrentQueryService(grokQuotaSnapshotReader{snapshot: facts})
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

func (service *QueryService) quotaSnapshot(ctx context.Context, evaluatedAtMS int64) (store.GrokSnapshot, bool, error) {
	snapshot, err := service.snapshot(ctx)
	if err != nil {
		return store.GrokSnapshot{}, false, err
	}
	stale := snapshot.BillingStale
	if snapshot.Billing != nil && evaluatedAtMS >= snapshot.Billing.PeriodEndMS {
		stale = true
		if evaluatedAtMS >= snapshot.Billing.PeriodEndMS {
			snapshot.Billing = nil
		}
	}
	return snapshot, stale, nil
}

func grokQuotaSnapshot(snapshot store.GrokSnapshot, evaluatedAtMS int64, stale bool) store.QuotaCurrentSnapshot {
	result := store.QuotaCurrentSnapshot{
		AccountScope: store.QuotaAccountScopeDefault, EvaluatedAtMS: evaluatedAtMS,
		Windows: []store.QuotaCurrentWindowSnapshot{},
	}
	if snapshot.Billing == nil {
		return result
	}
	billing := snapshot.Billing
	if billing.PeriodStartMS > evaluatedAtMS || evaluatedAtMS >= billing.PeriodEndMS {
		return result
	}
	limitID := grokCreditsLimitID
	name := grokQuotaLimitName(limitID, snapshot)
	minutes := (billing.PeriodEndMS - billing.PeriodStartMS) / 60_000
	reset := billing.PeriodEndMS
	used := billing.UsedPercent
	result.Windows = append(result.Windows, grokQuotaWindow(
		billing, limitID, name, used, minutes, reset, evaluatedAtMS, stale,
	))
	if percent, ok := onDemandUsedPercent(BillingCredits{
		OnDemandUsed: billing.OnDemandUsed, OnDemandCap: billing.OnDemandCap,
	}); ok {
		onDemandName := grokQuotaLimitName(grokOnDemandLimitID, snapshot)
		result.Windows = append(result.Windows, grokQuotaWindow(
			billing, grokOnDemandLimitID, onDemandName, percent, minutes, reset, evaluatedAtMS, stale,
		))
	}
	return result
}

func grokQuotaWindow(
	billing *store.GrokBillingSnapshot,
	limitID, name string,
	used float64,
	minutes, reset, evaluatedAtMS int64,
	stale bool,
) store.QuotaCurrentWindowSnapshot {
	source := store.QuotaSourceGrokBilling
	freshness := store.QuotaCurrentFresh
	if stale {
		freshness = store.QuotaCurrentStale
	}
	observationID := fmt.Sprintf("grok-billing:%d:%s", billing.Generation, limitID)
	usedCopy := used
	return store.QuotaCurrentWindowSnapshot{
		Current: store.QuotaCurrent{
			AccountScope: store.QuotaAccountScopeDefault, WindowKind: grokQuotaWindowKind(billing.PeriodType),
			LimitID: limitID, ObservationID: &observationID, EffectiveUsedPercent: &usedCopy,
			WindowMinutes: &minutes, ResetsAtMS: &reset, WindowGeneration: &reset,
			SelectedSource: &source, FreshnessState: freshness,
			ConflictState: store.QuotaConflictNone, LastSuccessAtMS: &billing.CollectedAtMS,
			LastAttemptAtMS: &billing.CollectedAtMS, RuleVersion: "grok-billing-v1",
			ExplanationCode: store.QuotaExplanationTrusted, EvaluatedAtMS: evaluatedAtMS,
		},
		Observations: []store.QuotaObservation{{
			ObservationID: observationID, AccountScope: store.QuotaAccountScopeDefault,
			Source: store.QuotaSourceGrokBilling, LimitID: &limitID, LimitName: &name,
			WindowKind: grokQuotaWindowKind(billing.PeriodType), UsedPercent: used,
			WindowMinutes: minutes, ResetsAtMS: reset, Validity: store.QuotaValidityAccepted,
			FirstObservedAtMS: billing.CollectedAtMS, LastObservedAtMS: billing.CollectedAtMS,
			SampleCount: 1, FirstSourceGeneration: billing.Generation, SourceGeneration: billing.Generation,
		}},
		Evidence: []store.QuotaArbitrationEvidence{},
	}
}

func grokQuotaWindowKind(string) store.QuotaWindowKind {
	return store.QuotaWindowPrimary
}

func grokQuotaLimitName(limitID string, snapshot store.GrokSnapshot) string {
	if limitID == grokOnDemandLimitID {
		return "Grok On-Demand"
	}
	if snapshot.Billing != nil && snapshot.Billing.PeriodType == "monthly" {
		return "Grok 月额度"
	}
	if snapshot.Billing != nil && snapshot.Billing.PeriodType == "weekly" {
		return "Grok 周额度"
	}
	if limitID == grokCreditsLimitID {
		return "Grok Credits"
	}
	return limitID
}
