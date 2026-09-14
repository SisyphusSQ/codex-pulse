package quota

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/SisyphusSQ/codex-pulse/internal/codex/accountbinding"
	"github.com/SisyphusSQ/codex-pulse/internal/codex/appserver"
	"github.com/SisyphusSQ/codex-pulse/internal/store"
	storesqlite "github.com/SisyphusSQ/codex-pulse/internal/store/sqlite"
)

func TestServiceRecordsTypedResultEvenWhenCallerCancelled(t *testing.T) {
	t.Parallel()

	key := testScopeKey(0x21)
	request := testBoundRequest(t, key, "acct-test-a", "request-service-cancel")
	client := mustQuotaClient(t, key, &scriptedRateLimitsReader{errs: []error{context.Canceled}})
	recorder := &captureRecorder{}
	service, err := NewService(client, recorder, time.Second)
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	service.SetBinding(request.Binding)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	result, err := service.Fetch(ctx, request.RequestID)
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

	key := testScopeKey(0x22)
	request := testBoundRequest(t, key, "acct-test-a", "request-recorder-error")
	client := mustQuotaClient(t, key, &scriptedRateLimitsReader{snapshots: []appserver.AccountRateLimitsSnapshot{
		testRateLimitsSnapshot("acct-test-a", 13, 21, nil),
	}})
	want := errors.New("synthetic recorder unavailable")
	recorder := &captureRecorder{err: want}
	service, err := NewService(client, recorder, time.Second)
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	service.SetBinding(request.Binding)
	result, err := service.Fetch(context.Background(), request.RequestID)
	if !errors.Is(err, want) || len(result.Observations) != 2 || result.Failure != nil || recorder.calls != 1 {
		t.Fatalf("Fetch() = %#v, %v, recorder=%#v", result, err, recorder)
	}
}

func TestServicePersistsPreRequestCancellationWithZeroAppServerAttempts(t *testing.T) {
	t.Parallel()

	key := testScopeKey(0x23)
	request := testBoundRequest(t, key, "acct-test-a", "request-persisted-cancel")
	reader := &scriptedRateLimitsReader{errs: []error{errors.New("must not be called")}}
	client := mustQuotaClient(t, key, reader)
	repository := newQuotaServiceTestRepository(t, key)
	service, err := NewService(client, repository, time.Second)
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	service.SetBinding(request.Binding)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	result, err := service.Fetch(ctx, request.RequestID)
	if err != nil || result.Failure == nil || result.Failure.Code != store.SourceFailureCancelled || reader.calls != 0 {
		t.Fatalf("Fetch() = %#v, %v, calls=%d", result, err, reader.calls)
	}
	attempts, err := repository.ListSourceAttempts(context.Background(), store.QuotaSourceInstanceAppServer(request.Binding.AccountScope), 10)
	if err != nil || len(attempts) != 1 || attempts[0].AttemptCount != 0 ||
		attempts[0].FailureCode == nil || *attempts[0].FailureCode != store.SourceFailureCancelled {
		t.Fatalf("persisted attempts = %#v, %v", attempts, err)
	}
}

func TestServicePersistsMixedRetryFailuresWithoutHTTPStatus(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		errs     []error
		wait     func(context.Context, time.Duration) error
		wantCode store.SourceFailureCode
	}{
		{
			name:     "server then network",
			errs:     []error{appserver.RPCError{Code: 1}, errors.New("synthetic network")},
			wantCode: store.SourceFailureNetworkUnavailable,
		},
		{
			name:     "server then timeout",
			errs:     []error{appserver.RPCError{Code: 1}, context.DeadlineExceeded},
			wantCode: store.SourceFailureTimeout,
		},
		{
			name: "server then retry wait cancellation",
			errs: []error{appserver.RPCError{Code: 1}},
			wait: func(context.Context, time.Duration) error {
				return context.Canceled
			},
			wantCode: store.SourceFailureCancelled,
		},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			key := testScopeKey(0x24)
			request := testBoundRequest(t, key, "acct-test-a", "request-mixed-"+strings.ReplaceAll(testCase.name, " ", "-"))
			wait := testCase.wait
			if wait == nil {
				wait = func(context.Context, time.Duration) error { return nil }
			}
			client, err := NewClient(ClientConfig{
				Reader: &scriptedRateLimitsReader{errs: testCase.errs}, ScopeKey: key,
				MaxAttempts: 2, RetryPolicy: fixedRetryPolicy{delays: []time.Duration{time.Millisecond}},
				Wait: wait, Now: fixedClock(1_784_000_000_000),
			})
			if err != nil {
				t.Fatalf("NewClient() error = %v", err)
			}
			repository := newQuotaServiceTestRepository(t, key)
			service, err := NewService(client, repository, time.Second)
			if err != nil {
				t.Fatalf("NewService() error = %v", err)
			}
			service.SetBinding(request.Binding)
			result, err := service.Fetch(context.Background(), request.RequestID)
			if err != nil {
				t.Fatalf("Fetch() error = %v, result = %#v", err, result)
			}
			if result.Failure == nil || result.Failure.Code != testCase.wantCode || result.HTTPStatus != nil {
				t.Fatalf("result = %#v", result)
			}
			attempts, err := repository.ListSourceAttempts(context.Background(), store.QuotaSourceInstanceAppServer(request.Binding.AccountScope), 10)
			if err != nil || len(attempts) != 1 || attempts[0].RequestID != request.RequestID ||
				attempts[0].FailureCode == nil || *attempts[0].FailureCode != testCase.wantCode || attempts[0].HTTPStatus != nil {
				t.Fatalf("attempts = %#v, %v", attempts, err)
			}
		})
	}
}

func TestServicePersistsAppServerFailureWithoutForgedHTTPStatus(t *testing.T) {
	t.Parallel()

	key := testScopeKey(0x25)
	request := testBoundRequest(t, key, "acct-test-a", "request-server-error")
	client, err := NewClient(ClientConfig{
		Reader:   &scriptedRateLimitsReader{errs: []error{appserver.RPCError{Code: 1}}},
		ScopeKey: key, MaxAttempts: 1, Now: fixedClock(1_784_000_000_000),
	})
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	repository := newQuotaServiceTestRepository(t, key)
	service, err := NewService(client, repository, time.Second)
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	service.SetBinding(request.Binding)
	result, err := service.Fetch(context.Background(), request.RequestID)
	if err != nil {
		t.Fatalf("Fetch() error = %v, result = %#v", err, result)
	}
	if result.Failure == nil || result.Failure.Code != store.SourceFailureServerError ||
		result.HTTPStatus != nil || result.Failure.RetryAtMS != nil {
		t.Fatalf("result = %#v", result)
	}
	attempts, err := repository.ListSourceAttempts(context.Background(), store.QuotaSourceInstanceAppServer(request.Binding.AccountScope), 10)
	if err != nil || len(attempts) != 1 || attempts[0].HTTPStatus != nil || attempts[0].RetryAtMS != nil {
		t.Fatalf("attempts = %#v, %v", attempts, err)
	}
}

func TestServiceDoesNotRecordAfterSQLiteAccountFenceChanges(t *testing.T) {
	t.Parallel()

	key := testScopeKey(0x26)
	request := testBoundRequest(t, key, "acct-test-a", "request-stale-fence")
	repository := newQuotaServiceTestRepository(t, key)
	client := mustQuotaClient(t, key, &scriptedRateLimitsReader{snapshots: []appserver.AccountRateLimitsSnapshot{
		testRateLimitsSnapshot("acct-test-a", 13, 21, nil),
	}})
	service, err := NewService(client, repository, time.Second)
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	service.SetBinding(request.Binding)
	scopeB, err := accountbinding.DeriveScope(key, []byte("acct-test-b"))
	if err != nil {
		t.Fatalf("DeriveScope(B) error = %v", err)
	}
	if _, _, err := repository.ConfirmCodexAccountBinding(
		context.Background(), scopeB, 1_784_000_000_100, store.CodexAccountBindingReasonAccountChanged,
	); err != nil {
		t.Fatalf("ConfirmCodexAccountBinding(B) error = %v", err)
	}
	result, err := service.FetchBound(context.Background(), request)
	if !errors.Is(err, store.ErrCodexAccountBindingChanged) {
		t.Fatalf("FetchBound() = %#v, %v, want ErrCodexAccountBindingChanged", result, err)
	}
	if attempts, listErr := repository.ListSourceAttempts(
		context.Background(), store.QuotaSourceInstanceAppServer(request.Binding.AccountScope), 10,
	); listErr != nil || len(attempts) != 0 {
		t.Fatalf("ListSourceAttempts() = %#v, %v", attempts, listErr)
	}
	if _, stateErr := repository.SourceState(
		context.Background(), store.QuotaSourceInstanceAppServer(request.Binding.AccountScope),
	); !errors.Is(stateErr, store.ErrNotFound) {
		t.Fatalf("SourceState() error = %v, want ErrNotFound", stateErr)
	}
}

func newQuotaServiceTestRepository(t *testing.T, key [32]byte) *store.Repository {
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
	stored, err := repository.EnsureCodexAccountScopeKey(context.Background(), key, 1_784_000_000_000)
	if err != nil {
		t.Fatalf("EnsureCodexAccountScopeKey() error = %v", err)
	}
	scope, err := accountbinding.DeriveScope(stored, []byte("acct-test-a"))
	if err != nil {
		t.Fatalf("DeriveScope() error = %v", err)
	}
	if _, _, err := repository.ConfirmCodexAccountBinding(
		context.Background(), scope, 1_784_000_000_000, store.CodexAccountBindingReasonStartup,
	); err != nil {
		t.Fatalf("ConfirmCodexAccountBinding() error = %v", err)
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
