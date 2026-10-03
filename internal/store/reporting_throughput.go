package store

import (
	"gorm.io/gorm"

	reportingv1 "github.com/SisyphusSQ/codex-pulse/api/codexpulse/reporting/v1"
	"github.com/SisyphusSQ/codex-pulse/internal/throughput"
)

func reportingThroughputMeasures(s throughput.Stats) reportingv1.ThroughputMeasures {
	if s.Status == "unavailable" {
		s.OutputTokens, s.ActiveMS = nil, nil
	}
	known := s.Reason != "index_pending" && s.Reason != "inherited_history" && s.Reason != "state_limit" && s.Reason != "numeric_overflow" && s.Reason != "history_filtered"
	return reportingv1.ThroughputMeasures{OutputTokens: s.OutputTokens, ActiveDurationMS: s.ActiveMS, IncludedTurns: s.IncludedTurns, ExcludedTurns: s.ExcludedTurns, OpenTurns: s.OpenTurns, UnattributedEvents: s.UnattributedEvents, CoverageKnown: known, Status: s.Status, Reason: s.Reason, DurationSource: s.DurationSource}
}

func exportReportingThroughput(db *gorm.DB, source ReportingSource, snapshot *reportingv1.SessionSnapshot) error {
	capsule := &reportingv1.ThroughputCapsule{Version: 1, Basis: "closed_turn_lifetime_output", RecentTurns: []reportingv1.ThroughputTurn{}}
	snapshot.Throughput = capsule
	if source.StartAtMS > 0 {
		capsule.Measures = reportingThroughputMeasures(throughput.Stats{Status: "unavailable", Reason: "history_filtered"})
		return nil
	}
	stats, turns, err := loadLightThroughput(db, []string{snapshot.SessionID})
	if err != nil {
		return err
	}
	value, exists := stats[snapshot.SessionID]
	if !exists {
		value = throughput.Stats{Status: "unavailable", Reason: "index_pending"}
	}
	capsule.Measures = reportingThroughputMeasures(value)
	items := turns[snapshot.SessionID]
	capsule.TurnsTotal = int64(len(items))
	for _, turn := range items[:min(len(items), reportingv1.MaxThroughputTurns)] {
		measures := reportingThroughputMeasures(turn.Stats)
		measures.CoverageKnown = true // 该轮次本身存在，哪怕其输出或时间证据不完整。
		capsule.RecentTurns = append(capsule.RecentTurns, reportingv1.ThroughputTurn{Key: reportingv1.Key("throughput-turn-v1", snapshot.Provider, snapshot.SessionID, turn.ID), StartedAtMS: turn.StartedAtMS, EndedAtMS: turn.EndedAtMS, Measures: measures})
	}
	return nil
}
