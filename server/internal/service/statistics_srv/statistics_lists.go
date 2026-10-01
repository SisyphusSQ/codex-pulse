package statistics_srv

import (
	"cmp"
	"context"
	"math/big"
	"slices"
	"strings"

	reporting_do "github.com/SisyphusSQ/codex-pulse/server/internal/models/do/mysql/reporting_do"
	access_dto "github.com/SisyphusSQ/codex-pulse/server/internal/models/dto/access_dto"
	statistics_dto "github.com/SisyphusSQ/codex-pulse/server/internal/models/dto/statistics_dto"
	statistics_vo "github.com/SisyphusSQ/codex-pulse/server/internal/models/vo/statistics_vo"
	access_srv "github.com/SisyphusSQ/codex-pulse/server/internal/service/access_srv"
	"github.com/SisyphusSQ/codex-pulse/server/utils"
)

func (o *statisticsRead) sessionView(m reporting_do.Session) statistics_vo.StatisticsSession {
	g := o.sessions[m.ID]
	if g == nil {
		g = newStatisticsAggregate()
	}
	totals, _ := g.finish(len(o.providerSeen) > 0)
	var raw *string
	if m.SessionKind != "unassigned_usage" {
		raw = new(m.SessionID)
	}
	project := o.projects[m.ProjectID]
	sources := o.sources[m.ID]
	if sources == nil {
		sources = []statistics_vo.StatisticsSource{}
	}
	return statistics_vo.StatisticsSession{ID: m.ID, Provider: m.Provider, SessionID: raw, Title: m.Title, SessionKind: m.SessionKind, ProjectID: m.ProjectID, ProjectGroupID: project.GroupID, ProjectName: project.Name, CreatedAtMS: m.CreatedAtMS, LastActiveAtMS: m.LastActiveAtMS, CollectedAtMS: m.CollectedAtMS, Complete: m.Complete, Conflict: m.Conflict, Sources: sources, Totals: totals}
}
func inStatisticsRange(at *int64, q statistics_dto.StatisticsQuery) bool {
	return at != nil && *at >= q.StartAtMS && *at < q.EndAtMS
}
func decimalCompare(a, b *string) int {
	if a == nil && b == nil {
		return 0
	}
	if a == nil {
		return -1
	}
	if b == nil {
		return 1
	}
	left, _ := new(big.Int).SetString(*a, 10)
	right, _ := new(big.Int).SetString(*b, 10)
	return left.Cmp(right)
}
func timestampCompare(a, b *int64) int {
	if a == nil && b == nil {
		return 0
	}
	if a == nil {
		return -1
	}
	if b == nil {
		return 1
	}
	return cmp.Compare(*a, *b)
}
func compareSession(a, b statistics_vo.StatisticsSession, q statistics_dto.StatisticsQuery) int {
	var result int
	switch q.Sort {
	case "title", "name":
		result = strings.Compare(a.Title, b.Title)
	case "tokens":
		result = decimalCompare(a.Totals.TotalTokens, b.Totals.TotalTokens)
	case "cost":
		result = decimalCompare(a.Totals.CostMicroUSD, b.Totals.CostMicroUSD)
	default:
		result = timestampCompare(a.LastActiveAtMS, b.LastActiveAtMS)
	}
	if q.Direction == "desc" {
		result = -result
	}
	if result == 0 {
		return strings.Compare(a.ID, b.ID)
	}
	return result
}
func statisticsPage[T any](items []T, q statistics_dto.StatisticsQuery) ([]T, statistics_vo.StatisticsPage) {
	page := statistics_vo.StatisticsPage{Page: q.Page, Limit: q.Limit, Total: int64(len(items))}
	start := min(len(items), (q.Page-1)*q.Limit)
	end := min(len(items), start+q.Limit)
	return items[start:end], page
}
func (o *statisticsRead) sessionList() statistics_vo.StatisticsSessions {
	items := make([]statistics_vo.StatisticsSession, 0)
	for _, m := range o.metadata {
		if o.sessions[m.ID] == nil && (!inStatisticsRange(m.CreatedAtMS, o.q) && !inStatisticsRange(m.LastActiveAtMS, o.q)) {
			continue
		}
		if o.q.Model != "" && o.sessions[m.ID] == nil {
			continue
		}
		items = append(items, o.sessionView(m))
	}
	slices.SortFunc(items, func(a, b statistics_vo.StatisticsSession) int { return compareSession(a, b, o.q) })
	items, page := statisticsPage(items, o.q)
	totals, _ := o.total.finish(len(o.providerSeen) > 0)
	return statistics_vo.StatisticsSessions{Range: statisticsRange(o.q), Scope: o.scope(), Page: page, Items: items, Totals: totals}
}
func (s *Statistics) Sessions(ctx context.Context, p access_dto.Principal, q statistics_dto.StatisticsQuery) (result statistics_vo.StatisticsSessions, err error) {
	if err = access_srv.RequireAdmin(p); err != nil {
		return
	}
	err = s.repository.Snapshot(ctx, func(ctx context.Context) error {
		read, err := s.read(ctx, q)
		if err != nil {
			return err
		}
		result = read.sessionList()
		result.Coverage = read.coverage(s.now())
		return nil
	})
	return
}
func (s *Statistics) Session(ctx context.Context, p access_dto.Principal, q statistics_dto.StatisticsQuery, key string) (result statistics_vo.StatisticsSessionDetail, err error) {
	if err = access_srv.RequireAdmin(p); err != nil {
		return
	}
	if !statisticsKey(key) {
		return result, utils.ErrBadParamInput
	}
	q.SessionKey = key
	err = s.repository.Snapshot(ctx, func(ctx context.Context) error {
		read, err := s.read(ctx, q)
		if err != nil {
			return err
		}
		m, ok := read.metadata[key]
		if !ok {
			return utils.ErrNotFound
		}
		result = statistics_vo.StatisticsSessionDetail{Session: read.sessionView(m), Range: statisticsRange(q), Trend: read.trend(), Tools: statisticsSlices(read.tools, true), Skills: statisticsSlices(read.skills, true), Coverage: read.coverage(s.now())}
		return nil
	})
	return
}
func (o *statisticsRead) projectViews() []statistics_vo.StatisticsProject {
	groups := map[string]statistics_vo.StatisticsProject{}
	for _, p := range o.projects {
		if o.q.ProjectID != "" && p.GroupID != o.q.ProjectID {
			continue
		}
		if o.q.Provider != "" && p.Provider != o.q.Provider {
			continue
		}
		if o.q.ClientID != "" && p.ClientID != o.q.ClientID {
			continue
		}
		group := groups[p.GroupID]
		if group.ID == "" {
			group.ID = p.GroupID
			group.Name = p.Name
		}
		if p.ID == p.GroupID {
			group.Name = p.Name
		}
		group.Members = append(group.Members, p.ID)
		groups[p.GroupID] = group
	}
	out := make([]statistics_vo.StatisticsProject, 0, len(groups))
	for id, project := range groups {
		g := o.projectGroups[id]
		if g == nil {
			g = newStatisticsAggregate()
		}
		project.Totals, _ = g.finish(len(o.providerSeen) > 0)
		for _, m := range o.metadata {
			if o.projects[m.ProjectID].GroupID != id {
				continue
			}
			if timestampCompare(m.LastActiveAtMS, project.LastActiveAtMS) > 0 {
				project.LastActiveAtMS = m.LastActiveAtMS
			}
			project.Conflict = project.Conflict || m.Conflict
		}
		if o.q.Search != "" && len(g.sessions) == 0 && !strings.Contains(strings.ToLower(project.Name), strings.ToLower(o.q.Search)) {
			continue
		}
		slices.Sort(project.Members)
		out = append(out, project)
	}
	slices.SortFunc(out, func(a, b statistics_vo.StatisticsProject) int {
		var result int
		switch o.q.Sort {
		case "title", "name":
			result = strings.Compare(a.Name, b.Name)
		case "tokens":
			result = decimalCompare(a.Totals.TotalTokens, b.Totals.TotalTokens)
		case "cost":
			result = decimalCompare(a.Totals.CostMicroUSD, b.Totals.CostMicroUSD)
		default:
			result = timestampCompare(a.LastActiveAtMS, b.LastActiveAtMS)
		}
		if o.q.Direction == "desc" {
			result = -result
		}
		if result == 0 {
			return strings.Compare(a.ID, b.ID)
		}
		return result
	})
	return out
}
func (s *Statistics) Projects(ctx context.Context, p access_dto.Principal, q statistics_dto.StatisticsQuery) (result statistics_vo.StatisticsProjects, err error) {
	if err = access_srv.RequireAdmin(p); err != nil {
		return
	}
	err = s.repository.Snapshot(ctx, func(ctx context.Context) error {
		read, err := s.read(ctx, q)
		if err != nil {
			return err
		}
		items, page := statisticsPage(read.projectViews(), q)
		totals, _ := read.total.finish(len(read.providerSeen) > 0)
		result = statistics_vo.StatisticsProjects{Range: statisticsRange(q), Scope: read.scope(), Page: page, Items: items, Totals: totals, Coverage: read.coverage(s.now())}
		return nil
	})
	return
}
func (s *Statistics) Project(ctx context.Context, p access_dto.Principal, q statistics_dto.StatisticsQuery, key string) (result statistics_vo.StatisticsProjectDetail, err error) {
	if err = access_srv.RequireAdmin(p); err != nil {
		return
	}
	if !statisticsKey(key) {
		return result, utils.ErrBadParamInput
	}
	if q.ProjectID != "" && q.ProjectID != key {
		return result, utils.ErrNotFound
	}
	q.ProjectID = key
	err = s.repository.Snapshot(ctx, func(ctx context.Context) error {
		read, err := s.read(ctx, q)
		if err != nil {
			return err
		}
		items := read.projectViews()
		if len(items) != 1 {
			return utils.ErrNotFound
		}
		sessions := read.sessionList()
		sessions.Coverage = read.coverage(s.now())
		summary := read.summary(s.now())
		result = statistics_vo.StatisticsProjectDetail{Project: items[0], Sessions: sessions, Trend: summary.Trend, Models: summary.Models}
		return nil
	})
	return
}
func (s *Statistics) Devices(ctx context.Context, p access_dto.Principal) (out []statistics_vo.StatisticsDevice, err error) {
	out = []statistics_vo.StatisticsDevice{}
	if err = access_srv.RequireAdmin(p); err != nil {
		return
	}
	now := s.now().UnixMilli()
	err = s.repository.Snapshot(ctx, func(ctx context.Context) error {
		clients, err := s.repository.Clients(ctx)
		if err != nil {
			return err
		}
		status, err := s.repository.Status(ctx, statistics_dto.StatisticsQuery{})
		if err != nil {
			return err
		}
		for _, c := range clients {
			view := statistics_vo.StatisticsDevice{ID: c.ID, Name: c.Name, RevokedAtMS: c.RevokedAtMS, LastReceivedAtMS: c.LastReceivedAtMS, Providers: []statistics_vo.StatisticsDeviceProvider{}}
			for _, st := range status {
				if st.ClientID != c.ID {
					continue
				}
				view.Providers = append(view.Providers, statistics_vo.StatisticsDeviceProvider{Provider: st.Provider, Version: st.Version, CollectedAtMS: st.CollectedAtMS, CoverageStartMS: st.CoverageStartMS, CoverageEndMS: st.CoverageEndMS, PendingBatches: st.PendingBatches, Status: st.Status, ReceivedAtMS: st.ReceivedAtMS, Stale: st.CollectedAtMS == nil || now-*st.CollectedAtMS > int64(15*60*1000)})
			}
			out = append(out, view)
		}
		return nil
	})
	return
}
