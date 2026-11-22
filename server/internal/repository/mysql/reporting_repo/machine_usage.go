package reporting_repo

import (
	"context"

	"gorm.io/gorm/clause"

	reporting_do "github.com/SisyphusSQ/codex-pulse/server/internal/models/do/mysql/reporting_do"
)

// SaveMachineUsageSource 与来源快照同事务切换入选来源；同机器全部删除后移除投影。
func (r *Reporting) SaveMachineUsageSource(ctx context.Context, sessionKey, clientID string, source *reporting_do.MachineUsageSource) error {
	db := r.engine.DB(ctx)
	if source == nil {
		return db.Where("session_key = ? AND client_id = ?", sessionKey, clientID).Delete(&reporting_do.MachineUsageSource{}).Error
	}
	return db.Clauses(clause.OnConflict{UpdateAll: true}).Create(source).Error
}
