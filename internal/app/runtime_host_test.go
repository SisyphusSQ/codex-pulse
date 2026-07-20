package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

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
