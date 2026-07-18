package app

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/SisyphusSQ/codex-pulse/internal/preferences"
	"github.com/SisyphusSQ/codex-pulse/internal/updater"
)

func TestApplicationUpdaterRuntimeRetainsStartupFailureAndCloses(t *testing.T) {
	t.Parallel()

	startErr := errors.New("missing update configuration")
	adapter := &appUpdaterAdapterStub{startErr: startErr}
	runtime, err := startApplicationUpdater(t.Context(), adapter, newAppUpdaterPreferenceStore(), nil)
	if err != nil {
		t.Fatalf("startApplicationUpdater: %v", err)
	}
	if !errors.Is(runtime.StartupError(), startErr) {
		t.Fatalf("StartupError=%v, want %v", runtime.StartupError(), startErr)
	}
	if snapshot := runtime.Snapshot(); snapshot.Phase != updater.PhaseError || snapshot.Fault == nil {
		t.Fatalf("Snapshot=%#v, want observable startup error", snapshot)
	}
	if err := runtime.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := runtime.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
	if adapter.closeCalls != 1 {
		t.Fatalf("close calls=%d, want 1", adapter.closeCalls)
	}
}

func TestApplicationUpdaterRuntimeRejectsNilAdapter(t *testing.T) {
	t.Parallel()

	if _, err := startApplicationUpdater(t.Context(), nil, newAppUpdaterPreferenceStore(), nil); !errors.Is(err, updater.ErrAdapterRequired) {
		t.Fatalf("error=%v, want ErrAdapterRequired", err)
	}
}

func TestApplicationUpdaterRuntimeCoordinatesManualCheck(t *testing.T) {
	t.Parallel()

	adapter := &appUpdaterAdapterStub{}
	store := newAppUpdaterPreferenceStore()
	store.snapshot.Updates.AutoCheckEnabled = false
	runtime, err := startApplicationUpdater(t.Context(), adapter, store, nil)
	if err != nil {
		t.Fatalf("startApplicationUpdater: %v", err)
	}
	t.Cleanup(func() { _ = runtime.Close() })

	receipt, err := runtime.Trigger(t.Context(), updater.TriggerManual)
	if err != nil || !receipt.Accepted || adapter.checkCalls != 1 {
		t.Fatalf("Trigger receipt=%#v checks=%d err=%v", receipt, adapter.checkCalls, err)
	}
}

type appUpdaterAdapterStub struct {
	startErr   error
	checkCalls int
	closeCalls int
}

func (adapter *appUpdaterAdapterStub) Start(updater.EventSink) error { return adapter.startErr }
func (adapter *appUpdaterAdapterStub) Check() error                  { adapter.checkCalls++; return nil }
func (*appUpdaterAdapterStub) Cancel() error                         { return nil }
func (adapter *appUpdaterAdapterStub) Close() error {
	adapter.closeCalls++
	return nil
}

type appUpdaterPreferenceStore struct {
	mu       sync.Mutex
	snapshot preferences.Snapshot
}

func newAppUpdaterPreferenceStore() *appUpdaterPreferenceStore {
	return &appUpdaterPreferenceStore{snapshot: preferences.Snapshot{
		Revision: 1, Updates: preferences.DefaultUpdatePreferences(),
	}}
}

func (store *appUpdaterPreferenceStore) LoadPreferences(context.Context) (preferences.Snapshot, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	return store.snapshot, nil
}

func (store *appUpdaterPreferenceStore) CompareAndSwap(_ context.Context, expected uint64, next preferences.Snapshot) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	if store.snapshot.Revision != expected {
		return preferences.ErrPreferencesConflict
	}
	store.snapshot = next
	return nil
}
