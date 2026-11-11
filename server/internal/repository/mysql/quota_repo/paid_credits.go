package quota_repo

import (
	"context"

	"gorm.io/gorm"

	reporting_do "github.com/SisyphusSQ/codex-pulse/server/internal/models/do/mysql/reporting_do"
	quota_dto "github.com/SisyphusSQ/codex-pulse/server/internal/models/dto/quota_dto"
	"github.com/SisyphusSQ/codex-pulse/server/utils"
)

func (r *Quota) PaidCredits(ctx context.Context, q quota_dto.Query, now int64) (rows []reporting_do.PaidCredit, err error) {
	base := r.query(ctx, q).Model(&reporting_do.PaidCredit{}).Where("observed_at_ms <= ?", now)
	latest := base.Session(&gorm.Session{}).Select("id,DENSE_RANK() OVER(PARTITION BY account_key,limit_id,state ORDER BY observed_at_ms DESC) AS recency")
	daily := base.Session(&gorm.Session{}).Where("state = 'known' AND observed_at_ms >= ?", max(int64(0), now-30*86400000)).Select("id,DENSE_RANK() OVER(PARTITION BY account_key,limit_id,FLOOR(observed_at_ms / 86400000) ORDER BY observed_at_ms DESC) AS recency")
	selected := r.engine.DB(ctx).Table("(?) AS latest", latest).Select("id").Where("recency = 1")
	days := r.engine.DB(ctx).Table("(?) AS daily", daily).Select("id").Where("recency = 1")
	err = r.engine.DB(ctx).Where("id IN (?) OR id IN (?)", selected, days).Order("observed_at_ms,id").Limit(quota_dto.MaximumObservations + 1).Find(&rows).Error
	if len(rows) > quota_dto.MaximumObservations {
		return nil, utils.ErrRequestBudget
	}
	return
}
