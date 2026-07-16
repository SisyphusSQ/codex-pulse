package quota

import (
	"context"
	"time"

	"github.com/SisyphusSQ/codex-pulse/internal/store"
)

type ResetCreditsRecorder interface {
	RecordResetCreditsFetch(context.Context, store.ResetCreditsFetchRecord) error
}

type ResetCreditsService struct {
	client        *ResetCreditsClient
	recorder      ResetCreditsRecorder
	recordTimeout time.Duration
}

func NewResetCreditsService(
	client *ResetCreditsClient,
	recorder ResetCreditsRecorder,
	recordTimeout time.Duration,
) (*ResetCreditsService, error) {
	if client == nil || recorder == nil || recordTimeout <= 0 {
		return nil, ErrInvalidClientConfig
	}
	return &ResetCreditsService{client: client, recorder: recorder, recordTimeout: recordTimeout}, nil
}

func (service *ResetCreditsService) Fetch(ctx context.Context, requestID string) (ResetCreditsResult, error) {
	if service == nil {
		return ResetCreditsResult{}, ErrInvalidClientConfig
	}
	if ctx == nil {
		ctx = context.Background()
	}
	result, err := service.client.Fetch(ctx, requestID)
	if err != nil {
		return result, err
	}
	record := resetCreditsFetchRecord(requestID, result)
	recordContext, cancel := context.WithTimeout(context.WithoutCancel(ctx), service.recordTimeout)
	defer cancel()
	return result, service.recorder.RecordResetCreditsFetch(recordContext, record)
}

func resetCreditsFetchRecord(requestID string, result ResetCreditsResult) store.ResetCreditsFetchRecord {
	attempt := store.SourceAttempt{
		RequestID: requestID, SourceInstanceID: store.ResetCreditsSourceInstanceWhamDefault,
		StartedAtMS: result.StartedAtMS, FinishedAtMS: result.FinishedAtMS,
		HTTPStatus: cloneInt64(result.HTTPStatus), PayloadSHA256: result.PayloadSHA256,
		AttemptCount: result.AttemptCount, ResponseBytes: result.ResponseBytes,
	}
	if result.Failure == nil {
		attempt.Outcome = store.SourceAttemptSucceeded
	} else {
		failureCode := result.Failure.Code
		attempt.FailureCode = &failureCode
		attempt.RetryAtMS = cloneInt64(result.Failure.RetryAtMS)
		errorClass := runtimeErrorClass(failureCode)
		attempt.ErrorClass = &errorClass
		if failureCode == store.SourceFailureCancelled {
			attempt.Outcome = store.SourceAttemptCancelled
		} else {
			attempt.Outcome = store.SourceAttemptFailed
		}
	}
	return store.ResetCreditsFetchRecord{
		SourceInstanceID: store.ResetCreditsSourceInstanceWhamDefault,
		SourceType:       store.ResetCreditsSourceTypeWham,
		ScopeKey:         store.QuotaAccountScopeDefault,
		Attempt:          attempt,
		Snapshot:         storeResetCreditsSnapshotClone(result.Snapshot),
	}
}

func storeResetCreditsSnapshotClone(value *store.ResetCreditsSnapshot) *store.ResetCreditsSnapshot {
	if value == nil {
		return nil
	}
	cloned := *value
	cloned.Credits = make([]store.ResetCredit, len(value.Credits))
	for index, credit := range value.Credits {
		cloned.Credits[index] = credit
		if credit.RedeemedAtMS != nil {
			redeemedAt := *credit.RedeemedAtMS
			cloned.Credits[index].RedeemedAtMS = &redeemedAt
		}
	}
	return &cloned
}
