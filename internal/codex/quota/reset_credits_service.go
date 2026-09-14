package quota

import (
	"context"
	"sync"
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

	mu      sync.Mutex
	binding AccountBindingFence
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
	service.mu.Lock()
	binding := service.binding
	service.mu.Unlock()
	return service.FetchBound(ctx, BoundRefreshRequest{RequestID: requestID, Binding: binding})
}

func (service *ResetCreditsService) SetBinding(binding AccountBindingFence) {
	if service == nil {
		return
	}
	service.mu.Lock()
	service.binding = binding
	service.mu.Unlock()
}

func (service *ResetCreditsService) FetchBound(ctx context.Context, request BoundRefreshRequest) (ResetCreditsResult, error) {
	if service == nil {
		return ResetCreditsResult{}, ErrInvalidClientConfig
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
		return ResetCreditsResult{}, store.ErrCodexAccountBindingChanged
	}
	record := resetCreditsFetchRecord(request, result)
	recordContext, cancel := context.WithTimeout(context.WithoutCancel(ctx), service.recordTimeout)
	defer cancel()
	return result, service.recorder.RecordResetCreditsFetch(recordContext, record)
}

func resetCreditsFetchRecord(request BoundRefreshRequest, result ResetCreditsResult) store.ResetCreditsFetchRecord {
	sourceInstanceID := store.ResetCreditsSourceInstanceAppServer(request.Binding.AccountScope)
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
	return store.ResetCreditsFetchRecord{
		AccountScope:      request.Binding.AccountScope,
		BindingGeneration: request.Binding.BindingGeneration,
		SourceInstanceID:  sourceInstanceID,
		SourceType:        store.ResetCreditsSourceTypeAppServer,
		ScopeKey:          request.Binding.AccountScope,
		Attempt:           attempt,
		Snapshot:          storeResetCreditsSnapshotClone(result.Snapshot),
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
		if credit.ExpiresAtMS != nil {
			expiresAt := *credit.ExpiresAtMS
			cloned.Credits[index].ExpiresAtMS = &expiresAt
		}
		if credit.RedeemedAtMS != nil {
			redeemedAt := *credit.RedeemedAtMS
			cloned.Credits[index].RedeemedAtMS = &redeemedAt
		}
	}
	return &cloned
}
