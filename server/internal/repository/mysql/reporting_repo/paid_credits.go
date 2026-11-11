package reporting_repo

import (
	"context"

	reporting_do "github.com/SisyphusSQ/codex-pulse/server/internal/models/do/mysql/reporting_do"
)

func (r *Reporting) PaidCredit(ctx context.Context, id string) (row reporting_do.PaidCredit, err error) {
	err = r.engine.DB(ctx).Where("id = ?", id).Take(&row).Error
	return
}

func (r *Reporting) SavePaidCredit(ctx context.Context, row reporting_do.PaidCredit) error {
	return r.engine.DB(ctx).Create(&row).Error
}
