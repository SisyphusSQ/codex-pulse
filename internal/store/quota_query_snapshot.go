package store

import (
	"context"
	"reflect"

	"gorm.io/gorm"

	"github.com/SisyphusSQ/codex-pulse/internal/runtimeclock"
)

// QuotaCurrentWindowSnapshot keeps one verified current projection next to the
// immutable observations and evidence that explain it. Callers must not join
// independently committed reader results into an official current response.
type QuotaCurrentWindowSnapshot struct {
	Current      QuotaCurrent
	Observations []QuotaObservation
	Evidence     []QuotaArbitrationEvidence
}

// QuotaCurrentSnapshot is the read-only fact boundary for the M5 quota query.
// Every field is loaded from one explicit SQLite read transaction.
type QuotaCurrentSnapshot struct {
	Binding             CodexAccountBinding
	AccountScope        string
	BindingGeneration   int64
	EvaluatedAtMS       int64
	Windows             []QuotaCurrentWindowSnapshot
	OnlineSourceState   *SourceState
	QuotaRefresh        *SourceRefreshSchedule
	ResetCredits        ResetCreditsSummary
	ResetCreditsRefresh *SourceRefreshSchedule
}

// QuotaCurrentSnapshot verifies all projectable observation/current/evidence
// keys and returns their query facts from one SQLite snapshot. A missing
// projection is recoverable through explicit RebuildQuotaProjection, but this
// query never writes or silently turns missing facts into an empty response.
// Codex callers must not pass an arbitrary HMAC scope; this method reads the
// current binding inside the same snapshot and ignores accountScope except for
// Cursor/Grok adapters that still construct in-memory default snapshots.
func (repository *Repository) QuotaCurrentSnapshot(
	ctx context.Context,
	accountScope string,
	evaluatedAtMS int64,
) (QuotaCurrentSnapshot, error) {
	if repository == nil || repository.database == nil {
		return QuotaCurrentSnapshot{}, ErrInvalidRepository
	}
	if evaluatedAtMS < 0 || evaluatedAtMS > runtimeclock.MaxTimestampMS {
		return QuotaCurrentSnapshot{}, invalidRecord("quota current snapshot input is invalid")
	}
	snapshot := QuotaCurrentSnapshot{EvaluatedAtMS: evaluatedAtMS}
	err := repository.database.View(ctx, func(ctx context.Context, connection *gorm.DB) error {
		return connection.WithContext(ctx).Transaction(func(transaction *gorm.DB) error {
			binding, err := loadStoredCodexAccountBinding(ctx, transaction)
			if err != nil {
				return err
			}
			snapshot.Binding = binding.CodexAccountBinding
			snapshot.BindingGeneration = binding.BindingGeneration
			if binding.State != CodexAccountBindingConfirmed || binding.AccountScope == nil {
				snapshot.Windows = []QuotaCurrentWindowSnapshot{}
				snapshot.ResetCredits = ResetCreditsSummary{AccountScope: ""}
				return nil
			}
			scope := *binding.AccountScope
			if accountScope != "" && accountScope != QuotaAccountScopeDefault && accountScope != scope {
				return invalidRecord("quota current snapshot input is invalid")
			}
			snapshot.AccountScope = scope
			observationKeys, err := quotaQueryProjectionKeys(
				ctx, transaction, &quotaObservationModel{}, scope, "limit_id IS NOT NULL",
			)
			if err != nil {
				return err
			}
			currentKeys, err := quotaQueryProjectionKeys(ctx, transaction, &quotaCurrentModel{}, scope, "")
			if err != nil {
				return err
			}
			evidenceKeys, err := quotaQueryProjectionKeys(
				ctx, transaction, &quotaArbitrationEvidenceModel{}, scope, "",
			)
			if err != nil {
				return err
			}
			if !reflect.DeepEqual(observationKeys, currentKeys) || !reflect.DeepEqual(observationKeys, evidenceKeys) {
				return ErrNotFound
			}
			snapshot.Windows = make([]QuotaCurrentWindowSnapshot, 0, len(observationKeys))
			for _, key := range observationKeys {
				projection, err := repository.readAndVerifyQuotaProjection(ctx, transaction, key)
				if err != nil {
					return err
				}
				if retiredCodexQuotaLimit(key.limitID) {
					continue
				}
				observations, err := quotaQueryWindowObservations(ctx, transaction, key)
				if err != nil {
					return err
				}
				snapshot.Windows = append(snapshot.Windows, QuotaCurrentWindowSnapshot{
					Current:      dynamicallyDegradeQuotaCurrent(projection.Current, evaluatedAtMS),
					Observations: observations,
					Evidence:     projection.Evidence,
				})
			}

			quotaInstanceID := QuotaSourceInstanceAppServer(scope)
			resetInstanceID := ResetCreditsSourceInstanceAppServer(scope)
			state, found, err := sourceStateByID(ctx, transaction, quotaInstanceID)
			if err != nil {
				return err
			}
			if found {
				if state.SourceInstanceID != quotaInstanceID ||
					state.SourceType != QuotaSourceTypeAppServerRateLimits || state.ScopeKey != scope {
					return invalidRecord("stored quota source state identity is invalid")
				}
				if err := validateSourceState(state); err != nil {
					return invalidRecord("stored quota source state is invalid")
				}
				snapshot.OnlineSourceState = cloneQuotaQuerySourceState(state)
			}
			quotaRefresh, found, err := sourceRefreshScheduleByID(ctx, transaction, quotaInstanceID)
			if err != nil {
				return err
			}
			if found {
				snapshot.QuotaRefresh = cloneQuotaQueryRefreshSchedule(quotaRefresh)
			}

			resetCredits, err := resetCreditsSummaryFromDatabase(
				ctx, transaction, scope, evaluatedAtMS,
			)
			if err != nil {
				return err
			}
			snapshot.ResetCredits = resetCredits
			resetRefresh, found, err := sourceRefreshScheduleByID(ctx, transaction, resetInstanceID)
			if err != nil {
				return err
			}
			if found {
				snapshot.ResetCreditsRefresh = cloneQuotaQueryRefreshSchedule(resetRefresh)
			}
			return nil
		})
	})
	return snapshot, err
}

func quotaQueryProjectionKeys(
	ctx context.Context,
	database *gorm.DB,
	model any,
	accountScope string,
	extraWhere string,
) ([]quotaProjectionKey, error) {
	query := database.WithContext(ctx).Model(model).
		Distinct("account_scope", "window_kind", "limit_id").
		Where("account_scope = ?", accountScope)
	if extraWhere != "" {
		query = query.Where(extraWhere)
	}
	var models []quotaProjectionKeyModel
	if err := query.Order("window_kind").Order("limit_id").Find(&models).Error; err != nil {
		return nil, err
	}
	keys := make([]quotaProjectionKey, 0, len(models))
	for _, model := range models {
		key := quotaProjectionKey{
			accountScope: model.AccountScope,
			windowKind:   QuotaWindowKind(model.WindowKind),
			limitID:      model.LimitID,
		}
		if err := validateQuotaProjectionKey(key); err != nil {
			return nil, err
		}
		keys = append(keys, key)
	}
	return keys, nil
}

func quotaQueryWindowObservations(
	ctx context.Context,
	database *gorm.DB,
	key quotaProjectionKey,
) ([]QuotaObservation, error) {
	var models []quotaObservationModel
	if err := database.WithContext(ctx).Where(
		"account_scope = ? AND window_kind = ? AND limit_id = ?",
		key.accountScope, string(key.windowKind), key.limitID,
	).Order("last_observed_at_ms").Order("observation_id").Find(&models).Error; err != nil {
		return nil, err
	}
	observations := make([]QuotaObservation, 0, len(models))
	for _, model := range models {
		observation, err := quotaObservationFromModel(model)
		if err != nil {
			return nil, err
		}
		observations = append(observations, observation)
	}
	return observations, nil
}

func cloneQuotaQuerySourceState(value SourceState) *SourceState {
	cloned := value
	cloned.LastAttemptAtMS = cloneQuotaInt64Pointer(value.LastAttemptAtMS)
	cloned.LastSuccessAtMS = cloneQuotaInt64Pointer(value.LastSuccessAtMS)
	cloned.NextDueAtMS = cloneQuotaInt64Pointer(value.NextDueAtMS)
	cloned.LastErrorClass = cloneRuntimeErrorClass(value.LastErrorClass)
	cloned.LastFailureCode = cloneSourceFailureCode(value.LastFailureCode)
	return &cloned
}

func cloneQuotaQueryRefreshSchedule(value SourceRefreshSchedule) *SourceRefreshSchedule {
	cloned := value
	cloned.NextDueAtMS = cloneQuotaInt64Pointer(value.NextDueAtMS)
	cloned.LastManualAtMS = cloneQuotaInt64Pointer(value.LastManualAtMS)
	cloned.ActiveClaimID = cloneQuotaString(value.ActiveClaimID)
	cloned.ClaimStartedAtMS = cloneQuotaInt64Pointer(value.ClaimStartedAtMS)
	cloned.ClaimExpiresAtMS = cloneQuotaInt64Pointer(value.ClaimExpiresAtMS)
	if value.ActiveTrigger != nil {
		trigger := *value.ActiveTrigger
		cloned.ActiveTrigger = &trigger
	}
	return &cloned
}
