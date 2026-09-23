package quota

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/SisyphusSQ/codex-pulse/internal/codex/accountbinding"
	"github.com/SisyphusSQ/codex-pulse/internal/codex/appserver"
	"github.com/SisyphusSQ/codex-pulse/internal/store"
)

func TestClientFetchesBoundAppServerObservations(t *testing.T) {
	t.Parallel()

	key := testScopeKey(0x11)
	request := testBoundRequest(t, key, "acct-test-a", "request-success")
	reader := &scriptedRateLimitsReader{snapshots: []appserver.AccountRateLimitsSnapshot{
		testRateLimitsSnapshot("acct-test-a", 42, 8, nil),
	}}
	client := mustQuotaClient(t, key, reader)
	result, err := client.Fetch(context.Background(), request)
	if err != nil {
		t.Fatalf("Fetch() error = %v", err)
	}
	if result.Failure != nil || result.AttemptCount != 1 || result.HTTPStatus != nil ||
		result.PayloadSHA256 == nil || len(result.Observations) != 2 {
		t.Fatalf("result = %#v", result)
	}
	if reader.lastExcludeResetCreditDetails != true {
		t.Fatal("quota fetch must exclude reset credit details")
	}
	primary := result.Observations[0]
	if primary.Source != store.QuotaSourceAppServer || primary.AccountScope != request.Binding.AccountScope ||
		primary.LimitID == nil || *primary.LimitID != "codex" || primary.WindowKind != store.QuotaWindowPrimary ||
		primary.UsedPercent != 42 || primary.WindowMinutes != 300 || primary.ResetsAtMS != 1_784_003_600_000 ||
		primary.PlanType == nil || *primary.PlanType != "pro" || primary.Validity != store.QuotaValidityAccepted {
		t.Fatalf("primary observation = %#v", primary)
	}
	if result.Observations[1].WindowKind != store.QuotaWindowSecondary || result.Observations[1].UsedPercent != 8 {
		t.Fatalf("secondary observation = %#v", result.Observations[1])
	}
	if strings.Contains(fmt.Sprintf("%#v", result), "acct-test-a") {
		t.Fatalf("result leaked raw account id: %#v", result)
	}
}

func TestClientRejectsBoundSnapshotFromDifferentAccount(t *testing.T) {
	t.Parallel()

	key := testScopeKey(0x11)
	request := testBoundRequest(t, key, "acct-test-a", "request-mismatch")
	client := mustQuotaClient(t, key, &scriptedRateLimitsReader{snapshots: []appserver.AccountRateLimitsSnapshot{
		testRateLimitsSnapshot("acct-test-b", 10, 1, nil),
	}})
	result, err := client.Fetch(context.Background(), request)
	if !errors.Is(err, store.ErrCodexAccountBindingChanged) || len(result.Observations) != 0 {
		t.Fatalf("Fetch() = %#v, %v", result, err)
	}
	if strings.Contains(err.Error(), "acct-test") {
		t.Fatalf("error leaked account id: %v", err)
	}
}

func TestClientTreatsMissingAccountIDAsIdentityUnavailable(t *testing.T) {
	t.Parallel()

	key := testScopeKey(0x12)
	request := testBoundRequest(t, key, "acct-test-a", "request-missing-id")
	snapshot := testRateLimitsSnapshot("acct-test-a", 3, 1, nil)
	snapshot.AccountID = nil
	client := mustQuotaClient(t, key, &scriptedRateLimitsReader{snapshots: []appserver.AccountRateLimitsSnapshot{snapshot}})
	result, err := client.Fetch(t.Context(), request)
	if !errors.Is(err, store.ErrCodexAccountBindingChanged) || result.Failure != nil ||
		len(result.Observations) != 0 {
		t.Fatalf("Fetch() = %#v, %v", result, err)
	}
}

func TestClientTreatsIdentityUnavailableReaderErrorAsBindingChange(t *testing.T) {
	t.Parallel()

	key := testScopeKey(0x12)
	request := testBoundRequest(t, key, "acct-test-a", "request-identity-unavailable")
	client := mustQuotaClient(t, key, &scriptedRateLimitsReader{
		errs: []error{appserver.ErrAccountIdentityUnavailable},
	})
	result, err := client.Fetch(t.Context(), request)
	if !errors.Is(err, store.ErrCodexAccountBindingChanged) || result.Failure != nil ||
		len(result.Observations) != 0 {
		t.Fatalf("Fetch() = %#v, %v", result, err)
	}
}

func TestClientPrefersRateLimitsByLimitID(t *testing.T) {
	t.Parallel()

	key := testScopeKey(0x13)
	request := testBoundRequest(t, key, "acct-test-a", "request-by-limit")
	primaryMins := int64(10_080)
	resets := int64(1_784_691_200)
	plan := "pro"
	codexID, sparkID := "codex", "codex_spark"
	sparkName := "GPT-5.3-Codex-Spark"
	snapshot := appserver.AccountRateLimitsSnapshot{
		AccountID: appserver.SensitiveAccountID("acct-test-a"),
		RateLimits: appserver.RateLimitSnapshot{
			LimitID: &codexID, PlanType: &plan,
			Primary: &appserver.RateLimitWindow{UsedPercent: 99, WindowDurationMins: &primaryMins, ResetsAtSeconds: &resets},
		},
		RateLimitsByLimitID: map[string]appserver.RateLimitSnapshot{
			"codex": {
				LimitID: &codexID, PlanType: &plan,
				Primary: &appserver.RateLimitWindow{UsedPercent: 80, WindowDurationMins: &primaryMins, ResetsAtSeconds: &resets},
			},
			"codex_spark": {
				LimitID: &sparkID, LimitName: &sparkName, PlanType: &plan,
				Primary: &appserver.RateLimitWindow{UsedPercent: 0, WindowDurationMins: &primaryMins, ResetsAtSeconds: &resets},
			},
		},
	}
	client := mustQuotaClient(t, key, &scriptedRateLimitsReader{snapshots: []appserver.AccountRateLimitsSnapshot{snapshot}})
	result, err := client.Fetch(context.Background(), request)
	if err != nil || result.Failure != nil || len(result.Observations) != 2 {
		t.Fatalf("Fetch() = %#v, %v", result, err)
	}
	if *result.Observations[0].LimitID != "codex" || result.Observations[0].UsedPercent != 80 ||
		*result.Observations[1].LimitID != "codex_spark" || result.Observations[1].UsedPercent != 0 ||
		result.Observations[1].LimitName == nil || *result.Observations[1].LimitName != sparkName {
		t.Fatalf("observations = %#v", result.Observations)
	}
}

func TestClientFailsClosedWhenRetrySeesDifferentAccountScope(t *testing.T) {
	t.Parallel()

	key := testScopeKey(0x14)
	request := testBoundRequest(t, key, "acct-test-a", "request-retry-mismatch")
	reader := &scriptedRateLimitsReader{
		errs:      []error{appserver.RPCError{Code: 1}, nil},
		snapshots: []appserver.AccountRateLimitsSnapshot{{}, testRateLimitsSnapshot("acct-test-b", 4, 1, nil)},
	}
	client := mustQuotaClient(t, key, reader)
	result, err := client.Fetch(context.Background(), request)
	if !errors.Is(err, store.ErrCodexAccountBindingChanged) || len(result.Observations) != 0 {
		t.Fatalf("Fetch() = %#v, %v", result, err)
	}
	if reader.calls != 2 {
		t.Fatalf("reader calls = %d, want 2", reader.calls)
	}
}

func TestClientClassifiesCancelAndCapabilityFailures(t *testing.T) {
	t.Parallel()

	key := testScopeKey(0x15)
	request := testBoundRequest(t, key, "acct-test-a", "request-classes")
	cancelled := mustQuotaClient(t, key, &scriptedRateLimitsReader{errs: []error{context.Canceled}})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	result, err := cancelled.Fetch(ctx, request)
	if err != nil || result.Failure == nil || result.Failure.Code != store.SourceFailureCancelled {
		t.Fatalf("cancelled Fetch() = %#v, %v", result, err)
	}

	missing := mustQuotaClient(t, key, &scriptedRateLimitsReader{errs: []error{appserver.ErrCapabilityUnavailable}})
	result, err = missing.Fetch(context.Background(), request)
	if err != nil || result.Failure == nil || result.Failure.Code != store.SourceFailureSchemaIncompatible {
		t.Fatalf("capability Fetch() = %#v, %v", result, err)
	}

	incompatible := mustQuotaClient(t, key, &scriptedRateLimitsReader{errs: []error{appserver.ErrRateLimitsSchemaIncompatible}})
	result, err = incompatible.Fetch(context.Background(), request)
	if err != nil || result.Failure == nil || result.Failure.Code != store.SourceFailureSchemaIncompatible {
		t.Fatalf("schema Fetch() = %#v, %v", result, err)
	}
}

func TestClassifyAppServerLocalRuntimeFailureStaysRetryable(t *testing.T) {
	t.Parallel()
	for _, err := range []error{
		appserver.ErrNodeRuntimeUnavailable,
		appserver.ErrCodexBinaryUnavailable,
		appserver.ErrCodexLaunchFailed,
		errors.Join(appserver.ErrCapabilityUnavailable, appserver.ErrNodeRuntimeUnavailable),
	} {
		code := classifyAppServerError(context.Background(), err)
		if code != store.SourceFailureNetworkUnavailable || !retryableAppServerFailure(code) {
			t.Fatalf("classifyAppServerError(%v) = %q", err, code)
		}
	}
}

func TestClientDoesNotImmediatelyRetryMissingLocalRuntime(t *testing.T) {
	t.Parallel()
	key := testScopeKey(0x16)
	reader := &scriptedRateLimitsReader{errs: []error{appserver.ErrNodeRuntimeUnavailable}}
	client := mustQuotaClient(t, key, reader)
	result, err := client.Fetch(context.Background(), testBoundRequest(t, key, "acct-test-a", "missing-node"))
	if err != nil || result.Failure == nil || result.Failure.Code != store.SourceFailureNetworkUnavailable || reader.calls != 1 {
		t.Fatalf("missing runtime result = %#v, err=%v, calls=%d", result, err, reader.calls)
	}
}

func TestResetCreditsClientPreservesNullCreditsAndNullableExpiry(t *testing.T) {
	t.Parallel()

	key := testScopeKey(0x16)
	request := testBoundRequest(t, key, "acct-test-a", "reset-null")
	granted := int64(1_783_000_000)
	snapshot := testRateLimitsSnapshot("acct-test-a", 1, 1, &appserver.RateLimitResetCreditsSummary{
		AvailableCount: 3,
		Credits:        nil,
	})
	client := mustResetCreditsClient(t, key, &scriptedRateLimitsReader{snapshots: []appserver.AccountRateLimitsSnapshot{snapshot}})
	result, err := client.Fetch(context.Background(), request)
	if err != nil || result.Failure != nil || result.Snapshot == nil ||
		result.Snapshot.AvailableCount != 3 || result.Snapshot.DetailsStatus != store.ResetCreditDetailsUnavailable ||
		result.Snapshot.Credits != nil {
		t.Fatalf("null credits result = %#v, %v", result, err)
	}
	if client.base.excludeResetCreditDetails {
		t.Fatal("reset credits fetch must include credit details")
	}

	partial := testRateLimitsSnapshot("acct-test-a", 1, 1, &appserver.RateLimitResetCreditsSummary{
		AvailableCount: 4,
		Credits: []appserver.RateLimitResetCredit{{
			ID: "credit-partial", Status: "available", ResetType: "codexRateLimits",
			GrantedAtSeconds: granted,
		}},
	})
	client = mustResetCreditsClient(t, key, &scriptedRateLimitsReader{snapshots: []appserver.AccountRateLimitsSnapshot{partial}})
	result, err = client.Fetch(context.Background(), request)
	if err != nil || result.Snapshot == nil || result.Snapshot.DetailsStatus != store.ResetCreditDetailsPartial ||
		result.Snapshot.AvailableCount != 4 || len(result.Snapshot.Credits) != 1 ||
		result.Snapshot.Credits[0].ExpiresAtMS != nil {
		t.Fatalf("partial credits result = %#v, %v", result, err)
	}
	if strings.Contains(fmt.Sprintf("%#v", result), "credit-partial") {
		t.Fatalf("result leaked credit id: %#v", result)
	}
}

type scriptedRateLimitsReader struct {
	mu                            sync.Mutex
	snapshots                     []appserver.AccountRateLimitsSnapshot
	errs                          []error
	calls                         int
	lastExcludeResetCreditDetails bool
}

func (reader *scriptedRateLimitsReader) Read(
	ctx context.Context,
	excludeResetCreditDetails bool,
) (appserver.AccountRateLimitsSnapshot, error) {
	reader.mu.Lock()
	defer reader.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return appserver.AccountRateLimitsSnapshot{}, err
	}
	index := reader.calls
	reader.calls++
	reader.lastExcludeResetCreditDetails = excludeResetCreditDetails
	if index < len(reader.errs) && reader.errs[index] != nil {
		return appserver.AccountRateLimitsSnapshot{}, reader.errs[index]
	}
	if index >= len(reader.snapshots) {
		return appserver.AccountRateLimitsSnapshot{}, errors.New("unexpected App Server read")
	}
	return cloneRateLimitsSnapshot(reader.snapshots[index]), nil
}

func mustQuotaClient(t *testing.T, key [32]byte, reader AccountRateLimitsReader) *Client {
	t.Helper()
	client, err := NewClient(ClientConfig{
		Reader: reader, ScopeKey: key, Now: fixedClock(1_784_000_000_000),
		Wait:        func(context.Context, time.Duration) error { return nil },
		RetryPolicy: fixedRetryPolicy{delays: []time.Duration{time.Millisecond, time.Millisecond}},
	})
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	return client
}

func mustResetCreditsClient(t *testing.T, key [32]byte, reader AccountRateLimitsReader) *ResetCreditsClient {
	t.Helper()
	client, err := NewResetCreditsClient(ClientConfig{
		Reader: reader, ScopeKey: key, Now: fixedClock(1_784_000_000_000),
		Wait:        func(context.Context, time.Duration) error { return nil },
		RetryPolicy: fixedRetryPolicy{delays: []time.Duration{time.Millisecond, time.Millisecond}},
	})
	if err != nil {
		t.Fatalf("NewResetCreditsClient() error = %v", err)
	}
	return client
}

func testBoundRequest(t *testing.T, key [32]byte, accountID, requestID string) BoundRefreshRequest {
	t.Helper()
	scope, err := accountbinding.DeriveScope(key, []byte(accountID))
	if err != nil {
		t.Fatalf("DeriveScope() error = %v", err)
	}
	return BoundRefreshRequest{
		RequestID: requestID,
		Binding:   AccountBindingFence{AccountScope: scope, BindingGeneration: 1},
	}
}

func testScopeKey(seed byte) [32]byte {
	var key [32]byte
	copy(key[:], bytes.Repeat([]byte{seed}, 32))
	return key
}

func testRateLimitsSnapshot(
	accountID string,
	primaryUsed, secondaryUsed int32,
	credits *appserver.RateLimitResetCreditsSummary,
) appserver.AccountRateLimitsSnapshot {
	minsPrimary := int64(300)
	minsSecondary := int64(10_080)
	resetPrimary := int64(1_784_003_600)
	resetSecondary := int64(1_784_604_800)
	plan := "pro"
	limitID := "codex"
	return appserver.AccountRateLimitsSnapshot{
		AccountID: appserver.SensitiveAccountID(accountID),
		RateLimits: appserver.RateLimitSnapshot{
			LimitID: &limitID, PlanType: &plan,
			Primary:   &appserver.RateLimitWindow{UsedPercent: primaryUsed, WindowDurationMins: &minsPrimary, ResetsAtSeconds: &resetPrimary},
			Secondary: &appserver.RateLimitWindow{UsedPercent: secondaryUsed, WindowDurationMins: &minsSecondary, ResetsAtSeconds: &resetSecondary},
		},
		RateLimitResetCredits: credits,
	}
}

func cloneRateLimitsSnapshot(value appserver.AccountRateLimitsSnapshot) appserver.AccountRateLimitsSnapshot {
	cloned := value
	cloned.AccountID = append([]byte(nil), value.AccountID...)
	if value.RateLimitsByLimitID != nil {
		cloned.RateLimitsByLimitID = make(map[string]appserver.RateLimitSnapshot, len(value.RateLimitsByLimitID))
		for key, bucket := range value.RateLimitsByLimitID {
			cloned.RateLimitsByLimitID[key] = bucket
		}
	}
	if value.RateLimitResetCredits != nil {
		credits := *value.RateLimitResetCredits
		if value.RateLimitResetCredits.Credits != nil {
			credits.Credits = append([]appserver.RateLimitResetCredit(nil), value.RateLimitResetCredits.Credits...)
		}
		cloned.RateLimitResetCredits = &credits
	}
	return cloned
}

type fixedRetryPolicy struct{ delays []time.Duration }

func (policy fixedRetryPolicy) Delay(attempt int) (time.Duration, bool, error) {
	if attempt < 1 || attempt > len(policy.delays) {
		return 0, false, nil
	}
	return policy.delays[attempt-1], true, nil
}

func fixedClock(milliseconds int64) func() time.Time {
	return func() time.Time { return time.UnixMilli(milliseconds).UTC() }
}
