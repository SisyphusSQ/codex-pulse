package statistics_srv

import (
	"context"
	"time"

	access_dto "github.com/SisyphusSQ/codex-pulse/server/internal/models/dto/access_dto"
	statistics_dto "github.com/SisyphusSQ/codex-pulse/server/internal/models/dto/statistics_dto"
	statistics_vo "github.com/SisyphusSQ/codex-pulse/server/internal/models/vo/statistics_vo"
	"github.com/SisyphusSQ/codex-pulse/server/internal/service/access_srv"
)

func (s *Statistics) annualQuery(q statistics_dto.StatisticsQuery) statistics_dto.StatisticsQuery {
	local := s.now().In(q.Location)
	end := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, q.Location).AddDate(0, 0, 1)
	q.EndAtMS = end.UnixMilli()
	q.StartAtMS = end.AddDate(0, 0, -365).UnixMilli()
	return q
}

func (s *Statistics) annual(ctx context.Context, q statistics_dto.StatisticsQuery) (out statistics_vo.AnnualStatistics, err error) {
	err = s.repository.Snapshot(ctx, func(ctx context.Context) error {
		read, err := s.readFacts(ctx, q, false, true)
		if err != nil {
			return err
		}
		totals, _ := read.total.finish(len(read.providerSeen) > 0)
		out = statistics_vo.AnnualStatistics{Heatmap: read.trend(), HeatmapRange: statisticsRange(q), HeatmapCoverage: read.coverage(s.now()), HeatmapTotals: totals}
		out.HeatmapActivity = statisticsActivity(out.Heatmap, totals.TotalTokens)
		return nil
	})
	return
}

// Annual 仅查询年度事实；KPI 的日期切换共享同一年度缓存。
func (s *Statistics) Annual(ctx context.Context, p access_dto.Principal, q statistics_dto.StatisticsQuery) (out statistics_vo.AnnualStatistics, err error) {
	if err = access_srv.RequireAdmin(p); err != nil {
		return
	}
	q = s.annualQuery(q)
	if s.cache == nil {
		return s.annual(ctx, q)
	}
	out, status, err := cachedProjection(ctx, s.cache, "annual", q, s.annual)
	out.Cache = status
	return out, err
}

func (s *Statistics) current(ctx context.Context, q statistics_dto.StatisticsQuery) (out statistics_vo.StatisticsSummary, err error) {
	err = s.repository.Snapshot(ctx, func(ctx context.Context) error {
		read, err := s.read(ctx, q)
		if err != nil {
			return err
		}
		out = read.summary(s.now())
		return nil
	})
	return
}

func (s *Statistics) currentCard(ctx context.Context, p access_dto.Principal, q statistics_dto.StatisticsQuery) (out statistics_vo.StatisticsSummary, err error) {
	if err = access_srv.RequireAdmin(p); err != nil {
		return
	}
	if s.cache == nil {
		return s.current(ctx, q)
	}
	out, status, err := cachedProjection(ctx, s.cache, "current", q, s.current)
	out.Cache = status
	return out, err
}

// Activity、Top 和 Breakdown 的缓存计算共享一个范围快照；各路由只返回本卡字段。
func (s *Statistics) Activity(ctx context.Context, p access_dto.Principal, q statistics_dto.StatisticsQuery) (statistics_vo.ActivityStatistics, error) {
	v, err := s.currentCard(ctx, p, q)
	return statistics_vo.ActivityStatistics{Cache: v.Cache, Range: v.Range, Coverage: v.Coverage, ActivityGranularity: v.ActivityGranularity, ActivityTimeline: v.ActivityTimeline, WeekdayHours: v.WeekdayHours}, err
}
func (s *Statistics) Top(ctx context.Context, p access_dto.Principal, q statistics_dto.StatisticsQuery) (statistics_vo.TopStatistics, error) {
	v, err := s.currentCard(ctx, p, q)
	return statistics_vo.TopStatistics{Cache: v.Cache, Range: v.Range, Coverage: v.Coverage, TopSessions: v.TopSessions}, err
}
func (s *Statistics) Breakdown(ctx context.Context, p access_dto.Principal, q statistics_dto.StatisticsQuery, models bool) (out statistics_vo.BreakdownStatistics, err error) {
	v, err := s.currentCard(ctx, p, q)
	out = statistics_vo.BreakdownStatistics{Cache: v.Cache, Range: v.Range, Coverage: v.Coverage}
	if models {
		out.Models = v.Models
	} else {
		out.Providers = v.Providers
	}
	return out, err
}

func (s *Statistics) Totals(ctx context.Context, p access_dto.Principal, q statistics_dto.StatisticsQuery) (statistics_vo.TotalsStatistics, error) {
	v, err := s.Usage(ctx, p, q)
	return statistics_vo.TotalsStatistics{Cache: v.Cache, Range: v.Range, Totals: v.Totals, Coverage: v.Coverage}, err
}
