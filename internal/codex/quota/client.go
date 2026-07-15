package quota

import (
	"context"
	"errors"
	"io"
	"math"
	"math/rand/v2"
	"mime"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/SisyphusSQ/codex-pulse/internal/retry"
	"github.com/SisyphusSQ/codex-pulse/internal/store"
)

const (
	defaultTimeout          = 15 * time.Second
	defaultMaxAttempts      = 3
	defaultMaxResponseBytes = int64(1 << 20)
	maximumResponseBytes    = int64(16 << 20)
)

type Client struct {
	httpClient       *http.Client
	credentials      CredentialProvider
	now              func() time.Time
	timeout          time.Duration
	maxAttempts      int
	maxResponseBytes int64
	retryPolicy      RetryPolicy
	wait             func(context.Context, time.Duration) error
}

func NewClient(config ClientConfig) (*Client, error) {
	if config.Credentials == nil {
		return nil, ErrInvalidClientConfig
	}
	if config.Transport == nil {
		config.Transport = http.DefaultTransport
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
	if config.MaxResponseBytes == 0 {
		config.MaxResponseBytes = defaultMaxResponseBytes
	}
	if config.Timeout <= 0 || config.MaxAttempts < 1 || config.MaxAttempts > defaultMaxAttempts ||
		config.MaxResponseBytes < 1 || config.MaxResponseBytes > maximumResponseBytes {
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
	httpClient := &http.Client{
		Transport: closeResponseOnTransportError{base: config.Transport},
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	return &Client{
		httpClient: httpClient, credentials: config.Credentials, now: config.Now,
		timeout: config.Timeout, maxAttempts: config.MaxAttempts,
		maxResponseBytes: config.MaxResponseBytes, retryPolicy: config.RetryPolicy, wait: config.Wait,
	}, nil
}

type closeResponseOnTransportError struct {
	base http.RoundTripper
}

func (transport closeResponseOnTransportError) RoundTrip(request *http.Request) (*http.Response, error) {
	response, err := transport.base.RoundTrip(request)
	if err != nil && response != nil {
		if response.Body != nil {
			_ = response.Body.Close()
		}
		return nil, err
	}
	return response, err
}

func (client *Client) Fetch(ctx context.Context, requestID string) (Result, error) {
	if client == nil || requestID == "" || len(requestID) > 512 {
		return Result{}, ErrInvalidClientConfig
	}
	if ctx == nil {
		ctx = context.Background()
	}
	result := Result{StartedAtMS: client.now().UnixMilli()}
	if err := ctx.Err(); err != nil {
		return client.finish(result, store.SourceFailureCancelled, nil), nil
	}

	for nextAttempt := 1; nextAttempt <= client.maxAttempts; nextAttempt++ {
		result.HTTPStatus = nil
		response, requestErr, attempted := client.do(ctx, requestID)
		if attempted {
			result.AttemptCount++
		}
		if !attempted && errors.Is(requestErr, ErrCredentialUnavailable) {
			return client.finish(result, store.SourceFailureAuthRequired, nil), nil
		}
		if requestErr != nil {
			if response != nil {
				result.ResponseBytes += discardResponseBody(response.Body, client.maxResponseBytes)
			}
			failureCode := classifyRequestError(ctx, requestErr)
			if failureCode == store.SourceFailureCancelled {
				return client.finish(result, failureCode, nil), nil
			}
			if nextAttempt < client.maxAttempts {
				shouldRetry, waitErr := client.retry(ctx, nextAttempt)
				if waitErr != nil {
					result.HTTPStatus = nil
					return client.finish(result, classifyRequestError(ctx, waitErr), nil), nil
				}
				if shouldRetry {
					continue
				}
			}
			return client.finish(result, failureCode, nil), nil
		}
		if response == nil {
			return client.finish(result, store.SourceFailureNetworkUnavailable, nil), nil
		}

		status := int64(response.StatusCode)
		result.HTTPStatus = &status
		if response.StatusCode != http.StatusOK {
			bytesRead := discardResponseBody(response.Body, client.maxResponseBytes)
			result.ResponseBytes += bytesRead
			finishedAtMS := client.finishedAtMS(result.StartedAtMS)
			code, retryAtMS := classifyHTTPStatus(response.StatusCode, response.Header, time.UnixMilli(finishedAtMS))
			if code == store.SourceFailureServerError && nextAttempt < client.maxAttempts {
				shouldRetry, waitErr := client.retry(ctx, nextAttempt)
				if waitErr != nil {
					result.HTTPStatus = nil
					return client.finish(result, classifyRequestError(ctx, waitErr), nil), nil
				}
				if shouldRetry {
					continue
				}
			}
			return client.finishAt(result, code, retryAtMS, finishedAtMS), nil
		}

		if !isJSONContentType(response.Header.Get("Content-Type")) {
			result.ResponseBytes += discardResponseBody(response.Body, client.maxResponseBytes)
			return client.finish(result, store.SourceFailureSchemaIncompatible, nil), nil
		}
		body, bytesRead, oversized, readErr := readResponseBody(response.Body, client.maxResponseBytes)
		result.ResponseBytes += bytesRead
		if readErr != nil {
			result.HTTPStatus = nil
			failureCode := classifyRequestError(ctx, readErr)
			if failureCode == store.SourceFailureCancelled {
				return client.finish(result, failureCode, nil), nil
			}
			if nextAttempt < client.maxAttempts {
				shouldRetry, waitErr := client.retry(ctx, nextAttempt)
				if waitErr != nil {
					return client.finish(result, classifyRequestError(ctx, waitErr), nil), nil
				}
				if shouldRetry {
					continue
				}
			}
			return client.finish(result, failureCode, nil), nil
		}
		if oversized {
			return client.finish(result, store.SourceFailureSchemaIncompatible, nil), nil
		}
		digest := store.SHA256DigestOf(body)
		result.PayloadSHA256 = &digest
		finishedAtMS := client.finishedAtMS(result.StartedAtMS)
		observations, partialFailure := decodeWhamUsage(body, requestID, finishedAtMS)
		result.Observations = observations
		result.FinishedAtMS = finishedAtMS
		if partialFailure {
			result.Failure = &Failure{Code: store.SourceFailureSchemaIncompatible}
		}
		return result, nil
	}
	return client.finish(result, store.SourceFailureNetworkUnavailable, nil), nil
}

func (client *Client) do(ctx context.Context, requestID string) (*http.Response, error, bool) {
	requestContext, cancel := context.WithTimeout(ctx, client.timeout)
	request, err := http.NewRequestWithContext(requestContext, http.MethodGet, WhamUsageEndpoint, nil)
	if err != nil {
		cancel()
		return nil, err, false
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Cache-Control", "no-store")
	request.Header.Set("X-Request-ID", requestID)
	var response *http.Response
	attempted := false
	err = client.credentials.WithAccessToken(requestContext, func(token []byte) error {
		attempted = true
		request.Header.Set("Authorization", "Bearer "+string(token))
		defer request.Header.Del("Authorization")
		var requestErr error
		response, requestErr = client.httpClient.Do(request)
		return requestErr
	})
	if err != nil || response == nil {
		cancel()
		return response, err, attempted
	}
	if response.Body == nil {
		response.Body = http.NoBody
	}
	response.Body = &cancelOnCloseBody{ReadCloser: response.Body, cancel: cancel}
	return response, err, attempted
}

type cancelOnCloseBody struct {
	io.ReadCloser
	cancel context.CancelFunc
}

func (body *cancelOnCloseBody) Close() error {
	err := body.ReadCloser.Close()
	body.cancel()
	return err
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

func (client *Client) finish(result Result, code store.SourceFailureCode, retryAtMS *int64) Result {
	return client.finishAt(result, code, retryAtMS, client.finishedAtMS(result.StartedAtMS))
}

func (client *Client) finishAt(
	result Result,
	code store.SourceFailureCode,
	retryAtMS *int64,
	finishedAtMS int64,
) Result {
	result.FinishedAtMS = finishedAtMS
	result.Failure = &Failure{Code: code, RetryAtMS: cloneInt64(retryAtMS)}
	return result
}

func (client *Client) finishedAtMS(startedAtMS int64) int64 {
	finishedAtMS := client.now().UnixMilli()
	if finishedAtMS < startedAtMS {
		return startedAtMS
	}
	return finishedAtMS
}

func classifyRequestError(ctx context.Context, err error) store.SourceFailureCode {
	if ctx != nil && errors.Is(ctx.Err(), context.Canceled) || errors.Is(err, context.Canceled) {
		return store.SourceFailureCancelled
	}
	if ctx != nil && errors.Is(ctx.Err(), context.DeadlineExceeded) || errors.Is(err, context.DeadlineExceeded) {
		return store.SourceFailureTimeout
	}
	return store.SourceFailureNetworkUnavailable
}

func classifyHTTPStatus(status int, headers http.Header, now time.Time) (store.SourceFailureCode, *int64) {
	switch {
	case status == http.StatusUnauthorized || status == http.StatusForbidden:
		return store.SourceFailureAuthRequired, nil
	case status == http.StatusTooManyRequests:
		return store.SourceFailureHTTP429, retryAtFromHeaders(headers, now)
	case status >= 500 && status <= 599:
		return store.SourceFailureServerError, nil
	default:
		return store.SourceFailureSchemaIncompatible, nil
	}
}

func retryAtFromHeaders(headers http.Header, now time.Time) *int64 {
	if value := strings.TrimSpace(headers.Get("Retry-After")); value != "" {
		if seconds, err := strconv.ParseInt(value, 10, 64); err == nil && seconds >= 0 {
			nowMS := now.UnixMilli()
			if seconds <= math.MaxInt64/1000 {
				deltaMS := seconds * 1000
				if nowMS <= math.MaxInt64-deltaMS {
					result := nowMS + deltaMS
					return &result
				}
			}
		}
		if instant, err := http.ParseTime(value); err == nil && instant.After(now) {
			result := instant.UnixMilli()
			return &result
		}
	}
	if value := strings.TrimSpace(headerValue(headers, "X-RateLimit-Reset")); value != "" {
		if seconds, err := strconv.ParseInt(value, 10, 64); err == nil && seconds > 0 && seconds <= math.MaxInt64/1000 {
			result := seconds * 1000
			if result >= now.UnixMilli() {
				return &result
			}
		}
	}
	return nil
}

func headerValue(headers http.Header, name string) string {
	for key, values := range headers {
		if strings.EqualFold(key, name) && len(values) > 0 {
			return values[0]
		}
	}
	return ""
}

func isJSONContentType(value string) bool {
	mediaType, _, err := mime.ParseMediaType(value)
	return err == nil && strings.EqualFold(mediaType, "application/json")
}

func readResponseBody(body io.ReadCloser, limit int64) ([]byte, int64, bool, error) {
	if body == nil {
		return nil, 0, false, nil
	}
	defer body.Close()
	content, err := io.ReadAll(io.LimitReader(body, limit+1))
	if err != nil {
		return nil, int64(len(content)), false, err
	}
	return content, int64(len(content)), int64(len(content)) > limit, nil
}

func discardResponseBody(body io.ReadCloser, limit int64) int64 {
	if body == nil {
		return 0
	}
	defer body.Close()
	bytesRead, _ := io.Copy(io.Discard, io.LimitReader(body, limit+1))
	return bytesRead
}

func cloneInt64(value *int64) *int64 {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}
