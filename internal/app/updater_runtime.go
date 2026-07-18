package app

import (
	"context"
	"time"

	"github.com/SisyphusSQ/codex-pulse/internal/updater"
)

type applicationUpdaterRuntime struct {
	controller  *updater.Controller
	coordinator *updater.Coordinator
	startupErr  error
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
