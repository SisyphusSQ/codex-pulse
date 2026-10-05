package reporting

import (
	"context"
	"strconv"

	reportingv1 "github.com/SisyphusSQ/codex-pulse/api/codexpulse/reporting/v1"
	"github.com/SisyphusSQ/codex-pulse/internal/store"
)

// accountUsageEpoch 由 serialized reporting worker 独占；重启及身份变化不补记旧消耗。
type accountUsageEpoch struct {
	HomeID, Scope, After  string
	Generation, StartAtMS int64
}

// AccountUsage 独立于全历史游标，只导出当前周期内可确认的新消耗。
func (e *Exporter) AccountUsage(ctx context.Context) ([]FactsGroup, error) {
	e.usagePending = nil
	source, err := e.source(ctx, "codex", 0)
	if err != nil {
		e.usageEpoch = accountUsageEpoch{}
		return nil, err
	}
	binding, err := e.repository.CodexAccountBinding(ctx)
	if err != nil {
		return nil, err
	}
	if binding.State != store.CodexAccountBindingConfirmed || binding.AccountScope == nil {
		e.usageEpoch = accountUsageEpoch{}
		return nil, nil
	}
	if err := e.captureIdentities(ctx); err != nil {
		return nil, err
	}
	identities, err := e.state.Identities(ctx)
	if err != nil {
		return nil, err
	}
	var identity AccountIdentity
	for _, i := range identities {
		if i.Provider == "codex" && i.LocalScope == *binding.AccountScope {
			identity = i
			break
		}
	}
	if identity.AccountID == "" {
		return nil, nil
	}
	now := e.now().UnixMilli()
	quota, err := e.repository.ReportingCurrentQuotaPage(ctx, "codex", 0)
	if err != nil {
		return nil, err
	}
	var usage reportingv1.AccountTokenUsage
	for _, q := range quota.Batch.Quotas {
		if q.LocalScope == identity.LocalScope && q.LimitID == "codex" && q.Source == "app_server" && q.Validity == "accepted" && q.WindowMinutes != nil && *q.WindowMinutes == 10080 && q.ResetsAtMS != nil && *q.ResetsAtMS > now && q.ObservedAtMS <= now {
			if usage.CollectedAtMS <= q.ObservedAtMS {
				usage = reportingv1.AccountTokenUsage{Provider: "codex", AccountID: identity.AccountID, LocalScope: e.state.PublicScope("codex", identity.LocalScope), WindowStartAtMS: *q.ResetsAtMS - 10080*60000, ResetsAtMS: *q.ResetsAtMS, CollectedAtMS: q.ObservedAtMS, Facts: []reportingv1.AccountTokenFact{}}
			}
		}
	}
	if usage.ResetsAtMS == 0 {
		return nil, nil
	}
	epoch := e.usageEpoch
	if epoch.HomeID != source.HomeID || epoch.Scope != identity.LocalScope || epoch.Generation != binding.BindingGeneration {
		epoch = accountUsageEpoch{HomeID: source.HomeID, Scope: identity.LocalScope, Generation: binding.BindingGeneration, StartAtMS: max(e.startedAtMS, binding.ObservedAtMS, now)}
		e.usageEpoch = epoch
	}
	cutoff := max(epoch.StartAtMS, usage.WindowStartAtMS)
	source.RecentAfterMS = cutoff
	page, err := e.repository.ReportingPage(ctx, "codex", source, epoch.After)
	if err != nil {
		return nil, err
	}
	groups := []FactsGroup{{Key: reportingv1.Key("account-token-period", source.HomeID, identity.AccountID, strconv.FormatInt(usage.ResetsAtMS, 10)), Batch: reportingv1.Batch{Bindings: []reportingv1.AccountBinding{bindingForIdentity(e.state, identity)}, AccountUsage: []reportingv1.AccountTokenUsage{usage}}}}
	for _, session := range page.Sessions {
		facts := accountTokenFacts(session, cutoff, now, usage.ResetsAtMS)
		for start := 0; start < len(facts); start += 1000 {
			packet := usage
			packet.CollectedAtMS = 0
			packet.Facts = facts[start:min(start+1000, len(facts))]
			// 不让采集时间在无新事实的循环中反复改变幂等正文。
			for _, fact := range packet.Facts {
				packet.CollectedAtMS = max(packet.CollectedAtMS, fact.ObservedAtMS)
			}
			groups = append(groups, FactsGroup{Key: reportingv1.Key("account-token-facts", source.HomeID, identity.AccountID, strconv.FormatInt(usage.ResetsAtMS, 10), session.SessionID, strconv.Itoa(start)), Batch: reportingv1.Batch{Bindings: []reportingv1.AccountBinding{bindingForIdentity(e.state, identity)}, AccountUsage: []reportingv1.AccountTokenUsage{packet}}})
		}
	}
	later, err := e.repository.CodexAccountBinding(ctx)
	if err != nil {
		return nil, err
	}
	latest, err := e.source(ctx, "codex", 0)
	if err != nil || latest.HomeID != source.HomeID || later.State != store.CodexAccountBindingConfirmed || later.AccountScope == nil || *later.AccountScope != identity.LocalScope || later.BindingGeneration != binding.BindingGeneration {
		return nil, store.ErrReportingSource
	}
	if len(page.Sessions) == 0 {
		epoch.After = ""
	} else {
		epoch.After = page.Next
	}
	e.usagePending = &epoch
	return groups, nil
}

// 只有整页进入持久 outbox 后才能推进游标；队列写入失败会重试同一页。
func (e *Exporter) CommitAccountUsage() {
	if e.usagePending != nil {
		e.usageEpoch = *e.usagePending
		e.usagePending = nil
	}
}

func accountTokenFacts(session reportingv1.SessionSnapshot, cutoff, now, reset int64) []reportingv1.AccountTokenFact {
	facts := []reportingv1.AccountTokenFact{}
	ordinals := map[string]int64{}
	previous := int64(-1)
	for _, c := range session.Contributions {
		base := reportingv1.ContributionID("codex", session.SessionID, c, 0)
		ordinal := ordinals[base]
		ordinals[base]++
		if c.ObservedAtMS == nil {
			continue
		}
		at := *c.ObservedAtMS
		// 旧会话第一条增量可能跨越启用/切换边界；无可靠前端点就跳过。
		within := previous >= cutoff || (session.CreatedAtMS != nil && *session.CreatedAtMS >= cutoff)
		previous = at
		if !within || at < cutoff || at > now || at >= reset || c.InputTokens == nil || c.CachedTokens == nil || c.OutputTokens == nil || c.ReasoningTokens == nil || c.TotalTokens == nil {
			continue
		}
		facts = append(facts, reportingv1.AccountTokenFact{SessionID: session.SessionID, ID: c.ID, ObservedAtMS: at, InputTokens: *c.InputTokens, CachedTokens: *c.CachedTokens, OutputTokens: *c.OutputTokens, ReasoningTokens: *c.ReasoningTokens, TotalTokens: *c.TotalTokens, Ordinal: ordinal})
	}
	return facts
}
