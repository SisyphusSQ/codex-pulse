package app

import (
	"context"
	"strconv"
	"time"

	basequery "github.com/SisyphusSQ/codex-pulse/internal/query"
	"github.com/SisyphusSQ/codex-pulse/internal/updater"
)

const (
	minimumUpdateSnoozeSeconds int64 = 5 * 60
	maximumUpdateSnoozeSeconds int64 = 7 * 24 * 60 * 60
)

// applicationVersion is injected by the macOS build task from the same
// APP_VERSION value later written to CFBundleShortVersionString.
var applicationVersion = "0.0.0"

type UpdateStateResponse struct {
	Phase                string  `json:"phase"`
	CurrentVersion       string  `json:"currentVersion"`
	Version              string  `json:"version"`
	DisplayVersion       string  `json:"displayVersion"`
	Architecture         string  `json:"architecture"`
	ReleaseNotes         string  `json:"releaseNotes"`
	ContentLength        string  `json:"contentLength"`
	SignatureStatus      string  `json:"signatureStatus"`
	ProgressStage        string  `json:"progressStage"`
	ProgressReceived     string  `json:"progressReceived"`
	ProgressTotal        string  `json:"progressTotal"`
	ProgressFraction     float64 `json:"progressFraction"`
	FaultCode            string  `json:"faultCode"`
	CanCancel            bool    `json:"canCancel"`
	ReadyToInstall       bool    `json:"readyToInstall"`
	ShutdownPhase        string  `json:"shutdownPhase"`
	ShutdownStage        string  `json:"shutdownStage"`
	ShutdownFailedStage  string  `json:"shutdownFailedStage"`
	AutoCheckEnabled     bool    `json:"autoCheckEnabled"`
	CheckIntervalSeconds int64   `json:"checkIntervalSeconds"`
	SkippedVersion       *string `json:"skippedVersion"`
	SnoozeUntilMS        *int64  `json:"snoozeUntilMs"`
	LastCheckAtMS        *int64  `json:"lastCheckAtMs"`
	PromptVisible        bool    `json:"promptVisible"`
}

type UpdateTriggerReceipt struct {
	Accepted    bool   `json:"accepted"`
	Reason      string `json:"reason"`
	CheckedAtMS *int64 `json:"checkedAtMs"`
}

type UpdateActionReceipt struct {
	Result string `json:"result"`
}

func (service *Service) UpdateState(ctx context.Context) (UpdateStateResponse, error) {
	command := service.updateControlsCommand()
	if command == nil {
		return UpdateStateResponse{}, newBindingFailure(ErrBindingService)
	}
	return bindingCall(func() (UpdateStateResponse, error) {
		view, err := command.View(ctx)
		if err != nil {
			return UpdateStateResponse{}, err
		}
		return updateStateResponse(view, command.InstallState()), nil
	})
}

func (service *Service) CheckForUpdates(ctx context.Context) (UpdateTriggerReceipt, error) {
	command := service.updateControlsCommand()
	if command == nil {
		return UpdateTriggerReceipt{}, newBindingFailure(ErrBindingService)
	}
	return bindingCall(func() (UpdateTriggerReceipt, error) {
		receipt, err := command.Trigger(ctx, updater.TriggerManual)
		return UpdateTriggerReceipt{
			Accepted: receipt.Accepted, Reason: string(receipt.Reason),
			CheckedAtMS: cloneBindingInt64(receipt.CheckedAtMS),
		}, err
	})
}

func (service *Service) DownloadUpdate(ctx context.Context) (UpdateActionReceipt, error) {
	return service.runUpdateAction(ctx, "download_requested", func(command updateBindingCommand) error {
		return command.Download(ctx)
	})
}

func (service *Service) InstallUpdate(ctx context.Context) (UpdateActionReceipt, error) {
	return service.runUpdateAction(ctx, "install_requested", func(command updateBindingCommand) error {
		return command.Install(ctx)
	})
}

func (service *Service) CancelUpdate(ctx context.Context) (UpdateActionReceipt, error) {
	return service.runUpdateAction(ctx, "cancel_requested", func(command updateBindingCommand) error {
		return command.Cancel(ctx)
	})
}

func (service *Service) SkipUpdate(ctx context.Context, version string) (UpdateActionReceipt, error) {
	if version == "" || len(version) > 128 {
		return UpdateActionReceipt{}, newBindingFailure(basequery.NewValidationFailure("version", nil))
	}
	return service.runUpdateAction(ctx, "skipped", func(command updateBindingCommand) error {
		return command.Skip(ctx, version)
	})
}

func (service *Service) SnoozeUpdate(ctx context.Context, seconds int64) (UpdateActionReceipt, error) {
	if seconds < minimumUpdateSnoozeSeconds || seconds > maximumUpdateSnoozeSeconds {
		return UpdateActionReceipt{}, newBindingFailure(basequery.NewValidationFailure("seconds", nil))
	}
	return service.runUpdateAction(ctx, "snoozed", func(command updateBindingCommand) error {
		return command.Snooze(ctx, time.Duration(seconds)*time.Second)
	})
}

func (service *Service) runUpdateAction(
	_ context.Context,
	result string,
	run func(updateBindingCommand) error,
) (UpdateActionReceipt, error) {
	command := service.updateControlsCommand()
	if command == nil {
		return UpdateActionReceipt{}, newBindingFailure(ErrBindingService)
	}
	return bindingCall(func() (UpdateActionReceipt, error) {
		if err := run(command); err != nil {
			return UpdateActionReceipt{}, err
		}
		return UpdateActionReceipt{Result: result}, nil
	})
}

func (service *Service) updateControlsCommand() updateBindingCommand {
	if service == nil {
		return nil
	}
	service.updateMu.RLock()
	defer service.updateMu.RUnlock()
	return service.updateControls
}

func updateStateResponse(view updater.View, shutdown shutdownSnapshot) UpdateStateResponse {
	response := UpdateStateResponse{
		Phase: string(view.Snapshot.Phase), CurrentVersion: applicationVersion,
		ProgressStage:    string(view.Snapshot.Progress.Stage),
		ProgressReceived: strconv.FormatUint(view.Snapshot.Progress.Received, 10),
		ProgressTotal:    strconv.FormatUint(view.Snapshot.Progress.Total, 10),
		ProgressFraction: view.Snapshot.Progress.Fraction,
		CanCancel:        view.Snapshot.CanCancel, ReadyToInstall: view.Snapshot.ReadyToInstall,
		AutoCheckEnabled: view.AutoCheckEnabled, CheckIntervalSeconds: view.CheckIntervalSeconds,
		SkippedVersion: cloneBindingString(view.SkippedVersion), SnoozeUntilMS: cloneBindingInt64(view.SnoozeUntilMS),
		LastCheckAtMS: cloneBindingInt64(view.LastCheckAtMS), PromptVisible: view.PromptVisible,
		ShutdownPhase: string(shutdown.Phase), ShutdownStage: shutdown.Stage,
		ShutdownFailedStage: shutdown.FailedStage,
	}
	if view.Snapshot.Update != nil {
		response.Version = view.Snapshot.Update.Version
		response.DisplayVersion = view.Snapshot.Update.DisplayVersion
		response.Architecture = view.Snapshot.Update.Architecture
		response.ReleaseNotes = view.Snapshot.Update.ReleaseNotes
		response.ContentLength = strconv.FormatUint(view.Snapshot.Update.ContentLength, 10)
		response.SignatureStatus = string(view.Snapshot.Update.FeedSignatureStatus)
	}
	if view.Snapshot.Fault != nil {
		response.FaultCode = string(view.Snapshot.Fault.Code)
	}
	return response
}

func cloneBindingString(value *string) *string {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}
