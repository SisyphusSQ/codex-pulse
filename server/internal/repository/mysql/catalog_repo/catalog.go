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
	err = r.engine.DB(ctx).Model(&reporting_do.Usage{}).Table("pulse_usage AS u").Select("s.provider, u.model").Joins("JOIN pulse_sessions AS s ON s.id = u.session_key").Where("u.model IS NOT NULL AND s.deleted = ?", false).Distinct().Order("s.provider,u.model").Limit(2001).Scan(&out).Error
	if len(out) > 2000 {
		return nil, utils.ErrRequestBudget
	}
	return
}

func (r *Catalog) Prices(ctx context.Context) (out []catalog_dto.PriceEvidence, err error) {
	err = r.engine.DB(ctx).Table("pulse_usage AS u").Select("s.provider,u.model,u.pricing_version,u.input_price,u.cached_price,u.cache_write_price,u.output_price").Joins("JOIN pulse_sessions AS s ON s.id = u.session_key").Where("u.model IS NOT NULL AND u.pricing_version IS NOT NULL AND s.deleted = ?", false).Distinct().Limit(2001).Scan(&out).Error
	if len(out) > 2000 {
		return nil, utils.ErrRequestBudget
	}
	return
}
