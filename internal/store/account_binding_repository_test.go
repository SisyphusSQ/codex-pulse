package store

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"gorm.io/gorm"

	"github.com/SisyphusSQ/codex-pulse/internal/codex/accountbinding"
)

func TestCodexAccountBindingABAStateMachine(t *testing.T) {
	t.Parallel()

	repository := openAccountBindingRepository(t)
	var candidate [32]byte
	copy(candidate[:], bytes.Repeat([]byte{0x44}, 32))
	key, err := repository.EnsureCodexAccountScopeKey(t.Context(), candidate, 1_784_000_000_000)
	if err != nil {
		t.Fatalf("EnsureCodexAccountScopeKey() error = %v", err)
	}
	scopeA, err := accountbinding.DeriveScope(key, []byte("acct-test-a"))
	if err != nil {
		t.Fatalf("DeriveScope(A) error = %v", err)
	}
	scopeB, err := accountbinding.DeriveScope(key, []byte("acct-test-b"))
	if err != nil {
		t.Fatalf("DeriveScope(B) error = %v", err)
	}

	first, changed, err := repository.ConfirmCodexAccountBinding(
		t.Context(), scopeA, 1_784_000_000_100, CodexAccountBindingReasonStartup,
	)
	if err != nil || !changed {
		t.Fatalf("Confirm(A first) = %#v changed=%v err=%v", first, changed, err)
	}
	assertCodexAccountBinding(t, first, CodexAccountBindingConfirmed, &scopeA, 1)

	same, changed, err := repository.ConfirmCodexAccountBinding(
		t.Context(), scopeA, 1_784_000_000_200, CodexAccountBindingReasonStable,
	)
	if err != nil || changed {
		t.Fatalf("Confirm(A refresh) = %#v changed=%v err=%v", same, changed, err)
	}
	assertCodexAccountBinding(t, same, CodexAccountBindingConfirmed, &scopeA, 1)

	pending, err := repository.MarkCodexAccountBindingPending(
		t.Context(), 1_784_000_000_300, CodexAccountBindingReasonAccountChanged,
	)
	if err != nil {
		t.Fatalf("MarkPending() error = %v", err)
	}
	assertCodexAccountBinding(t, pending, CodexAccountBindingPending, nil, 2)

	confirmedB, changed, err := repository.ConfirmCodexAccountBinding(
		t.Context(), scopeB, 1_784_000_000_400, CodexAccountBindingReasonAccountChanged,
	)
	if err != nil || !changed {
		t.Fatalf("Confirm(B) = %#v changed=%v err=%v", confirmedB, changed, err)
	}
	assertCodexAccountBinding(t, confirmedB, CodexAccountBindingConfirmed, &scopeB, 2)

	signedOut, changed, err := repository.SetCodexAccountBindingUnavailable(
		t.Context(), CodexAccountBindingSignedOut, 1_784_000_000_500, CodexAccountBindingReasonSignedOut,
	)
	if err != nil || !changed {
		t.Fatalf("SetUnavailable(signed_out) = %#v changed=%v err=%v", signedOut, changed, err)
	}
	assertCodexAccountBinding(t, signedOut, CodexAccountBindingSignedOut, nil, 3)

	restored, changed, err := repository.ConfirmCodexAccountBinding(
		t.Context(), scopeA, 1_784_000_000_600, CodexAccountBindingReasonStartup,
	)
	if err != nil || !changed {
		t.Fatalf("Confirm(A restore) = %#v changed=%v err=%v", restored, changed, err)
	}
	assertCodexAccountBinding(t, restored, CodexAccountBindingConfirmed, &scopeA, 3)

	readback, err := repository.CodexAccountBinding(t.Context())
	if err != nil {
		t.Fatalf("CodexAccountBinding() error = %v", err)
	}
	assertCodexAccountBinding(t, readback, CodexAccountBindingConfirmed, &scopeA, 3)
	if count := scalarCount(t, repository.database, `SELECT COUNT(*) FROM codex_account_scopes`); count != 2 {
		t.Fatalf("scope rows = %d, want 2", count)
	}
	if lastConfirmed := scalarText(t, repository.database, `SELECT last_confirmed_scope FROM codex_account_binding`); lastConfirmed != scopeA {
		t.Fatalf("last_confirmed_scope = %q, want restored A", lastConfirmed)
	}
}

func TestCodexAccountBindingEnsureScopeKeyIsSingletonUnderConcurrency(t *testing.T) {
	t.Parallel()

	repository := openAccountBindingRepository(t)
	results := make([][32]byte, 8)
	errs := make([]error, 8)
	var started sync.WaitGroup
	var finished sync.WaitGroup
	started.Add(8)
	finished.Add(8)
	for index := 0; index < 8; index++ {
		go func(index int) {
			defer finished.Done()
			var candidate [32]byte
			candidate[0] = byte(index + 1)
			started.Done()
			started.Wait()
			results[index], errs[index] = repository.EnsureCodexAccountScopeKey(
				context.Background(), candidate, 1_784_000_001_000+int64(index),
			)
		}(index)
	}
	finished.Wait()

	var kept [32]byte
	for index, err := range errs {
		if err != nil {
			t.Fatalf("EnsureCodexAccountScopeKey(%d) error = %v", index, err)
		}
		if index == 0 {
			kept = results[index]
			continue
		}
		if results[index] != kept {
			t.Fatalf("concurrent keys diverged: first=%x got=%x", kept, results[index])
		}
	}
	if count := scalarCount(t, repository.database, `SELECT COUNT(*) FROM codex_account_scope_key`); count != 1 {
		t.Fatalf("scope key rows = %d, want 1", count)
	}
}

func TestCodexAccountBindingWriterFenceRejectsStaleGeneration(t *testing.T) {
	t.Parallel()

	repository := openAccountBindingRepository(t)
	var candidate [32]byte
	copy(candidate[:], bytes.Repeat([]byte{0x55}, 32))
	key, err := repository.EnsureCodexAccountScopeKey(t.Context(), candidate, 1_784_000_002_000)
	if err != nil {
		t.Fatalf("EnsureCodexAccountScopeKey() error = %v", err)
	}
	scopeA, err := accountbinding.DeriveScope(key, []byte("acct-test-a"))
	if err != nil {
		t.Fatalf("DeriveScope(A) error = %v", err)
	}
	if _, _, err := repository.ConfirmCodexAccountBinding(
		t.Context(), scopeA, 1_784_000_002_100, CodexAccountBindingReasonStartup,
	); err != nil {
		t.Fatalf("Confirm(A) error = %v", err)
	}
	if err := repository.database.Write(t.Context(), func(ctx context.Context, transaction *gorm.DB) error {
		return requireCodexAccountFence(ctx, transaction, scopeA, 1)
	}); err != nil {
		t.Fatalf("requireCodexAccountFence(current) error = %v", err)
	}
	if _, err := repository.MarkCodexAccountBindingPending(
		t.Context(), 1_784_000_002_200, CodexAccountBindingReasonAccountChanged,
	); err != nil {
		t.Fatalf("MarkPending() error = %v", err)
	}
	err = repository.database.Write(t.Context(), func(ctx context.Context, transaction *gorm.DB) error {
		return requireCodexAccountFence(ctx, transaction, scopeA, 1)
	})
	if !errors.Is(err, ErrCodexAccountBindingChanged) {
		t.Fatalf("stale fence error = %v, want ErrCodexAccountBindingChanged", err)
	}
	if strings.Contains(err.Error(), scopeA) || strings.Contains(err.Error(), "acct-test-a") {
		t.Fatalf("fence error leaked scope: %v", err)
	}
}

func TestCodexAccountBindingRejectsLegacyDefaultScope(t *testing.T) {
	t.Parallel()

	repository := openAccountBindingRepository(t)
	_, changed, err := repository.ConfirmCodexAccountBinding(
		t.Context(), QuotaAccountScopeDefault, 1_784_000_003_000, CodexAccountBindingReasonStartup,
	)
	if changed || !errors.Is(err, ErrInvalidRecord) {
		t.Fatalf("Confirm(default) changed=%v err=%v", changed, err)
	}
	if strings.Contains(err.Error(), "acct-") {
		t.Fatalf("error leaked synthetic account id: %v", err)
	}
}

func openAccountBindingRepository(t *testing.T) *Repository {
	t.Helper()
	repository := NewRepository(openTestDatabase(t))
	if err := repository.EnsureApplicationSchema(t.Context()); err != nil {
		t.Fatalf("EnsureApplicationSchema() error = %v", err)
	}
	return repository
}

func assertCodexAccountBinding(
	t *testing.T,
	got CodexAccountBinding,
	state CodexAccountBindingState,
	scope *string,
	generation int64,
) {
	t.Helper()
	if got.State != state || got.BindingGeneration != generation {
		t.Fatalf("binding = %#v, want state=%s generation=%d", got, state, generation)
	}
	if scope == nil {
		if got.AccountScope != nil {
			t.Fatalf("binding scope = %q, want empty", *got.AccountScope)
		}
		return
	}
	if got.AccountScope == nil || *got.AccountScope != *scope {
		t.Fatalf("binding scope = %#v, want %s", got.AccountScope, *scope)
	}
}
