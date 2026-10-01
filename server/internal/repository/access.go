package repository

import (
	"context"

	gormv2 "github.com/SisyphusSQ/codex-pulse/server/internal/lib/gorm"
	"github.com/SisyphusSQ/codex-pulse/server/internal/models/do"
	"gorm.io/gorm/clause"
)

type Access struct{ engine *gormv2.Engine }

func NewAccess(engine *gormv2.Engine) *Access { return &Access{engine: engine} }

func (r *Access) Transaction(ctx context.Context, fn func(context.Context) error) error {
	return r.engine.Transaction(ctx, fn)
}
func (r *Access) AddPairing(ctx context.Context, pairing do.Pairing) error {
	return r.engine.DB(ctx).Create(&pairing).Error
}
func (r *Access) Pairing(ctx context.Context, hash string) (do.Pairing, error) {
	var pairing do.Pairing
	err := r.engine.DB(ctx).Clauses(clause.Locking{Strength: "UPDATE"}).Where("code_hash = ?", hash).Take(&pairing).Error
	return pairing, err
}
func (r *Access) Consume(ctx context.Context, hash string, now int64) (bool, error) {
	result := r.engine.DB(ctx).Model(&do.Pairing{}).Where("code_hash = ? AND consumed_at_ms IS NULL AND expires_at_ms > ?", hash, now).Update("consumed_at_ms", now)
	return result.RowsAffected == 1, result.Error
}
func (r *Access) AddClient(ctx context.Context, client do.Client) error {
	return r.engine.DB(ctx).Create(&client).Error
}
func (r *Access) ClientBySecret(ctx context.Context, hash string) (do.Client, error) {
	var client do.Client
	err := r.engine.DB(ctx).Where("secret_hash = ?", hash).Take(&client).Error
	return client, err
}
func (r *Access) Clients(ctx context.Context) ([]do.Client, error) {
	var clients []do.Client
	err := r.engine.DB(ctx).Order("created_at_ms DESC, id").Limit(1000).Find(&clients).Error
	return clients, err
}
func (r *Access) Revoke(ctx context.Context, id string, now int64) (bool, error) {
	result := r.engine.DB(ctx).Model(&do.Client{}).Where("id = ?", id).Update("revoked_at_ms", now)
	return result.RowsAffected == 1, result.Error
}
