package statistics_srv

import (
	"context"
	"encoding/json/v2"
	"reflect"
	"strconv"

	reportingv1 "github.com/SisyphusSQ/codex-pulse/api/codexpulse/reporting/v1"
	"github.com/SisyphusSQ/codex-pulse/internal/throughput"
	reporting_do "github.com/SisyphusSQ/codex-pulse/server/internal/models/do/mysql/reporting_do"
	reporting_dto "github.com/SisyphusSQ/codex-pulse/server/internal/models/dto/reporting_dto"
	statistics_dto "github.com/SisyphusSQ/codex-pulse/server/internal/models/dto/statistics_dto"
	statistics_vo "github.com/SisyphusSQ/codex-pulse/server/internal/models/vo/statistics_vo"
	"github.com/SisyphusSQ/codex-pulse/server/internal/service/reporting_srv"
	"github.com/SisyphusSQ/codex-pulse/server/utils"
)

func throughputDecimal(n *int64) *string {
	if n == nil {
		return nil
	}
	return new(strconv.FormatInt(*n, 10))
}
func throughputView(m reportingv1.ThroughputMeasures) statistics_vo.ThroughputView {
	v := statistics_vo.ThroughputView{OutputTokens: throughputDecimal(m.OutputTokens), ActiveDurationMS: throughputDecimal(m.ActiveDurationMS), Status: m.Status, Reason: m.Reason, DurationSource: m.DurationSource, Basis: "closed_turn_lifetime_output", AverageUnit: "milli_tokens_per_second", DurationUnit: "milliseconds"}
	if m.CoverageKnown {
		v.IncludedTurns, v.ExcludedTurns, v.OpenTurns, v.UnattributedEvents = throughputDecimal(&m.IncludedTurns), throughputDecimal(&m.ExcludedTurns), throughputDecimal(&m.OpenTurns), throughputDecimal(&m.UnattributedEvents)
	}
	if m.OutputTokens != nil && m.ActiveDurationMS != nil {
		calculated := throughput.Measure(*m.OutputTokens, *m.ActiveDurationMS)
		v.AverageOutputMilliTPS = throughputDecimal(calculated.AverageMilliTPS)
		if v.AverageOutputMilliTPS == nil {
			v.Status, v.Reason = "unavailable", "numeric_overflow"
		}
	}
	return v
}

// attachThroughput 只读取返回页的生命周期 capsule；日期/模型不进入平均计算。
func (s *Statistics) attachLegacyThroughput(ctx context.Context, q statistics_dto.StatisticsQuery, items []statistics_vo.StatisticsSession, metadata map[string]reporting_do.Session, recent bool) (statistics_vo.ThroughputTurnsView, error) {
	limit := q.ThroughputLimit
	if limit == 0 {
		limit = 20
	}
	result := statistics_vo.ThroughputTurnsView{Items: []statistics_vo.ThroughputTurnView{}, Limit: limit}
	ids := make([]string, 0, len(items))
	for _, item := range items {
		if item.Provider == "codex" || item.Provider == "dsh" {
			ids = append(ids, item.ID)
		}
	}
	canonical := make(map[string]reportingv1.SessionSnapshot)
	sources := make(map[string][]reporting_dto.SourceSnapshot)
	bytes := 0
	decode := func(payload string, target *reportingv1.SessionSnapshot) error {
		bytes += len(payload)
		if bytes > 64<<20 {
			return utils.ErrRequestBudget
		}
		return json.Unmarshal([]byte(payload), target, json.RejectUnknownMembers(true))
	}
	if q.ClientID == "" {
		if err := s.repository.StreamCanonicalSessions(ctx, ids, func(row reporting_do.CanonicalSnapshot) error {
			var snapshot reportingv1.SessionSnapshot
			if err := decode(row.Payload, &snapshot); err != nil {
				return err
			}
			canonical[row.SessionKey] = snapshot
			return nil
		}); err != nil {
			return result, err
		}
	}
	if err := s.repository.StreamSessionSources(ctx, ids, q.ClientID, func(row reporting_do.SessionSource) error {
		if len(sources[row.SessionKey]) >= 128 {
			return utils.ErrRequestBudget
		}
		var snapshot reportingv1.SessionSnapshot
		if err := decode(row.Payload, &snapshot); err != nil {
			return err
		}
		sources[row.SessionKey] = append(sources[row.SessionKey], reporting_dto.SourceSnapshot{ID: row.ID, ClientID: row.ClientID, Snapshot: snapshot})
		return nil
	}); err != nil {
		return result, err
	}
	for i := range items {
		item := &items[i]
		reason := "unsupported_provider"
		if item.Provider == "codex" || item.Provider == "dsh" {
			reason = "not_reported"
		}
		view := throughputView(reportingv1.ThroughputMeasures{Status: "unavailable", Reason: reason})
		item.Throughput = &view
		chosen := canonical[item.ID]
		owner := ""
		for _, source := range sources[item.ID] {
			if source.ID == metadata[item.ID].CanonicalSourceID {
				owner = source.ClientID
			}
		}
		if q.ClientID != "" {
			decision := reporting_srv.DecideSnapshot(sources[item.ID], nil)
			chosen, owner = decision.Source.Snapshot, decision.Source.ClientID
		}
		item.CacheHitRate = cacheHitRateView(chosen, owner, item.Provider, sources[item.ID])
		capsule := chosen.Throughput
		if capsule == nil {
			continue
		}
		view = throughputView(capsule.Measures)
		if owner != "" {
			view.SourceClientID = new(owner)
		}
		// 同一 Token 事实的两个完整 TPS 证据相异时保留已接受值并显式标冲突。
		for _, source := range sources[item.ID] {
			other := source.Snapshot
			if other.Throughput == nil || other.Throughput.Measures.Status != "complete" || capsule.Measures.Status != "complete" || !sameThroughputFacts(chosen, other) {
				continue
			}
			if !reflect.DeepEqual(capsule.Measures, other.Throughput.Measures) {
				view.Conflict, view.Status, view.Reason = true, "partial", "source_conflict"
			}
		}
		item.Throughput = &view
		if recent {
			if capsule.Measures.CoverageKnown {
				result.Total = new(strconv.FormatInt(capsule.TurnsTotal, 10))
			}
			result.Truncated = capsule.TurnsTotal > int64(min(limit, len(capsule.RecentTurns)))
			for _, turn := range capsule.RecentTurns[:min(limit, len(capsule.RecentTurns))] {
				result.Items = append(result.Items, statistics_vo.ThroughputTurnView{Key: turn.Key, StartedAtMS: turn.StartedAtMS, EndedAtMS: turn.EndedAtMS, Throughput: throughputView(turn.Measures)})
			}
		}
	}
	return result, nil
}

func sameThroughputFacts(a, b reportingv1.SessionSnapshot) bool {
	if a.HistoryStartAtMS != b.HistoryStartAtMS || len(a.Contributions) != len(b.Contributions) {
		return false
	}
	ids := make(map[string]bool, len(a.Contributions))
	for _, c := range a.Contributions {
		ids[c.ID] = true
	}
	for _, c := range b.Contributions {
		if !ids[c.ID] {
			return false
		}
	}
	return true
}
