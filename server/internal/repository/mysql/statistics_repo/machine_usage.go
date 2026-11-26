package statistics_repo

import (
	"context"

	"gorm.io/gorm"

	reporting_do "github.com/SisyphusSQ/codex-pulse/server/internal/models/do/mysql/reporting_do"
	statistics_dto "github.com/SisyphusSQ/codex-pulse/server/internal/models/dto/statistics_dto"
	"github.com/SisyphusSQ/codex-pulse/server/internal/repository/mysql/reporting_repo"
	"github.com/SisyphusSQ/codex-pulse/server/utils"
)

func (r *Statistics) machineUsageSources(ctx context.Context, q statistics_dto.StatisticsQuery) *gorm.DB {
	db := r.engine.DB(ctx).Table("pulse_machine_usage_sources AS mus").
		Joins("JOIN pulse_sessions AS s ON s.id=mus.session_key AND s.deleted=0")
	if q.SessionKey != "" {
		db = db.Where("mus.session_key = ?", q.SessionKey)
	}
	if q.ClientID != "" {
		db = db.Where("mus.client_id = ?", q.ClientID)
	}
	return db
}

// MachineUsageSources 读取轻量覆盖信息，无时间事实和最近采集时间独立于日期范围。
func (r *Statistics) MachineUsageSources(ctx context.Context, q statistics_dto.StatisticsQuery) (sources []reporting_do.MachineUsageSource, err error) {
	err = r.machineUsageSources(ctx, q).Select("mus.*").Limit(statistics_dto.MaximumStatisticsSources + 1).Find(&sources).Error
	if len(sources) > statistics_dto.MaximumStatisticsSources {
		return nil, utils.ErrRequestBudget
	}
	return
}

// StreamMachineUsageSources 只打开时间范围与查询相交的入选来源正文。
func (r *Statistics) StreamMachineUsageSources(ctx context.Context, q statistics_dto.StatisticsQuery, visit func(reporting_do.SessionSource) error) error {
	db := r.machineUsageSources(ctx, q).
		Where("mus.last_observed_at_ms >= ? AND mus.first_observed_at_ms < ?", q.StartAtMS, q.EndAtMS).
		Joins("JOIN pulse_session_sources AS src ON src.id=mus.source_id")
	// SeekDB 宽范围也先筛轻量索引表，再按键读取正文和分片。
	const hint = "/*+ LEADING(mus s src owner part) USE_NL(s src owner part) */ "
	return reporting_repo.StreamSourcePayloads(ctx, db, hint, statistics_dto.MaximumStatisticsSources, visit)
}
