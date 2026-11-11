package reporting_srv

import (
	"context"
	"errors"
	"reflect"

	"gorm.io/gorm"

	reportingv1 "github.com/SisyphusSQ/codex-pulse/api/codexpulse/reporting/v1"
	reporting_do "github.com/SisyphusSQ/codex-pulse/server/internal/models/do/mysql/reporting_do"
	access_dto "github.com/SisyphusSQ/codex-pulse/server/internal/models/dto/access_dto"
	"github.com/SisyphusSQ/codex-pulse/server/utils"
)

func (s *Reporting) acceptPaidCredits(ctx context.Context, p access_dto.Principal, batch reportingv1.Batch, received int64) error {
	for _, c := range batch.PaidCredits {
		account, err := s.resolveAccount(ctx, p, c.Provider, c.LocalScope, c.AccountID, "confirmed")
		if err != nil {
			return err
		}
		if account == nil {
			return utils.ErrBadParamInput
		}
		v := c.Snapshot
		row := reporting_do.PaidCredit{ID: reportingv1.Key(p.ID, "paid-credit", c.ID), ClientID: p.ID, Provider: c.Provider, AccountKey: *account, LocalScope: c.LocalScope, LimitID: v.LimitID, State: v.State, Balance: v.Balance, HasCredits: v.HasCredits, Unlimited: v.Unlimited, ObservedAtMS: v.ObservedAtMS, ReceivedAtMS: received}
		previous, err := s.repository.PaidCredit(ctx, row.ID)
		if err == nil {
			row.ReceivedAtMS = previous.ReceivedAtMS
			if !reflect.DeepEqual(row, previous) {
				return utils.ErrConflict
			}
			continue
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		if err := s.repository.SavePaidCredit(ctx, row); err != nil {
			return err
		}
	}
	return nil
}
