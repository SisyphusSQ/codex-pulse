package statistics_srv

import (
	"context"
	"encoding/json/v2"
	"slices"
	"strings"

	reportingv1 "github.com/SisyphusSQ/codex-pulse/api/codexpulse/reporting/v1"
	reporting_do "github.com/SisyphusSQ/codex-pulse/server/internal/models/do/mysql/reporting_do"
	access_dto "github.com/SisyphusSQ/codex-pulse/server/internal/models/dto/access_dto"
	reporting_dto "github.com/SisyphusSQ/codex-pulse/server/internal/models/dto/reporting_dto"
	statistics_dto "github.com/SisyphusSQ/codex-pulse/server/internal/models/dto/statistics_dto"
	statistics_vo "github.com/SisyphusSQ/codex-pulse/server/internal/models/vo/statistics_vo"
	access_srv "github.com/SisyphusSQ/codex-pulse/server/internal/service/access_srv"
	reporting_srv "github.com/SisyphusSQ/codex-pulse/server/internal/service/reporting_srv"
	"github.com/SisyphusSQ/codex-pulse/server/utils"
)

// SourceUsage 一次流式扫描各机器的 Codex 副本；机器小计可重叠，不能累加成全局用量。
func (s *Statistics) SourceUsage(ctx context.Context, p access_dto.Principal, q statistics_dto.StatisticsQuery) (out statistics_vo.StatisticsSourceUsage, err error) {
	out = statistics_vo.StatisticsSourceUsage{Range: statisticsRange(q), Scope: "collector_copies_may_overlap", Items: []statistics_vo.StatisticsCollectorUsage{}}
	if err = access_srv.RequireAdmin(p); err != nil {
		return
	}
	if s.cache != nil {
		out, status, err := cachedProjection(ctx, s.cache, "source-usage", q, s.sourceUsage)
		out.Cache = status
		return out, err
	}
	return s.sourceUsage(ctx, q)
}

func (s *Statistics) sourceUsage(ctx context.Context, q statistics_dto.StatisticsQuery) (out statistics_vo.StatisticsSourceUsage, err error) {
	out = statistics_vo.StatisticsSourceUsage{Range: statisticsRange(q), Scope: "collector_copies_may_overlap", Items: []statistics_vo.StatisticsCollectorUsage{}}
	if q.Provider != "" && q.Provider != "codex" {
		return
	}
	q.Provider = "codex"
	err = s.repository.Snapshot(ctx, func(ctx context.Context) error {
		clients, e := s.repository.Clients(ctx)
		if e != nil {
			return e
		}
		projects, e := s.repository.Projects(ctx)
		if e != nil {
			return e
		}
		projectMap := map[string]reporting_do.Project{}
		for _, project := range projects {
			projectMap[project.ID] = project
		}
		readers := map[string]*statisticsRead{}
		for _, c := range clients {
			if q.ClientID != "" && q.ClientID != c.ID {
				continue
			}
			cq := q
			cq.ClientID = c.ID
			readers[c.ID] = &statisticsRead{q: cq, projects: projectMap, metadata: map[string]reporting_do.Session{}, total: newStatisticsAggregate(), providerSeen: map[string]bool{}, days: map[string]*statisticsAggregate{}, heatmapOnly: true}
		}
		status, e := s.repository.Status(ctx, q)
		if e != nil {
			return e
		}
		for _, st := range status {
			if r := readers[st.ClientID]; r != nil {
				r.status = append(r.status, st)
				r.collected = st.CollectedAtMS
				if st.CollectedAtMS != nil && (st.Status == "ready" || st.Status == "partial") {
					r.providerSeen["codex"] = true
				}
			}
		}
		own := map[string][]reporting_dto.SourceSnapshot{}
		key := ""
		sourceRows, facts, size := 0, 0, 0
		flush := func() error {
			for client, copies := range own {
				r := readers[client]
				if r == nil {
					continue
				}
				d := reporting_srv.DecideSnapshot(copies, nil)
				if d.Deleted {
					continue
				}
				snap := d.Source.Snapshot
				m := reporting_do.Session{ID: key, Provider: "codex", SessionID: snap.SessionID, Title: snap.Title, ProjectID: reportingv1.Key(client, "codex", snap.ProjectID), SessionKind: snap.SessionKind, CollectedAtMS: snap.CollectedAtMS, Complete: snap.Complete, Conflict: d.Conflict}
				if !r.matches(m) {
					continue
				}
				r.metadata[key] = m
				r.providerSeen["codex"] = true
				if r.collected == nil || snap.CollectedAtMS > *r.collected {
					r.collected = new(snap.CollectedAtMS)
				}
				for _, c := range snap.Contributions {
					facts++
					if facts > statistics_dto.MaximumStatisticsFacts {
						return utils.ErrRequestBudget
					}
					r.usage(usageFromContribution(key, c))
				}
			}
			own = map[string][]reporting_dto.SourceSnapshot{}
			size = 0
			return nil
		}
		scanQuery := q
		scanQuery.ProjectID = ""
		e = s.repository.StreamSources(ctx, scanQuery, func(row reporting_do.SessionSource) error {
			sourceRows++
			if sourceRows > statistics_dto.MaximumStatisticsSources {
				return utils.ErrRequestBudget
			}
			if key != row.SessionKey {
				if e := flush(); e != nil {
					return e
				}
				key = row.SessionKey
			}
			size += len(row.Payload)
			if size > 64<<20 || len(own[row.ClientID]) >= 128 {
				return utils.ErrRequestBudget
			}
			var snap reportingv1.SessionSnapshot
			if e := json.Unmarshal([]byte(row.Payload), &snap, json.RejectUnknownMembers(true)); e != nil {
				return e
			}
			snap.Invocations = nil
			own[row.ClientID] = append(own[row.ClientID], reporting_dto.SourceSnapshot{ID: row.ID, ClientID: row.ClientID, Snapshot: snap})
			return nil
		})
		if e != nil {
			return e
		}
		if e = flush(); e != nil {
			return e
		}
		for _, c := range clients {
			r := readers[c.ID]
			if r == nil {
				continue
			}
			totals, _ := r.total.finish(false)
			out.Items = append(out.Items, statistics_vo.StatisticsCollectorUsage{Machine: statistics_vo.StatisticsMachine{ClientID: c.ID, ClientName: c.Name}, Totals: totals, Coverage: r.coverage(s.now()), RevokedAtMS: c.RevokedAtMS})
		}
		slices.SortFunc(out.Items, func(a, b statistics_vo.StatisticsCollectorUsage) int {
			n := -decimalCompare(a.Totals.TotalTokens, b.Totals.TotalTokens)
			if n == 0 {
				return strings.Compare(a.Machine.ClientID, b.Machine.ClientID)
			}
			return n
		})
		return nil
	})
	return
}
