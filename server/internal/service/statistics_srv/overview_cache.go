package statistics_srv

import (
	"context"
	"encoding/json/v2"

	statistics_dto "github.com/SisyphusSQ/codex-pulse/server/internal/models/dto/statistics_dto"
	"github.com/SisyphusSQ/codex-pulse/server/utils"
)

// 同范围两卡均到期时共读事实；模型日桶与展示来源的失败不互相传播。
func (s *Statistics) overviewLoader(q statistics_dto.StatisticsQuery) overviewLoader {
	return func(ctx context.Context) map[string]statisticsCacheResult {
		results := make(map[string]statisticsCacheResult, 2)
		err := s.repository.Snapshot(ctx, func(ctx context.Context) error {
			read, err := s.readProjection(ctx, q, true, false, "overview")
			if err != nil {
				return err
			}
			now := s.now()
			usage := statisticsCacheResult{err: utils.ErrRequestBudget}
			if !read.modelTrendExceeded {
				usage.body, usage.err = json.Marshal(read.usageView(now))
			}
			results["usage"] = usage
			current := read.summary(now)
			result := statisticsCacheResult{err: s.attachPageSources(ctx, q, current.TopSessions, read)}
			if result.err == nil {
				result.body, result.err = json.Marshal(current)
			}
			results["current"] = result
			return nil
		})
		if err != nil {
			results["usage"], results["current"] = statisticsCacheResult{err: err}, statisticsCacheResult{err: err}
		}
		return results
	}
}
