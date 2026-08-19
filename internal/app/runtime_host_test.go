package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/SisyphusSQ/codex-pulse/internal/apisubscriptions"
	"github.com/SisyphusSQ/codex-pulse/internal/core"
	storesqlite "github.com/SisyphusSQ/codex-pulse/internal/store/sqlite"
)

func TestRuntimeOwnsCoreGraphAndClosesIdempotently(t *testing.T) {
	directory := t.TempDir()
	if err := os.Chmod(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	broker, err := core.NewInvalidationBroker(2)
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := Open(t.Context(), Config{
		Broker:          broker,
		Store:           storesqlite.Config{Path: filepath.Join(directory, "codex-pulse.db")},
		PreferencesPath: filepath.Join(directory, "preferences.json"),
	})
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	if runtime.Service() == nil || runtime.Broker() != broker || runtime.Recovery() != nil {
		t.Fatalf("runtime graph = %#v", runtime)
	}
	if !runtime.RequestShutdown("client_exit") || runtime.RequestShutdown("client_restart") {
		t.Fatal("RequestShutdown() was not first-writer-wins")
	}
	select {
	case reason := <-runtime.StopRequested():
		if reason != "client_exit" {
			t.Fatalf("stop reason = %q", reason)
		}
	case <-time.After(time.Second):
		t.Fatal("stop request was not published")
	}
	if err := runtime.Close(t.Context()); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if err := runtime.Close(t.Context()); err != nil {
		t.Fatalf("Close(replay) error = %v", err)
	}
	_, _, err = broker.Subscribe(context.Background(), nil, 0)
	if !errors.Is(err, core.ErrInvalidation) {
		t.Fatalf("broker after Close error = %v", err)
	}
}

func TestRuntimeOwnsSeparateCredentialDatabaseAcrossRestart(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()
	if err := os.Chmod(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	databasePath := filepath.Join(directory, "codex-pulse.db")
	credentialsPath := filepath.Join(directory, "credentials.db")
	preferencesPath := filepath.Join(directory, "preferences.json")

	openRuntime := func(t testing.TB) *Runtime {
		t.Helper()
		broker, err := core.NewInvalidationBroker(2)
		if err != nil {
			t.Fatal(err)
		}
		runtime, err := Open(t.Context(), Config{
			Broker:          broker,
			Store:           storesqlite.Config{Path: databasePath},
			PreferencesPath: preferencesPath,
		})
		if err != nil {
			broker.Close()
			t.Fatalf("Open() error = %v", err)
		}
		return runtime
	}

	first := openRuntime(t)
	status, err := first.Service().UpdateAPICredential(t.Context(), core.APICredentialUpdateRequest{
		Service: apisubscriptions.ServiceDeepSeek,
		Secret:  []byte("deepseek-secret"),
	})
	if err != nil {
		t.Fatalf("UpdateAPICredential() error = %v", err)
	}
	if !status.DeepSeekConfigured || status.OpenCodeGoConfigured {
		t.Fatalf("UpdateAPICredential() status = %#v", status)
	}
	if err := first.Close(t.Context()); err != nil {
		t.Fatalf("Close(first) error = %v", err)
	}

	info, err := os.Stat(credentialsPath)
	if err != nil {
		t.Fatalf("Stat(credentials.db) error = %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("credentials.db mode = %#o, want 0600", info.Mode().Perm())
	}
	second := openRuntime(t)
	t.Cleanup(func() { _ = second.Close(context.Background()) })
	status, err = second.Service().APICredentialStatus(t.Context())
	if err != nil {
		t.Fatalf("APICredentialStatus(reopen) error = %v", err)
	}
	if !status.DeepSeekConfigured || status.OpenCodeGoConfigured {
		t.Fatalf("APICredentialStatus(reopen) = %#v", status)
	}
}
