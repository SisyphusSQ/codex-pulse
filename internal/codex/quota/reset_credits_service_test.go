package quota

import (
	"context"
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
