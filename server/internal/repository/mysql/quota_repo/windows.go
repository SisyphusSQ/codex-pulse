package quota_repo

import (
	"context"
	"fmt"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	access_do "github.com/SisyphusSQ/codex-pulse/server/internal/models/do/mysql/access_do"
	reporting_do "github.com/SisyphusSQ/codex-pulse/server/internal/models/do/mysql/reporting_do"
	quota_dto "github.com/SisyphusSQ/codex-pulse/server/internal/models/dto/quota_dto"
	"github.com/SisyphusSQ/codex-pulse/server/utils"
)

func (r *Quota) Windows(ctx context.Context, q quota_dto.Query) (out []quota_dto.WindowScope, err error) {
	db := r.quotaWindowsQuery(ctx, q).Model(&reporting_do.QuotaObservation{})
	err = db.Select("provider,account_key,CASE WHEN account_key IS NULL THEN client_id ELSE '' END AS client_id,CASE WHEN account_key IS NULL THEN local_scope ELSE '' END AS local_scope,limit_id,window_kind,window_minutes").Distinct().Limit(4097).Scan(&out).Error
	if len(out) > 4096 {
		return nil, utils.ErrRequestBudget
	}
	return
}

func (r *Quota) windowQuery(ctx context.Context, q quota_dto.Query, w quota_dto.WindowScope) *gorm.DB {
	db := r.quotaWindowsQuery(ctx, q).Model(&reporting_do.QuotaObservation{}).Where("provider = ? AND limit_id = ? AND window_kind = ?", w.Provider, w.LimitID, w.WindowKind)
	if w.AccountKey != nil {
		db = db.Where("account_key = ?", *w.AccountKey)
	} else {
		db = db.Where("account_key IS NULL AND client_id = ? AND local_scope = ?", w.ClientID, w.LocalScope)
	}
	if w.WindowMinutes != nil {
		db = db.Where("window_minutes = ?", *w.WindowMinutes)
	} else {
		db = db.Where("window_minutes IS NULL")
	}
	return db
}

// Headers 读取每种 reset/source 的实际首末与非零证据，供共享仲裁规则选择周期。
func (r *Quota) Headers(ctx context.Context, q quota_dto.Query, w quota_dto.WindowScope, nowMS, clockSkewMS int64) (out []reporting_do.QuotaObservation, err error) {
	// 不让未来时间或无效 reset 排挤最后有效值；零值与正用量的首末保留锚点转变。
	partition := "client_id,local_scope,source,history_origin,validity,resets_at_ms,CASE WHEN used_percent > 0 THEN 1 ELSE 0 END,CASE WHEN observed_at_ms <= ? AND resets_at_ms > observed_at_ms AND resets_at_ms-observed_at_ms <= window_minutes*60000+? THEN 1 ELSE 0 END"
	selectSQL := fmt.Sprintf("*,ROW_NUMBER() OVER (PARTITION BY %s ORDER BY observed_at_ms,id) AS first_rank,ROW_NUMBER() OVER (PARTITION BY %s ORDER BY observed_at_ms DESC,id DESC) AS last_rank", partition, partition)
	base := r.windowQuery(ctx, q, w).Select(selectSQL, nowMS+clockSkewMS, clockSkewMS, nowMS+clockSkewMS, clockSkewMS)
	err = r.engine.DB(ctx).Table("(?) AS headers", base).Where("first_rank=1 OR last_rank=1").Order("observed_at_ms,id").Limit(100001).Find(&out).Error
	if len(out) > 100000 {
		return nil, utils.ErrRequestBudget
	}
	return
}

func (r *Quota) WindowObservations(ctx context.Context, q quota_dto.Query, w quota_dto.WindowScope, resets []int64) (out []reporting_do.QuotaObservation, err error) {
	db := r.windowQuery(ctx, q, w)
	if len(resets) > 0 {
		recent := r.windowQuery(ctx, q, w).Select("id").Order("observed_at_ms DESC,id DESC").Limit(100)
		recent = r.engine.DB(ctx).Table("(?) AS recent", recent).Select("id")
		db = db.Where("resets_at_ms IN ? OR id IN (?)", resets, recent)
	} else {
		db = db.Order("observed_at_ms DESC,id DESC").Limit(100)
	}
	err = db.Order("observed_at_ms,id").Find(&out).Error
	return
}

func (r *Quota) AllWindowObservations(ctx context.Context, w quota_dto.WindowScope) (out []reporting_do.QuotaObservation, err error) {
	err = r.windowQuery(ctx, quota_dto.Query{RawHistory: true}, w).Order("observed_at_ms,id").Find(&out).Error
	return
}

func (r *Quota) Transaction(ctx context.Context, fn func(context.Context) error) error {
	return r.engine.Transaction(ctx, fn)
}

func (r *Quota) Retire(ctx context.Context, rows []reporting_do.RetiredObservation) error {
	if len(rows) == 0 {
		return nil
	}
	ids := make([]string, 0, len(rows))
	for _, row := range rows {
		ids = append(ids, row.ID)
	}
	var clients []string
	if err := r.engine.DB(ctx).Model(&reporting_do.QuotaObservation{}).Where("id IN ?", ids).Distinct().Pluck("client_id", &clients).Error; err != nil {
		return err
	}
	var locked []access_do.Client
	if err := r.engine.DB(ctx).Clauses(clause.Locking{Strength: "UPDATE"}).Where("id IN ?", clients).Order("id").Find(&locked).Error; err != nil {
		return err
	}
	if err := r.engine.DB(ctx).CreateInBatches(rows, 250).Error; err != nil {
		return err
	}
	return r.engine.DB(ctx).Where("id IN ?", ids).Delete(&reporting_do.QuotaObservation{}).Error
}
