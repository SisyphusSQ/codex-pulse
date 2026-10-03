package statistics_repo

import (
	"context"

	"gorm.io/gorm"

	gormv2 "github.com/SisyphusSQ/codex-pulse/server/internal/lib/gorm"
	access_do "github.com/SisyphusSQ/codex-pulse/server/internal/models/do/mysql/access_do"
	reporting_do "github.com/SisyphusSQ/codex-pulse/server/internal/models/do/mysql/reporting_do"
	statistics_dto "github.com/SisyphusSQ/codex-pulse/server/internal/models/dto/statistics_dto"
	"github.com/SisyphusSQ/codex-pulse/server/utils"
)

// Statistics 只读中心结构化事实；一次 Snapshot 的所有投影共享数据库事务。
type Statistics struct{ engine *gormv2.Engine }

func NewStatistics(engine *gormv2.Engine) *Statistics { return &Statistics{engine: engine} }
func (r *Statistics) Snapshot(ctx context.Context, fn func(context.Context) error) error {
	return r.engine.ReadSnapshot(ctx, fn)
}
func (r *Statistics) sessionQuery(ctx context.Context, q statistics_dto.StatisticsQuery) *gorm.DB {
	db := r.engine.DB(ctx).Table("pulse_sessions AS s").Select("s.*").Where("s.deleted = ?", false)
	if q.Provider != "" {
		db = db.Where("s.provider = ?", q.Provider)
	}
	if q.SessionKey != "" {
		db = db.Where("s.id = ?", q.SessionKey)
	}
	if q.ProjectID != "" && q.ClientID == "" {
		db = db.Joins("JOIN pulse_projects p ON p.id=s.project_id").Where("p.group_id = ?", q.ProjectID)
	}
	if q.ClientID != "" {
		db = db.Where("EXISTS (SELECT 1 FROM pulse_session_sources src WHERE src.session_key=s.id AND src.client_id=?)", q.ClientID)
	}
	return db
}
func (r *Statistics) Sessions(ctx context.Context, q statistics_dto.StatisticsQuery) (rows []reporting_do.Session, err error) {
	err = r.sessionQuery(ctx, q).Order("s.id").Limit(statistics_dto.MaximumStatisticsSessions + 1).Find(&rows).Error
	if len(rows) > statistics_dto.MaximumStatisticsSessions {
		return nil, utils.ErrRequestBudget
	}
	return
}
func (r *Statistics) Projects(ctx context.Context) (rows []reporting_do.Project, err error) {
	err = r.engine.DB(ctx).Order("id").Limit(statistics_dto.MaximumStatisticsProjects + 1).Find(&rows).Error
	if len(rows) > statistics_dto.MaximumStatisticsProjects {
		return nil, utils.ErrRequestBudget
	}
	return
}
func (r *Statistics) Clients(ctx context.Context) (rows []access_do.Client, err error) {
	err = r.engine.DB(ctx).Where("purpose = ?", "collector").Order("name,id").Limit(statistics_dto.MaximumStatisticsClients + 1).Find(&rows).Error
	if len(rows) > statistics_dto.MaximumStatisticsClients {
		return nil, utils.ErrRequestBudget
	}
	return
}
func (r *Statistics) Status(ctx context.Context, q statistics_dto.StatisticsQuery) (rows []reporting_do.DeviceStatus, err error) {
	db := r.engine.DB(ctx).Table("pulse_device_status AS st").Select("st.*, sy.sync_state, sy.sync_checked_at_ms, sy.full_sync_state").Joins("LEFT JOIN pulse_device_sync AS sy ON sy.client_id=st.client_id AND sy.provider=st.provider")
	if q.Provider != "" {
		db = db.Where("st.provider = ?", q.Provider)
	}
	if q.ClientID != "" {
		db = db.Where("st.client_id = ?", q.ClientID)
	}
	err = db.Order("st.client_id,st.provider").Limit(statistics_dto.MaximumStatisticsClients*3 + 1).Find(&rows).Error
	if len(rows) > statistics_dto.MaximumStatisticsClients*3 {
		return nil, utils.ErrRequestBudget
	}
	return
}

func (r *Statistics) StreamUsage(ctx context.Context, q statistics_dto.StatisticsQuery, visit func(reporting_do.Usage) error) error {
	db := r.engine.DB(ctx).Table("pulse_usage AS u").Select("u.session_key,u.contribution_id,u.position,u.observed_at_ms,u.model,u.input_tokens,u.cached_tokens,u.cache_write_tokens,u.output_tokens,u.reasoning_tokens,u.total_tokens,u.cost_micro_usd,u.reported_charge_micro_usd,u.pricing_version,u.pricing_mode,u.input_price,u.cached_price,u.cache_write_price,u.output_price,u.cost_status").Where("u.observed_at_ms >= ? AND u.observed_at_ms < ?", q.StartAtMS, q.EndAtMS).Where("u.session_key IN (?)", r.sessionQuery(ctx, q).Select("s.id"))
	rows, err := db.Rows()
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		if err := ctx.Err(); err != nil {
			return err
		}
		var row reporting_do.Usage
		if err := rows.Scan(&row.SessionKey, &row.ContributionID, &row.Position, &row.ObservedAtMS, &row.Model, &row.InputTokens, &row.CachedTokens, &row.CacheWriteTokens, &row.OutputTokens, &row.ReasoningTokens, &row.TotalTokens, &row.CostMicroUSD, &row.ReportedChargeMicroUSD, &row.PricingVersion, &row.PricingMode, &row.InputPrice, &row.CachedPrice, &row.CacheWritePrice, &row.OutputPrice, &row.CostStatus); err != nil {
			return err
		}
		if err := visit(row); err != nil {
			return err
		}
	}
	return rows.Err()
}
func (r *Statistics) StreamInvocations(ctx context.Context, q statistics_dto.StatisticsQuery, visit func(reporting_do.Invocation) error) error {
	db := r.engine.DB(ctx).Table("pulse_invocations AS i").Select("i.session_key,i.invocation_id,i.observed_at_ms,i.kind,i.tool_name,i.outcome,i.duration_ms").Where("i.observed_at_ms >= ? AND i.observed_at_ms < ?", q.StartAtMS, q.EndAtMS).Where("i.session_key IN (?)", r.sessionQuery(ctx, q).Select("s.id"))
	rows, err := db.Rows()
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		if err := ctx.Err(); err != nil {
			return err
		}
		var row reporting_do.Invocation
		if err := rows.Scan(&row.SessionKey, &row.InvocationID, &row.ObservedAtMS, &row.Kind, &row.ToolName, &row.Outcome, &row.DurationMS); err != nil {
			return err
		}
		if err := visit(row); err != nil {
			return err
		}
	}
	return rows.Err()
}
func (r *Statistics) StreamSources(ctx context.Context, q statistics_dto.StatisticsQuery, visit func(reporting_do.SessionSource) error) error {
	db := r.engine.DB(ctx).Where("session_key IN (?)", r.sessionQuery(ctx, q).Select("s.id"))
	if q.ClientID != "" {
		db = db.Where("client_id = ?", q.ClientID)
	}
	rows, err := db.Model(&reporting_do.SessionSource{}).Order("session_key,id").Rows()
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		if err := ctx.Err(); err != nil {
			return err
		}
		var row reporting_do.SessionSource
		if err := db.ScanRows(rows, &row); err != nil {
			return err
		}
		if err := visit(row); err != nil {
			return err
		}
	}
	return rows.Err()
}

func (r *Statistics) StreamUntimed(ctx context.Context, q statistics_dto.StatisticsQuery, visit func(reporting_do.Usage) error) error {
	db := r.engine.DB(ctx).Table("pulse_usage AS u").Select("u.session_key,u.contribution_id,u.model").Where("u.observed_at_ms IS NULL").Where("u.session_key IN (?)", r.sessionQuery(ctx, q).Select("s.id"))
	rows, err := db.Rows()
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		if err := ctx.Err(); err != nil {
			return err
		}
		var row reporting_do.Usage
		if err := rows.Scan(&row.SessionKey, &row.ContributionID, &row.Model); err != nil {
			return err
		}
		if err := visit(row); err != nil {
			return err
		}
	}
	return rows.Err()
}
