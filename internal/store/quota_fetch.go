package store

import (
	"context"
	"math"
	"sort"

	"gorm.io/gorm"

	storesqlite "github.com/SisyphusSQ/codex-pulse/internal/store/sqlite"
)

// RecordQuotaFetch atomically appends one content-free online attempt, all
// validated observations from that response, and the logical source state.
func (repository *Repository) RecordQuotaFetch(ctx context.Context, record QuotaFetchRecord) error {
	if repository == nil || repository.database == nil {
		return ErrInvalidRepository
	}
	if err := validateQuotaFetchRecord(record); err != nil {
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
				return invalidRecord("quota fetch conflicts with append-only attempt")
			}
			if err := validateQuotaFetchReplay(database, record); err != nil {
				return err
			}
			for _, observation := range record.Observations {
				if err := upsertQuotaObservation(ctx, database, observation); err != nil {
					return err
				}
			}
			return nil
		}
		evaluatedAtMS, err := repository.quotaEvaluationTimeMS()
		if err != nil {
			return err
		}

		existingState, found, err := sourceStateByID(ctx, database, record.SourceInstanceID)
		if err != nil {
			return err
		}
		if found && (existingState.SourceType != record.SourceType || existingState.ScopeKey != record.ScopeKey) {
			return invalidRecord("quota source stable identity conflicts")
		}
		projected, advance, err := projectQuotaSourceState(existingState, found, record)
		if err != nil {
			return err
		}
		if !found {
			model := sourceStateModelFromDomain(projected)
			if err := database.Create(&model).Error; err != nil {
				return err
			}
		}
		for _, observation := range record.Observations {
			if err := upsertQuotaObservation(ctx, database, observation); err != nil {
				return err
			}
		}
		attemptModel := sourceAttemptModelFromDomain(record.Attempt)
		if err := database.Create(&attemptModel).Error; err != nil {
			return err
		}
		if found && advance {
			model := sourceStateModelFromDomain(projected)
			result := database.Model(&sourceStateModel{}).
				Where("source_instance_id = ? AND updated_at_ms = ?", record.SourceInstanceID, existingState.UpdatedAtMS).
				Updates(sourceStateUpdates(model))
			if result.Error != nil {
				return result.Error
			}
			if result.RowsAffected != 1 {
				return invalidRecord("quota source state changed during writer transaction")
			}
		}
		return repository.rebuildQuotaScopeProjectionInTransaction(
			ctx, database, record.ScopeKey, evaluatedAtMS, defaultQuotaArbitrationRule(),
		)
	})
}

func validateQuotaFetchRecord(record QuotaFetchRecord) error {
	if record.SourceInstanceID != QuotaSourceInstanceWhamDefault || record.SourceType != QuotaSourceTypeWham ||
		record.ScopeKey != QuotaAccountScopeDefault || record.Attempt.SourceInstanceID != record.SourceInstanceID {
		return invalidRecord("quota fetch source identity is invalid")
	}
	if err := validateSourceAttempt(record.Attempt); err != nil {
		return err
	}
	if len(record.Observations) > 100 {
		return invalidRecord("quota fetch has too many observations")
	}
	seen := make(map[string]struct{}, len(record.Observations))
	for _, observation := range record.Observations {
		if err := validateQuotaObservationSample(observation); err != nil {
			return err
		}
		if observation.Source != QuotaSourceWham || observation.AccountScope != record.ScopeKey ||
			observation.RequestID == nil || *observation.RequestID != record.Attempt.RequestID ||
			observation.SessionID != nil || observation.SourceFileID != nil ||
			observation.ObservedAtMS < record.Attempt.StartedAtMS || observation.ObservedAtMS > record.Attempt.FinishedAtMS {
			return invalidRecord("quota fetch observation provenance is invalid")
		}
		if _, duplicate := seen[observation.ObservationID]; duplicate {
			return invalidRecord("quota fetch observation is duplicated")
		}
		seen[observation.ObservationID] = struct{}{}
	}
	if record.Attempt.Outcome == SourceAttemptSucceeded && len(record.Observations) == 0 {
		return invalidRecord("successful quota fetch has no observation")
	}
	if record.Attempt.Outcome == SourceAttemptCancelled && len(record.Observations) != 0 {
		return invalidRecord("cancelled quota fetch has observations")
	}
	return validateQuotaFetchFailureMapping(record.Attempt)
}

func validateQuotaFetchFailureMapping(attempt SourceAttempt) error {
	if attempt.Outcome == SourceAttemptSucceeded {
		if attempt.HTTPStatus == nil || *attempt.HTTPStatus != 200 {
			return invalidRecord("successful quota fetch needs HTTP 200")
		}
		return nil
	}
	if attempt.FailureCode == nil || attempt.ErrorClass == nil {
		return invalidRecord("quota fetch failure is untyped")
	}
	wantClass := RuntimeErrorUnknown
	switch *attempt.FailureCode {
	case SourceFailureNetworkUnavailable:
		wantClass = RuntimeErrorUnavailable
		if attempt.HTTPStatus != nil {
			return invalidRecord("network failure must not have HTTP status")
		}
	case SourceFailureTimeout:
		wantClass = RuntimeErrorTimeout
		if attempt.HTTPStatus != nil {
			return invalidRecord("timeout failure must not have HTTP status")
		}
	case SourceFailureAuthRequired:
		wantClass = RuntimeErrorPermission
		if attempt.HTTPStatus != nil && *attempt.HTTPStatus != 401 && *attempt.HTTPStatus != 403 {
			return invalidRecord("auth failure status is invalid")
		}
	case SourceFailureHTTP429:
		wantClass = RuntimeErrorUnavailable
		if attempt.HTTPStatus == nil || *attempt.HTTPStatus != 429 {
			return invalidRecord("rate-limit failure needs HTTP 429")
		}
	case SourceFailureServerError:
		wantClass = RuntimeErrorUnavailable
		if attempt.HTTPStatus == nil || *attempt.HTTPStatus < 500 || *attempt.HTTPStatus > 599 {
			return invalidRecord("server failure status is invalid")
		}
	case SourceFailureSchemaIncompatible:
		wantClass = RuntimeErrorInvalid
		if attempt.HTTPStatus == nil {
			return invalidRecord("schema failure needs HTTP status")
		}
	case SourceFailureCancelled:
		wantClass = RuntimeErrorCanceled
		if attempt.Outcome != SourceAttemptCancelled || attempt.HTTPStatus != nil {
			return invalidRecord("cancelled quota fetch state is invalid")
		}
	}
	if *attempt.ErrorClass != wantClass {
		return invalidRecord("quota fetch generic and exact failure classes disagree")
	}
	return nil
}

func projectQuotaSourceState(
	existing SourceState,
	found bool,
	record QuotaFetchRecord,
) (SourceState, bool, error) {
	if found && existing.LastAttemptAtMS != nil && record.Attempt.FinishedAtMS < *existing.LastAttemptAtMS {
		return existing, false, nil
	}
	state := existing
	if !found {
		state = SourceState{
			SourceInstanceID: record.SourceInstanceID, SourceType: record.SourceType,
			ScopeKey: record.ScopeKey, FreshnessState: SourceFreshnessUnknown,
		}
	}
	updatedAtMS := record.Attempt.FinishedAtMS
	if found && updatedAtMS <= existing.UpdatedAtMS {
		if existing.UpdatedAtMS == math.MaxInt64 {
			return SourceState{}, false, invalidRecord("quota source update timestamp is exhausted")
		}
		updatedAtMS = existing.UpdatedAtMS + 1
	}
	finishedAtMS := record.Attempt.FinishedAtMS
	state.LastAttemptAtMS = &finishedAtMS
	state.UpdatedAtMS = updatedAtMS
	state.CursorVersion++
	state.NextDueAtMS = nil
	switch record.Attempt.Outcome {
	case SourceAttemptSucceeded:
		state.LastSuccessAtMS = &finishedAtMS
		state.ConsecutiveFailures = 0
		state.LastErrorClass = nil
		state.LastFailureCode = nil
		state.FreshnessState = SourceFreshnessCurrent
	case SourceAttemptFailed:
		if quotaFetchHasAcceptedObservation(record.Observations) {
			state.LastSuccessAtMS = &finishedAtMS
		}
		state.ConsecutiveFailures++
		state.LastErrorClass = cloneRuntimeErrorClass(record.Attempt.ErrorClass)
		state.LastFailureCode = cloneSourceFailureCode(record.Attempt.FailureCode)
		state.NextDueAtMS = cloneQuotaInt64Pointer(record.Attempt.RetryAtMS)
		if state.LastSuccessAtMS == nil {
			state.FreshnessState = SourceFreshnessUnavailable
		} else {
			state.FreshnessState = SourceFreshnessStale
		}
	case SourceAttemptCancelled:
		state.LastErrorClass = cloneRuntimeErrorClass(record.Attempt.ErrorClass)
		state.LastFailureCode = cloneSourceFailureCode(record.Attempt.FailureCode)
	}
	return state, true, validateSourceState(state)
}

func quotaFetchHasAcceptedObservation(observations []QuotaObservationSample) bool {
	for _, observation := range observations {
		if observation.Validity == QuotaValidityAccepted {
			return true
		}
	}
	return false
}

func validateQuotaFetchReplay(database *gorm.DB, record QuotaFetchRecord) error {
	var storedIDs []string
	if err := database.Model(&quotaObservationModel{}).
		Where("source = ? AND request_id = ?", string(QuotaSourceWham), record.Attempt.RequestID).
		Order("observation_id").Pluck("observation_id", &storedIDs).Error; err != nil {
		return err
	}
	incomingIDs := make([]string, len(record.Observations))
	for index, observation := range record.Observations {
		incomingIDs[index] = observation.ObservationID
	}
	sort.Strings(incomingIDs)
	if len(storedIDs) != len(incomingIDs) {
		return invalidRecord("quota fetch replay observation set conflicts")
	}
	for index := range storedIDs {
		if storedIDs[index] != incomingIDs[index] {
			return invalidRecord("quota fetch replay observation identity conflicts")
		}
	}
	return nil
}

func cloneSourceFailureCode(value *SourceFailureCode) *SourceFailureCode {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}

func cloneQuotaInt64Pointer(value *int64) *int64 {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}
