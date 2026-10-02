package statistics_srv

import (
	"context"
	"math/big"
	"slices"
	"strings"

	access_dto "github.com/SisyphusSQ/codex-pulse/server/internal/models/dto/access_dto"
	statistics_dto "github.com/SisyphusSQ/codex-pulse/server/internal/models/dto/statistics_dto"
	statistics_vo "github.com/SisyphusSQ/codex-pulse/server/internal/models/vo/statistics_vo"
	access_srv "github.com/SisyphusSQ/codex-pulse/server/internal/service/access_srv"
)

// Usage 与其他统计共用事实仲裁和价格计算；模型日桶与总量在同一快照读取。
func (s *Statistics) Usage(ctx context.Context, p access_dto.Principal, q statistics_dto.StatisticsQuery) (out statistics_vo.UsageResponse, err error) {
	if err = access_srv.RequireAdmin(p); err != nil {
		return
	}
	err = s.repository.Snapshot(ctx, func(ctx context.Context) error {
		read, err := s.readWithModelTrend(ctx, q, true)
		if err != nil {
			return err
		}
		known := len(read.providerSeen) > 0
		totals, _ := read.total.finish(known)
		out = statistics_vo.UsageResponse{Range: statisticsRange(q), Scope: read.scope(), Totals: totals, Coverage: read.coverage(s.now()), Models: []statistics_vo.UsageModel{}, ModelDays: []statistics_vo.UsageModelDay{}, Trend: read.trend(), Providers: statisticsSlices(read.providers, known), CursorPools: statisticsSlices(read.cursorPools, known)}
		cacheProvider := q.Provider
		if cacheProvider == "" && len(read.providerSeen) == 1 && read.providerSeen["codex"] {
			cacheProvider = "codex"
		}
		out.CacheHitRate = rangeCacheHitRate(read.total, cacheProvider)
		sum := new(big.Int)
		for key, group := range read.modelTotals {
			parts := strings.Split(key, "\x00")
			t, _ := group.finish(false)
			out.Models = append(out.Models, statistics_vo.UsageModel{Provider: parts[0], Model: parts[1], Totals: t, CacheHitRate: rangeCacheHitRate(group, parts[0])})
			if t.CostMicroUSD != nil {
				v, _ := new(big.Int).SetString(*t.CostMicroUSD, 10)
				sum.Add(sum, v)
			}
		}
		for key, group := range read.modelDays {
			parts := strings.Split(key, "\x00")
			t, _ := group.finish(false)
			out.ModelDays = append(out.ModelDays, statistics_vo.UsageModelDay{Provider: parts[0], Model: parts[1], Date: parts[2], Totals: t})
		}
		if totals.CostMicroUSD != nil {
			v, _ := new(big.Int).SetString(*totals.CostMicroUSD, 10)
			out.ModelCostRoundingDeltaMicroUSD = new(v.Sub(v, sum).String())
		}
		out.TrendCostRoundingDeltaMicroUSD = decimalDifference(totals.CostMicroUSD, out.Trend)
		slices.SortFunc(out.Models, func(a, b statistics_vo.UsageModel) int {
			return strings.Compare(a.Provider+"\x00"+a.Model, b.Provider+"\x00"+b.Model)
		})
		slices.SortFunc(out.ModelDays, func(a, b statistics_vo.UsageModelDay) int {
			return strings.Compare(a.Date+a.Provider+a.Model, b.Date+b.Provider+b.Model)
		})
		return nil
	})
	return
}
