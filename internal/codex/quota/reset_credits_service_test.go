package quota

import (
	"context"
	"testing"
	"time"

	"github.com/SisyphusSQ/codex-pulse/internal/store"
)

func TestResetCreditsServicePersistsSuccessfulTypedSnapshot(t *testing.T) {
	t.Parallel()

	repository := newQuotaServiceTestRepository(t)
	client := resetCreditsClientForBody(t, `{
		"available_count":1,
		"credits":[{
			"id":"RateLimitResetCredit_service-private",
			"status":"available",
			"reset_type":"codex_rate_limits",
			"granted_at":"2026-07-15T10:00:00Z",
			"expires_at":"2026-07-16T10:00:00Z"
		}]
	}`)
	service, err := NewResetCreditsService(client, repository, time.Second)
	if err != nil {
		t.Fatalf("NewResetCreditsService() error = %v", err)
	}
	result, err := service.Fetch(context.Background(), "reset-service-success")
	if err != nil || result.Snapshot == nil || result.Failure != nil {
		t.Fatalf("Fetch() = %#v, %v", result, err)
	}
	summary, err := repository.ResetCreditsSummary(
		context.Background(), store.QuotaAccountScopeDefault, result.FinishedAtMS,
	)
	if err != nil || summary.AvailableCount == nil || *summary.AvailableCount != 1 ||
		summary.TotalCount == nil || *summary.TotalCount != 1 || summary.NextExpiresAtMS == nil {
		t.Fatalf("summary = %#v, %v", summary, err)
	}
}

func TestResetCreditsServiceRecordsPreRequestCancellationDetached(t *testing.T) {
	t.Parallel()

	repository := newQuotaServiceTestRepository(t)
	client := resetCreditsClientForBody(t, `{"available_count":0,"credits":[]}`)
	service, err := NewResetCreditsService(client, repository, time.Second)
	if err != nil {
		t.Fatalf("NewResetCreditsService() error = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	result, err := service.Fetch(ctx, "reset-service-cancel")
	if err != nil || result.Failure == nil || result.Failure.Code != store.SourceFailureCancelled {
		t.Fatalf("Fetch() = %#v, %v", result, err)
	}
	attempts, err := repository.ListSourceAttempts(
		context.Background(), store.ResetCreditsSourceInstanceWhamDefault, 10,
	)
	if err != nil || len(attempts) != 1 || attempts[0].AttemptCount != 0 ||
		attempts[0].FailureCode == nil || *attempts[0].FailureCode != store.SourceFailureCancelled {
		t.Fatalf("attempts = %#v, %v", attempts, err)
	}
}
