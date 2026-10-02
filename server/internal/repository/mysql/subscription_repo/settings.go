package subscription_repo

import (
	"context"
	"errors"

	"gorm.io/gorm"

	gormv2 "github.com/SisyphusSQ/codex-pulse/server/internal/lib/gorm"
	reporting_do "github.com/SisyphusSQ/codex-pulse/server/internal/models/do/mysql/reporting_do"
	subscription_do "github.com/SisyphusSQ/codex-pulse/server/internal/models/do/mysql/subscription_do"
	"github.com/SisyphusSQ/codex-pulse/server/utils"
)

type Subscription struct{ engine *gormv2.Engine }

func NewSubscription(engine *gormv2.Engine) *Subscription { return &Subscription{engine: engine} }
func (r *Subscription) Transaction(ctx context.Context, fn func(context.Context) error) error {
	return r.engine.Transaction(ctx, fn)
}
func (r *Subscription) Account(ctx context.Context, key string) (a reporting_do.Account, err error) {
	err = r.engine.DB(ctx).Where("id = ?", key).Take(&a).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		err = utils.ErrNotFound
	}
	return
}
func (r *Subscription) Settings(ctx context.Context, key string) (s subscription_do.Settings, err error) {
	err = r.engine.DB(ctx).Where("account_key = ?", key).Take(&s).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return subscription_do.Settings{AccountKey: key, TimeZone: "Asia/Shanghai"}, nil
	}
	return
}
func (r *Subscription) Save(ctx context.Context, s subscription_do.Settings, expected int64) error {
	db := r.engine.DB(ctx)
	var result *gorm.DB
	if expected == 0 {
		result = db.Create(&s)
	} else {
		result = db.Model(&subscription_do.Settings{}).Where("account_key = ? AND revision = ?", s.AccountKey, expected).Updates(map[string]any{"revision": s.Revision, "alias": s.Alias, "manual_plan": s.ManualPlan, "date_kind": s.DateKind, "renewal_day": s.RenewalDay, "membership_date": s.MembershipDate, "time_zone": s.TimeZone, "updated_at_ms": s.UpdatedAtMS})
	}
	if errors.Is(result.Error, gorm.ErrDuplicatedKey) {
		return utils.ErrConflict
	}
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return utils.ErrConflict
	}
	return nil
}

func (r *Subscription) Snapshot(ctx context.Context, fn func(context.Context) error) error {
	return r.engine.ReadSnapshot(ctx, fn)
}
