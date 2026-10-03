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

// quotaWindowsQuery 在数据库分组前排除未归因和套餐之外的 Codex 窗口。
func (r *Quota) quotaWindowsQuery(ctx context.Context, q quota_dto.Query) *gorm.DB {
	db := r.query(ctx, q)
	if q.RawHistory {
		return db
	}
	return db.Where("account_key IS NOT NULL").Where(`provider <> 'codex' OR (limit_id = 'codex' AND EXISTS (
 SELECT 1 FROM pulse_accounts a WHERE a.id = account_key AND a.provider = 'codex' AND (
 (LOWER(a.plan) IN ('pro','prolite','pro5x','pro20x') AND window_minutes = 10080) OR
 (LOWER(a.plan) = 'plus' AND window_minutes IN (300,10080)))))`)
}
func (r *Quota) Observations(ctx context.Context, q quota_dto.Query) (out []reporting_do.QuotaObservation, err error) {
	err = r.quotaWindowsQuery(ctx, q).Order("observed_at_ms,id").Limit(quota_dto.MaximumObservations + 1).Find(&out).Error
	if len(out) > quota_dto.MaximumObservations {
		return nil, utils.ErrRequestBudget
	}
	return
}
func (r *Quota) Credits(ctx context.Context, q quota_dto.Query, latestAtMS int64) (out []reporting_do.ResetCredits, err error) {
	base := r.query(ctx, q).Where("account_key IS NOT NULL").Model(&reporting_do.ResetCredits{}).Select("*,DENSE_RANK() OVER (PARTITION BY provider,account_key,CASE WHEN account_key IS NULL THEN client_id ELSE '' END,CASE WHEN account_key IS NULL THEN local_scope ELSE '' END,CASE WHEN inventory IS NOT NULL AND status IN ('accepted','fresh','stale') AND observed_at_ms <= ? THEN 1 ELSE 0 END ORDER BY observed_at_ms DESC) AS recency", latestAtMS)
	err = r.engine.DB(ctx).Table("(?) AS credits", base).Where("recency <= 2").Order("observed_at_ms,id").Find(&out).Error
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
