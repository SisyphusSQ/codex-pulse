package quota

import (
	"context"
	"sync"
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

	mu      sync.Mutex
	binding AccountBindingFence
}

func NewService(client *Client, recorder Recorder, recordTimeout time.Duration) (*Service, error) {
	if client == nil || recorder == nil || recordTimeout <= 0 {
		return nil, ErrInvalidClientConfig
	}
	return &Service{client: client, recorder: recorder, recordTimeout: recordTimeout}, nil
}

func (service *Service) SetBinding(binding AccountBindingFence) {
	if service == nil {
		return
	}
	service.mu.Lock()
	service.binding = binding
	service.mu.Unlock()
}

func (service *Service) Fetch(ctx context.Context, requestID string) (Result, error) {
	if service == nil {
		return Result{}, ErrInvalidClientConfig
	}
	service.mu.Lock()
	binding := service.binding
	service.mu.Unlock()
	return service.FetchBound(ctx, BoundRefreshRequest{RequestID: requestID, Binding: binding})
}

func (service *Service) FetchBound(ctx context.Context, request BoundRefreshRequest) (Result, error) {
	if service == nil {
		return Result{}, ErrInvalidClientConfig
	}
	if ctx == nil {
		ctx = context.Background()
	}
	result, err := service.client.Fetch(ctx, request)
	if err != nil {
		return result, err
	}
	service.mu.Lock()
	current := service.binding
	service.mu.Unlock()
	if current != request.Binding {
		return Result{}, store.ErrCodexAccountBindingChanged
	}
	record := quotaFetchRecord(request, result)
	recordContext, cancel := context.WithTimeout(context.WithoutCancel(ctx), service.recordTimeout)
	defer cancel()
	return result, service.recorder.RecordQuotaFetch(recordContext, record)
}

func quotaFetchRecord(request BoundRefreshRequest, result Result) store.QuotaFetchRecord {
	sourceInstanceID := store.QuotaSourceInstanceAppServer(request.Binding.AccountScope)
	attempt := store.SourceAttempt{
		RequestID: request.RequestID, SourceInstanceID: sourceInstanceID,
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
		AccountScope:      request.Binding.AccountScope,
		BindingGeneration: request.Binding.BindingGeneration,
		SourceInstanceID:  sourceInstanceID,
		SourceType:        store.QuotaSourceTypeAppServerRateLimits, ScopeKey: request.Binding.AccountScope,
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
