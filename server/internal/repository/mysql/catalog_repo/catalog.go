package catalog_repo

import (
	"context"

	gormv2 "github.com/SisyphusSQ/codex-pulse/server/internal/lib/gorm"
	reporting_do "github.com/SisyphusSQ/codex-pulse/server/internal/models/do/mysql/reporting_do"
	catalog_dto "github.com/SisyphusSQ/codex-pulse/server/internal/models/dto/catalog_dto"
	"github.com/SisyphusSQ/codex-pulse/server/utils"
)

type Catalog struct{ engine *gormv2.Engine }

func NewCatalog(engine *gormv2.Engine) *Catalog { return &Catalog{engine: engine} }
func (r *Catalog) Snapshot(ctx context.Context, fn func(context.Context) error) error {
	return r.engine.ReadSnapshot(ctx, fn)
}

func (r *Catalog) Models(ctx context.Context) (out []catalog_dto.ObservedModel, err error) {
	// 先按会话去重，避免在连接中重复处理每条用量；外层继续保留跨会话去重。
	facts := r.engine.DB(ctx).Model(&reporting_do.Usage{}).Select("session_key,model").Where("model IS NOT NULL").Distinct()
	err = r.engine.DB(ctx).Table("(?) AS u", facts).Select("s.provider, u.model").Joins("JOIN pulse_sessions AS s ON s.id = u.session_key").Where("s.deleted = ?", false).Distinct().Order("s.provider,u.model").Limit(2001).Scan(&out).Error
	if len(out) > 2000 {
		return nil, utils.ErrRequestBudget
	}
	return
}

func (r *Catalog) Prices(ctx context.Context) (out []catalog_dto.PriceEvidence, err error) {
	facts := r.engine.DB(ctx).Model(&reporting_do.Usage{}).Select("session_key,model,pricing_version,input_price,cached_price,cache_write_price,output_price").Where("model IS NOT NULL AND pricing_version IS NOT NULL").Distinct()
	err = r.engine.DB(ctx).Table("(?) AS u", facts).Select("s.provider,u.model,u.pricing_version,u.input_price,u.cached_price,u.cache_write_price,u.output_price").Joins("JOIN pulse_sessions AS s ON s.id = u.session_key").Where("s.deleted = ?", false).Distinct().Limit(2001).Scan(&out).Error
	if len(out) > 2000 {
		return nil, utils.ErrRequestBudget
	}
	return
}
