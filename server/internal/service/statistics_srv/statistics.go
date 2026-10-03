package statistics_srv

import (
	"context"
	"encoding/json/v2"
	"math/big"
	"strings"
	"time"

	reportingv1 "github.com/SisyphusSQ/codex-pulse/api/codexpulse/reporting/v1"
	"github.com/SisyphusSQ/codex-pulse/internal/pricing"
	access_do "github.com/SisyphusSQ/codex-pulse/server/internal/models/do/mysql/access_do"
	reporting_do "github.com/SisyphusSQ/codex-pulse/server/internal/models/do/mysql/reporting_do"
	access_dto "github.com/SisyphusSQ/codex-pulse/server/internal/models/dto/access_dto"
	reporting_dto "github.com/SisyphusSQ/codex-pulse/server/internal/models/dto/reporting_dto"
	statistics_dto "github.com/SisyphusSQ/codex-pulse/server/internal/models/dto/statistics_dto"
	statistics_vo "github.com/SisyphusSQ/codex-pulse/server/internal/models/vo/statistics_vo"
	statistics_repo "github.com/SisyphusSQ/codex-pulse/server/internal/repository/mysql/statistics_repo"
	access_srv "github.com/SisyphusSQ/codex-pulse/server/internal/service/access_srv"
	reporting_srv "github.com/SisyphusSQ/codex-pulse/server/internal/service/reporting_srv"
	"github.com/SisyphusSQ/codex-pulse/server/utils"
)

type Statistics struct {
	repository *statistics_repo.Statistics
	now        func() time.Time
}

func NewStatistics(repository *statistics_repo.Statistics) *Statistics {
	return &Statistics{repository: repository, now: time.Now}
}

// statisticsRead 仅保存本次只读快照的计算状态；对外只返回明确 VO。
type statisticsRead struct {
	q                                                                      statistics_dto.StatisticsQuery
	projects                                                               map[string]reporting_do.Project
	metadata                                                               map[string]reporting_do.Session
	sources                                                                map[string][]statistics_vo.StatisticsSource
	clients                                                                map[string]access_do.Client
	status                                                                 []reporting_do.DeviceStatus
	total                                                                  *statisticsAggregate
	providers, models, days, sessions, tools, skills, hours, projectGroups map[string]*statisticsAggregate
	providerSeen                                                           map[string]bool
	collected                                                              *int64
	modelDays                                                              map[string]*statisticsAggregate
	modelTotals                                                            map[string]*statisticsAggregate
	cursorPools                                                            map[string]*statisticsAggregate
	modelTrendExceeded                                                     bool
	heatmapOnly                                                            bool
}

func (s *Statistics) read(ctx context.Context, q statistics_dto.StatisticsQuery) (*statisticsRead, error) {
	return s.readWithModelTrend(ctx, q, false)
}
func (s *Statistics) readWithModelTrend(ctx context.Context, q statistics_dto.StatisticsQuery, include bool) (*statisticsRead, error) {
	return s.readFacts(ctx, q, include, false)
}
func (s *Statistics) readFacts(ctx context.Context, q statistics_dto.StatisticsQuery, include, heatmapOnly bool) (*statisticsRead, error) {
	out := &statisticsRead{q: q, projects: map[string]reporting_do.Project{}, metadata: map[string]reporting_do.Session{}, sources: map[string][]statistics_vo.StatisticsSource{}, clients: map[string]access_do.Client{}, total: newStatisticsAggregate(), providers: map[string]*statisticsAggregate{}, models: map[string]*statisticsAggregate{}, days: map[string]*statisticsAggregate{}, sessions: map[string]*statisticsAggregate{}, tools: map[string]*statisticsAggregate{}, skills: map[string]*statisticsAggregate{}, hours: map[string]*statisticsAggregate{}, projectGroups: map[string]*statisticsAggregate{}, providerSeen: map[string]bool{}}
	out.heatmapOnly = heatmapOnly
	if include {
		out.modelDays = map[string]*statisticsAggregate{}
		out.modelTotals = map[string]*statisticsAggregate{}
		out.cursorPools = map[string]*statisticsAggregate{}
	}
	projects, err := s.repository.Projects(ctx)
	if err != nil {
		return nil, err
	}
	for _, p := range projects {
		out.projects[p.ID] = p
		if (q.Provider == "" || q.Provider == p.Provider) && (q.ClientID == "" || q.ClientID == p.ClientID) && (q.ProjectID == "" || q.ProjectID == p.GroupID) && p.UpdatedAtMS > 0 {
			out.providerSeen[p.Provider] = true
		}
	}
	clients, err := s.repository.Clients(ctx)
	if err != nil {
		return nil, err
	}
	for _, c := range clients {
		out.clients[c.ID] = c
	}
	out.status, err = s.repository.Status(ctx, q)
	if err != nil {
		return nil, err
	}
	for _, st := range out.status {
		if st.CollectedAtMS != nil {
			if out.collected == nil || *st.CollectedAtMS > *out.collected {
				out.collected = st.CollectedAtMS
			}
			if st.Status == "ready" || st.Status == "partial" {
				out.providerSeen[st.Provider] = true
			}
		}
	}
	metadata, err := s.repository.Sessions(ctx, q)
	if err != nil {
		return nil, err
	}
	for _, m := range metadata {
		out.metadata[m.ID] = m
		if q.ClientID == "" && m.CollectedAtMS > 0 {
			out.providerSeen[m.Provider] = true
			if out.collected == nil || m.CollectedAtMS > *out.collected {
				out.collected = new(m.CollectedAtMS)
			}
		}
	}
	// 来源按 session 顺序流式读取，限定单个会话的仲裁内存；不在 Rows 活跃时再次查询数据库。
	sourceRows := 0
	factRows := 0
	var own []reporting_dto.SourceSnapshot
	size := 0
	key := ""
	flush := func() error {
		if len(own) == 0 {
			return nil
		}
		decision := reporting_srv.DecideSnapshot(own, nil)
		own = nil
		size = 0
		if decision.Deleted {
			delete(out.metadata, key)
			return nil
		}
		chosen := decision.Source.Snapshot
		m := reporting_do.Session{ID: key, Provider: chosen.Provider, SessionID: chosen.SessionID, Title: chosen.Title, ProjectID: reportingv1.Key(q.ClientID, chosen.Provider, chosen.ProjectID), SessionKind: chosen.SessionKind, SourceKind: chosen.SourceKind, HistoryStartAtMS: chosen.HistoryStartAtMS, CollectedAtMS: chosen.CollectedAtMS, CreatedAtMS: chosen.CreatedAtMS, LastActiveAtMS: chosen.LastActiveAtMS, Complete: chosen.Complete, Conflict: decision.Conflict, CanonicalSourceID: decision.Source.ID}
		out.metadata[key] = m
		if m.CollectedAtMS > 0 {
			out.providerSeen[m.Provider] = true
			if out.collected == nil || m.CollectedAtMS > *out.collected {
				out.collected = new(m.CollectedAtMS)
			}
		}
		if !out.matches(m) {
			delete(out.metadata, key)
			return nil
		}
		for _, c := range chosen.Contributions {
			factRows++
			if factRows > statistics_dto.MaximumStatisticsFacts {
				return utils.ErrRequestBudget
			}
			out.usage(usageFromContribution(key, c))
			if out.modelTrendExceeded {
				return utils.ErrRequestBudget
			}
		}
		for _, i := range chosen.Invocations {
			factRows++
			if factRows > statistics_dto.MaximumStatisticsFacts {
				return utils.ErrRequestBudget
			}
			out.call(reporting_do.Invocation{SessionKey: key, InvocationID: i.ID, ObservedAtMS: i.ObservedAtMS, Kind: i.Kind, ToolName: i.Name, Outcome: i.Outcome, DurationMS: i.DurationMS})
		}
		return nil
	}
	if q.ClientID == "" {
		if !heatmapOnly {
			err = s.repository.StreamSourceMetadata(ctx, q, func(row statistics_dto.SourceMetadata) error {
				sourceRows++
				if sourceRows > statistics_dto.MaximumStatisticsSources {
					return utils.ErrRequestBudget
				}
				out.sources[row.SessionKey] = append(out.sources[row.SessionKey], statistics_vo.StatisticsSource{ID: row.ID, ClientID: row.ClientID, ClientName: out.clients[row.ClientID].Name, CollectedAtMS: row.CollectedAtMS, Revision: row.Revision, SourceKind: row.SourceKind, Complete: row.Complete, Deleted: row.Deleted})
				return nil
			})
		}
	} else {
		err = s.repository.StreamSources(ctx, q, func(row reporting_do.SessionSource) error {
			sourceRows++
			if sourceRows > statistics_dto.MaximumStatisticsSources {
				return utils.ErrRequestBudget
			}
			if q.ClientID != "" && key != row.SessionKey {
				if err := flush(); err != nil {
					return err
				}
				key = row.SessionKey
			}
			var snapshot reportingv1.SessionSnapshot
			if err := json.Unmarshal([]byte(row.Payload), &snapshot, json.RejectUnknownMembers(true)); err != nil {
				return err
			}
			out.sources[row.SessionKey] = append(out.sources[row.SessionKey], statistics_vo.StatisticsSource{ID: row.ID, ClientID: row.ClientID, ClientName: out.clients[row.ClientID].Name, CollectedAtMS: row.CollectedAtMS, Revision: row.Revision, SourceKind: row.SourceKind, Complete: snapshot.Complete, Deleted: snapshot.Deleted})
			if q.ClientID != "" {
				size += len(row.Payload)
				if size > 64<<20 || len(own) >= 128 {
					return utils.ErrRequestBudget
				}
				own = append(own, reporting_dto.SourceSnapshot{ID: row.ID, ClientID: row.ClientID, Snapshot: snapshot})
			}
			return nil
		})

	}
	if err != nil {
		return nil, err
	}
	if q.ClientID != "" {
		if err := flush(); err != nil {
			return nil, err
		}
	} else {
		for key, m := range out.metadata {
			if !out.matches(m) {
				delete(out.metadata, key)
			}
		}
		if err := s.repository.StreamUsage(ctx, q, func(row reporting_do.Usage) error {
			factRows++
			if factRows > statistics_dto.MaximumStatisticsFacts {
				return utils.ErrRequestBudget
			}
			out.usage(row)
			if out.modelTrendExceeded {
				return utils.ErrRequestBudget
			}
			return nil
		}); err != nil {
			return nil, err
		}
		if err := s.repository.StreamInvocations(ctx, q, func(row reporting_do.Invocation) error {
			factRows++
			if factRows > statistics_dto.MaximumStatisticsFacts {
				return utils.ErrRequestBudget
			}
			out.call(row)
			return nil
		}); err != nil {
			return nil, err
		}
		if err := s.repository.StreamUntimed(ctx, q, func(row reporting_do.Usage) error {
			factRows++
			if factRows > statistics_dto.MaximumStatisticsFacts {
				return utils.ErrRequestBudget
			}
			if _, ok := out.metadata[row.SessionKey]; ok && (q.Model == "" || valueString(row.Model, "unknown") == q.Model) {
				out.total.untimed++
			}
			return nil
		}); err != nil {
			return nil, err
		}
	}
	return out, nil
}
func usageFromContribution(key string, c reportingv1.Contribution) reporting_do.Usage {
	row := reporting_do.Usage{SessionKey: key, ContributionID: c.ID, ObservedAtMS: c.ObservedAtMS, Model: c.Model, InputTokens: c.InputTokens, CachedTokens: c.CachedTokens, CacheWriteTokens: c.CacheWriteTokens, OutputTokens: c.OutputTokens, ReasoningTokens: c.ReasoningTokens, TotalTokens: c.TotalTokens, CostMicroUSD: c.CostMicroUSD, ReportedChargeMicroUSD: c.ReportedChargeMicroUSD, PricingVersion: c.PricingVersion, PricingMode: c.PricingMode, CostStatus: c.CostStatus}
	if c.Rates != nil {
		row.InputPrice = c.Rates.InputMicroUSD
		row.CachedPrice = c.Rates.CachedMicroUSD
		row.CacheWritePrice = c.Rates.CacheWriteMicroUSD
		row.OutputPrice = c.Rates.OutputMicroUSD
	}
	return row
}
func (o *statisticsRead) matches(m reporting_do.Session) bool {
	if o.q.ProjectID != "" && o.projects[m.ProjectID].GroupID != o.q.ProjectID {
		return false
	}
	if o.q.Search != "" {
		term := strings.ToLower(o.q.Search)
		return strings.Contains(strings.ToLower(m.Title), term) || strings.Contains(strings.ToLower(m.SessionID), term) || strings.Contains(strings.ToLower(o.projects[m.ProjectID].Name), term)
	}
	return true
}
func (o *statisticsRead) usage(row reporting_do.Usage) {
	m, ok := o.metadata[row.SessionKey]
	if !ok || (o.q.Model != "" && valueString(row.Model, "unknown") != o.q.Model) {
		return
	}
	if row.ObservedAtMS == nil {
		o.total.untimed++
		return
	}
	at := *row.ObservedAtMS
	if at < o.q.StartAtMS || at >= o.q.EndAtMS {
		return
	}
	o.providerSeen[m.Provider] = true
	day := statisticsDay(at, o.q.Location).Format(time.DateOnly)
	if o.modelDays != nil {
		model := valueString(row.Model, "unknown")
		key := strings.Join([]string{m.Provider, model, day}, "\x00")
		if _, exists := o.modelDays[key]; !exists && len(o.modelDays) >= 20000 {
			o.modelTrendExceeded = true
			return
		}
		aggregateFor(o.modelDays, key).usage(row, o.q.Location, m.Provider)
		aggregateFor(o.modelTotals, m.Provider+"\x00"+model).usage(row, o.q.Location, m.Provider)
		if m.Provider == "cursor" {
			aggregateFor(o.cursorPools, pricing.CursorUsagePoolForModel(model, at)).usage(row, o.q.Location, m.Provider)
		}
	}

	if o.heatmapOnly {
		o.total.usage(row, o.q.Location, m.Provider)
		aggregateFor(o.days, day).usage(row, o.q.Location, m.Provider)
		return
	}
	local := time.UnixMilli(at).In(o.q.Location)
	hour := local.Format("Mon-15")
	for _, g := range []*statisticsAggregate{o.total, sessionAggregate(o.sessions, row.SessionKey), aggregateFor(o.providers, m.Provider), aggregateFor(o.models, valueString(row.Model, "unknown")), aggregateFor(o.days, day), aggregateFor(o.hours, hour), aggregateFor(o.projectGroups, o.projects[m.ProjectID].GroupID)} {
		g.usage(row, o.q.Location, m.Provider)
	}
}
func (o *statisticsRead) call(row reporting_do.Invocation) {
	m, ok := o.metadata[row.SessionKey]
	if !ok || row.ObservedAtMS < o.q.StartAtMS || row.ObservedAtMS >= o.q.EndAtMS {
		return
	}
	// 模型归因属于 Token 事实；模型筛选时工具计数不能假定属于该模型。
	if o.q.Model != "" {
		return
	}
	o.providerSeen[m.Provider] = true
	if o.heatmapOnly {
		o.total.call(row)
		aggregateFor(o.days, statisticsDay(row.ObservedAtMS, o.q.Location).Format(time.DateOnly)).call(row)
		return
	}
	for _, g := range []*statisticsAggregate{o.total, sessionAggregate(o.sessions, row.SessionKey), aggregateFor(o.providers, m.Provider), aggregateFor(o.days, statisticsDay(row.ObservedAtMS, o.q.Location).Format(time.DateOnly)), aggregateFor(o.projectGroups, o.projects[m.ProjectID].GroupID)} {
		g.call(row)
	}
	if row.Kind == "skill" {
		aggregateFor(o.skills, row.ToolName).call(row)
	} else {
		aggregateFor(o.tools, row.ToolName).call(row)
	}
}
func statisticsRange(q statistics_dto.StatisticsQuery) statistics_vo.StatisticsRange {
	return statistics_vo.StatisticsRange{StartAtMS: q.StartAtMS, EndAtMS: q.EndAtMS, TimeZone: q.TimeZone}
}
func (o *statisticsRead) scope() string {
	if o.q.ClientID != "" {
		return "collector:" + o.q.ClientID
	}
	return "global_deduplicated"
}
func (o *statisticsRead) coverage(now time.Time) statistics_vo.StatisticsCoverage {
	_, unpriced := o.total.finish(false)
	c := statistics_vo.StatisticsCoverage{State: "unknown", KnownProviders: len(o.providerSeen), UnpricedFacts: unpriced, UnknownTokenFacts: o.total.unknown, UntimedFacts: o.total.untimed, CollectedAtMS: o.collected, Stale: o.collected == nil || now.UnixMilli()-*o.collected > int64(15*time.Minute/time.Millisecond)}
	// ready 描述本机索引状态；不能证明全部分页已经送达中心。
	// 当前只确认收到的事实，未收到全量导出清单前不把范围覆盖声明为完整。
	c.RangeCovered = false
	c.Scope = "received_facts"
	latest := map[string]int64{}
	for _, st := range o.status {
		if st.CollectedAtMS != nil {
			latest[st.Provider] = max(latest[st.Provider], *st.CollectedAtMS)
		}
	}
	for _, m := range o.metadata {
		latest[m.Provider] = max(latest[m.Provider], m.CollectedAtMS)
	}
	for provider := range o.providerSeen {
		if latest[provider] == 0 || now.UnixMilli()-latest[provider] > int64(15*time.Minute/time.Millisecond) {
			c.Stale = true
		}
	}
	for key := range o.total.sessions {
		m := o.metadata[key]
		if !m.Complete {
			c.PartialSessions++
		}
		if m.Conflict {
			c.ConflictedSessions++
		}
	}
	if c.KnownProviders > 0 {
		c.State = "known"
		if c.Stale || !c.RangeCovered || c.UnpricedFacts > 0 || c.UnknownTokenFacts > 0 || c.UntimedFacts > 0 || c.PartialSessions > 0 || c.ConflictedSessions > 0 {
			c.State = "partial"
		}
	}
	return c
}
func (o *statisticsRead) trend() []statistics_vo.StatisticsDay {
	out := make([]statistics_vo.StatisticsDay, 0)
	for day := statisticsDay(o.q.StartAtMS, o.q.Location); day.UnixMilli() < o.q.EndAtMS; day = day.AddDate(0, 0, 1) {
		key := day.Format(time.DateOnly)
		g := o.days[key]
		if g == nil {
			g = newStatisticsAggregate()
		}
		// 收到其他日期的事实或 Provider 状态不能证明本日已观测为零。
		totals, _ := g.finish(false)
		out = append(out, statistics_vo.StatisticsDay{Date: key, StartAtMS: day.UnixMilli(), Totals: totals})
	}
	return out
}
func (o *statisticsRead) summary(now time.Time) statistics_vo.StatisticsSummary {
	totals, _ := o.total.finish(len(o.providerSeen) > 0)
	models := statisticsSlices(o.models, len(o.providerSeen) > 0)
	// Cursor 按整个请求范围一次舍入。模型构成单独舍入的差值明确列出，不能改动事实或报成另一模型。
	if totals.CostMicroUSD != nil {
		delta, _ := new(big.Int).SetString(*totals.CostMicroUSD, 10)
		for _, m := range models {
			if m.Totals.CostMicroUSD != nil {
				n, _ := new(big.Int).SetString(*m.Totals.CostMicroUSD, 10)
				delta.Sub(delta, n)
			}
		}
		if delta.Sign() != 0 {
			zero, _ := newStatisticsAggregate().finish(true)
			zero.CostMicroUSD = new(delta.String())
			models = append(models, statistics_vo.StatisticsSlice{Key: "rounding_adjustment", Name: "范围舍入差额", Totals: zero})
		}
	}
	trend := o.trend()
	hours := make([]statistics_vo.StatisticsHour, 0, 168)
	for weekday := range 7 {
		for hour := range 24 {
			key := time.Date(2023, 1, 1+weekday, hour, 0, 0, 0, time.UTC).Format("Mon-15")
			g := o.hours[key]
			if g == nil {
				g = newStatisticsAggregate()
			}
			t, _ := g.finish(false)
			hours = append(hours, statistics_vo.StatisticsHour{Weekday: weekday, Hour: hour, Tokens: t.TotalTokens, Sessions: t.Sessions})
		}
	}
	return statistics_vo.StatisticsSummary{Range: statisticsRange(o.q), Scope: o.scope(), CostBasis: "codex_day_model_version;cursor_range_sum;event_cost_sum", TrendCostRoundingDeltaMicroUSD: decimalDifference(totals.CostMicroUSD, trend), Totals: totals, Coverage: o.coverage(now), Providers: statisticsSlices(o.providers, len(o.providerSeen) > 0), Models: models, Devices: []statistics_vo.StatisticsSlice{{Key: "execution_unknown", Name: "执行设备未知", Totals: totals}}, Trend: trend, WeekdayHours: hours, Tools: statisticsSlices(o.tools, true), Skills: statisticsSlices(o.skills, true)}
}
func (s *Statistics) Summary(ctx context.Context, p access_dto.Principal, q statistics_dto.StatisticsQuery) (result statistics_vo.StatisticsSummary, err error) {
	if err = access_srv.RequireAdmin(p); err != nil {
		return
	}
	now := s.now()
	err = s.repository.Snapshot(ctx, func(ctx context.Context) error {
		current, err := s.read(ctx, q)
		if err != nil {
			return err
		}
		result = current.summary(now)
		// 热力图固定到当前自然日之前的连续 365 个自然日，与 KPI 范围独立。
		h := q
		local := now.In(q.Location)
		end := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, q.Location).AddDate(0, 0, 1)
		h.EndAtMS = end.UnixMilli()
		h.StartAtMS = end.AddDate(0, 0, -365).UnixMilli()
		annual, err := s.readFacts(ctx, h, false, true)
		if err != nil {
			return err
		}
		result.Heatmap = annual.trend()
		result.HeatmapRange = statisticsRange(h)
		result.HeatmapCoverage = annual.coverage(now)
		annualTotals, _ := annual.total.finish(len(annual.providerSeen) > 0)
		result.HeatmapTotals = annualTotals
		result.HeatmapActivity = statisticsActivity(result.Heatmap, annualTotals.TotalTokens)
		return nil
	})
	return
}
