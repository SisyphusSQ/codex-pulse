package agyprovider

import (
	"context"
	"fmt"
	"sort"

	"github.com/SisyphusSQ/codex-pulse/internal/agentprovider"
	quotaquery "github.com/SisyphusSQ/codex-pulse/internal/codex/quota"
	basequery "github.com/SisyphusSQ/codex-pulse/internal/query"
	"github.com/SisyphusSQ/codex-pulse/internal/query/runtimeinfo"
	"github.com/SisyphusSQ/codex-pulse/internal/store"
)

const agyQuotaRuleVersion = "agy-ls-rpc-v1"

type agyQuotaSnapshotReader struct {
	snapshot store.QuotaCurrentSnapshot
}

func (reader agyQuotaSnapshotReader) QuotaCurrentSnapshot(
	_ context.Context,
	requestedScope string,
	_ int64,
) (store.QuotaCurrentSnapshot, error) {
	snap := reader.snapshot
	if requestedScope != "" && snap.AccountScope != requestedScope {
		snap.AccountScope = requestedScope
		windows := make([]store.QuotaCurrentWindowSnapshot, len(snap.Windows))
		for i, w := range snap.Windows {
			w.Current.AccountScope = requestedScope
			obs := make([]store.QuotaObservation, len(w.Observations))
			for j, o := range w.Observations {
				o.AccountScope = requestedScope
				obs[j] = o
			}
			w.Observations = obs
			windows[i] = w
		}
		snap.Windows = windows
	}
	return snap, nil
}

// QuotaCurrent 返回当前账号所有 AGY 配额池的 5h + 周双窗状态；
// 离线或关闭 agy_online_enabled 时对未过期窗口保留 last-known-good 并标 stale，跨 resets_at_ms 返 unknown。
func (service *QueryService) QuotaCurrent(
	ctx context.Context,
	evaluatedAtMS int64,
) (runtimeinfo.QuotaCurrentResponse, error) {
	snapshot, err := service.snapshot(ctx)
	if err != nil {
		return runtimeinfo.QuotaCurrentResponse{}, err
	}
	facts, sourceInfo, isPartial, err := service.buildQuotaFacts(ctx, snapshot, evaluatedAtMS)
	if err != nil {
		return runtimeinfo.QuotaCurrentResponse{}, err
	}
	current := quotaquery.CurrentResponse{
		Version:       quotaquery.CurrentContractVersion,
		AccountScope:  facts.AccountScope,
		EvaluatedAtMS: evaluatedAtMS,
		Windows:       make([]quotaquery.CurrentWindow, 0, len(facts.Windows)),
		Sources:       []quotaquery.CurrentSource{sourceInfo},
	}
	currents := make([]store.QuotaCurrent, 0, len(facts.Windows))
	for _, w := range facts.Windows {
		current.Windows = append(current.Windows, mapAGYCurrentWindow(w, evaluatedAtMS))
		currents = append(currents, w.Current)
	}
	summary, err := quotaquery.CalculateQuotaResetSummary(currents, evaluatedAtMS)
	if err != nil {
		return runtimeinfo.QuotaCurrentResponse{}, err
	}
	current.NextReset = quotaquery.CurrentNextReset{
		AtMS:               cloneInt64Ptr(summary.NextResetAtMS),
		RemainingMS:        cloneInt64Ptr(summary.RemainingMS),
		TrustedWindowCount: summary.TrustedWindowCount,
	}
	if current.NextReset.AtMS == nil {
		reason := quotaquery.CurrentUnknownNoTrustedReset
		current.NextReset.UnknownReason = &reason
	}
	meta := completeMeta(nil)
	if isPartial {
		meta = partialMeta(nil)
	}
	return runtimeinfo.QuotaCurrentResponse{
		ProviderContext: contextForQuota(snapshot, len(facts.Windows) > 0),
		Meta:            meta,
		Current:         current,
	}, nil
}

// QuotaPace 基于有界持久化的 agy_quota_observations 返回各配额池双窗真实消耗节奏。
func (service *QueryService) QuotaPace(
	ctx context.Context,
	evaluatedAtMS int64,
) (runtimeinfo.QuotaPaceResponse, error) {
	snapshot, err := service.snapshot(ctx)
	if err != nil {
		return runtimeinfo.QuotaPaceResponse{}, err
	}
	facts, _, isPartial, err := service.buildQuotaFacts(ctx, snapshot, evaluatedAtMS)
	if err != nil {
		return runtimeinfo.QuotaPaceResponse{}, err
	}
	query, err := quotaquery.NewCurrentQueryService(agyQuotaSnapshotReader{snapshot: facts})
	if err != nil {
		return runtimeinfo.QuotaPaceResponse{}, err
	}
	pace, err := query.Pace(ctx, evaluatedAtMS)
	if err != nil {
		return runtimeinfo.QuotaPaceResponse{}, err
	}
	pace.AccountScope = facts.AccountScope
	meta := completeMeta(nil)
	if isPartial {
		meta = partialMeta(nil)
	}
	return runtimeinfo.QuotaPaceResponse{
		ProviderContext: contextForQuota(snapshot, len(facts.Windows) > 0),
		Meta:            meta,
		Pace:            pace,
	}, nil
}

// RefreshQuota 触发一次立即本机 LS Connect RPC 配额刷新（受 provider 控制面与 agy_online_enabled 约束）。
func (service *QueryService) RefreshQuota(ctx context.Context) error {
	if service == nil || service.quotaCollector == nil {
		return basequery.NewUnavailableFailure(nil)
	}
	var res OnlineRefreshClass
	err := service.withOperation(ctx, 0, func(opCtx context.Context) error {
		res = service.quotaCollector.RefreshIfDue(opCtx, true)
		if res.Disabled {
			return basequery.NewUnavailableFailure(nil)
		}
		return res.Err
	})
	if err != nil {
		return err
	}
	service.notifyQuotaRefresh()
	return nil
}

func (service *QueryService) buildQuotaFacts(
	ctx context.Context,
	snapshot store.AGYSnapshot,
	evaluatedAtMS int64,
) (store.QuotaCurrentSnapshot, quotaquery.CurrentSource, bool, error) {
	enabled, activeScope, lastAttemptMS, lastSuccessMS, runtimeStale, failureCode := service.quotaCollector.runtimeStatus()

	var persisted []store.AGYQuotaSnapshot
	if service.quotaStore != nil {
		var err error
		persisted, err = service.quotaStore.AGYQuotaSnapshots(ctx, activeScope)
		if err != nil {
			return store.QuotaCurrentSnapshot{}, quotaquery.CurrentSource{}, false, err
		}
		if len(persisted) == 0 && activeScope != "" {
			// 若当前活动 scope 尚无记录，尝试读取最近任意账号的缓存快照
			persisted, _ = service.quotaStore.AGYQuotaSnapshots(ctx, "")
		}
	}

	// 若未指定 activeScope，选取最新 UpdatedAtMS 对应的单一 account_scope，避免混串多账号
	selectedScope := activeScope
	if selectedScope == "" && len(persisted) > 0 {
		var maxUpdated int64
		for _, item := range persisted {
			if item.UpdatedAtMS >= maxUpdated {
				maxUpdated = item.UpdatedAtMS
				selectedScope = item.AccountScope
			}
		}
	}
	filtered := make([]store.AGYQuotaSnapshot, 0, len(persisted))
	for _, item := range persisted {
		if selectedScope == "" || item.AccountScope == selectedScope {
			filtered = append(filtered, item)
		}
	}
	sort.Slice(filtered, func(i, j int) bool {
		ri, rj := agyPoolOrderRank(filtered[i].LimitID), agyPoolOrderRank(filtered[j].LimitID)
		if ri != rj {
			return ri < rj
		}
		if filtered[i].LimitID != filtered[j].LimitID {
			return filtered[i].LimitID < filtered[j].LimitID
		}
		return agyWindowRank(filtered[i].WindowKind) < agyWindowRank(filtered[j].WindowKind)
	})

	if selectedScope == "" {
		selectedScope = store.QuotaAccountScopeDefault
	}

	// 检查持久化来源行 agy.ls_rpc 作为 lastAttempt/lastSuccess 补充
	for _, src := range snapshot.Sources {
		if src.SourceKey != SourceLSRPC {
			continue
		}
		if lastAttemptMS == nil && src.LastAttemptAtMS > 0 {
			ms := src.LastAttemptAtMS
			lastAttemptMS = &ms
		}
		if lastSuccessMS == nil && src.LastSuccessAtMS != nil {
			lastSuccessMS = cloneInt64Ptr(src.LastSuccessAtMS)
		}
		if src.State == "unavailable" {
			runtimeStale = true
			if failureCode == nil && src.FailureCode != nil {
				fc := *src.FailureCode
				failureCode = &fc
			}
		}
	}

	facts := store.QuotaCurrentSnapshot{
		AccountScope:  selectedScope,
		EvaluatedAtMS: evaluatedAtMS,
		Windows:       make([]store.QuotaCurrentWindowSnapshot, 0, len(filtered)),
	}

	var selectedWindowCount int64
	anyPartial := runtimeStale || !enabled
	sourceKind := store.QuotaSourceAGYLS

	for _, item := range filtered {
		var obsRows []store.AGYQuotaObservation
		if service.quotaStore != nil && selectedScope != store.QuotaAccountScopeDefault {
			obsRows, _ = service.quotaStore.AGYQuotaObservations(ctx, selectedScope, item.LimitID, item.WindowKind)
		}
		mappedObs := make([]store.QuotaObservation, 0, len(obsRows))
		for _, o := range obsRows {
			if o.ObservedAtMS > evaluatedAtMS || o.ResetsAtMS == nil {
				continue
			}
			lid := o.LimitID
			lname := item.LimitName
			mappedObs = append(mappedObs, store.QuotaObservation{
				ObservationID:         fmt.Sprintf("agy-ls:%s:%s:%d", o.LimitID, o.WindowKind, o.ObservedAtMS),
				AccountScope:          selectedScope,
				Source:                sourceKind,
				LimitID:               &lid,
				LimitName:             &lname,
				WindowKind:            store.QuotaWindowKind(o.WindowKind),
				UsedPercent:           float64(o.UsedPercent),
				WindowMinutes:         item.WindowMinutes,
				ResetsAtMS:            *o.ResetsAtMS,
				Validity:              store.QuotaValidityAccepted,
				FirstObservedAtMS:     o.ObservedAtMS,
				LastObservedAtMS:      o.ObservedAtMS,
				SampleCount:           1,
				FirstSourceGeneration: item.Generation,
				SourceGeneration:      item.Generation,
			})
		}

		current := store.QuotaCurrent{
			AccountScope:    selectedScope,
			WindowKind:      store.QuotaWindowKind(item.WindowKind),
			LimitID:         item.LimitID,
			ConflictState:   store.QuotaConflictNone,
			RuleVersion:     agyQuotaRuleVersion,
			EvaluatedAtMS:   evaluatedAtMS,
			LastAttemptAtMS: cloneInt64Ptr(lastAttemptMS),
			LastSuccessAtMS: &item.DataAsOfMS,
		}
		if current.LastAttemptAtMS == nil {
			current.LastAttemptAtMS = &item.DataAsOfMS
		}

		expired := item.ResetsAtMS != nil && evaluatedAtMS >= *item.ResetsAtMS
		if expired {
			current.FreshnessState = store.QuotaCurrentExpiredUnknown
			current.ExplanationCode = store.QuotaExplanationExpired
			anyPartial = true
		} else if item.UsedPercent != nil {
			obsID := fmt.Sprintf("agy-ls:%d:%s:%s", item.Generation, item.LimitID, item.WindowKind)
			used := float64(*item.UsedPercent)
			mins := item.WindowMinutes
			current.ObservationID = &obsID
			current.EffectiveUsedPercent = &used
			current.WindowMinutes = &mins
			current.ResetsAtMS = cloneInt64Ptr(item.ResetsAtMS)
			current.WindowGeneration = cloneInt64Ptr(item.ResetsAtMS)
			current.SelectedSource = &sourceKind
			if runtimeStale || !enabled {
				current.FreshnessState = store.QuotaCurrentStale
				current.ExplanationCode = store.QuotaExplanationStale
				anyPartial = true
			} else {
				current.FreshnessState = store.QuotaCurrentFresh
				current.ExplanationCode = store.QuotaExplanationTrusted
			}
			selectedWindowCount++
		} else {
			current.FreshnessState = store.QuotaCurrentNeverLoaded
			current.ExplanationCode = store.QuotaExplanationUnavailable
			anyPartial = true
		}

		// 若历史观测为空但当前窗口有效，补一条当前观测点让 QuotaPace 有起点
		if len(mappedObs) == 0 && current.EffectiveUsedPercent != nil && item.ResetsAtMS != nil {
			lid := item.LimitID
			lname := item.LimitName
			mappedObs = append(mappedObs, store.QuotaObservation{
				ObservationID:         fmt.Sprintf("agy-ls:%d:%s:%s", item.Generation, item.LimitID, item.WindowKind),
				AccountScope:          selectedScope,
				Source:                sourceKind,
				LimitID:               &lid,
				LimitName:             &lname,
				WindowKind:            store.QuotaWindowKind(item.WindowKind),
				UsedPercent:           *current.EffectiveUsedPercent,
				WindowMinutes:         item.WindowMinutes,
				ResetsAtMS:            *item.ResetsAtMS,
				Validity:              store.QuotaValidityAccepted,
				FirstObservedAtMS:     item.DataAsOfMS,
				LastObservedAtMS:      item.DataAsOfMS,
				SampleCount:           1,
				FirstSourceGeneration: item.Generation,
				SourceGeneration:      item.Generation,
			})
		}

		facts.Windows = append(facts.Windows, store.QuotaCurrentWindowSnapshot{
			Current:      current,
			Observations: mappedObs,
		})
	}

	sourceInfo := quotaquery.CurrentSource{
		Source:              quotaquery.CurrentSourceAGYLS,
		Freshness:           store.SourceFreshnessUnknown,
		LastAttemptAtMS:     cloneInt64Ptr(lastAttemptMS),
		LastSuccessAtMS:     cloneInt64Ptr(lastSuccessMS),
		LastObservedAtMS:    cloneInt64Ptr(lastSuccessMS),
		SelectedWindowCount: selectedWindowCount,
	}
	if failureCode != nil {
		fc := store.SourceFailureCode(*failureCode)
		sourceInfo.FailureCode = &fc
	}
	switch {
	case !enabled && len(filtered) > 0:
		sourceInfo.Freshness = store.SourceFreshnessStale
	case !enabled:
		sourceInfo.Freshness = store.SourceFreshnessUnavailable
		reason := quotaquery.CurrentUnknownSourceUnavailable
		sourceInfo.UnknownReason = &reason
	case runtimeStale && len(filtered) > 0:
		sourceInfo.Freshness = store.SourceFreshnessStale
	case runtimeStale:
		sourceInfo.Freshness = store.SourceFreshnessUnavailable
		reason := quotaquery.CurrentUnknownSourceUnavailable
		sourceInfo.UnknownReason = &reason
	case len(filtered) > 0:
		sourceInfo.Freshness = store.SourceFreshnessCurrent
	default:
		sourceInfo.Freshness = store.SourceFreshnessUnknown
		reason := quotaquery.CurrentUnknownNeverLoaded
		sourceInfo.UnknownReason = &reason
	}

	return facts, sourceInfo, anyPartial && len(filtered) > 0 || (runtimeStale && len(filtered) == 0), nil
}

func mapAGYCurrentWindow(
	facts store.QuotaCurrentWindowSnapshot,
	evaluatedAtMS int64,
) quotaquery.CurrentWindow {
	current := facts.Current
	name := agyLimitDisplayName(current.LimitID, facts.Observations)
	window := quotaquery.CurrentWindow{
		WindowKind:       current.WindowKind,
		LimitID:          current.LimitID,
		LimitName:        &name,
		WindowMinutes:    cloneInt64Ptr(current.WindowMinutes),
		ResetsAtMS:       cloneInt64Ptr(current.ResetsAtMS),
		WindowGeneration: cloneInt64Ptr(current.WindowGeneration),
		SelectedSource:   cloneQuotaSource(current.SelectedSource),
		Freshness:        current.FreshnessState,
		Conflict:         store.QuotaConflictNone,
		ExplanationCode:  current.ExplanationCode,
		LastSuccessAtMS:  cloneInt64Ptr(current.LastSuccessAtMS),
		LastAttemptAtMS:  cloneInt64Ptr(current.LastAttemptAtMS),
	}
	if current.EffectiveUsedPercent == nil {
		switch current.FreshnessState {
		case store.QuotaCurrentNeverLoaded:
			reason := quotaquery.CurrentUnknownNeverLoaded
			window.UnknownReason = &reason
		case store.QuotaCurrentExpiredUnknown:
			reason := quotaquery.CurrentUnknownNoTrustedReset
			window.UnknownReason = &reason
		default:
			reason := quotaquery.CurrentUnknownSourceUnavailable
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

func agyLimitDisplayName(limitID string, observations []store.QuotaObservation) string {
	for _, o := range observations {
		if o.LimitName != nil && *o.LimitName != "" {
			return *o.LimitName
		}
	}
	switch limitID {
	case AGYLimitGemini:
		return "Gemini"
	case AGYLimitArgon:
		return "Gemini 4 Argon"
	case AGYLimitCarbon:
		return "Gemini 4 Carbon"
	case AGYLimitClaudeGPT:
		return "Claude & GPT"
	default:
		return limitID
	}
}

func agyWindowRank(kind string) int {
	if kind == "primary" {
		return 0
	}
	return 1
}

func cloneQuotaSource(v *store.QuotaSource) *store.QuotaSource {
	if v == nil {
		return nil
	}
	cp := *v
	return &cp
}

func contextForQuota(snapshot store.AGYSnapshot, hasQuota bool) agentprovider.Context {
	ctx := contextFor(snapshot)
	if hasQuota && !containsString(ctx.Capabilities, "quota") {
		ctx.Capabilities = append(ctx.Capabilities, "quota")
	}
	return ctx
}
