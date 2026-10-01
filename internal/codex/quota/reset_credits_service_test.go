package quota

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/SisyphusSQ/codex-pulse/internal/codex/appserver"
	"github.com/SisyphusSQ/codex-pulse/internal/store"
)

func TestResetCreditsServicePersistsSuccessfulTypedSnapshot(t *testing.T) {
	t.Parallel()

	key := testScopeKey(0x31)
	request := testBoundRequest(t, key, "acct-test-a", "reset-service-success")
	granted := int64(1_783_000_000)
	expires := int64(1_784_691_200)
	repository := newQuotaServiceTestRepository(t, key)
	client := mustResetCreditsClient(t, key, &scriptedRateLimitsReader{snapshots: []appserver.AccountRateLimitsSnapshot{
		testRateLimitsSnapshot("acct-test-a", 1, 1, &appserver.RateLimitResetCreditsSummary{
			AvailableCount: 1,
			Credits: []appserver.RateLimitResetCredit{{
				ID: "credit-service-private", Status: "available", ResetType: "codexRateLimits",
				GrantedAtSeconds: granted, ExpiresAtSeconds: &expires,
			}},
		}),
	}})
	service, err := NewResetCreditsService(client, repository, time.Second)
	if err != nil {
		t.Fatalf("NewResetCreditsService() error = %v", err)
	}
	service.SetBinding(request.Binding)
	result, err := service.Fetch(context.Background(), request.RequestID)
	if err != nil || result.Snapshot == nil || result.Failure != nil {
		t.Fatalf("Fetch() = %#v, %v", result, err)
	}
	summary, err := repository.ResetCreditsSummary(
		context.Background(), request.Binding.AccountScope, result.FinishedAtMS,
	)
	if err != nil || summary.AvailableCount == nil || *summary.AvailableCount != 1 ||
		summary.NextExpiresAtMS == nil {
		t.Fatalf("summary = %#v, %v", summary, err)
	}
}

func TestResetCreditsServiceRecordsPreRequestCancellationDetached(t *testing.T) {
	t.Parallel()

	key := testScopeKey(0x32)
	request := testBoundRequest(t, key, "acct-test-a", "reset-service-cancel")
	repository := newQuotaServiceTestRepository(t, key)
	client := mustResetCreditsClient(t, key, &scriptedRateLimitsReader{
		snapshots: []appserver.AccountRateLimitsSnapshot{
			testRateLimitsSnapshot("acct-test-a", 1, 1, &appserver.RateLimitResetCreditsSummary{
				AvailableCount: 0, Credits: []appserver.RateLimitResetCredit{},
			}),
		},
	})
	service, err := NewResetCreditsService(client, repository, time.Second)
	if err != nil {
		t.Fatalf("NewResetCreditsService() error = %v", err)
	}
	service.SetBinding(request.Binding)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	result, err := service.Fetch(ctx, request.RequestID)
	if err != nil || result.Failure == nil || result.Failure.Code != store.SourceFailureCancelled {
		t.Fatalf("Fetch() = %#v, %v", result, err)
	}
	attempts, err := repository.ListSourceAttempts(
		context.Background(), store.ResetCreditsSourceInstanceAppServer(request.Binding.AccountScope), 10,
	)
	if err != nil || len(attempts) != 1 || attempts[0].AttemptCount != 0 ||
		attempts[0].FailureCode == nil || *attempts[0].FailureCode != store.SourceFailureCancelled {
		t.Fatalf("attempts = %#v, %v", attempts, err)
	}
}

func TestResetCreditsServicePreservesUnavailableDetailsWithoutStoppingRefresh(t *testing.T) {
	t.Parallel()
	for _, count := range []int64{0, 3} {
		t.Run(fmt.Sprint(count), func(t *testing.T) {
			key := testScopeKey(0x33)
			request := testBoundRequest(t, key, "acct-test-a", "reset-null-details")
			repository := newQuotaServiceTestRepository(t, key)
			client := mustResetCreditsClient(t, key, &scriptedRateLimitsReader{snapshots: []appserver.AccountRateLimitsSnapshot{
				testRateLimitsSnapshot("acct-test-a", 1, 1, &appserver.RateLimitResetCreditsSummary{AvailableCount: count}),
			}})
			service, err := NewResetCreditsService(client, repository, time.Second)
			if err != nil {
				t.Fatal(err)
			}
			service.SetBinding(request.Binding)
			result, err := service.FetchBound(t.Context(), request)
			if err != nil || result.Failure != nil {
				t.Fatalf("null details must be persistable: result=%#v error=%v", result, err)
			}
			summary, err := repository.ResetCreditsSummary(t.Context(), request.Binding.AccountScope, result.FinishedAtMS)
			if err != nil || summary.AvailableCount == nil || *summary.AvailableCount != count {
				t.Fatalf("summary=%#v error=%v", summary, err)
			}
		})
	}
}

func TestResetCreditsServiceRecordsExpiredAvailableItemAsSourceFailure(t *testing.T) {
	t.Parallel()
	key := testScopeKey(0x34)
	request := testBoundRequest(t, key, "acct-test-a", "reset-expired-item")
	repository := newQuotaServiceTestRepository(t, key)
	client := mustResetCreditsClient(t, key, &scriptedRateLimitsReader{snapshots: []appserver.AccountRateLimitsSnapshot{
		testRateLimitsSnapshot("acct-test-a", 1, 1, &appserver.RateLimitResetCreditsSummary{AvailableCount: 1, Credits: []appserver.RateLimitResetCredit{{ID: "synthetic-private-credit", Status: "available", ResetType: "codexRateLimits", GrantedAtSeconds: 1_783_000_000, ExpiresAtSeconds: new(int64(1_783_999_999))}}}),
	}})
	service, err := NewResetCreditsService(client, repository, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	service.SetBinding(request.Binding)
	result, err := service.FetchBound(t.Context(), request)
	if err != nil || result.Failure == nil || result.Failure.Code != store.SourceFailureSchemaIncompatible {
		t.Fatalf("external invalid item must be a recorded source failure: result=%#v error=%v", result, err)
	}
	attempt, err := repository.SourceAttempt(t.Context(), request.RequestID)
	if err != nil || attempt.Outcome != store.SourceAttemptFailed {
		t.Fatalf("attempt=%#v error=%v", attempt, err)
	}
}
