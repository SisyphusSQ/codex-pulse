package scheduler

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sync/atomic"
	"testing"
	"time"

	quotaonline "github.com/SisyphusSQ/codex-pulse/internal/codex/quota"
	"github.com/SisyphusSQ/codex-pulse/internal/preferences"
	"github.com/SisyphusSQ/codex-pulse/internal/store"
	storesqlite "github.com/SisyphusSQ/codex-pulse/internal/store/sqlite"
)

func TestQuotaRefreshCoordinatorPersistsStartupPlanAndManualThrottle(t *testing.T) {
	t.Parallel()

	const nowMS = int64(1_784_000_000_000)
	repository := newQuotaRefreshTestRepository(t)
	policy, err := quotaonline.NewRefreshPolicy(func() float64 { return 0 })
	if err != nil {
		t.Fatalf("NewRefreshPolicy() error = %v", err)
	}
	requestSequence := 0
	fetchCalls := 0
	coordinator, err := NewQuotaRefreshCoordinator(QuotaRefreshCoordinatorConfig{
		Repository: repository,
		Preferences: staticRefreshPreferences{snapshot: preferences.Snapshot{
			Online: preferences.OnlinePreferences{QuotaEnabled: false, ResetCreditsEnabled: true},
			Refresh: preferences.RefreshPreferences{
				QuotaIntervalSeconds: 300, ResetCreditsIntervalSeconds: 1_800,
			},
		}},
		Policy: &policy,
		ResetCreditsFetcher: SourceRefreshFunc(func(ctx context.Context, requestID string) error {
			fetchCalls++
			status := int64(200)
			return repository.RecordResetCreditsFetch(ctx, store.ResetCreditsFetchRecord{
				SourceInstanceID: store.ResetCreditsSourceInstanceWhamDefault,
				SourceType:       store.ResetCreditsSourceTypeWham, ScopeKey: store.QuotaAccountScopeDefault,
				Attempt: store.SourceAttempt{
					RequestID: requestID, SourceInstanceID: store.ResetCreditsSourceInstanceWhamDefault,
					StartedAtMS: nowMS, FinishedAtMS: nowMS, Outcome: store.SourceAttemptSucceeded,
					HTTPStatus: &status, AttemptCount: 1,
				},
				Snapshot: &store.ResetCreditsSnapshot{
					SnapshotID: "scheduler-snapshot-" + requestID, RequestID: requestID,
					AccountScope: store.QuotaAccountScopeDefault, ObservedAtMS: nowMS,
				},
			})
		}),
		QuotaFetcher: SourceRefreshFunc(func(context.Context, string) error {
			return fmt.Errorf("disabled quota fetcher must not run")
		}),
		Clock: func() time.Time { return time.UnixMilli(nowMS) },
		NewRequestID: func(source quotaonline.RefreshSource) (string, error) {
			requestSequence++
			return fmt.Sprintf("%s-request-%d", source, requestSequence), nil
		},
	})
	if err != nil {
		t.Fatalf("NewQuotaRefreshCoordinator() error = %v", err)
	}
	if err := coordinator.Initialize(context.Background()); err != nil {
		t.Fatalf("Initialize() error = %v", err)
	}
	if fetchCalls != 1 {
		t.Fatalf("startup fetch calls = %d, want 1", fetchCalls)
	}
	schedule, err := repository.SourceRefreshSchedule(
		context.Background(), store.ResetCreditsSourceInstanceWhamDefault,
	)
	if err != nil || schedule.ActiveClaimID != nil || schedule.NextDueAtMS == nil ||
		*schedule.NextDueAtMS != nowMS+1_800_000 || schedule.Reason != store.RefreshReasonNormalInterval {
		t.Fatalf("startup schedule = %#v, %v", schedule, err)
	}
	if err := coordinator.RunDueCycle(context.Background()); err != nil {
		t.Fatalf("RunDueCycle(not due) error = %v", err)
	}
	if fetchCalls != 1 {
		t.Fatalf("not-due fetch calls = %d, want 1", fetchCalls)
	}

	manual, err := coordinator.RequestRefresh(
		context.Background(), quotaonline.RefreshSourceResetCredits, store.RefreshTriggerManual,
	)
	if err != nil || fetchCalls != 2 || manual.LastManualAtMS == nil || *manual.LastManualAtMS != nowMS ||
		manual.Reason != store.RefreshReasonNormalInterval {
		t.Fatalf("manual refresh = %#v, calls=%d, error=%v", manual, fetchCalls, err)
	}
	throttled, err := coordinator.RequestRefresh(
		context.Background(), quotaonline.RefreshSourceResetCredits, store.RefreshTriggerManual,
	)
	if err != nil || fetchCalls != 2 || throttled.Revision != manual.Revision {
		t.Fatalf("throttled refresh = %#v, calls=%d, error=%v", throttled, fetchCalls, err)
	}
}

func TestQuotaRefreshCommittedNotifiesOnlyAfterRefreshCommit(t *testing.T) {
	t.Parallel()

	const nowMS = int64(1_784_000_100_000)
	repository := newQuotaRefreshTestRepository(t)
	policy, err := quotaonline.NewRefreshPolicy(func() float64 { return 0 })
	if err != nil {
		t.Fatalf("NewRefreshPolicy() error = %v", err)
	}
	coordinator, err := NewQuotaRefreshCoordinator(QuotaRefreshCoordinatorConfig{
		Repository: repository,
		Preferences: staticRefreshPreferences{snapshot: preferences.Snapshot{
			Online: preferences.OnlinePreferences{ResetCreditsEnabled: true},
			Refresh: preferences.RefreshPreferences{
				QuotaIntervalSeconds: 300, ResetCreditsIntervalSeconds: 1_800,
			},
		}},
		Policy:       &policy,
		QuotaFetcher: SourceRefreshFunc(func(context.Context, string) error { return nil }),
		ResetCreditsFetcher: SourceRefreshFunc(func(ctx context.Context, requestID string) error {
			status := int64(200)
			return repository.RecordResetCreditsFetch(ctx, store.ResetCreditsFetchRecord{
				SourceInstanceID: store.ResetCreditsSourceInstanceWhamDefault,
				SourceType:       store.ResetCreditsSourceTypeWham, ScopeKey: store.QuotaAccountScopeDefault,
				Attempt: store.SourceAttempt{
					RequestID: requestID, SourceInstanceID: store.ResetCreditsSourceInstanceWhamDefault,
					StartedAtMS: nowMS, FinishedAtMS: nowMS, Outcome: store.SourceAttemptSucceeded,
					HTTPStatus: &status, AttemptCount: 1,
				},
				Snapshot: &store.ResetCreditsSnapshot{
					SnapshotID: "notify-" + requestID, RequestID: requestID,
					AccountScope: store.QuotaAccountScopeDefault, ObservedAtMS: nowMS,
				},
			})
		}),
		Clock:        func() time.Time { return time.UnixMilli(nowMS) },
		NewRequestID: func(quotaonline.RefreshSource) (string, error) { return "notify-request", nil },
	})
	if err != nil {
		t.Fatalf("NewQuotaRefreshCoordinator() error = %v", err)
	}
	var notifications []quotaonline.RefreshSource
	coordinator.refreshCommitted = func(_ context.Context, source quotaonline.RefreshSource) {
		notifications = append(notifications, source)
	}
	if err := coordinator.Initialize(context.Background()); err != nil {
		t.Fatalf("Initialize() error = %v", err)
	}
	if !reflect.DeepEqual(notifications, []quotaonline.RefreshSource{quotaonline.RefreshSourceResetCredits}) {
		t.Fatalf("notifications = %#v", notifications)
	}

	failingRepository := newQuotaRefreshTestRepository(t)
	failing, err := NewQuotaRefreshCoordinator(QuotaRefreshCoordinatorConfig{
		Repository: failingRepository,
		Preferences: staticRefreshPreferences{snapshot: preferences.Snapshot{
			Online: preferences.OnlinePreferences{ResetCreditsEnabled: true},
			Refresh: preferences.RefreshPreferences{
				QuotaIntervalSeconds: 300, ResetCreditsIntervalSeconds: 1_800,
			},
		}},
		Policy:       &policy,
		QuotaFetcher: SourceRefreshFunc(func(context.Context, string) error { return nil }),
		ResetCreditsFetcher: SourceRefreshFunc(func(context.Context, string) error {
			return errors.New("synthetic fetch failure")
		}),
		Clock:        func() time.Time { return time.UnixMilli(nowMS) },
		NewRequestID: func(quotaonline.RefreshSource) (string, error) { return "failed-request", nil },
	})
	if err != nil {
		t.Fatalf("NewQuotaRefreshCoordinator(failing) error = %v", err)
	}
	failedNotifications := 0
	failing.refreshCommitted = func(context.Context, quotaonline.RefreshSource) { failedNotifications++ }
	if err := failing.Initialize(context.Background()); err == nil {
		t.Fatal("Initialize(failing) error = nil")
	}
	if failedNotifications != 0 {
		t.Fatalf("failed notifications = %d, want 0", failedNotifications)
	}
}

func TestQuotaRefreshCoordinatorManualDoesNotBypassRetryAfterHiddenByLongerBackoff(t *testing.T) {
	t.Parallel()

	const nowMS = int64(1_784_000_500_000)
	repository := newQuotaRefreshTestRepository(t)
	retryAtMS := nowMS + 60_000
	status := int64(429)
	failure := store.SourceFailureHTTP429
	class := store.RuntimeErrorUnavailable
	if err := repository.RecordResetCreditsFetch(context.Background(), store.ResetCreditsFetchRecord{
		SourceInstanceID: store.ResetCreditsSourceInstanceWhamDefault,
		SourceType:       store.ResetCreditsSourceTypeWham, ScopeKey: store.QuotaAccountScopeDefault,
		Attempt: store.SourceAttempt{
			RequestID: "manual-retry-after-fence", SourceInstanceID: store.ResetCreditsSourceInstanceWhamDefault,
			StartedAtMS: nowMS - 1_000, FinishedAtMS: nowMS - 1_000,
			Outcome: store.SourceAttemptFailed, HTTPStatus: &status, ErrorClass: &class,
			FailureCode: &failure, RetryAtMS: &retryAtMS, AttemptCount: 1,
		},
	}); err != nil {
		t.Fatalf("RecordResetCreditsFetch() error = %v", err)
	}
	backoffAtMS := nowMS + 300_000
	seeded, err := repository.UpsertSourceRefreshSchedule(context.Background(), store.SourceRefreshScheduleUpdate{
		SourceInstanceID: store.ResetCreditsSourceInstanceWhamDefault,
		SourceType:       store.ResetCreditsSourceTypeWham, ScopeKey: store.QuotaAccountScopeDefault,
		NextDueAtMS: &backoffAtMS, Reason: store.RefreshReasonNetworkBackoff, AtMS: nowMS - 1_000,
	})
	if err != nil {
		t.Fatalf("UpsertSourceRefreshSchedule() error = %v", err)
	}
	policy, err := quotaonline.NewRefreshPolicy(func() float64 { return 0 })
	if err != nil {
		t.Fatalf("NewRefreshPolicy() error = %v", err)
	}
	fetchCalls := 0
	coordinator, err := NewQuotaRefreshCoordinator(QuotaRefreshCoordinatorConfig{
		Repository: repository,
		Preferences: staticRefreshPreferences{snapshot: preferences.Snapshot{
			Online: preferences.OnlinePreferences{ResetCreditsEnabled: true},
			Refresh: preferences.RefreshPreferences{
				QuotaIntervalSeconds: 300, ResetCreditsIntervalSeconds: 1_800,
			},
		}},
		Policy:       &policy,
		QuotaFetcher: SourceRefreshFunc(func(context.Context, string) error { return nil }),
		ResetCreditsFetcher: SourceRefreshFunc(func(context.Context, string) error {
			fetchCalls++
			return errors.New("fetch must remain fenced by Retry-After")
		}),
		Clock: func() time.Time { return time.UnixMilli(nowMS) },
	})
	if err != nil {
		t.Fatalf("NewQuotaRefreshCoordinator() error = %v", err)
	}
	readback, err := coordinator.RequestRefresh(
		context.Background(), quotaonline.RefreshSourceResetCredits, store.RefreshTriggerManual,
	)
	if err != nil || fetchCalls != 0 || readback.Revision != seeded.Revision ||
		readback.Reason != store.RefreshReasonNetworkBackoff || readback.NextDueAtMS == nil ||
		*readback.NextDueAtMS != backoffAtMS {
		t.Fatalf("manual fenced schedule = %#v, calls=%d, error=%v", readback, fetchCalls, err)
	}
}

func TestQuotaRefreshCoordinatorReconcilePreservesDurableNetworkBackoff(t *testing.T) {
	t.Parallel()

	const failureAtMS = int64(1_784_000_800_000)
	repository := newQuotaRefreshTestRepository(t)
	failure := store.SourceFailureNetworkUnavailable
	class := store.RuntimeErrorUnavailable
	if err := repository.RecordResetCreditsFetch(context.Background(), store.ResetCreditsFetchRecord{
		SourceInstanceID: store.ResetCreditsSourceInstanceWhamDefault,
		SourceType:       store.ResetCreditsSourceTypeWham, ScopeKey: store.QuotaAccountScopeDefault,
		Attempt: store.SourceAttempt{
			RequestID: "reconcile-durable-backoff", SourceInstanceID: store.ResetCreditsSourceInstanceWhamDefault,
			StartedAtMS: failureAtMS, FinishedAtMS: failureAtMS,
			Outcome: store.SourceAttemptFailed, ErrorClass: &class, FailureCode: &failure, AttemptCount: 1,
		},
	}); err != nil {
		t.Fatalf("RecordResetCreditsFetch() error = %v", err)
	}
	backoffDueAtMS := failureAtMS + 300_000
	seeded, err := repository.UpsertSourceRefreshSchedule(context.Background(), store.SourceRefreshScheduleUpdate{
		SourceInstanceID: store.ResetCreditsSourceInstanceWhamDefault,
		SourceType:       store.ResetCreditsSourceTypeWham, ScopeKey: store.QuotaAccountScopeDefault,
		NextDueAtMS: &backoffDueAtMS, Reason: store.RefreshReasonNetworkBackoff, AtMS: failureAtMS,
	})
	if err != nil {
		t.Fatalf("UpsertSourceRefreshSchedule() error = %v", err)
	}
	policy, err := quotaonline.NewRefreshPolicy(func() float64 { return 0 })
	if err != nil {
		t.Fatalf("NewRefreshPolicy() error = %v", err)
	}
	fetchCalls := 0
	coordinator, err := NewQuotaRefreshCoordinator(QuotaRefreshCoordinatorConfig{
		Repository: repository,
		Preferences: staticRefreshPreferences{snapshot: preferences.Snapshot{
			Online: preferences.OnlinePreferences{ResetCreditsEnabled: true},
			Refresh: preferences.RefreshPreferences{
				QuotaIntervalSeconds: 300, ResetCreditsIntervalSeconds: 1_800,
			},
		}},
		Policy:       &policy,
		QuotaFetcher: SourceRefreshFunc(func(context.Context, string) error { return nil }),
		ResetCreditsFetcher: SourceRefreshFunc(func(context.Context, string) error {
			fetchCalls++
			return errors.New("durable backoff must not fetch before due")
		}),
		Clock: func() time.Time { return time.UnixMilli(failureAtMS + 60_000) },
	})
	if err != nil {
		t.Fatalf("NewQuotaRefreshCoordinator() error = %v", err)
	}
	if err := coordinator.ReconcilePreferences(context.Background()); err != nil {
		t.Fatalf("ReconcilePreferences() error = %v", err)
	}
	readback, err := repository.SourceRefreshSchedule(
		context.Background(), store.ResetCreditsSourceInstanceWhamDefault,
	)
	if err != nil || fetchCalls != 0 || readback.Revision != seeded.Revision ||
		readback.Reason != store.RefreshReasonNetworkBackoff || readback.NextDueAtMS == nil ||
		*readback.NextDueAtMS != backoffDueAtMS {
		t.Fatalf("reconciled schedule = %#v, calls=%d, error=%v", readback, fetchCalls, err)
	}
}

func TestQuotaRefreshRunnerUsesInjectedRobfigCronLifecycle(t *testing.T) {
	t.Parallel()

	repository := newQuotaRefreshTestRepository(t)
	policy, err := quotaonline.NewRefreshPolicy(func() float64 { return 0 })
	if err != nil {
		t.Fatalf("NewRefreshPolicy() error = %v", err)
	}
	coordinator, err := NewQuotaRefreshCoordinator(QuotaRefreshCoordinatorConfig{
		Repository: repository,
		Preferences: staticRefreshPreferences{snapshot: preferences.Snapshot{
			Online: preferences.OnlinePreferences{},
			Refresh: preferences.RefreshPreferences{
				QuotaIntervalSeconds: 300, ResetCreditsIntervalSeconds: 1_800,
			},
		}},
		Policy:              &policy,
		QuotaFetcher:        SourceRefreshFunc(func(context.Context, string) error { return nil }),
		ResetCreditsFetcher: SourceRefreshFunc(func(context.Context, string) error { return nil }),
	})
	if err != nil {
		t.Fatalf("NewQuotaRefreshCoordinator() error = %v", err)
	}
	runner, err := NewQuotaRefreshRunner(coordinator)
	if err != nil {
		t.Fatalf("NewQuotaRefreshRunner() error = %v", err)
	}
	fake := newFakeCronRunner()
	runner.newCronRunner = fake.factory
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- runner.Run(ctx) }()
	fake.waitStarted(t, done)
	cancel()
	select {
	case err := <-done:
		if err != context.Canceled {
			t.Fatalf("Run() error = %v, want context.Canceled", err)
		}
	case <-time.After(time.Second):
		t.Fatal("quota refresh runner did not stop")
	}
	if fake.stopCalls.Load() != 1 {
		t.Fatalf("cron Stop calls = %d, want 1", fake.stopCalls.Load())
	}
}

func TestQuotaRefreshCoordinatorRecoversClaimThatExpiresAfterStartup(t *testing.T) {
	t.Parallel()

	repository := newQuotaRefreshTestRepository(t)
	dueAt := int64(100)
	schedule, err := repository.UpsertSourceRefreshSchedule(context.Background(), store.SourceRefreshScheduleUpdate{
		SourceInstanceID: store.ResetCreditsSourceInstanceWhamDefault,
		SourceType:       store.ResetCreditsSourceTypeWham, ScopeKey: store.QuotaAccountScopeDefault,
		NextDueAtMS: &dueAt, Reason: store.RefreshReasonStartup, AtMS: 90,
	})
	if err != nil {
		t.Fatalf("create schedule: %v", err)
	}
	if _, ok, err := repository.ClaimSourceRefresh(
		context.Background(), store.ResetCreditsSourceInstanceWhamDefault, schedule.Revision,
		"crashed-claim", store.RefreshTriggerStartup, 100, 50,
	); err != nil || !ok {
		t.Fatalf("seed crashed claim = %v, %v", ok, err)
	}
	policy, err := quotaonline.NewRefreshPolicy(func() float64 { return 0 })
	if err != nil {
		t.Fatalf("NewRefreshPolicy() error = %v", err)
	}
	nowMS := int64(149)
	fetchCalls := 0
	coordinator, err := NewQuotaRefreshCoordinator(QuotaRefreshCoordinatorConfig{
		Repository: repository,
		Preferences: staticRefreshPreferences{snapshot: preferences.Snapshot{
			Online: preferences.OnlinePreferences{ResetCreditsEnabled: true},
			Refresh: preferences.RefreshPreferences{
				QuotaIntervalSeconds: 300, ResetCreditsIntervalSeconds: 1_800,
			},
		}},
		Policy:       &policy,
		QuotaFetcher: SourceRefreshFunc(func(context.Context, string) error { return nil }),
		ResetCreditsFetcher: SourceRefreshFunc(func(ctx context.Context, requestID string) error {
			fetchCalls++
			status := int64(200)
			return repository.RecordResetCreditsFetch(ctx, store.ResetCreditsFetchRecord{
				SourceInstanceID: store.ResetCreditsSourceInstanceWhamDefault,
				SourceType:       store.ResetCreditsSourceTypeWham, ScopeKey: store.QuotaAccountScopeDefault,
				Attempt: store.SourceAttempt{
					RequestID: requestID, SourceInstanceID: store.ResetCreditsSourceInstanceWhamDefault,
					StartedAtMS: nowMS, FinishedAtMS: nowMS, Outcome: store.SourceAttemptSucceeded,
					HTTPStatus: &status, AttemptCount: 1,
				},
				Snapshot: &store.ResetCreditsSnapshot{
					SnapshotID: "recovered-" + requestID, RequestID: requestID,
					AccountScope: store.QuotaAccountScopeDefault, ObservedAtMS: nowMS,
				},
			})
		}),
		Clock: func() time.Time { return time.UnixMilli(nowMS) },
		NewRequestID: func(quotaonline.RefreshSource) (string, error) {
			return "recovered-request", nil
		},
	})
	if err != nil {
		t.Fatalf("NewQuotaRefreshCoordinator() error = %v", err)
	}
	if err := coordinator.RunDueCycle(context.Background()); err != nil || fetchCalls != 0 {
		t.Fatalf("pre-expiry cycle calls=%d error=%v", fetchCalls, err)
	}
	nowMS = 150
	if err := coordinator.RunDueCycle(context.Background()); err != nil || fetchCalls != 1 {
		t.Fatalf("expiry recovery cycle calls=%d error=%v", fetchCalls, err)
	}
	readback, err := repository.SourceRefreshSchedule(
		context.Background(), store.ResetCreditsSourceInstanceWhamDefault,
	)
	if err != nil || readback.ActiveClaimID != nil || readback.Reason != store.RefreshReasonNormalInterval ||
		readback.NextDueAtMS == nil || *readback.NextDueAtMS != nowMS+1_800_000 {
		t.Fatalf("recovered schedule = %#v, %v", readback, err)
	}
}

func TestQuotaRefreshCoordinatorRecoversRecordedSuccessfulClaimWithoutRefetch(t *testing.T) {
	t.Parallel()

	const (
		claimedAtMS   = int64(1_784_050_000_000)
		recoveryAtMS  = claimedAtMS + 1_000
		refreshDelay  = int64(1_800_000)
		recordedClaim = "recorded-success-claim"
	)
	repository := newQuotaRefreshTestRepository(t)
	seedRecordedResetCreditsClaim(t, repository, recordedClaim, claimedAtMS, 1_000, nil)
	policy, err := quotaonline.NewRefreshPolicy(func() float64 { return 0 })
	if err != nil {
		t.Fatalf("NewRefreshPolicy() error = %v", err)
	}
	var fetchCalls atomic.Int32
	coordinator, err := NewQuotaRefreshCoordinator(QuotaRefreshCoordinatorConfig{
		Repository: repository,
		Preferences: staticRefreshPreferences{snapshot: preferences.Snapshot{
			Online: preferences.OnlinePreferences{ResetCreditsEnabled: true},
			Refresh: preferences.RefreshPreferences{
				QuotaIntervalSeconds: 300, ResetCreditsIntervalSeconds: 1_800,
			},
		}},
		Policy:       &policy,
		QuotaFetcher: SourceRefreshFunc(func(context.Context, string) error { return nil }),
		ResetCreditsFetcher: SourceRefreshFunc(func(context.Context, string) error {
			fetchCalls.Add(1)
			return nil
		}),
		Clock: func() time.Time { return time.UnixMilli(recoveryAtMS) },
	})
	if err != nil {
		t.Fatalf("NewQuotaRefreshCoordinator() error = %v", err)
	}
	if err := coordinator.RunDueCycle(context.Background()); err != nil {
		t.Fatalf("RunDueCycle() error = %v", err)
	}
	if fetchCalls.Load() != 0 {
		t.Fatalf("recovery fetch calls = %d, want 0", fetchCalls.Load())
	}
	schedule, err := repository.SourceRefreshSchedule(
		context.Background(), store.ResetCreditsSourceInstanceWhamDefault,
	)
	if err != nil || schedule.ActiveClaimID != nil || schedule.Reason != store.RefreshReasonNormalInterval ||
		schedule.NextDueAtMS == nil || *schedule.NextDueAtMS != recoveryAtMS+refreshDelay {
		t.Fatalf("recovered success schedule = %#v, %v", schedule, err)
	}
}

func TestQuotaRefreshCoordinatorRecoversRecordedRetryAfterWithoutEarlyFetch(t *testing.T) {
	t.Parallel()

	const (
		claimedAtMS   = int64(1_784_060_000_000)
		recoveryAtMS  = claimedAtMS + 1_000
		retryAtMS     = claimedAtMS + 900_000
		recordedClaim = "recorded-rate-limit-claim"
	)
	repository := newQuotaRefreshTestRepository(t)
	seedRecordedResetCreditsClaim(t, repository, recordedClaim, claimedAtMS, 1_000, int64Pointer(retryAtMS))
	policy, err := quotaonline.NewRefreshPolicy(func() float64 { return 0 })
	if err != nil {
		t.Fatalf("NewRefreshPolicy() error = %v", err)
	}
	var fetchCalls atomic.Int32
	coordinator, err := NewQuotaRefreshCoordinator(QuotaRefreshCoordinatorConfig{
		Repository: repository,
		Preferences: staticRefreshPreferences{snapshot: preferences.Snapshot{
			Online: preferences.OnlinePreferences{ResetCreditsEnabled: true},
			Refresh: preferences.RefreshPreferences{
				QuotaIntervalSeconds: 300, ResetCreditsIntervalSeconds: 1_800,
			},
		}},
		Policy:       &policy,
		QuotaFetcher: SourceRefreshFunc(func(context.Context, string) error { return nil }),
		ResetCreditsFetcher: SourceRefreshFunc(func(context.Context, string) error {
			fetchCalls.Add(1)
			return nil
		}),
		Clock: func() time.Time { return time.UnixMilli(recoveryAtMS) },
	})
	if err != nil {
		t.Fatalf("NewQuotaRefreshCoordinator() error = %v", err)
	}
	if err := coordinator.RunDueCycle(context.Background()); err != nil {
		t.Fatalf("RunDueCycle() error = %v", err)
	}
	if fetchCalls.Load() != 0 {
		t.Fatalf("recovery fetch calls = %d, want 0", fetchCalls.Load())
	}
	schedule, err := repository.SourceRefreshSchedule(
		context.Background(), store.ResetCreditsSourceInstanceWhamDefault,
	)
	if err != nil || schedule.ActiveClaimID != nil || schedule.Reason != store.RefreshReasonRetryAfter ||
		schedule.NextDueAtMS == nil || *schedule.NextDueAtMS != retryAtMS {
		t.Fatalf("recovered retry-after schedule = %#v, %v", schedule, err)
	}
}

func TestQuotaRefreshRunnerSurvivesTransientFetcherFailureAndRecoversLease(t *testing.T) {
	t.Parallel()

	const baseMS = int64(1_784_070_000_000)
	repository := newQuotaRefreshTestRepository(t)
	policy, err := quotaonline.NewRefreshPolicy(func() float64 { return 0 })
	if err != nil {
		t.Fatalf("NewRefreshPolicy() error = %v", err)
	}
	var nowMS atomic.Int64
	nowMS.Store(baseMS)
	var fetchCalls atomic.Int32
	transientSeen := make(chan struct{})
	recoveredSeen := make(chan struct{})
	coordinator, err := NewQuotaRefreshCoordinator(QuotaRefreshCoordinatorConfig{
		Repository: repository,
		Preferences: staticRefreshPreferences{snapshot: preferences.Snapshot{
			Online: preferences.OnlinePreferences{ResetCreditsEnabled: true},
			Refresh: preferences.RefreshPreferences{
				QuotaIntervalSeconds: 300, ResetCreditsIntervalSeconds: 1_800,
			},
		}},
		Policy:       &policy,
		ClaimLease:   time.Second,
		QuotaFetcher: SourceRefreshFunc(func(context.Context, string) error { return nil }),
		ResetCreditsFetcher: SourceRefreshFunc(func(ctx context.Context, requestID string) error {
			call := fetchCalls.Add(1)
			if call == 2 {
				close(transientSeen)
				return errors.New("transient recorder failure")
			}
			atMS := nowMS.Load()
			status := int64(200)
			err := repository.RecordResetCreditsFetch(ctx, store.ResetCreditsFetchRecord{
				SourceInstanceID: store.ResetCreditsSourceInstanceWhamDefault,
				SourceType:       store.ResetCreditsSourceTypeWham, ScopeKey: store.QuotaAccountScopeDefault,
				Attempt: store.SourceAttempt{
					RequestID: requestID, SourceInstanceID: store.ResetCreditsSourceInstanceWhamDefault,
					StartedAtMS: atMS, FinishedAtMS: atMS, Outcome: store.SourceAttemptSucceeded,
					HTTPStatus: &status, AttemptCount: 1,
				},
				Snapshot: &store.ResetCreditsSnapshot{
					SnapshotID: "runner-recovery-" + requestID, RequestID: requestID,
					AccountScope: store.QuotaAccountScopeDefault, ObservedAtMS: atMS,
				},
			})
			if call == 3 && err == nil {
				close(recoveredSeen)
			}
			return err
		}),
		Clock: func() time.Time { return time.UnixMilli(nowMS.Load()) },
		NewRequestID: func(source quotaonline.RefreshSource) (string, error) {
			return fmt.Sprintf("runner-recovery-%s-%d", source, fetchCalls.Load()+1), nil
		},
	})
	if err != nil {
		t.Fatalf("NewQuotaRefreshCoordinator() error = %v", err)
	}
	runner, err := NewQuotaRefreshRunner(coordinator)
	if err != nil {
		t.Fatalf("NewQuotaRefreshRunner() error = %v", err)
	}
	fake := newFakeCronRunner()
	runner.newCronRunner = fake.factory
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- runner.Run(ctx) }()
	fake.waitStarted(t, done)

	nowMS.Store(baseMS + 1_800_000)
	fake.trigger(t)
	select {
	case <-transientSeen:
	case err := <-done:
		cancel()
		t.Fatalf("runner exited before transient failure was observed: %v", err)
	case <-time.After(time.Second):
		cancel()
		t.Fatal("transient fetcher failure was not exercised")
	}
	select {
	case err := <-done:
		cancel()
		t.Fatalf("runner exited on recoverable source error: %v", err)
	case <-time.After(100 * time.Millisecond):
	}

	nowMS.Store(baseMS + 1_801_000)
	fake.trigger(t)
	select {
	case <-recoveredSeen:
	case err := <-done:
		cancel()
		t.Fatalf("runner exited before lease recovery: %v", err)
	case <-time.After(time.Second):
		cancel()
		t.Fatal("expired claim was not recovered by a later cron tick")
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Run() error = %v, want context.Canceled", err)
		}
	case <-time.After(time.Second):
		t.Fatal("runner did not stop")
	}
}

func TestQuotaRefreshRunnerStartsAfterTransientInitializeFailure(t *testing.T) {
	t.Parallel()

	const baseMS = int64(1_784_080_000_000)
	repository := newQuotaRefreshTestRepository(t)
	policy, err := quotaonline.NewRefreshPolicy(func() float64 { return 0 })
	if err != nil {
		t.Fatalf("NewRefreshPolicy() error = %v", err)
	}
	var nowMS atomic.Int64
	nowMS.Store(baseMS)
	var fetchCalls atomic.Int32
	recoveredSeen := make(chan struct{})
	coordinator, err := NewQuotaRefreshCoordinator(QuotaRefreshCoordinatorConfig{
		Repository: repository,
		Preferences: staticRefreshPreferences{snapshot: preferences.Snapshot{
			Online: preferences.OnlinePreferences{ResetCreditsEnabled: true},
			Refresh: preferences.RefreshPreferences{
				QuotaIntervalSeconds: 300, ResetCreditsIntervalSeconds: 1_800,
			},
		}},
		Policy:       &policy,
		ClaimLease:   time.Second,
		QuotaFetcher: SourceRefreshFunc(func(context.Context, string) error { return nil }),
		ResetCreditsFetcher: SourceRefreshFunc(func(ctx context.Context, requestID string) error {
			if fetchCalls.Add(1) == 1 {
				return errors.New("transient initialize recorder failure")
			}
			atMS := nowMS.Load()
			status := int64(200)
			err := repository.RecordResetCreditsFetch(ctx, store.ResetCreditsFetchRecord{
				SourceInstanceID: store.ResetCreditsSourceInstanceWhamDefault,
				SourceType:       store.ResetCreditsSourceTypeWham, ScopeKey: store.QuotaAccountScopeDefault,
				Attempt: store.SourceAttempt{
					RequestID: requestID, SourceInstanceID: store.ResetCreditsSourceInstanceWhamDefault,
					StartedAtMS: atMS, FinishedAtMS: atMS, Outcome: store.SourceAttemptSucceeded,
					HTTPStatus: &status, AttemptCount: 1,
				},
				Snapshot: &store.ResetCreditsSnapshot{
					SnapshotID: "initialize-recovery-" + requestID, RequestID: requestID,
					AccountScope: store.QuotaAccountScopeDefault, ObservedAtMS: atMS,
				},
			})
			if err == nil {
				close(recoveredSeen)
			}
			return err
		}),
		Clock: func() time.Time { return time.UnixMilli(nowMS.Load()) },
		NewRequestID: func(source quotaonline.RefreshSource) (string, error) {
			return fmt.Sprintf("initialize-recovery-%s-%d", source, fetchCalls.Load()+1), nil
		},
	})
	if err != nil {
		t.Fatalf("NewQuotaRefreshCoordinator() error = %v", err)
	}
	runner, err := NewQuotaRefreshRunner(coordinator)
	if err != nil {
		t.Fatalf("NewQuotaRefreshRunner() error = %v", err)
	}
	fake := newFakeCronRunner()
	runner.newCronRunner = fake.factory
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- runner.Run(ctx) }()
	fake.waitStarted(t, done)

	nowMS.Store(baseMS + 1_000)
	fake.trigger(t)
	select {
	case <-recoveredSeen:
	case err := <-done:
		cancel()
		t.Fatalf("runner exited before startup claim recovery: %v", err)
	case <-time.After(time.Second):
		cancel()
		t.Fatal("startup claim was not recovered")
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Run() error = %v, want context.Canceled", err)
		}
	case <-time.After(time.Second):
		t.Fatal("runner did not stop")
	}
}

func TestQuotaRefreshRunnerClassifiesClaimConflictAsRecoverable(t *testing.T) {
	t.Parallel()

	cycleErrors := &quotaRefreshCycleErrors{}
	cycleErrors.add(store.ErrSourceRefreshConflict, isPermanentQuotaRefreshCycleError(store.ErrSourceRefreshConflict))
	if shouldStopQuotaRefreshRunner(cycleErrors.result()) {
		t.Fatal("claim CAS conflict must not stop quota refresh runner")
	}
	cycleErrors = &quotaRefreshCycleErrors{}
	cycleErrors.add(store.ErrInvalidRecord, isPermanentQuotaRefreshCycleError(store.ErrInvalidRecord))
	if !shouldStopQuotaRefreshRunner(cycleErrors.result()) {
		t.Fatal("invalid persisted fact contract must stop quota refresh runner")
	}
}

func seedRecordedResetCreditsClaim(
	t *testing.T,
	repository *store.Repository,
	claimID string,
	claimedAtMS int64,
	leaseMS int64,
	retryAtMS *int64,
) {
	t.Helper()
	dueAtMS := claimedAtMS
	schedule, err := repository.UpsertSourceRefreshSchedule(context.Background(), store.SourceRefreshScheduleUpdate{
		SourceInstanceID: store.ResetCreditsSourceInstanceWhamDefault,
		SourceType:       store.ResetCreditsSourceTypeWham, ScopeKey: store.QuotaAccountScopeDefault,
		NextDueAtMS: &dueAtMS, Reason: store.RefreshReasonStartup, AtMS: claimedAtMS,
	})
	if err != nil {
		t.Fatalf("seed schedule: %v", err)
	}
	if _, claimed, err := repository.ClaimSourceRefresh(
		context.Background(), store.ResetCreditsSourceInstanceWhamDefault, schedule.Revision,
		claimID, store.RefreshTriggerStartup, claimedAtMS, leaseMS,
	); err != nil || !claimed {
		t.Fatalf("seed claim = %v, %v", claimed, err)
	}
	attempt := store.SourceAttempt{
		RequestID: claimID, SourceInstanceID: store.ResetCreditsSourceInstanceWhamDefault,
		StartedAtMS: claimedAtMS, FinishedAtMS: claimedAtMS + 1,
		AttemptCount: 1,
	}
	record := store.ResetCreditsFetchRecord{
		SourceInstanceID: store.ResetCreditsSourceInstanceWhamDefault,
		SourceType:       store.ResetCreditsSourceTypeWham, ScopeKey: store.QuotaAccountScopeDefault,
		Attempt: attempt,
	}
	if retryAtMS == nil {
		status := int64(200)
		record.Attempt.Outcome = store.SourceAttemptSucceeded
		record.Attempt.HTTPStatus = &status
		record.Snapshot = &store.ResetCreditsSnapshot{
			SnapshotID: "recorded-" + claimID, RequestID: claimID,
			AccountScope: store.QuotaAccountScopeDefault, ObservedAtMS: claimedAtMS + 1,
		}
	} else {
		status := int64(429)
		failure := store.SourceFailureHTTP429
		class := store.RuntimeErrorUnavailable
		record.Attempt.Outcome = store.SourceAttemptFailed
		record.Attempt.HTTPStatus = &status
		record.Attempt.FailureCode = &failure
		record.Attempt.ErrorClass = &class
		record.Attempt.RetryAtMS = retryAtMS
	}
	if err := repository.RecordResetCreditsFetch(context.Background(), record); err != nil {
		t.Fatalf("seed recorded attempt: %v", err)
	}
}

func int64Pointer(value int64) *int64 { return &value }

func TestQuotaRefreshCoordinatorCompletesCancelledAttemptWithDetachedContext(t *testing.T) {
	t.Parallel()

	const nowMS = int64(1_784_100_000_000)
	repository := newQuotaRefreshTestRepository(t)
	policy, err := quotaonline.NewRefreshPolicy(func() float64 { return 0 })
	if err != nil {
		t.Fatalf("NewRefreshPolicy() error = %v", err)
	}
	fetchCalls := 0
	var cancelRequest context.CancelFunc
	coordinator, err := NewQuotaRefreshCoordinator(QuotaRefreshCoordinatorConfig{
		Repository: repository,
		Preferences: staticRefreshPreferences{snapshot: preferences.Snapshot{
			Online: preferences.OnlinePreferences{ResetCreditsEnabled: true},
			Refresh: preferences.RefreshPreferences{
				QuotaIntervalSeconds: 300, ResetCreditsIntervalSeconds: 1_800,
			},
		}},
		Policy:       &policy,
		QuotaFetcher: SourceRefreshFunc(func(context.Context, string) error { return nil }),
		ResetCreditsFetcher: SourceRefreshFunc(func(ctx context.Context, requestID string) error {
			fetchCalls++
			if fetchCalls == 1 {
				status := int64(200)
				return repository.RecordResetCreditsFetch(ctx, store.ResetCreditsFetchRecord{
					SourceInstanceID: store.ResetCreditsSourceInstanceWhamDefault,
					SourceType:       store.ResetCreditsSourceTypeWham, ScopeKey: store.QuotaAccountScopeDefault,
					Attempt: store.SourceAttempt{
						RequestID: requestID, SourceInstanceID: store.ResetCreditsSourceInstanceWhamDefault,
						StartedAtMS: nowMS, FinishedAtMS: nowMS, Outcome: store.SourceAttemptSucceeded,
						HTTPStatus: &status, AttemptCount: 1,
					},
					Snapshot: &store.ResetCreditsSnapshot{
						SnapshotID: "cancel-setup-" + requestID, RequestID: requestID,
						AccountScope: store.QuotaAccountScopeDefault, ObservedAtMS: nowMS,
					},
				})
			}
			cancelRequest()
			code := store.SourceFailureCancelled
			class := store.RuntimeErrorCanceled
			return repository.RecordResetCreditsFetch(context.Background(), store.ResetCreditsFetchRecord{
				SourceInstanceID: store.ResetCreditsSourceInstanceWhamDefault,
				SourceType:       store.ResetCreditsSourceTypeWham, ScopeKey: store.QuotaAccountScopeDefault,
				Attempt: store.SourceAttempt{
					RequestID: requestID, SourceInstanceID: store.ResetCreditsSourceInstanceWhamDefault,
					StartedAtMS: nowMS, FinishedAtMS: nowMS, Outcome: store.SourceAttemptCancelled,
					ErrorClass: &class, FailureCode: &code,
				},
			})
		}),
		Clock: func() time.Time { return time.UnixMilli(nowMS) },
		NewRequestID: func(source quotaonline.RefreshSource) (string, error) {
			return fmt.Sprintf("cancel-%s-%d", source, fetchCalls+1), nil
		},
	})
	if err != nil {
		t.Fatalf("NewQuotaRefreshCoordinator() error = %v", err)
	}
	if err := coordinator.Initialize(context.Background()); err != nil {
		t.Fatalf("Initialize() error = %v", err)
	}
	requestCtx, cancel := context.WithCancel(context.Background())
	cancelRequest = cancel
	schedule, err := coordinator.RequestRefresh(
		requestCtx, quotaonline.RefreshSourceResetCredits, store.RefreshTriggerManual,
	)
	if err != nil || requestCtx.Err() != context.Canceled || schedule.ActiveClaimID != nil ||
		schedule.Reason != store.RefreshReasonCancelled || schedule.NextDueAtMS == nil ||
		*schedule.NextDueAtMS != nowMS+1_800_000 {
		t.Fatalf("cancelled refresh = %#v, ctx=%v, error=%v", schedule, requestCtx.Err(), err)
	}
	state, err := repository.SourceState(context.Background(), store.ResetCreditsSourceInstanceWhamDefault)
	if err != nil || state.ConsecutiveFailures != 0 || state.LastFailureCode == nil ||
		*state.LastFailureCode != store.SourceFailureCancelled {
		t.Fatalf("cancelled source state = %#v, %v", state, err)
	}
}

func TestQuotaRefreshCoordinatorReconcilesDisabledPreferenceImmediately(t *testing.T) {
	t.Parallel()

	const nowMS = int64(1_784_200_000_000)
	repository := newQuotaRefreshTestRepository(t)
	policy, err := quotaonline.NewRefreshPolicy(func() float64 { return 0 })
	if err != nil {
		t.Fatalf("NewRefreshPolicy() error = %v", err)
	}
	reader := &mutableRefreshPreferences{snapshot: preferences.Snapshot{
		Online: preferences.OnlinePreferences{ResetCreditsEnabled: true},
		Refresh: preferences.RefreshPreferences{
			QuotaIntervalSeconds: 300, ResetCreditsIntervalSeconds: 1_800,
		},
	}}
	fetchCalls := 0
	coordinator, err := NewQuotaRefreshCoordinator(QuotaRefreshCoordinatorConfig{
		Repository: repository, Preferences: reader, Policy: &policy,
		QuotaFetcher: SourceRefreshFunc(func(context.Context, string) error { return nil }),
		ResetCreditsFetcher: SourceRefreshFunc(func(ctx context.Context, requestID string) error {
			fetchCalls++
			status := int64(200)
			return repository.RecordResetCreditsFetch(ctx, store.ResetCreditsFetchRecord{
				SourceInstanceID: store.ResetCreditsSourceInstanceWhamDefault,
				SourceType:       store.ResetCreditsSourceTypeWham, ScopeKey: store.QuotaAccountScopeDefault,
				Attempt: store.SourceAttempt{
					RequestID: requestID, SourceInstanceID: store.ResetCreditsSourceInstanceWhamDefault,
					StartedAtMS: nowMS, FinishedAtMS: nowMS, Outcome: store.SourceAttemptSucceeded,
					HTTPStatus: &status, AttemptCount: 1,
				},
				Snapshot: &store.ResetCreditsSnapshot{
					SnapshotID: "disable-" + requestID, RequestID: requestID,
					AccountScope: store.QuotaAccountScopeDefault, ObservedAtMS: nowMS,
				},
			})
		}),
		Clock: func() time.Time { return time.UnixMilli(nowMS) },
	})
	if err != nil {
		t.Fatalf("NewQuotaRefreshCoordinator() error = %v", err)
	}
	if err := coordinator.Initialize(context.Background()); err != nil || fetchCalls != 1 {
		t.Fatalf("Initialize() calls=%d error=%v", fetchCalls, err)
	}
	reader.snapshot.Online.ResetCreditsEnabled = false
	if err := coordinator.ReconcilePreferences(context.Background()); err != nil {
		t.Fatalf("ReconcilePreferences() error = %v", err)
	}
	schedule, err := repository.SourceRefreshSchedule(
		context.Background(), store.ResetCreditsSourceInstanceWhamDefault,
	)
	if err != nil || schedule.NextDueAtMS != nil || schedule.Reason != store.RefreshReasonDisabled ||
		fetchCalls != 1 {
		t.Fatalf("disabled schedule = %#v, calls=%d, error=%v", schedule, fetchCalls, err)
	}
}

type staticRefreshPreferences struct {
	snapshot preferences.Snapshot
	err      error
}

type mutableRefreshPreferences struct {
	snapshot preferences.Snapshot
}

func (reader *mutableRefreshPreferences) LoadPreferences(ctx context.Context) (preferences.Snapshot, error) {
	if err := ctx.Err(); err != nil {
		return preferences.Snapshot{}, err
	}
	return reader.snapshot, nil
}

func (reader staticRefreshPreferences) LoadPreferences(ctx context.Context) (preferences.Snapshot, error) {
	if err := ctx.Err(); err != nil {
		return preferences.Snapshot{}, err
	}
	return reader.snapshot, reader.err
}

func newQuotaRefreshTestRepository(t *testing.T) *store.Repository {
	t.Helper()
	directory := t.TempDir()
	if err := os.Chmod(directory, 0o700); err != nil {
		t.Fatalf("Chmod(temp dir) error = %v", err)
	}
	database, err := storesqlite.Open(context.Background(), storesqlite.Config{
		Path: filepath.Join(directory, "quota-refresh.db"),
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
