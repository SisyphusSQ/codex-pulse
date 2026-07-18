package app

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

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

func TestApplicationUpdaterRuntimeInstallsOnlyAfterSharedShutdown(t *testing.T) {
	t.Parallel()

	adapter := &appUpdaterAdapterStub{}
	runtime, err := startApplicationUpdater(t.Context(), adapter, newAppUpdaterPreferenceStore(), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtime.Close() })
	adapter.sink(updater.Event{Kind: updater.EventUpdateFound, Update: &updater.Update{Version: "42", Architecture: "arm64"}})
	adapter.sink(updater.Event{Kind: updater.EventDownloadStarted})
	adapter.sink(updater.Event{Kind: updater.EventReadyToInstall})
	closed := false
	shutdown, err := newApplicationShutdownCoordinator(shutdownComponent{Name: "sqlite", Close: func(context.Context) error {
		closed = true
		return nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	if err := runtime.bindShutdown(shutdown); err != nil {
		t.Fatal(err)
	}
	if err := runtime.Install(t.Context()); err != nil {
		t.Fatal(err)
	}
	if !closed || adapter.installCalls != 1 {
		t.Fatalf("closed=%v installCalls=%d", closed, adapter.installCalls)
	}
}

func TestApplicationUpdaterRuntimeDoesNotInstallAfterShutdownFailure(t *testing.T) {
	t.Parallel()

	adapter := &appUpdaterAdapterStub{}
	runtime, err := startApplicationUpdater(t.Context(), adapter, newAppUpdaterPreferenceStore(), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtime.Close() })
	prepareAppUpdaterInstall(t, adapter)
	want := errors.New("sqlite close failed")
	shutdown, err := newApplicationShutdownCoordinator(shutdownComponent{Name: "sqlite", Close: func(context.Context) error {
		return want
	}})
	if err != nil {
		t.Fatal(err)
	}
	if err := runtime.bindShutdown(shutdown); err != nil {
		t.Fatal(err)
	}
	if err := runtime.Install(t.Context()); !errors.Is(err, want) {
		t.Fatalf("Install() error=%v, want %v", err, want)
	}
	if adapter.installCalls != 0 {
		t.Fatalf("installCalls=%d, want 0", adapter.installCalls)
	}
}

func TestApplicationUpdaterRuntimeTimeoutDoesNotInstallUntilReawaitSucceeds(t *testing.T) {
	t.Parallel()

	adapter := &appUpdaterAdapterStub{}
	runtime, err := startApplicationUpdater(t.Context(), adapter, newAppUpdaterPreferenceStore(), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtime.Close() })
	prepareAppUpdaterInstall(t, adapter)
	release := make(chan struct{})
	shutdown, err := newApplicationShutdownCoordinator(shutdownComponent{Name: "sqlite", Close: func(context.Context) error {
		<-release
		return nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	if err := runtime.bindShutdown(shutdown); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()
	if err := runtime.Install(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Install(timeout) error=%v", err)
	}
	if adapter.installCalls != 0 {
		t.Fatalf("installCalls after timeout=%d, want 0", adapter.installCalls)
	}
	close(release)
	if err := runtime.Install(t.Context()); err != nil {
		t.Fatalf("Install(reawait) error=%v", err)
	}
	if adapter.installCalls != 1 {
		t.Fatalf("installCalls after reawait=%d, want 1", adapter.installCalls)
	}
}

func TestApplicationUpdaterRuntimeArbitratesQuitUntilNativeInstallDispatch(t *testing.T) {
	t.Parallel()

	gate := &nativeQuitPreflight{}
	adapter := &appUpdaterAdapterStub{}
	runtime, err := startApplicationUpdater(t.Context(), adapter, newAppUpdaterPreferenceStore(), func(snapshot updater.Snapshot) {
		if snapshot.Phase == updater.PhaseInstalling {
			gate.MarkInstallReady()
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtime.Close() })
	prepareAppUpdaterInstall(t, adapter)
	shutdown, err := newApplicationShutdownCoordinator(shutdownComponent{Name: "sqlite", Close: func(context.Context) error {
		return nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	if err := gate.Bind(shutdown, func() {}); err != nil {
		t.Fatal(err)
	}
	if err := runtime.bindShutdown(shutdown); err != nil {
		t.Fatal(err)
	}
	if err := runtime.bindInstallGate(gate); err != nil {
		t.Fatal(err)
	}
	adapter.installHook = func() {
		if err := gate.BeginQuit(); err == nil {
			t.Error("concurrent Quit was admitted before native install dispatch")
		}
		adapter.sink(updater.Event{Kind: updater.EventInstallStarted})
	}
	if err := runtime.Install(t.Context()); err != nil {
		t.Fatal(err)
	}
	if !gate.ShouldQuit() {
		t.Fatal("native termination was not admitted after install dispatch")
	}
}

func prepareAppUpdaterInstall(t *testing.T, adapter *appUpdaterAdapterStub) {
	t.Helper()
	adapter.sink(updater.Event{Kind: updater.EventUpdateFound, Update: &updater.Update{Version: "42", Architecture: "arm64"}})
	adapter.sink(updater.Event{Kind: updater.EventDownloadStarted})
	adapter.sink(updater.Event{Kind: updater.EventReadyToInstall})
}

type appUpdaterAdapterStub struct {
	startErr     error
	checkCalls   int
	installCalls int
	installHook  func()
	closeCalls   int
	sink         updater.EventSink
}

func (adapter *appUpdaterAdapterStub) Start(sink updater.EventSink) error {
	adapter.sink = sink
	return adapter.startErr
}
func (adapter *appUpdaterAdapterStub) Check() error { adapter.checkCalls++; return nil }
func (adapter *appUpdaterAdapterStub) Install() error {
	adapter.installCalls++
	if adapter.installHook != nil {
		adapter.installHook()
	}
	return nil
}
func (*appUpdaterAdapterStub) Cancel() error { return nil }
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
