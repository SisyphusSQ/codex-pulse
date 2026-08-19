package apisubscriptions

import (
	"context"
	"errors"
	"fmt"
	"net"
	"sync"
	"time"
)

type ServiceConfig struct {
	DeepSeek     *DeepSeekClient
	OpenCodeGo   *OpenCodeGoClient
	History      BalanceHistory
	QuotaHistory QuotaHistory
	Location     *time.Location
}

type Service struct {
	deepSeek     *DeepSeekClient
	openCodeGo   *OpenCodeGoClient
	history      BalanceHistory
	quotaHistory QuotaHistory
	location     *time.Location

	mu                sync.Mutex
	lastDeepSeek      *Balance
	lastDeepSeekAtMS  *int64
	lastDeepSeekEpoch string
	lastOpenCodeGo    *Quota
	lastOpenCodeAtMS  *int64
}

func NewService(config ServiceConfig) (*Service, error) {
	if config.DeepSeek == nil || config.OpenCodeGo == nil {
		return nil, errors.New("all API subscription clients are required")
	}
	history := config.History
	if history == nil {
		history = newMemoryBalanceHistory()
	}
	quotaHistory := config.QuotaHistory
	if quotaHistory == nil {
		if combined, ok := history.(QuotaHistory); ok {
			quotaHistory = combined
		} else {
			quotaHistory = newMemoryBalanceHistory()
		}
	}
	location := config.Location
	if location == nil {
		location = time.Local
	}
	return &Service{
		deepSeek: config.DeepSeek, openCodeGo: config.OpenCodeGo,
		history: history, quotaHistory: quotaHistory, location: location,
	}, nil
}

func (service *Service) Current(ctx context.Context, evaluatedAtMS int64) (CurrentSnapshot, error) {
	type deepSeekResult struct {
		balance Balance
		err     error
	}
	type openCodeResult struct {
		quota Quota
		err   error
	}
	deepSeekChannel := make(chan deepSeekResult, 1)
	openCodeChannel := make(chan openCodeResult, 1)
	go func() {
		balance, err := service.deepSeek.GetBalance(ctx)
		deepSeekChannel <- deepSeekResult{balance: balance, err: err}
	}()
	go func() {
		quota, err := service.openCodeGo.GetQuota(ctx)
		openCodeChannel <- openCodeResult{quota: quota, err: err}
	}()
	deepSeek := <-deepSeekChannel
	openCode := <-openCodeChannel

	service.mu.Lock()
	defer service.mu.Unlock()
	result := CurrentSnapshot{EvaluatedAtMS: evaluatedAtMS}
	var err error
	result.DeepSeek, err = service.resolveDeepSeek(ctx, deepSeek.balance, deepSeek.err, evaluatedAtMS)
	if err != nil {
		return CurrentSnapshot{}, err
	}
	result.OpenCodeGo, err = service.resolveOpenCodeGo(ctx, openCode.quota, openCode.err, evaluatedAtMS)
	if err != nil {
		return CurrentSnapshot{}, err
	}
	deepSeekEpoch, _ := credentialEpoch(service.deepSeek.base.credentials, ServiceDeepSeek)
	openCodeGoEpoch, _ := credentialEpoch(service.openCodeGo.base.credentials, ServiceOpenCodeGo)
	result.ActivityCalendar, err = subscriptionActivityCalendar(
		ctx, service.history, service.quotaHistory, deepSeekEpoch, openCodeGoEpoch,
		evaluatedAtMS, service.location,
	)
	if err != nil {
		return CurrentSnapshot{}, fmt.Errorf("read API subscription activity: %w", err)
	}
	return result, nil
}

func (service *Service) resolveDeepSeek(
	ctx context.Context,
	balance Balance,
	err error,
	evaluatedAtMS int64,
) (DeepSeekSnapshot, error) {
	if err == nil {
		copy := cloneBalance(balance)
		epoch, ok := credentialEpoch(service.deepSeek.base.credentials, ServiceDeepSeek)
		if !ok {
			return DeepSeekSnapshot{}, fmt.Errorf("%w: DeepSeek credential epoch is unavailable", ErrProtocol)
		}
		observation := BalanceObservation{
			CredentialEpoch: epoch, ObservedAtMS: evaluatedAtMS, Balance: copy,
		}
		if recordErr := service.history.Record(ctx, observation); recordErr != nil {
			return DeepSeekSnapshot{}, fmt.Errorf("record DeepSeek balance history: %w", recordErr)
		}
		periods, periodErr := balancePeriods(ctx, service.history, epoch, observation, service.location)
		if periodErr != nil {
			return DeepSeekSnapshot{}, fmt.Errorf("read DeepSeek balance history: %w", periodErr)
		}
		if service.lastDeepSeekAtMS == nil || evaluatedAtMS >= *service.lastDeepSeekAtMS {
			service.lastDeepSeek = &copy
			service.lastDeepSeekAtMS = int64Pointer(evaluatedAtMS)
			service.lastDeepSeekEpoch = epoch
		}
		return DeepSeekSnapshot{
			Status: currentStatus(evaluatedAtMS), Balance: balancePointer(copy), Periods: periods,
		}, nil
	}
	if errors.Is(err, ErrUnconfigured) {
		return DeepSeekSnapshot{Status: SourceStatus{State: StateUnconfigured}}, nil
	}
	var periods []BalancePeriod
	if service.lastDeepSeek == nil {
		epoch, hasEpoch := credentialEpoch(service.deepSeek.base.credentials, ServiceDeepSeek)
		if history, ok := service.history.(latestBalanceHistory); ok && hasEpoch {
			latest, found, latestErr := history.Latest(ctx, epoch, evaluatedAtMS)
			if latestErr != nil {
				return DeepSeekSnapshot{}, fmt.Errorf("restore DeepSeek balance history: %w", latestErr)
			}
			if found {
				copy := cloneBalance(latest.Balance)
				service.lastDeepSeek = &copy
				service.lastDeepSeekAtMS = int64Pointer(latest.ObservedAtMS)
				service.lastDeepSeekEpoch = epoch
				periods, latestErr = balancePeriodsAt(
					ctx, service.history, epoch, latest, evaluatedAtMS, service.location,
				)
				if latestErr != nil {
					return DeepSeekSnapshot{}, fmt.Errorf("read restored DeepSeek balance history: %w", latestErr)
				}
			}
		}
	}
	status := failureStatus(err, service.lastDeepSeekAtMS, service.lastDeepSeek != nil)
	if service.lastDeepSeek == nil {
		return DeepSeekSnapshot{Status: status}, nil
	}
	if len(periods) == 0 {
		lastGood := BalanceObservation{
			CredentialEpoch: service.lastDeepSeekEpoch,
			ObservedAtMS:    *service.lastDeepSeekAtMS,
			Balance:         cloneBalance(*service.lastDeepSeek),
		}
		var periodErr error
		periods, periodErr = balancePeriodsAt(
			ctx, service.history, service.lastDeepSeekEpoch, lastGood, evaluatedAtMS, service.location,
		)
		if periodErr != nil {
			return DeepSeekSnapshot{}, fmt.Errorf("read stale DeepSeek balance history: %w", periodErr)
		}
	}
	return DeepSeekSnapshot{
		Status: status, Balance: balancePointer(cloneBalance(*service.lastDeepSeek)), Periods: periods,
	}, nil
}

func credentialEpoch(provider APIKeyProvider, service string) (string, bool) {
	if epochs, ok := provider.(CredentialEpochProvider); ok {
		if epoch, exists := epochs.CredentialEpoch(service); exists && validCredentialEpoch(epoch) {
			return epoch, true
		}
	}
	return "", false
}

func (service *Service) resolveOpenCodeGo(
	ctx context.Context,
	quota Quota,
	err error,
	evaluatedAtMS int64,
) (OpenCodeGoSnapshot, error) {
	if err == nil {
		copy := cloneQuota(quota)
		epoch, ok := credentialEpoch(service.openCodeGo.base.credentials, ServiceOpenCodeGo)
		if !ok {
			return OpenCodeGoSnapshot{}, fmt.Errorf("%w: OpenCode Go credential epoch is unavailable", ErrProtocol)
		}
		if recordErr := service.quotaHistory.RecordQuota(ctx, QuotaObservation{
			CredentialEpoch: epoch, ObservedAtMS: evaluatedAtMS, Quota: copy,
		}); recordErr != nil {
			return OpenCodeGoSnapshot{}, fmt.Errorf("record OpenCode Go quota history: %w", recordErr)
		}
		if service.lastOpenCodeAtMS == nil || evaluatedAtMS >= *service.lastOpenCodeAtMS {
			service.lastOpenCodeGo = &copy
			service.lastOpenCodeAtMS = int64Pointer(evaluatedAtMS)
		}
		return OpenCodeGoSnapshot{Status: currentStatus(evaluatedAtMS), Quota: quotaPointer(copy)}, nil
	}
	if errors.Is(err, ErrUnconfigured) {
		return OpenCodeGoSnapshot{Status: SourceStatus{State: StateUnconfigured}}, nil
	}
	status := failureStatus(err, service.lastOpenCodeAtMS, service.lastOpenCodeGo != nil)
	if service.lastOpenCodeGo == nil {
		return OpenCodeGoSnapshot{Status: status}, nil
	}
	return OpenCodeGoSnapshot{Status: status, Quota: quotaPointer(cloneQuota(*service.lastOpenCodeGo))}, nil
}

func currentStatus(atMS int64) SourceStatus {
	return SourceStatus{State: StateCurrent, LastSuccessAtMS: int64Pointer(atMS)}
}

func failureStatus(err error, lastSuccessAtMS *int64, hasLastGood bool) SourceStatus {
	state := StateUnavailable
	if hasLastGood {
		state = StateStale
	}
	code := failureCode(err)
	return SourceStatus{
		State: state, LastSuccessAtMS: cloneInt64Pointer(lastSuccessAtMS), FailureCode: &code,
	}
}

func failureCode(err error) string {
	var networkError net.Error
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return FailureTimeout
	case errors.Is(err, ErrAuth):
		return FailureAuth
	case errors.Is(err, ErrForbidden):
		return FailureForbidden
	case errors.Is(err, ErrRateLimit):
		return FailureRateLimit
	case errors.Is(err, ErrServer):
		return FailureServer
	case errors.Is(err, ErrProtocol):
		return FailureProtocol
	case errors.As(err, &networkError) && networkError.Timeout():
		return FailureTimeout
	case errors.As(err, &networkError):
		return FailureNetwork
	default:
		return FailureNetwork
	}
}

func cloneBalance(value Balance) Balance {
	result := value
	result.Balances = append([]CurrencyBalance(nil), value.Balances...)
	return result
}

func cloneQuota(value Quota) Quota {
	result := value
	result.Windows = append([]QuotaWindow(nil), value.Windows...)
	return result
}

func balancePointer(value Balance) *Balance { return &value }

func quotaPointer(value Quota) *Quota { return &value }

func int64Pointer(value int64) *int64 { return &value }

func cloneInt64Pointer(value *int64) *int64 {
	if value == nil {
		return nil
	}
	return int64Pointer(*value)
}
