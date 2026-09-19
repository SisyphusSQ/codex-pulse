package app

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/SisyphusSQ/codex-pulse/internal/codex/accountbinding"
	"github.com/SisyphusSQ/codex-pulse/internal/codex/subscriptionaccounts"
	"github.com/SisyphusSQ/codex-pulse/internal/store"
	storesqlite "github.com/SisyphusSQ/codex-pulse/internal/store/sqlite"
)

func TestCodexSubscriptionRuntimeAttachesLegacyQuotaHistoryStatus(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()
	if err := os.Chmod(directory, 0o700); err != nil {
		t.Fatalf("Chmod(temp dir) error = %v", err)
	}
	database, err := storesqlite.Open(context.Background(), storesqlite.Config{
		Path: filepath.Join(directory, "subscription-runtime.db"),
	})
	if err != nil {
		t.Fatalf("sqlite.Open() error = %v", err)
	}
	t.Cleanup(func() {
		if closeErr := database.Close(context.Background()); closeErr != nil {
			t.Errorf("Close() error = %v", closeErr)
		}
	})
	repository := store.NewRepository(database)
	if err := repository.EnsureApplicationSchema(context.Background()); err != nil {
		t.Fatalf("EnsureApplicationSchema() error = %v", err)
	}
	const nowMS = int64(1_784_500_000_000)
	var key [32]byte
	copy(key[:], bytes.Repeat([]byte{0x61}, 32))
	storedKey, err := repository.EnsureCodexAccountScopeKey(context.Background(), key, nowMS)
	if err != nil {
		t.Fatalf("EnsureCodexAccountScopeKey() error = %v", err)
	}
	scope, err := accountbinding.DeriveScope(storedKey, []byte("subscription-runtime-account"))
	if err != nil {
		t.Fatalf("DeriveScope() error = %v", err)
	}
	if _, _, err := repository.ConfirmCodexAccountBinding(
		context.Background(), scope, nowMS, store.CodexAccountBindingReasonStartup,
	); err != nil {
		t.Fatalf("ConfirmCodexAccountBinding() error = %v", err)
	}

	runtime := newCodexSubscriptionRuntime(repository, nil)
	snapshot, err := runtime.ListCodexSubscriptionAccounts(
		context.Background(), nowMS, "Asia/Shanghai",
	)
	if err != nil || len(snapshot.Accounts) != 1 ||
		snapshot.Accounts[0].LegacyQuotaHistory == nil ||
		snapshot.Accounts[0].LegacyQuotaHistory.State != subscriptionaccounts.LegacyQuotaHistoryUnavailable {
		t.Fatalf("ListCodexSubscriptionAccounts() = %#v, %v", snapshot, err)
	}
}
