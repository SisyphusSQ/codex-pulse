package updater

import (
	"errors"
	"testing"
)

func TestControllerDrivesAdapterAndReducesCallbacks(t *testing.T) {
	t.Parallel()

	adapter := &fakeAdapter{}
	controller, err := NewController(adapter)
	if err != nil {
		t.Fatalf("NewController: %v", err)
	}
	if err := controller.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if err := controller.Check(); err != nil {
		t.Fatalf("Check: %v", err)
	}
	if adapter.checkCalls != 1 || controller.Snapshot().Phase != PhaseChecking {
		t.Fatalf("after Check calls=%d snapshot=%#v", adapter.checkCalls, controller.Snapshot())
	}

	adapter.emit(Event{Kind: EventUpdateFound, Update: &Update{Version: "42", DisplayVersion: "0.2.0", Architecture: "arm64"}})
	adapter.emit(Event{Kind: EventDownloadStarted})
	if controller.Snapshot().Phase != PhaseDownloading {
		t.Fatalf("callbacks snapshot=%#v, want downloading", controller.Snapshot())
	}
	if err := controller.Cancel(); err != nil {
		t.Fatalf("Cancel: %v", err)
	}
	if adapter.cancelCalls != 1 {
		t.Fatalf("cancel calls=%d, want 1", adapter.cancelCalls)
	}
	adapter.emit(Event{Kind: EventDownloadCancelled})
	if controller.Snapshot().Phase != PhaseAvailable {
		t.Fatalf("cancel callback snapshot=%#v, want available", controller.Snapshot())
	}
}

func TestControllerDownloadRequiresAvailableUpdate(t *testing.T) {
	t.Parallel()

	adapter := &fakeDownloadAdapter{}
	controller, err := NewController(adapter)
	if err != nil {
		t.Fatalf("NewController: %v", err)
	}
	if err := controller.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if err := controller.Download(); !errors.Is(err, ErrCannotDownload) {
		t.Fatalf("Download while idle error=%v, want ErrCannotDownload", err)
	}
	if err := controller.Check(); err != nil {
		t.Fatalf("Check: %v", err)
	}
	adapter.emit(Event{Kind: EventUpdateFound, Update: &Update{Version: "42", Architecture: "arm64"}})
	if err := controller.Download(); err != nil {
		t.Fatalf("Download: %v", err)
	}
	if adapter.downloadCalls != 1 {
		t.Fatalf("download calls=%d, want 1", adapter.downloadCalls)
	}
	if err := controller.Download(); !errors.Is(err, ErrCannotDownload) {
		t.Fatalf("Download while request pending error=%v, want ErrCannotDownload", err)
	}
	if adapter.downloadCalls != 1 {
		t.Fatalf("download calls after duplicate=%d, want 1", adapter.downloadCalls)
	}
	if err := controller.Check(); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("Check while download request pending error=%v, want ErrInvalidTransition", err)
	}
	adapter.emit(Event{Kind: EventDownloadStarted})
	if err := controller.Download(); !errors.Is(err, ErrCannotDownload) {
		t.Fatalf("second Download error=%v, want ErrCannotDownload", err)
	}
}

func TestControllerCancelsActiveCheck(t *testing.T) {
	t.Parallel()

	adapter := &fakeAdapter{}
	controller, err := NewController(adapter)
	if err != nil {
		t.Fatalf("NewController: %v", err)
	}
	if err := controller.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if err := controller.Check(); err != nil {
		t.Fatalf("Check: %v", err)
	}
	if !controller.Snapshot().CanCancel {
		t.Fatalf("checking snapshot=%#v, want cancellable", controller.Snapshot())
	}
	if err := controller.Cancel(); err != nil {
		t.Fatalf("Cancel: %v", err)
	}
	if adapter.cancelCalls != 1 {
		t.Fatalf("cancel calls=%d, want 1", adapter.cancelCalls)
	}
	adapter.emit(Event{Kind: EventCheckCancelled})
	if snapshot := controller.Snapshot(); snapshot.Phase != PhaseIdle || snapshot.CanCancel {
		t.Fatalf("cancel callback snapshot=%#v, want idle", snapshot)
	}
}

func TestControllerRejectsInvalidCommands(t *testing.T) {
	t.Parallel()

	controller, err := NewController(&fakeAdapter{})
	if err != nil {
		t.Fatalf("NewController: %v", err)
	}
	if err := controller.Check(); !errors.Is(err, ErrNotStarted) {
		t.Fatalf("Check before Start error=%v, want ErrNotStarted", err)
	}
	if err := controller.Cancel(); !errors.Is(err, ErrCannotCancel) {
		t.Fatalf("Cancel while idle error=%v, want ErrCannotCancel", err)
	}
	if err := controller.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if err := controller.Start(); !errors.Is(err, ErrAlreadyStarted) {
		t.Fatalf("second Start error=%v, want ErrAlreadyStarted", err)
	}
}

func TestControllerMapsSynchronousAdapterErrors(t *testing.T) {
	t.Parallel()

	checkErr := errors.New("native check rejected")
	adapter := &fakeAdapter{checkErr: checkErr}
	controller, err := NewController(adapter)
	if err != nil {
		t.Fatalf("NewController: %v", err)
	}
	if err := controller.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if err := controller.Check(); !errors.Is(err, checkErr) {
		t.Fatalf("Check error=%v, want wrapped native error", err)
	}
	snapshot := controller.Snapshot()
	if snapshot.Phase != PhaseError || snapshot.Fault == nil || snapshot.Fault.Code != FaultCheck {
		t.Fatalf("check failure snapshot=%#v, want typed check fault", snapshot)
	}
}

func TestControllerPreservesTypedStartupFault(t *testing.T) {
	t.Parallel()

	adapter := &fakeAdapter{startErr: &NativeError{Code: FaultConfiguration, Message: "missing feed"}}
	controller, err := NewController(adapter)
	if err != nil {
		t.Fatalf("NewController: %v", err)
	}
	if err := controller.Start(); err == nil {
		t.Fatal("Start succeeded")
	}
	snapshot := controller.Snapshot()
	if snapshot.Fault == nil || snapshot.Fault.Code != FaultConfiguration {
		t.Fatalf("snapshot=%#v, want configuration fault", snapshot)
	}
}

func TestControllerCloseInvalidatesLateCallbacks(t *testing.T) {
	t.Parallel()

	adapter := &fakeAdapter{}
	controller, err := NewController(adapter)
	if err != nil {
		t.Fatalf("NewController: %v", err)
	}
	if err := controller.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	late := adapter.sink
	if err := controller.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := controller.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
	if adapter.closeCalls != 1 {
		t.Fatalf("close calls=%d, want 1", adapter.closeCalls)
	}
	late(Event{Kind: EventFailed, Fault: &Fault{Code: FaultNative, Message: "late callback"}})
	snapshot := controller.Snapshot()
	if !snapshot.Closed || snapshot.Phase != PhaseIdle || snapshot.Fault != nil {
		t.Fatalf("late callback snapshot=%#v, want closed idle", snapshot)
	}
	if err := controller.Check(); !errors.Is(err, ErrClosed) {
		t.Fatalf("Check after close error=%v, want ErrClosed", err)
	}
}

func TestControllerSnapshotIsDefensiveCopy(t *testing.T) {
	t.Parallel()

	adapter := &fakeAdapter{}
	controller, err := NewController(adapter)
	if err != nil {
		t.Fatalf("NewController: %v", err)
	}
	if err := controller.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if err := controller.Check(); err != nil {
		t.Fatalf("Check: %v", err)
	}
	adapter.emit(Event{Kind: EventUpdateFound, Update: &Update{Version: "42", Architecture: "arm64"}})

	copy := controller.Snapshot()
	copy.Update.Version = "tampered"
	if controller.Snapshot().Update.Version != "42" {
		t.Fatal("caller mutated controller snapshot")
	}
}

type fakeAdapter struct {
	sink        EventSink
	startErr    error
	checkErr    error
	cancelErr   error
	closeErr    error
	checkCalls  int
	cancelCalls int
	closeCalls  int
}

type fakeDownloadAdapter struct {
	fakeAdapter
	downloadCalls int
}

func (adapter *fakeDownloadAdapter) Download() error {
	adapter.downloadCalls++
	return nil
}

func (adapter *fakeAdapter) Start(sink EventSink) error {
	adapter.sink = sink
	return adapter.startErr
}

func (adapter *fakeAdapter) Check() error {
	adapter.checkCalls++
	return adapter.checkErr
}

func (adapter *fakeAdapter) Cancel() error {
	adapter.cancelCalls++
	return adapter.cancelErr
}

func (adapter *fakeAdapter) Close() error {
	adapter.closeCalls++
	return adapter.closeErr
}

func (adapter *fakeAdapter) emit(event Event) {
	adapter.sink(event)
}
