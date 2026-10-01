package quota_repo

import (
	"context"

	"gorm.io/gorm"

	gormv2 "github.com/SisyphusSQ/codex-pulse/server/internal/lib/gorm"
	access_do "github.com/SisyphusSQ/codex-pulse/server/internal/models/do/mysql/access_do"
	reporting_do "github.com/SisyphusSQ/codex-pulse/server/internal/models/do/mysql/reporting_do"
	quota_dto "github.com/SisyphusSQ/codex-pulse/server/internal/models/dto/quota_dto"
	"github.com/SisyphusSQ/codex-pulse/server/utils"
)

type Quota struct{ engine *gormv2.Engine }

func NewQuota(engine *gormv2.Engine) *Quota { return &Quota{engine: engine} }
func (r *Quota) Snapshot(ctx context.Context, fn func(context.Context) error) error {
	return r.engine.ReadSnapshot(ctx, fn)
}
func (r *Quota) query(ctx context.Context, q quota_dto.Query) *gorm.DB {
	db := r.engine.DB(ctx)
	if q.Provider != "" {
		db = db.Where("provider = ?", q.Provider)
	}
	if q.AccountKey != "" {
		db = db.Where("account_key = ?", q.AccountKey)
	}
	if q.ClientID != "" {
		db = db.Where("client_id = ?", q.ClientID)
	}
	return db
}
func (r *Quota) Observations(ctx context.Context, q quota_dto.Query) (out []reporting_do.QuotaObservation, err error) {
	err = r.query(ctx, q).Order("observed_at_ms,id").Limit(quota_dto.MaximumObservations + 1).Find(&out).Error
	if len(out) > quota_dto.MaximumObservations {
		return nil, utils.ErrRequestBudget
	}
	return
}
func (r *Quota) Credits(ctx context.Context, q quota_dto.Query) (out []reporting_do.ResetCredits, err error) {
	err = r.query(ctx, q).Order("observed_at_ms,id").Limit(quota_dto.MaximumObservations + 1).Find(&out).Error
	if len(out) > quota_dto.MaximumObservations {
		return nil, utils.ErrRequestBudget
	}
	return
}
func (r *Quota) Accounts(ctx context.Context, q quota_dto.Query) (out []reporting_do.Account, err error) {
	db := r.engine.DB(ctx)
	if q.Provider != "" {
		db = db.Where("provider = ?", q.Provider)
	}
	if q.AccountKey != "" {
		db = db.Where("id = ?", q.AccountKey)
	}
	if q.ClientID != "" {
		db = db.Where("id IN (SELECT account_key FROM pulse_account_bindings WHERE client_id = ?)", q.ClientID)
	}
	err = db.Order("provider,account_id").Limit(quota_dto.MaximumAccounts + 1).Find(&out).Error
	if len(out) > quota_dto.MaximumAccounts {
		return nil, utils.ErrRequestBudget
	}
	return
}
func (r *Quota) Clients(ctx context.Context) (out []access_do.Client, err error) {
	err = r.engine.DB(ctx).Where("purpose = ?", "collector").Limit(quota_dto.MaximumAccounts + 1).Find(&out).Error
	if len(out) > quota_dto.MaximumAccounts {
		return nil, utils.ErrRequestBudget
	}
	return
}
