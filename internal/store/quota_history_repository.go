package store

import (
	"context"
	"errors"
	"math"

	"gorm.io/gorm"
)

func (repository *Repository) LegacyQuotaHistoryStatus(
	ctx context.Context,
	accountScope string,
) (LegacyQuotaHistoryStatus, error) {
	if repository == nil || repository.database == nil {
		return LegacyQuotaHistoryStatus{}, ErrInvalidRepository
	}
	if !validDerivedCodexAccountScope(accountScope) {
		return LegacyQuotaHistoryStatus{}, invalidRecord("quota history account scope is invalid")
	}
	var status LegacyQuotaHistoryStatus
	err := repository.database.View(ctx, func(ctx context.Context, connection *gorm.DB) error {
		association, found, err := loadLegacyQuotaHistoryAssociation(ctx, connection)
		if err != nil {
			return err
		}
		if found && association.AccountScope != accountScope {
			status.State = LegacyQuotaHistoryLinkedElsewhere
			return nil
		}
		coverage, err := legacyQuotaHistoryCoverage(ctx, connection)
		if err != nil {
			return err
		}
		status = coverage
		if found {
			status.State = LegacyQuotaHistoryLinked
			status.AssociationRevision = cloneInt64Value(association.Revision)
			status.AssociationAccountScope = cloneQuotaString(&association.AccountScope)
		}
		return nil
	})
	return status, err
}

func (repository *Repository) LinkLegacyQuotaHistory(
	ctx context.Context,
	request LegacyQuotaHistoryLinkRequest,
) (LegacyQuotaHistoryMutation, error) {
	if repository == nil || repository.database == nil {
		return LegacyQuotaHistoryMutation{}, ErrInvalidRepository
	}
	detectedID, err := codexSubscriptionPublicID(
		request.DetectedAccountID, "quota history detected account id is invalid",
	)
	if err != nil || request.ExpectedDetectedRevision <= 0 ||
		!validCodexAccountBindingTimestamp(request.NowMS) {
		return LegacyQuotaHistoryMutation{}, invalidRecord("quota history link request is invalid")
	}
	var mutation LegacyQuotaHistoryMutation
	err = repository.database.Write(ctx, func(ctx context.Context, transaction *gorm.DB) error {
		detected, found, err := loadDetectedByPublicID(ctx, transaction, detectedID)
		if err != nil {
			return err
		}
		if !found {
			return invalidRecord("quota history detected account is not found")
		}
		if detected.Revision != request.ExpectedDetectedRevision {
			mutation = legacyQuotaHistoryConflict(LegacyQuotaHistoryReasonRevisionChanged)
			return nil
		}
		binding, err := loadStoredCodexAccountBinding(ctx, transaction)
		if err != nil {
			return err
		}
		if binding.State != CodexAccountBindingConfirmed || binding.AccountScope == nil ||
			*binding.AccountScope != detected.AccountScope {
			mutation = legacyQuotaHistoryConflict(LegacyQuotaHistoryReasonCurrentAccountChanged)
			return nil
		}
		association, associated, err := loadLegacyQuotaHistoryAssociation(ctx, transaction)
		if err != nil {
			return err
		}
		if associated {
			if association.AccountScope == detected.AccountScope {
				mutation = LegacyQuotaHistoryMutation{Result: LegacyQuotaHistoryMutationNoop}
			} else {
				mutation = legacyQuotaHistoryConflict(LegacyQuotaHistoryReasonAlreadyLinked)
			}
			return nil
		}
		coverage, err := legacyQuotaHistoryCoverage(ctx, transaction)
		if err != nil {
			return err
		}
		if coverage.State != LegacyQuotaHistoryAvailable {
			mutation = LegacyQuotaHistoryMutation{Result: LegacyQuotaHistoryMutationNoop}
			return nil
		}
		revision, err := nextLegacyQuotaHistoryAssociationRevision(ctx, transaction, request.NowMS)
		if err != nil {
			return err
		}
		model := quotaHistoryAssociationModel{
			LegacyAccountScope: QuotaAccountScopeDefault,
			AccountScope:       detected.AccountScope,
			Revision:           revision,
			LinkedAtMS:         request.NowMS,
			UpdatedAtMS:        request.NowMS,
		}
		if err := transaction.WithContext(ctx).Create(&model).Error; err != nil {
			return err
		}
		mutation = LegacyQuotaHistoryMutation{Result: LegacyQuotaHistoryMutationApplied}
		return nil
	})
	return mutation, err
}

func nextLegacyQuotaHistoryAssociationRevision(
	ctx context.Context,
	database *gorm.DB,
	nowMS int64,
) (int64, error) {
	var generation quotaHistoryAssociationGenerationModel
	err := database.WithContext(ctx).
		Where("legacy_account_scope = ?", QuotaAccountScopeDefault).
		Take(&generation).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		generation = quotaHistoryAssociationGenerationModel{
			LegacyAccountScope: QuotaAccountScopeDefault,
			Revision:           1,
			UpdatedAtMS:        nowMS,
		}
		if err := database.WithContext(ctx).Create(&generation).Error; err != nil {
			return 0, err
		}
		return generation.Revision, nil
	}
	if err != nil {
		return 0, err
	}
	if generation.Revision <= 0 || generation.Revision == math.MaxInt64 || generation.UpdatedAtMS < 0 {
		return 0, invalidRecord("stored quota history association generation is invalid")
	}
	next := generation.Revision + 1
	result := database.WithContext(ctx).
		Model(&quotaHistoryAssociationGenerationModel{}).
		Where("legacy_account_scope = ? AND revision = ?", QuotaAccountScopeDefault, generation.Revision).
		Updates(map[string]any{"revision": next, "updated_at_ms": nowMS})
	if result.Error != nil {
		return 0, result.Error
	}
	if result.RowsAffected != 1 {
		return 0, invalidRecord("quota history association generation changed")
	}
	return next, nil
}

func (repository *Repository) UnlinkLegacyQuotaHistory(
	ctx context.Context,
	request LegacyQuotaHistoryUnlinkRequest,
) (LegacyQuotaHistoryMutation, error) {
	if repository == nil || repository.database == nil {
		return LegacyQuotaHistoryMutation{}, ErrInvalidRepository
	}
	detectedID, err := codexSubscriptionPublicID(
		request.DetectedAccountID, "quota history detected account id is invalid",
	)
	if err != nil || request.ExpectedDetectedRevision <= 0 || request.ExpectedAssociationRevision <= 0 {
		return LegacyQuotaHistoryMutation{}, invalidRecord("quota history unlink request is invalid")
	}
	var mutation LegacyQuotaHistoryMutation
	err = repository.database.Write(ctx, func(ctx context.Context, transaction *gorm.DB) error {
		detected, found, err := loadDetectedByPublicID(ctx, transaction, detectedID)
		if err != nil {
			return err
		}
		if !found {
			return invalidRecord("quota history detected account is not found")
		}
		if detected.Revision != request.ExpectedDetectedRevision {
			mutation = legacyQuotaHistoryConflict(LegacyQuotaHistoryReasonRevisionChanged)
			return nil
		}
		association, associated, err := loadLegacyQuotaHistoryAssociation(ctx, transaction)
		if err != nil {
			return err
		}
		if !associated {
			mutation = LegacyQuotaHistoryMutation{Result: LegacyQuotaHistoryMutationNoop}
			return nil
		}
		if association.AccountScope != detected.AccountScope {
			mutation = legacyQuotaHistoryConflict(LegacyQuotaHistoryReasonLinkTargetChanged)
			return nil
		}
		if association.Revision != request.ExpectedAssociationRevision {
			mutation = legacyQuotaHistoryConflict(LegacyQuotaHistoryReasonRevisionChanged)
			return nil
		}
		result := transaction.WithContext(ctx).Where(
			"legacy_account_scope = ? AND account_scope = ? AND revision = ?",
			QuotaAccountScopeDefault, detected.AccountScope, association.Revision,
		).Delete(&quotaHistoryAssociationModel{})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			mutation = legacyQuotaHistoryConflict(LegacyQuotaHistoryReasonRevisionChanged)
			return nil
		}
		mutation = LegacyQuotaHistoryMutation{Result: LegacyQuotaHistoryMutationApplied}
		return nil
	})
	return mutation, err
}

func loadLegacyQuotaHistoryAssociation(
	ctx context.Context,
	database *gorm.DB,
) (quotaHistoryAssociationModel, bool, error) {
	var model quotaHistoryAssociationModel
	err := database.WithContext(ctx).
		Where("legacy_account_scope = ?", QuotaAccountScopeDefault).
		Take(&model).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return quotaHistoryAssociationModel{}, false, nil
	}
	if err != nil {
		return quotaHistoryAssociationModel{}, false, err
	}
	if model.LegacyAccountScope != QuotaAccountScopeDefault ||
		!validDerivedCodexAccountScope(model.AccountScope) || model.Revision <= 0 ||
		model.LinkedAtMS < 0 || model.UpdatedAtMS < model.LinkedAtMS {
		return quotaHistoryAssociationModel{}, false, invalidRecord("stored quota history association is invalid")
	}
	return model, true, nil
}

func legacyQuotaHistoryCoverage(
	ctx context.Context,
	database *gorm.DB,
) (LegacyQuotaHistoryStatus, error) {
	var models []quotaObservationModel
	if err := database.WithContext(ctx).Where(
		"account_scope = ? AND validity = ? AND limit_id IS NOT NULL",
		QuotaAccountScopeDefault, string(QuotaValidityAccepted),
	).Order("first_observed_at_ms").Order("observation_id").Find(&models).Error; err != nil {
		return LegacyQuotaHistoryStatus{}, err
	}
	type cycleFamily struct {
		source        QuotaSource
		windowKind    QuotaWindowKind
		limitID       string
		windowMinutes int64
	}
	cycles := make(map[cycleFamily][]int64)
	status := LegacyQuotaHistoryStatus{State: LegacyQuotaHistoryUnavailable}
	for _, model := range models {
		observation, err := quotaObservationFromModel(model)
		if err != nil {
			return LegacyQuotaHistoryStatus{}, err
		}
		if observation.LimitID == nil || retiredCodexQuotaLimit(*observation.LimitID) {
			continue
		}
		status.ObservationCount++
		if status.FirstObservedAtMS == nil || observation.FirstObservedAtMS < *status.FirstObservedAtMS {
			status.FirstObservedAtMS = cloneInt64Value(observation.FirstObservedAtMS)
		}
		if status.LastObservedAtMS == nil || observation.LastObservedAtMS > *status.LastObservedAtMS {
			status.LastObservedAtMS = cloneInt64Value(observation.LastObservedAtMS)
		}
		family := cycleFamily{
			source: observation.Source, windowKind: observation.WindowKind,
			limitID: *observation.LimitID, windowMinutes: observation.WindowMinutes,
		}
		matched := false
		for _, generation := range cycles[family] {
			if QuotaResetsEquivalentForWindow(
				observation.Source, observation.WindowMinutes, generation, observation.ResetsAtMS,
			) {
				matched = true
				break
			}
		}
		if !matched {
			cycles[family] = append(cycles[family], observation.ResetsAtMS)
			status.CycleCount++
		}
	}
	if status.ObservationCount > 0 {
		status.State = LegacyQuotaHistoryAvailable
	}
	return status, nil
}

func legacyQuotaHistoryConflict(reason string) LegacyQuotaHistoryMutation {
	return LegacyQuotaHistoryMutation{
		Result: LegacyQuotaHistoryMutationConflict,
		Reason: cloneQuotaString(&reason),
	}
}
