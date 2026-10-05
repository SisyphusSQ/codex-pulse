package reporting_srv

import (
	"context"
	"slices"
	"strconv"
	"strings"

	reportingv1 "github.com/SisyphusSQ/codex-pulse/api/codexpulse/reporting/v1"
	reporting_do "github.com/SisyphusSQ/codex-pulse/server/internal/models/do/mysql/reporting_do"
	access_dto "github.com/SisyphusSQ/codex-pulse/server/internal/models/dto/access_dto"
	"github.com/SisyphusSQ/codex-pulse/server/utils"
)

func (s *Reporting) acceptAccountTokens(ctx context.Context, p access_dto.Principal, batch reportingv1.Batch, received int64) error {
	rows := map[string]reporting_do.AccountTokenFact{}
	for _, u := range batch.AccountUsage {
		// 允许离线旧周期补传以保持原队列幂等，但查询永远只读当前窗口。
		if u.CollectedAtMS > received+5*60*1000 {
			return utils.ErrBadParamInput
		}
		account, err := s.resolveAccount(ctx, p, u.Provider, u.LocalScope, &u.AccountID, "confirmed")
		if err != nil {
			return err
		}
		if account == nil {
			return utils.ErrBadParamInput
		}
		for _, f := range u.Facts {
			row := reporting_do.AccountTokenFact{ID: f.ID, AccountKey: *account, Provider: u.Provider, ObservedAtMS: f.ObservedAtMS, TotalTokens: f.TotalTokens}
			if previous, ok := rows[row.ID]; ok && previous != row {
				return utils.ErrConflict
			}
			rows[row.ID] = row
		}
		period := reporting_do.AccountTokenPeriod{ID: reportingv1.Key(p.ID, *account, strconv.FormatInt(u.ResetsAtMS, 10)), AccountKey: *account, Provider: u.Provider, ClientID: p.ID, WindowStartAtMS: u.WindowStartAtMS, ResetsAtMS: u.ResetsAtMS, CollectedAtMS: u.CollectedAtMS}
		if err := s.repository.SaveAccountTokenPeriod(ctx, period); err != nil {
			return err
		}
	}
	ids := make([]string, 0, len(rows))
	for id := range rows {
		ids = append(ids, id)
	}
	slices.SortFunc(ids, strings.Compare)
	for _, id := range ids {
		row := rows[id]
		previous, err := s.repository.LockAccountTokenFact(ctx, row)
		if err != nil {
			return err
		}
		if previous != row {
			return utils.ErrConflict
		}
		if err := s.repository.SaveAccountTokenSource(ctx, reporting_do.AccountTokenSource{ID: reportingv1.Key(p.ID, id), FactID: id, ClientID: p.ID}); err != nil {
			return err
		}
	}
	return nil
}
