package reportingv1

import (
	"encoding/hex"
	"math/big"
	"slices"
)

const MaxThroughputTurns = 50

// ThroughputMeasures 是本机完整生命周期计算得到的安全统计证据；不传浮点平均。
type ThroughputMeasures struct {
	OutputTokens       *int64 `json:"output_tokens,string"`
	ActiveDurationMS   *int64 `json:"active_duration_ms,string"`
	IncludedTurns      int64  `json:"included_turns"`
	ExcludedTurns      int64  `json:"excluded_turns"`
	OpenTurns          int64  `json:"open_turns"`
	UnattributedEvents int64  `json:"unattributed_events"`
	CoverageKnown      bool   `json:"coverage_known"`
	Status             string `json:"status"`
	Reason             string `json:"reason"`
	DurationSource     string `json:"duration_source"`
}

type ThroughputTurn struct {
	Key         string             `json:"key"`
	StartedAtMS *int64             `json:"started_at_ms"`
	EndedAtMS   *int64             `json:"ended_at_ms"`
	Measures    ThroughputMeasures `json:"measures"`
}

// ThroughputCapsule 的整体指标独立于 RecentTurns 截断；不含 raw Turn ID/offset/generation。
type ThroughputCapsule struct {
	Version     int                `json:"version"`
	Basis       string             `json:"basis"`
	Measures    ThroughputMeasures `json:"measures"`
	RecentTurns []ThroughputTurn   `json:"recent_turns"`
	TurnsTotal  int64              `json:"turns_total"`
}

func validThroughputMeasures(m ThroughputMeasures) bool {
	if !optionalTimestamp(m.OutputTokens) || !optionalTimestamp(m.ActiveDurationMS) ||
		m.IncludedTurns < 0 || m.ExcludedTurns < 0 || m.OpenTurns < 0 || m.UnattributedEvents < 0 ||
		m.IncludedTurns > 50000 || m.ExcludedTurns > 50000 || m.OpenTurns > 50000 || m.UnattributedEvents > MaxTimestampMS ||
		m.IncludedTurns+m.ExcludedTurns+m.OpenTurns > 50000 ||
		!slices.Contains([]string{"", "duration_ms", "log_timestamp", "source_seconds", "mixed"}, m.DurationSource) {
		return false
	}
	if m.Status == "complete" || m.Status == "partial" {
		if !m.CoverageKnown || m.OutputTokens == nil || m.ActiveDurationMS == nil || *m.ActiveDurationMS <= 0 || m.IncludedTurns <= 0 || m.DurationSource == "" {
			return false
		}
		if m.Status == "complete" {
			return m.Reason == "" && m.ExcludedTurns+m.OpenTurns+m.UnattributedEvents == 0
		}
		return m.Reason == "index_incomplete" || (m.Reason == "incomplete_coverage" && m.ExcludedTurns+m.OpenTurns+m.UnattributedEvents > 0)
	}
	return m.Status == "unavailable" && m.OutputTokens == nil && m.ActiveDurationMS == nil && slices.Contains([]string{"no_closed_turns", "missing_facts", "open_turn", "inherited_history", "state_limit", "index_pending", "numeric_overflow", "history_filtered"}, m.Reason)
}

func validThroughput(snapshot SessionSnapshot) bool {
	c := snapshot.Throughput
	if c == nil {
		return true
	}
	if snapshot.Provider != "codex" || snapshot.SourceKind != "light_index" || snapshot.Deleted || c.Version != 1 || c.Basis != "closed_turn_lifetime_output" || !validThroughputMeasures(c.Measures) || len(c.RecentTurns) > MaxThroughputTurns || c.TurnsTotal < int64(len(c.RecentTurns)) || c.TurnsTotal > 50000 {
		return false
	}
	if c.Measures.CoverageKnown && c.TurnsTotal != c.Measures.IncludedTurns+c.Measures.ExcludedTurns+c.Measures.OpenTurns {
		return false
	}
	if snapshot.HistoryStartAtMS > 0 && c.Measures.Reason != "history_filtered" {
		return false
	}
	knownOutput := new(big.Int)
	for _, contribution := range snapshot.Contributions {
		if contribution.OutputTokens != nil {
			knownOutput.Add(knownOutput, big.NewInt(*contribution.OutputTokens))
		}
	}
	if c.Measures.OutputTokens != nil && big.NewInt(*c.Measures.OutputTokens).Cmp(knownOutput) > 0 {
		return false
	}
	keys := make(map[string]bool)
	for _, turn := range c.RecentTurns {
		if len(turn.Key) != 64 || keys[turn.Key] || !optionalTimestamp(turn.StartedAtMS) || !optionalTimestamp(turn.EndedAtMS) || !validThroughputMeasures(turn.Measures) {
			return false
		}
		if _, err := hex.DecodeString(turn.Key); err != nil {
			return false
		}
		keys[turn.Key] = true
		m := turn.Measures
		if m.IncludedTurns+m.ExcludedTurns+m.OpenTurns != 1 || m.UnattributedEvents != 0 || !m.CoverageKnown {
			return false
		}
		if m.Status == "complete" {
			if m.IncludedTurns != 1 || turn.StartedAtMS == nil || turn.EndedAtMS == nil || *turn.EndedAtMS <= *turn.StartedAtMS || *m.ActiveDurationMS != *turn.EndedAtMS-*turn.StartedAtMS || c.Measures.OutputTokens == nil || *m.OutputTokens > *c.Measures.OutputTokens {
				return false
			}
		} else if m.Status != "unavailable" {
			return false
		}
	}
	return true
}
