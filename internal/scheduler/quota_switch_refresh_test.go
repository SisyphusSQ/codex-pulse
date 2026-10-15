package scheduler

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	quotaonline "github.com/SisyphusSQ/codex-pulse/internal/codex/quota"
	"github.com/SisyphusSQ/codex-pulse/internal/preferences"
	"github.com/SisyphusSQ/codex-pulse/internal/store"
)

func TestQuotaRefreshSwitchAndManualRefreshRespectOnlyServerFence(t *testing.T) {
	const nowMS = int64(1_784_001_000_000)
	for _, scenario := range []struct {
		name          string
		manual        bool
		serverLimited bool
	}{
		{name: "switch ignores historical server error"},
		{name: "manual bypasses ordinary global backoff", manual: true},
		{name: "switch preserves server limit", serverLimited: true},
		{name: "manual preserves server limit", manual: true, serverLimited: true},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			repository := newQuotaRefreshTestRepository(t)
			scope := quotaRefreshScope(repository)
			sourceID := store.ResetCreditsSourceInstanceAppServer(scope)
			failureAt := nowMS - 3*24*60*60*1000
			failure := store.SourceFailureServerError
			class := store.RuntimeErrorUnavailable
			if err := repository.RecordResetCreditsFetch(t.Context(), store.ResetCreditsFetchRecord{
				SourceInstanceID: sourceID, SourceType: store.ResetCreditsSourceTypeAppServer,
				ScopeKey: scope, BindingGeneration: 1,
				Attempt: store.SourceAttempt{RequestID: "old-failure", SourceInstanceID: sourceID,
					StartedAtMS: failureAt, FinishedAtMS: failureAt, Outcome: store.SourceAttemptFailed,
					ErrorClass: &class, FailureCode: &failure, AttemptCount: 1},
			}); err != nil {
				t.Fatal(err)
			}
			if _, err := repository.UpsertSourceRefreshSchedule(t.Context(), store.SourceRefreshScheduleUpdate{
				SourceInstanceID: sourceID, SourceType: store.ResetCreditsSourceTypeAppServer,
				ScopeKey: scope, BindingGeneration: 1, Reason: store.RefreshReasonInactiveAccount, AtMS: failureAt,
			}); err != nil {
				t.Fatal(err)
			}
			if !scenario.manual {
				if _, err := repository.MarkCodexAccountBindingPending(t.Context(), nowMS-1, store.CodexAccountBindingReasonAccountChanged); err != nil {
					t.Fatal(err)
				}
				if _, _, err := repository.ConfirmCodexAccountBinding(t.Context(), scope, nowMS, store.CodexAccountBindingReasonAccountChanged); err != nil {
					t.Fatal(err)
				}
			}
			fenceReason := "network_backoff"
			if scenario.serverLimited {
				fenceReason = "retry_after"
			}
			if err := repository.RaiseSourceRefreshGlobalFence(t.Context(), nowMS+60_000, fenceReason, nowMS-1); err != nil {
				t.Fatal(err)
			}
			policy, err := quotaonline.NewRefreshPolicy(func() float64 { return 0 })
			if err != nil {
				t.Fatal(err)
			}
			coordinator, err := NewQuotaRefreshCoordinator(QuotaRefreshCoordinatorConfig{
				Repository: repository,
				Preferences: staticRefreshPreferences{snapshot: preferences.Snapshot{
					Online:  preferences.OnlinePreferences{ResetCreditsEnabled: true},
					Refresh: preferences.RefreshPreferences{QuotaIntervalSeconds: 300, ResetCreditsIntervalSeconds: 1800},
				}},
				Policy: &policy, Clock: func() time.Time { return time.UnixMilli(nowMS) },
				QuotaFetcher: SourceRefreshFunc(func(ctx context.Context, request quotaonline.BoundRefreshRequest) error { return nil }),
				ResetCreditsFetcher: SourceRefreshFunc(func(ctx context.Context, request quotaonline.BoundRefreshRequest) error {
					return repository.RecordResetCreditsFetch(ctx, store.ResetCreditsFetchRecord{
						SourceInstanceID: sourceID, SourceType: store.ResetCreditsSourceTypeAppServer,
						ScopeKey: scope, BindingGeneration: request.Binding.BindingGeneration,
						Attempt: store.SourceAttempt{RequestID: request.RequestID, SourceInstanceID: sourceID,
							StartedAtMS: nowMS, FinishedAtMS: nowMS, Outcome: store.SourceAttemptSucceeded, AttemptCount: 1},
						Snapshot: &store.ResetCreditsSnapshot{SnapshotID: request.RequestID, RequestID: request.RequestID, AccountScope: scope, ObservedAtMS: nowMS},
					})
				}),
			})
			if err != nil {
				t.Fatal(err)
			}
			if scenario.manual {
				schedule, fetched, err := coordinator.RequestRefreshResult(t.Context(), quotaonline.RefreshSourceResetCredits, store.RefreshTriggerManual)
				if err != nil || fetched == scenario.serverLimited {
					t.Fatalf("manual fetched=%v, error=%v", fetched, err)
				}
				if scenario.serverLimited && (schedule.Reason != store.RefreshReasonRetryAfter || schedule.NextDueAtMS == nil || *schedule.NextDueAtMS != nowMS+60_000) {
					t.Fatalf("server fence receipt = %#v", schedule)
				}
			} else if err := coordinator.Initialize(t.Context()); err != nil {
				t.Fatal(err)
			}
			state, err := repository.SourceState(t.Context(), sourceID)
			if err != nil {
				t.Fatal(err)
			}
			if scenario.serverLimited {
				if state.LastSuccessAtMS != nil {
					t.Fatal("fetched before server limit elapsed")
				}
			} else if state.LastSuccessAtMS == nil || *state.LastSuccessAtMS != nowMS {
				t.Fatalf("no immediate success: %#v", state)
			}
		})
	}
}

func TestQuotaRefreshServerLimitBlocksCompanionAndExpires(t *testing.T) {
	for _, scenario := range []struct {
		name       string
		delayMS    int64
		retryAfter bool
	}{
		{name: "with Retry-After", delayMS: 900_000, retryAfter: true},
		{name: "without Retry-After", delayMS: 300_000},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			testQuotaRefreshServerLimit(t, scenario.delayMS, scenario.retryAfter)
		})
	}
}

func testQuotaRefreshServerLimit(t *testing.T, delayMS int64, retryAfter bool) {
	t.Helper()
	const startedAt = int64(1_784_002_000_000)
	nowMS := startedAt
	var retryAtMS *int64
	if retryAfter {
		retryAtMS = new(startedAt + delayMS)
	}
	repository := newQuotaRefreshTestRepository(t)
	scope := quotaRefreshScope(repository)
	if err := repository.RaiseSourceRefreshGlobalFence(t.Context(), startedAt+1_800_000, "network_backoff", startedAt-1); err != nil {
		t.Fatal(err)
	}
	policy, err := quotaonline.NewRefreshPolicy(func() float64 { return 0 })
	if err != nil {
		t.Fatal(err)
	}
	coordinator, err := NewQuotaRefreshCoordinator(QuotaRefreshCoordinatorConfig{
		Repository: repository,
		Preferences: staticRefreshPreferences{snapshot: preferences.Snapshot{
			Online:  preferences.OnlinePreferences{QuotaEnabled: true, ResetCreditsEnabled: true},
			Refresh: preferences.RefreshPreferences{QuotaIntervalSeconds: 300, ResetCreditsIntervalSeconds: 1800},
		}},
		Policy: &policy, Clock: func() time.Time { return time.UnixMilli(nowMS) },
		QuotaFetcher: SourceRefreshFunc(func(ctx context.Context, request quotaonline.BoundRefreshRequest) error {
			return repository.RecordQuotaFetch(ctx, store.QuotaFetchRecord{
				AccountScope: scope, ScopeKey: scope, BindingGeneration: request.Binding.BindingGeneration,
				SourceInstanceID: store.QuotaSourceInstanceAppServer(scope), SourceType: store.QuotaSourceTypeAppServerRateLimits,
				Attempt: store.SourceAttempt{RequestID: request.RequestID, SourceInstanceID: store.QuotaSourceInstanceAppServer(scope),
					StartedAtMS: nowMS, FinishedAtMS: nowMS, Outcome: store.SourceAttemptFailed, AttemptCount: 1,
					HTTPStatus: new(int64(429)), FailureCode: new(store.SourceFailureHTTP429), ErrorClass: new(store.RuntimeErrorUnavailable), RetryAtMS: retryAtMS},
			})
		}),
		ResetCreditsFetcher: SourceRefreshFunc(func(ctx context.Context, request quotaonline.BoundRefreshRequest) error {
			sourceID := store.ResetCreditsSourceInstanceAppServer(scope)
			return repository.RecordResetCreditsFetch(ctx, store.ResetCreditsFetchRecord{
				SourceInstanceID: sourceID, SourceType: store.ResetCreditsSourceTypeAppServer, ScopeKey: scope, BindingGeneration: request.Binding.BindingGeneration,
				Attempt:  store.SourceAttempt{RequestID: request.RequestID, SourceInstanceID: sourceID, StartedAtMS: nowMS, FinishedAtMS: nowMS, Outcome: store.SourceAttemptSucceeded, AttemptCount: 1},
				Snapshot: &store.ResetCreditsSnapshot{SnapshotID: request.RequestID, RequestID: request.RequestID, AccountScope: scope, ObservedAtMS: nowMS},
			})
		}),
	})
	if err != nil {
		t.Fatal(err)
	}
	_, fetched, err := coordinator.RequestRefreshResult(t.Context(), quotaonline.RefreshSourceQuota, store.RefreshTriggerManual)
	if !fetched || !errors.Is(err, ErrQuotaRefreshSourceFailed) {
		t.Fatalf("rate limited fetch=%v, error=%v", fetched, err)
	}
	for _, trigger := range []store.SourceRefreshTrigger{store.RefreshTriggerManual, store.RefreshTriggerForeground} {
		schedule, fetched, err := coordinator.RequestRefreshResult(t.Context(), quotaonline.RefreshSourceResetCredits, trigger)
		if err != nil || fetched || schedule.Reason != store.RefreshReasonRetryAfter || schedule.NextDueAtMS == nil || *schedule.NextDueAtMS != startedAt+delayMS {
			t.Fatalf("%s limited receipt=%#v fetched=%v error=%v", trigger, schedule, fetched, err)
		}
	}
	if _, err := repository.MarkCodexAccountBindingPending(t.Context(), nowMS, store.CodexAccountBindingReasonAccountChanged); err != nil {
		t.Fatal(err)
	}
	scope = strings.Repeat("b", 64)
	if _, _, err := repository.ConfirmCodexAccountBinding(t.Context(), scope, nowMS, store.CodexAccountBindingReasonAccountChanged); err != nil {
		t.Fatal(err)
	}
	if err := coordinator.Initialize(t.Context()); err != nil {
		t.Fatal(err)
	}
	limited, fetched, err := coordinator.RequestRefreshResult(t.Context(), quotaonline.RefreshSourceResetCredits, store.RefreshTriggerManual)
	if err != nil || fetched || limited.Reason != store.RefreshReasonRetryAfter || limited.NextDueAtMS == nil || *limited.NextDueAtMS != startedAt+delayMS {
		t.Fatalf("switched/reinitialized limited receipt=%#v fetched=%v error=%v", limited, fetched, err)
	}
	nowMS += delayMS
	_, fetched, err = coordinator.RequestRefreshResult(t.Context(), quotaonline.RefreshSourceResetCredits, store.RefreshTriggerManual)
	if err != nil || !fetched {
		t.Fatalf("after expiry fetched=%v error=%v", fetched, err)
	}
}
