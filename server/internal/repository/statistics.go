package repository

import (
	"context"

	"gorm.io/gorm"

	gormv2 "github.com/SisyphusSQ/codex-pulse/server/internal/lib/gorm"
	"github.com/SisyphusSQ/codex-pulse/server/internal/models/do"
	"github.com/SisyphusSQ/codex-pulse/server/internal/models/dto"
	"github.com/SisyphusSQ/codex-pulse/server/utils"
)

// Statistics 只读中心结构化事实；一次 Snapshot 的所有投影共享数据库事务。
type Statistics struct{ engine *gormv2.Engine }

func NewStatistics(engine *gormv2.Engine) *Statistics { return &Statistics{engine: engine} }
func (r *Statistics) Snapshot(ctx context.Context, fn func(context.Context) error) error {
	return r.engine.ReadSnapshot(ctx, fn)
}
func (r *Statistics) sessionQuery(ctx context.Context, q dto.StatisticsQuery) *gorm.DB {
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
func (r *Statistics) Sessions(ctx context.Context, q dto.StatisticsQuery) (rows []do.Session, err error) {
	err = r.sessionQuery(ctx, q).Order("s.id").Limit(dto.MaximumStatisticsSessions + 1).Find(&rows).Error
	if len(rows) > dto.MaximumStatisticsSessions {
		return nil, utils.ErrRequestBudget
	}
	return
}
func (r *Statistics) Projects(ctx context.Context) (rows []do.Project, err error) {
	err = r.engine.DB(ctx).Order("id").Limit(dto.MaximumStatisticsProjects + 1).Find(&rows).Error
	if len(rows) > dto.MaximumStatisticsProjects {
		return nil, utils.ErrRequestBudget
	}
	return
}
func (r *Statistics) Clients(ctx context.Context) (rows []do.Client, err error) {
	err = r.engine.DB(ctx).Where("purpose = ?", "collector").Order("name,id").Limit(dto.MaximumStatisticsClients + 1).Find(&rows).Error
	if len(rows) > dto.MaximumStatisticsClients {
		return nil, utils.ErrRequestBudget
	}
	return
}
func (r *Statistics) Status(ctx context.Context, q dto.StatisticsQuery) (rows []do.DeviceStatus, err error) {
	db := r.engine.DB(ctx)
	if q.Provider != "" {
		db = db.Where("provider = ?", q.Provider)
	}
	if q.ClientID != "" {
		db = db.Where("client_id = ?", q.ClientID)
	}
	err = db.Order("client_id,provider").Limit(dto.MaximumStatisticsClients*3 + 1).Find(&rows).Error
	if len(rows) > dto.MaximumStatisticsClients*3 {
		return nil, utils.ErrRequestBudget
	}
	return
}

func (r *Statistics) StreamUsage(ctx context.Context, q dto.StatisticsQuery, visit func(do.Usage) error) error {
	db := r.engine.DB(ctx).Table("pulse_usage AS u").Select("u.*").Where("u.observed_at_ms >= ? AND u.observed_at_ms < ?", q.StartAtMS, q.EndAtMS).Where("u.session_key IN (?)", r.sessionQuery(ctx, q).Select("s.id"))
	rows, err := db.Order("u.session_key,u.position").Rows()
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		if err := ctx.Err(); err != nil {
			return err
		}
		var row do.Usage
		if err := db.ScanRows(rows, &row); err != nil {
			return err
		}
		if err := visit(row); err != nil {
			return err
		}
	}
	return rows.Err()
}
func (r *Statistics) StreamInvocations(ctx context.Context, q dto.StatisticsQuery, visit func(do.Invocation) error) error {
	db := r.engine.DB(ctx).Table("pulse_invocations AS i").Select("i.*").Where("i.observed_at_ms >= ? AND i.observed_at_ms < ?", q.StartAtMS, q.EndAtMS).Where("i.session_key IN (?)", r.sessionQuery(ctx, q).Select("s.id"))
	rows, err := db.Order("i.session_key,i.observed_at_ms,i.invocation_id").Rows()
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		if err := ctx.Err(); err != nil {
			return err
		}
		var row do.Invocation
		if err := db.ScanRows(rows, &row); err != nil {
			return err
		}
		if err := visit(row); err != nil {
			return err
		}
	}
	return rows.Err()
}
func (r *Statistics) StreamSources(ctx context.Context, q dto.StatisticsQuery, visit func(do.SessionSource) error) error {
	db := r.engine.DB(ctx).Where("session_key IN (?)", r.sessionQuery(ctx, q).Select("s.id"))
	if q.ClientID != "" {
		db = db.Where("client_id = ?", q.ClientID)
	}
	rows, err := db.Model(&do.SessionSource{}).Order("session_key,id").Rows()
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		if err := ctx.Err(); err != nil {
			return err
		}
		var row do.SessionSource
		if err := db.ScanRows(rows, &row); err != nil {
			return err
		}
		if err := visit(row); err != nil {
			return err
		}
	}
	return rows.Err()
}

func (r *Statistics) StreamUntimed(ctx context.Context, q dto.StatisticsQuery, visit func(do.Usage) error) error {
	db := r.engine.DB(ctx).Table("pulse_usage AS u").Select("u.*").Where("u.observed_at_ms IS NULL").Where("u.session_key IN (?)", r.sessionQuery(ctx, q).Select("s.id"))
	rows, err := db.Order("u.session_key,u.position").Rows()
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		if err := ctx.Err(); err != nil {
			return err
		}
		var row do.Usage
		if err := db.ScanRows(rows, &row); err != nil {
			return err
		}
		if err := visit(row); err != nil {
			return err
		}
	}
	return rows.Err()
}
