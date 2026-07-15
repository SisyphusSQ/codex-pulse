package quota

import (
	"context"
	"time"

	"github.com/SisyphusSQ/codex-pulse/internal/store"
)

type Recorder interface {
	RecordQuotaFetch(context.Context, store.QuotaFetchRecord) error
}

type Service struct {
	client        *Client
	recorder      Recorder
	recordTimeout time.Duration
}

func NewService(client *Client, recorder Recorder, recordTimeout time.Duration) (*Service, error) {
	if client == nil || recorder == nil || recordTimeout <= 0 {
		return nil, ErrInvalidClientConfig
	}
	return &Service{client: client, recorder: recorder, recordTimeout: recordTimeout}, nil
}

func (service *Service) Fetch(ctx context.Context, requestID string) (Result, error) {
	if service == nil {
		return Result{}, ErrInvalidClientConfig
	}
	if ctx == nil {
		ctx = context.Background()
	}
	result, err := service.client.Fetch(ctx, requestID)
	if err != nil {
		return result, err
	}
	record := quotaFetchRecord(requestID, result)
	recordContext, cancel := context.WithTimeout(context.WithoutCancel(ctx), service.recordTimeout)
	defer cancel()
	return result, service.recorder.RecordQuotaFetch(recordContext, record)
}

func quotaFetchRecord(requestID string, result Result) store.QuotaFetchRecord {
	attempt := store.SourceAttempt{
		RequestID: requestID, SourceInstanceID: store.QuotaSourceInstanceWhamDefault,
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
	return store.QuotaFetchRecord{
		SourceInstanceID: store.QuotaSourceInstanceWhamDefault,
		SourceType:       store.QuotaSourceTypeWham, ScopeKey: store.QuotaAccountScopeDefault,
		Attempt: attempt, Observations: append([]store.QuotaObservationSample(nil), result.Observations...),
	}
}

func runtimeErrorClass(code store.SourceFailureCode) store.RuntimeErrorClass {
	switch code {
	case store.SourceFailureTimeout:
		return store.RuntimeErrorTimeout
	case store.SourceFailureAuthRequired:
		return store.RuntimeErrorPermission
	case store.SourceFailureSchemaIncompatible:
		return store.RuntimeErrorInvalid
	case store.SourceFailureCancelled:
		return store.RuntimeErrorCanceled
	case store.SourceFailureNetworkUnavailable, store.SourceFailureHTTP429, store.SourceFailureServerError:
		return store.RuntimeErrorUnavailable
	default:
		return store.RuntimeErrorUnknown
	}
}
