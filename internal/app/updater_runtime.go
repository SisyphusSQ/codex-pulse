package app

import "github.com/SisyphusSQ/codex-pulse/internal/updater"

type applicationUpdaterRuntime struct {
	controller *updater.Controller
	startupErr error
}

func startApplicationUpdater(adapter updater.Adapter) (*applicationUpdaterRuntime, error) {
	controller, err := updater.NewController(adapter)
	if err != nil {
		return nil, err
	}
	startupErr := controller.Start()
	return &applicationUpdaterRuntime{controller: controller, startupErr: startupErr}, nil
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

func (runtime *applicationUpdaterRuntime) Close() error {
	if runtime == nil || runtime.controller == nil {
		return nil
	}
	return runtime.controller.Close()
}
