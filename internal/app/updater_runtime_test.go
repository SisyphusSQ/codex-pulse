package app

import (
	"errors"
	"testing"

	"github.com/SisyphusSQ/codex-pulse/internal/updater"
)

func TestApplicationUpdaterRuntimeRetainsStartupFailureAndCloses(t *testing.T) {
	t.Parallel()

	startErr := errors.New("missing update configuration")
	adapter := &appUpdaterAdapterStub{startErr: startErr}
	runtime, err := startApplicationUpdater(adapter)
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

	if _, err := startApplicationUpdater(nil); !errors.Is(err, updater.ErrAdapterRequired) {
		t.Fatalf("error=%v, want ErrAdapterRequired", err)
	}
}

type appUpdaterAdapterStub struct {
	startErr   error
	closeCalls int
}

func (adapter *appUpdaterAdapterStub) Start(updater.EventSink) error { return adapter.startErr }
func (*appUpdaterAdapterStub) Check() error                          { return nil }
func (*appUpdaterAdapterStub) Cancel() error                         { return nil }
func (adapter *appUpdaterAdapterStub) Close() error {
	adapter.closeCalls++
	return nil
}
