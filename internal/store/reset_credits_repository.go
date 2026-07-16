package store

import (
	"context"
	"errors"
	"math"
	"reflect"
	"sort"

	"gorm.io/gorm"

	"github.com/SisyphusSQ/codex-pulse/internal/runtimeclock"
	storesqlite "github.com/SisyphusSQ/codex-pulse/internal/store/sqlite"
)

// RecordResetCreditsFetch atomically appends one source attempt and its
// successful typed snapshot. Exact request replay is a no-op.
func (repository *Repository) RecordResetCreditsFetch(ctx context.Context, record ResetCreditsFetchRecord) error {
	if repository == nil || repository.database == nil {
		return ErrInvalidRepository
	}
	if err := validateResetCreditsFetchRecord(record); err != nil {
		return err
	}
	return repository.database.Write(ctx, func(ctx context.Context, transaction storesqlite.WriteTx) error {
		database := transaction.WithContext(ctx)
		existingAttempt, replay, err := sourceAttemptByID(ctx, database, record.Attempt.RequestID)
		if err != nil {
			return err
		}
		if replay {
			if !sourceAttemptsEqual(existingAttempt, record.Attempt) {
				return invalidRecord("reset credits fetch conflicts with append-only attempt")
			}
			return validateResetCreditsReplay(ctx, database, record)
		}
		if err := validateSourceRefreshClaimAttempt(
			ctx, database, record.Attempt.RequestID, record.SourceInstanceID,
		); err != nil {
			return err
		}

		existingState, found, err := sourceStateByID(ctx, database, record.SourceInstanceID)
		if err != nil {
			return err
		}
		if found && (existingState.SourceType != record.SourceType || existingState.ScopeKey != record.ScopeKey) {
			return invalidRecord("reset credits source stable identity conflicts")
		}
		projected, advance, err := projectOnlineSourceState(
			existingState, found, record.SourceInstanceID, record.SourceType, record.ScopeKey,
			record.Attempt, record.Snapshot != nil,
		)
		if err != nil {
			return err
		}
		if !found {
			model := sourceStateModelFromDomain(projected)
			if err := database.Create(&model).Error; err != nil {
				return err
			}
		} else if advance {
			model := sourceStateModelFromDomain(projected)
			result := database.Model(&sourceStateModel{}).
				Where("source_instance_id = ? AND updated_at_ms = ?", record.SourceInstanceID, existingState.UpdatedAtMS).
				Updates(sourceStateUpdates(model))
			if result.Error != nil {
				return result.Error
			}
			if result.RowsAffected != 1 {
				return invalidRecord("reset credits source state changed during writer transaction")
			}
		}
		attemptModel := sourceAttemptModelFromDomain(record.Attempt)
		if err := database.Create(&attemptModel).Error; err != nil {
			return err
		}
		if record.Snapshot == nil {
			return nil
		}
		snapshotModel := resetCreditsSnapshotModelFromDomain(*record.Snapshot)
		if err := database.Create(&snapshotModel).Error; err != nil {
			return err
		}
		models := make([]resetCreditModel, len(record.Snapshot.Credits))
		for index, credit := range record.Snapshot.Credits {
			models[index] = resetCreditModelFromDomain(record.Snapshot.SnapshotID, credit)
		}
		if len(models) > 0 {
			if err := database.CreateInBatches(&models, 100).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

// ResetCreditsSummary returns the newest successful snapshot and recomputes
// effective availability against evaluationAtMS in one read snapshot.
func (repository *Repository) ResetCreditsSummary(
	ctx context.Context,
	accountScope string,
	evaluationAtMS int64,
) (ResetCreditsSummary, error) {
	if repository == nil || repository.database == nil {
		return ResetCreditsSummary{}, ErrInvalidRepository
	}
	if accountScope != QuotaAccountScopeDefault || evaluationAtMS < 0 ||
		evaluationAtMS > runtimeclock.MaxTimestampMS {
		return ResetCreditsSummary{}, invalidRecord("reset credits summary input is invalid")
	}
	summary := ResetCreditsSummary{
		AccountScope: accountScope, FreshnessState: SourceFreshnessUnknown, EvaluationAtMS: evaluationAtMS,
	}
	err := repository.database.View(ctx, func(ctx context.Context, connection storesqlite.ReadConn) error {
		database := connection.WithContext(ctx)
		state, found, err := sourceStateByID(ctx, database, ResetCreditsSourceInstanceWhamDefault)
		if err != nil {
			return err
		}
		if found {
			if state.SourceType != ResetCreditsSourceTypeWham || state.ScopeKey != accountScope {
				return invalidRecord("reset credits source state identity is invalid")
			}
			summary.LastSuccessAtMS = cloneQuotaInt64Pointer(state.LastSuccessAtMS)
			summary.LastAttemptAtMS = cloneQuotaInt64Pointer(state.LastAttemptAtMS)
			summary.LastFailureCode = cloneSourceFailureCode(state.LastFailureCode)
			summary.FreshnessState = state.FreshnessState
		}
		var snapshotModel resetCreditsSnapshotModel
		err = database.Where("account_scope = ?", accountScope).
			Order("observed_at_ms DESC").Order("snapshot_id DESC").Take(&snapshotModel).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		var creditModels []resetCreditModel
		if err := database.Where("snapshot_id = ?", snapshotModel.SnapshotID).
			Order("credit_id_hash").Find(&creditModels).Error; err != nil {
			return err
		}
		snapshot, err := resetCreditsSnapshotFromModels(snapshotModel, creditModels)
		if err != nil {
			return err
		}
		attempt, found, err := sourceAttemptByID(ctx, database, snapshot.RequestID)
		if err != nil {
			return err
		}
		if !found || attempt.SourceInstanceID != ResetCreditsSourceInstanceWhamDefault ||
			attempt.Outcome != SourceAttemptSucceeded || snapshot.ObservedAtMS < attempt.StartedAtMS ||
			snapshot.ObservedAtMS > attempt.FinishedAtMS {
			return invalidRecord("reset credits snapshot attempt provenance is invalid")
		}
		if err := validateResetCreditsSnapshot(snapshot); err != nil {
			return err
		}
		populateResetCreditsSummary(&summary, snapshot, evaluationAtMS)
		return nil
	})
	return summary, err
}

func validateResetCreditsFetchRecord(record ResetCreditsFetchRecord) error {
	if record.SourceInstanceID != ResetCreditsSourceInstanceWhamDefault ||
		record.SourceType != ResetCreditsSourceTypeWham || record.ScopeKey != QuotaAccountScopeDefault ||
		record.Attempt.SourceInstanceID != record.SourceInstanceID {
		return invalidRecord("reset credits fetch source identity is invalid")
	}
	if err := validateSourceAttempt(record.Attempt); err != nil {
		return err
	}
	if err := validateQuotaFetchFailureMapping(record.Attempt); err != nil {
		return err
	}
	switch record.Attempt.Outcome {
	case SourceAttemptSucceeded:
		if record.Snapshot == nil {
			return invalidRecord("successful reset credits fetch needs a snapshot")
		}
	case SourceAttemptFailed, SourceAttemptCancelled:
		if record.Snapshot != nil {
			return invalidRecord("unsuccessful reset credits fetch cannot carry a snapshot")
		}
	}
	if record.Snapshot == nil {
		return nil
	}
	if record.Snapshot.RequestID != record.Attempt.RequestID ||
		record.Snapshot.AccountScope != record.ScopeKey ||
		record.Snapshot.ObservedAtMS < record.Attempt.StartedAtMS ||
		record.Snapshot.ObservedAtMS > record.Attempt.FinishedAtMS {
		return invalidRecord("reset credits snapshot provenance is invalid")
	}
	return validateResetCreditsSnapshot(*record.Snapshot)
}

func validateResetCreditsSnapshot(snapshot ResetCreditsSnapshot) error {
	if snapshot.SnapshotID == "" || len(snapshot.SnapshotID) > 512 || snapshot.RequestID == "" ||
		len(snapshot.RequestID) > 512 || snapshot.AccountScope != QuotaAccountScopeDefault ||
		snapshot.AvailableCount < 0 || snapshot.AvailableCount > maxResetCreditsPerSnapshot ||
		len(snapshot.Credits) > maxResetCreditsPerSnapshot || snapshot.ObservedAtMS < 0 ||
		snapshot.ObservedAtMS > runtimeclock.MaxTimestampMS {
		return invalidRecord("reset credits snapshot is invalid")
	}
	seen := make(map[string]struct{}, len(snapshot.Credits))
	available := int64(0)
	for _, credit := range snapshot.Credits {
		digest := credit.CreditIDHash.String()
		if digest == "" {
			return invalidRecord("reset credit identity is invalid")
		}
		if _, duplicate := seen[digest]; duplicate {
			return invalidRecord("reset credit identity is duplicated")
		}
		seen[digest] = struct{}{}
		if !validResetCreditStatus(credit.Status) || !validResetCreditType(credit.Type) ||
			credit.GrantedAtMS < 0 || credit.ExpiresAtMS < credit.GrantedAtMS ||
			credit.ExpiresAtMS > runtimeclock.MaxTimestampMS {
			return invalidRecord("reset credit fields are invalid")
		}
		if credit.RedeemedAtMS != nil && (*credit.RedeemedAtMS < credit.GrantedAtMS ||
			*credit.RedeemedAtMS > credit.ExpiresAtMS) {
			return invalidRecord("reset credit redeemed time is invalid")
		}
		switch credit.Status {
		case ResetCreditAvailable:
			if credit.RedeemedAtMS != nil || credit.ExpiresAtMS <= snapshot.ObservedAtMS {
				return invalidRecord("available reset credit is not currently usable")
			}
			available++
		case ResetCreditRedeemed, ResetCreditUsed:
			if credit.RedeemedAtMS == nil {
				return invalidRecord("consumed reset credit lacks redeemed time")
			}
		}
	}
	if available != snapshot.AvailableCount {
		return invalidRecord("reset credits available count conflicts with items")
	}
	return nil
}

func validateResetCreditsReplay(ctx context.Context, database *gorm.DB, record ResetCreditsFetchRecord) error {
	var model resetCreditsSnapshotModel
	err := database.WithContext(ctx).Where("request_id = ?", record.Attempt.RequestID).Take(&model).Error
	if record.Snapshot == nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		return invalidRecord("failed reset credits replay found a successful snapshot")
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return invalidRecord("successful reset credits replay is missing its snapshot")
	}
	if err != nil {
		return err
	}
	var creditModels []resetCreditModel
	if err := database.WithContext(ctx).Where("snapshot_id = ?", model.SnapshotID).
		Order("credit_id_hash").Find(&creditModels).Error; err != nil {
		return err
	}
	stored, err := resetCreditsSnapshotFromModels(model, creditModels)
	if err != nil {
		return err
	}
	incoming := cloneResetCreditsSnapshot(record.Snapshot)
	sort.Slice(incoming.Credits, func(left, right int) bool {
		return incoming.Credits[left].CreditIDHash.String() < incoming.Credits[right].CreditIDHash.String()
	})
	if !reflect.DeepEqual(stored, *incoming) {
		return invalidRecord("reset credits fetch replay snapshot conflicts")
	}
	return nil
}

func populateResetCreditsSummary(summary *ResetCreditsSummary, snapshot ResetCreditsSnapshot, evaluationAtMS int64) {
	snapshotID := snapshot.SnapshotID
	total := int64(len(snapshot.Credits))
	available, redeemed, cumulative := int64(0), int64(0), int64(0)
	var next *int64
	for _, credit := range snapshot.Credits {
		switch credit.Status {
		case ResetCreditAvailable:
			if credit.ExpiresAtMS <= evaluationAtMS {
				continue
			}
			available++
			remaining := credit.ExpiresAtMS - evaluationAtMS
			if cumulative <= math.MaxInt64-remaining {
				cumulative += remaining
			} else {
				cumulative = math.MaxInt64
			}
			if next == nil || credit.ExpiresAtMS < *next {
				value := credit.ExpiresAtMS
				next = &value
			}
		case ResetCreditRedeemed, ResetCreditUsed:
			redeemed++
		}
	}
	summary.SnapshotID = &snapshotID
	summary.AvailableCount = &available
	summary.TotalCount = &total
	summary.RedeemedCount = &redeemed
	summary.CumulativeRemainingMS = &cumulative
	summary.NextExpiresAtMS = next
}

func resetCreditsSnapshotModelFromDomain(snapshot ResetCreditsSnapshot) resetCreditsSnapshotModel {
	return resetCreditsSnapshotModel{
		SnapshotID: snapshot.SnapshotID, RequestID: snapshot.RequestID,
		AccountScope: snapshot.AccountScope, AvailableCount: snapshot.AvailableCount,
		ObservedAtMS: snapshot.ObservedAtMS,
	}
}

func resetCreditModelFromDomain(snapshotID string, credit ResetCredit) resetCreditModel {
	return resetCreditModel{
		SnapshotID: snapshotID, CreditIDHash: credit.CreditIDHash.String(),
		Status: string(credit.Status), ResetType: string(credit.Type),
		GrantedAtMS: credit.GrantedAtMS, ExpiresAtMS: credit.ExpiresAtMS,
		RedeemedAtMS: cloneQuotaInt64Pointer(credit.RedeemedAtMS),
	}
}

func resetCreditsSnapshotFromModels(
	snapshot resetCreditsSnapshotModel,
	credits []resetCreditModel,
) (ResetCreditsSnapshot, error) {
	result := ResetCreditsSnapshot{
		SnapshotID: snapshot.SnapshotID, RequestID: snapshot.RequestID,
		AccountScope: snapshot.AccountScope, AvailableCount: snapshot.AvailableCount,
		ObservedAtMS: snapshot.ObservedAtMS, Credits: make([]ResetCredit, len(credits)),
	}
	for index, model := range credits {
		if model.SnapshotID != snapshot.SnapshotID {
			return ResetCreditsSnapshot{}, invalidRecord("reset credit snapshot reference is invalid")
		}
		digest, valid := parseSHA256Digest(model.CreditIDHash)
		if !valid {
			return ResetCreditsSnapshot{}, invalidRecord("stored reset credit digest is invalid")
		}
		result.Credits[index] = ResetCredit{
			CreditIDHash: digest, Status: ResetCreditStatus(model.Status), Type: ResetCreditType(model.ResetType),
			GrantedAtMS: model.GrantedAtMS, ExpiresAtMS: model.ExpiresAtMS,
			RedeemedAtMS: cloneQuotaInt64Pointer(model.RedeemedAtMS),
		}
	}
	return result, nil
}

func validResetCreditStatus(value ResetCreditStatus) bool {
	return value == ResetCreditAvailable || value == ResetCreditRedeemed ||
		value == ResetCreditExpired || value == ResetCreditUsed
}

func validResetCreditType(value ResetCreditType) bool {
	return value == ResetCreditTypeCodexRateLimits || value == ResetCreditTypeUnknown
}
