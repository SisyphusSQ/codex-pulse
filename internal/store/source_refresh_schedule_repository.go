package store

import (
	"context"
	"errors"
	"math"

	"gorm.io/gorm"

	"github.com/SisyphusSQ/codex-pulse/internal/runtimeclock"
	storesqlite "github.com/SisyphusSQ/codex-pulse/internal/store/sqlite"
)

const sourceRefreshManualMinimumIntervalMS = int64(60_000)

func (repository *Repository) UpsertSourceRefreshSchedule(
	ctx context.Context,
	update SourceRefreshScheduleUpdate,
) (SourceRefreshSchedule, error) {
	if repository == nil || repository.database == nil {
		return SourceRefreshSchedule{}, ErrInvalidRepository
	}
	if err := validateSourceRefreshScheduleUpdate(update); err != nil {
		return SourceRefreshSchedule{}, err
	}
	var stored SourceRefreshSchedule
	err := repository.database.Write(ctx, func(ctx context.Context, transaction storesqlite.WriteTx) error {
		existing, found, err := sourceRefreshScheduleByID(ctx, transaction, update.SourceInstanceID)
		if err != nil {
			return err
		}
		if !found {
			if update.ExpectedRevision != 0 {
				return invalidRecord("source refresh schedule create revision is stale")
			}
			stored = SourceRefreshSchedule{
				SourceInstanceID: update.SourceInstanceID, SourceType: update.SourceType, ScopeKey: update.ScopeKey,
				NextDueAtMS: cloneQuotaInt64Pointer(update.NextDueAtMS), Reason: update.Reason,
				Revision: 1, UpdatedAtMS: update.AtMS,
			}
			model := sourceRefreshScheduleModelFromDomain(stored)
			return transaction.WithContext(ctx).Create(&model).Error
		}
		if existing.SourceType != update.SourceType || existing.ScopeKey != update.ScopeKey {
			return invalidRecord("source refresh schedule stable identity conflicts")
		}
		if existing.Revision != update.ExpectedRevision {
			return sourceRefreshConflict("source refresh schedule revision is stale")
		}
		if existing.ActiveClaimID != nil {
			return invalidRecord("source refresh schedule is actively claimed")
		}
		if existing.Revision == math.MaxInt64 {
			return invalidRecord("source refresh schedule revision is exhausted")
		}
		stored = existing
		stored.NextDueAtMS = cloneQuotaInt64Pointer(update.NextDueAtMS)
		stored.Reason = update.Reason
		stored.Revision++
		stored.UpdatedAtMS = monotonicScheduleAt(update.AtMS, existing.UpdatedAtMS)
		model := sourceRefreshScheduleModelFromDomain(stored)
		result := transaction.WithContext(ctx).Model(&sourceRefreshScheduleModel{}).
			Where("source_instance_id = ? AND revision = ?", existing.SourceInstanceID, existing.Revision).
			Updates(sourceRefreshScheduleUpdates(model))
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return sourceRefreshConflict("source refresh schedule changed during update")
		}
		return nil
	})
	return stored, err
}

func (repository *Repository) SourceRefreshSchedule(
	ctx context.Context,
	sourceInstanceID string,
) (SourceRefreshSchedule, error) {
	if repository == nil || repository.database == nil || sourceInstanceID == "" {
		return SourceRefreshSchedule{}, ErrInvalidRepository
	}
	var schedule SourceRefreshSchedule
	err := repository.database.View(ctx, func(ctx context.Context, connection storesqlite.ReadConn) error {
		value, found, err := sourceRefreshScheduleByID(ctx, connection, sourceInstanceID)
		if err != nil {
			return err
		}
		if !found {
			return ErrNotFound
		}
		schedule = value
		return nil
	})
	return schedule, err
}

func (repository *Repository) ListDueSourceRefreshSchedules(
	ctx context.Context,
	atMS int64,
	limit int,
) ([]SourceRefreshSchedule, error) {
	if repository == nil || repository.database == nil {
		return nil, ErrInvalidRepository
	}
	if atMS < 0 || atMS > runtimeclock.MaxTimestampMS || limit < 1 || limit > 100 {
		return nil, invalidRecord("source refresh due query is invalid")
	}
	var schedules []SourceRefreshSchedule
	err := repository.database.View(ctx, func(ctx context.Context, connection storesqlite.ReadConn) error {
		var models []sourceRefreshScheduleModel
		if err := connection.WithContext(ctx).
			Where("next_due_at_ms IS NOT NULL AND next_due_at_ms <= ? AND active_claim_id IS NULL", atMS).
			Order("next_due_at_ms").Order("source_instance_id").Limit(limit).Find(&models).Error; err != nil {
			return err
		}
		schedules = make([]SourceRefreshSchedule, len(models))
		for index, model := range models {
			value, err := sourceRefreshScheduleFromModel(model)
			if err != nil {
				return err
			}
			schedules[index] = value
		}
		return nil
	})
	return schedules, err
}

func (repository *Repository) ClaimSourceRefresh(
	ctx context.Context,
	sourceInstanceID string,
	expectedRevision int64,
	claimID string,
	trigger SourceRefreshTrigger,
	atMS int64,
	leaseMS int64,
) (SourceRefreshSchedule, bool, error) {
	if repository == nil || repository.database == nil {
		return SourceRefreshSchedule{}, false, ErrInvalidRepository
	}
	if sourceInstanceID == "" || claimID == "" || len(claimID) > 512 || !validSourceRefreshTrigger(trigger) ||
		expectedRevision <= 0 || atMS < 0 || atMS > runtimeclock.MaxTimestampMS || leaseMS <= 0 ||
		atMS > runtimeclock.MaxTimestampMS-leaseMS {
		return SourceRefreshSchedule{}, false, invalidRecord("source refresh claim is invalid")
	}
	var stored SourceRefreshSchedule
	claimed := false
	err := repository.database.Write(ctx, func(ctx context.Context, transaction storesqlite.WriteTx) error {
		existing, found, err := sourceRefreshScheduleByID(ctx, transaction, sourceInstanceID)
		if err != nil {
			return err
		}
		if !found {
			return ErrNotFound
		}
		stored = existing
		if existing.Revision != expectedRevision {
			return sourceRefreshConflict("source refresh claim revision is stale")
		}
		if existing.ActiveClaimID != nil || existing.NextDueAtMS == nil || *existing.NextDueAtMS > atMS {
			return nil
		}
		if trigger == RefreshTriggerManual && existing.LastManualAtMS != nil &&
			atMS < addSourceRefreshTimestamp(*existing.LastManualAtMS, sourceRefreshManualMinimumIntervalMS) {
			return nil
		}
		if _, found, err := sourceRefreshClaimByID(ctx, transaction, claimID); err != nil {
			return err
		} else if found {
			return invalidRecord("source refresh claim identity is not append-only")
		}
		if existing.Revision == math.MaxInt64 {
			return invalidRecord("source refresh schedule revision is exhausted")
		}
		claimStartedAtMS := atMS
		claimExpiresAtMS := atMS + leaseMS
		triggerCopy := trigger
		stored.ActiveClaimID = &claimID
		stored.ActiveTrigger = &triggerCopy
		stored.ClaimStartedAtMS = &claimStartedAtMS
		stored.ClaimExpiresAtMS = &claimExpiresAtMS
		if trigger == RefreshTriggerManual {
			stored.LastManualAtMS = &claimStartedAtMS
		}
		stored.Revision++
		stored.UpdatedAtMS = monotonicScheduleAt(atMS, existing.UpdatedAtMS)
		model := sourceRefreshScheduleModelFromDomain(stored)
		result := transaction.WithContext(ctx).Model(&sourceRefreshScheduleModel{}).
			Where("source_instance_id = ? AND revision = ? AND active_claim_id IS NULL", sourceInstanceID, existing.Revision).
			Updates(sourceRefreshScheduleUpdates(model))
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return sourceRefreshConflict("source refresh schedule changed during claim")
		}
		claim := sourceRefreshClaimModel{
			ClaimID: claimID, SourceInstanceID: sourceInstanceID, ScheduleRevision: stored.Revision,
			Trigger: string(trigger), StartedAtMS: claimStartedAtMS, ExpiresAtMS: claimExpiresAtMS,
			State: string(sourceRefreshClaimActive),
		}
		if err := transaction.WithContext(ctx).Create(&claim).Error; err != nil {
			return err
		}
		claimed = true
		return nil
	})
	return stored, claimed, err
}

func (repository *Repository) CompleteSourceRefresh(
	ctx context.Context,
	completion SourceRefreshCompletion,
) (SourceRefreshSchedule, error) {
	if repository == nil || repository.database == nil {
		return SourceRefreshSchedule{}, ErrInvalidRepository
	}
	if completion.SourceInstanceID == "" || completion.ClaimID == "" || len(completion.ClaimID) > 512 ||
		completion.ExpectedRevision <= 0 || completion.AtMS < 0 || completion.AtMS > runtimeclock.MaxTimestampMS ||
		validateSourceRefreshDecision(completion.NextDueAtMS, completion.Reason) != nil {
		return SourceRefreshSchedule{}, invalidRecord("source refresh completion is invalid")
	}
	var stored SourceRefreshSchedule
	err := repository.database.Write(ctx, func(ctx context.Context, transaction storesqlite.WriteTx) error {
		existing, found, err := sourceRefreshScheduleByID(ctx, transaction, completion.SourceInstanceID)
		if err != nil {
			return err
		}
		if !found {
			return ErrNotFound
		}
		if existing.Revision != completion.ExpectedRevision || existing.ActiveClaimID == nil ||
			*existing.ActiveClaimID != completion.ClaimID {
			return sourceRefreshConflict("source refresh completion claim is stale")
		}
		claim, found, err := sourceRefreshClaimByID(ctx, transaction, completion.ClaimID)
		if err != nil {
			return err
		}
		if !found || claim.SourceInstanceID != completion.SourceInstanceID ||
			claim.ScheduleRevision != completion.ExpectedRevision {
			return invalidRecord("source refresh completion claim fence is invalid")
		}
		if sourceRefreshClaimState(claim.State) != sourceRefreshClaimActive {
			return sourceRefreshConflict("source refresh completion claim is finalized")
		}
		if existing.Revision == math.MaxInt64 {
			return invalidRecord("source refresh schedule revision is exhausted")
		}
		stored = existing
		stored.NextDueAtMS = cloneQuotaInt64Pointer(completion.NextDueAtMS)
		stored.Reason = completion.Reason
		stored.ActiveClaimID = nil
		stored.ActiveTrigger = nil
		stored.ClaimStartedAtMS = nil
		stored.ClaimExpiresAtMS = nil
		stored.Revision++
		stored.UpdatedAtMS = monotonicScheduleAt(completion.AtMS, existing.UpdatedAtMS)
		model := sourceRefreshScheduleModelFromDomain(stored)
		result := transaction.WithContext(ctx).Model(&sourceRefreshScheduleModel{}).
			Where("source_instance_id = ? AND revision = ? AND active_claim_id = ?", completion.SourceInstanceID, existing.Revision, completion.ClaimID).
			Updates(sourceRefreshScheduleUpdates(model))
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return sourceRefreshConflict("source refresh schedule changed during completion")
		}
		claimResult := transaction.WithContext(ctx).Model(&sourceRefreshClaimModel{}).
			Where("claim_id = ? AND state = ? AND schedule_revision = ?", completion.ClaimID, sourceRefreshClaimActive, completion.ExpectedRevision).
			Updates(map[string]any{"state": sourceRefreshClaimCompleted, "finalized_at_ms": completion.AtMS})
		if claimResult.Error != nil {
			return claimResult.Error
		}
		if claimResult.RowsAffected != 1 {
			return sourceRefreshConflict("source refresh completion lost claim fence")
		}
		return nil
	})
	return stored, err
}

// ListExpiredSourceRefreshClaims returns expired claims without mutating them.
// The caller must first check whether ActiveClaimID already has a durable
// source_attempt before choosing completion or release semantics.
func (repository *Repository) ListExpiredSourceRefreshClaims(
	ctx context.Context,
	atMS int64,
	limit int,
) ([]SourceRefreshSchedule, error) {
	if repository == nil || repository.database == nil {
		return nil, ErrInvalidRepository
	}
	if atMS < 0 || atMS > runtimeclock.MaxTimestampMS || limit < 1 || limit > 100 {
		return nil, invalidRecord("source refresh recovery time is invalid")
	}
	var expired []SourceRefreshSchedule
	err := repository.database.View(ctx, func(ctx context.Context, connection storesqlite.ReadConn) error {
		var models []sourceRefreshScheduleModel
		if err := connection.WithContext(ctx).
			Where("active_claim_id IS NOT NULL AND claim_expires_at_ms <= ?", atMS).
			Order("claim_expires_at_ms").Order("source_instance_id").Limit(limit).Find(&models).Error; err != nil {
			return err
		}
		expired = make([]SourceRefreshSchedule, len(models))
		for index, model := range models {
			value, err := sourceRefreshScheduleFromModel(model)
			if err != nil {
				return err
			}
			expired[index] = value
		}
		return nil
	})
	return expired, err
}

// ReleaseExpiredSourceRefreshClaim makes an unrecorded request immediately
// eligible for a recovery fetch. The claim identity and revision are checked
// again in the writer transaction so a late completion cannot be overwritten.
func (repository *Repository) ReleaseExpiredSourceRefreshClaim(
	ctx context.Context,
	recovery SourceRefreshClaimRecovery,
) (SourceRefreshSchedule, bool, error) {
	if repository == nil || repository.database == nil {
		return SourceRefreshSchedule{}, false, ErrInvalidRepository
	}
	if recovery.SourceInstanceID == "" || recovery.ClaimID == "" || len(recovery.ClaimID) > 512 ||
		recovery.ExpectedRevision <= 0 || recovery.AtMS < 0 || recovery.AtMS > runtimeclock.MaxTimestampMS {
		return SourceRefreshSchedule{}, false, invalidRecord("source refresh recovery is invalid")
	}
	var recovered SourceRefreshSchedule
	released := false
	err := repository.database.Write(ctx, func(ctx context.Context, transaction storesqlite.WriteTx) error {
		existing, found, err := sourceRefreshScheduleByID(ctx, transaction, recovery.SourceInstanceID)
		if err != nil {
			return err
		}
		if !found {
			return ErrNotFound
		}
		if existing.Revision != recovery.ExpectedRevision || existing.ActiveClaimID == nil ||
			*existing.ActiveClaimID != recovery.ClaimID {
			return sourceRefreshConflict("source refresh recovery claim is stale")
		}
		if existing.ClaimExpiresAtMS == nil || *existing.ClaimExpiresAtMS > recovery.AtMS {
			return invalidRecord("source refresh claim has not expired")
		}
		claim, found, err := sourceRefreshClaimByID(ctx, transaction, recovery.ClaimID)
		if err != nil {
			return err
		}
		if !found || claim.SourceInstanceID != recovery.SourceInstanceID ||
			claim.ScheduleRevision != recovery.ExpectedRevision {
			return invalidRecord("source refresh recovery claim fence is invalid")
		}
		if sourceRefreshClaimState(claim.State) != sourceRefreshClaimActive {
			return sourceRefreshConflict("source refresh recovery claim is finalized")
		}
		_, attemptRecorded, err := sourceAttemptByID(ctx, transaction, recovery.ClaimID)
		if err != nil {
			return err
		}
		if attemptRecorded {
			recovered = existing
			return nil
		}
		if existing.Revision == math.MaxInt64 {
			return invalidRecord("source refresh schedule revision is exhausted")
		}
		recovered = existing
		recovered.NextDueAtMS = cloneQuotaInt64Pointer(&recovery.AtMS)
		recovered.Reason = RefreshReasonRecovery
		recovered.ActiveClaimID = nil
		recovered.ActiveTrigger = nil
		recovered.ClaimStartedAtMS = nil
		recovered.ClaimExpiresAtMS = nil
		recovered.Revision++
		recovered.UpdatedAtMS = monotonicScheduleAt(recovery.AtMS, existing.UpdatedAtMS)
		updated := sourceRefreshScheduleModelFromDomain(recovered)
		result := transaction.WithContext(ctx).Model(&sourceRefreshScheduleModel{}).
			Where("source_instance_id = ? AND revision = ? AND active_claim_id = ?", existing.SourceInstanceID, existing.Revision, recovery.ClaimID).
			Updates(sourceRefreshScheduleUpdates(updated))
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return sourceRefreshConflict("source refresh schedule changed during recovery")
		}
		claimResult := transaction.WithContext(ctx).Model(&sourceRefreshClaimModel{}).
			Where("claim_id = ? AND state = ? AND schedule_revision = ?", recovery.ClaimID, sourceRefreshClaimActive, recovery.ExpectedRevision).
			Updates(map[string]any{"state": sourceRefreshClaimAbandoned, "finalized_at_ms": recovery.AtMS})
		if claimResult.Error != nil {
			return claimResult.Error
		}
		if claimResult.RowsAffected != 1 {
			return sourceRefreshConflict("source refresh recovery lost claim fence")
		}
		released = true
		return nil
	})
	return recovered, released, err
}

func validateSourceRefreshScheduleUpdate(update SourceRefreshScheduleUpdate) error {
	if update.SourceInstanceID == "" || len(update.SourceInstanceID) > 512 || update.SourceType == "" ||
		len(update.SourceType) > 128 || update.ScopeKey == "" || len(update.ScopeKey) > 128 ||
		update.ExpectedRevision < 0 || update.AtMS < 0 || update.AtMS > runtimeclock.MaxTimestampMS {
		return invalidRecord("source refresh schedule update is invalid")
	}
	if !validSourceRefreshIdentity(update.SourceInstanceID, update.SourceType, update.ScopeKey) {
		return invalidRecord("source refresh schedule identity is invalid")
	}
	return validateSourceRefreshDecision(update.NextDueAtMS, update.Reason)
}

func validateSourceRefreshDecision(nextDueAtMS *int64, reason SourceRefreshReason) error {
	if !validSourceRefreshReason(reason) || nextDueAtMS != nil &&
		(*nextDueAtMS < 0 || *nextDueAtMS > runtimeclock.MaxTimestampMS) {
		return invalidRecord("source refresh decision is invalid")
	}
	paused := reason == RefreshReasonAuthRequired || reason == RefreshReasonSchemaIncompatible ||
		reason == RefreshReasonDisabled
	if paused != (nextDueAtMS == nil) {
		return invalidRecord("source refresh decision due shape is invalid")
	}
	return nil
}

func sourceRefreshScheduleByID(
	ctx context.Context,
	database *gorm.DB,
	sourceInstanceID string,
) (SourceRefreshSchedule, bool, error) {
	var model sourceRefreshScheduleModel
	err := database.WithContext(ctx).Take(&model, "source_instance_id = ?", sourceInstanceID).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return SourceRefreshSchedule{}, false, nil
	}
	if err != nil {
		return SourceRefreshSchedule{}, false, err
	}
	value, err := sourceRefreshScheduleFromModel(model)
	return value, err == nil, err
}

type sourceRefreshClaimState string

const (
	sourceRefreshClaimActive    sourceRefreshClaimState = "active"
	sourceRefreshClaimCompleted sourceRefreshClaimState = "completed"
	sourceRefreshClaimAbandoned sourceRefreshClaimState = "abandoned"
)

func sourceRefreshClaimByID(
	ctx context.Context,
	database *gorm.DB,
	claimID string,
) (sourceRefreshClaimModel, bool, error) {
	var model sourceRefreshClaimModel
	err := database.WithContext(ctx).Take(&model, "claim_id = ?", claimID).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return sourceRefreshClaimModel{}, false, nil
	}
	if err != nil {
		return sourceRefreshClaimModel{}, false, err
	}
	if model.ClaimID == "" || model.SourceInstanceID == "" || model.ScheduleRevision <= 0 ||
		!validSourceRefreshTrigger(SourceRefreshTrigger(model.Trigger)) ||
		model.StartedAtMS < 0 || model.ExpiresAtMS < model.StartedAtMS ||
		!validSourceRefreshClaimState(sourceRefreshClaimState(model.State)) ||
		(sourceRefreshClaimState(model.State) == sourceRefreshClaimActive) != (model.FinalizedAtMS == nil) ||
		model.FinalizedAtMS != nil && *model.FinalizedAtMS < model.StartedAtMS {
		return sourceRefreshClaimModel{}, false, invalidRecord("stored source refresh claim fence is invalid")
	}
	return model, true, nil
}

func validateSourceRefreshClaimAttempt(
	ctx context.Context,
	database *gorm.DB,
	requestID string,
	sourceInstanceID string,
) error {
	claim, found, err := sourceRefreshClaimByID(ctx, database, requestID)
	if err != nil || !found {
		return err
	}
	if claim.SourceInstanceID != sourceInstanceID {
		return invalidRecord("source attempt claim provenance is invalid")
	}
	if sourceRefreshClaimState(claim.State) != sourceRefreshClaimActive {
		return sourceRefreshConflict("source attempt claim was already finalized")
	}
	return nil
}

func validSourceRefreshClaimState(value sourceRefreshClaimState) bool {
	return value == sourceRefreshClaimActive || value == sourceRefreshClaimCompleted ||
		value == sourceRefreshClaimAbandoned
}

func sourceRefreshScheduleFromModel(model sourceRefreshScheduleModel) (SourceRefreshSchedule, error) {
	value := SourceRefreshSchedule{
		SourceInstanceID: model.SourceInstanceID, SourceType: model.SourceType, ScopeKey: model.ScopeKey,
		NextDueAtMS: cloneQuotaInt64Pointer(model.NextDueAtMS), Reason: SourceRefreshReason(model.Reason),
		LastManualAtMS:   cloneQuotaInt64Pointer(model.LastManualAtMS),
		ActiveClaimID:    cloneQuotaString(model.ActiveClaimID),
		ClaimStartedAtMS: cloneQuotaInt64Pointer(model.ClaimStartedAtMS),
		ClaimExpiresAtMS: cloneQuotaInt64Pointer(model.ClaimExpiresAtMS),
		Revision:         model.Revision, UpdatedAtMS: model.UpdatedAtMS,
	}
	if model.ActiveTrigger != nil {
		trigger := SourceRefreshTrigger(*model.ActiveTrigger)
		value.ActiveTrigger = &trigger
	}
	if err := validateStoredSourceRefreshSchedule(value); err != nil {
		return SourceRefreshSchedule{}, err
	}
	return value, nil
}

func validateStoredSourceRefreshSchedule(value SourceRefreshSchedule) error {
	if validateSourceRefreshScheduleUpdate(SourceRefreshScheduleUpdate{
		SourceInstanceID: value.SourceInstanceID, SourceType: value.SourceType, ScopeKey: value.ScopeKey,
		ExpectedRevision: value.Revision, NextDueAtMS: value.NextDueAtMS, Reason: value.Reason, AtMS: value.UpdatedAtMS,
	}) != nil || value.Revision <= 0 || value.LastManualAtMS != nil && (*value.LastManualAtMS < 0 ||
		*value.LastManualAtMS > runtimeclock.MaxTimestampMS) {
		return invalidRecord("stored source refresh schedule is invalid")
	}
	claimed := value.ActiveClaimID != nil
	if claimed != (value.ActiveTrigger != nil) || claimed != (value.ClaimStartedAtMS != nil) ||
		claimed != (value.ClaimExpiresAtMS != nil) {
		return invalidRecord("stored source refresh claim shape is invalid")
	}
	if claimed && (*value.ActiveClaimID == "" || !validSourceRefreshTrigger(*value.ActiveTrigger) ||
		*value.ClaimStartedAtMS < 0 || *value.ClaimExpiresAtMS < *value.ClaimStartedAtMS) {
		return invalidRecord("stored source refresh claim is invalid")
	}
	return nil
}

func sourceRefreshScheduleModelFromDomain(value SourceRefreshSchedule) sourceRefreshScheduleModel {
	model := sourceRefreshScheduleModel{
		SourceInstanceID: value.SourceInstanceID, SourceType: value.SourceType, ScopeKey: value.ScopeKey,
		NextDueAtMS: cloneQuotaInt64Pointer(value.NextDueAtMS), Reason: string(value.Reason),
		LastManualAtMS:   cloneQuotaInt64Pointer(value.LastManualAtMS),
		ActiveClaimID:    cloneQuotaString(value.ActiveClaimID),
		ClaimStartedAtMS: cloneQuotaInt64Pointer(value.ClaimStartedAtMS),
		ClaimExpiresAtMS: cloneQuotaInt64Pointer(value.ClaimExpiresAtMS),
		Revision:         value.Revision, UpdatedAtMS: value.UpdatedAtMS,
	}
	if value.ActiveTrigger != nil {
		trigger := string(*value.ActiveTrigger)
		model.ActiveTrigger = &trigger
	}
	return model
}

func sourceRefreshScheduleUpdates(model sourceRefreshScheduleModel) map[string]any {
	return map[string]any{
		"next_due_at_ms": model.NextDueAtMS, "reason": model.Reason,
		"last_manual_at_ms": model.LastManualAtMS, "active_claim_id": model.ActiveClaimID,
		"active_trigger": model.ActiveTrigger, "claim_started_at_ms": model.ClaimStartedAtMS,
		"claim_expires_at_ms": model.ClaimExpiresAtMS, "revision": model.Revision,
		"updated_at_ms": model.UpdatedAtMS,
	}
}

func validSourceRefreshReason(value SourceRefreshReason) bool {
	switch value {
	case RefreshReasonStartup, RefreshReasonNormalInterval, RefreshReasonLowRemaining,
		RefreshReasonNearReset, RefreshReasonResetGrace, RefreshReasonForeground,
		RefreshReasonWakeStale, RefreshReasonManual, RefreshReasonNetworkBackoff,
		RefreshReasonRetryAfter, RefreshReasonAuthRequired, RefreshReasonSchemaIncompatible,
		RefreshReasonCancelled, RefreshReasonDisabled, RefreshReasonRecovery:
		return true
	default:
		return false
	}
}

func validSourceRefreshTrigger(value SourceRefreshTrigger) bool {
	return value == RefreshTriggerScheduled || value == RefreshTriggerStartup ||
		value == RefreshTriggerForeground || value == RefreshTriggerWake ||
		value == RefreshTriggerManual || value == RefreshTriggerRecovery
}

func validSourceRefreshIdentity(sourceInstanceID, sourceType, scopeKey string) bool {
	if scopeKey != QuotaAccountScopeDefault {
		return false
	}
	return sourceInstanceID == QuotaSourceInstanceWhamDefault && sourceType == QuotaSourceTypeWham ||
		sourceInstanceID == ResetCreditsSourceInstanceWhamDefault && sourceType == ResetCreditsSourceTypeWham
}

func monotonicScheduleAt(atMS, previousMS int64) int64 {
	if atMS > previousMS {
		return atMS
	}
	if previousMS == math.MaxInt64 {
		return previousMS
	}
	return previousMS + 1
}

func addSourceRefreshTimestamp(value, delta int64) int64 {
	if value > runtimeclock.MaxTimestampMS-delta {
		return runtimeclock.MaxTimestampMS
	}
	return value + delta
}
