package updater

import (
	"errors"
	"sync"
	"testing"
)

func TestControllerDrivesAdapterAndReducesCallbacks(t *testing.T) {
	t.Parallel()

	adapter := &fakeDownloadAdapter{}
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

func TestControllerRejectsInformationOnlyDownload(t *testing.T) {
	adapter := &fakeDownloadAdapter{}
	controller, err := NewController(adapter)
	if err != nil {
		t.Fatal(err)
	}
	if err := controller.Start(); err != nil {
		t.Fatal(err)
	}
	if err := controller.Check(); err != nil {
		t.Fatal(err)
	}
	adapter.emit(Event{Kind: EventUpdateFound, Update: &Update{
		Version: "42", Architecture: "arm64", InformationOnly: true,
		InformationURL: "https://example.com/fallback",
	}})
	if err := controller.Download(); !errors.Is(err, ErrCannotDownload) {
		t.Fatalf("Download information-only error = %v", err)
	}
	if adapter.downloadCalls != 0 {
		t.Fatalf("native download calls = %d", adapter.downloadCalls)
	}
}

func TestControllerInstallRequiresReadySnapshotAndMapsFailure(t *testing.T) {
	t.Parallel()

	installErr := errors.New("native install reply missing")
	adapter := &fakeInstallAdapter{}
	controller, err := NewController(adapter)
	if err != nil {
		t.Fatal(err)
	}
	if err := controller.Start(); err != nil {
		t.Fatal(err)
	}
	if err := controller.Install(); !errors.Is(err, ErrCannotInstall) {
		t.Fatalf("Install idle=%v", err)
	}
	if err := controller.Check(); err != nil {
		t.Fatal(err)
	}
	adapter.emit(Event{Kind: EventUpdateFound, Update: &Update{Version: "42", Architecture: "arm64"}})
	adapter.emit(Event{Kind: EventDownloadStarted})
	adapter.emit(Event{Kind: EventReadyToInstall})
	if err := controller.Install(); err != nil || adapter.installCalls != 1 {
		t.Fatalf("Install ready calls=%d err=%v", adapter.installCalls, err)
	}
	if err := controller.Install(); !errors.Is(err, ErrCannotInstall) || adapter.installCalls != 1 {
		t.Fatalf("duplicate Install calls=%d err=%v", adapter.installCalls, err)
	}
	adapter.emit(Event{Kind: EventInstallStarted})
	adapter.emit(Event{Kind: EventCycleFinished})
	if err := controller.Check(); err != nil {
		t.Fatal(err)
	}
	adapter.emit(Event{Kind: EventUpdateFound, Update: &Update{Version: "43", Architecture: "arm64"}})
	adapter.emit(Event{Kind: EventDownloadStarted})
	adapter.emit(Event{Kind: EventReadyToInstall})
	adapter.installErr = installErr
	if err := controller.Install(); !errors.Is(err, installErr) {
		t.Fatalf("Install failure=%v", err)
	}
	if snapshot := controller.Snapshot(); snapshot.Phase != PhaseError || snapshot.Fault == nil || snapshot.Fault.Code != FaultInstall {
		t.Fatalf("snapshot=%#v", snapshot)
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

func TestControllerObserverReceivesDefensiveStateChanges(t *testing.T) {
	t.Parallel()

	adapter := &fakeAdapter{}
	var mu sync.Mutex
	var observed []Snapshot
	controller, err := NewControllerWithObserver(adapter, func(snapshot Snapshot) {
		mu.Lock()
		observed = append(observed, snapshot)
		if snapshot.Update != nil {
			snapshot.Update.Version = "observer-mutated"
		}
		mu.Unlock()
	})
	if err != nil {
		t.Fatalf("NewControllerWithObserver: %v", err)
	}
	if err := controller.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if err := controller.Check(); err != nil {
		t.Fatalf("Check: %v", err)
	}
	adapter.emit(Event{Kind: EventUpdateFound, Update: &Update{Version: "42", Architecture: "arm64"}})
	if err := controller.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(observed) != 3 || observed[0].Phase != PhaseChecking || observed[1].Phase != PhaseAvailable || !observed[2].Closed {
		t.Fatalf("observed=%#v, want checking, available, closed", observed)
	}
	if observed[1].Update == nil || observed[1].Update.Version != "observer-mutated" {
		t.Fatalf("observer did not receive mutable copy: %#v", observed[1])
	}
	if snapshot := controller.Snapshot(); snapshot.Update != nil {
		t.Fatalf("closed controller retained update after observer mutation: %#v", snapshot)
	}
}

func TestControllerObserverReceivesSynchronousFailures(t *testing.T) {
	t.Parallel()

	adapter := &fakeAdapter{checkErr: errors.New("offline")}
	var observed []Snapshot
	controller, err := NewControllerWithObserver(adapter, func(snapshot Snapshot) { observed = append(observed, snapshot) })
	if err != nil {
		t.Fatalf("NewControllerWithObserver: %v", err)
	}
	if err := controller.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if err := controller.Check(); err == nil {
		t.Fatal("Check succeeded")
	}
	if len(observed) != 2 || observed[0].Phase != PhaseChecking || observed[1].Phase != PhaseError {
		t.Fatalf("observed=%#v, want checking then error", observed)
	}
}

func TestControllerContainsObserverPanic(t *testing.T) {
	t.Parallel()

	controller, err := NewControllerWithObserver(&fakeAdapter{}, func(Snapshot) { panic("observer failure") })
	if err != nil {
		t.Fatalf("NewControllerWithObserver: %v", err)
	}
	if err := controller.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if err := controller.Check(); err != nil {
		t.Fatalf("Check after observer panic: %v", err)
	}
	if controller.Snapshot().Phase != PhaseChecking {
		t.Fatalf("snapshot=%#v, want checking", controller.Snapshot())
	}
}

func TestControllerSubmitsUpdateChoiceAndReturnsIdle(t *testing.T) {
	t.Parallel()

	adapter := &fakeChoiceAdapter{}
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
	if err := controller.Choose(UpdateChoiceDismiss); err != nil {
		t.Fatalf("Choose: %v", err)
	}
	if len(adapter.choices) != 1 || adapter.choices[0] != UpdateChoiceDismiss {
		t.Fatalf("choices=%v, want dismiss", adapter.choices)
	}
	if snapshot := controller.Snapshot(); snapshot.Phase != PhaseIdle || snapshot.Update != nil {
		t.Fatalf("snapshot=%#v, want idle", snapshot)
	}
}

func TestControllerClassifiesFeedNetworkFailureAsCheckFault(t *testing.T) {
	adapter := &fakeAdapter{}
	controller, err := NewController(adapter)
	if err != nil {
		t.Fatal(err)
	}
	if err := controller.Start(); err != nil {
		t.Fatal(err)
	}
	if err := controller.Check(); err != nil {
		t.Fatal(err)
	}
	adapter.emit(Event{Kind: EventFailed, Fault: &Fault{Code: FaultDownload, Message: "network unavailable"}})
	snapshot := controller.Snapshot()
	if snapshot.Phase != PhaseError || snapshot.Fault == nil || snapshot.Fault.Code != FaultCheck {
		t.Fatalf("checking network failure = %#v", snapshot)
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

type fakeChoiceAdapter struct {
	fakeAdapter
	choices []UpdateChoice
}

type fakeInstallAdapter struct {
	fakeAdapter
	installCalls int
	installErr   error
}

func (adapter *fakeInstallAdapter) Install() error {
	adapter.installCalls++
	return adapter.installErr
}

func (adapter *fakeChoiceAdapter) Choose(choice UpdateChoice) error {
	adapter.choices = append(adapter.choices, choice)
	return nil
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
