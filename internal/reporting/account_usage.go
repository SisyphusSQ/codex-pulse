package reporting

import (
	"cmp"
	"context"
	"slices"
	"strconv"

	reportingv1 "github.com/SisyphusSQ/codex-pulse/api/codexpulse/reporting/v1"
	"github.com/SisyphusSQ/codex-pulse/internal/store"
)

const accountUsageWindowMS = 10080 * 60000

type accountUsageEpoch struct {
	HomeID, Scope         string
	HomeGeneration        uint64
	Generation, StartAtMS int64
}

type accountUsagePlan struct {
	identity AccountIdentity
	period   reportingv1.AccountTokenUsage
	ranges   []store.ReportingAccountRange
}

// ExportAccountUsage sweeps the current windows using durable quota evidence across restarts.
func (e *Exporter) ExportAccountUsage(ctx context.Context, partition string, historyStart int64) (bool, error) {
	source, err := e.source(ctx, "codex", 0)
	if err != nil {
		e.usageEpoch = accountUsageEpoch{}
		return false, err
	}
	binding, err := e.repository.CodexAccountBinding(ctx)
	if err != nil {
		return false, err
	}
	if err := e.captureIdentities(ctx); err != nil {
		return false, err
	}
	identities, err := e.state.Identities(ctx)
	if err != nil {
		return false, err
	}
	now := e.now().UnixMilli()
	lower := max(now-accountUsageWindowMS, source.HomeConfirmedAtMS, historyStart)
	if lower > now {
		return false, nil
	}
	quota, err := e.repository.ReportingCurrentQuotaPage(ctx, "codex", lower)
	if err != nil {
		return false, err
	}
	ranges, err := e.repository.ReportingAccountRanges(ctx, lower, now)
	if err != nil {
		return false, err
	}
	plans, cutoff := e.accountUsagePlans(source, binding, identities, quota.Batch.Quotas, ranges, lower, now)
	if len(plans) == 0 {
		return false, nil
	}
	cursor, err := e.state.cursor(ctx, partition, "codex-account-usage",
		reportingv1.Key(source.HomeID, strconv.FormatUint(source.HomeGeneration, 10), strconv.FormatInt(source.HomeConfirmedAtMS, 10)))
	if err != nil {
		return false, err
	}
	var page store.ReportingPage
	if cutoff <= now {
		source.RecentAfterMS = cutoff
		page, err = e.repository.ReportingPage(ctx, "codex", source, cursor.After)
		if err != nil {
			return false, err
		}
	}
	groups := e.accountUsageGroups(source.HomeID, plans, page.Sessions)
	later, err := e.repository.CodexAccountBinding(ctx)
	if err != nil {
		return false, err
	}
	latest, err := e.source(ctx, "codex", 0)
	if err != nil || latest.HomeID != source.HomeID || latest.HomeGeneration != source.HomeGeneration ||
		latest.HomeConfirmedAtMS != source.HomeConfirmedAtMS || later.State != binding.State || later.BindingGeneration != binding.BindingGeneration {
		return false, store.ErrReportingSource
	}
	if _, err := e.state.EnqueueFactGroups(ctx, partition, groups, -1); err != nil {
		return false, err
	}
	// A crash before saving the cursor repeats a page against the durable outbox checkpoints.
	if cursor.After != page.Next {
		cursor.After = page.Next
		if err := e.state.saveCursor(ctx, cursor); err != nil {
			return false, err
		}
	}
	return cursor.After != "", nil
}

func (e *Exporter) accountUsagePlans(source store.ReportingSource, binding store.CodexAccountBinding, identities []AccountIdentity, quotas []reportingv1.QuotaObservation, ranges []store.ReportingAccountRange, lower, now int64) ([]accountUsagePlan, int64) {
	var activeScope string
	if binding.State == store.CodexAccountBindingConfirmed && binding.AccountScope != nil {
		activeScope = *binding.AccountScope
		epoch := e.usageEpoch
		if epoch.HomeID != source.HomeID || epoch.HomeGeneration != source.HomeGeneration || epoch.Scope != activeScope || epoch.Generation != binding.BindingGeneration {
			e.usageEpoch = accountUsageEpoch{HomeID: source.HomeID, HomeGeneration: source.HomeGeneration, Scope: activeScope, Generation: binding.BindingGeneration, StartAtMS: max(e.startedAtMS, binding.ObservedAtMS, now)}
		}
		live := store.ReportingAccountRange{AccountScope: activeScope, BindingGeneration: binding.BindingGeneration, FromMS: max(lower, e.usageEpoch.StartAtMS), ThroughMS: now}
		at := slices.IndexFunc(ranges, func(r store.ReportingAccountRange) bool {
			return r.AccountScope == activeScope && r.BindingGeneration == binding.BindingGeneration
		})
		if at >= 0 && ranges[at].ThroughMS >= live.FromMS {
			live.FromMS = min(live.FromMS, ranges[at].FromMS)
			ranges[at] = live
		} else {
			ranges = append(ranges, live)
		}
	} else {
		e.usageEpoch = accountUsageEpoch{}
	}
	ranges = disjointAccountRanges(ranges)
	plans := []accountUsagePlan{}
	cutoff := now + 1
	for _, identity := range identities {
		if identity.Provider != "codex" || identity.AccountID == "" {
			continue
		}
		period := accountUsagePeriod(quotas, identity, e.state.PublicScope("codex", identity.LocalScope), now)
		if period.ResetsAtMS == 0 {
			continue
		}
		plan := accountUsagePlan{identity: identity, period: period}
		for _, r := range ranges {
			r.FromMS = max(r.FromMS, period.WindowStartAtMS)
			if r.AccountScope == identity.LocalScope && r.FromMS <= r.ThroughMS {
				plan.ranges = append(plan.ranges, r)
				cutoff = min(cutoff, r.FromMS)
			}
		}
		if len(plan.ranges) != 0 || identity.LocalScope == activeScope {
			plans = append(plans, plan)
		}
	}
	return plans, cutoff
}

func disjointAccountRanges(ranges []store.ReportingAccountRange) []store.ReportingAccountRange {
	slices.SortFunc(ranges, func(a, b store.ReportingAccountRange) int { return cmp.Compare(a.FromMS, b.FromMS) })
	disjoint := make([]store.ReportingAccountRange, 0, len(ranges))
	previousEnd := int64(-1)
	for i, r := range ranges {
		// Overlapping generations cannot prove attribution, for example after a backwards clock jump.
		if r.FromMS > previousEnd && (i+1 == len(ranges) || r.ThroughMS < ranges[i+1].FromMS) {
			disjoint = append(disjoint, r)
		}
		previousEnd = max(previousEnd, r.ThroughMS)
	}
	return disjoint
}

func (e *Exporter) accountUsageGroups(homeID string, plans []accountUsagePlan, sessions []reportingv1.SessionSnapshot) []FactsGroup {
	groups := []FactsGroup{}
	for _, plan := range plans {
		period, identity := plan.period, plan.identity
		reset := strconv.FormatInt(period.ResetsAtMS, 10)
		bindings := []reportingv1.AccountBinding{bindingForIdentity(e.state, identity)}
		groups = append(groups, FactsGroup{Key: reportingv1.Key("account-token-period", homeID, identity.AccountID, reset), Batch: reportingv1.Batch{Bindings: bindings, AccountUsage: []reportingv1.AccountTokenUsage{period}}})
		for _, session := range sessions {
			facts := accountTokenFacts(session, plan.ranges, period.ResetsAtMS)
			for start := 0; start < len(facts); start += 1000 {
				packet := period
				packet.CollectedAtMS = 0
				packet.Facts = facts[start:min(start+1000, len(facts))]
				for _, fact := range packet.Facts {
					packet.CollectedAtMS = max(packet.CollectedAtMS, fact.ObservedAtMS)
				}
				groups = append(groups, FactsGroup{Key: reportingv1.Key("account-token-facts", homeID, identity.AccountID, reset, session.SessionID, strconv.Itoa(start)), Batch: reportingv1.Batch{Bindings: bindings, AccountUsage: []reportingv1.AccountTokenUsage{packet}}})
			}
		}
	}
	return groups
}

func accountUsagePeriod(quotas []reportingv1.QuotaObservation, identity AccountIdentity, publicScope string, now int64) (usage reportingv1.AccountTokenUsage) {
	for _, q := range quotas {
		if q.LocalScope != identity.LocalScope || q.LimitID != "codex" || q.Source != "app_server" || q.Validity != "accepted" || q.WindowMinutes == nil || *q.WindowMinutes != 10080 || q.ResetsAtMS == nil || *q.ResetsAtMS <= now || q.ObservedAtMS > now {
			continue
		}
		start := *q.ResetsAtMS - accountUsageWindowMS
		// 重置观测的时间可能略早于推算的周期起点；等待有效观测，
		// 不改写采集时间，也不让可选账号消耗阻断其他事实上报。
		if q.ObservedAtMS < start {
			continue
		}
		if usage.CollectedAtMS <= q.ObservedAtMS {
			usage = reportingv1.AccountTokenUsage{Provider: "codex", AccountID: identity.AccountID, LocalScope: publicScope, WindowStartAtMS: start, ResetsAtMS: *q.ResetsAtMS, CollectedAtMS: q.ObservedAtMS, Facts: []reportingv1.AccountTokenFact{}}
		}
	}
	return
}

func accountTokenFacts(session reportingv1.SessionSnapshot, ranges []store.ReportingAccountRange, reset int64) []reportingv1.AccountTokenFact {
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
		within := slices.ContainsFunc(ranges, func(r store.ReportingAccountRange) bool {
			return r.FromMS <= at && at <= r.ThroughMS &&
				(previous >= r.FromMS && previous <= at || session.CreatedAtMS != nil && *session.CreatedAtMS >= r.FromMS && *session.CreatedAtMS <= at)
		})
		previous = at
		if !within || at >= reset || c.InputTokens == nil || c.CachedTokens == nil || c.OutputTokens == nil || c.ReasoningTokens == nil || c.TotalTokens == nil {
			continue
		}
		facts = append(facts, reportingv1.AccountTokenFact{SessionID: session.SessionID, ID: c.ID, ObservedAtMS: at, InputTokens: *c.InputTokens, CachedTokens: *c.CachedTokens, OutputTokens: *c.OutputTokens, ReasoningTokens: new(*c.ReasoningTokens), TotalTokens: *c.TotalTokens, Ordinal: ordinal})
	}
	return facts
}

type agyAccountUsagePlan struct {
	identity AccountIdentity
	periods  []reportingv1.AccountTokenUsage
	ranges   []store.ReportingAccountRange
}

// ExportAGYAccountUsage sweeps AGY active quota windows using confirmed account_scope
// observation intervals and exports AccountTokenUsage facts sharing ContributionIDs.
func (e *Exporter) ExportAGYAccountUsage(ctx context.Context, partition string, historyStart int64) (bool, error) {
	source, err := e.source(ctx, "agy", 0)
	if err != nil {
		return false, err
	}
	if err := e.captureIdentities(ctx); err != nil {
		return false, err
	}
	identities, err := e.state.Identities(ctx)
	if err != nil {
		return false, err
	}
	now := e.now().UnixMilli()
	lower := max(now-accountUsageWindowMS, historyStart)
	if lower > now {
		return false, nil
	}
	quota, err := e.repository.ReportingCurrentQuotaPage(ctx, "agy", lower)
	if err != nil {
		return false, err
	}
	ranges, err := e.repository.ReportingAGYAccountRanges(ctx, lower, now)
	if err != nil {
		return false, err
	}
	plans, cutoff := e.agyAccountUsagePlans(identities, quota.Batch.Quotas, ranges, now)
	if len(plans) == 0 {
		return false, nil
	}
	cursor, err := e.state.cursor(ctx, partition, "agy-account-usage", source.HomeID)
	if err != nil {
		return false, err
	}
	var page store.ReportingPage
	if cutoff <= now {
		source.RecentAfterMS = cutoff
		page, err = e.repository.ReportingPage(ctx, "agy", source, cursor.After)
		if err != nil {
			return false, err
		}
	}
	groups := e.agyAccountUsageGroups(source.HomeID, plans, page.Sessions)
	latest, err := e.source(ctx, "agy", 0)
	if err != nil || latest.HomeID != source.HomeID {
		return false, store.ErrReportingSource
	}
	if _, err := e.state.EnqueueFactGroups(ctx, partition, groups, -1); err != nil {
		return false, err
	}
	if cursor.After != page.Next {
		cursor.After = page.Next
		if err := e.state.saveCursor(ctx, cursor); err != nil {
			return false, err
		}
	}
	return cursor.After != "", nil
}

func (e *Exporter) agyAccountUsagePlans(identities []AccountIdentity, quotas []reportingv1.QuotaObservation, ranges []store.ReportingAccountRange, now int64) ([]agyAccountUsagePlan, int64) {
	ranges = disjointAccountRanges(ranges)
	plans := []agyAccountUsagePlan{}
	cutoff := now + 1
	for _, identity := range identities {
		if identity.Provider != "agy" || identity.AccountID == "" {
			continue
		}
		periods := agyAccountUsagePeriods(quotas, identity, e.state.PublicScope("agy", identity.LocalScope), now)
		if len(periods) == 0 {
			continue
		}
		minStart := periods[0].WindowStartAtMS
		for _, p := range periods[1:] {
			minStart = min(minStart, p.WindowStartAtMS)
		}
		plan := agyAccountUsagePlan{identity: identity, periods: periods}
		for _, r := range ranges {
			r.FromMS = max(r.FromMS, minStart)
			if r.AccountScope == identity.LocalScope && r.FromMS <= r.ThroughMS {
				plan.ranges = append(plan.ranges, r)
				cutoff = min(cutoff, r.FromMS)
			}
		}
		plans = append(plans, plan)
	}
	return plans, cutoff
}

func agyAccountUsagePeriods(quotas []reportingv1.QuotaObservation, identity AccountIdentity, publicScope string, now int64) []reportingv1.AccountTokenUsage {
	type spanKey struct{ start, reset int64 }
	bySpan := map[spanKey]reportingv1.AccountTokenUsage{}
	for _, q := range quotas {
		if q.LocalScope != identity.LocalScope || q.Source != "agy_ls_rpc" || q.Validity != "accepted" ||
			q.WindowMinutes == nil || *q.WindowMinutes <= 0 || q.ResetsAtMS == nil || *q.ResetsAtMS <= now || q.ObservedAtMS > now {
			continue
		}
		start := *q.ResetsAtMS - *q.WindowMinutes*60000
		if start < 0 || q.ObservedAtMS < start {
			continue
		}
		key := spanKey{start: start, reset: *q.ResetsAtMS}
		if prev, ok := bySpan[key]; !ok || prev.CollectedAtMS <= q.ObservedAtMS {
			bySpan[key] = reportingv1.AccountTokenUsage{
				Provider:        "agy",
				AccountID:       identity.AccountID,
				LocalScope:      publicScope,
				WindowStartAtMS: start,
				ResetsAtMS:      *q.ResetsAtMS,
				CollectedAtMS:   q.ObservedAtMS,
				Facts:           []reportingv1.AccountTokenFact{},
			}
		}
	}
	periods := make([]reportingv1.AccountTokenUsage, 0, len(bySpan))
	for _, p := range bySpan {
		periods = append(periods, p)
	}
	// Prefer widest window first when assigning facts so each fact is emitted once.
	slices.SortFunc(periods, func(a, b reportingv1.AccountTokenUsage) int {
		if order := cmp.Compare(b.ResetsAtMS-b.WindowStartAtMS, a.ResetsAtMS-a.WindowStartAtMS); order != 0 {
			return order
		}
		if order := cmp.Compare(a.WindowStartAtMS, b.WindowStartAtMS); order != 0 {
			return order
		}
		return cmp.Compare(a.ResetsAtMS, b.ResetsAtMS)
	})
	return periods
}

func (e *Exporter) agyAccountUsageGroups(homeID string, plans []agyAccountUsagePlan, sessions []reportingv1.SessionSnapshot) []FactsGroup {
	groups := []FactsGroup{}
	for _, plan := range plans {
		identity := plan.identity
		bindings := []reportingv1.AccountBinding{bindingForIdentity(e.state, identity)}
		for _, period := range plan.periods {
			startStr := strconv.FormatInt(period.WindowStartAtMS, 10)
			resetStr := strconv.FormatInt(period.ResetsAtMS, 10)
			groups = append(groups, FactsGroup{
				Key:   reportingv1.Key("account-token-period", "agy", homeID, identity.AccountID, startStr, resetStr),
				Batch: reportingv1.Batch{Bindings: bindings, AccountUsage: []reportingv1.AccountTokenUsage{period}},
			})
		}
		for _, session := range sessions {
			perPeriod := agyAccountTokenFacts(session, plan.ranges, plan.periods)
			for idx, period := range plan.periods {
				facts := perPeriod[idx]
				startStr := strconv.FormatInt(period.WindowStartAtMS, 10)
				resetStr := strconv.FormatInt(period.ResetsAtMS, 10)
				for offset := 0; offset < len(facts); offset += 1000 {
					packet := period
					packet.CollectedAtMS = 0
					packet.Facts = facts[offset:min(offset+1000, len(facts))]
					for _, fact := range packet.Facts {
						packet.CollectedAtMS = max(packet.CollectedAtMS, fact.ObservedAtMS)
					}
					groups = append(groups, FactsGroup{
						Key:   reportingv1.Key("account-token-facts", "agy", homeID, identity.AccountID, startStr, resetStr, session.SessionID, strconv.Itoa(offset)),
						Batch: reportingv1.Batch{Bindings: bindings, AccountUsage: []reportingv1.AccountTokenUsage{packet}},
					})
				}
			}
		}
	}
	return groups
}

func agyAccountTokenFacts(session reportingv1.SessionSnapshot, ranges []store.ReportingAccountRange, periods []reportingv1.AccountTokenUsage) [][]reportingv1.AccountTokenFact {
	out := make([][]reportingv1.AccountTokenFact, len(periods))
	ordinals := map[string]int64{}
	for _, c := range session.Contributions {
		base := reportingv1.ContributionID("agy", session.SessionID, c, 0)
		ordinal := ordinals[base]
		ordinals[base]++
		if c.ObservedAtMS == nil || c.InputTokens == nil || c.CachedTokens == nil || c.OutputTokens == nil || c.TotalTokens == nil {
			continue
		}
		at := *c.ObservedAtMS
		within := slices.ContainsFunc(ranges, func(r store.ReportingAccountRange) bool {
			return r.FromMS <= at && at <= r.ThroughMS
		})
		if !within {
			continue
		}
		periodIdx := slices.IndexFunc(periods, func(p reportingv1.AccountTokenUsage) bool {
			return p.WindowStartAtMS <= at && at < p.ResetsAtMS
		})
		if periodIdx < 0 {
			continue
		}
		var reasoning *int64
		if c.ReasoningTokens != nil {
			reasoning = new(*c.ReasoningTokens)
		}
		out[periodIdx] = append(out[periodIdx], reportingv1.AccountTokenFact{
			SessionID:       session.SessionID,
			ID:              c.ID,
			ObservedAtMS:    at,
			InputTokens:     *c.InputTokens,
			CachedTokens:    *c.CachedTokens,
			OutputTokens:    *c.OutputTokens,
			ReasoningTokens: reasoning,
			TotalTokens:     *c.TotalTokens,
			Ordinal:         ordinal,
		})
	}
	return out
}
