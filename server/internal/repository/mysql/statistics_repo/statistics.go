package statistics_repo

import (
	"context"
	"database/sql"

	"gorm.io/gorm"

	gormv2 "github.com/SisyphusSQ/codex-pulse/server/internal/lib/gorm"
	access_do "github.com/SisyphusSQ/codex-pulse/server/internal/models/do/mysql/access_do"
	reporting_do "github.com/SisyphusSQ/codex-pulse/server/internal/models/do/mysql/reporting_do"
	statistics_dto "github.com/SisyphusSQ/codex-pulse/server/internal/models/dto/statistics_dto"
	reporting_repo "github.com/SisyphusSQ/codex-pulse/server/internal/repository/mysql/reporting_repo"
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
	err = db.Order("st.client_id,st.provider").Limit(statistics_dto.MaximumStatisticsClients*4 + 1).Find(&rows).Error
	if len(rows) > statistics_dto.MaximumStatisticsClients*4 {
		return nil, utils.ErrRequestBudget
	}
	return
}

func (r *Statistics) StreamUsage(ctx context.Context, q statistics_dto.StatisticsQuery, visit func(reporting_do.Usage) error) error {
	return r.streamUsage(ctx, q, false, visit)
}

// StreamHeatmapUsage 的年度范围使用哈希连接，避免 SeekDB 对宽范围合并连接超过查询期限。
// 固定 hint 不改变筛选或事实；其他数据库可忽略该注释，窄范围查询保留原有计划。
func (r *Statistics) StreamHeatmapUsage(ctx context.Context, q statistics_dto.StatisticsQuery, visit func(reporting_do.Usage) error) error {
	return r.streamUsage(ctx, q, true, visit)
}

func (r *Statistics) usageFilter(ctx context.Context, q statistics_dto.StatisticsQuery, db *gorm.DB) *gorm.DB {
	if q.MatchedSessionKeys != nil {
		db = db.Where("u.session_key IN ?", q.MatchedSessionKeys)
	} else {
		db = db.Where("u.session_key IN (?)", r.sessionQuery(ctx, q).Select("s.id"))
	}
	if q.Model == "unknown" {
		db = db.Where("(u.model IS NULL OR u.model = '' OR u.model = ?)", q.Model)
	} else if q.Model != "" {
		db = db.Where("u.model = ?", q.Model)
	}
	return db
}

func (r *Statistics) streamUsage(ctx context.Context, q statistics_dto.StatisticsQuery, heatmap bool, visit func(reporting_do.Usage) error) error {
	columns := "u.session_key,u.contribution_id,u.position,u.observed_at_ms,u.model,u.input_tokens,u.cached_tokens,u.cache_write_tokens,u.output_tokens,u.reasoning_tokens,u.total_tokens,u.cost_micro_usd,u.reported_charge_micro_usd,u.pricing_version,u.pricing_mode,u.input_price,u.cached_price,u.cache_write_price,u.output_price,u.cost_status"
	if heatmap {
		columns = "/*+ USE_HASH(u) */ " + columns
	}
	for _, physical := range []string{"pulse_usage AS u", "pulse_usage_versions AS u"} {
		base := r.engine.DB(ctx).Table(physical)
		if physical == "pulse_usage AS u" {
			base = base.Where("NOT EXISTS (SELECT 1 FROM pulse_session_publications p WHERE p.session_key=u.session_key)")
		} else {
			base = base.Joins("JOIN pulse_session_publications p ON p.session_key=u.session_key AND p.generation=u.generation")
		}
		db := r.usageFilter(ctx, q, base.Select(columns).Where("u.observed_at_ms >= ? AND u.observed_at_ms < ?", q.StartAtMS, q.EndAtMS))
		rows, err := db.Rows()
		if err != nil {
			return err
		}

		for rows.Next() {
			if err := ctx.Err(); err != nil {
				rows.Close()
				return err
			}
			var row reporting_do.Usage
			if err := rows.Scan(&row.SessionKey, &row.ContributionID, &row.Position, &row.ObservedAtMS, &row.Model, &row.InputTokens, &row.CachedTokens, &row.CacheWriteTokens, &row.OutputTokens, &row.ReasoningTokens, &row.TotalTokens, &row.CostMicroUSD, &row.ReportedChargeMicroUSD, &row.PricingVersion, &row.PricingMode, &row.InputPrice, &row.CachedPrice, &row.CacheWritePrice, &row.OutputPrice, &row.CostStatus); err != nil {
				rows.Close()
				return err
			}
			if err := visit(row); err != nil {
				rows.Close()
				return err
			}
		}
		err = rows.Err()
		closeErr := rows.Close()
		if err != nil {
			return err
		}
		if closeErr != nil {
			return closeErr
		}
	}
	return nil
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

// StreamSources 在同一有序流中读取内联来源及已发布分片；分片校验不再查询数据库。
func (r *Statistics) StreamSources(ctx context.Context, q statistics_dto.StatisticsQuery, visit func(reporting_do.SessionSource) error) error {
	db := r.engine.DB(ctx).Table("pulse_session_sources AS src").
		Select("src.id,src.session_key,src.client_id,src.home_id,src.source_kind,src.revision,src.collected_at_ms,src.digest,src.payload,part.part_index,part.body").
		Joins("LEFT JOIN pulse_payload_owners owner ON owner.id=CONCAT('source:',src.id)").
		Joins("LEFT JOIN pulse_payload_parts part ON part.payload_key=owner.payload_key").
		Where("src.session_key IN (?)", r.sessionQuery(ctx, q).Select("s.id"))
	if q.ClientID != "" {
		db = db.Where("src.client_id = ?", q.ClientID)
	}
	rows, err := db.Order("src.session_key,src.id,part.part_index").Rows()
	if err != nil {
		return err
	}
	defer rows.Close()
	var current reporting_do.SessionSource
	var parts []reporting_do.PayloadPart
	count := 0
	flush := func() error {
		if current.ID == "" {
			return nil
		}
		payload, err := reporting_repo.ResolvePayloadParts(current.Payload, parts)
		if err != nil {
			return err
		}
		current.Payload = payload
		return visit(current)
	}
	for rows.Next() {
		if err := ctx.Err(); err != nil {
			return err
		}
		var row reporting_do.SessionSource
		var index sql.NullInt64
		var body []byte
		if err := rows.Scan(&row.ID, &row.SessionKey, &row.ClientID, &row.HomeID, &row.SourceKind, &row.Revision, &row.CollectedAtMS, &row.Digest, &row.Payload, &index, &body); err != nil {
			return err
		}
		if row.ID != current.ID {
			if err := flush(); err != nil {
				return err
			}
			count++
			if count > statistics_dto.MaximumStatisticsSources {
				return utils.ErrRequestBudget
			}
			current = row
			parts = nil
		}
		if index.Valid {
			if len(parts) >= 257 {
				return utils.ErrRequestBudget
			}
			parts = append(parts, reporting_do.PayloadPart{PartIndex: int(index.Int64), Body: body})
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	return flush()
}

func (r *Statistics) StreamUntimed(ctx context.Context, q statistics_dto.StatisticsQuery, visit func(reporting_do.Usage) error) error {
	db := r.usageFilter(ctx, q, r.engine.DB(ctx).Table(reporting_repo.PublishedUsage).Select("u.session_key,u.contribution_id,u.model").Where("u.observed_at_ms IS NULL"))
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
