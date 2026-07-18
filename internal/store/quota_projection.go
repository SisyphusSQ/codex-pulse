package store

import (
	"context"
	"errors"
	"math"
	"reflect"
	"sort"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const quotaEvidenceWriteBatchSize = 256

type quotaProjectionKey struct {
	accountScope string
	windowKind   QuotaWindowKind
	limitID      string
}

type quotaMigrationEvaluationTimeKey struct{}

func rebuildQuotaProjectionDuringMigration(ctx context.Context, database *gorm.DB) error {
	evaluatedAtMS, found := ctx.Value(quotaMigrationEvaluationTimeKey{}).(int64)
	if !found || evaluatedAtMS < 0 {
		return invalidRecord("quota migration trusted clock is unavailable")
	}
	repository := &Repository{}
	return repository.rebuildQuotaScopeProjectionInTransaction(
		ctx, database, QuotaAccountScopeDefault, evaluatedAtMS, defaultQuotaArbitrationRule(),
	)
}

// RebuildQuotaProjection recomputes every logical quota window from immutable
// observations using the repository's trusted wall clock in one queue-owned
// transaction.
func (repository *Repository) RebuildQuotaProjection(
	ctx context.Context,
	rule QuotaArbitrationRule,
) error {
	if repository == nil || repository.database == nil {
		return ErrInvalidRepository
	}
	evaluatedAtMS, err := repository.quotaEvaluationTimeMS()
	if err != nil {
		return err
	}
	return repository.database.Write(ctx, func(ctx context.Context, transaction *gorm.DB) error {
		return repository.rebuildQuotaScopeProjectionInTransaction(
			ctx, transaction.WithContext(ctx), QuotaAccountScopeDefault, evaluatedAtMS, rule,
		)
	})
}

func (repository *Repository) rebuildQuotaScopeProjectionInTransaction(
	ctx context.Context,
	database *gorm.DB,
	accountScope string,
	evaluatedAtMS int64,
	rule QuotaArbitrationRule,
) error {
	var models []quotaProjectionKeyModel
	if err := database.WithContext(ctx).Model(&quotaObservationModel{}).
		Distinct("account_scope", "window_kind", "limit_id").
		Where("account_scope = ? AND limit_id IS NOT NULL", accountScope).
		Order("window_kind").Order("limit_id").Find(&models).Error; err != nil {
		return err
	}
	for _, model := range models {
		key := quotaProjectionKey{
			accountScope: model.AccountScope, windowKind: QuotaWindowKind(model.WindowKind), limitID: model.LimitID,
		}
		if err := repository.rebuildQuotaWindowProjectionInTransaction(
			ctx, database, key, evaluatedAtMS, rule,
		); err != nil {
			return err
		}
	}
	return nil
}

func (repository *Repository) rebuildQuotaWindowProjectionInTransaction(
	ctx context.Context,
	database *gorm.DB,
	key quotaProjectionKey,
	evaluatedAtMS int64,
	rule QuotaArbitrationRule,
) error {
	if err := validateQuotaProjectionKey(key); err != nil {
		return err
	}
	var observationModels []quotaObservationModel
	if err := database.WithContext(ctx).Where(
		"account_scope = ? AND window_kind = ? AND limit_id = ?",
		key.accountScope, string(key.windowKind), key.limitID,
	).Order("last_observed_at_ms").Order("observation_id").Find(&observationModels).Error; err != nil {
		return err
	}
	if len(observationModels) == 0 {
		return invalidRecord("quota projection window has no observation")
	}
	observations := make([]QuotaObservation, 0, len(observationModels))
	for _, model := range observationModels {
		observation, err := quotaObservationFromModel(model)
		if err != nil {
			return err
		}
		observations = append(observations, observation)
	}
	sourceStates := make(map[QuotaSource]SourceState)
	if state, found, err := sourceStateByID(ctx, database, QuotaSourceInstanceWhamDefault); err != nil {
		return err
	} else if found {
		sourceStates[QuotaSourceWham] = state
	}
	projection, err := arbitrateQuotaWindowWithSourceStates(observations, evaluatedAtMS, rule, sourceStates)
	if err != nil {
		return err
	}
	currentModel, err := quotaCurrentModelFromDomain(projection.Current)
	if err != nil {
		return err
	}
	evidenceModels, err := quotaEvidenceModelsFromDomain(projection.Evidence)
	if err != nil {
		return err
	}

	var storedEvidence []quotaArbitrationEvidenceModel
	if err := database.WithContext(ctx).Where(
		"account_scope = ? AND window_kind = ? AND limit_id = ?",
		key.accountScope, string(key.windowKind), key.limitID,
	).Find(&storedEvidence).Error; err != nil {
		return err
	}
	storedByObservation := make(map[string]quotaArbitrationEvidenceModel, len(storedEvidence))
	for _, model := range storedEvidence {
		storedByObservation[model.ObservationID] = model
	}
	desiredByObservation := make(map[string]quotaArbitrationEvidenceModel, len(evidenceModels))
	changedEvidence := make([]quotaArbitrationEvidenceModel, 0, len(evidenceModels))
	for _, model := range evidenceModels {
		desiredByObservation[model.ObservationID] = model
		if stored, found := storedByObservation[model.ObservationID]; !found || !reflect.DeepEqual(stored, model) {
			changedEvidence = append(changedEvidence, model)
		}
	}
	staleObservationIDs := make([]string, 0)
	for observationID := range storedByObservation {
		if _, found := desiredByObservation[observationID]; !found {
			staleObservationIDs = append(staleObservationIDs, observationID)
		}
	}
	sort.Strings(staleObservationIDs)
	if len(staleObservationIDs) > 0 {
		if err := database.WithContext(ctx).Where(
			"account_scope = ? AND window_kind = ? AND limit_id = ? AND observation_id IN ?",
			key.accountScope, string(key.windowKind), key.limitID, staleObservationIDs,
		).Delete(&quotaArbitrationEvidenceModel{}).Error; err != nil {
			return err
		}
	}
	if err := repository.callQuotaProjectionHook("after_delete"); err != nil {
		return err
	}
	if err := database.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "account_scope"}, {Name: "window_kind"}, {Name: "limit_id"}},
		DoUpdates: clause.AssignmentColumns([]string{
			"observation_id", "effective_used_percent", "window_minutes", "resets_at_ms",
			"window_generation", "selected_source", "freshness_state", "conflict_state",
			"fresh_until_ms", "last_success_at_ms", "last_attempt_at_ms", "rule_version",
			"explanation_code", "evaluated_at_ms",
		}),
	}).Create(&currentModel).Error; err != nil {
		return err
	}
	if len(changedEvidence) > 0 {
		if err := database.WithContext(ctx).Clauses(clause.OnConflict{
			Columns: []clause.Column{
				{Name: "account_scope"}, {Name: "window_kind"}, {Name: "limit_id"}, {Name: "observation_id"},
			},
			DoUpdates: clause.AssignmentColumns([]string{
				"window_generation", "disposition", "reason", "explanation_code",
			}),
		}).CreateInBatches(&changedEvidence, quotaEvidenceWriteBatchSize).Error; err != nil {
			return err
		}
	}
	if err := repository.callQuotaProjectionHook("after_write"); err != nil {
		return err
	}
	return repository.verifyQuotaProjectionReadback(ctx, database, projection)
}

func (repository *Repository) callQuotaProjectionHook(stage string) error {
	if repository != nil && repository.quotaProjectionHook != nil {
		return repository.quotaProjectionHook(stage)
	}
	return nil
}

func (repository *Repository) callQuotaProjectionReadHook(stage string) error {
	if repository != nil && repository.quotaProjectionReadHook != nil {
		return repository.quotaProjectionReadHook(stage)
	}
	return nil
}

// QuotaCurrent reads one current row and applies only monotonic time-based
// degradation. It never upgrades a persisted suspicious/stale/expired state.
func (repository *Repository) QuotaCurrent(
	ctx context.Context,
	accountScope string,
	windowKind QuotaWindowKind,
	limitID string,
	evaluatedAtMS int64,
) (QuotaCurrent, error) {
	if repository == nil || repository.database == nil {
		return QuotaCurrent{}, ErrInvalidRepository
	}
	key := quotaProjectionKey{accountScope: accountScope, windowKind: windowKind, limitID: limitID}
	if err := validateQuotaProjectionKey(key); err != nil || evaluatedAtMS < 0 {
		if err != nil {
			return QuotaCurrent{}, err
		}
		return QuotaCurrent{}, invalidRecord("quota current evaluation time is invalid")
	}
	var current QuotaCurrent
	err := repository.database.View(ctx, func(ctx context.Context, connection *gorm.DB) error {
		// Store.View binds the read-only pool but does not itself establish a
		// multi-statement SQLite snapshot. Keep current, raw candidates, source
		// state, and evidence in one explicit transaction so a concurrent writer
		// cannot make the verifier compare two valid commits.
		return connection.WithContext(ctx).Transaction(func(transaction *gorm.DB) error {
			projection, err := repository.readAndVerifyQuotaProjection(ctx, transaction, key)
			if err != nil {
				return err
			}
			current = dynamicallyDegradeQuotaCurrent(projection.Current, evaluatedAtMS)
			return nil
		})
	})
	return current, err
}

// ListQuotaCurrent verifies and reads every current window from one explicit
// SQLite snapshot. Scheduler policy must not combine independently committed
// per-window reads when calculating the next reset.
func (repository *Repository) ListQuotaCurrent(
	ctx context.Context,
	accountScope string,
	evaluatedAtMS int64,
) ([]QuotaCurrent, error) {
	if repository == nil || repository.database == nil {
		return nil, ErrInvalidRepository
	}
	if accountScope != QuotaAccountScopeDefault || evaluatedAtMS < 0 {
		return nil, invalidRecord("quota current list input is invalid")
	}
	var currents []QuotaCurrent
	err := repository.database.View(ctx, func(ctx context.Context, connection *gorm.DB) error {
		return connection.WithContext(ctx).Transaction(func(transaction *gorm.DB) error {
			var models []quotaProjectionKeyModel
			if err := transaction.WithContext(ctx).Model(&quotaCurrentModel{}).
				Select("account_scope", "window_kind", "limit_id").
				Where("account_scope = ?", accountScope).
				Order("window_kind").Order("limit_id").Find(&models).Error; err != nil {
				return err
			}
			currents = make([]QuotaCurrent, 0, len(models))
			for _, model := range models {
				key := quotaProjectionKey{
					accountScope: model.AccountScope,
					windowKind:   QuotaWindowKind(model.WindowKind),
					limitID:      model.LimitID,
				}
				projection, err := repository.readAndVerifyQuotaProjection(ctx, transaction, key)
				if err != nil {
					return err
				}
				currents = append(currents, dynamicallyDegradeQuotaCurrent(projection.Current, evaluatedAtMS))
			}
			return nil
		})
	})
	return currents, err
}

func (repository *Repository) ListQuotaArbitrationEvidence(
	ctx context.Context,
	accountScope string,
	windowKind QuotaWindowKind,
	limitID string,
) ([]QuotaArbitrationEvidence, error) {
	if repository == nil || repository.database == nil {
		return nil, ErrInvalidRepository
	}
	key := quotaProjectionKey{accountScope: accountScope, windowKind: windowKind, limitID: limitID}
	if err := validateQuotaProjectionKey(key); err != nil {
		return nil, err
	}
	var evidence []QuotaArbitrationEvidence
	err := repository.database.View(ctx, func(ctx context.Context, connection *gorm.DB) error {
		return connection.WithContext(ctx).Transaction(func(transaction *gorm.DB) error {
			projection, err := repository.readAndVerifyQuotaProjection(ctx, transaction, key)
			if err != nil {
				return err
			}
			evidence = projection.Evidence
			return nil
		})
	})
	return evidence, err
}

func validateQuotaProjectionKey(key quotaProjectionKey) error {
	if key.accountScope != QuotaAccountScopeDefault || !validQuotaWindowKind(key.windowKind) ||
		key.limitID == "" || len(key.limitID) > 512 {
		return invalidRecord("quota projection key is invalid")
	}
	return nil
}

func quotaCurrentModelFromDomain(current QuotaCurrent) (quotaCurrentModel, error) {
	if err := validateQuotaCurrent(current); err != nil {
		return quotaCurrentModel{}, err
	}
	return quotaCurrentModel{
		AccountScope: current.AccountScope, WindowKind: string(current.WindowKind), LimitID: current.LimitID,
		ObservationID:        cloneQuotaString(current.ObservationID),
		EffectiveUsedPercent: cloneQuotaFloatPointer(current.EffectiveUsedPercent),
		WindowMinutes:        cloneQuotaInt64Pointer(current.WindowMinutes), ResetsAtMS: cloneQuotaInt64Pointer(current.ResetsAtMS),
		WindowGeneration: cloneQuotaInt64Pointer(current.WindowGeneration),
		SelectedSource:   optionalQuotaSourceString(current.SelectedSource),
		FreshnessState:   string(current.FreshnessState), ConflictState: string(current.ConflictState),
		FreshUntilMS: cloneQuotaInt64Pointer(current.FreshUntilMS), LastSuccessAtMS: cloneQuotaInt64Pointer(current.LastSuccessAtMS),
		LastAttemptAtMS: cloneQuotaInt64Pointer(current.LastAttemptAtMS), RuleVersion: current.RuleVersion,
		ExplanationCode: string(current.ExplanationCode), EvaluatedAtMS: current.EvaluatedAtMS,
	}, nil
}

func quotaCurrentFromModel(model quotaCurrentModel) (QuotaCurrent, error) {
	var source *QuotaSource
	if model.SelectedSource != nil {
		parsed := QuotaSource(*model.SelectedSource)
		source = &parsed
	}
	current := QuotaCurrent{
		AccountScope: model.AccountScope, WindowKind: QuotaWindowKind(model.WindowKind), LimitID: model.LimitID,
		ObservationID:        cloneQuotaString(model.ObservationID),
		EffectiveUsedPercent: cloneQuotaFloatPointer(model.EffectiveUsedPercent),
		WindowMinutes:        cloneQuotaInt64Pointer(model.WindowMinutes), ResetsAtMS: cloneQuotaInt64Pointer(model.ResetsAtMS),
		WindowGeneration: cloneQuotaInt64Pointer(model.WindowGeneration), SelectedSource: source,
		FreshnessState: QuotaCurrentFreshness(model.FreshnessState), ConflictState: QuotaConflictState(model.ConflictState),
		FreshUntilMS: cloneQuotaInt64Pointer(model.FreshUntilMS), LastSuccessAtMS: cloneQuotaInt64Pointer(model.LastSuccessAtMS),
		LastAttemptAtMS: cloneQuotaInt64Pointer(model.LastAttemptAtMS), RuleVersion: model.RuleVersion,
		ExplanationCode: QuotaExplanationCode(model.ExplanationCode), EvaluatedAtMS: model.EvaluatedAtMS,
	}
	if err := validateQuotaCurrent(current); err != nil {
		return QuotaCurrent{}, invalidRecord("stored quota current is invalid")
	}
	return current, nil
}

func validateQuotaCurrent(current QuotaCurrent) error {
	if err := validateQuotaProjectionKey(quotaProjectionKey{
		accountScope: current.AccountScope, windowKind: current.WindowKind, limitID: current.LimitID,
	}); err != nil || current.EvaluatedAtMS < 0 || current.RuleVersion == "" || len(current.RuleVersion) > 128 ||
		!validQuotaCurrentFreshness(current.FreshnessState) || !validQuotaConflictState(current.ConflictState) ||
		!validQuotaExplanation(current.ExplanationCode) {
		return invalidRecord("quota current is invalid")
	}
	if _, found := quotaArbitrationRuleByVersion(current.RuleVersion); !found {
		return invalidRecord("quota current rule version is unsupported")
	}
	selected := current.ObservationID != nil
	if selected != (current.EffectiveUsedPercent != nil) || selected != (current.WindowMinutes != nil) ||
		selected != (current.ResetsAtMS != nil) || selected != (current.WindowGeneration != nil) ||
		selected != (current.SelectedSource != nil) || selected != (current.FreshUntilMS != nil) ||
		selected != (current.LastSuccessAtMS != nil) {
		return invalidRecord("quota current selected fields are incomplete")
	}
	if current.ExplanationCode != quotaCurrentExplanation(current.FreshnessState, current.ConflictState) {
		return invalidRecord("quota current explanation is inconsistent")
	}
	if !selected {
		if current.FreshnessState != QuotaCurrentNeverLoaded || current.ConflictState != QuotaConflictNone ||
			current.ExplanationCode != QuotaExplanationUnavailable {
			return invalidRecord("quota current never-loaded state is inconsistent")
		}
		return nil
	}
	if current.FreshnessState == QuotaCurrentNeverLoaded {
		return invalidRecord("quota current selected value cannot be never-loaded")
	}
	if *current.ObservationID == "" || math.IsNaN(*current.EffectiveUsedPercent) ||
		math.IsInf(*current.EffectiveUsedPercent, 0) || *current.EffectiveUsedPercent < 0 ||
		*current.EffectiveUsedPercent > 100 || *current.WindowMinutes <= 0 ||
		*current.WindowMinutes > maxQuotaWindowMinutes || *current.ResetsAtMS < 0 ||
		*current.WindowGeneration < 0 || *current.WindowGeneration != *current.ResetsAtMS ||
		!validQuotaSource(*current.SelectedSource) ||
		*current.FreshUntilMS > *current.ResetsAtMS || current.LastAttemptAtMS == nil ||
		*current.LastSuccessAtMS > *current.LastAttemptAtMS {
		return invalidRecord("quota current selected value is invalid")
	}
	return nil
}

func quotaEvidenceModelsFromDomain(evidence []QuotaArbitrationEvidence) ([]quotaArbitrationEvidenceModel, error) {
	models := make([]quotaArbitrationEvidenceModel, 0, len(evidence))
	for _, item := range evidence {
		if err := validateQuotaEvidence(item); err != nil {
			return nil, err
		}
		models = append(models, quotaArbitrationEvidenceModel{
			AccountScope: item.AccountScope, WindowKind: string(item.WindowKind), LimitID: item.LimitID,
			ObservationID: item.ObservationID, WindowGeneration: *item.WindowGeneration,
			Disposition: string(item.Disposition), Reason: optionalQuotaReasonString(item.Reason),
			ExplanationCode: string(item.ExplanationCode),
		})
	}
	return models, nil
}

func quotaEvidenceFromModel(model quotaArbitrationEvidenceModel) (QuotaArbitrationEvidence, error) {
	generation := model.WindowGeneration
	var reason *QuotaRejectionReason
	if model.Reason != nil {
		parsed := QuotaRejectionReason(*model.Reason)
		reason = &parsed
	}
	evidence := QuotaArbitrationEvidence{
		AccountScope: model.AccountScope, WindowKind: QuotaWindowKind(model.WindowKind), LimitID: model.LimitID,
		ObservationID: model.ObservationID, WindowGeneration: &generation,
		Disposition: QuotaEvidenceDisposition(model.Disposition), Reason: reason,
		ExplanationCode: QuotaExplanationCode(model.ExplanationCode),
	}
	if err := validateQuotaEvidence(evidence); err != nil {
		return QuotaArbitrationEvidence{}, invalidRecord("stored quota arbitration evidence is invalid")
	}
	return evidence, nil
}

func validateQuotaEvidence(evidence QuotaArbitrationEvidence) error {
	if err := validateQuotaProjectionKey(quotaProjectionKey{
		accountScope: evidence.AccountScope, windowKind: evidence.WindowKind, limitID: evidence.LimitID,
	}); err != nil || evidence.ObservationID == "" || evidence.WindowGeneration == nil || *evidence.WindowGeneration < 0 ||
		!validQuotaEvidenceDisposition(evidence.Disposition) || !validQuotaExplanation(evidence.ExplanationCode) {
		return invalidRecord("quota arbitration evidence is invalid")
	}
	switch evidence.Disposition {
	case QuotaEvidenceSuspicious, QuotaEvidenceRejected:
		if evidence.Reason == nil || !validQuotaRejectionReason(*evidence.Reason) {
			return invalidRecord("quota arbitration finding needs an allowlisted reason")
		}
	case QuotaEvidenceSelected, QuotaEvidenceEligible:
		if evidence.Reason != nil && *evidence.Reason != QuotaReasonSourceConflict {
			return invalidRecord("quota arbitration eligible reason is invalid")
		}
	case QuotaEvidenceSuperseded:
		if evidence.Reason != nil {
			return invalidRecord("quota arbitration superseded reason is invalid")
		}
	}
	if evidence.ExplanationCode != quotaEvidenceDomainExplanation(evidence) {
		return invalidRecord("quota arbitration evidence explanation is inconsistent")
	}
	return nil
}

func quotaEvidenceDomainExplanation(evidence QuotaArbitrationEvidence) QuotaExplanationCode {
	if evidence.Reason != nil && *evidence.Reason == QuotaReasonSourceConflict {
		return QuotaExplanationSourceConflict
	}
	switch evidence.Disposition {
	case QuotaEvidenceSelected, QuotaEvidenceEligible, QuotaEvidenceSuperseded:
		return QuotaExplanationTrusted
	case QuotaEvidenceSuspicious:
		return QuotaExplanationSuspicious
	default:
		return QuotaExplanationUnavailable
	}
}

func (repository *Repository) readAndVerifyQuotaProjection(
	ctx context.Context,
	database *gorm.DB,
	key quotaProjectionKey,
) (quotaWindowProjection, error) {
	var currentModel quotaCurrentModel
	err := database.WithContext(ctx).Take(
		&currentModel,
		"account_scope = ? AND window_kind = ? AND limit_id = ?",
		key.accountScope, string(key.windowKind), key.limitID,
	).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return quotaWindowProjection{}, ErrNotFound
	}
	if err != nil {
		return quotaWindowProjection{}, err
	}
	current, err := quotaCurrentFromModel(currentModel)
	if err != nil {
		return quotaWindowProjection{}, err
	}
	rule, found := quotaArbitrationRuleByVersion(current.RuleVersion)
	if !found {
		return quotaWindowProjection{}, invalidRecord("stored quota projection rule is unsupported")
	}
	if err := repository.callQuotaProjectionReadHook("after_current"); err != nil {
		return quotaWindowProjection{}, err
	}

	var observationModels []quotaObservationModel
	if err := database.WithContext(ctx).Where(
		"account_scope = ? AND window_kind = ? AND limit_id = ?",
		key.accountScope, string(key.windowKind), key.limitID,
	).Order("last_observed_at_ms").Order("observation_id").Find(&observationModels).Error; err != nil {
		return quotaWindowProjection{}, err
	}
	if len(observationModels) == 0 {
		return quotaWindowProjection{}, invalidRecord("stored quota projection has no raw observation")
	}
	observations := make([]QuotaObservation, 0, len(observationModels))
	for _, model := range observationModels {
		observation, err := quotaObservationFromModel(model)
		if err != nil {
			return quotaWindowProjection{}, invalidRecord("stored quota projection observation is invalid")
		}
		observations = append(observations, observation)
	}
	sourceStates := make(map[QuotaSource]SourceState)
	if state, found, err := sourceStateByID(ctx, database, QuotaSourceInstanceWhamDefault); err != nil {
		return quotaWindowProjection{}, err
	} else if found {
		sourceStates[QuotaSourceWham] = state
	}
	want, err := arbitrateQuotaWindowWithSourceStates(observations, current.EvaluatedAtMS, rule, sourceStates)
	if err != nil {
		return quotaWindowProjection{}, err
	}

	var evidenceModels []quotaArbitrationEvidenceModel
	if err := database.WithContext(ctx).Where(
		"account_scope = ? AND window_kind = ? AND limit_id = ?",
		key.accountScope, string(key.windowKind), key.limitID,
	).Order("observation_id").Find(&evidenceModels).Error; err != nil {
		return quotaWindowProjection{}, err
	}
	evidence := make([]QuotaArbitrationEvidence, 0, len(evidenceModels))
	for _, model := range evidenceModels {
		mapped, err := quotaEvidenceFromModel(model)
		if err != nil {
			return quotaWindowProjection{}, err
		}
		evidence = append(evidence, mapped)
	}
	stored := quotaWindowProjection{Current: current, Evidence: evidence}
	if !reflect.DeepEqual(stored, want) {
		return quotaWindowProjection{}, invalidRecord("stored quota projection differs from recomputed arbitration")
	}
	return stored, nil
}

func quotaArbitrationRuleByVersion(version string) (QuotaArbitrationRule, bool) {
	rule := defaultQuotaArbitrationRule()
	if version != rule.Version {
		return QuotaArbitrationRule{}, false
	}
	return rule, true
}

// verifyQuotaProjectionReadback checks the exact rows written by the current
// transaction without repeating arbitration over the immutable observations.
// Every public projection reader still calls readAndVerifyQuotaProjection and
// recomputes from raw observations, so a later mismatch remains fail closed.
func (repository *Repository) verifyQuotaProjectionReadback(
	ctx context.Context,
	database *gorm.DB,
	want quotaWindowProjection,
) error {
	key := quotaProjectionKey{
		accountScope: want.Current.AccountScope, windowKind: want.Current.WindowKind, limitID: want.Current.LimitID,
	}
	var currentModel quotaCurrentModel
	if err := database.WithContext(ctx).Take(
		&currentModel,
		"account_scope = ? AND window_kind = ? AND limit_id = ?",
		key.accountScope, string(key.windowKind), key.limitID,
	).Error; err != nil {
		return err
	}
	current, err := quotaCurrentFromModel(currentModel)
	if err != nil {
		return err
	}
	var evidenceModels []quotaArbitrationEvidenceModel
	if err := database.WithContext(ctx).Where(
		"account_scope = ? AND window_kind = ? AND limit_id = ?",
		key.accountScope, string(key.windowKind), key.limitID,
	).Order("observation_id").Find(&evidenceModels).Error; err != nil {
		return err
	}
	evidence := make([]QuotaArbitrationEvidence, 0, len(evidenceModels))
	for _, model := range evidenceModels {
		mapped, err := quotaEvidenceFromModel(model)
		if err != nil {
			return err
		}
		evidence = append(evidence, mapped)
	}
	stored := quotaWindowProjection{Current: current, Evidence: evidence}
	if !reflect.DeepEqual(stored, want) {
		return invalidRecord("stored quota projection differs from arbitration write result")
	}
	return nil
}

func dynamicallyDegradeQuotaCurrent(current QuotaCurrent, evaluatedAtMS int64) QuotaCurrent {
	current.EvaluatedAtMS = evaluatedAtMS
	if current.ObservationID == nil {
		return current
	}
	if current.ResetsAtMS != nil && evaluatedAtMS >= *current.ResetsAtMS {
		current.FreshnessState = QuotaCurrentExpiredUnknown
	} else if current.FreshnessState == QuotaCurrentFresh && current.FreshUntilMS != nil && evaluatedAtMS > *current.FreshUntilMS {
		current.FreshnessState = QuotaCurrentStale
	}
	current.ExplanationCode = quotaCurrentExplanation(current.FreshnessState, current.ConflictState)
	return current
}

func validQuotaCurrentFreshness(value QuotaCurrentFreshness) bool {
	switch value {
	case QuotaCurrentNeverLoaded, QuotaCurrentFresh, QuotaCurrentStale, QuotaCurrentExpiredUnknown, QuotaCurrentSuspicious:
		return true
	default:
		return false
	}
}

func validQuotaConflictState(value QuotaConflictState) bool {
	return value == QuotaConflictNone || value == QuotaConflictPresent
}

func validQuotaExplanation(value QuotaExplanationCode) bool {
	switch value {
	case QuotaExplanationTrusted, QuotaExplanationStale, QuotaExplanationExpired,
		QuotaExplanationSuspicious, QuotaExplanationSourceConflict, QuotaExplanationUnavailable:
		return true
	default:
		return false
	}
}

func validQuotaEvidenceDisposition(value QuotaEvidenceDisposition) bool {
	switch value {
	case QuotaEvidenceSelected, QuotaEvidenceEligible, QuotaEvidenceSuperseded,
		QuotaEvidenceSuspicious, QuotaEvidenceRejected:
		return true
	default:
		return false
	}
}

func optionalQuotaSourceString(source *QuotaSource) *string {
	if source == nil {
		return nil
	}
	value := string(*source)
	return &value
}

func cloneQuotaFloatPointer(value *float64) *float64 {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}
