package quota

import (
	"context"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/SisyphusSQ/codex-pulse/internal/store"
	storesqlite "github.com/SisyphusSQ/codex-pulse/internal/store/sqlite"
)

func TestServiceRecordsTypedResultAfterCredentialReleaseEvenWhenCallerCancelled(t *testing.T) {
	t.Parallel()

	provider, err := NewMemoryCredentialProvider([]byte(syntheticAccessToken))
	if err != nil {
		t.Fatalf("NewMemoryCredentialProvider() error = %v", err)
	}
	t.Cleanup(provider.Close)
	client := mustClient(t, ClientConfig{
		Transport: roundTripperFunc(func(request *http.Request) (*http.Response, error) {
			<-request.Context().Done()
			return nil, request.Context().Err()
		}),
		Credentials: provider, Timeout: time.Hour, Now: fixedClock(1_784_000_000_000),
	})
	recorder := &captureRecorder{}
	service, err := NewService(client, recorder, time.Second)
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	result, err := service.Fetch(ctx, "request-service-cancel")
	if err != nil {
		t.Fatalf("Fetch() error = %v", err)
	}
	if result.Failure == nil || result.Failure.Code != store.SourceFailureCancelled || recorder.calls != 1 {
		t.Fatalf("result=%#v recorder=%#v", result, recorder)
	}
	if recorder.ctxErr != nil {
		t.Fatalf("recorder inherited cancelled context: %v", recorder.ctxErr)
	}
	if recorder.record.Attempt.FailureCode == nil || *recorder.record.Attempt.FailureCode != store.SourceFailureCancelled ||
		recorder.record.Attempt.Outcome != store.SourceAttemptCancelled || len(recorder.record.Observations) != 0 {
		t.Fatalf("record = %#v", recorder.record)
	}
}

func TestServiceReturnsSafeRecorderFailureWithoutChangingFetchResult(t *testing.T) {
	t.Parallel()

	client := clientForBody(t, validWhamPayload("pro", 13, 21))
	want := errors.New("synthetic recorder unavailable")
	recorder := &captureRecorder{err: want}
	service, err := NewService(client, recorder, time.Second)
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	result, err := service.Fetch(context.Background(), "request-recorder-error")
	if !errors.Is(err, want) || len(result.Observations) != 2 || result.Failure != nil || recorder.calls != 1 {
		t.Fatalf("Fetch() = %#v, %v, recorder=%#v", result, err, recorder)
	}
}

func TestServicePersistsPreRequestCancellationWithZeroHTTPAttempts(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()
	if err := os.Chmod(directory, 0o700); err != nil {
		t.Fatalf("Chmod(temp dir) error = %v", err)
	}
	database, err := storesqlite.Open(context.Background(), storesqlite.Config{
		Path: filepath.Join(directory, "quota-service.db"),
	})
	if err != nil {
		t.Fatalf("sqlite.Open() error = %v", err)
	}
	t.Cleanup(func() {
		if err := database.Close(context.Background()); err != nil {
			t.Errorf("Close() error = %v", err)
		}
	})
	repository := store.NewRepository(database)
	if err := repository.EnsureApplicationSchema(context.Background()); err != nil {
		t.Fatalf("EnsureApplicationSchema() error = %v", err)
	}
	provider, err := NewMemoryCredentialProvider([]byte(syntheticAccessToken))
	if err != nil {
		t.Fatalf("NewMemoryCredentialProvider() error = %v", err)
	}
	t.Cleanup(provider.Close)
	httpCalls := 0
	client := mustClient(t, ClientConfig{
		Transport: roundTripperFunc(func(*http.Request) (*http.Response, error) {
			httpCalls++
			return nil, errors.New("must not be called")
		}),
		Credentials: provider, Now: fixedClock(1_784_000_000_000),
	})
	service, err := NewService(client, repository, time.Second)
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	result, err := service.Fetch(ctx, "request-persisted-cancel")
	if err != nil || result.Failure == nil || result.Failure.Code != store.SourceFailureCancelled || httpCalls != 0 {
		t.Fatalf("Fetch() = %#v, %v, calls=%d", result, err, httpCalls)
	}
	attempts, err := repository.ListSourceAttempts(context.Background(), store.QuotaSourceInstanceWhamDefault, 10)
	if err != nil || len(attempts) != 1 || attempts[0].AttemptCount != 0 ||
		attempts[0].FailureCode == nil || *attempts[0].FailureCode != store.SourceFailureCancelled {
		t.Fatalf("persisted attempts = %#v, %v", attempts, err)
	}
}

func TestServicePersistsMixedRetryFailuresWithoutStaleHTTPStatus(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		steps    []doerStep
		wait     func(context.Context, time.Duration) error
		wantCode store.SourceFailureCode
	}{
		{
			name: "server then network", steps: []doerStep{{status: 503}, {err: errors.New("synthetic network")}},
			wantCode: store.SourceFailureNetworkUnavailable,
		},
		{
			name: "server then timeout", steps: []doerStep{{status: 503}, {err: context.DeadlineExceeded}},
			wantCode: store.SourceFailureTimeout,
		},
		{
			name: "server then retry wait cancellation", steps: []doerStep{{status: 503}},
			wait: func(context.Context, time.Duration) error {
				return context.Canceled
			},
			wantCode: store.SourceFailureCancelled,
		},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			repository := newQuotaServiceTestRepository(t)
			provider, err := NewMemoryCredentialProvider([]byte(syntheticAccessToken))
			if err != nil {
				t.Fatalf("NewMemoryCredentialProvider() error = %v", err)
			}
			t.Cleanup(provider.Close)
			wait := testCase.wait
			if wait == nil {
				wait = func(context.Context, time.Duration) error { return nil }
			}
			client := mustClient(t, ClientConfig{
				Transport: &sequenceDoer{steps: testCase.steps}, Credentials: provider, MaxAttempts: 2,
				RetryPolicy: fixedRetryPolicy{delays: []time.Duration{time.Millisecond}},
				Wait:        wait, Now: fixedClock(1_784_000_000_000),
			})
			service, err := NewService(client, repository, time.Second)
			if err != nil {
				t.Fatalf("NewService() error = %v", err)
			}
			requestID := "request-mixed-" + strings.ReplaceAll(testCase.name, " ", "-")
			result, err := service.Fetch(context.Background(), requestID)
			if err != nil {
				t.Fatalf("Fetch() error = %v, result = %#v", err, result)
			}
			if result.Failure == nil || result.Failure.Code != testCase.wantCode || result.HTTPStatus != nil {
				t.Fatalf("result = %#v", result)
			}
			attempts, err := repository.ListSourceAttempts(context.Background(), store.QuotaSourceInstanceWhamDefault, 10)
			if err != nil || len(attempts) != 1 || attempts[0].RequestID != requestID ||
				attempts[0].FailureCode == nil || *attempts[0].FailureCode != testCase.wantCode || attempts[0].HTTPStatus != nil {
				t.Fatalf("attempts = %#v, %v", attempts, err)
			}
		})
	}
}

func TestServicePersistsRateLimitHintAtFinishedBoundary(t *testing.T) {
	t.Parallel()

	repository := newQuotaServiceTestRepository(t)
	provider, err := NewMemoryCredentialProvider([]byte(syntheticAccessToken))
	if err != nil {
		t.Fatalf("NewMemoryCredentialProvider() error = %v", err)
	}
	t.Cleanup(provider.Close)
	const baseMS = int64(1_784_000_000_000)
	nowCalls := int64(0)
	client := mustClient(t, ClientConfig{
		Transport: roundTripperFunc(func(*http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusTooManyRequests,
				Header:     http.Header{"Retry-After": []string{"0"}},
				Body:       http.NoBody,
			}, nil
		}),
		Credentials: provider, MaxAttempts: 1,
		Now: func() time.Time {
			value := time.UnixMilli(baseMS + nowCalls).UTC()
			nowCalls++
			return value
		},
	})
	service, err := NewService(client, repository, time.Second)
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	result, err := service.Fetch(context.Background(), "request-rate-limit-boundary")
	if err != nil {
		t.Fatalf("Fetch() error = %v, result = %#v", err, result)
	}
	if result.Failure == nil || result.Failure.Code != store.SourceFailureHTTP429 ||
		result.Failure.RetryAtMS == nil || *result.Failure.RetryAtMS < result.FinishedAtMS {
		t.Fatalf("result = %#v", result)
	}
	attempts, err := repository.ListSourceAttempts(context.Background(), store.QuotaSourceInstanceWhamDefault, 10)
	if err != nil || len(attempts) != 1 || attempts[0].RetryAtMS == nil || *attempts[0].RetryAtMS < attempts[0].FinishedAtMS {
		t.Fatalf("attempts = %#v, %v", attempts, err)
	}
}

func newQuotaServiceTestRepository(t *testing.T) *store.Repository {
	t.Helper()
	directory := t.TempDir()
	if err := os.Chmod(directory, 0o700); err != nil {
		t.Fatalf("Chmod(temp dir) error = %v", err)
	}
	database, err := storesqlite.Open(context.Background(), storesqlite.Config{
		Path: filepath.Join(directory, "quota-service.db"),
	})
	if err != nil {
		t.Fatalf("sqlite.Open() error = %v", err)
	}
	t.Cleanup(func() {
		if err := database.Close(context.Background()); err != nil {
			t.Errorf("Close() error = %v", err)
		}
	})
	repository := store.NewRepository(database)
	if err := repository.EnsureApplicationSchema(context.Background()); err != nil {
		t.Fatalf("EnsureApplicationSchema() error = %v", err)
	}
	return repository
}

type captureRecorder struct {
	calls  int
	ctxErr error
	record store.QuotaFetchRecord
	err    error
}

func (recorder *captureRecorder) RecordQuotaFetch(ctx context.Context, record store.QuotaFetchRecord) error {
	recorder.calls++
	recorder.ctxErr = ctx.Err()
	recorder.record = record
	return recorder.err
}
