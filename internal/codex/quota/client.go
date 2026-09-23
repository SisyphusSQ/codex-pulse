package quota

import (
	"context"
	"errors"
	"math/rand/v2"
	"time"

	"github.com/SisyphusSQ/codex-pulse/internal/codex/accountbinding"
	"github.com/SisyphusSQ/codex-pulse/internal/codex/appserver"
	"github.com/SisyphusSQ/codex-pulse/internal/retry"
	"github.com/SisyphusSQ/codex-pulse/internal/store"
)

const (
	defaultTimeout          = 15 * time.Second
	defaultMaxAttempts      = 3
	quotaReadExcludeDetails = true
	resetReadExcludeDetails = false
)

type Client struct {
	reader                    AccountRateLimitsReader
	scopeKey                  [32]byte
	excludeResetCreditDetails bool
	now                       func() time.Time
	timeout                   time.Duration
	maxAttempts               int
	retryPolicy               RetryPolicy
	wait                      func(context.Context, time.Duration) error
}

func NewClient(config ClientConfig) (*Client, error) {
	return newRateLimitsClient(config, quotaReadExcludeDetails)
}

func newRateLimitsClient(config ClientConfig, excludeResetCreditDetails bool) (*Client, error) {
	if config.Reader == nil {
		return nil, ErrInvalidClientConfig
	}
	if config.Now == nil {
		config.Now = time.Now
	}
	if config.Timeout == 0 {
		config.Timeout = defaultTimeout
	}
	if config.MaxAttempts == 0 {
		config.MaxAttempts = defaultMaxAttempts
	}
	if config.Timeout <= 0 || config.MaxAttempts < 1 || config.MaxAttempts > defaultMaxAttempts {
		return nil, ErrInvalidClientConfig
	}
	if config.RetryPolicy == nil {
		policy, err := retry.NewPolicy(retry.Config{
			BaseDelay: 250 * time.Millisecond, MaxDelay: time.Second,
			MaxAttempts: defaultMaxAttempts - 1, Jitter: rand.Float64,
		})
		if err != nil {
			return nil, ErrInvalidClientConfig
		}
		config.RetryPolicy = policy
	}
	if config.Wait == nil {
		config.Wait = retry.Wait
	}
	return &Client{
		reader: config.Reader, scopeKey: config.ScopeKey,
		excludeResetCreditDetails: excludeResetCreditDetails,
		now:                       config.Now, timeout: config.Timeout, maxAttempts: config.MaxAttempts,
		retryPolicy: config.RetryPolicy, wait: config.Wait,
	}, nil
}

func (client *Client) Fetch(ctx context.Context, request BoundRefreshRequest) (Result, error) {
	if client == nil || !validBoundRefreshRequest(request) {
		return Result{}, ErrInvalidClientConfig
	}
	if ctx == nil {
		ctx = context.Background()
	}
	result := Result{StartedAtMS: client.now().UnixMilli()}
	if err := ctx.Err(); err != nil {
		return client.finish(result, store.SourceFailureCancelled), nil
	}
	snapshot, err := client.readBoundSnapshot(ctx, request, &result)
	if err != nil {
		return Result{}, err
	}
	if result.Failure != nil {
		return result, nil
	}
	finishedAtMS := client.finishedAtMS(result.StartedAtMS)
	observations, partial := observationsFromRateLimits(snapshot, request, finishedAtMS)
	result.Observations = observations
	digest := quotaTypedDigest(request, finishedAtMS, observations)
	result.PayloadSHA256 = &digest
	result.FinishedAtMS = finishedAtMS
	if partial || len(observations) == 0 {
		result.Failure = &Failure{Code: store.SourceFailureSchemaIncompatible}
		if len(observations) == 0 {
			result.Observations = nil
		}
	}
	return result, nil
}

func (client *Client) readBoundSnapshot(
	ctx context.Context,
	request BoundRefreshRequest,
	result *Result,
) (appserver.AccountRateLimitsSnapshot, error) {
	var seenScope string
	for nextAttempt := 1; nextAttempt <= client.maxAttempts; nextAttempt++ {
		requestContext, cancel := context.WithTimeout(ctx, client.timeout)
		snapshot, err := client.reader.Read(requestContext, client.excludeResetCreditDetails)
		cancel()
		result.AttemptCount++
		if err != nil {
			if errors.Is(err, appserver.ErrAccountIdentityUnavailable) {
				return appserver.AccountRateLimitsSnapshot{}, store.ErrCodexAccountBindingChanged
			}
			code := classifyAppServerError(ctx, err)
			if code == store.SourceFailureCancelled || localCodexRuntimeUnavailable(err) ||
				!retryableAppServerFailure(code) ||
				nextAttempt == client.maxAttempts {
				*result = client.finish(*result, code)
				return appserver.AccountRateLimitsSnapshot{}, nil
			}
			shouldRetry, waitErr := client.retry(ctx, nextAttempt)
			if waitErr != nil {
				*result = client.finish(*result, classifyAppServerError(ctx, waitErr))
				return appserver.AccountRateLimitsSnapshot{}, nil
			}
			if shouldRetry {
				continue
			}
			*result = client.finish(*result, code)
			return appserver.AccountRateLimitsSnapshot{}, nil
		}
		accountID := append([]byte(nil), snapshot.AccountID...)
		clearBytes(snapshot.AccountID)
		scope, deriveErr := accountbinding.DeriveScope(client.scopeKey, accountID)
		if deriveErr != nil {
			if errors.Is(deriveErr, accountbinding.ErrAccountIdentityUnavailable) {
				return appserver.AccountRateLimitsSnapshot{}, store.ErrCodexAccountBindingChanged
			}
			*result = client.finish(*result, store.SourceFailureSchemaIncompatible)
			return appserver.AccountRateLimitsSnapshot{}, nil
		}
		if seenScope != "" && seenScope != scope {
			return appserver.AccountRateLimitsSnapshot{}, store.ErrCodexAccountBindingChanged
		}
		if scope != request.Binding.AccountScope {
			return appserver.AccountRateLimitsSnapshot{}, store.ErrCodexAccountBindingChanged
		}
		seenScope = scope
		return snapshot, nil
	}
	*result = client.finish(*result, store.SourceFailureNetworkUnavailable)
	return appserver.AccountRateLimitsSnapshot{}, nil
}

func (client *Client) retry(ctx context.Context, failedAttempt int) (bool, error) {
	delay, shouldRetry, err := client.retryPolicy.Delay(failedAttempt)
	if err != nil || !shouldRetry || delay <= 0 {
		return false, nil
	}
	if err := client.wait(ctx, delay); err != nil {
		return false, err
	}
	return true, nil
}

func (client *Client) finish(result Result, code store.SourceFailureCode) Result {
	result.FinishedAtMS = client.finishedAtMS(result.StartedAtMS)
	result.Observations = nil
	result.Failure = &Failure{Code: code}
	return result
}

func (client *Client) finishedAtMS(startedAtMS int64) int64 {
	finishedAtMS := client.now().UnixMilli()
	if finishedAtMS < startedAtMS {
		return startedAtMS
	}
	return finishedAtMS
}

func validBoundRefreshRequest(request BoundRefreshRequest) bool {
	if request.RequestID == "" || len(request.RequestID) > 512 {
		return false
	}
	if request.Binding.BindingGeneration <= 0 || !validDerivedScope(request.Binding.AccountScope) {
		return false
	}
	return true
}

func validDerivedScope(value string) bool {
	if len(value) != 64 {
		return false
	}
	for index := 0; index < len(value); index++ {
		character := value[index]
		if character < '0' || character > '9' {
			if character < 'a' || character > 'f' {
				return false
			}
		}
	}
	return true
}

func classifyAppServerError(ctx context.Context, err error) store.SourceFailureCode {
	if ctx != nil && errors.Is(ctx.Err(), context.Canceled) || errors.Is(err, context.Canceled) {
		return store.SourceFailureCancelled
	}
	if ctx != nil && errors.Is(ctx.Err(), context.DeadlineExceeded) || errors.Is(err, context.DeadlineExceeded) {
		return store.SourceFailureTimeout
	}
	// The current persisted failure vocabulary has no local-runtime code. Use the
	// retryable unavailable state so installing Node or updating the App can recover.
	if localCodexRuntimeUnavailable(err) {
		return store.SourceFailureNetworkUnavailable
	}
	if errors.Is(err, appserver.ErrCapabilityUnavailable) ||
		errors.Is(err, appserver.ErrProtocolIncompatible) ||
		errors.Is(err, appserver.ErrRateLimitsSchemaIncompatible) {
		return store.SourceFailureSchemaIncompatible
	}
	var rpc appserver.RPCError
	if errors.As(err, &rpc) {
		return store.SourceFailureServerError
	}
	return store.SourceFailureNetworkUnavailable
}

func localCodexRuntimeUnavailable(err error) bool {
	return errors.Is(err, appserver.ErrNodeRuntimeUnavailable) ||
		errors.Is(err, appserver.ErrCodexBinaryUnavailable) ||
		errors.Is(err, appserver.ErrCodexLaunchFailed)
}

func retryableAppServerFailure(code store.SourceFailureCode) bool {
	return code == store.SourceFailureTimeout || code == store.SourceFailureNetworkUnavailable ||
		code == store.SourceFailureServerError
}

func clearBytes(value []byte) {
	for index := range value {
		value[index] = 0
	}
}

func cloneInt64(value *int64) *int64 {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}
