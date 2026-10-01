package access_repo

import (
	"context"

	"gorm.io/gorm/clause"

	gormv2 "github.com/SisyphusSQ/codex-pulse/server/internal/lib/gorm"
	access_do "github.com/SisyphusSQ/codex-pulse/server/internal/models/do/mysql/access_do"
	"github.com/SisyphusSQ/codex-pulse/server/utils"
)

type Access struct{ engine *gormv2.Engine }

func NewAccess(engine *gormv2.Engine) *Access { return &Access{engine: engine} }

func (r *Access) Transaction(ctx context.Context, fn func(context.Context) error) error {
	return r.engine.Transaction(ctx, fn)
}
func (r *Access) AddPairing(ctx context.Context, pairing access_do.Pairing) error {
	return r.engine.DB(ctx).Create(&pairing).Error
}
func (r *Access) Pairing(ctx context.Context, hash string) (access_do.Pairing, error) {
	var pairing access_do.Pairing
	err := r.engine.DB(ctx).Clauses(clause.Locking{Strength: "UPDATE"}).Where("code_hash = ?", hash).Take(&pairing).Error
	return pairing, err
}
func (r *Access) Consume(ctx context.Context, hash string, now int64) (bool, error) {
	result := r.engine.DB(ctx).Model(&access_do.Pairing{}).Where("code_hash = ? AND consumed_at_ms IS NULL AND expires_at_ms > ?", hash, now).Update("consumed_at_ms", now)
	return result.RowsAffected == 1, result.Error
}
func (r *Access) AddClient(ctx context.Context, client access_do.Client) error {
	return r.engine.DB(ctx).Create(&client).Error
}
func (r *Access) ClientBySecret(ctx context.Context, hash string) (access_do.Client, error) {
	var client access_do.Client
	err := r.engine.DB(ctx).Where("secret_hash = ?", hash).Take(&client).Error
	return client, err
}
func (r *Access) Clients(ctx context.Context) ([]access_do.Client, error) {
	var clients []access_do.Client
	err := r.engine.DB(ctx).Order("created_at_ms DESC, id").Limit(1001).Find(&clients).Error
	if len(clients) > 1000 {
		return nil, utils.ErrRequestBudget
	}
	return clients, err
}

func (r *Access) Rename(ctx context.Context, id, name string) (bool, error) {
	result := r.engine.DB(ctx).Model(&access_do.Client{}).Where("id = ?", id).Update("name", name)
	if result.Error != nil || result.RowsAffected > 0 {
		return result.RowsAffected > 0, result.Error
	}
	// MySQL 相同名称可报告零变更；不能将已存在的客户端误报为不存在。
	var count int64
	err := r.engine.DB(ctx).Model(&access_do.Client{}).Where("id = ?", id).Count(&count).Error
	return count == 1, err
}
func (r *Access) Revoke(ctx context.Context, id string, now int64) (bool, error) {
	result := r.engine.DB(ctx).Model(&access_do.Client{}).Where("id = ?", id).Update("revoked_at_ms", now)
	return result.RowsAffected == 1, result.Error
}
