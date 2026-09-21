package store

import (
	"errors"
	"testing"

	"github.com/SisyphusSQ/codex-pulse/internal/codex/accountbinding"
)

func TestConfirmCodexAccountBindingAppliesQuotaRetentionOnlyAfterConfirmedSwitch(t *testing.T) {
	for _, testCase := range []struct {
		name                string
		retainPreviousQuota bool
		wantPreviousWindows bool
	}{
		{name: "retain", retainPreviousQuota: true, wantPreviousWindows: true},
		{name: "purge", retainPreviousQuota: false, wantPreviousWindows: false},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			repository := openAccountBindingRepository(t)
			key, scopeA, generationA := confirmSyntheticCodexAccount(
				t, repository, "acct-retention-a", 1_784_000_000_000,
			)
			quotaA := appServerQuotaFetchRecord(
				"retention-a", scopeA, generationA, 1_784_000_000_100,
			)
			if err := repository.RecordQuotaFetch(t.Context(), quotaA); err != nil {
				t.Fatalf("RecordQuotaFetch(A) error = %v", err)
			}
			resetA := appServerResetCreditsFetchRecord(
				"retention-reset-a", scopeA, generationA, 1_784_000_000_100,
			)
			if err := repository.RecordResetCreditsFetch(t.Context(), resetA); err != nil {
				t.Fatalf("RecordResetCreditsFetch(A) error = %v", err)
			}

			if _, err := repository.MarkCodexAccountBindingPending(
				t.Context(), 1_784_000_000_200, CodexAccountBindingReasonAccountChanged,
			); err != nil {
				t.Fatalf("MarkCodexAccountBindingPending() error = %v", err)
			}
			if _, err := repository.QuotaCurrent(
				t.Context(), scopeA, QuotaWindowPrimary, "codex", 1_784_000_000_200,
			); err != nil {
				t.Fatalf("pending transition changed A quota facts: %v", err)
			}
			scopeB, _ := confirmSyntheticCodexAccountWithKey(
				t, repository, key, "acct-retention-b", 1_784_000_000_300,
			)
			// Re-run the same confirmed transition through the retention-aware entrypoint.
			// The helper above confirms B with retention enabled, so restore A as the last
			// confirmed account before exercising the tested A -> B transition.
			if _, _, err := repository.ConfirmCodexAccountBinding(
				t.Context(), scopeA, 1_784_000_000_400, CodexAccountBindingReasonStartup,
			); err != nil {
				t.Fatalf("ConfirmCodexAccountBinding(A restore) error = %v", err)
			}
			if _, err := repository.MarkCodexAccountBindingPending(
				t.Context(), 1_784_000_000_500, CodexAccountBindingReasonAccountChanged,
			); err != nil {
				t.Fatalf("MarkCodexAccountBindingPending(second) error = %v", err)
			}
			if _, _, err := repository.ConfirmCodexAccountBindingWithRetention(
				t.Context(), scopeB, 1_784_000_000_600,
				CodexAccountBindingReasonAccountChanged, testCase.retainPreviousQuota,
			); err != nil {
				t.Fatalf("ConfirmCodexAccountBindingWithRetention(B) error = %v", err)
			}

			_, err := repository.QuotaCurrent(
				t.Context(), scopeA, QuotaWindowPrimary, "codex", 1_784_000_000_600,
			)
			if testCase.wantPreviousWindows && err != nil {
				t.Fatalf("QuotaCurrent(A retained) error = %v", err)
			}
			if !testCase.wantPreviousWindows && !errors.Is(err, ErrNotFound) {
				t.Fatalf("QuotaCurrent(A purged) error = %v, want ErrNotFound", err)
			}
		})
	}
}

func TestConfirmCodexAccountBindingDoesNotPurgeQuotaDuringStartupRecovery(t *testing.T) {
	repository := openAccountBindingRepository(t)
	key, scopeA, generationA := confirmSyntheticCodexAccount(
		t, repository, "acct-startup-a", 1_784_050_000_000,
	)
	if err := repository.RecordQuotaFetch(t.Context(), appServerQuotaFetchRecord(
		"startup-a", scopeA, generationA, 1_784_050_000_100,
	)); err != nil {
		t.Fatalf("RecordQuotaFetch(A) error = %v", err)
	}
	scopeB, err := accountbinding.DeriveScope(key, []byte("acct-startup-b"))
	if err != nil {
		t.Fatalf("deriveCodexAccountScope(B) error = %v", err)
	}
	if _, _, err := repository.ConfirmCodexAccountBindingWithRetention(
		t.Context(), scopeB, 1_784_050_000_200,
		CodexAccountBindingReasonStartup, false,
	); err != nil {
		t.Fatalf("ConfirmCodexAccountBindingWithRetention(B startup) error = %v", err)
	}
	if _, err := repository.QuotaCurrent(
		t.Context(), scopeA, QuotaWindowPrimary, "codex", 1_784_050_000_300,
	); err != nil {
		t.Fatalf("startup recovery purged A quota facts: %v", err)
	}
}

func TestClearHistoricalCodexAccountQuotaPreservesLastConfirmedFactsWhilePending(t *testing.T) {
	repository := openAccountBindingRepository(t)
	_, scopeA, generationA := confirmSyntheticCodexAccount(
		t, repository, "acct-clear-pending-a", 1_784_075_000_000,
	)
	if err := repository.RecordQuotaFetch(t.Context(), appServerQuotaFetchRecord(
		"clear-pending-a", scopeA, generationA, 1_784_075_000_100,
	)); err != nil {
		t.Fatalf("RecordQuotaFetch(A) error = %v", err)
	}
	if _, err := repository.MarkCodexAccountBindingPending(
		t.Context(), 1_784_075_000_200, CodexAccountBindingReasonAccountChanged,
	); err != nil {
		t.Fatalf("MarkCodexAccountBindingPending() error = %v", err)
	}
	receipt, err := repository.ClearHistoricalCodexAccountQuota(t.Context())
	if err != nil {
		t.Fatalf("ClearHistoricalCodexAccountQuota() error = %v", err)
	}
	if receipt != (CodexAccountQuotaPurgeResult{}) {
		t.Fatalf("clear receipt = %#v, want no purge while binding is pending", receipt)
	}
	if _, err := repository.QuotaCurrent(
		t.Context(), scopeA, QuotaWindowPrimary, "codex", 1_784_075_000_300,
	); err != nil {
		t.Fatalf("clear while pending removed last confirmed facts: %v", err)
	}
}

func TestClearHistoricalCodexAccountQuotaPreservesCurrentFacts(t *testing.T) {
	repository := openAccountBindingRepository(t)
	key, scopeA, generationA := confirmSyntheticCodexAccount(
		t, repository, "acct-clear-a", 1_784_100_000_000,
	)
	if err := repository.RecordQuotaFetch(t.Context(), appServerQuotaFetchRecord(
		"clear-a", scopeA, generationA, 1_784_100_000_100,
	)); err != nil {
		t.Fatalf("RecordQuotaFetch(A) error = %v", err)
	}
	if _, err := repository.MarkCodexAccountBindingPending(
		t.Context(), 1_784_100_000_200, CodexAccountBindingReasonAccountChanged,
	); err != nil {
		t.Fatalf("MarkCodexAccountBindingPending() error = %v", err)
	}
	scopeB, generationB := confirmSyntheticCodexAccountWithKey(
		t, repository, key, "acct-clear-b", 1_784_100_000_300,
	)
	if err := repository.RecordQuotaFetch(t.Context(), appServerQuotaFetchRecord(
		"clear-b", scopeB, generationB, 1_784_100_000_400,
	)); err != nil {
		t.Fatalf("RecordQuotaFetch(B) error = %v", err)
	}

	receipt, err := repository.ClearHistoricalCodexAccountQuota(t.Context())
	if err != nil {
		t.Fatalf("ClearHistoricalCodexAccountQuota() error = %v", err)
	}
	if receipt.AccountCount != 1 || receipt.WindowCount != 2 || receipt.ObservationCount != 2 {
		t.Fatalf("clear receipt = %#v", receipt)
	}
	if _, err := repository.QuotaCurrent(
		t.Context(), scopeA, QuotaWindowPrimary, "codex", 1_784_100_000_500,
	); !errors.Is(err, ErrNotFound) {
		t.Fatalf("QuotaCurrent(A) error = %v, want ErrNotFound", err)
	}
	if _, err := repository.QuotaCurrent(
		t.Context(), scopeB, QuotaWindowPrimary, "codex", 1_784_100_000_500,
	); err != nil {
		t.Fatalf("QuotaCurrent(B) error = %v", err)
	}
}
