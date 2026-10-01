package service

import (
	"context"
	"encoding/json/v2"
	"errors"
	"slices"

	"gorm.io/gorm"

	reportingv1 "github.com/SisyphusSQ/codex-pulse/api/codexpulse/reporting/v1"
	"github.com/SisyphusSQ/codex-pulse/server/internal/models/do"
	"github.com/SisyphusSQ/codex-pulse/server/internal/models/dto"
	"github.com/SisyphusSQ/codex-pulse/server/utils"
)

func (s *Reporting) acceptAccountFacts(ctx context.Context, p dto.Principal, batch reportingv1.Batch, received int64) error {

	// 一次取得本批所有账号 owner，按全局键排序；不能先锁账号A再因
	// binding取得B，让另一设备以B、A的顺序形成交叉等待。
	owners := make(map[string]do.Account)
	for _, a := range batch.Accounts {
		key := reportingv1.Key(a.Provider, a.ID)
		owners[key] = do.Account{ID: key, Provider: a.Provider, AccountID: a.ID, Email: a.Email, Plan: a.Plan, CollectedAtMS: a.CollectedAtMS}
	}
	for _, b := range batch.Bindings {
		key := reportingv1.Key(b.Provider, b.AccountID)
		if _, ok := owners[key]; !ok {
			owners[key] = do.Account{ID: key, Provider: b.Provider, AccountID: b.AccountID, CollectedAtMS: b.ConfirmedAtMS}
		}
	}
	keys := make([]string, 0, len(owners))
	for key := range owners {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	currentAccounts := make(map[string]do.Account)
	for _, key := range keys {
		if err := s.repository.EnsureAccount(ctx, owners[key]); err != nil {
			return err
		}
		current, err := s.repository.Account(ctx, key)
		if err != nil {
			return err
		}
		currentAccounts[key] = current
	}
	for _, a := range batch.Accounts {
		key := reportingv1.Key(a.Provider, a.ID)
		row := owners[key]
		if row.CollectedAtMS >= currentAccounts[key].CollectedAtMS {
			if err := s.repository.SaveAccount(ctx, row); err != nil {
				return err
			}
		}
	}

	for _, binding := range batch.Bindings {
		if binding.LocalScope == "default" {
			return utils.ErrBadParamInput
		}
		accountKey := reportingv1.Key(binding.Provider, binding.AccountID)
		id := reportingv1.Key(p.ID, binding.Provider, binding.LocalScope)
		current, err := s.repository.Binding(ctx, id)
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		if current.ID != "" && current.AccountKey != accountKey {
			return utils.ErrConflict
		}
		if current.ID == "" || binding.ConfirmedAtMS > current.ConfirmedAtMS {
			if err := s.repository.SaveBinding(ctx, do.AccountBinding{ID: id, ClientID: p.ID, Provider: binding.Provider, LocalScope: binding.LocalScope, AccountKey: accountKey, ConfirmedAtMS: binding.ConfirmedAtMS}); err != nil {
				return err
			}
		}
	}
	for _, q := range batch.Quotas {
		accountKey, err := s.resolveAccount(ctx, p, q.Provider, q.LocalScope, q.AccountID, q.HistoryOrigin)
		if err != nil {
			return err
		}
		id := reportingv1.Key(p.ID, q.Provider, q.ID)
		row := do.QuotaObservation{ID: id, ClientID: p.ID, Provider: q.Provider, AccountKey: accountKey, LocalScope: q.LocalScope, ObservationID: q.ID, LimitID: q.LimitID, WindowKind: q.WindowKind, WindowMinutes: q.WindowMinutes, ResetsAtMS: q.ResetsAtMS, ObservedAtMS: q.ObservedAtMS, UsedPercent: normalizedPercent(q.UsedPercent), Validity: q.Validity, Source: q.Source, HistoryOrigin: q.HistoryOrigin, ReceivedAtMS: received}
		if accountKey != nil && row.HistoryOrigin == "pending_association" {
			row.HistoryOrigin = "confirmed"
		}
		previous, err := s.repository.Observation(ctx, id)
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		if previous.ID != "" {
			before, after := previous, row
			before.AccountKey = nil
			after.AccountKey = nil
			before.HistoryOrigin = ""
			after.HistoryOrigin = ""
			before.ReceivedAtMS = 0
			after.ReceivedAtMS = 0
			_, first, err := jsonDigest(before)
			if err != nil {
				return err
			}
			_, second, err := jsonDigest(after)
			if err != nil {
				return err
			}
			if first != second {
				return utils.ErrConflict
			}
			row.ReceivedAtMS = previous.ReceivedAtMS
		}
		if err := s.repository.SaveObservation(ctx, row); err != nil {
			return err
		}
	}
	for _, c := range batch.Credits {
		accountKey, err := s.resolveAccount(ctx, p, c.Provider, c.LocalScope, c.AccountID, "confirmed")
		if err != nil {
			return err
		}
		id := reportingv1.Key(p.ID, c.Provider, c.ID)
		row := do.ResetCredits{ID: id, ClientID: p.ID, Provider: c.Provider, AccountKey: accountKey, LocalScope: c.LocalScope, ObservedAtMS: c.ObservedAtMS, Inventory: c.Inventory, Status: c.Status, NextResetAtMS: c.NextResetAtMS}
		previous, err := s.repository.Credits(ctx, id)
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		if previous.ID != "" {
			a, b := previous, row
			a.AccountKey = nil
			b.AccountKey = nil
			if !jsonEqual(a, b) {
				return utils.ErrConflict
			}
		}
		if err := s.repository.SaveCredits(ctx, row); err != nil {
			return err
		}
	}
	return nil
}
func (s *Reporting) resolveAccount(ctx context.Context, p dto.Principal, provider, scope string, rawID *string, origin string) (*string, error) {
	if origin == "legacy_unassigned" {
		if rawID != nil {
			return nil, utils.ErrBadParamInput
		}
		return nil, nil
	}
	binding, err := s.repository.Binding(ctx, reportingv1.Key(p.ID, provider, scope))
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	if binding.ID == "" {
		if rawID != nil || origin == "linked_history" {
			return nil, utils.ErrBadParamInput
		}
		return nil, nil
	}
	if rawID != nil && reportingv1.Key(provider, *rawID) != binding.AccountKey {
		return nil, utils.ErrConflict
	}
	return &binding.AccountKey, nil
}
func jsonEqual(a, b any) bool {
	first, err := json.Marshal(a)
	if err != nil {
		return false
	}
	second, err := json.Marshal(b)
	return err == nil && string(first) == string(second)
}
