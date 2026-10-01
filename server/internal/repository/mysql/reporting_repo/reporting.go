package reporting_repo

import (
	"context"

	"gorm.io/gorm/clause"

	gormv2 "github.com/SisyphusSQ/codex-pulse/server/internal/lib/gorm"
	access_do "github.com/SisyphusSQ/codex-pulse/server/internal/models/do/mysql/access_do"
	reporting_do "github.com/SisyphusSQ/codex-pulse/server/internal/models/do/mysql/reporting_do"
)

// Reporting 在同一个 Engine/事务内存储来源、幂等确认与中心投影。
type Reporting struct{ engine *gormv2.Engine }

func NewReporting(engine *gormv2.Engine) *Reporting { return &Reporting{engine: engine} }
func (r *Reporting) Transaction(ctx context.Context, fn func(context.Context) error) error {
	return r.engine.Transaction(ctx, fn)
}
func (r *Reporting) LockClient(ctx context.Context, id string) (client access_do.Client, err error) {
	err = r.engine.DB(ctx).Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", id).Take(&client).Error
	return
}
func (r *Reporting) Batch(ctx context.Context, clientID, batchID string) (batch reporting_do.Batch, err error) {
	err = r.engine.DB(ctx).Where("client_id = ? AND batch_id = ?", clientID, batchID).Take(&batch).Error
	return
}
func (r *Reporting) CommitBatch(ctx context.Context, batch reporting_do.Batch) error {
	if err := r.engine.DB(ctx).Create(&batch).Error; err != nil {
		return err
	}
	return r.engine.DB(ctx).Model(&access_do.Client{}).Where("id = ?", batch.ClientID).Update("last_received_at_ms", batch.ReceivedAtMS).Error
}
func (r *Reporting) Sync(ctx context.Context, clientID string) (client access_do.Client, batches int64, err error) {
	if err = r.engine.DB(ctx).Where("id = ?", clientID).Take(&client).Error; err != nil {
		return
	}
	err = r.engine.DB(ctx).Model(&reporting_do.Batch{}).Where("client_id = ?", clientID).Count(&batches).Error
	return
}

// LockSession 建立并锁定仲裁 owner。各批次按 session key 排序锁定，避免交叉设备
// 同时创建或更新多个会话时以不同顺序取得锁；来源写入只能在此后进行。
func (r *Reporting) LockSession(ctx context.Context, row reporting_do.Session) (current reporting_do.Session, err error) {
	if err = r.engine.DB(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(&row).Error; err != nil {
		return
	}
	err = r.engine.DB(ctx).Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", row.ID).Take(&current).Error
	return
}
func (r *Reporting) Source(ctx context.Context, id string) (source reporting_do.SessionSource, err error) {
	err = r.engine.DB(ctx).Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", id).Take(&source).Error
	return
}
func (r *Reporting) SaveSource(ctx context.Context, source reporting_do.SessionSource) error {
	return r.engine.DB(ctx).Save(&source).Error
}
func (r *Reporting) Sources(ctx context.Context, sessionKey string) (sources []reporting_do.SessionSource, err error) {
	// Locking reads also see the latest committed versions under MySQL repeatable-read.
	err = r.engine.DB(ctx).Clauses(clause.Locking{Strength: "UPDATE"}).Where("session_key = ?", sessionKey).Order("id").Limit(129).Find(&sources).Error
	return
}
func (r *Reporting) Canonical(ctx context.Context, key string) (row reporting_do.CanonicalSnapshot, err error) {
	err = r.engine.DB(ctx).Where("session_key = ?", key).Take(&row).Error
	return
}
func (r *Reporting) SaveCanonical(ctx context.Context, session reporting_do.Session, payload reporting_do.CanonicalSnapshot, usage []reporting_do.Usage, invocations []reporting_do.Invocation) error {
	db := r.engine.DB(ctx)
	if err := db.Save(&session).Error; err != nil {
		return err
	}
	if err := db.Save(&payload).Error; err != nil {
		return err
	}
	if err := db.Where("session_key = ?", session.ID).Delete(&reporting_do.Usage{}).Error; err != nil {
		return err
	}
	if err := db.Where("session_key = ?", session.ID).Delete(&reporting_do.Invocation{}).Error; err != nil {
		return err
	}
	if len(usage) > 0 {
		if err := db.CreateInBatches(usage, 250).Error; err != nil {
			return err
		}
	}
	if len(invocations) > 0 {
		if err := db.CreateInBatches(invocations, 250).Error; err != nil {
			return err
		}
	}
	return nil
}
func (r *Reporting) SaveSessionFlags(ctx context.Context, row reporting_do.Session) error {
	return r.engine.DB(ctx).Save(&row).Error
}
func (r *Reporting) Project(ctx context.Context, id string) (row reporting_do.Project, err error) {
	err = r.engine.DB(ctx).Where("id = ?", id).Take(&row).Error
	return
}
func (r *Reporting) SaveProject(ctx context.Context, row reporting_do.Project) error {
	// 来源更新不能覆盖管理端显式 group_id；只更新本来源元数据。
	return r.engine.DB(ctx).Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "id"}}, DoUpdates: clause.AssignmentColumns([]string{"name", "updated_at_ms"})}).Create(&row).Error
}
func (r *Reporting) Account(ctx context.Context, key string) (row reporting_do.Account, err error) {
	err = r.engine.DB(ctx).Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", key).Take(&row).Error
	return
}
func (r *Reporting) SaveAccount(ctx context.Context, row reporting_do.Account) error {
	return r.engine.DB(ctx).Save(&row).Error
}
func (r *Reporting) Binding(ctx context.Context, key string) (row reporting_do.AccountBinding, err error) {
	err = r.engine.DB(ctx).Where("id = ?", key).Take(&row).Error
	return
}
func (r *Reporting) SaveBinding(ctx context.Context, row reporting_do.AccountBinding) error {
	db := r.engine.DB(ctx)
	if err := db.Save(&row).Error; err != nil {
		return err
	}
	// HMAC-only records can be attached only through the same collector/scope proof.
	// Legacy unassigned history needs an explicit upstream linked-history export.
	if err := db.Model(&reporting_do.QuotaObservation{}).Where("client_id = ? AND provider = ? AND local_scope = ? AND account_key IS NULL AND history_origin IN ?", row.ClientID, row.Provider, row.LocalScope, []string{"pending_association", "confirmed"}).Updates(map[string]any{"account_key": row.AccountKey, "history_origin": "confirmed"}).Error; err != nil {
		return err
	}
	return db.Model(&reporting_do.ResetCredits{}).Where("client_id = ? AND provider = ? AND local_scope = ? AND account_key IS NULL", row.ClientID, row.Provider, row.LocalScope).Update("account_key", row.AccountKey).Error
}
func (r *Reporting) Observation(ctx context.Context, id string) (row reporting_do.QuotaObservation, err error) {
	err = r.engine.DB(ctx).Where("id = ?", id).Take(&row).Error
	return
}
func (r *Reporting) SaveObservation(ctx context.Context, row reporting_do.QuotaObservation) error {
	return r.engine.DB(ctx).Save(&row).Error
}
func (r *Reporting) Credits(ctx context.Context, id string) (row reporting_do.ResetCredits, err error) {
	err = r.engine.DB(ctx).Where("id = ?", id).Take(&row).Error
	return
}
func (r *Reporting) SaveCredits(ctx context.Context, row reporting_do.ResetCredits) error {
	return r.engine.DB(ctx).Save(&row).Error
}
func (r *Reporting) DeviceStatus(ctx context.Context, client, provider string) (row reporting_do.DeviceStatus, err error) {
	err = r.engine.DB(ctx).Where("client_id = ? AND provider = ?", client, provider).Take(&row).Error
	return
}
func (r *Reporting) SaveStatus(ctx context.Context, row reporting_do.DeviceStatus) error {
	return r.engine.DB(ctx).Save(&row).Error
}

func (r *Reporting) EnsureAccount(ctx context.Context, row reporting_do.Account) error {
	return r.engine.DB(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(&row).Error
}

func (r *Reporting) LockProjects(ctx context.Context, ids []string) (rows []reporting_do.Project, err error) {
	err = r.engine.DB(ctx).Clauses(clause.Locking{Strength: "UPDATE"}).Where("id IN ?", ids).Order("id").Find(&rows).Error
	return
}
func (r *Reporting) AssociateProject(ctx context.Context, id, groupID string) error {
	return r.engine.DB(ctx).Model(&reporting_do.Project{}).Where("id = ?", id).Update("group_id", groupID).Error
}
