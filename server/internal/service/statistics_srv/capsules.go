package statistics_srv

import (
	"context"
	"encoding/json/v2"
	"reflect"
	"strconv"

	reportingv1 "github.com/SisyphusSQ/codex-pulse/api/codexpulse/reporting/v1"
	reporting_do "github.com/SisyphusSQ/codex-pulse/server/internal/models/do/mysql/reporting_do"
	statistics_dto "github.com/SisyphusSQ/codex-pulse/server/internal/models/dto/statistics_dto"
	statistics_vo "github.com/SisyphusSQ/codex-pulse/server/internal/models/vo/statistics_vo"
)

func capsuleSnapshot(row reporting_do.SessionCapsule) (out reportingv1.SessionSnapshot, err error) {
	out.Complete, out.Deleted = row.Complete, row.Deleted
	if row.Throughput != nil {
		err = json.Unmarshal([]byte(*row.Throughput), &out.Throughput)
		if err != nil {
			return
		}
	}
	if row.CacheUsage != nil {
		err = json.Unmarshal([]byte(*row.CacheUsage), &out.CacheUsage)
	}
	return
}

// attachThroughput 只读取小型生命周期投影，不为列表读取贡献与调用正文。
func (s *Statistics) attachThroughput(ctx context.Context, q statistics_dto.StatisticsQuery, items []statistics_vo.StatisticsSession, metadata map[string]reporting_do.Session, recent bool) (result statistics_vo.ThroughputTurnsView, err error) {
	limit := q.ThroughputLimit
	if limit == 0 {
		limit = 20
	}
	result = statistics_vo.ThroughputTurnsView{Items: []statistics_vo.ThroughputTurnView{}, Limit: limit}
	ids := []string{}
	for _, item := range items {
		if item.Provider == "codex" || item.Provider == "dsh" {
			ids = append(ids, item.ID)
		}
	}
	rows, err := s.repository.Capsules(ctx, ids, q.ClientID)
	if err != nil {
		return result, err
	}
	canonical := map[string]reporting_do.SessionCapsule{}
	sources := map[string][]reporting_do.SessionCapsule{}
	for _, row := range rows {
		if row.Kind == "canonical" {
			canonical[row.SessionKey] = row
		} else {
			sources[row.SessionKey] = append(sources[row.SessionKey], row)
		}
	}
	// 旧库摘要首次补建期间，维持原有指标语义；后台只需重建一次。
	for _, item := range items {
		if item.Provider != "codex" && item.Provider != "dsh" {
			continue
		}
		id := item.ID
		if len(sources[id]) == 0 || len(sources[id]) != len(item.Sources) || q.ClientID == "" && canonical[id].ID == "" {
			return s.attachLegacyThroughput(ctx, q, items, metadata, recent)
		}
	}
	for i := range items {
		item := &items[i]
		row := canonical[item.ID]
		owner := ""
		for _, source := range sources[item.ID] {
			if source.ID == metadata[item.ID].CanonicalSourceID {
				owner = source.ClientID
				if q.ClientID != "" {
					row = source
				}
			}
		}
		chosen, err := capsuleSnapshot(row)
		if err != nil {
			return result, err
		}
		item.CacheHitRate = cacheHitRateView(chosen, owner, item.Provider, nil)
		reason := "not_reported"
		if item.Provider != "codex" && item.Provider != "dsh" {
			reason = "unsupported_provider"
		}
		view := throughputView(reportingv1.ThroughputMeasures{Status: "unavailable", Reason: reason})
		item.Throughput = &view
		if chosen.Throughput != nil {
			view = throughputView(chosen.Throughput.Measures)
			if owner != "" {
				view.SourceClientID = new(owner)
			}
		}
		for _, source := range sources[item.ID] {
			if source.FactsDigest != row.FactsDigest {
				continue
			}
			other, err := capsuleSnapshot(source)
			if err != nil {
				return result, err
			}
			if chosen.Complete && other.Complete && chosen.CacheUsage != nil && other.CacheUsage != nil && !reflect.DeepEqual(chosen.CacheUsage, other.CacheUsage) {
				item.CacheHitRate.Conflict = true
				item.CacheHitRate.Status = "partial"
				item.CacheHitRate.Reason = "source_conflict"
			}
			if chosen.Throughput != nil && other.Throughput != nil && chosen.Throughput.Measures.Status == "complete" && other.Throughput.Measures.Status == "complete" && !reflect.DeepEqual(chosen.Throughput.Measures, other.Throughput.Measures) {
				view.Conflict = true
				view.Status = "partial"
				view.Reason = "source_conflict"
			}
		}
		if recent && chosen.Throughput != nil {
			capsule := chosen.Throughput
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
