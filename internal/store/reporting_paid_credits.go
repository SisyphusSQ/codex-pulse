package store

import (
	"context"

	"gorm.io/gorm"

	reportingv1 "github.com/SisyphusSQ/codex-pulse/api/codexpulse/reporting/v1"
)

// ReportingPaidCredits exports bounded UTC daily observations independently of Session cursors.
func (r *Repository) ReportingPaidCredits(ctx context.Context, start int64) (out []reportingv1.PaidCredit, err error) {
	err = r.database.ViewSnapshot(ctx, func(ctx context.Context, db *gorm.DB) error {
		ranked := db.Model(&paidCreditModel{}).Where("observed_at_ms >= ?", start).
			Select("*, ROW_NUMBER() OVER(PARTITION BY account_scope, limit_id, CAST(observed_at_ms / 86400000 AS INTEGER), state ORDER BY observed_at_ms DESC,id DESC) AS position")
		var rows []paidCreditModel
		if err := db.Table("(?) AS ranked", ranked).Where("position = 1").Order("observed_at_ms,id").Limit(2001).Find(&rows).Error; err != nil {
			return err
		}
		if len(rows) > 2000 {
			return ErrReportingBudget
		}
		for _, row := range rows {
			out = append(out, reportingv1.PaidCredit{ID: row.ID, Provider: "codex", LocalScope: row.AccountScope, Snapshot: paidCreditSnapshot(row)})
		}
		return nil
	})
	return
}
