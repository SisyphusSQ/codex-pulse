package apisubscriptions

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	storesqlite "github.com/SisyphusSQ/codex-pulse/internal/store/sqlite"
)

func TestSQLiteCredentialStorePersistsPrivateCredentialsAcrossRestart(t *testing.T) {
	t.Parallel()

	path := privateCredentialTestPath(t)
	clock := func() time.Time { return time.UnixMilli(1_756_000_000_000) }
	store, err := OpenSQLiteCredentialStore(t.Context(), SQLiteCredentialStoreConfig{
		Store: storesqlite.Config{Path: path},
		Now:   clock,
	})
	if err != nil {
		t.Fatalf("OpenSQLiteCredentialStore() error = %v", err)
	}
	if err := store.Save(t.Context(), ServiceDeepSeek, []byte("deepseek-secret")); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	firstEpoch, ok := store.CredentialEpoch(ServiceDeepSeek)
	if !ok || firstEpoch == "" {
		t.Fatalf("CredentialEpoch() = %q, %v", firstEpoch, ok)
	}
	secret, ok := store.APIKey(ServiceDeepSeek)
	if !ok || string(secret) != "deepseek-secret" {
		t.Fatalf("APIKey() = %q, %v", secret, ok)
	}
	clear(secret)
	if err := store.Close(context.Background()); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat() error = %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("credentials.db mode = %#o, want 0600", info.Mode().Perm())
	}

	reopened, err := OpenSQLiteCredentialStore(t.Context(), SQLiteCredentialStoreConfig{
		Store: storesqlite.Config{Path: path},
		Now:   clock,
	})
	if err != nil {
		t.Fatalf("OpenSQLiteCredentialStore(reopen) error = %v", err)
	}
	t.Cleanup(func() { _ = reopened.Close(context.Background()) })
	secret, ok = reopened.APIKey(ServiceDeepSeek)
	if !ok || string(secret) != "deepseek-secret" {
		t.Fatalf("APIKey(reopen) = %q, %v", secret, ok)
	}
	clear(secret)
	if epoch, ok := reopened.CredentialEpoch(ServiceDeepSeek); !ok || epoch != firstEpoch {
		t.Fatalf("CredentialEpoch(reopen) = %q, %v, want %q", epoch, ok, firstEpoch)
	}
	status, err := reopened.Status(t.Context())
	if err != nil {
		t.Fatalf("Status() error = %v", err)
	}
	if !status.DeepSeekConfigured || status.OpenCodeGoConfigured {
		t.Fatalf("Status() = %#v", status)
	}
}

func TestSQLiteCredentialStoreReplacementRotatesEpochAndDeleteClearsSecret(t *testing.T) {
	t.Parallel()

	store, err := OpenSQLiteCredentialStore(t.Context(), SQLiteCredentialStoreConfig{
		Store: storesqlite.Config{Path: privateCredentialTestPath(t)},
		Now:   func() time.Time { return time.UnixMilli(1_756_000_000_000) },
	})
	if err != nil {
		t.Fatalf("OpenSQLiteCredentialStore() error = %v", err)
	}
	t.Cleanup(func() { _ = store.Close(context.Background()) })

	if err := store.Save(t.Context(), ServiceOpenCodeGo, []byte("first-secret")); err != nil {
		t.Fatalf("Save(first) error = %v", err)
	}
	firstEpoch, _ := store.CredentialEpoch(ServiceOpenCodeGo)
	if err := store.Save(t.Context(), ServiceOpenCodeGo, []byte("second-secret")); err != nil {
		t.Fatalf("Save(second) error = %v", err)
	}
	secondEpoch, ok := store.CredentialEpoch(ServiceOpenCodeGo)
	if !ok || secondEpoch == "" || secondEpoch == firstEpoch {
		t.Fatalf("replacement epoch = %q, want non-empty value different from %q", secondEpoch, firstEpoch)
	}
	secret, ok := store.APIKey(ServiceOpenCodeGo)
	if !ok || string(secret) != "second-secret" {
		t.Fatalf("APIKey(replaced) = %q, %v", secret, ok)
	}
	clear(secret)

	if err := store.Delete(t.Context(), ServiceOpenCodeGo); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
	if _, ok := store.APIKey(ServiceOpenCodeGo); ok {
		t.Fatal("APIKey() returned deleted credential")
	}
	if _, ok := store.CredentialEpoch(ServiceOpenCodeGo); ok {
		t.Fatal("CredentialEpoch() returned deleted credential epoch")
	}
	status, err := store.Status(t.Context())
	if err != nil {
		t.Fatalf("Status() error = %v", err)
	}
	if status.DeepSeekConfigured || status.OpenCodeGoConfigured {
		t.Fatalf("Status(after delete) = %#v", status)
	}
}

func TestSQLiteCredentialStoreRejectsInvalidServiceAndSecret(t *testing.T) {
	t.Parallel()

	store, err := OpenSQLiteCredentialStore(t.Context(), SQLiteCredentialStoreConfig{
		Store: storesqlite.Config{Path: privateCredentialTestPath(t)},
	})
	if err != nil {
		t.Fatalf("OpenSQLiteCredentialStore() error = %v", err)
	}
	t.Cleanup(func() { _ = store.Close(context.Background()) })

	for name, testCase := range map[string]struct {
		service string
		secret  []byte
	}{
		"unknown service": {service: "unknown", secret: []byte("secret")},
		"empty secret":    {service: ServiceDeepSeek},
		"oversize secret": {service: ServiceDeepSeek, secret: make([]byte, 4_097)},
	} {
		t.Run(name, func(t *testing.T) {
			if err := store.Save(t.Context(), testCase.service, testCase.secret); err == nil {
				t.Fatal("Save() accepted invalid credential")
			}
		})
	}
	if err := store.Delete(t.Context(), "unknown"); err == nil {
		t.Fatal("Delete() accepted unknown service")
	}
}

func privateCredentialTestPath(t testing.TB) string {
	t.Helper()
	directory := t.TempDir()
	if err := os.Chmod(directory, 0o700); err != nil {
		t.Fatalf("Chmod() error = %v", err)
	}
	return filepath.Join(directory, "credentials.db")
}
