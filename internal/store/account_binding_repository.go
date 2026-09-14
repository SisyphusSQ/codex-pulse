package store

import (
	"context"
	"errors"
	"math"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func (repository *Repository) EnsureCodexAccountScopeKey(
	ctx context.Context,
	candidate [32]byte,
	createdAtMS int64,
) ([32]byte, error) {
	var empty [32]byte
	if repository == nil || repository.database == nil {
		return empty, ErrInvalidRepository
	}
	if !validCodexAccountBindingTimestamp(createdAtMS) {
		return empty, invalidRecord("codex account scope key timestamp is invalid")
	}
	var stored [32]byte
	err := repository.database.Write(ctx, func(ctx context.Context, transaction *gorm.DB) error {
		model := codexAccountScopeKeyModel{
			SingletonID: 1, KeyBytes: append([]byte(nil), candidate[:]...), CreatedAtMS: createdAtMS,
		}
		if err := transaction.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).
			Create(&model).Error; err != nil {
			return err
		}
		var current codexAccountScopeKeyModel
		if err := transaction.WithContext(ctx).Where("singleton_id = ?", 1).Take(&current).Error; err != nil {
			return err
		}
		if len(current.KeyBytes) != 32 {
			return invalidRecord("stored codex account scope key is invalid")
		}
		copy(stored[:], current.KeyBytes)
		return nil
	})
	if err != nil {
		return empty, err
	}
	return stored, nil
}

func (repository *Repository) MarkCodexAccountBindingPending(
	ctx context.Context,
	observedAtMS int64,
	reason CodexAccountBindingReason,
) (CodexAccountBinding, error) {
	if repository == nil || repository.database == nil {
		return CodexAccountBinding{}, ErrInvalidRepository
	}
	if err := validateCodexAccountBindingTransition(observedAtMS, reason); err != nil {
		return CodexAccountBinding{}, err
	}
	var stored CodexAccountBinding
	err := repository.database.Write(ctx, func(ctx context.Context, transaction *gorm.DB) error {
		current, err := loadStoredCodexAccountBinding(ctx, transaction)
		if err != nil {
			return err
		}
		generation, err := incrementCodexAccountBindingGeneration(current, true)
		if err != nil {
			return err
		}
		next := storedCodexAccountBinding{
			CodexAccountBinding: CodexAccountBinding{
				State:             CodexAccountBindingPending,
				BindingGeneration: generation,
				ObservedAtMS:      observedAtMS,
				Reason:            reason,
			},
			LastConfirmedScope: cloneQuotaString(current.LastConfirmedScope),
			Present:            true,
		}
		if err := persistCodexAccountBinding(ctx, transaction, next); err != nil {
			return err
		}
		stored = next.CodexAccountBinding
		return nil
	})
	return stored, err
}

func (repository *Repository) ConfirmCodexAccountBinding(
	ctx context.Context,
	accountScope string,
	observedAtMS int64,
	reason CodexAccountBindingReason,
) (CodexAccountBinding, bool, error) {
	if repository == nil || repository.database == nil {
		return CodexAccountBinding{}, false, ErrInvalidRepository
	}
	if !validDerivedCodexAccountScope(accountScope) {
		return CodexAccountBinding{}, false, invalidRecord("codex account scope is invalid")
	}
	if err := validateCodexAccountBindingTransition(observedAtMS, reason); err != nil {
		return CodexAccountBinding{}, false, err
	}
	var stored CodexAccountBinding
	var changed bool
	err := repository.database.Write(ctx, func(ctx context.Context, transaction *gorm.DB) error {
		current, err := loadStoredCodexAccountBinding(ctx, transaction)
		if err != nil {
			return err
		}
		if err := upsertCodexAccountScope(ctx, transaction, accountScope, observedAtMS); err != nil {
			return err
		}
		generation, err := confirmCodexAccountBindingGeneration(current, accountScope)
		if err != nil {
			return err
		}
		next := storedCodexAccountBinding{
			CodexAccountBinding: CodexAccountBinding{
				State:             CodexAccountBindingConfirmed,
				AccountScope:      cloneQuotaString(&accountScope),
				BindingGeneration: generation,
				ObservedAtMS:      observedAtMS,
				Reason:            reason,
			},
			LastConfirmedScope: cloneQuotaString(&accountScope),
			Present:            true,
		}
		changed = codexAccountBindingObservableChanged(current.CodexAccountBinding, next.CodexAccountBinding)
		if err := persistCodexAccountBinding(ctx, transaction, next); err != nil {
			return err
		}
		stored = next.CodexAccountBinding
		return nil
	})
	return stored, changed, err
}

func (repository *Repository) SetCodexAccountBindingUnavailable(
	ctx context.Context,
	state CodexAccountBindingState,
	observedAtMS int64,
	reason CodexAccountBindingReason,
) (CodexAccountBinding, bool, error) {
	if repository == nil || repository.database == nil {
		return CodexAccountBinding{}, false, ErrInvalidRepository
	}
	if !validCodexAccountBindingUnavailableState(state) {
		return CodexAccountBinding{}, false, invalidRecord("codex account binding unavailable state is invalid")
	}
	if err := validateCodexAccountBindingTransition(observedAtMS, reason); err != nil {
		return CodexAccountBinding{}, false, err
	}
	var stored CodexAccountBinding
	var changed bool
	err := repository.database.Write(ctx, func(ctx context.Context, transaction *gorm.DB) error {
		current, err := loadStoredCodexAccountBinding(ctx, transaction)
		if err != nil {
			return err
		}
		generation, err := incrementCodexAccountBindingGeneration(current, true)
		if err != nil {
			return err
		}
		next := storedCodexAccountBinding{
			CodexAccountBinding: CodexAccountBinding{
				State:             state,
				BindingGeneration: generation,
				ObservedAtMS:      observedAtMS,
				Reason:            reason,
			},
			LastConfirmedScope: cloneQuotaString(current.LastConfirmedScope),
			Present:            true,
		}
		changed = codexAccountBindingObservableChanged(current.CodexAccountBinding, next.CodexAccountBinding)
		if err := persistCodexAccountBinding(ctx, transaction, next); err != nil {
			return err
		}
		stored = next.CodexAccountBinding
		return nil
	})
	return stored, changed, err
}

func (repository *Repository) CodexAccountBinding(
	ctx context.Context,
) (CodexAccountBinding, error) {
	if repository == nil || repository.database == nil {
		return CodexAccountBinding{}, ErrInvalidRepository
	}
	var stored CodexAccountBinding
	err := repository.database.View(ctx, func(ctx context.Context, connection *gorm.DB) error {
		current, err := loadStoredCodexAccountBinding(ctx, connection)
		if err != nil {
			return err
		}
		stored = current.CodexAccountBinding
		return nil
	})
	return stored, err
}

func requireCodexAccountFence(
	ctx context.Context,
	transaction *gorm.DB,
	accountScope string,
	bindingGeneration int64,
) error {
	if transaction == nil || !validDerivedCodexAccountScope(accountScope) || bindingGeneration <= 0 {
		return ErrCodexAccountBindingChanged
	}
	current, err := loadStoredCodexAccountBinding(ctx, transaction)
	if err != nil {
		return err
	}
	if current.State != CodexAccountBindingConfirmed || current.AccountScope == nil ||
		*current.AccountScope != accountScope || current.BindingGeneration != bindingGeneration {
		return ErrCodexAccountBindingChanged
	}
	return nil
}

func validateCodexAccountBindingTransition(observedAtMS int64, reason CodexAccountBindingReason) error {
	if !validCodexAccountBindingTimestamp(observedAtMS) {
		return invalidRecord("codex account binding timestamp is invalid")
	}
	if !validCodexAccountBindingReason(reason) {
		return invalidRecord("codex account binding reason is invalid")
	}
	return nil
}

func loadStoredCodexAccountBinding(
	ctx context.Context,
	database *gorm.DB,
) (storedCodexAccountBinding, error) {
	var model codexAccountBindingModel
	err := database.WithContext(ctx).Where("singleton_id = ?", 1).Take(&model).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return storedCodexAccountBinding{
			CodexAccountBinding: CodexAccountBinding{
				State: CodexAccountBindingUnknown, Reason: CodexAccountBindingReasonStartup,
			},
		}, nil
	}
	if err != nil {
		return storedCodexAccountBinding{}, err
	}
	state := CodexAccountBindingState(model.State)
	reason := CodexAccountBindingReason(model.Reason)
	if !validCodexAccountBindingState(state) || !validCodexAccountBindingReason(reason) ||
		model.BindingGeneration < 0 || !validCodexAccountBindingTimestamp(model.ObservedAtMS) {
		return storedCodexAccountBinding{}, invalidRecord("stored codex account binding is invalid")
	}
	if (state == CodexAccountBindingConfirmed) != (model.AccountScope != nil) ||
		(state == CodexAccountBindingConfirmed && (model.LastConfirmedScope == nil ||
			*model.AccountScope != *model.LastConfirmedScope || model.BindingGeneration <= 0)) {
		return storedCodexAccountBinding{}, invalidRecord("stored codex account binding is inconsistent")
	}
	if model.AccountScope != nil && !validDerivedCodexAccountScope(*model.AccountScope) {
		return storedCodexAccountBinding{}, invalidRecord("stored codex account binding is invalid")
	}
	if model.LastConfirmedScope != nil && !validDerivedCodexAccountScope(*model.LastConfirmedScope) {
		return storedCodexAccountBinding{}, invalidRecord("stored codex account binding is invalid")
	}
	return storedCodexAccountBinding{
		CodexAccountBinding: CodexAccountBinding{
			State:             state,
			AccountScope:      cloneQuotaString(model.AccountScope),
			BindingGeneration: model.BindingGeneration,
			ObservedAtMS:      model.ObservedAtMS,
			Reason:            reason,
		},
		LastConfirmedScope: cloneQuotaString(model.LastConfirmedScope),
		Present:            true,
	}, nil
}

func persistCodexAccountBinding(
	ctx context.Context,
	transaction *gorm.DB,
	value storedCodexAccountBinding,
) error {
	model := codexAccountBindingModel{
		SingletonID:        1,
		State:              string(value.State),
		AccountScope:       cloneQuotaString(value.AccountScope),
		LastConfirmedScope: cloneQuotaString(value.LastConfirmedScope),
		BindingGeneration:  value.BindingGeneration,
		ObservedAtMS:       value.ObservedAtMS,
		Reason:             string(value.Reason),
	}
	return transaction.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "singleton_id"}},
		DoUpdates: clause.AssignmentColumns([]string{
			"state", "account_scope", "last_confirmed_scope", "binding_generation",
			"observed_at_ms", "reason",
		}),
	}).Create(&model).Error
}

func upsertCodexAccountScope(
	ctx context.Context,
	transaction *gorm.DB,
	accountScope string,
	observedAtMS int64,
) error {
	var existing codexAccountScopeModel
	err := transaction.WithContext(ctx).Where("account_scope = ?", accountScope).Take(&existing).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return transaction.WithContext(ctx).Create(&codexAccountScopeModel{
			AccountScope: accountScope, FirstSeenAtMS: observedAtMS, LastSeenAtMS: observedAtMS,
		}).Error
	}
	if err != nil {
		return err
	}
	lastSeenAtMS := existing.LastSeenAtMS
	if observedAtMS > lastSeenAtMS {
		lastSeenAtMS = observedAtMS
	}
	return transaction.WithContext(ctx).Model(&codexAccountScopeModel{}).
		Where("account_scope = ?", accountScope).
		Update("last_seen_at_ms", lastSeenAtMS).Error
}

func incrementCodexAccountBindingGeneration(
	current storedCodexAccountBinding,
	enteringNonConfirmed bool,
) (int64, error) {
	if !enteringNonConfirmed {
		return current.BindingGeneration, nil
	}
	if current.State != CodexAccountBindingConfirmed && current.State != CodexAccountBindingUnknown {
		return current.BindingGeneration, nil
	}
	if current.BindingGeneration == math.MaxInt64 {
		return 0, invalidRecord("codex account binding generation is exhausted")
	}
	return current.BindingGeneration + 1, nil
}

func confirmCodexAccountBindingGeneration(
	current storedCodexAccountBinding,
	accountScope string,
) (int64, error) {
	if current.State == CodexAccountBindingConfirmed && current.AccountScope != nil &&
		*current.AccountScope == accountScope {
		return current.BindingGeneration, nil
	}
	if current.State == CodexAccountBindingConfirmed {
		if current.BindingGeneration == math.MaxInt64 {
			return 0, invalidRecord("codex account binding generation is exhausted")
		}
		return current.BindingGeneration + 1, nil
	}
	if current.BindingGeneration == 0 {
		return 1, nil
	}
	return current.BindingGeneration, nil
}

func codexAccountBindingObservableChanged(before, after CodexAccountBinding) bool {
	return before.State != after.State ||
		!equalStringPointer(before.AccountScope, after.AccountScope) ||
		before.BindingGeneration != after.BindingGeneration
}
