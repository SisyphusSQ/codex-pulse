package store

import (
	"context"
	"reflect"

	"gorm.io/gorm"

	"github.com/SisyphusSQ/codex-pulse/internal/runtimeclock"
)

// CodexAccountQuotaRecords loads detected account metadata and every retained
// quota projection from one SQLite read snapshot.
func (repository *Repository) CodexAccountQuotaRecords(
	ctx context.Context,
	evaluatedAtMS int64,
) (CodexAccountQuotaRecords, error) {
	if repository == nil || repository.database == nil {
		return CodexAccountQuotaRecords{}, ErrInvalidRepository
	}
	if evaluatedAtMS < 0 || evaluatedAtMS > runtimeclock.MaxTimestampMS {
		return CodexAccountQuotaRecords{}, invalidRecord("codex account quota evaluation time is invalid")
	}
	var records CodexAccountQuotaRecords
	err := repository.database.View(ctx, func(ctx context.Context, connection *gorm.DB) error {
		return connection.WithContext(ctx).Transaction(func(transaction *gorm.DB) error {
			subscriptions, err := readCodexSubscriptionRecords(ctx, transaction)
			if err != nil {
				return err
			}
			records.Subscriptions = subscriptions
			records.Accounts = make([]CodexDetectedAccountQuotaRecord, 0, len(subscriptions.Detected))
			for _, detected := range subscriptions.Detected {
				windows, err := readCodexAccountQuotaWindows(
					ctx, repository, transaction, detected.AccountScope, evaluatedAtMS,
				)
				if err != nil {
					return err
				}
				records.Accounts = append(records.Accounts, CodexDetectedAccountQuotaRecord{
					DetectedAccountID: detected.DetectedAccountID,
					Windows:           windows,
				})
			}
			return nil
		})
	})
	return records, err
}

func readCodexAccountQuotaWindows(
	ctx context.Context,
	repository *Repository,
	database *gorm.DB,
	accountScope string,
	evaluatedAtMS int64,
) ([]QuotaCurrent, error) {
	observationKeys, err := quotaQueryProjectionKeys(
		ctx, database, &quotaObservationModel{}, accountScope, "limit_id IS NOT NULL",
	)
	if err != nil {
		return nil, err
	}
	currentKeys, err := quotaQueryProjectionKeys(ctx, database, &quotaCurrentModel{}, accountScope, "")
	if err != nil {
		return nil, err
	}
	evidenceKeys, err := quotaQueryProjectionKeys(
		ctx, database, &quotaArbitrationEvidenceModel{}, accountScope, "",
	)
	if err != nil {
		return nil, err
	}
	if !reflect.DeepEqual(observationKeys, currentKeys) || !reflect.DeepEqual(observationKeys, evidenceKeys) {
		return nil, ErrNotFound
	}
	windows := make([]QuotaCurrent, 0, len(currentKeys))
	for _, key := range currentKeys {
		projection, err := repository.readAndVerifyQuotaProjection(ctx, database, key)
		if err != nil {
			return nil, err
		}
		if retiredCodexQuotaLimit(key.limitID) {
			continue
		}
		windows = append(windows, dynamicallyDegradeQuotaCurrent(projection.Current, evaluatedAtMS))
	}
	return windows, nil
}

// ClearHistoricalCodexAccountQuota removes retained quota facts for every
// detected account except the currently confirmed account.
func (repository *Repository) ClearHistoricalCodexAccountQuota(
	ctx context.Context,
) (CodexAccountQuotaPurgeResult, error) {
	if repository == nil || repository.database == nil {
		return CodexAccountQuotaPurgeResult{}, ErrInvalidRepository
	}
	var result CodexAccountQuotaPurgeResult
	err := repository.database.Write(ctx, func(ctx context.Context, transaction *gorm.DB) error {
		binding, err := loadStoredCodexAccountBinding(ctx, transaction)
		if err != nil {
			return err
		}
		var preservedScope string
		switch binding.State {
		case CodexAccountBindingConfirmed:
			if binding.AccountScope != nil {
				preservedScope = *binding.AccountScope
			}
		case CodexAccountBindingPending, CodexAccountBindingIdentityUnavailable:
			if binding.LastConfirmedScope != nil {
				preservedScope = *binding.LastConfirmedScope
			}
		}
		var scopes []string
		if err := transaction.WithContext(ctx).Model(&codexSubscriptionDetectedAccountModel{}).
			Order("account_scope").Pluck("account_scope", &scopes).Error; err != nil {
			return err
		}
		for _, scope := range scopes {
			if scope == preservedScope {
				continue
			}
			purged, err := purgeCodexAccountQuotaFacts(ctx, transaction, scope)
			if err != nil {
				return err
			}
			if purged.WindowCount+purged.ObservationCount+purged.ResetSnapshotCount > 0 {
				result.AccountCount++
			}
			result.WindowCount += purged.WindowCount
			result.ObservationCount += purged.ObservationCount
			result.ResetSnapshotCount += purged.ResetSnapshotCount
		}
		return nil
	})
	return result, err
}

func purgeCodexAccountQuotaFacts(
	ctx context.Context,
	database *gorm.DB,
	accountScope string,
) (CodexAccountQuotaPurgeResult, error) {
	if database == nil || !validDerivedCodexAccountScope(accountScope) {
		return CodexAccountQuotaPurgeResult{}, invalidRecord("codex account quota purge scope is invalid")
	}
	result := CodexAccountQuotaPurgeResult{}
	if err := database.WithContext(ctx).Model(&quotaCurrentModel{}).
		Where("account_scope = ?", accountScope).Count(&result.WindowCount).Error; err != nil {
		return CodexAccountQuotaPurgeResult{}, err
	}
	if err := database.WithContext(ctx).Model(&quotaObservationModel{}).
		Where("account_scope = ?", accountScope).Count(&result.ObservationCount).Error; err != nil {
		return CodexAccountQuotaPurgeResult{}, err
	}
	if err := database.WithContext(ctx).Model(&resetCreditsSnapshotModel{}).
		Where("account_scope = ?", accountScope).Count(&result.ResetSnapshotCount).Error; err != nil {
		return CodexAccountQuotaPurgeResult{}, err
	}
	for _, deletion := range []struct {
		model any
	}{
		{model: &quotaArbitrationEvidenceModel{}},
		{model: &quotaCurrentModel{}},
		{model: &quotaObservationModel{}},
		{model: &resetCreditsSnapshotModel{}},
	} {
		if err := database.WithContext(ctx).Where("account_scope = ?", accountScope).
			Delete(deletion.model).Error; err != nil {
			return CodexAccountQuotaPurgeResult{}, err
		}
	}
	return result, nil
}
