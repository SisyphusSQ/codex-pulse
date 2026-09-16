package store

import (
	"context"
	"errors"
	"math"

	"gorm.io/gorm"

	"github.com/SisyphusSQ/codex-pulse/internal/codex/subscriptionaccounts"
)

func (repository *Repository) ListCodexSubscriptionRecords(
	ctx context.Context,
) (CodexSubscriptionRecords, error) {
	if repository == nil || repository.database == nil {
		return CodexSubscriptionRecords{}, ErrInvalidRepository
	}
	var records CodexSubscriptionRecords
	err := repository.database.View(ctx, func(ctx context.Context, connection *gorm.DB) error {
		binding, err := loadStoredCodexAccountBinding(ctx, connection)
		if err != nil {
			return err
		}
		var detected []codexSubscriptionDetectedAccountModel
		if err := connection.WithContext(ctx).Order("detected_account_id").Find(&detected).Error; err != nil {
			return err
		}
		var manuals []codexSubscriptionManualEntryModel
		if err := connection.WithContext(ctx).Order("updated_at_ms DESC, manual_entry_id").Find(&manuals).Error; err != nil {
			return err
		}
		var links []codexSubscriptionLinkModel
		if err := connection.WithContext(ctx).Order("account_scope").Find(&links).Error; err != nil {
			return err
		}
		records = CodexSubscriptionRecords{
			Binding: subscriptionaccounts.Binding{
				State:             subscriptionaccounts.BindingState(binding.State),
				AccountScope:      cloneQuotaString(binding.AccountScope),
				BindingGeneration: binding.BindingGeneration,
			},
			Detected: make([]subscriptionaccounts.DetectedAccount, 0, len(detected)),
			Manual:   make([]subscriptionaccounts.ManualEntry, 0, len(manuals)),
			Links:    make([]subscriptionaccounts.Link, 0, len(links)),
		}
		for _, model := range detected {
			records.Detected = append(records.Detected, detectedAccountFromModel(model))
		}
		for _, model := range manuals {
			records.Manual = append(records.Manual, manualEntryFromModel(model))
		}
		for _, model := range links {
			records.Links = append(records.Links, linkFromModel(model))
		}
		return nil
	})
	return records, err
}

func (repository *Repository) RecordDetectedCodexSubscriptionProfile(
	ctx context.Context,
	fence CodexAccountFence,
	profile DetectedCodexSubscriptionProfile,
) (CodexSubscriptionDetectedAccount, bool, error) {
	if repository == nil || repository.database == nil {
		return CodexSubscriptionDetectedAccount{}, false, ErrInvalidRepository
	}
	if err := profile.Automatic.Validate(); err != nil {
		return CodexSubscriptionDetectedAccount{}, false, invalidCodexSubscriptionRecord(
			"codex subscription automatic plan is invalid", err,
		)
	}
	if profile.Automatic.State == subscriptionaccounts.AutomaticPlanUnavailable {
		return CodexSubscriptionDetectedAccount{}, false, invalidCodexSubscriptionRecord(
			"codex subscription automatic plan is invalid",
			subscriptionaccounts.ErrInvalidAutomaticPlanFact,
		)
	}
	if !validCodexAccountBindingTimestamp(profile.ObservedAtMS) {
		return CodexSubscriptionDetectedAccount{}, false, invalidRecord("codex subscription observation time is invalid")
	}
	email, matchKey, err := subscriptionaccounts.OptionalEmail(profile.Email)
	if err != nil {
		return CodexSubscriptionDetectedAccount{}, false, invalidCodexSubscriptionRecord(
			"codex subscription detected email is invalid", err,
		)
	}
	var stored CodexSubscriptionDetectedAccount
	var changed bool
	err = repository.database.Write(ctx, func(ctx context.Context, transaction *gorm.DB) error {
		if err := requireCodexAccountFence(ctx, transaction, fence.AccountScope, fence.BindingGeneration); err != nil {
			return err
		}
		current, err := loadDetectedByScope(ctx, transaction, fence.AccountScope)
		if err != nil {
			return err
		}
		next := current
		changed = false
		if email != nil {
			changed = !equalCodexSubscriptionPointer(current.DetectedEmail, email) ||
				!equalCodexSubscriptionPointer(current.EmailMatchKey, matchKey)
			next.DetectedEmail = cloneQuotaString(email)
			next.EmailMatchKey = cloneQuotaString(matchKey)
			next.DetectedEmailObservedAtMS = cloneInt64Value(profile.ObservedAtMS)
		}
		if current.AutomaticPlanState != profile.Automatic.State {
			changed = true
		}
		next.AutomaticPlanState = profile.Automatic.State
		if profile.Automatic.Plan != nil {
			plan := *profile.Automatic.Plan
			if !equalCodexSubscriptionPointer(current.AutomaticPlan, &plan) {
				changed = true
			}
			next.AutomaticPlan = &plan
		} else {
			if current.AutomaticPlan != nil {
				changed = true
			}
			next.AutomaticPlan = nil
		}
		next.AutomaticPlanObservedAtMS = cloneInt64Value(profile.ObservedAtMS)
		revision, err := nextSubscriptionRevision(current.Revision)
		if err != nil {
			return err
		}
		next.Revision = revision
		if err := transaction.WithContext(ctx).
			Where("account_scope = ?", next.AccountScope).
			Select("*").Updates(&next).Error; err != nil {
			return err
		}
		stored = detectedAccountFromModel(next)
		return nil
	})
	return stored, changed, err
}

func (repository *Repository) CreateCodexSubscriptionManualEntry(
	ctx context.Context,
	request CodexSubscriptionManualCreate,
) (CodexSubscriptionMutation, error) {
	if repository == nil || repository.database == nil {
		return CodexSubscriptionMutation{}, ErrInvalidRepository
	}
	publicID, err := codexSubscriptionPublicID(request.ManualEntryID, "codex subscription manual id is invalid")
	if err != nil {
		return CodexSubscriptionMutation{}, err
	}
	if !validCodexAccountBindingTimestamp(request.NowMS) {
		return CodexSubscriptionMutation{}, invalidRecord("codex subscription timestamp is invalid")
	}
	normalized, err := normalizeCodexSubscriptionManual(request.Fields, subscriptionaccounts.ManualStandalone)
	if err != nil {
		return CodexSubscriptionMutation{}, err
	}
	return repository.writeCodexSubscriptionMutation(ctx, func(
		ctx context.Context, transaction *gorm.DB,
	) (CodexSubscriptionMutation, error) {
		existing, found, err := loadManualByID(ctx, transaction, publicID)
		if err != nil {
			return CodexSubscriptionMutation{}, err
		}
		if found {
			if manualMatchesNormalized(existing, normalized) {
				return subscriptionMutation(subscriptionaccounts.MutationNoop, ""), nil
			}
			return subscriptionConflict(subscriptionaccounts.ReasonRequestIDReused), nil
		}
		model := manualModelFromNormalized(publicID, normalized, 1, request.NowMS, request.NowMS)
		if err := transaction.WithContext(ctx).Create(&model).Error; err != nil {
			return CodexSubscriptionMutation{}, err
		}
		return subscriptionMutation(subscriptionaccounts.MutationApplied, ""), nil
	})
}

func (repository *Repository) UpdateCodexSubscriptionManualEntry(
	ctx context.Context,
	request CodexSubscriptionManualUpdate,
) (CodexSubscriptionMutation, error) {
	if repository == nil || repository.database == nil {
		return CodexSubscriptionMutation{}, ErrInvalidRepository
	}
	accountID, err := codexSubscriptionPublicID(request.AccountID, "codex subscription account id is invalid")
	if err != nil {
		return CodexSubscriptionMutation{}, err
	}
	if !validCodexAccountBindingTimestamp(request.NowMS) {
		return CodexSubscriptionMutation{}, invalidRecord("codex subscription timestamp is invalid")
	}
	var newManualID string
	if request.NewManualEntryID != nil {
		newManualID, err = codexSubscriptionPublicID(*request.NewManualEntryID, "codex subscription manual id is invalid")
		if err != nil {
			return CodexSubscriptionMutation{}, err
		}
	}
	return repository.writeCodexSubscriptionMutation(ctx, func(
		ctx context.Context, transaction *gorm.DB,
	) (CodexSubscriptionMutation, error) {
		if detected, ok, err := loadDetectedByPublicID(ctx, transaction, accountID); err != nil {
			return CodexSubscriptionMutation{}, err
		} else if ok {
			return updateDetectedManual(ctx, transaction, detected, request, newManualID)
		}
		manual, ok, err := loadManualByID(ctx, transaction, accountID)
		if err != nil {
			return CodexSubscriptionMutation{}, err
		}
		if !ok {
			return CodexSubscriptionMutation{}, invalidRecord("codex subscription account is not found")
		}
		_, linked, err := loadLinkByManualID(ctx, transaction, manual.ManualEntryID)
		if err != nil {
			return CodexSubscriptionMutation{}, err
		}
		if linked {
			return CodexSubscriptionMutation{}, invalidRecord("codex subscription account is not found")
		}
		return updateStandaloneManual(ctx, transaction, manual, request)
	})
}

func (repository *Repository) DeleteCodexSubscriptionAccount(
	ctx context.Context,
	request CodexSubscriptionDeleteRequest,
) (CodexSubscriptionMutation, error) {
	if repository == nil || repository.database == nil {
		return CodexSubscriptionMutation{}, ErrInvalidRepository
	}
	accountID, err := codexSubscriptionPublicID(request.AccountID, "codex subscription account id is invalid")
	if err != nil {
		return CodexSubscriptionMutation{}, err
	}
	for _, revision := range []*int64{
		request.ExpectedDetectedRevision,
		request.ExpectedManualRevision,
		request.ExpectedLinkRevision,
	} {
		if revision != nil {
			if err := subscriptionaccounts.ParseRevision(*revision); err != nil {
				return CodexSubscriptionMutation{}, invalidCodexSubscriptionRecord(
					"codex subscription revision is invalid", err,
				)
			}
		}
	}
	return repository.writeCodexSubscriptionMutation(ctx, func(
		ctx context.Context, transaction *gorm.DB,
	) (CodexSubscriptionMutation, error) {
		detected, found, err := loadDetectedByPublicID(ctx, transaction, accountID)
		if err != nil {
			return CodexSubscriptionMutation{}, err
		}
		if found {
			return deleteDetectedCodexSubscriptionAccount(ctx, transaction, detected, request)
		}
		manual, found, err := loadManualByID(ctx, transaction, accountID)
		if err != nil {
			return CodexSubscriptionMutation{}, err
		}
		if !found {
			return subscriptionMutation(subscriptionaccounts.MutationNoop, ""), nil
		}
		return deleteManualCodexSubscriptionAccount(ctx, transaction, manual, request)
	})
}

func deleteDetectedCodexSubscriptionAccount(
	ctx context.Context,
	transaction *gorm.DB,
	detected codexSubscriptionDetectedAccountModel,
	request CodexSubscriptionDeleteRequest,
) (CodexSubscriptionMutation, error) {
	binding, err := loadStoredCodexAccountBinding(ctx, transaction)
	if err != nil {
		return CodexSubscriptionMutation{}, err
	}
	if binding.State == CodexAccountBindingConfirmed &&
		binding.AccountScope != nil && *binding.AccountScope == detected.AccountScope {
		return subscriptionConflict(subscriptionaccounts.ReasonCurrentAccount), nil
	}
	if request.ExpectedDetectedRevision == nil ||
		*request.ExpectedDetectedRevision != detected.Revision {
		return subscriptionConflict(subscriptionaccounts.ReasonRevisionChanged), nil
	}
	link, linked, err := loadLinkByScope(ctx, transaction, detected.AccountScope)
	if err != nil {
		return CodexSubscriptionMutation{}, err
	}
	if !linked && (request.ExpectedManualRevision != nil || request.ExpectedLinkRevision != nil) {
		return subscriptionConflict(subscriptionaccounts.ReasonLinkTargetChanged), nil
	}
	if linked {
		manual, found, err := loadManualByID(ctx, transaction, link.ManualEntryID)
		if err != nil {
			return CodexSubscriptionMutation{}, err
		}
		if !found {
			return CodexSubscriptionMutation{}, invalidRecord("codex subscription account is not found")
		}
		if request.ExpectedManualRevision == nil ||
			*request.ExpectedManualRevision != manual.Revision ||
			request.ExpectedLinkRevision == nil ||
			*request.ExpectedLinkRevision != link.Revision {
			return subscriptionConflict(subscriptionaccounts.ReasonRevisionChanged), nil
		}
		if err := transaction.WithContext(ctx).
			Where("account_scope = ?", detected.AccountScope).
			Delete(&codexSubscriptionLinkModel{}).Error; err != nil {
			return CodexSubscriptionMutation{}, err
		}
		if err := transaction.WithContext(ctx).
			Where("manual_entry_id = ?", manual.ManualEntryID).
			Delete(&codexSubscriptionManualEntryModel{}).Error; err != nil {
			return CodexSubscriptionMutation{}, err
		}
	}
	if err := transaction.WithContext(ctx).
		Where("account_scope = ?", detected.AccountScope).
		Delete(&codexSubscriptionDetectedAccountModel{}).Error; err != nil {
		return CodexSubscriptionMutation{}, err
	}
	return subscriptionMutation(subscriptionaccounts.MutationApplied, ""), nil
}

func deleteManualCodexSubscriptionAccount(
	ctx context.Context,
	transaction *gorm.DB,
	manual codexSubscriptionManualEntryModel,
	request CodexSubscriptionDeleteRequest,
) (CodexSubscriptionMutation, error) {
	if request.ExpectedDetectedRevision != nil || request.ExpectedLinkRevision != nil {
		return subscriptionConflict(subscriptionaccounts.ReasonLinkTargetChanged), nil
	}
	if _, linked, err := loadLinkByManualID(ctx, transaction, manual.ManualEntryID); err != nil {
		return CodexSubscriptionMutation{}, err
	} else if linked {
		return subscriptionConflict(subscriptionaccounts.ReasonLinkTargetChanged), nil
	}
	if request.ExpectedManualRevision == nil ||
		*request.ExpectedManualRevision != manual.Revision {
		return subscriptionConflict(subscriptionaccounts.ReasonRevisionChanged), nil
	}
	if err := transaction.WithContext(ctx).
		Where("manual_entry_id = ?", manual.ManualEntryID).
		Delete(&codexSubscriptionManualEntryModel{}).Error; err != nil {
		return CodexSubscriptionMutation{}, err
	}
	return subscriptionMutation(subscriptionaccounts.MutationApplied, ""), nil
}

func (repository *Repository) LinkCodexSubscriptionAccount(
	ctx context.Context,
	request CodexSubscriptionLinkRequest,
) (CodexSubscriptionMutation, error) {
	if repository == nil || repository.database == nil {
		return CodexSubscriptionMutation{}, ErrInvalidRepository
	}
	detectedID, err := codexSubscriptionPublicID(request.DetectedAccountID, "codex subscription detected account id is invalid")
	if err != nil {
		return CodexSubscriptionMutation{}, err
	}
	manualID, err := codexSubscriptionPublicID(request.ManualEntryID, "codex subscription manual id is invalid")
	if err != nil {
		return CodexSubscriptionMutation{}, err
	}
	if err := subscriptionaccounts.ParseRevision(request.ExpectedManualRevision); err != nil {
		return CodexSubscriptionMutation{}, invalidCodexSubscriptionRecord(
			"codex subscription revision is invalid", err,
		)
	}
	if !validCodexAccountBindingTimestamp(request.NowMS) {
		return CodexSubscriptionMutation{}, invalidRecord("codex subscription timestamp is invalid")
	}
	return repository.writeCodexSubscriptionMutation(ctx, func(
		ctx context.Context, transaction *gorm.DB,
	) (CodexSubscriptionMutation, error) {
		pair, err := loadCodexSubscriptionPair(ctx, transaction, detectedID, manualID)
		if err != nil {
			return CodexSubscriptionMutation{}, err
		}
		if pair.linkedTogether() {
			return subscriptionMutation(subscriptionaccounts.MutationNoop, ""), nil
		}
		if pair.detectedLinked || pair.manualLinked {
			return subscriptionConflict(subscriptionaccounts.ReasonAlreadyLinked), nil
		}
		if pair.manual.Revision != request.ExpectedManualRevision {
			return subscriptionConflict(subscriptionaccounts.ReasonRevisionChanged), nil
		}
		if err := transaction.WithContext(ctx).Create(&codexSubscriptionLinkModel{
			AccountScope:  pair.detected.AccountScope,
			ManualEntryID: pair.manual.ManualEntryID,
			Revision:      1,
			LinkedAtMS:    request.NowMS,
			UpdatedAtMS:   request.NowMS,
		}).Error; err != nil {
			return CodexSubscriptionMutation{}, err
		}
		return subscriptionMutation(subscriptionaccounts.MutationApplied, ""), nil
	})
}

func (repository *Repository) UnlinkCodexSubscriptionAccount(
	ctx context.Context,
	request CodexSubscriptionUnlinkRequest,
) (CodexSubscriptionMutation, error) {
	if repository == nil || repository.database == nil {
		return CodexSubscriptionMutation{}, ErrInvalidRepository
	}
	detectedID, err := codexSubscriptionPublicID(request.DetectedAccountID, "codex subscription detected account id is invalid")
	if err != nil {
		return CodexSubscriptionMutation{}, err
	}
	manualID, err := codexSubscriptionPublicID(request.ManualEntryID, "codex subscription manual id is invalid")
	if err != nil {
		return CodexSubscriptionMutation{}, err
	}
	if err := subscriptionaccounts.ParseRevision(request.ExpectedManualRevision); err != nil {
		return CodexSubscriptionMutation{}, invalidCodexSubscriptionRecord(
			"codex subscription revision is invalid", err,
		)
	}
	if err := subscriptionaccounts.ParseRevision(request.ExpectedLinkRevision); err != nil {
		return CodexSubscriptionMutation{}, invalidCodexSubscriptionRecord(
			"codex subscription revision is invalid", err,
		)
	}
	if !validCodexAccountBindingTimestamp(request.NowMS) {
		return CodexSubscriptionMutation{}, invalidRecord("codex subscription timestamp is invalid")
	}
	return repository.writeCodexSubscriptionMutation(ctx, func(
		ctx context.Context, transaction *gorm.DB,
	) (CodexSubscriptionMutation, error) {
		pair, err := loadCodexSubscriptionPair(ctx, transaction, detectedID, manualID)
		if err != nil {
			return CodexSubscriptionMutation{}, err
		}
		if !pair.linkedTogether() {
			if !pair.detectedLinked && !pair.manualLinked {
				return subscriptionMutation(subscriptionaccounts.MutationNoop, ""), nil
			}
			return subscriptionConflict(subscriptionaccounts.ReasonLinkTargetChanged), nil
		}
		if err := subscriptionaccounts.RequireManualEmailForUnlink(
			subscriptionaccounts.ManualEntry(pair.manual),
		); err != nil {
			return subscriptionConflict(subscriptionaccounts.ReasonManualEmailRequiredBeforeUnlink), nil
		}
		if pair.manual.Revision != request.ExpectedManualRevision ||
			pair.detectedLink.Revision != request.ExpectedLinkRevision {
			return subscriptionConflict(subscriptionaccounts.ReasonRevisionChanged), nil
		}
		if err := transaction.WithContext(ctx).
			Where("account_scope = ? AND manual_entry_id = ?", pair.detected.AccountScope, pair.manual.ManualEntryID).
			Delete(&codexSubscriptionLinkModel{}).Error; err != nil {
			return CodexSubscriptionMutation{}, err
		}
		return subscriptionMutation(subscriptionaccounts.MutationApplied, ""), nil
	})
}

type codexSubscriptionPair struct {
	detected       codexSubscriptionDetectedAccountModel
	manual         codexSubscriptionManualEntryModel
	detectedLink   codexSubscriptionLinkModel
	manualLink     codexSubscriptionLinkModel
	detectedLinked bool
	manualLinked   bool
}

func (pair codexSubscriptionPair) linkedTogether() bool {
	return pair.detectedLinked && pair.manualLinked &&
		pair.detectedLink.ManualEntryID == pair.manual.ManualEntryID &&
		pair.manualLink.AccountScope == pair.detected.AccountScope
}

func loadCodexSubscriptionPair(
	ctx context.Context,
	transaction *gorm.DB,
	detectedID, manualID string,
) (codexSubscriptionPair, error) {
	detected, detectedFound, err := loadDetectedByPublicID(ctx, transaction, detectedID)
	if err != nil || !detectedFound {
		return codexSubscriptionPair{}, subscriptionAccountLookupError(err)
	}
	manual, manualFound, err := loadManualByID(ctx, transaction, manualID)
	if err != nil || !manualFound {
		return codexSubscriptionPair{}, subscriptionAccountLookupError(err)
	}
	detectedLink, detectedLinked, err := loadLinkByScope(ctx, transaction, detected.AccountScope)
	if err != nil {
		return codexSubscriptionPair{}, err
	}
	manualLink, manualLinked, err := loadLinkByManualID(ctx, transaction, manual.ManualEntryID)
	return codexSubscriptionPair{
		detected: detected, manual: manual,
		detectedLink: detectedLink, manualLink: manualLink,
		detectedLinked: detectedLinked, manualLinked: manualLinked,
	}, err
}

func subscriptionAccountLookupError(err error) error {
	if err != nil {
		return err
	}
	return invalidRecord("codex subscription account is not found")
}

func (repository *Repository) writeCodexSubscriptionMutation(
	ctx context.Context,
	operation func(context.Context, *gorm.DB) (CodexSubscriptionMutation, error),
) (CodexSubscriptionMutation, error) {
	var mutation CodexSubscriptionMutation
	err := repository.database.Write(ctx, func(ctx context.Context, transaction *gorm.DB) error {
		var err error
		mutation, err = operation(ctx, transaction)
		return err
	})
	return mutation, err
}

func updateDetectedManual(
	ctx context.Context,
	transaction *gorm.DB,
	detected codexSubscriptionDetectedAccountModel,
	request CodexSubscriptionManualUpdate,
	newManualID string,
) (CodexSubscriptionMutation, error) {
	normalized, err := normalizeCodexSubscriptionManual(
		request.Fields, subscriptionaccounts.ManualLinkedSupplement,
	)
	if err != nil {
		return CodexSubscriptionMutation{}, err
	}
	link, linked, err := loadLinkByScope(ctx, transaction, detected.AccountScope)
	if err != nil {
		return CodexSubscriptionMutation{}, err
	}
	if !linked {
		if newManualID == "" {
			return CodexSubscriptionMutation{}, invalidRecord("codex subscription manual id is invalid")
		}
		return createDetectedSupplement(ctx, transaction, detected, normalized, newManualID, request.NowMS)
	}
	manual, ok, err := loadManualByID(ctx, transaction, link.ManualEntryID)
	if err != nil {
		return CodexSubscriptionMutation{}, err
	}
	if !ok {
		return CodexSubscriptionMutation{}, invalidRecord("codex subscription account is not found")
	}
	if newManualID != "" {
		if newManualID != manual.ManualEntryID {
			return subscriptionConflict(subscriptionaccounts.ReasonRequestIDReused), nil
		}
		if manualMatchesNormalized(manual, normalized) {
			return subscriptionMutation(subscriptionaccounts.MutationNoop, ""), nil
		}
		return subscriptionConflict(subscriptionaccounts.ReasonRequestIDReused), nil
	}
	if manualMatchesNormalized(manual, normalized) {
		return subscriptionMutation(subscriptionaccounts.MutationNoop, ""), nil
	}
	if request.ExpectedManualRevision == nil || *request.ExpectedManualRevision != manual.Revision ||
		request.ExpectedLinkRevision == nil || *request.ExpectedLinkRevision != link.Revision {
		return subscriptionConflict(subscriptionaccounts.ReasonRevisionChanged), nil
	}
	nextManualRevision, err := nextSubscriptionRevision(manual.Revision)
	if err != nil {
		return CodexSubscriptionMutation{}, err
	}
	nextLinkRevision, err := nextSubscriptionRevision(link.Revision)
	if err != nil {
		return CodexSubscriptionMutation{}, err
	}
	if request.NowMS < manual.CreatedAtMS || request.NowMS < link.LinkedAtMS {
		return CodexSubscriptionMutation{}, invalidRecord("codex subscription timestamp is invalid")
	}
	if err := persistManualNormalized(ctx, transaction, manual.ManualEntryID, normalized, nextManualRevision, request.NowMS); err != nil {
		return CodexSubscriptionMutation{}, err
	}
	if err := transaction.WithContext(ctx).Model(&codexSubscriptionLinkModel{}).
		Where("account_scope = ?", detected.AccountScope).
		Updates(map[string]any{
			"revision":      nextLinkRevision,
			"updated_at_ms": request.NowMS,
		}).Error; err != nil {
		return CodexSubscriptionMutation{}, err
	}
	return subscriptionMutation(subscriptionaccounts.MutationApplied, ""), nil
}

func createDetectedSupplement(
	ctx context.Context,
	transaction *gorm.DB,
	detected codexSubscriptionDetectedAccountModel,
	normalized subscriptionaccounts.NormalizedManual,
	newManualID string,
	nowMS int64,
) (CodexSubscriptionMutation, error) {
	existing, found, err := loadManualByID(ctx, transaction, newManualID)
	if err != nil {
		return CodexSubscriptionMutation{}, err
	}
	if found {
		link, linked, err := loadLinkByManualID(ctx, transaction, existing.ManualEntryID)
		if err != nil {
			return CodexSubscriptionMutation{}, err
		}
		if linked && link.AccountScope == detected.AccountScope && manualMatchesNormalized(existing, normalized) {
			return subscriptionMutation(subscriptionaccounts.MutationNoop, ""), nil
		}
		return subscriptionConflict(subscriptionaccounts.ReasonRequestIDReused), nil
	}
	if nowMS < 0 {
		return CodexSubscriptionMutation{}, invalidRecord("codex subscription timestamp is invalid")
	}
	model := manualModelFromNormalized(newManualID, normalized, 1, nowMS, nowMS)
	if err := transaction.WithContext(ctx).Create(&model).Error; err != nil {
		return CodexSubscriptionMutation{}, err
	}
	if err := transaction.WithContext(ctx).Create(&codexSubscriptionLinkModel{
		AccountScope:  detected.AccountScope,
		ManualEntryID: newManualID,
		Revision:      1,
		LinkedAtMS:    nowMS,
		UpdatedAtMS:   nowMS,
	}).Error; err != nil {
		return CodexSubscriptionMutation{}, err
	}
	return subscriptionMutation(subscriptionaccounts.MutationApplied, ""), nil
}

func updateStandaloneManual(
	ctx context.Context,
	transaction *gorm.DB,
	manual codexSubscriptionManualEntryModel,
	request CodexSubscriptionManualUpdate,
) (CodexSubscriptionMutation, error) {
	if request.NewManualEntryID != nil {
		return CodexSubscriptionMutation{}, invalidRecord("codex subscription manual id is invalid")
	}
	normalized, err := normalizeCodexSubscriptionManual(request.Fields, subscriptionaccounts.ManualStandalone)
	if err != nil {
		return CodexSubscriptionMutation{}, err
	}
	if manualMatchesNormalized(manual, normalized) {
		return subscriptionMutation(subscriptionaccounts.MutationNoop, ""), nil
	}
	if request.ExpectedManualRevision == nil || *request.ExpectedManualRevision != manual.Revision {
		return subscriptionConflict(subscriptionaccounts.ReasonRevisionChanged), nil
	}
	nextRevision, err := nextSubscriptionRevision(manual.Revision)
	if err != nil {
		return CodexSubscriptionMutation{}, err
	}
	if request.NowMS < manual.CreatedAtMS {
		return CodexSubscriptionMutation{}, invalidRecord("codex subscription timestamp is invalid")
	}
	if err := persistManualNormalized(ctx, transaction, manual.ManualEntryID, normalized, nextRevision, request.NowMS); err != nil {
		return CodexSubscriptionMutation{}, err
	}
	return subscriptionMutation(subscriptionaccounts.MutationApplied, ""), nil
}

func persistManualNormalized(
	ctx context.Context,
	transaction *gorm.DB,
	manualEntryID string,
	normalized subscriptionaccounts.NormalizedManual,
	revision, updatedAtMS int64,
) error {
	return transaction.WithContext(ctx).Model(&codexSubscriptionManualEntryModel{}).
		Where("manual_entry_id = ?", manualEntryID).
		Updates(map[string]any{
			"email":           nullableString(normalized.Email),
			"email_match_key": nullableString(normalized.EmailMatchKey),
			"alias":           nullableString(normalized.Alias),
			"manual_plan":     nullableString(normalized.Plan),
			"membership_date": nullableString(normalized.MembershipDate),
			"date_kind":       nullableString(normalized.DateKind),
			"revision":        revision,
			"updated_at_ms":   updatedAtMS,
		}).Error
}

func loadDetectedByScope(
	ctx context.Context,
	transaction *gorm.DB,
	accountScope string,
) (codexSubscriptionDetectedAccountModel, error) {
	model, found, err := loadCodexSubscriptionModel[codexSubscriptionDetectedAccountModel](
		ctx, transaction, "account_scope", accountScope,
	)
	if !found && err == nil {
		return codexSubscriptionDetectedAccountModel{}, invalidRecord("codex subscription detected account is missing")
	}
	return model, err
}

func loadDetectedByPublicID(
	ctx context.Context,
	transaction *gorm.DB,
	detectedAccountID string,
) (codexSubscriptionDetectedAccountModel, bool, error) {
	return loadCodexSubscriptionModel[codexSubscriptionDetectedAccountModel](
		ctx, transaction, "detected_account_id", detectedAccountID,
	)
}

func loadManualByID(
	ctx context.Context,
	transaction *gorm.DB,
	manualEntryID string,
) (codexSubscriptionManualEntryModel, bool, error) {
	return loadCodexSubscriptionModel[codexSubscriptionManualEntryModel](
		ctx, transaction, "manual_entry_id", manualEntryID,
	)
}

func loadLinkByScope(
	ctx context.Context,
	transaction *gorm.DB,
	accountScope string,
) (codexSubscriptionLinkModel, bool, error) {
	return loadCodexSubscriptionModel[codexSubscriptionLinkModel](
		ctx, transaction, "account_scope", accountScope,
	)
}

func loadLinkByManualID(
	ctx context.Context,
	transaction *gorm.DB,
	manualEntryID string,
) (codexSubscriptionLinkModel, bool, error) {
	return loadCodexSubscriptionModel[codexSubscriptionLinkModel](
		ctx, transaction, "manual_entry_id", manualEntryID,
	)
}

func loadCodexSubscriptionModel[Model any](
	ctx context.Context,
	transaction *gorm.DB,
	column string,
	value any,
) (Model, bool, error) {
	var model Model
	err := transaction.WithContext(ctx).Where(column+" = ?", value).Take(&model).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return model, false, nil
	}
	return model, err == nil, err
}

func detectedAccountFromModel(model codexSubscriptionDetectedAccountModel) CodexSubscriptionDetectedAccount {
	return subscriptionaccounts.DetectedAccount(model)
}

func manualEntryFromModel(model codexSubscriptionManualEntryModel) subscriptionaccounts.ManualEntry {
	return subscriptionaccounts.ManualEntry(model)
}

func linkFromModel(model codexSubscriptionLinkModel) subscriptionaccounts.Link {
	return subscriptionaccounts.Link(model)
}

func manualModelFromNormalized(
	id string,
	normalized subscriptionaccounts.NormalizedManual,
	revision, createdAtMS, updatedAtMS int64,
) codexSubscriptionManualEntryModel {
	return codexSubscriptionManualEntryModel{
		ManualEntryID:  id,
		Email:          cloneQuotaString(normalized.Email),
		EmailMatchKey:  cloneQuotaString(normalized.EmailMatchKey),
		Alias:          cloneQuotaString(normalized.Alias),
		ManualPlan:     normalized.Plan,
		MembershipDate: cloneQuotaString(normalized.MembershipDate),
		DateKind:       normalized.DateKind,
		Revision:       revision,
		CreatedAtMS:    createdAtMS,
		UpdatedAtMS:    updatedAtMS,
	}
}

func manualMatchesNormalized(
	model codexSubscriptionManualEntryModel,
	normalized subscriptionaccounts.NormalizedManual,
) bool {
	return equalCodexSubscriptionPointer(model.Email, normalized.Email) &&
		equalCodexSubscriptionPointer(model.EmailMatchKey, normalized.EmailMatchKey) &&
		equalCodexSubscriptionPointer(model.Alias, normalized.Alias) &&
		equalCodexSubscriptionPointer(model.ManualPlan, normalized.Plan) &&
		equalCodexSubscriptionPointer(model.MembershipDate, normalized.MembershipDate) &&
		equalCodexSubscriptionPointer(model.DateKind, normalized.DateKind)
}

func nullableString[Value ~string](value *Value) any {
	if value == nil {
		return nil
	}
	return *value
}

func equalCodexSubscriptionPointer[Value comparable](left, right *Value) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
}

func subscriptionMutation(result subscriptionaccounts.MutationResult, reason string) CodexSubscriptionMutation {
	mutation := CodexSubscriptionMutation{Result: string(result)}
	if reason != "" {
		mutation.Reason = cloneQuotaString(&reason)
	}
	return mutation
}

func subscriptionConflict(reason string) CodexSubscriptionMutation {
	return subscriptionMutation(subscriptionaccounts.MutationConflict, reason)
}

func invalidCodexSubscriptionRecord(message string, cause error) error {
	return errors.Join(invalidRecord(message), cause)
}

func codexSubscriptionPublicID(value, message string) (string, error) {
	id, err := subscriptionaccounts.ParsePublicID(value)
	if err != nil {
		return "", invalidCodexSubscriptionRecord(message, err)
	}
	return id, nil
}

func normalizeCodexSubscriptionManual(
	fields subscriptionaccounts.ManualFields,
	role subscriptionaccounts.ManualRole,
) (subscriptionaccounts.NormalizedManual, error) {
	normalized, err := subscriptionaccounts.NormalizeManualFields(fields, role)
	if err != nil {
		return subscriptionaccounts.NormalizedManual{}, invalidCodexSubscriptionRecord(
			"codex subscription manual fields are invalid", err,
		)
	}
	return normalized, nil
}

func nextSubscriptionRevision(current int64) (int64, error) {
	if current <= 0 {
		return 0, invalidRecord("codex subscription revision is invalid")
	}
	if current == math.MaxInt64 {
		return 0, invalidRecord("codex subscription revision is exhausted")
	}
	return current + 1, nil
}

func cloneInt64Value(value int64) *int64 {
	copied := value
	return &copied
}
