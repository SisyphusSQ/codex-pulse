package app

import (
	"context"
	"sync"
	"time"

	"github.com/SisyphusSQ/codex-pulse/internal/updater"
)

type applicationUpdaterRuntime struct {
	controller  *updater.Controller
	coordinator *updater.Coordinator
	startupErr  error
	shutdownMu  sync.RWMutex
	shutdown    *applicationShutdownCoordinator
	installGate *nativeQuitPreflight
}

func (runtime *applicationUpdaterRuntime) bindShutdown(shutdown *applicationShutdownCoordinator) error {
	if runtime == nil || shutdown == nil {
		return updater.ErrInvalidCoordinator
	}
	runtime.shutdownMu.Lock()
	defer runtime.shutdownMu.Unlock()
	if runtime.shutdown != nil {
		return updater.ErrInvalidCoordinator
	}
	runtime.shutdown = shutdown
	return nil
}

func (runtime *applicationUpdaterRuntime) bindInstallGate(gate *nativeQuitPreflight) error {
	if runtime == nil || gate == nil {
		return updater.ErrInvalidCoordinator
	}
	runtime.shutdownMu.Lock()
	defer runtime.shutdownMu.Unlock()
	if runtime.installGate != nil {
		return updater.ErrInvalidCoordinator
	}
	runtime.installGate = gate
	return nil
}

func startApplicationUpdater(
	ctx context.Context,
	adapter updater.Adapter,
	store updater.PreferenceStore,
	observer updater.SnapshotObserver,
) (*applicationUpdaterRuntime, error) {
	if ctx == nil {
		return nil, updater.ErrInvalidCoordinator
	}
	var coordinator *updater.Coordinator
	controller, err := updater.NewControllerWithObserver(adapter, func(snapshot updater.Snapshot) {
		if coordinator != nil {
			coordinator.ObserveSnapshot(snapshot)
		}
		if observer != nil {
			observer(snapshot)
		}
	})
	if err != nil {
		return nil, err
	}
	startupErr := controller.Start()
	coordinator, err = updater.NewCoordinator(updater.CoordinatorConfig{Store: store, Controller: controller})
	if err != nil {
		_ = controller.Close()
		return nil, err
	}
	if err := coordinator.Start(ctx); err != nil {
		_ = controller.Close()
		return nil, err
	}
	return &applicationUpdaterRuntime{controller: controller, coordinator: coordinator, startupErr: startupErr}, nil
}

func (runtime *applicationUpdaterRuntime) StartupError() error {
	if runtime == nil {
		return updater.ErrNotStarted
	}
	return runtime.startupErr
}

func (runtime *applicationUpdaterRuntime) Snapshot() updater.Snapshot {
	if runtime == nil || runtime.controller == nil {
		return updater.Snapshot{Phase: updater.PhaseError, Fault: &updater.Fault{
			Code: updater.FaultUnavailable, Message: updater.ErrNotStarted.Error(),
		}}
	}
	return runtime.controller.Snapshot()
}

func (runtime *applicationUpdaterRuntime) View(ctx context.Context) (updater.View, error) {
	if runtime == nil || runtime.coordinator == nil {
		return updater.View{}, updater.ErrNotStarted
	}
	return runtime.coordinator.View(ctx)
}

func (runtime *applicationUpdaterRuntime) Trigger(ctx context.Context, trigger updater.Trigger) (updater.TriggerReceipt, error) {
	if runtime == nil || runtime.coordinator == nil {
		return updater.TriggerReceipt{}, updater.ErrNotStarted
	}
	return runtime.coordinator.Trigger(ctx, trigger)
}

func (runtime *applicationUpdaterRuntime) Wake(ctx context.Context) (updater.TriggerReceipt, error) {
	if runtime == nil || runtime.coordinator == nil {
		return updater.TriggerReceipt{}, updater.ErrNotStarted
	}
	return runtime.coordinator.Wake(ctx)
}

func (runtime *applicationUpdaterRuntime) Download(ctx context.Context) error {
	if runtime == nil || runtime.coordinator == nil {
		return updater.ErrNotStarted
	}
	return runtime.coordinator.Download(ctx)
}

func (runtime *applicationUpdaterRuntime) Install(ctx context.Context) error {
	if runtime == nil || runtime.coordinator == nil || ctx == nil {
		return updater.ErrNotStarted
	}
	runtime.shutdownMu.RLock()
	shutdown := runtime.shutdown
	installGate := runtime.installGate
	runtime.shutdownMu.RUnlock()
	if shutdown == nil {
		return updater.ErrCannotInstall
	}
	if snapshot := runtime.controller.Snapshot(); snapshot.Phase != updater.PhaseAvailable || !snapshot.ReadyToInstall {
		return updater.ErrCannotInstall
	}
	if installGate != nil {
		if err := installGate.BeginInstall(); err != nil {
			return updater.ErrCannotInstall
		}
		accepted := false
		defer func() {
			if !accepted {
				installGate.AbortInstall()
			}
		}()
		shutdownCtx, cancel := context.WithTimeout(ctx, desktopShutdownTimeout)
		defer cancel()
		if err := shutdown.Close(shutdownCtx); err != nil {
			return err
		}
		if err := runtime.coordinator.Install(ctx); err != nil {
			return err
		}
		accepted = true
		return nil
	}
	shutdownCtx, cancel := context.WithTimeout(ctx, desktopShutdownTimeout)
	defer cancel()
	if err := shutdown.Close(shutdownCtx); err != nil {
		return err
	}
	return runtime.coordinator.Install(ctx)
}

func (runtime *applicationUpdaterRuntime) InstallState() shutdownSnapshot {
	if runtime == nil {
		return shutdownSnapshot{}
	}
	runtime.shutdownMu.RLock()
	shutdown := runtime.shutdown
	runtime.shutdownMu.RUnlock()
	if shutdown == nil {
		return shutdownSnapshot{Phase: shutdownPhaseRunning}
	}
	return shutdown.Snapshot()
}

func (runtime *applicationUpdaterRuntime) Suspend() error {
	if runtime == nil || runtime.coordinator == nil {
		return updater.ErrNotStarted
	}
	return runtime.coordinator.Suspend()
}

func (runtime *applicationUpdaterRuntime) Cancel(ctx context.Context) error {
	if runtime == nil || runtime.coordinator == nil {
		return updater.ErrNotStarted
	}
	return runtime.coordinator.Cancel(ctx)
}

func (runtime *applicationUpdaterRuntime) Skip(ctx context.Context, version string) error {
	if runtime == nil || runtime.coordinator == nil {
		return updater.ErrNotStarted
	}
	return runtime.coordinator.Skip(ctx, version)
}

func (runtime *applicationUpdaterRuntime) Snooze(ctx context.Context, duration time.Duration) error {
	if runtime == nil || runtime.coordinator == nil {
		return updater.ErrNotStarted
	}
	return runtime.coordinator.Snooze(ctx, duration)
}

func (runtime *applicationUpdaterRuntime) Close() error {
	if runtime == nil || runtime.coordinator == nil {
		return nil
	}
	return runtime.coordinator.Close()
}
