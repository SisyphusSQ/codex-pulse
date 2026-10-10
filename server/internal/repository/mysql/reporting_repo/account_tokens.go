package reporting_repo

import (
	"context"

	"gorm.io/gorm/clause"

	reporting_do "github.com/SisyphusSQ/codex-pulse/server/internal/models/do/mysql/reporting_do"
)

// LockAccountTokenFacts 按调用方的全局 ID 顺序分块插入并锁定读回，保留既有事实供业务层核对。
func (r *Reporting) LockAccountTokenFacts(ctx context.Context, rows []reporting_do.AccountTokenFact) (current []reporting_do.AccountTokenFact, err error) {
	db := r.engine.DB(ctx)
	for start := 0; start < len(rows); start += 1000 {
		chunk := rows[start:min(start+1000, len(rows))]
		if err = db.Clauses(clause.OnConflict{DoNothing: true}).Create(&chunk).Error; err != nil {
			return nil, err
		}
		ids := make([]string, len(chunk))
		for i, row := range chunk {
			ids[i] = row.ID
		}
		var stored []reporting_do.AccountTokenFact
		if err = db.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id IN ?", ids).Order("id").Find(&stored).Error; err != nil {
			return nil, err
		}
		current = append(current, stored...)
	}
	return
}

func (r *Reporting) SaveAccountTokenSources(ctx context.Context, rows []reporting_do.AccountTokenSource) error {
	for start := 0; start < len(rows); start += 1000 {
		chunk := rows[start:min(start+1000, len(rows))]
		if err := r.engine.DB(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(&chunk).Error; err != nil {
			return err
		}
	}
	return nil
}

func (r *Reporting) SaveAccountTokenPeriod(ctx context.Context, row reporting_do.AccountTokenPeriod) error {
	// Period 的身份和起止不变，只推进真实采集时间；晚到批次不倒退。
	return r.engine.DB(ctx).Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "id"}}, DoUpdates: clause.Assignments(map[string]any{"collected_at_ms": clause.Expr{SQL: "CASE WHEN collected_at_ms < ? THEN ? ELSE collected_at_ms END", Vars: []any{row.CollectedAtMS, row.CollectedAtMS}}})}).Create(&row).Error
}
