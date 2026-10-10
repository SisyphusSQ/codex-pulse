package reporting_srv

import (
	"cmp"
	"reflect"
	"slices"

	reportingv1 "github.com/SisyphusSQ/codex-pulse/api/codexpulse/reporting/v1"
	reporting_dto "github.com/SisyphusSQ/codex-pulse/server/internal/models/dto/reporting_dto"
)

// candidate 仅是本文件的仲裁状态，不参与持久化或序列化。
type candidate struct {
	source       reporting_dto.SourceSnapshot
	inconsistent bool
}

func containsFacts(a, b reportingv1.SessionSnapshot) bool {
	// a 覆盖 b；不根据数值总量取最大，也不拼接不可比较副本。
	facts := make(map[string]bool, len(a.Contributions))
	for _, c := range a.Contributions {
		facts[c.ID] = true
	}
	for _, c := range b.Contributions {
		if !facts[c.ID] {
			return false
		}
	}
	calls := make(map[string]bool, len(a.Invocations))
	for _, i := range a.Invocations {
		calls[i.ID] = true
	}
	for _, i := range b.Invocations {
		if !calls[i.ID] {
			return false
		}
	}
	return true
}
func pricingEvidence(s reportingv1.SessionSnapshot) int {
	known := 0
	for _, c := range s.Contributions {
		if c.CostStatus == "known" && (c.Rates != nil || c.CostMicroUSD != nil) {
			known++
		}
	}
	return known
}
func enrichable(a, b reportingv1.SessionSnapshot) bool {
	// 相同 Token 身份允许 unknown -> 已有历史价格证据，不覆盖两份不同的已知价格。
	facts := make(map[string]reportingv1.Contribution)
	for _, c := range a.Contributions {
		facts[c.ID] = c
	}
	for _, c := range b.Contributions {
		previous, ok := facts[c.ID]
		if !ok {
			continue
		}
		if previous.CostStatus == "known" && c.CostStatus == "known" && !reflect.DeepEqual(previous, c) {
			return false
		}
	}
	return true
}
func mergeCandidates(sources []reporting_dto.SourceSnapshot) (out []candidate) {
	dashboard := make(map[string]int)
	for _, source := range sources {
		if source.Snapshot.Deleted {
			continue
		}
		if source.Snapshot.Provider != "cursor" || source.Snapshot.SourceKind != "cursor_dashboard" {
			out = append(out, candidate{source: source})
			continue
		}
		slot, found := dashboard[source.ClientID]
		if !found {
			dashboard[source.ClientID] = len(out)
			out = append(out, candidate{source: source})
			continue
		}
		current := &out[slot]
		old := current.source.Snapshot
		incoming := source.Snapshot
		contributions := make(map[string]reportingv1.Contribution, len(old.Contributions))
		for _, c := range old.Contributions {
			contributions[c.ID] = c
		}
		calls := make(map[string]reportingv1.Invocation, len(old.Invocations))
		for _, i := range old.Invocations {
			calls[i.ID] = i
		}
		for _, c := range incoming.Contributions {
			if previous, ok := contributions[c.ID]; ok && !reflect.DeepEqual(previous, c) {
				current.inconsistent = true
			} else {
				contributions[c.ID] = c
			}
		}
		for _, i := range incoming.Invocations {
			if previous, ok := calls[i.ID]; ok && !reflect.DeepEqual(previous, i) {
				current.inconsistent = true
			} else {
				calls[i.ID] = i
			}
		}
		if incoming.CollectedAtMS > old.CollectedAtMS {
			current.source = source
		}
		current.source.Snapshot.Complete = old.Complete && incoming.Complete
		current.source.Snapshot.HistoryStartAtMS = min(old.HistoryStartAtMS, incoming.HistoryStartAtMS)
		current.source.Snapshot.Contributions = make([]reportingv1.Contribution, 0, len(contributions))
		for _, c := range contributions {
			current.source.Snapshot.Contributions = append(current.source.Snapshot.Contributions, c)
		}
		current.source.Snapshot.Invocations = make([]reportingv1.Invocation, 0, len(calls))
		for _, i := range calls {
			current.source.Snapshot.Invocations = append(current.source.Snapshot.Invocations, i)
		}
		slices.SortFunc(current.source.Snapshot.Contributions, func(a, b reportingv1.Contribution) int {
			if a.ObservedAtMS == nil && b.ObservedAtMS != nil {
				return 1
			}
			if a.ObservedAtMS != nil && b.ObservedAtMS == nil {
				return -1
			}
			if a.ObservedAtMS != nil && b.ObservedAtMS != nil {
				if v := cmp.Compare(*a.ObservedAtMS, *b.ObservedAtMS); v != 0 {
					return v
				}
			}
			return cmp.Compare(a.ID, b.ID)
		})
		slices.SortFunc(current.source.Snapshot.Invocations, func(a, b reportingv1.Invocation) int {
			if v := cmp.Compare(a.ObservedAtMS, b.ObservedAtMS); v != 0 {
				return v
			}
			return cmp.Compare(a.ID, b.ID)
		})
	}
	// Dashboard 与本地 Cursor 的 Token 口径不同。存在已知 Dashboard 时沿用本机
	// 权威来源选择；各账期属于同一来源历史，不能与本地摘要再次累加。
	hasDashboard := false
	for _, c := range out {
		hasDashboard = hasDashboard || c.source.Snapshot.SourceKind == "cursor_dashboard"
	}
	if hasDashboard {
		out = slices.DeleteFunc(out, func(c candidate) bool { return c.source.Snapshot.SourceKind == "cursor_local" })
	}
	return
}

// authoritativeRevision 区分用量事实的完整性与本地会话元数据的覆盖率。
// Dashboard 仅在完整分页成功后原子提交；旧 Helper 的 Complete 误用了本地
// metadata 覆盖率。兼容已有来源，不改写其 payload 或 revision。
func authoritativeRevision(snapshot reportingv1.SessionSnapshot) bool {
	return snapshot.Complete || (snapshot.Provider == "cursor" && snapshot.SourceKind == "cursor_dashboard")
}

// DecideSnapshot 在接收及本设备来源对账中复用同一事实仲裁，不依赖 HTTP 或存储。
func DecideSnapshot(sources []reporting_dto.SourceSnapshot, previous *reporting_dto.SourceSnapshot) reporting_dto.MergeDecision {
	candidates := mergeCandidates(sources)
	if len(candidates) == 0 {
		return reporting_dto.MergeDecision{Deleted: true}
	}
	selected := candidates[0]
	correctionFence := false
	hasPrevious := false
	if previous != nil {
		for _, c := range candidates {
			if c.source.ID == previous.ID {
				hasPrevious = true
				correctionFence = previous.CorrectionFence || (authoritativeRevision(c.source.Snapshot) && c.source.Snapshot.Revision > previous.Snapshot.Revision && !containsFacts(c.source.Snapshot, previous.Snapshot))
				selected = c
				// 一份部分修订不能抹去此前已经接受的完整证据；完整来源可正式修订
				// 自身历史，即使数量减少，其他设备仍会参与冲突比较。
				if !authoritativeRevision(c.source.Snapshot) && !containsFacts(c.source.Snapshot, previous.Snapshot) {
					selected.source = *previous
					selected.source.Snapshot.Complete = false
					selected.inconsistent = true
				}
				break
			}
		}
	}
	for _, c := range candidates {
		if c.source.ID == selected.source.ID {
			continue
		}
		current, incoming := selected.source.Snapshot, c.source.Snapshot
		more := containsFacts(incoming, current)
		less := containsFacts(current, incoming)
		if more && !less && !correctionFence {
			selected = c
			continue
		}
		if more && less && enrichable(current, incoming) && !correctionFence {
			if pricingEvidence(incoming) > pricingEvidence(current) || (!hasPrevious && incoming.Complete && !current.Complete) {
				selected = c
			}
		}
	}
	conflict := selected.inconsistent
	for _, c := range candidates {
		a, b := selected.source.Snapshot, c.source.Snapshot
		conflict = conflict || c.inconsistent || (correctionFence && !containsFacts(a, b))
		if !containsFacts(a, b) && !containsFacts(b, a) {
			conflict = true
			continue
		}
		if !enrichable(a, b) {
			conflict = true
		}
	}
	return reporting_dto.MergeDecision{Source: selected.source, Conflict: conflict, CorrectionFence: correctionFence}
}
