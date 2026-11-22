package statistics_srv

import (
	"context"
	"encoding/json/v2"
	"slices"
	"strings"

	reporting_do "github.com/SisyphusSQ/codex-pulse/server/internal/models/do/mysql/reporting_do"
	access_dto "github.com/SisyphusSQ/codex-pulse/server/internal/models/dto/access_dto"
	statistics_dto "github.com/SisyphusSQ/codex-pulse/server/internal/models/dto/statistics_dto"
	statistics_vo "github.com/SisyphusSQ/codex-pulse/server/internal/models/vo/statistics_vo"
	access_srv "github.com/SisyphusSQ/codex-pulse/server/internal/service/access_srv"
	"github.com/SisyphusSQ/codex-pulse/server/internal/sessionmerge"
	"github.com/SisyphusSQ/codex-pulse/server/utils"
)

// SourceUsage 读取与日期范围相交的机器来源；各机器小计仍可重叠。
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
			// 来源接口只返回总量及覆盖；保留按日计价，但不重复构建未使用的日桶。
			readers[c.ID] = &statisticsRead{q: cq, projects: projectMap, metadata: map[string]reporting_do.Session{}, total: newStatisticsAggregate(), providerSeen: map[string]bool{}, projection: "source-usage"}
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
		sources, e := s.repository.MachineUsageSources(ctx, q)
		if e != nil {
			return e
		}
		for _, src := range sources {
			r := readers[src.ClientID]
			m := reporting_do.Session{ID: src.SessionKey, Provider: "codex", SessionID: src.SessionID, Title: src.Title, ProjectID: src.ProjectID, CollectedAtMS: src.CollectedAtMS, Complete: src.Complete, Conflict: src.Conflict}
			if r == nil || !r.matches(m) {
				continue
			}
			r.metadata[m.ID] = m
			r.providerSeen["codex"] = true
			if r.collected == nil || m.CollectedAtMS > *r.collected {
				r.collected = new(m.CollectedAtMS)
			}
			if src.UntimedModels != nil {
				var untimed map[string]int64
				if e := json.Unmarshal([]byte(*src.UntimedModels), &untimed); e != nil {
					return e
				}
				if q.Model != "" {
					r.total.untimed += untimed[q.Model]
				} else {
					for _, count := range untimed {
						r.total.untimed += count
					}
				}
			}
		}
		facts := 0
		e = s.repository.StreamMachineUsageSources(ctx, q, func(row reporting_do.SessionSource) error {
			r := readers[row.ClientID]
			if r == nil || r.metadata[row.SessionKey].ID == "" {
				return nil
			}
			source, e := sessionmerge.DecodeSource(row)
			if e != nil {
				return e
			}
			for _, fact := range source.Snapshot.Contributions {
				at := fact.ObservedAtMS
				if at == nil || *at < q.StartAtMS || *at >= q.EndAtMS {
					continue
				}
				facts++
				if facts > statistics_dto.MaximumStatisticsFacts {
					return utils.ErrRequestBudget
				}
				r.usage(usageFromContribution(row.SessionKey, fact))
			}
			return nil
		})
		if e != nil {
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
