package quota

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/SisyphusSQ/codex-pulse/internal/store"
)

const syntheticAccessToken = "synthetic-secret-token-must-not-escape"

func TestMemoryCredentialProviderScopesCopiesAndReleasesToken(t *testing.T) {
	t.Parallel()

	input := []byte(syntheticAccessToken)
	provider, err := NewMemoryCredentialProvider(input)
	if err != nil {
		t.Fatalf("NewMemoryCredentialProvider() error = %v", err)
	}
	for index := range input {
		input[index] = 'x'
	}
	var first []byte
	if err := provider.WithAccessToken(context.Background(), func(token []byte) error {
		first = append(first, token...)
		token[0] = 'X'
		return nil
	}); err != nil {
		t.Fatalf("WithAccessToken(first) error = %v", err)
	}
	if got := string(first); got != syntheticAccessToken {
		t.Fatalf("first token = %q, want injected value", got)
	}
	if err := provider.WithAccessToken(context.Background(), func(token []byte) error {
		if got := string(token); got != syntheticAccessToken {
			t.Fatalf("provider token was mutated through lease: %q", got)
		}
		return nil
	}); err != nil {
		t.Fatalf("WithAccessToken(second) error = %v", err)
	}

	replacement := []byte("synthetic-replacement")
	if err := provider.Replace(replacement); err != nil {
		t.Fatalf("Replace() error = %v", err)
	}
	for index := range replacement {
		replacement[index] = 'y'
	}
	if err := provider.WithAccessToken(context.Background(), func(token []byte) error {
		if got := string(token); got != "synthetic-replacement" {
			t.Fatalf("replacement token = %q", got)
		}
		return nil
	}); err != nil {
		t.Fatalf("WithAccessToken(replacement) error = %v", err)
	}
	provider.Close()
	if err := provider.WithAccessToken(context.Background(), func([]byte) error { return nil }); !errors.Is(err, ErrCredentialUnavailable) {
		t.Fatalf("WithAccessToken(closed) error = %v, want ErrCredentialUnavailable", err)
	}
	if err := provider.Replace([]byte("after-close")); !errors.Is(err, ErrCredentialUnavailable) {
		t.Fatalf("Replace(closed) error = %v, want ErrCredentialUnavailable", err)
	}
}

func TestClientFetchesValidatedWhamObservationsAndClearsAuthorization(t *testing.T) {
	t.Parallel()

	provider, err := NewMemoryCredentialProvider([]byte(syntheticAccessToken))
	if err != nil {
		t.Fatalf("NewMemoryCredentialProvider() error = %v", err)
	}
	t.Cleanup(provider.Close)
	body := newTrackingBody(validWhamPayload("pro", 42, 8))
	var retained *http.Request
	doer := roundTripperFunc(func(request *http.Request) (*http.Response, error) {
		retained = request
		if request.Method != http.MethodGet || request.URL.String() != WhamUsageEndpoint {
			t.Fatalf("request = %s %s", request.Method, request.URL)
		}
		if got := request.Header.Get("Authorization"); got != "Bearer "+syntheticAccessToken {
			t.Fatalf("Authorization = %q", got)
		}
		if request.Header.Get("Cache-Control") != "no-store" || request.Header.Get("Accept") != "application/json" {
			t.Fatalf("safe request headers = %#v", request.Header)
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json; charset=utf-8"}},
			Body:       body,
		}, nil
	})
	client := mustClient(t, ClientConfig{
		Transport: doer, Credentials: provider, Now: fixedClock(1_784_000_000_000),
	})
	result, err := client.Fetch(context.Background(), "request-success")
	if err != nil {
		t.Fatalf("Fetch() error = %v", err)
	}
	if result.Failure != nil || result.AttemptCount != 1 || result.HTTPStatus == nil || *result.HTTPStatus != 200 ||
		result.ResponseBytes == 0 || result.PayloadSHA256 == nil {
		t.Fatalf("result metadata = %#v", result)
	}
	if len(result.Observations) != 2 {
		t.Fatalf("observations = %#v, want primary + secondary", result.Observations)
	}
	primary, secondary := result.Observations[0], result.Observations[1]
	if primary.Source != store.QuotaSourceWham || primary.AccountScope != store.QuotaAccountScopeDefault ||
		primary.LimitID == nil || *primary.LimitID != "codex" || primary.WindowKind != store.QuotaWindowPrimary ||
		primary.UsedPercent != 42 || primary.WindowMinutes != 300 || primary.ResetsAtMS != 1_784_003_600_000 ||
		primary.PlanType == nil || *primary.PlanType != "pro" || primary.Validity != store.QuotaValidityAccepted ||
		primary.RequestID == nil || *primary.RequestID != "request-success" || primary.SessionID != nil ||
		primary.SourceFileID != nil {
		t.Fatalf("primary observation = %#v", primary)
	}
	if secondary.WindowKind != store.QuotaWindowSecondary || secondary.UsedPercent != 8 ||
		secondary.WindowMinutes != 10080 || secondary.ResetsAtMS != 1_784_604_800_000 {
		t.Fatalf("secondary observation = %#v", secondary)
	}
	if primary.ObservationID == secondary.ObservationID || primary.ObservationID == "" {
		t.Fatalf("observation IDs = %q, %q", primary.ObservationID, secondary.ObservationID)
	}
	if retained == nil || retained.Header.Get("Authorization") != "" {
		t.Fatalf("Authorization reference remained after Fetch: %#v", retained)
	}
	if !body.closed {
		t.Fatal("response body was not closed")
	}
	if output := fmt.Sprintf("%#v", result); strings.Contains(output, syntheticAccessToken) || strings.Contains(output, "Bearer ") {
		t.Fatalf("result leaked credential: %s", output)
	}
}

func TestClientFetchesNamedAdditionalRateLimitBuckets(t *testing.T) {
	t.Parallel()

	client := clientForBody(t, `{
  "plan_type": "pro",
  "rate_limit": {
    "primary_window": {
      "used_percent": 80,
      "limit_window_seconds": 604800,
      "reset_at": 1784604800
    }
  },
  "additional_rate_limits": [
    {
      "limit_name": "GPT-5.3-Codex-Spark",
      "metered_feature": "codex_spark",
      "rate_limit": {
        "primary_window": {
          "used_percent": 0,
          "limit_window_seconds": 604800,
          "reset_at": 1784691200
        }
      }
    }
  ]
}`)
	result, err := client.Fetch(context.Background(), "request-additional")
	if err != nil {
		t.Fatalf("Fetch() error = %v", err)
	}
	if result.Failure != nil {
		t.Fatalf("failure = %#v", result.Failure)
	}
	if len(result.Observations) != 2 {
		t.Fatalf("observations = %#v, want general + named additional quota", result.Observations)
	}
	general, additional := result.Observations[0], result.Observations[1]
	if general.LimitID == nil || *general.LimitID != "codex" {
		t.Fatalf("general limit ID = %#v", general.LimitID)
	}
	if additional.LimitID == nil || *additional.LimitID != "codex_spark" ||
		additional.WindowKind != store.QuotaWindowPrimary || additional.UsedPercent != 0 ||
		additional.WindowMinutes != 10_080 || additional.ResetsAtMS != 1_784_691_200_000 {
		t.Fatalf("additional observation = %#v", additional)
	}
	if general.ObservationID == additional.ObservationID {
		t.Fatalf("different limit buckets shared observation ID %q", general.ObservationID)
	}
	if additional.LimitName == nil || *additional.LimitName != "GPT-5.3-Codex-Spark" {
		t.Fatalf("additional limit name was not preserved: %#v", additional)
	}
}

func TestClientIgnoresAdditionalRateLimitWithoutWindows(t *testing.T) {
	t.Parallel()

	client := clientForBody(t, `{
  "plan_type": "pro",
  "rate_limit": {
    "primary_window": {
      "used_percent": 20,
      "limit_window_seconds": 604800,
      "reset_at": 1784604800
    }
  },
  "additional_rate_limits": [
    {
      "limit_name": "Future Model",
      "metered_feature": "future_model",
      "rate_limit": null
    }
  ]
}`)
	result, err := client.Fetch(context.Background(), "request-empty-additional")
	if err != nil {
		t.Fatalf("Fetch() error = %v", err)
	}
	if result.Failure != nil || len(result.Observations) != 1 ||
		result.Observations[0].LimitID == nil || *result.Observations[0].LimitID != "codex" {
		t.Fatalf("result = %#v, want only the valid general quota", result)
	}
}

func TestClientKeepsAttemptContextAliveUntilResponseBodyCloses(t *testing.T) {
	t.Parallel()

	provider, err := NewMemoryCredentialProvider([]byte(syntheticAccessToken))
	if err != nil {
		t.Fatalf("NewMemoryCredentialProvider() error = %v", err)
	}
	t.Cleanup(provider.Close)
	var body *contextAwareBody
	client := mustClient(t, ClientConfig{
		Transport: roundTripperFunc(func(request *http.Request) (*http.Response, error) {
			body = &contextAwareBody{
				ctx: request.Context(), reader: strings.NewReader(validWhamPayload("pro", 12, 34)),
			}
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"application/json"}},
				Body:       body,
			}, nil
		}),
		Credentials: provider, Now: fixedClock(1_784_000_000_000),
	})
	result, err := client.Fetch(context.Background(), "request-context-body")
	if err != nil || result.Failure != nil || len(result.Observations) != 2 {
		t.Fatalf("Fetch() = %#v, %v", result, err)
	}
	if body == nil || !body.readWhileActive || !body.closed {
		t.Fatalf("body lifecycle = %#v", body)
	}
}

func TestClientHandlesPartialAndSuspiciousWhamPayloadsWithoutFalseZero(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		body         string
		wantWindows  []store.QuotaWindowKind
		wantFailure  *store.SourceFailureCode
		wantValidity store.QuotaValidity
		wantReason   *store.QuotaRejectionReason
		wantUsed     float64
	}{
		{
			name:        "secondary absent is valid partial response",
			body:        `{"plan_type":"plus","rate_limit":{"primary_window":{"used_percent":31,"limit_window_seconds":18000,"reset_at":1784003600}}}`,
			wantWindows: []store.QuotaWindowKind{store.QuotaWindowPrimary}, wantValidity: store.QuotaValidityAccepted, wantUsed: 31,
		},
		{
			name:        "invalid secondary preserves primary and reports schema failure",
			body:        `{"plan_type":"plus","rate_limit":{"primary_window":{"used_percent":32,"limit_window_seconds":18000,"reset_at":1784003600},"secondary_window":{"used_percent":"zero","limit_window_seconds":604800,"reset_at":1784604800}}}`,
			wantWindows: []store.QuotaWindowKind{store.QuotaWindowPrimary}, wantFailure: pointer(store.SourceFailureSchemaIncompatible),
			wantValidity: store.QuotaValidityAccepted, wantUsed: 32,
		},
		{
			name:        "missing primary preserves secondary as suspicious",
			body:        `{"plan_type":"plus","rate_limit":{"primary_window":null,"secondary_window":{"used_percent":17,"limit_window_seconds":604800,"reset_at":1784604800}}}`,
			wantWindows: []store.QuotaWindowKind{store.QuotaWindowSecondary}, wantFailure: pointer(store.SourceFailureSchemaIncompatible),
			wantValidity: store.QuotaValiditySuspicious, wantReason: pointer(store.QuotaReasonMissingPrimaryWindow), wantUsed: 17,
		},
		{
			name:        "future plan is unknown suspicious instead of zero",
			body:        `{"plan_type":"future_plan","rate_limit":{"primary_window":{"used_percent":19,"limit_window_seconds":18000,"reset_at":1784003600}},"future":{"ignored":true}}`,
			wantWindows: []store.QuotaWindowKind{store.QuotaWindowPrimary}, wantValidity: store.QuotaValiditySuspicious,
			wantReason: pointer(store.QuotaReasonUnknownPlanType), wantUsed: 19,
		},
		{
			name:        "past reset is suspicious without fabricating zero",
			body:        `{"plan_type":"pro","rate_limit":{"primary_window":{"used_percent":27,"limit_window_seconds":18000,"reset_at":1}}}`,
			wantWindows: []store.QuotaWindowKind{store.QuotaWindowPrimary}, wantValidity: store.QuotaValiditySuspicious,
			wantReason: pointer(store.QuotaReasonResetNotFuture), wantUsed: 27,
		},
		{
			name:        "mixed case alias is ignored instead of overriding allowlisted key",
			body:        `{"plan_type":"pro","rate_limit":{"primary_window":{"used_percent":42,"USED_PERCENT":0,"limit_window_seconds":18000,"reset_at":1784003600}}}`,
			wantWindows: []store.QuotaWindowKind{store.QuotaWindowPrimary}, wantValidity: store.QuotaValidityAccepted, wantUsed: 42,
		},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			client := clientForBody(t, testCase.body)
			result, err := client.Fetch(context.Background(), "request-partial")
			if err != nil {
				t.Fatalf("Fetch() error = %v", err)
			}
			if len(result.Observations) != len(testCase.wantWindows) {
				t.Fatalf("observations = %#v", result.Observations)
			}
			for index, window := range testCase.wantWindows {
				observation := result.Observations[index]
				if observation.WindowKind != window || observation.UsedPercent != testCase.wantUsed ||
					observation.Validity != testCase.wantValidity || !equalQuotaReason(observation.RejectionReason, testCase.wantReason) {
					t.Fatalf("observation[%d] = %#v", index, observation)
				}
			}
			if testCase.wantFailure == nil {
				if result.Failure != nil {
					t.Fatalf("failure = %#v, want nil", result.Failure)
				}
			} else if result.Failure == nil || result.Failure.Code != *testCase.wantFailure {
				t.Fatalf("failure = %#v, want %s", result.Failure, *testCase.wantFailure)
			}
		})
	}
}

func TestClientClassifiesStatusAndSchemaFailuresWithoutLeakingBodies(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		status      int
		headers     http.Header
		body        string
		maxBytes    int64
		wantCode    store.SourceFailureCode
		wantRetryAt *int64
	}{
		{name: "unauthorized", status: 401, body: syntheticAccessToken, wantCode: store.SourceFailureAuthRequired},
		{name: "forbidden", status: 403, body: syntheticAccessToken, wantCode: store.SourceFailureAuthRequired},
		{name: "rate limited seconds", status: 429, headers: http.Header{"Retry-After": []string{"120"}}, body: syntheticAccessToken,
			wantCode: store.SourceFailureHTTP429, wantRetryAt: pointer(int64(1_784_000_120_000))},
		{name: "rate limited epoch reset", status: 429, headers: http.Header{"X-RateLimit-Reset": []string{"1784000300"}},
			body: syntheticAccessToken, wantCode: store.SourceFailureHTTP429, wantRetryAt: pointer(int64(1_784_000_300_000))},
		{name: "unexpected client status", status: 404, body: syntheticAccessToken, wantCode: store.SourceFailureSchemaIncompatible},
		{name: "wrong content type", status: 200, headers: http.Header{"Content-Type": []string{"text/plain"}}, body: validWhamPayload("pro", 1, 2),
			wantCode: store.SourceFailureSchemaIncompatible},
		{name: "duplicate key", status: 200, body: `{"plan_type":"pro","plan_type":"plus","rate_limit":{}}`, wantCode: store.SourceFailureSchemaIncompatible},
		{name: "null plan", status: 200, body: `{"plan_type":null,"rate_limit":{"primary_window":{"used_percent":19,"limit_window_seconds":18000,"reset_at":1784003600}}}`, wantCode: store.SourceFailureSchemaIncompatible},
		{name: "null used percent", status: 200, body: `{"plan_type":"pro","rate_limit":{"primary_window":{"used_percent":null,"limit_window_seconds":18000,"reset_at":1784003600}}}`, wantCode: store.SourceFailureSchemaIncompatible},
		{name: "null reset", status: 200, body: `{"plan_type":"pro","rate_limit":{"primary_window":{"used_percent":19,"limit_window_seconds":18000,"reset_at":null}}}`, wantCode: store.SourceFailureSchemaIncompatible},
		{name: "missing windows", status: 200, body: `{"plan_type":"pro","rate_limit":null}`, wantCode: store.SourceFailureSchemaIncompatible},
		{name: "oversized body", status: 200, body: strings.Repeat("x", 65), maxBytes: 64, wantCode: store.SourceFailureSchemaIncompatible},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			provider, err := NewMemoryCredentialProvider([]byte(syntheticAccessToken))
			if err != nil {
				t.Fatalf("NewMemoryCredentialProvider() error = %v", err)
			}
			t.Cleanup(provider.Close)
			headers := testCase.headers.Clone()
			if headers == nil {
				headers = make(http.Header)
			}
			if headers.Get("Content-Type") == "" && testCase.status == 200 {
				headers.Set("Content-Type", "application/json")
			}
			client := mustClient(t, ClientConfig{
				Transport: roundTripperFunc(func(*http.Request) (*http.Response, error) {
					return &http.Response{StatusCode: testCase.status, Header: headers, Body: io.NopCloser(strings.NewReader(testCase.body))}, nil
				}),
				Credentials: provider, MaxAttempts: 1, MaxResponseBytes: testCase.maxBytes,
				Now: fixedClock(1_784_000_000_000),
			})
			result, err := client.Fetch(context.Background(), "request-failure")
			if err != nil {
				t.Fatalf("Fetch() error = %v", err)
			}
			if len(result.Observations) != 0 || result.Failure == nil || result.Failure.Code != testCase.wantCode ||
				!equalInt64(result.Failure.RetryAtMS, testCase.wantRetryAt) {
				t.Fatalf("result = %#v", result)
			}
			output := fmt.Sprintf("%#v", result)
			if strings.Contains(output, syntheticAccessToken) || strings.Contains(output, testCase.body) && testCase.body == syntheticAccessToken {
				t.Fatalf("failure leaked body/token: %s", output)
			}
		})
	}
}

func TestClientRetriesOnlyNetworkTimeoutAndServerFailuresWithBoundedPolicy(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		steps    []doerStep
		wantCode *store.SourceFailureCode
	}{
		{
			name:  "network recovers",
			steps: []doerStep{{err: errors.New("synthetic network")}, {err: errors.New("synthetic network")}, {body: validWhamPayload("pro", 7, 9)}},
		},
		{
			name:  "server recovers",
			steps: []doerStep{{status: 500}, {status: 503}, {body: validWhamPayload("pro", 7, 9)}},
		},
		{
			name:     "timeout exhausts",
			steps:    []doerStep{{err: context.DeadlineExceeded}, {err: context.DeadlineExceeded}, {err: context.DeadlineExceeded}},
			wantCode: pointer(store.SourceFailureTimeout),
		},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			provider, err := NewMemoryCredentialProvider([]byte(syntheticAccessToken))
			if err != nil {
				t.Fatalf("NewMemoryCredentialProvider() error = %v", err)
			}
			t.Cleanup(provider.Close)
			doer := &sequenceDoer{steps: testCase.steps}
			var waits []time.Duration
			client := mustClient(t, ClientConfig{
				Transport: doer, Credentials: provider, MaxAttempts: 3,
				RetryPolicy: fixedRetryPolicy{delays: []time.Duration{10 * time.Millisecond, 20 * time.Millisecond}},
				Wait:        func(_ context.Context, delay time.Duration) error { waits = append(waits, delay); return nil },
				Now:         fixedClock(1_784_000_000_000),
			})
			result, err := client.Fetch(context.Background(), "request-retry")
			if err != nil {
				t.Fatalf("Fetch() error = %v", err)
			}
			if result.AttemptCount != 3 || doer.calls != 3 || len(waits) != 2 || waits[0] != 10*time.Millisecond || waits[1] != 20*time.Millisecond {
				t.Fatalf("attempts=%d calls=%d waits=%v", result.AttemptCount, doer.calls, waits)
			}
			if testCase.wantCode == nil {
				if result.Failure != nil || len(result.Observations) != 2 {
					t.Fatalf("recovered result = %#v", result)
				}
			} else if result.Failure == nil || result.Failure.Code != *testCase.wantCode {
				t.Fatalf("failure result = %#v", result)
			}
		})
	}
}

func TestClientRetriesResponseBodyIOFailuresWithBoundedPolicy(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		bodyErrors []error
		wantCode   *store.SourceFailureCode
	}{
		{name: "network body error recovers", bodyErrors: []error{errors.New("synthetic body network failure")}},
		{name: "timeout body error exhausts", bodyErrors: []error{context.DeadlineExceeded, context.DeadlineExceeded, context.DeadlineExceeded}, wantCode: pointer(store.SourceFailureTimeout)},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			provider, err := NewMemoryCredentialProvider([]byte(syntheticAccessToken))
			if err != nil {
				t.Fatalf("NewMemoryCredentialProvider() error = %v", err)
			}
			t.Cleanup(provider.Close)
			calls := 0
			client := mustClient(t, ClientConfig{
				Transport: roundTripperFunc(func(*http.Request) (*http.Response, error) {
					calls++
					var body io.ReadCloser
					if calls <= len(testCase.bodyErrors) {
						body = &errorBody{err: testCase.bodyErrors[calls-1]}
					} else {
						body = io.NopCloser(strings.NewReader(validWhamPayload("pro", 7, 9)))
					}
					return &http.Response{
						StatusCode: http.StatusOK,
						Header:     http.Header{"Content-Type": []string{"application/json"}},
						Body:       body,
					}, nil
				}),
				Credentials: provider, MaxAttempts: 3,
				RetryPolicy: fixedRetryPolicy{delays: []time.Duration{time.Millisecond, time.Millisecond}},
				Wait:        func(context.Context, time.Duration) error { return nil },
				Now:         fixedClock(1_784_000_000_000),
			})
			result, err := client.Fetch(context.Background(), "request-body-retry")
			if err != nil {
				t.Fatalf("Fetch() error = %v", err)
			}
			if testCase.wantCode == nil {
				if result.Failure != nil || len(result.Observations) != 2 || calls != 2 {
					t.Fatalf("recovered result = %#v, calls=%d", result, calls)
				}
			} else if result.Failure == nil || result.Failure.Code != *testCase.wantCode ||
				result.HTTPStatus != nil || result.AttemptCount != 3 || calls != 3 {
				t.Fatalf("failure result = %#v, calls=%d", result, calls)
			}
		})
	}
}

func TestClientCancellationAndMissingCredentialAreTypedWithoutHTTPRetry(t *testing.T) {
	t.Parallel()

	closed, err := NewMemoryCredentialProvider([]byte("temporary"))
	if err != nil {
		t.Fatalf("NewMemoryCredentialProvider() error = %v", err)
	}
	closed.Close()
	called := 0
	missing := mustClient(t, ClientConfig{
		Transport:   roundTripperFunc(func(*http.Request) (*http.Response, error) { called++; return nil, errors.New("must not run") }),
		Credentials: closed, Now: fixedClock(1_784_000_000_000),
	})
	result, err := missing.Fetch(context.Background(), "request-auth")
	if err != nil || result.Failure == nil || result.Failure.Code != store.SourceFailureAuthRequired || called != 0 {
		t.Fatalf("missing credential result = %#v, %v, calls=%d", result, err, called)
	}

	provider, err := NewMemoryCredentialProvider([]byte(syntheticAccessToken))
	if err != nil {
		t.Fatalf("NewMemoryCredentialProvider() error = %v", err)
	}
	t.Cleanup(provider.Close)
	cancelled := mustClient(t, ClientConfig{
		Transport: roundTripperFunc(func(request *http.Request) (*http.Response, error) {
			<-request.Context().Done()
			return nil, request.Context().Err()
		}),
		Credentials: provider, Timeout: time.Hour, Now: fixedClock(1_784_000_000_000),
	})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	result, err = cancelled.Fetch(ctx, "request-cancelled")
	if err != nil || result.Failure == nil || result.Failure.Code != store.SourceFailureCancelled || result.AttemptCount != 0 {
		t.Fatalf("cancelled result = %#v, %v", result, err)
	}
}

func TestClientCancellationDuringRetryWaitOverridesTransientFailure(t *testing.T) {
	t.Parallel()

	provider, err := NewMemoryCredentialProvider([]byte(syntheticAccessToken))
	if err != nil {
		t.Fatalf("NewMemoryCredentialProvider() error = %v", err)
	}
	t.Cleanup(provider.Close)
	ctx, cancel := context.WithCancel(context.Background())
	calls := 0
	client := mustClient(t, ClientConfig{
		Transport: roundTripperFunc(func(*http.Request) (*http.Response, error) {
			calls++
			return nil, errors.New("synthetic transient network failure")
		}),
		Credentials: provider, MaxAttempts: 3,
		RetryPolicy: fixedRetryPolicy{delays: []time.Duration{time.Second}},
		Wait: func(context.Context, time.Duration) error {
			cancel()
			return context.Canceled
		},
		Now: fixedClock(1_784_000_000_000),
	})
	result, err := client.Fetch(ctx, "request-cancel-during-wait")
	if err != nil || result.Failure == nil || result.Failure.Code != store.SourceFailureCancelled ||
		result.AttemptCount != 1 || calls != 1 {
		t.Fatalf("Fetch() = %#v, %v, calls=%d", result, err, calls)
	}
}

func TestClientClosesResponseReturnedAlongsideTransportError(t *testing.T) {
	t.Parallel()

	provider, err := NewMemoryCredentialProvider([]byte(syntheticAccessToken))
	if err != nil {
		t.Fatalf("NewMemoryCredentialProvider() error = %v", err)
	}
	t.Cleanup(provider.Close)
	body := newTrackingBody(syntheticAccessToken)
	client := mustClient(t, ClientConfig{
		Transport: roundTripperFunc(func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: 502, Body: body}, errors.New("synthetic redirect failure")
		}),
		Credentials: provider, MaxAttempts: 1, Now: fixedClock(1_784_000_000_000),
	})
	result, err := client.Fetch(context.Background(), "request-response-error")
	if err != nil || result.Failure == nil || result.Failure.Code != store.SourceFailureNetworkUnavailable {
		t.Fatalf("Fetch() = %#v, %v", result, err)
	}
	if !body.closed {
		t.Fatal("response body returned with transport error was not closed")
	}
}

func TestClientClampsFinishedTimeWhenWallClockMovesBackward(t *testing.T) {
	t.Parallel()

	provider, err := NewMemoryCredentialProvider([]byte(syntheticAccessToken))
	if err != nil {
		t.Fatalf("NewMemoryCredentialProvider() error = %v", err)
	}
	t.Cleanup(provider.Close)
	times := []time.Time{time.UnixMilli(1_784_000_000_000), time.UnixMilli(1_783_999_999_000)}
	next := 0
	client := mustClient(t, ClientConfig{
		Transport: roundTripperFunc(func(*http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: 200, Header: http.Header{"Content-Type": []string{"application/json"}},
				Body: io.NopCloser(strings.NewReader(validWhamPayload("pro", 1, 2))),
			}, nil
		}),
		Credentials: provider,
		Now: func() time.Time {
			value := times[next]
			if next < len(times)-1 {
				next++
			}
			return value
		},
	})
	result, err := client.Fetch(context.Background(), "request-clock-rollback")
	if err != nil || result.FinishedAtMS < result.StartedAtMS || len(result.Observations) != 2 {
		t.Fatalf("Fetch() = %#v, %v", result, err)
	}
	for _, observation := range result.Observations {
		if observation.ObservedAtMS != result.FinishedAtMS {
			t.Fatalf("observation time = %d, result finish = %d", observation.ObservedAtMS, result.FinishedAtMS)
		}
	}
}

func TestClientDoesNotFollowRedirectOrForwardAuthorization(t *testing.T) {
	t.Parallel()

	provider, err := NewMemoryCredentialProvider([]byte(syntheticAccessToken))
	if err != nil {
		t.Fatalf("NewMemoryCredentialProvider() error = %v", err)
	}
	t.Cleanup(provider.Close)
	calls := 0
	transport := roundTripperFunc(func(request *http.Request) (*http.Response, error) {
		calls++
		if calls > 1 {
			t.Fatalf("redirect followed to %s with Authorization=%q", request.URL, request.Header.Get("Authorization"))
		}
		return &http.Response{
			StatusCode: http.StatusFound,
			Header:     http.Header{"Location": []string{"https://subdomain.chatgpt.com/credential-sink"}},
			Body:       io.NopCloser(strings.NewReader("redirect")),
		}, nil
	})
	client := mustClient(t, ClientConfig{
		Transport: transport, Credentials: provider,
		MaxAttempts: 1, Now: fixedClock(1_784_000_000_000),
	})
	result, err := client.Fetch(context.Background(), "request-no-redirect")
	if err != nil || result.Failure == nil || result.Failure.Code != store.SourceFailureSchemaIncompatible || calls != 1 {
		t.Fatalf("Fetch() = %#v, %v, calls=%d", result, err, calls)
	}
}

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (function roundTripperFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}

type trackingBody struct {
	reader io.Reader
	closed bool
}

func newTrackingBody(value string) *trackingBody {
	return &trackingBody{reader: strings.NewReader(value)}
}

func (body *trackingBody) Read(target []byte) (int, error) { return body.reader.Read(target) }
func (body *trackingBody) Close() error                    { body.closed = true; return nil }

type contextAwareBody struct {
	ctx             context.Context
	reader          io.Reader
	readWhileActive bool
	closed          bool
}

func (body *contextAwareBody) Read(target []byte) (int, error) {
	if err := body.ctx.Err(); err != nil {
		return 0, err
	}
	body.readWhileActive = true
	return body.reader.Read(target)
}

func (body *contextAwareBody) Close() error {
	body.closed = true
	return nil
}

type errorBody struct {
	err    error
	closed bool
}

func (body *errorBody) Read([]byte) (int, error) { return 0, body.err }
func (body *errorBody) Close() error             { body.closed = true; return nil }

type fixedRetryPolicy struct{ delays []time.Duration }

func (policy fixedRetryPolicy) Delay(attempt int) (time.Duration, bool, error) {
	if attempt < 1 || attempt > len(policy.delays) {
		return 0, false, nil
	}
	return policy.delays[attempt-1], true, nil
}

type doerStep struct {
	status int
	body   string
	err    error
}

type sequenceDoer struct {
	mu    sync.Mutex
	steps []doerStep
	calls int
}

func (doer *sequenceDoer) RoundTrip(*http.Request) (*http.Response, error) {
	doer.mu.Lock()
	defer doer.mu.Unlock()
	step := doer.steps[doer.calls]
	doer.calls++
	if step.err != nil {
		return nil, step.err
	}
	status := step.status
	if status == 0 {
		status = http.StatusOK
	}
	body := step.body
	if body == "" && status == http.StatusOK {
		body = validWhamPayload("pro", 7, 9)
	}
	return &http.Response{
		StatusCode: status,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(body)),
	}, nil
}

func mustClient(t *testing.T, config ClientConfig) *Client {
	t.Helper()
	client, err := NewClient(config)
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	return client
}

func clientForBody(t *testing.T, body string) *Client {
	t.Helper()
	provider, err := NewMemoryCredentialProvider([]byte(syntheticAccessToken))
	if err != nil {
		t.Fatalf("NewMemoryCredentialProvider() error = %v", err)
	}
	t.Cleanup(provider.Close)
	return mustClient(t, ClientConfig{
		Transport: roundTripperFunc(func(*http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"application/json"}},
				Body: io.NopCloser(strings.NewReader(body)),
			}, nil
		}),
		Credentials: provider, Now: fixedClock(1_784_000_000_000),
	})
}

func fixedClock(milliseconds int64) func() time.Time {
	return func() time.Time { return time.UnixMilli(milliseconds).UTC() }
}

func validWhamPayload(plan string, primary, secondary float64) string {
	return fmt.Sprintf(`{
  "plan_type": %q,
  "rate_limit": {
    "allowed": true,
    "limit_reached": false,
    "primary_window": {
      "used_percent": %v,
      "limit_window_seconds": 18000,
      "reset_after_seconds": 3600,
      "reset_at": 1784003600
    },
    "secondary_window": {
      "used_percent": %v,
      "limit_window_seconds": 604800,
      "reset_after_seconds": 604800,
      "reset_at": 1784604800
    }
  },
  "credits": {"unlimited": false, "balance": "0"}
}`, plan, primary, secondary)
}

func equalQuotaReason(left, right *store.QuotaRejectionReason) bool {
	return left == nil && right == nil || left != nil && right != nil && *left == *right
}

func equalInt64(left, right *int64) bool {
	return left == nil && right == nil || left != nil && right != nil && *left == *right
}

func pointer[T any](value T) *T { return &value }
