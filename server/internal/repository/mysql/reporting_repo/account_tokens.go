package reporting_repo

import (
	"context"

	"gorm.io/gorm/clause"

	reporting_do "github.com/SisyphusSQ/codex-pulse/server/internal/models/do/mysql/reporting_do"
)

func (r *Reporting) LockAccountTokenFact(ctx context.Context, row reporting_do.AccountTokenFact) (current reporting_do.AccountTokenFact, err error) {
	db := r.engine.DB(ctx)
	if err = db.Clauses(clause.OnConflict{DoNothing: true}).Create(&row).Error; err != nil {
		return
	}
	err = db.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", row.ID).Take(&current).Error
	return
}

func (r *Reporting) SaveAccountTokenSource(ctx context.Context, row reporting_do.AccountTokenSource) error {
	return r.engine.DB(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(&row).Error
}

func (r *Reporting) SaveAccountTokenPeriod(ctx context.Context, row reporting_do.AccountTokenPeriod) error {
	// Period 的身份和起止不变，只推进真实采集时间；晚到批次不倒退。
	return r.engine.DB(ctx).Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "id"}}, DoUpdates: clause.Assignments(map[string]any{"collected_at_ms": clause.Expr{SQL: "CASE WHEN collected_at_ms < ? THEN ? ELSE collected_at_ms END", Vars: []any{row.CollectedAtMS, row.CollectedAtMS}}})}).Create(&row).Error
}
