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
	accounts := map[string]string{}
	periods := map[string]reporting_do.AccountTokenPeriod{}
	for _, u := range batch.AccountUsage {
		// 允许离线旧周期补传以保持原队列幂等，但查询永远只读当前窗口。
		if u.CollectedAtMS > received+5*60*1000 {
			return utils.ErrBadParamInput
		}
		bindingKey := reportingv1.Key(u.Provider, u.LocalScope, u.AccountID)
		accountKey, ok := accounts[bindingKey]
		if !ok {
			account, err := s.resolveAccount(ctx, p, u.Provider, u.LocalScope, &u.AccountID, "confirmed")
			if err != nil {
				return err
			}
			if account == nil {
				return utils.ErrBadParamInput
			}
			accountKey = *account
			accounts[bindingKey] = accountKey
		}
		for _, f := range u.Facts {
			row := reporting_do.AccountTokenFact{ID: f.ID, AccountKey: accountKey, Provider: u.Provider, ObservedAtMS: f.ObservedAtMS, TotalTokens: f.TotalTokens}
			if previous, ok := rows[row.ID]; ok && previous != row {
				return utils.ErrConflict
			}
			rows[row.ID] = row
		}
		period := reporting_do.AccountTokenPeriod{ID: reportingv1.Key(p.ID, accountKey, strconv.FormatInt(u.ResetsAtMS, 10)), AccountKey: accountKey, Provider: u.Provider, ClientID: p.ID, WindowStartAtMS: u.WindowStartAtMS, ResetsAtMS: u.ResetsAtMS, CollectedAtMS: u.CollectedAtMS}
		if previous, ok := periods[period.ID]; !ok || previous.CollectedAtMS < period.CollectedAtMS {
			periods[period.ID] = period
		}
	}
	periodIDs := make([]string, 0, len(periods))
	for id := range periods {
		periodIDs = append(periodIDs, id)
	}
	slices.Sort(periodIDs)
	for _, id := range periodIDs {
		if err := s.repository.SaveAccountTokenPeriod(ctx, periods[id]); err != nil {
			return err
		}
	}
	ids := make([]string, 0, len(rows))
	for id := range rows {
		ids = append(ids, id)
	}
	slices.SortFunc(ids, strings.Compare)
	facts := make([]reporting_do.AccountTokenFact, len(ids))
	sources := make([]reporting_do.AccountTokenSource, len(ids))
	for i, id := range ids {
		facts[i] = rows[id]
		sources[i] = reporting_do.AccountTokenSource{ID: reportingv1.Key(p.ID, id), FactID: id, ClientID: p.ID}
	}
	previous, err := s.repository.LockAccountTokenFacts(ctx, facts)
	if err != nil {
		return err
	}
	if !slices.Equal(previous, facts) {
		return utils.ErrConflict
	}
	return s.repository.SaveAccountTokenSources(ctx, sources)
}
