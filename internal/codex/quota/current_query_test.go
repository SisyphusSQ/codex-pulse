package quota

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"gorm.io/gorm"

	"github.com/SisyphusSQ/codex-pulse/internal/codex/accountbinding"
	"github.com/SisyphusSQ/codex-pulse/internal/store"
	storesqlite "github.com/SisyphusSQ/codex-pulse/internal/store/sqlite"
)

func TestCurrentQueryMapsStableNullZeroExplanationAndRefreshContract(t *testing.T) {
	t.Parallel()

	const nowMS = int64(1_784_320_000_000)
	zero := float64(0)
	primaryMinutes := int64(300)
	primaryReset := nowMS + 3_600_000
	primaryObservationID := "opaque-primary"
	primaryLimitName := "GPT-5.3-Codex-Spark"
	wham := store.QuotaSourceWham
	selected := store.QuotaEvidenceSelected
	trusted := store.QuotaExplanationTrusted
	accepted := store.QuotaValidityAccepted
	secondaryAttempt := nowMS - 1_000
	zeroCount := int64(0)
	http429 := store.SourceFailureHTTP429
	retryAtMS := nowMS + 600_000
	quotaReason := store.RefreshReasonNetworkBackoff
	quotaTrigger := store.RefreshTriggerManual
	claimStartedAtMS := nowMS - 500
	claimExpiresAtMS := nowMS + 29_500
	reader := currentSnapshotReaderFunc(func(
		context.Context, string, int64,
	) (store.QuotaCurrentSnapshot, error) {
		return store.QuotaCurrentSnapshot{
			AccountScope: store.QuotaAccountScopeDefault, EvaluatedAtMS: nowMS,
			Windows: []store.QuotaCurrentWindowSnapshot{
				{
					Current: store.QuotaCurrent{
						AccountScope: store.QuotaAccountScopeDefault, WindowKind: store.QuotaWindowPrimary,
						LimitID: "codex", ObservationID: &primaryObservationID,
						EffectiveUsedPercent: &zero, WindowMinutes: &primaryMinutes,
						ResetsAtMS: &primaryReset, WindowGeneration: &primaryReset,
						SelectedSource: &wham, FreshnessState: store.QuotaCurrentFresh,
						ConflictState: store.QuotaConflictNone, LastSuccessAtMS: &secondaryAttempt,
						LastAttemptAtMS: &secondaryAttempt, RuleVersion: "quota-arbiter-v1",
						ExplanationCode: trusted, EvaluatedAtMS: nowMS,
					},
					Observations: []store.QuotaObservation{{
						ObservationID: primaryObservationID, AccountScope: store.QuotaAccountScopeDefault,
						Source: wham, LimitID: stringPointer("codex"), LimitName: &primaryLimitName,
						WindowKind:  store.QuotaWindowPrimary,
						UsedPercent: zero, WindowMinutes: primaryMinutes, ResetsAtMS: primaryReset,
						Validity: accepted, FirstObservedAtMS: secondaryAttempt,
						LastObservedAtMS: secondaryAttempt, SampleCount: 1,
					}},
					Evidence: []store.QuotaArbitrationEvidence{{
						AccountScope: store.QuotaAccountScopeDefault, WindowKind: store.QuotaWindowPrimary,
						LimitID: "codex", ObservationID: primaryObservationID,
						WindowGeneration: &primaryReset, Disposition: selected, ExplanationCode: trusted,
					}},
				},
				{
					Current: store.QuotaCurrent{
						AccountScope: store.QuotaAccountScopeDefault, WindowKind: store.QuotaWindowSecondary,
						LimitID: "codex", FreshnessState: store.QuotaCurrentNeverLoaded,
						ConflictState: store.QuotaConflictNone, LastAttemptAtMS: &secondaryAttempt,
						RuleVersion: "quota-arbiter-v1", ExplanationCode: store.QuotaExplanationUnavailable,
						EvaluatedAtMS: nowMS,
					},
				},
			},
			OnlineSourceState: &store.SourceState{
				SourceInstanceID: store.QuotaSourceInstanceWhamDefault, SourceType: store.QuotaSourceTypeWham,
				ScopeKey: store.QuotaAccountScopeDefault, LastAttemptAtMS: &secondaryAttempt,
				LastSuccessAtMS: &secondaryAttempt, NextDueAtMS: &retryAtMS,
				ConsecutiveFailures: 1, LastFailureCode: &http429, FreshnessState: store.SourceFreshnessStale,
			},
			QuotaRefresh: &store.SourceRefreshSchedule{
				SourceInstanceID: store.QuotaSourceInstanceWhamDefault, SourceType: store.QuotaSourceTypeWham,
				ScopeKey: store.QuotaAccountScopeDefault, NextDueAtMS: &retryAtMS, Reason: quotaReason,
				ActiveClaimID: stringPointer("private-claim-id"), ActiveTrigger: &quotaTrigger,
				ClaimStartedAtMS: &claimStartedAtMS, ClaimExpiresAtMS: &claimExpiresAtMS,
				Revision: 4, UpdatedAtMS: nowMS,
			},
			ResetCredits: store.ResetCreditsSummary{
				AccountScope: store.QuotaAccountScopeDefault, SnapshotID: stringPointer("opaque-reset-snapshot"),
				AvailableCount: &zeroCount, CumulativeRemainingMS: &zeroCount,
				LastSuccessAtMS: &secondaryAttempt, LastAttemptAtMS: &secondaryAttempt,
				FreshnessState: store.SourceFreshnessCurrent, EvaluationAtMS: nowMS,
			},
		}, nil
	})
	service, err := NewCurrentQueryService(reader)
	if err != nil {
		t.Fatalf("NewCurrentQueryService() error = %v", err)
	}
	response, err := service.Query(context.Background(), nowMS)
	if err != nil {
		t.Fatalf("Query() error = %v", err)
	}
	if response.Version != CurrentContractVersion || response.AccountScope != store.QuotaAccountScopeDefault ||
		len(response.Windows) != 2 || response.Windows[0].RemainingPercent == nil ||
		*response.Windows[0].RemainingPercent != 100 || response.Windows[0].UnknownReason != nil ||
		response.Windows[1].RemainingPercent != nil || response.Windows[1].UnknownReason == nil ||
		*response.Windows[1].UnknownReason != CurrentUnknownNeverLoaded {
		t.Fatalf("response windows = %#v", response.Windows)
	}
	if response.Windows[0].LimitName == nil || *response.Windows[0].LimitName != primaryLimitName {
		t.Fatalf("selected window limit name was not preserved: %#v", response.Windows[0])
	}
	if response.NextReset.AtMS == nil || *response.NextReset.AtMS != primaryReset ||
		response.NextReset.RemainingMS == nil || *response.NextReset.RemainingMS != 3_600_000 ||
		response.NextReset.TrustedWindowCount != 1 || len(response.Sources) != 2 ||
		response.Sources[0].Source != CurrentSourceLocal || response.Sources[0].UnknownReason == nil ||
		response.Sources[1].Source != CurrentSourceWham || response.Sources[1].FailureCode == nil ||
		*response.Sources[1].FailureCode != http429 {
		t.Fatalf("response reset/sources = %#v / %#v", response.NextReset, response.Sources)
	}
	if response.ResetCredits.AvailableCount == nil || *response.ResetCredits.AvailableCount != 0 ||
		response.ResetCredits.UnknownReason != nil || response.Refresh.Quota.State != CurrentRefreshInFlight ||
		response.Refresh.Quota.ActiveTrigger == nil || *response.Refresh.Quota.ActiveTrigger != quotaTrigger ||
		response.Refresh.Quota.NextDueAtMS == nil || *response.Refresh.Quota.NextDueAtMS != retryAtMS ||
		response.Refresh.Quota.Reason == nil || *response.Refresh.Quota.Reason != quotaReason ||
		response.Refresh.Quota.ClaimStartedAtMS == nil || response.Refresh.Quota.ClaimExpiresAtMS == nil ||
		response.Refresh.Quota.UnknownReason != nil || response.Refresh.ResetCredits.State != CurrentRefreshUnknown {
		t.Fatalf("response reset credits/refresh = %#v / %#v", response.ResetCredits, response.Refresh)
	}
	encoded, err := json.Marshal(response)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	for _, forbidden := range []string{"private-claim-id", "snapshotId", "creditId", "requestId", "authorization", "cookie"} {
		if strings.Contains(strings.ToLower(string(encoded)), strings.ToLower(forbidden)) {
			t.Fatalf("response leaks forbidden marker %q: %s", forbidden, encoded)
		}
	}
}

func TestCurrentLimitNameFallsBackToLatestAcceptedObservation(t *testing.T) {
	t.Parallel()

	selectedID := "selected-local-without-name"
	displayName := "GPT-5.3-Codex-Spark"
	observations := []store.QuotaObservation{
		{
			ObservationID:    selectedID,
			LimitName:        nil,
			Validity:         store.QuotaValidityAccepted,
			LastObservedAtMS: 1_000,
		},
		{
			ObservationID:    "newer-wham-with-name",
			LimitName:        &displayName,
			Validity:         store.QuotaValidityAccepted,
			LastObservedAtMS: 2_000,
		},
	}

	got, err := currentLimitName(observations, &selectedID)
	if err != nil {
		t.Fatalf("currentLimitName() error = %v", err)
	}
	if got == nil || *got != displayName {
		t.Fatalf("currentLimitName() = %#v, want %q from latest accepted observation", got, displayName)
	}
}

func TestCurrentLimitNameIgnoresRejectedSupplementalObservation(t *testing.T) {
	t.Parallel()

	selectedID := "selected-local-without-name"
	acceptedName := "GPT-5.3-Codex-Spark"
	rejectedName := "untrusted-name"
	observations := []store.QuotaObservation{
		{ObservationID: selectedID, Validity: store.QuotaValidityAccepted, LastObservedAtMS: 1_000},
		{
			ObservationID: "accepted-name", LimitName: &acceptedName,
			Validity: store.QuotaValidityAccepted, LastObservedAtMS: 2_000,
		},
		{
			ObservationID: "rejected-name", LimitName: &rejectedName,
			Validity: store.QuotaValidityRejected, LastObservedAtMS: 3_000,
		},
	}

	got, err := currentLimitName(observations, &selectedID)
	if err != nil {
		t.Fatalf("currentLimitName() error = %v", err)
	}
	if got == nil || *got != acceptedName {
		t.Fatalf("currentLimitName() = %#v, want accepted name %q", got, acceptedName)
	}
}

func TestCurrentLimitNameDoesNotChooseBetweenLatestConflictingNames(t *testing.T) {
	t.Parallel()

	selectedID := "selected-local-without-name"
	firstName := "First Model Name"
	secondName := "Second Model Name"
	observations := []store.QuotaObservation{
		{ObservationID: selectedID, Validity: store.QuotaValidityAccepted, LastObservedAtMS: 1_000},
		{
			ObservationID: "first-name", LimitName: &firstName,
			Validity: store.QuotaValidityAccepted, LastObservedAtMS: 2_000,
		},
		{
			ObservationID: "second-name", LimitName: &secondName,
			Validity: store.QuotaValidityAccepted, LastObservedAtMS: 2_000,
		},
	}

	got, err := currentLimitName(observations, &selectedID)
	if err != nil {
		t.Fatalf("currentLimitName() error = %v", err)
	}
	if got != nil {
		t.Fatalf("currentLimitName() = %q, want nil for latest conflicting names", *got)
	}
}

func TestCurrentQueryCancellationAndMissingProjectionAreReadOnlyFailures(t *testing.T) {
	t.Parallel()

	calls := 0
	reader := currentSnapshotReaderFunc(func(
		ctx context.Context, _ string, _ int64,
	) (store.QuotaCurrentSnapshot, error) {
		calls++
		if err := ctx.Err(); err != nil {
			return store.QuotaCurrentSnapshot{}, err
		}
		return store.QuotaCurrentSnapshot{}, store.ErrNotFound
	})
	service, err := NewCurrentQueryService(reader)
	if err != nil {
		t.Fatalf("NewCurrentQueryService() error = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := service.Query(ctx, 1); !errors.Is(err, context.Canceled) || calls != 0 {
		t.Fatalf("Query(cancelled) calls=%d error=%v", calls, err)
	}
	if _, err := service.Query(context.Background(), 1); !errors.Is(err, ErrQuotaCurrentUnavailable) || calls != 1 {
		t.Fatalf("Query(missing projection) calls=%d error=%v", calls, err)
	}
}

func TestCurrentQueryMapsResetCreditItemsWithoutIdentifiers(t *testing.T) {
	t.Parallel()

	const nowMS = int64(1_784_320_000_000)
	one := int64(1)
	remaining := int64(3_600_000)
	last := nowMS - 1_000
	reader := currentSnapshotReaderFunc(func(
		context.Context, string, int64,
	) (store.QuotaCurrentSnapshot, error) {
		return store.QuotaCurrentSnapshot{
			AccountScope: store.QuotaAccountScopeDefault, EvaluatedAtMS: nowMS,
			ResetCredits: store.ResetCreditsSummary{
				AccountScope: store.QuotaAccountScopeDefault, SnapshotID: stringPointer("private-snapshot"),
				AvailableCount:        &one,
				CumulativeRemainingMS: &remaining, NextExpiresAtMS: int64Pointer(nowMS + remaining),
				LastSuccessAtMS: &last, LastAttemptAtMS: &last,
				FreshnessState: store.SourceFreshnessCurrent, EvaluationAtMS: nowMS,
				Credits: []store.ResetCredit{
					{Status: store.ResetCreditRedeemed, Type: store.ResetCreditTypeCodexRateLimits, GrantedAtMS: nowMS - 20_000, ExpiresAtMS: int64Pointer(nowMS + 7_200_000), RedeemedAtMS: &last},
					{Status: store.ResetCreditAvailable, Type: store.ResetCreditTypeCodexRateLimits, GrantedAtMS: nowMS - 10_000, ExpiresAtMS: int64Pointer(nowMS + remaining)},
				},
			},
		}, nil
	})
	service, err := NewCurrentQueryService(reader)
	if err != nil {
		t.Fatalf("NewCurrentQueryService() error = %v", err)
	}
	response, err := service.Query(context.Background(), nowMS)
	if err != nil {
		t.Fatalf("Query() error = %v", err)
	}
	items := response.ResetCredits.Items
	if len(items) != 2 || items[0].Status != store.ResetCreditAvailable ||
		items[0].RemainingMS == nil || *items[0].RemainingMS != remaining ||
		items[1].Status != store.ResetCreditRedeemed || items[1].RedeemedAtMS == nil {
		t.Fatalf("reset credit items = %#v", items)
	}
	encoded, err := json.Marshal(response.ResetCredits)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	for _, forbidden := range []string{"private-snapshot", "creditId", "creditHash", "requestId"} {
		if strings.Contains(strings.ToLower(string(encoded)), strings.ToLower(forbidden)) {
			t.Fatalf("reset credit items leak %q: %s", forbidden, encoded)
		}
	}
}

func TestCurrentQueryMapsCountOnlyAndPartialResetCreditsWithoutInventingRemaining(t *testing.T) {
	t.Parallel()

	const nowMS = int64(1_784_320_000_000)
	three := int64(3)
	four := int64(4)
	last := nowMS - 1_000
	countOnly := currentSnapshotReaderFunc(func(
		context.Context, string, int64,
	) (store.QuotaCurrentSnapshot, error) {
		return store.QuotaCurrentSnapshot{
			AccountScope: store.QuotaAccountScopeDefault, EvaluatedAtMS: nowMS,
			ResetCredits: store.ResetCreditsSummary{
				AccountScope: store.QuotaAccountScopeDefault, SnapshotID: stringPointer("private-count-only"),
				AvailableCount: &three, LastSuccessAtMS: &last, LastAttemptAtMS: &last,
				FreshnessState: store.SourceFreshnessCurrent, EvaluationAtMS: nowMS,
			},
		}, nil
	})
	countService, err := NewCurrentQueryService(countOnly)
	if err != nil {
		t.Fatalf("NewCurrentQueryService(count-only) error = %v", err)
	}
	countResponse, err := countService.Query(context.Background(), nowMS)
	if err != nil {
		t.Fatalf("Query(count-only) error = %v", err)
	}
	if countResponse.ResetCredits.AvailableCount == nil || *countResponse.ResetCredits.AvailableCount != 3 ||
		countResponse.ResetCredits.CumulativeRemainingMS != nil ||
		countResponse.ResetCredits.NextExpiresAtMS != nil ||
		countResponse.ResetCredits.DetailsState != store.ResetCreditDetailsUnavailable ||
		len(countResponse.ResetCredits.Items) != 0 {
		t.Fatalf("count-only reset credits = %#v", countResponse.ResetCredits)
	}

	partial := currentSnapshotReaderFunc(func(
		context.Context, string, int64,
	) (store.QuotaCurrentSnapshot, error) {
		return store.QuotaCurrentSnapshot{
			AccountScope: store.QuotaAccountScopeDefault, EvaluatedAtMS: nowMS,
			ResetCredits: store.ResetCreditsSummary{
				AccountScope: store.QuotaAccountScopeDefault, SnapshotID: stringPointer("private-partial"),
				AvailableCount: &four, LastSuccessAtMS: &last, LastAttemptAtMS: &last,
				FreshnessState: store.SourceFreshnessCurrent, EvaluationAtMS: nowMS,
				Credits: []store.ResetCredit{{
					Status: store.ResetCreditAvailable, Type: store.ResetCreditTypeCodexRateLimits,
					GrantedAtMS: nowMS - 10_000,
				}},
			},
		}, nil
	})
	partialService, err := NewCurrentQueryService(partial)
	if err != nil {
		t.Fatalf("NewCurrentQueryService(partial) error = %v", err)
	}
	partialResponse, err := partialService.Query(context.Background(), nowMS)
	if err != nil {
		t.Fatalf("Query(partial) error = %v", err)
	}
	if partialResponse.ResetCredits.AvailableCount == nil || *partialResponse.ResetCredits.AvailableCount != 4 ||
		partialResponse.ResetCredits.CumulativeRemainingMS != nil ||
		partialResponse.ResetCredits.NextExpiresAtMS != nil ||
		partialResponse.ResetCredits.DetailsState != store.ResetCreditDetailsPartial ||
		len(partialResponse.ResetCredits.Items) != 1 ||
		partialResponse.ResetCredits.Items[0].RemainingMS != nil {
		t.Fatalf("partial reset credits = %#v", partialResponse.ResetCredits)
	}
}

func TestCurrentQueryMapsLoadedResetCreditInventoryWithoutInventingHistoricalCounts(t *testing.T) {
	t.Parallel()

	const nowMS = int64(1_784_320_000_000)
	zero := int64(0)
	last := nowMS - 1_000
	reader := currentSnapshotReaderFunc(func(
		context.Context, string, int64,
	) (store.QuotaCurrentSnapshot, error) {
		return store.QuotaCurrentSnapshot{
			AccountScope: store.QuotaAccountScopeDefault, EvaluatedAtMS: nowMS,
			ResetCredits: store.ResetCreditsSummary{
				AccountScope: store.QuotaAccountScopeDefault, SnapshotID: stringPointer("private-empty-inventory"),
				AvailableCount: &zero, CumulativeRemainingMS: &zero,
				LastSuccessAtMS: &last, LastAttemptAtMS: &last,
				FreshnessState: store.SourceFreshnessCurrent, EvaluationAtMS: nowMS,
			},
		}, nil
	})
	service, err := NewCurrentQueryService(reader)
	if err != nil {
		t.Fatalf("NewCurrentQueryService() error = %v", err)
	}
	response, err := service.Query(context.Background(), nowMS)
	if err != nil {
		t.Fatalf("Query() error = %v", err)
	}
	if response.ResetCredits.AvailableCount == nil || *response.ResetCredits.AvailableCount != 0 ||
		response.ResetCredits.CumulativeRemainingMS == nil ||
		*response.ResetCredits.CumulativeRemainingMS != 0 ||
		response.ResetCredits.UnknownReason != nil || len(response.ResetCredits.Items) != 0 {
		t.Fatalf("empty inventory current = %#v, want known available zero without historical counts", response.ResetCredits)
	}
}

func TestCurrentQueryEmptyRepositoryReturnsDeterministicUnknownContract(t *testing.T) {
	t.Parallel()

	_, service := newCurrentQueryTestService(t)
	const evaluatedAtMS = int64(1_784_320_000_000)
	first := queryCurrentAt(t, service, evaluatedAtMS)
	second := queryCurrentAt(t, service, evaluatedAtMS)
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("empty query is not deterministic:\nfirst=%#v\nsecond=%#v", first, second)
	}
	if first.Version != CurrentContractVersion || first.Binding.State != store.CodexAccountBindingUnknown ||
		len(first.Windows) != 0 || len(first.Sources) != 1 || first.Sources[0].Source != CurrentSourceAppServer ||
		first.Sources[0].UnknownReason == nil || *first.Sources[0].UnknownReason != CurrentUnknownBindingUnavailable ||
		first.NextReset.AtMS != nil || first.NextReset.UnknownReason == nil ||
		*first.NextReset.UnknownReason != CurrentUnknownBindingUnavailable ||
		first.ResetCredits.AvailableCount != nil || first.ResetCredits.UnknownReason == nil ||
		*first.ResetCredits.UnknownReason != CurrentUnknownBindingUnavailable ||
		first.Refresh.Quota.State != CurrentRefreshUnknown || first.Refresh.Quota.UnknownReason == nil ||
		*first.Refresh.Quota.UnknownReason != CurrentUnknownBindingUnavailable ||
		first.Refresh.ResetCredits.State != CurrentRefreshUnknown {
		t.Fatalf("empty response = %#v", first)
	}
	if first.Windows == nil {
		t.Fatalf("empty windows = nil, want empty slice")
	}
}

func TestCurrentQueryRejectsMalformedResetCreditsWithoutLeakingSnapshotIdentity(t *testing.T) {
	t.Parallel()

	const (
		evaluatedAtMS   = int64(1_784_320_000_000)
		privateSnapshot = "private-reset-snapshot"
	)
	zero := int64(0)
	reader := currentSnapshotReaderFunc(func(
		context.Context, string, int64,
	) (store.QuotaCurrentSnapshot, error) {
		return store.QuotaCurrentSnapshot{
			AccountScope: store.QuotaAccountScopeDefault, EvaluatedAtMS: evaluatedAtMS,
			ResetCredits: store.ResetCreditsSummary{
				AccountScope: store.QuotaAccountScopeDefault, EvaluationAtMS: evaluatedAtMS,
				SnapshotID: stringPointer(privateSnapshot), AvailableCount: &zero,
				FreshnessState: store.SourceFreshnessCurrent,
			},
		}, nil
	})
	service, err := NewCurrentQueryService(reader)
	if err != nil {
		t.Fatalf("NewCurrentQueryService() error = %v", err)
	}
	_, err = service.Query(context.Background(), evaluatedAtMS)
	if !errors.Is(err, ErrInvalidCurrentQuery) {
		t.Fatalf("Query(malformed reset credits) error = %v, want ErrInvalidCurrentQuery", err)
	}
	if strings.Contains(err.Error(), privateSnapshot) {
		t.Fatalf("Query(malformed reset credits) leaked snapshot identity: %v", err)
	}
}

func TestCurrentQueryKeepsLatestUsageDecreaseAndTrustedResetCountdown(t *testing.T) {
	t.Parallel()

	repository, service := newCurrentQueryTestService(t)
	nowMS := time.Now().UnixMilli()
	resetAtMS := nowMS + 5*60*60*1_000
	recordCurrentQueryAppServer(t, repository, "usage-before-decrease", nowMS, 40, -1, resetAtMS, 0)
	recordCurrentQueryAppServer(t, repository, "usage-after-decrease", nowMS+1, 20, -1, resetAtMS, 0)
	response := queryCurrentAt(t, service, nowMS+2)
	if len(response.Windows) != 1 || response.Windows[0].Freshness != store.QuotaCurrentFresh ||
		response.Windows[0].UsedPercent == nil || *response.Windows[0].UsedPercent != 20 ||
		response.Windows[0].ResetsAtMS == nil || *response.Windows[0].ResetsAtMS != resetAtMS ||
		response.Windows[0].ResetRemainingMS == nil ||
		*response.Windows[0].ResetRemainingMS != resetAtMS-(nowMS+2) ||
		response.NextReset.AtMS == nil || *response.NextReset.AtMS != resetAtMS ||
		response.NextReset.RemainingMS == nil ||
		*response.NextReset.RemainingMS != resetAtMS-(nowMS+2) {
		t.Fatalf("usage decrease response did not remain trusted = %#v", response)
	}
}

func TestCurrentQuerySurvivesStoreRestartWithCompleteAggregateFacts(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()
	if err := os.Chmod(directory, 0o700); err != nil {
		t.Fatalf("Chmod(temp dir) error = %v", err)
	}
	databasePath := filepath.Join(directory, "quota-current-restart.db")
	database, repository, service := openCurrentQueryTestRuntime(t, databasePath)
	nowMS := time.Now().UnixMilli()
	primaryResetAtMS := nowMS + 5*60*60*1_000
	secondaryResetAtMS := nowMS + 7*24*60*60*1_000
	recordCurrentQueryAppServer(
		t, repository, "restart-quota", nowMS, 40, 20, primaryResetAtMS, secondaryResetAtMS,
	)
	creditExpiresAtMS := nowMS + 60_000
	recordCurrentQueryResetCredits(t, repository, "restart-reset", nowMS, &creditExpiresAtMS)
	scope, generation := currentQueryBindingAt(t, repository, nowMS)
	quotaDueAtMS := nowMS + 300_000
	if _, err := repository.UpsertSourceRefreshSchedule(context.Background(), store.SourceRefreshScheduleUpdate{
		SourceInstanceID: store.QuotaSourceInstanceAppServer(scope), SourceType: store.QuotaSourceTypeAppServerRateLimits,
		ScopeKey: scope, BindingGeneration: generation, NextDueAtMS: &quotaDueAtMS,
		Reason: store.RefreshReasonNormalInterval, AtMS: nowMS,
	}); err != nil {
		t.Fatalf("UpsertSourceRefreshSchedule(quota) error = %v", err)
	}
	resetDueAtMS := nowMS + 1_800_000
	if _, err := repository.UpsertSourceRefreshSchedule(context.Background(), store.SourceRefreshScheduleUpdate{
		SourceInstanceID: store.ResetCreditsSourceInstanceAppServer(scope),
		SourceType:       store.ResetCreditsSourceTypeAppServer, ScopeKey: scope, BindingGeneration: generation,
		NextDueAtMS: &resetDueAtMS, Reason: store.RefreshReasonNormalInterval, AtMS: nowMS,
	}); err != nil {
		t.Fatalf("UpsertSourceRefreshSchedule(reset credits) error = %v", err)
	}

	before := queryCurrentAt(t, service, nowMS)
	if err := database.Close(context.Background()); err != nil {
		t.Fatalf("Close(before restart) error = %v", err)
	}
	reopenedDatabase, _, reopenedService := openCurrentQueryTestRuntime(t, databasePath)
	t.Cleanup(func() {
		if err := reopenedDatabase.Close(context.Background()); err != nil {
			t.Errorf("Close(reopened database) error = %v", err)
		}
	})
	after := queryCurrentAt(t, reopenedService, nowMS)
	if !reflect.DeepEqual(after, before) {
		t.Fatalf("restart changed current contract:\nbefore=%#v\nafter=%#v", before, after)
	}
	advanced := queryCurrentAt(t, reopenedService, nowMS+120_000)
	if advanced.Windows[0].ResetRemainingMS == nil || before.Windows[0].ResetRemainingMS == nil ||
		*advanced.Windows[0].ResetRemainingMS != *before.Windows[0].ResetRemainingMS-120_000 ||
		advanced.ResetCredits.AvailableCount == nil || *advanced.ResetCredits.AvailableCount != 0 ||
		advanced.ResetCredits.NextExpiresAtMS != nil {
		t.Fatalf("advanced restart response = %#v", advanced)
	}
}

func TestCurrentQueryMapsTamperedProjectionToRecoverableDomainError(t *testing.T) {
	t.Parallel()

	database, repository, service := newCurrentQueryTestRuntime(t)
	nowMS := time.Now().UnixMilli()
	resetAtMS := nowMS + 5*60*60*1_000
	recordCurrentQueryAppServer(t, repository, "tamper-app-server", nowMS, 40, -1, resetAtMS, 0)
	if err := database.Write(context.Background(), func(ctx context.Context, transaction *gorm.DB) error {
		return transaction.WithContext(ctx).
			Where("observation_id = ?", "query-tamper-app-server-primary").
			Delete(&currentQueryEvidenceFixture{}).Error
	}); err != nil {
		t.Fatalf("tamper projection evidence: %v", err)
	}
	if _, err := service.Query(context.Background(), nowMS+2); !errors.Is(err, ErrQuotaCurrentUnavailable) ||
		!errors.Is(err, store.ErrNotFound) {
		t.Fatalf("Query(tampered projection) error = %v, want domain unavailable + Store cause", err)
	}
	if err := repository.RebuildQuotaProjection(
		context.Background(), store.DefaultQuotaArbitrationRule(),
	); err != nil {
		t.Fatalf("RebuildQuotaProjection() error = %v", err)
	}
	if response, err := service.Query(context.Background(), nowMS+2); err != nil || len(response.Windows) != 1 {
		t.Fatalf("Query(after rebuild) = %#v, %v", response, err)
	}
}

func TestCurrentQueryLocalJSONLIsNotProjectedForConfirmedAccount(t *testing.T) {
	t.Parallel()

	repository, service := newCurrentQueryTestService(t)
	nowMS := time.Now().UnixMilli()
	resetAtMS := nowMS + 5*60*60*1_000
	confirmCurrentQueryAccount(t, repository, "acct-test-a", nowMS)
	recordCurrentQueryLocal(t, repository, "freshness-local", nowMS, resetAtMS, 40, store.QuotaWindowPrimary, 100)
	response := queryCurrentAt(t, service, nowMS)
	if response.Binding.State != store.CodexAccountBindingConfirmed || len(response.Windows) != 0 {
		t.Fatalf("confirmed query projected local JSONL = %#v", response)
	}
	for _, source := range response.Sources {
		if source.Source == CurrentSourceLocal {
			t.Fatalf("confirmed sources still include local JSONL: %#v", response.Sources)
		}
	}
}

func TestCurrentQuerySuspiciousLocalJSONLIsNotProjectedForConfirmedAccount(t *testing.T) {
	t.Parallel()

	repository, service := newCurrentQueryTestService(t)
	nowMS := time.Now().UnixMilli()
	resetAtMS := nowMS + 5*60*60*1_000
	reason := store.QuotaReasonUnknownPlanType
	confirmCurrentQueryAccount(t, repository, "acct-test-a", nowMS)
	recordCurrentQueryLocalWithValidity(
		t, repository, "suspicious-only-local", nowMS, resetAtMS, 40,
		store.QuotaWindowPrimary, 100, store.QuotaValiditySuspicious, &reason,
	)
	response := queryCurrentAt(t, service, nowMS)
	if len(response.Windows) != 0 {
		t.Fatalf("suspicious local JSONL leaked into current windows = %#v", response)
	}
}

func TestCurrentQueryTrustedScenarioMatrix(t *testing.T) {
	t.Parallel()

	t.Run("local only stays off current account limits", func(t *testing.T) {
		repository, service := newCurrentQueryTestService(t)
		nowMS := time.Now().UnixMilli()
		resetAtMS := nowMS + 5*60*60*1_000
		confirmCurrentQueryAccount(t, repository, "acct-test-a", nowMS)
		recordCurrentQueryLocal(t, repository, "local-only", nowMS, resetAtMS, 38, store.QuotaWindowPrimary, 100)
		response := queryCurrentAt(t, service, nowMS)
		if len(response.Windows) != 0 || response.Binding.State != store.CodexAccountBindingConfirmed {
			t.Fatalf("local-only response = %#v", response)
		}
	})

	t.Run("app server only cross window and real zero reset credits", func(t *testing.T) {
		repository, service := newCurrentQueryTestService(t)
		nowMS := time.Now().UnixMilli()
		primaryReset := nowMS + 5*60*60*1_000
		secondaryReset := nowMS + 7*24*60*60*1_000
		recordCurrentQueryAppServer(
			t, repository, "app-server-only", nowMS, 40, 20, primaryReset, secondaryReset,
		)
		recordCurrentQueryResetCredits(t, repository, "zero-reset-credits", nowMS, nil)
		response := queryCurrentAt(t, service, nowMS)
		if len(response.Windows) != 2 || response.Windows[0].RemainingPercent == nil ||
			*response.Windows[0].RemainingPercent != 60 || response.Windows[1].RemainingPercent == nil ||
			*response.Windows[1].RemainingPercent != 80 || response.NextReset.AtMS == nil ||
			*response.NextReset.AtMS != primaryReset || response.NextReset.TrustedWindowCount != 2 ||
			response.ResetCredits.AvailableCount == nil || *response.ResetCredits.AvailableCount != 0 ||
			response.ResetCredits.UnknownReason != nil {
			t.Fatalf("app-server/cross-window response = %#v", response)
		}
	})

	t.Run("local JSONL does not mix into app server current", func(t *testing.T) {
		repository, service := newCurrentQueryTestService(t)
		nowMS := time.Now().UnixMilli()
		resetAtMS := nowMS + 5*60*60*1_000
		recordCurrentQueryAppServer(t, repository, "consistent-app-server", nowMS, 45, -1, resetAtMS, 0)
		recordCurrentQueryLocal(
			t, repository, "consistent-local", nowMS+1, resetAtMS, 45, store.QuotaWindowPrimary, 100,
		)
		response := queryCurrentAt(t, service, nowMS+2)
		if len(response.Windows) != 1 || response.Windows[0].Conflict != store.QuotaConflictNone ||
			len(response.Windows[0].Explanations) != 1 ||
			response.Windows[0].Explanations[0].Source != store.QuotaSourceAppServer ||
			response.Windows[0].RemainingPercent == nil || *response.Windows[0].RemainingPercent != 55 {
			t.Fatalf("app-server isolation response = %#v", response)
		}
	})

	t.Run("local JSONL conflict cannot replace app server remaining", func(t *testing.T) {
		repository, service := newCurrentQueryTestService(t)
		nowMS := time.Now().UnixMilli()
		resetAtMS := nowMS + 5*60*60*1_000
		recordCurrentQueryAppServer(t, repository, "conflict-app-server", nowMS, 41, -1, resetAtMS, 0)
		recordCurrentQueryLocal(
			t, repository, "conflict-local", nowMS+1, resetAtMS, 45, store.QuotaWindowPrimary, 100,
		)
		response := queryCurrentAt(t, service, nowMS+2)
		window := response.Windows[0]
		if window.Conflict != store.QuotaConflictNone || window.SelectedSource == nil ||
			*window.SelectedSource != store.QuotaSourceAppServer || window.RemainingPercent == nil ||
			*window.RemainingPercent != 59 || len(window.Explanations) != 1 {
			t.Fatalf("local conflict leaked into current = %#v", response)
		}
	})

	t.Run("expired keeps last known good but has no trusted next reset", func(t *testing.T) {
		repository, service := newCurrentQueryTestService(t)
		nowMS := time.Now().UnixMilli()
		resetAtMS := nowMS + 1_000
		recordCurrentQueryAppServer(t, repository, "expired-app-server", nowMS, 40, -1, resetAtMS, 0)
		response := queryCurrentAt(t, service, nowMS+2_000)
		window := response.Windows[0]
		if window.Freshness != store.QuotaCurrentExpiredUnknown || window.RemainingPercent == nil ||
			*window.RemainingPercent != 60 || window.ResetRemainingMS != nil || response.NextReset.AtMS != nil ||
			response.NextReset.UnknownReason == nil ||
			*response.NextReset.UnknownReason != CurrentUnknownNoTrustedReset {
			t.Fatalf("expired response = %#v", response)
		}
	})

	t.Run("rate limited keeps last known good and refresh fence", func(t *testing.T) {
		repository, service := newCurrentQueryTestService(t)
		nowMS := time.Now().UnixMilli()
		resetAtMS := nowMS + 5*60*60*1_000
		recordCurrentQueryAppServer(t, repository, "rate-limit-success", nowMS-1_000, 38, -1, resetAtMS, 0)
		retryAtMS := nowMS + 600_000
		recordCurrentQueryRateLimit(t, repository, "rate-limit-failure", nowMS, retryAtMS)
		scope, generation := currentQueryBindingAt(t, repository, nowMS)
		if _, err := repository.UpsertSourceRefreshSchedule(context.Background(), store.SourceRefreshScheduleUpdate{
			SourceInstanceID: store.QuotaSourceInstanceAppServer(scope),
			SourceType:       store.QuotaSourceTypeAppServerRateLimits, ScopeKey: scope,
			BindingGeneration: generation, NextDueAtMS: &retryAtMS,
			Reason: store.RefreshReasonRetryAfter, AtMS: nowMS,
		}); err != nil {
			t.Fatalf("UpsertSourceRefreshSchedule() error = %v", err)
		}
		response := queryCurrentAt(t, service, nowMS)
		if len(response.Windows) != 1 || response.Windows[0].RemainingPercent == nil ||
			*response.Windows[0].RemainingPercent != 62 || response.Windows[0].Freshness != store.QuotaCurrentStale ||
			response.Sources[0].FailureCode == nil ||
			*response.Sources[0].FailureCode != store.SourceFailureHTTP429 ||
			response.Refresh.Quota.State != CurrentRefreshScheduled || response.Refresh.Quota.NextDueAtMS == nil ||
			*response.Refresh.Quota.NextDueAtMS != retryAtMS || response.Refresh.Quota.Reason == nil ||
			*response.Refresh.Quota.Reason != store.RefreshReasonRetryAfter {
			t.Fatalf("429 response = %#v", response)
		}
	})

	t.Run("reset credits expire dynamically without becoming unknown", func(t *testing.T) {
		repository, service := newCurrentQueryTestService(t)
		nowMS := time.Now().UnixMilli()
		expiresAtMS := nowMS + 1_000
		recordCurrentQueryResetCredits(t, repository, "expiring-reset-credit", nowMS, &expiresAtMS)
		response := queryCurrentAt(t, service, nowMS+2_000)
		if response.ResetCredits.AvailableCount == nil || *response.ResetCredits.AvailableCount != 0 ||
			response.ResetCredits.CumulativeRemainingMS == nil ||
			*response.ResetCredits.CumulativeRemainingMS != 0 ||
			response.ResetCredits.NextExpiresAtMS != nil || response.ResetCredits.UnknownReason != nil {
			t.Fatalf("expired reset credits response = %#v", response.ResetCredits)
		}
		encoded, err := json.Marshal(response)
		if err != nil {
			t.Fatalf("json.Marshal() error = %v", err)
		}
		for _, forbidden := range []string{"raw-reset-credit-id", "/synthetic/", "requestId", "snapshotId"} {
			if strings.Contains(string(encoded), forbidden) {
				t.Fatalf("response leaks %q: %s", forbidden, encoded)
			}
		}
	})
}

type currentSnapshotReaderFunc func(context.Context, string, int64) (store.QuotaCurrentSnapshot, error)

type currentQueryEvidenceFixture struct {
	ObservationID string `gorm:"column:observation_id"`
}

func (currentQueryEvidenceFixture) TableName() string { return "quota_arbitration_evidence" }

func (reader currentSnapshotReaderFunc) QuotaCurrentSnapshot(
	ctx context.Context,
	accountScope string,
	evaluatedAtMS int64,
) (store.QuotaCurrentSnapshot, error) {
	return reader(ctx, accountScope, evaluatedAtMS)
}

func stringPointer(value string) *string { return &value }

func newCurrentQueryTestService(t *testing.T) (*store.Repository, *CurrentQueryService) {
	t.Helper()
	_, repository, service := newCurrentQueryTestRuntime(t)
	return repository, service
}

func newCurrentQueryTestRuntime(
	t *testing.T,
) (*storesqlite.Store, *store.Repository, *CurrentQueryService) {
	t.Helper()
	directory := t.TempDir()
	if err := os.Chmod(directory, 0o700); err != nil {
		t.Fatalf("Chmod(temp dir) error = %v", err)
	}
	database, repository, service := openCurrentQueryTestRuntime(
		t, filepath.Join(directory, "quota-current.db"),
	)
	t.Cleanup(func() {
		if err := database.Close(context.Background()); err != nil {
			t.Errorf("Close() error = %v", err)
		}
	})
	return database, repository, service
}

func openCurrentQueryTestRuntime(
	t *testing.T,
	databasePath string,
) (*storesqlite.Store, *store.Repository, *CurrentQueryService) {
	t.Helper()
	database, err := storesqlite.Open(context.Background(), storesqlite.Config{Path: databasePath})
	if err != nil {
		t.Fatalf("sqlite.Open() error = %v", err)
	}
	repository := store.NewRepository(database)
	if err := repository.EnsureApplicationSchema(context.Background()); err != nil {
		_ = database.Close(context.Background())
		t.Fatalf("EnsureApplicationSchema() error = %v", err)
	}
	service, err := NewCurrentQueryService(repository)
	if err != nil {
		_ = database.Close(context.Background())
		t.Fatalf("NewCurrentQueryService() error = %v", err)
	}
	return database, repository, service
}

func queryCurrentAt(t *testing.T, service *CurrentQueryService, evaluatedAtMS int64) CurrentResponse {
	t.Helper()
	response, err := service.Query(context.Background(), evaluatedAtMS)
	if err != nil {
		t.Fatalf("Query() error = %v", err)
	}
	return response
}

func confirmCurrentQueryAccount(
	t *testing.T,
	repository *store.Repository,
	accountID string,
	atMS int64,
) (string, int64) {
	t.Helper()
	var key [32]byte
	copy(key[:], bytes.Repeat([]byte{0x51}, 32))
	stored, err := repository.EnsureCodexAccountScopeKey(context.Background(), key, atMS)
	if err != nil {
		t.Fatalf("EnsureCodexAccountScopeKey() error = %v", err)
	}
	scope, err := accountbinding.DeriveScope(stored, []byte(accountID))
	if err != nil {
		t.Fatalf("DeriveScope(%s) error = %v", accountID, err)
	}
	binding, _, err := repository.ConfirmCodexAccountBinding(
		context.Background(), scope, atMS, store.CodexAccountBindingReasonStartup,
	)
	if err != nil || binding.AccountScope == nil {
		t.Fatalf("ConfirmCodexAccountBinding(%s) = %#v, %v", accountID, binding, err)
	}
	return *binding.AccountScope, binding.BindingGeneration
}

func currentQueryBindingAt(
	t *testing.T,
	repository *store.Repository,
	atMS int64,
) (string, int64) {
	t.Helper()
	binding, err := repository.CodexAccountBinding(context.Background())
	if err != nil {
		t.Fatalf("CodexAccountBinding() error = %v", err)
	}
	if binding.State == store.CodexAccountBindingConfirmed && binding.AccountScope != nil {
		return *binding.AccountScope, binding.BindingGeneration
	}
	return confirmCurrentQueryAccount(t, repository, "acct-test-a", atMS)
}

func recordCurrentQueryAppServer(
	t *testing.T,
	repository *store.Repository,
	requestID string,
	observedAtMS int64,
	primaryUsed float64,
	secondaryUsed float64,
	primaryResetAtMS int64,
	secondaryResetAtMS int64,
) {
	t.Helper()
	scope, generation := currentQueryBindingAt(t, repository, observedAtMS)
	limitID := "codex"
	request := requestID
	plan := "pro"
	digest := store.SHA256DigestOf([]byte("synthetic quota response " + requestID))
	observations := []store.QuotaObservationSample{{
		ObservationID: "query-" + requestID + "-primary", AccountScope: scope,
		Source: store.QuotaSourceAppServer, LimitID: &limitID, WindowKind: store.QuotaWindowPrimary,
		UsedPercent: primaryUsed, WindowMinutes: 300, ResetsAtMS: primaryResetAtMS,
		PlanType: &plan, ObservedAtMS: observedAtMS, Validity: store.QuotaValidityAccepted,
		RequestID: &request,
	}}
	if secondaryUsed >= 0 {
		observations = append(observations, store.QuotaObservationSample{
			ObservationID: "query-" + requestID + "-secondary", AccountScope: scope,
			Source: store.QuotaSourceAppServer, LimitID: &limitID, WindowKind: store.QuotaWindowSecondary,
			UsedPercent: secondaryUsed, WindowMinutes: 10_080, ResetsAtMS: secondaryResetAtMS,
			PlanType: &plan, ObservedAtMS: observedAtMS, Validity: store.QuotaValidityAccepted,
			RequestID: &request,
		})
	}
	if err := repository.RecordQuotaFetch(context.Background(), store.QuotaFetchRecord{
		AccountScope: scope, BindingGeneration: generation,
		SourceInstanceID: store.QuotaSourceInstanceAppServer(scope),
		SourceType:       store.QuotaSourceTypeAppServerRateLimits, ScopeKey: scope,
		Attempt: store.SourceAttempt{
			RequestID: requestID, SourceInstanceID: store.QuotaSourceInstanceAppServer(scope),
			StartedAtMS: observedAtMS, FinishedAtMS: observedAtMS,
			Outcome: store.SourceAttemptSucceeded, PayloadSHA256: &digest,
			AttemptCount: 1, ResponseBytes: 256,
		},
		Observations: observations,
	}); err != nil {
		t.Fatalf("RecordQuotaFetch(%s) error = %v", requestID, err)
	}
}

func recordCurrentQueryWham(
	t *testing.T,
	repository *store.Repository,
	requestID string,
	observedAtMS int64,
	primaryUsed float64,
	secondaryUsed float64,
	primaryResetAtMS int64,
	secondaryResetAtMS int64,
) {
	t.Helper()
	limitID := "codex"
	request := requestID
	plan := "pro"
	status := int64(200)
	digest := store.SHA256DigestOf([]byte("synthetic quota response " + requestID))
	observations := []store.QuotaObservationSample{{
		ObservationID: "query-" + requestID + "-primary", AccountScope: store.QuotaAccountScopeDefault,
		Source: store.QuotaSourceWham, LimitID: &limitID, WindowKind: store.QuotaWindowPrimary,
		UsedPercent: primaryUsed, WindowMinutes: 300, ResetsAtMS: primaryResetAtMS,
		PlanType: &plan, ObservedAtMS: observedAtMS, Validity: store.QuotaValidityAccepted,
		RequestID: &request,
	}}
	if secondaryUsed >= 0 {
		observations = append(observations, store.QuotaObservationSample{
			ObservationID: "query-" + requestID + "-secondary", AccountScope: store.QuotaAccountScopeDefault,
			Source: store.QuotaSourceWham, LimitID: &limitID, WindowKind: store.QuotaWindowSecondary,
			UsedPercent: secondaryUsed, WindowMinutes: 10_080, ResetsAtMS: secondaryResetAtMS,
			PlanType: &plan, ObservedAtMS: observedAtMS, Validity: store.QuotaValidityAccepted,
			RequestID: &request,
		})
	}
	if err := repository.RecordQuotaFetch(context.Background(), store.QuotaFetchRecord{
		SourceInstanceID: store.QuotaSourceInstanceWhamDefault, SourceType: store.QuotaSourceTypeWham,
		ScopeKey: store.QuotaAccountScopeDefault,
		Attempt: store.SourceAttempt{
			RequestID: requestID, SourceInstanceID: store.QuotaSourceInstanceWhamDefault,
			StartedAtMS: observedAtMS, FinishedAtMS: observedAtMS,
			Outcome: store.SourceAttemptSucceeded, HTTPStatus: &status, PayloadSHA256: &digest,
			AttemptCount: 1, ResponseBytes: 256,
		},
		Observations: observations,
	}); err != nil {
		t.Fatalf("RecordQuotaFetch(%s) error = %v", requestID, err)
	}
}

func recordCurrentQueryLocal(
	t *testing.T,
	repository *store.Repository,
	identity string,
	observedAtMS int64,
	resetAtMS int64,
	used float64,
	windowKind store.QuotaWindowKind,
	offset int64,
) {
	t.Helper()
	recordCurrentQueryLocalWithValidity(
		t, repository, identity, observedAtMS, resetAtMS, used, windowKind, offset,
		store.QuotaValidityAccepted, nil,
	)
}

func recordCurrentQueryLocalWithValidity(
	t *testing.T,
	repository *store.Repository,
	identity string,
	observedAtMS int64,
	resetAtMS int64,
	used float64,
	windowKind store.QuotaWindowKind,
	offset int64,
	validity store.QuotaValidity,
	rejectionReason *store.QuotaRejectionReason,
) {
	t.Helper()
	sessionID := "query-session-" + identity
	if err := repository.UpsertFacts(context.Background(), store.FactBatch{Session: &store.Session{
		SessionID: sessionID, Provider: "codex", SourceKind: "session",
		CreatedAtMS: observedAtMS, FirstSeenAtMS: observedAtMS, LastSeenAtMS: observedAtMS,
	}}); err != nil {
		t.Fatalf("UpsertFacts(session) error = %v", err)
	}
	sourceFileID := "query-source-" + identity
	lastScannedAtMS := observedAtMS
	if err := repository.UpsertSourceFile(context.Background(), store.SourceFile{
		SourceFileID: sourceFileID, Provider: "codex", SessionID: &sessionID,
		CurrentPath: "/synthetic/" + identity + ".jsonl", DeviceID: "synthetic-device-" + identity,
		Inode: 1, SizeBytes: 1_000, MTimeNS: observedAtMS * 1_000_000, ParsedOffset: 1_000,
		ParserVersion: "codex-rollout-v1", ActiveGeneration: 1, State: store.SourceFileActive,
		LastScannedAtMS: &lastScannedAtMS, UpdatedAtMS: observedAtMS,
	}); err != nil {
		t.Fatalf("UpsertSourceFile() error = %v", err)
	}
	limitID := "codex"
	if err := repository.UpsertFacts(context.Background(), store.FactBatch{
		QuotaObservation: &store.QuotaObservationSample{
			ObservationID: "query-" + identity + "-observation",
			AccountScope:  store.QuotaAccountScopeDefault, Source: store.QuotaSourceLocalJSONL,
			LimitID: &limitID, WindowKind: windowKind, UsedPercent: used,
			WindowMinutes: currentQueryWindowMinutes(windowKind), ResetsAtMS: resetAtMS,
			ObservedAtMS: observedAtMS, Validity: validity, RejectionReason: rejectionReason,
			SessionID: &sessionID, SourceFileID: &sourceFileID,
			SourceGeneration: 1, SourceOffset: offset,
		},
	}); err != nil {
		t.Fatalf("UpsertFacts(quota) error = %v", err)
	}
}

func recordCurrentQueryRateLimit(
	t *testing.T,
	repository *store.Repository,
	requestID string,
	finishedAtMS int64,
	retryAtMS int64,
) {
	t.Helper()
	scope, generation := currentQueryBindingAt(t, repository, finishedAtMS)
	status := int64(429)
	errorClass := store.RuntimeErrorUnavailable
	failure := store.SourceFailureHTTP429
	if err := repository.RecordQuotaFetch(context.Background(), store.QuotaFetchRecord{
		AccountScope: scope, BindingGeneration: generation,
		SourceInstanceID: store.QuotaSourceInstanceAppServer(scope),
		SourceType:       store.QuotaSourceTypeAppServerRateLimits, ScopeKey: scope,
		Attempt: store.SourceAttempt{
			RequestID: requestID, SourceInstanceID: store.QuotaSourceInstanceAppServer(scope),
			StartedAtMS: finishedAtMS, FinishedAtMS: finishedAtMS,
			Outcome: store.SourceAttemptFailed, HTTPStatus: &status,
			ErrorClass: &errorClass, FailureCode: &failure, RetryAtMS: &retryAtMS,
			AttemptCount: 1,
		},
	}); err != nil {
		t.Fatalf("RecordQuotaFetch(429) error = %v", err)
	}
}

func recordCurrentQueryResetCredits(
	t *testing.T,
	repository *store.Repository,
	requestID string,
	observedAtMS int64,
	expiresAtMS *int64,
) {
	t.Helper()
	status := int64(200)
	scope, generation := currentQueryBindingAt(t, repository, observedAtMS)
	snapshot := &store.ResetCreditsSnapshot{
		SnapshotID: "query-reset-" + requestID, RequestID: requestID,
		AccountScope: scope, ObservedAtMS: observedAtMS,
	}
	if expiresAtMS != nil {
		snapshot.AvailableCount = 1
		snapshot.Credits = []store.ResetCredit{{
			CreditIDHash: store.SHA256DigestOf([]byte("raw-reset-credit-id")),
			Status:       store.ResetCreditAvailable, Type: store.ResetCreditTypeCodexRateLimits,
			GrantedAtMS: observedAtMS, ExpiresAtMS: expiresAtMS,
		}}
	}
	if err := repository.RecordResetCreditsFetch(context.Background(), store.ResetCreditsFetchRecord{
		AccountScope: scope, BindingGeneration: generation,
		SourceInstanceID: store.ResetCreditsSourceInstanceAppServer(scope),
		SourceType:       store.ResetCreditsSourceTypeAppServer, ScopeKey: scope,
		Attempt: store.SourceAttempt{
			RequestID: requestID, SourceInstanceID: store.ResetCreditsSourceInstanceAppServer(scope),
			StartedAtMS: observedAtMS, FinishedAtMS: observedAtMS,
			Outcome: store.SourceAttemptSucceeded, HTTPStatus: &status, AttemptCount: 1,
		},
		Snapshot: snapshot,
	}); err != nil {
		t.Fatalf("RecordResetCreditsFetch() error = %v", err)
	}
}

func currentQueryWindowMinutes(kind store.QuotaWindowKind) int64 {
	if kind == store.QuotaWindowSecondary {
		return 10_080
	}
	return 300
}
