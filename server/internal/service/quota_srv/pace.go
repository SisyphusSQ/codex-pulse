package quota_srv

import (
	"context"
	"strconv"

	reportingv1 "github.com/SisyphusSQ/codex-pulse/api/codexpulse/reporting/v1"
	quotaengine "github.com/SisyphusSQ/codex-pulse/internal/codex/quota"
	"github.com/SisyphusSQ/codex-pulse/internal/store"
	access_dto "github.com/SisyphusSQ/codex-pulse/server/internal/models/dto/access_dto"
	quota_dto "github.com/SisyphusSQ/codex-pulse/server/internal/models/dto/quota_dto"
	quota_vo "github.com/SisyphusSQ/codex-pulse/server/internal/models/vo/quota_vo"
)

func (s *Quota) Pace(ctx context.Context, principal access_dto.Principal, q quota_dto.Query) (out quota_vo.PaceResponse, err error) {
	q.View = ""
	current, err := s.Current(ctx, principal, q)
	if err != nil {
		return
	}
	out = quota_vo.PaceResponse{EvaluatedAtMS: current.EvaluatedAtMS, RuleVersion: "center-pace-v2/snapshot/" + quotaengine.PaceContractVersion, Accounts: current.Accounts, Windows: []quota_vo.PaceWindow{}, Coverage: current.Coverage}
	for _, window := range current.Windows {
		pace, buildErr := paceWindow(window, current.EvaluatedAtMS)
		if buildErr != nil {
			return quota_vo.PaceResponse{}, buildErr
		}
		out.Windows = append(out.Windows, pace)
	}
	return
}
func paceWindow(window quota_vo.Window, now int64) (quota_vo.PaceWindow, error) {
	out := quota_vo.PaceWindow{Key: window.Key, Provider: window.Provider, AccountKey: window.AccountKey, IdentityState: window.IdentityState, LimitID: window.LimitID, WindowKind: window.WindowKind, WindowMinutes: window.WindowMinutes, Current: window.Current, CurrentPoints: []quota_vo.PacePoint{}, HistoricalCycles: []quota_vo.PaceCycle{}, HistoryBand: []quota_vo.HistoryBandPoint{}, Coverage: window.Coverage, Forecast: quota_vo.Forecast{State: "unavailable", Method: "none"}}
	if window.IdentityState != "confirmed" {
		out.UnknownReason = new("binding_unavailable")
		out.Forecast.UnknownReason = out.UnknownReason
		return out, nil
	}
	freshness := store.QuotaCurrentFreshness(window.Current.Freshness)
	if window.Current.ObservedAtMS != nil && window.Current.UsedPercent != nil {
		out.SnapshotAtMS = window.Current.ObservedAtMS
		now = *out.SnapshotAtMS
		// 按最后有效观测的时刻评估历史快照，保留响应中的当前 freshness。
		if freshness == store.QuotaCurrentStale || freshness == store.QuotaCurrentExpiredUnknown {
			freshness = store.QuotaCurrentFresh
		}
	}
	current := store.QuotaCurrent{AccountScope: window.Key, WindowKind: store.QuotaWindowKind(window.WindowKind), LimitID: window.LimitID, EffectiveUsedPercent: window.Current.UsedPercent, WindowMinutes: window.WindowMinutes, ResetsAtMS: window.Current.ResetsAtMS, WindowGeneration: window.Current.ResetsAtMS, FreshnessState: freshness, ConflictState: store.QuotaConflictNone, LastSuccessAtMS: window.Current.ObservedAtMS, EvaluatedAtMS: now}
	if window.Current.Source != nil {
		current.SelectedSource = new(quotaSource(*window.Current.Source))
	}
	if window.Current.Conflict {
		current.ConflictState = store.QuotaConflictPresent
	}
	facts := store.QuotaCurrentWindowSnapshot{Current: current, Observations: []store.QuotaObservation{}, AssociatedHistoryObservations: []store.QuotaObservation{}, AssociatedHistoryScope: new("linked:" + window.Key)}
	for _, o := range window.Observations {
		if o.CanonicalResetAtMS == nil || o.UsedPercent == nil || window.WindowMinutes == nil {
			continue
		}
		scope := window.Key
		linked := o.HistoryOrigin == "linked_history" || o.HistoryOrigin == "legacy_unassigned"
		if linked {
			scope = *facts.AssociatedHistoryScope
		}
		observation := store.QuotaObservation{ObservationID: o.ID, AccountScope: scope, Source: quotaSource(o.Source), LimitID: new(window.LimitID), WindowKind: store.QuotaWindowKind(window.WindowKind), UsedPercent: *o.UsedPercent, WindowMinutes: *window.WindowMinutes, ResetsAtMS: *o.CanonicalResetAtMS, Validity: store.QuotaValidityAccepted, FirstObservedAtMS: o.ObservedAtMS, LastObservedAtMS: o.ObservedAtMS, SampleCount: 1}
		if linked {
			facts.AssociatedHistoryObservations = append(facts.AssociatedHistoryObservations, observation)
		} else {
			facts.Observations = append(facts.Observations, observation)
		}
	}
	pace, err := quotaengine.ComputePaceWindow(facts, window.Key, now)
	if err != nil {
		return out, err
	}
	out.ElapsedPercent, out.PaceDeltaPP = pace.ElapsedPercent, pace.PaceDeltaPP
	out.UnknownReason = paceReason(pace.UnknownReason)
	if pace.Forecast.State != "" {
		out.Forecast = quota_vo.Forecast{State: string(pace.Forecast.State), Method: string(pace.Forecast.Method), ExhaustAtMS: pace.Forecast.ExhaustAtMS, LeadBeforeResetMS: pace.Forecast.LeadBeforeResetMS, EvidenceCount: pace.Forecast.EvidenceCount, EvidenceSpanMS: pace.Forecast.EvidenceSpanMS, UnknownReason: paceReason(pace.Forecast.UnknownReason)}
	} else {
		out.Forecast.UnknownReason = out.UnknownReason
	}
	// 纯计算入口只返回真实采样，避免把显示延伸端点当成新事实。
	for _, p := range pace.CurrentPoints {
		out.CurrentPoints = append(out.CurrentPoints, pacePoint(p))
	}
	if pace.PreviousCycle != nil {
		out.PreviousCycle = new(paceCycle(window.Key, *pace.PreviousCycle))
	}
	for _, c := range pace.HistoricalCycles {
		out.HistoricalCycles = append(out.HistoricalCycles, paceCycle(window.Key, c))
	}
	for _, b := range pace.HistoryBand {
		out.HistoryBand = append(out.HistoryBand, quota_vo.HistoryBandPoint{ElapsedPercent: b.ElapsedPercent, MedianRemaining: b.MedianRemaining, MinimumRemaining: b.MinimumRemaining, MaximumRemaining: b.MaximumRemaining, CycleCount: b.CycleCount})
	}
	out.HistoryCycleCount = pace.HistoryCycleCount
	out.PreviousRemainingAtElapsed = pace.PreviousRemainingAtElapsed
	out.HistoryMedianRemainingAtElapsed = pace.HistoryMedianRemainingAtElapsed
	out.CurrentPoints = displayPoints(out.CurrentPoints)
	if out.PreviousCycle != nil {
		out.PreviousCycle.Points = displayPoints(out.PreviousCycle.Points)
	}
	for i := range out.HistoricalCycles {
		out.HistoricalCycles[i].Points = displayPoints(out.HistoricalCycles[i].Points)
	}
	return out, nil
}
func pacePoint(p quotaengine.PacePoint) quota_vo.PacePoint {
	return quota_vo.PacePoint{ObservedAtMS: p.ObservedAtMS, ElapsedPercent: p.ElapsedPercent, UsedPercent: p.UsedPercent, RemainingPercent: p.RemainingPercent, LinkedHistory: p.LinkedHistory}
}
func paceCycle(key string, c quotaengine.PaceCycle) quota_vo.PaceCycle {
	out := quota_vo.PaceCycle{ID: reportingv1.Key("center-cycle", key, strconv.FormatInt(c.ResetsAtMS, 10)), WindowStartAtMS: c.WindowStartAtMS, ResetsAtMS: c.ResetsAtMS, Complete: c.Complete, Points: []quota_vo.PacePoint{}}
	for _, p := range c.Points {
		out.Points = append(out.Points, pacePoint(p))
	}
	return out
}
func paceReason(reason *quotaengine.PaceUnknownReason) *string {
	if reason == nil {
		return nil
	}
	return new(string(*reason))
}
