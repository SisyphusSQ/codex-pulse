package updater

import (
	"errors"
	"fmt"
	"sync"
)

var (
	ErrAdapterRequired = errors.New("updater adapter is required")
	ErrNotStarted      = errors.New("updater is not started")
	ErrAlreadyStarted  = errors.New("updater is already started")
	ErrCannotCancel    = errors.New("updater operation cannot be cancelled")
	ErrCannotDownload  = errors.New("updater download cannot be started")
	ErrClosed          = errors.New("updater is closed")
)

type Controller struct {
	mu              sync.RWMutex
	adapter         Adapter
	snapshot        Snapshot
	started         bool
	closed          bool
	downloadPending bool
	generation      uint64
}

func (controller *Controller) Download() error {
	controller.mu.RLock()
	if controller.closed {
		controller.mu.RUnlock()
		return ErrClosed
	}
	started := controller.started
	snapshot := controller.snapshot
	pending := controller.downloadPending
	controller.mu.RUnlock()
	if !started {
		return ErrNotStarted
	}
	if snapshot.Phase != PhaseAvailable || snapshot.Update == nil || snapshot.ReadyToInstall || pending {
		return ErrCannotDownload
	}
	controller.mu.Lock()
	if controller.closed || controller.downloadPending || controller.snapshot.Phase != PhaseAvailable || controller.snapshot.ReadyToInstall {
		controller.mu.Unlock()
		return ErrCannotDownload
	}
	controller.downloadPending = true
	controller.mu.Unlock()
	downloader, ok := controller.adapter.(DownloadAdapter)
	if !ok {
		controller.clearDownloadPending()
		return ErrCannotDownload
	}
	if err := downloader.Download(); err != nil {
		controller.clearDownloadPending()
		controller.recordFailure(FaultDownload, err)
		return fmt.Errorf("download update: %w", err)
	}
	return nil
}

func NewController(adapter Adapter) (*Controller, error) {
	if adapter == nil {
		return nil, ErrAdapterRequired
	}
	return &Controller{adapter: adapter, snapshot: Snapshot{Phase: PhaseIdle}}, nil
}

func (controller *Controller) Start() error {
	controller.mu.Lock()
	if controller.closed {
		controller.mu.Unlock()
		return ErrClosed
	}
	if controller.started {
		controller.mu.Unlock()
		return ErrAlreadyStarted
	}
	controller.started = true
	controller.generation++
	generation := controller.generation
	controller.mu.Unlock()

	err := controller.adapter.Start(func(event Event) {
		controller.handle(generation, event)
	})
	if err == nil {
		return nil
	}

	controller.mu.Lock()
	if !controller.closed && controller.generation == generation {
		controller.started = false
		controller.generation++
		controller.snapshot = failureSnapshot(faultCodeFromError(err, FaultNative), err)
	}
	controller.mu.Unlock()
	return fmt.Errorf("start updater adapter: %w", err)
}

func (controller *Controller) Check() error {
	controller.mu.Lock()
	if controller.closed {
		controller.mu.Unlock()
		return ErrClosed
	}
	if !controller.started {
		controller.mu.Unlock()
		return ErrNotStarted
	}
	if controller.downloadPending {
		controller.mu.Unlock()
		return fmt.Errorf("%w: download request pending", ErrInvalidTransition)
	}
	next, err := Reduce(controller.snapshot, Event{Kind: EventCheckStarted})
	if err != nil {
		controller.mu.Unlock()
		return err
	}
	controller.snapshot = next
	controller.mu.Unlock()

	if err := controller.adapter.Check(); err != nil {
		controller.recordFailure(FaultCheck, err)
		return fmt.Errorf("check for updates: %w", err)
	}
	return nil
}

func (controller *Controller) Cancel() error {
	controller.mu.RLock()
	if controller.closed {
		controller.mu.RUnlock()
		return ErrClosed
	}
	canCancel := controller.snapshot.CanCancel
	started := controller.started
	controller.mu.RUnlock()
	if !canCancel {
		return ErrCannotCancel
	}
	if !started {
		return ErrNotStarted
	}
	if err := controller.adapter.Cancel(); err != nil {
		return fmt.Errorf("cancel updater operation: %w", err)
	}
	return nil
}

func (controller *Controller) Close() error {
	controller.mu.Lock()
	if controller.closed {
		controller.mu.Unlock()
		return nil
	}
	controller.closed = true
	controller.started = false
	controller.downloadPending = false
	controller.generation++
	controller.snapshot, _ = Reduce(controller.snapshot, Event{Kind: EventClosed})
	controller.mu.Unlock()

	if err := controller.adapter.Close(); err != nil {
		return fmt.Errorf("close updater adapter: %w", err)
	}
	return nil
}

func (controller *Controller) Snapshot() Snapshot {
	controller.mu.RLock()
	defer controller.mu.RUnlock()
	return cloneSnapshot(controller.snapshot)
}

func (controller *Controller) handle(generation uint64, event Event) {
	controller.mu.Lock()
	defer controller.mu.Unlock()
	if controller.closed || controller.generation != generation {
		return
	}
	if event.Kind == EventDownloadStarted || event.Kind == EventReadyToInstall ||
		event.Kind == EventDownloadCancelled || event.Kind == EventFailed || event.Kind == EventClosed {
		controller.downloadPending = false
	}
	next, err := Reduce(controller.snapshot, event)
	if err != nil {
		controller.snapshot = failureSnapshot(FaultNative, err)
		return
	}
	controller.snapshot = next
}

func (controller *Controller) clearDownloadPending() {
	controller.mu.Lock()
	controller.downloadPending = false
	controller.mu.Unlock()
}

func (controller *Controller) recordFailure(code FaultCode, err error) {
	controller.mu.Lock()
	defer controller.mu.Unlock()
	if controller.closed {
		return
	}
	controller.snapshot = failureSnapshot(code, err)
}

func failureSnapshot(code FaultCode, err error) Snapshot {
	return Snapshot{Phase: PhaseError, Fault: &Fault{Code: code, Message: err.Error()}}
}

func faultCodeFromError(err error, fallback FaultCode) FaultCode {
	var nativeError *NativeError
	if errors.As(err, &nativeError) && nativeError.Code != "" {
		return nativeError.Code
	}
	return fallback
}

func cloneSnapshot(snapshot Snapshot) Snapshot {
	clone := snapshot
	if snapshot.Update != nil {
		update := *snapshot.Update
		clone.Update = &update
	}
	if snapshot.Fault != nil {
		fault := *snapshot.Fault
		clone.Fault = &fault
	}
	return clone
}
