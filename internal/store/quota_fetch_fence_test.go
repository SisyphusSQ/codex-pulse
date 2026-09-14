package store

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/SisyphusSQ/codex-pulse/internal/codex/accountbinding"
)

func TestRecordQuotaFetchRejectsStaleAccountFence(t *testing.T) {
	t.Parallel()

	repository := openRuntimeRepository(t)
	key, scopeA, generationA := confirmSyntheticCodexAccount(t, repository, "acct-test-a", 1_784_000_000_000)
	record := appServerQuotaFetchRecord("late-reply-a", scopeA, generationA, 1_784_000_000_100)
	_, generationB := confirmSyntheticCodexAccountWithKey(
		t, repository, key, "acct-test-b", 1_784_000_000_200,
	)
	if generationB <= generationA {
		t.Fatalf("B generation = %d, want greater than A %d", generationB, generationA)
	}

	err := repository.RecordQuotaFetch(context.Background(), record)
	if !errors.Is(err, ErrCodexAccountBindingChanged) {
		t.Fatalf("RecordQuotaFetch(stale A) error = %v, want ErrCodexAccountBindingChanged", err)
	}
	assertOnlineFetchDidNotPersist(t, repository, record.SourceInstanceID, record.Observations)
}

func TestRecordQuotaFetchDoesNotCopyDefaultObservationsOntoAccountScope(t *testing.T) {
	t.Parallel()

	repository := openRuntimeRepository(t)
	record := successfulQuotaFetchRecord("default-to-account", 1_784_000_000_000, 41, 9)
	scope := stringsRepeatHex("ab")
	for index := range record.Observations {
		record.Observations[index].AccountScope = scope
		record.Observations[index].Source = QuotaSourceAppServer
	}
	if err := repository.RecordQuotaFetch(context.Background(), record); !errors.Is(err, ErrInvalidRecord) {
		t.Fatalf("RecordQuotaFetch(default record + account observations) error = %v, want ErrInvalidRecord", err)
	}
	assertOnlineFetchDidNotPersist(t, repository, record.SourceInstanceID, record.Observations)
}

func TestRecordResetCreditsFetchRejectsStaleAccountFence(t *testing.T) {
	t.Parallel()

	repository := openRuntimeRepository(t)
	key, scopeA, generationA := confirmSyntheticCodexAccount(t, repository, "acct-test-a", 1_784_000_000_000)
	record := appServerResetCreditsFetchRecord("late-reset-a", scopeA, generationA, 1_784_000_000_100)
	confirmSyntheticCodexAccountWithKey(t, repository, key, "acct-test-b", 1_784_000_000_200)

	err := repository.RecordResetCreditsFetch(context.Background(), record)
	if !errors.Is(err, ErrCodexAccountBindingChanged) {
		t.Fatalf("RecordResetCreditsFetch(stale A) error = %v, want ErrCodexAccountBindingChanged", err)
	}
	if _, err := repository.SourceState(context.Background(), record.SourceInstanceID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("SourceState(after stale reset) error = %v, want ErrNotFound", err)
	}
	if attempts, err := repository.ListSourceAttempts(context.Background(), record.SourceInstanceID, 10); err != nil || len(attempts) != 0 {
		t.Fatalf("ListSourceAttempts(after stale reset) = %#v, %v", attempts, err)
	}
	summary, err := repository.ResetCreditsSummary(context.Background(), scopeA, 1_784_000_000_100)
	if err != nil || summary.AvailableCount != nil || summary.SnapshotID != nil {
		t.Fatalf("ResetCreditsSummary(A after stale) = %#v, %v", summary, err)
	}
}

func confirmSyntheticCodexAccount(
	t testing.TB,
	repository *Repository,
	accountID string,
	atMS int64,
) ([32]byte, string, int64) {
	t.Helper()
	var key [32]byte
	copy(key[:], bytes.Repeat([]byte{0x51}, 32))
	scope, generation := confirmSyntheticCodexAccountWithKey(t, repository, key, accountID, atMS)
	return key, scope, generation
}

func confirmSyntheticCodexAccountWithKey(
	t testing.TB,
	repository *Repository,
	key [32]byte,
	accountID string,
	atMS int64,
) (string, int64) {
	t.Helper()
	stored, err := repository.EnsureCodexAccountScopeKey(context.Background(), key, atMS)
	if err != nil {
		t.Fatalf("EnsureCodexAccountScopeKey() error = %v", err)
	}
	scope, err := accountbinding.DeriveScope(stored, []byte(accountID))
	if err != nil {
		t.Fatalf("DeriveScope(%s) error = %v", accountID, err)
	}
	binding, _, err := repository.ConfirmCodexAccountBinding(
		context.Background(), scope, atMS, CodexAccountBindingReasonStartup,
	)
	if err != nil || binding.AccountScope == nil {
		t.Fatalf("ConfirmCodexAccountBinding(%s) = %#v, %v", accountID, binding, err)
	}
	return *binding.AccountScope, binding.BindingGeneration
}

func appServerQuotaFetchRecord(requestID, scope string, generation, finishedAtMS int64) QuotaFetchRecord {
	return appServerQuotaFetchRecordWithUsage(requestID, scope, generation, finishedAtMS, 41, 9)
}

func appServerQuotaFetchRecordWithUsage(
	requestID, scope string, generation, finishedAtMS int64, primary, secondary float64,
) QuotaFetchRecord {
	record := successfulQuotaFetchRecord(requestID, finishedAtMS, primary, secondary)
	record.AccountScope = scope
	record.BindingGeneration = generation
	record.SourceInstanceID = QuotaSourceInstanceAppServer(scope)
	record.SourceType = QuotaSourceTypeAppServerRateLimits
	record.ScopeKey = scope
	record.Attempt.SourceInstanceID = record.SourceInstanceID
	record.Attempt.HTTPStatus = nil
	for index := range record.Observations {
		record.Observations[index].Source = QuotaSourceAppServer
		record.Observations[index].AccountScope = scope
	}
	return record
}

func appServerResetCreditsFetchRecord(requestID, scope string, generation, observedAtMS int64) ResetCreditsFetchRecord {
	status := int64(200)
	return ResetCreditsFetchRecord{
		AccountScope: scope, BindingGeneration: generation,
		SourceInstanceID: ResetCreditsSourceInstanceAppServer(scope),
		SourceType:       ResetCreditsSourceTypeAppServer, ScopeKey: scope,
		Attempt: SourceAttempt{
			RequestID: requestID, SourceInstanceID: ResetCreditsSourceInstanceAppServer(scope),
			StartedAtMS: observedAtMS, FinishedAtMS: observedAtMS, Outcome: SourceAttemptSucceeded,
			HTTPStatus: &status, AttemptCount: 1, ResponseBytes: 80,
		},
		Snapshot: &ResetCreditsSnapshot{
			SnapshotID: requestID + "-snapshot", RequestID: requestID,
			AccountScope: scope, AvailableCount: 1, ObservedAtMS: observedAtMS,
			Credits: []ResetCredit{{
				CreditIDHash: SHA256DigestOf([]byte(requestID + "-credit")),
				Status:       ResetCreditAvailable,
				Type:         ResetCreditTypeCodexRateLimits,
				GrantedAtMS:  observedAtMS - 3_600_000,
			}},
		},
	}
}

func assertOnlineFetchDidNotPersist(
	t testing.TB,
	repository *Repository,
	sourceInstanceID string,
	observations []QuotaObservationSample,
) {
	t.Helper()
	if _, err := repository.SourceState(context.Background(), sourceInstanceID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("SourceState(after rejected fetch) error = %v, want ErrNotFound", err)
	}
	if attempts, err := repository.ListSourceAttempts(context.Background(), sourceInstanceID, 10); err != nil || len(attempts) != 0 {
		t.Fatalf("ListSourceAttempts(after rejected fetch) = %#v, %v", attempts, err)
	}
	for _, observation := range observations {
		if _, err := repository.QuotaObservation(context.Background(), observation.ObservationID); !errors.Is(err, ErrNotFound) {
			t.Fatalf("QuotaObservation(%s) error = %v, want ErrNotFound", observation.ObservationID, err)
		}
	}
}

func stringsRepeatHex(unit string) string {
	return string(bytes.Repeat([]byte(unit), 32))
}
