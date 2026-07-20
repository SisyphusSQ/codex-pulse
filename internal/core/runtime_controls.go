package core

import (
	"context"
	"path/filepath"
	"strconv"

	basequery "github.com/SisyphusSQ/codex-pulse/internal/query"
)

type runtimeControlCommand interface {
	UpdateSettings(context.Context, SettingsUpdateRequest) (SettingsUpdateReceipt, error)
	PlanHomeSwitch(context.Context, HomeSwitchPlanRequest) (HomeSwitchPlanReceipt, error)
	ConfirmHomeSwitch(context.Context) (HomeSwitchReceipt, error)
	RecoverHomeSwitch(context.Context) (HomeSwitchReceipt, error)
	RunRuntimeAction(context.Context, RuntimeAction) (RuntimeActionReceipt, error)
	AnalyzeSessionIndexRepair(context.Context) (RepairDryRunReceipt, error)
}

type SettingsUpdateRequest struct {
	ExpectedRevision string                `json:"expectedRevision"`
	Online           SettingsOnlineUpdate  `json:"online"`
	Refresh          SettingsRefreshUpdate `json:"refresh"`
	Updates          SettingsUpdatesUpdate `json:"updates"`
	UI               SettingsUIUpdate      `json:"ui"`
}

type SettingsOnlineUpdate struct {
	QuotaEnabled        bool `json:"quotaEnabled"`
	ResetCreditsEnabled bool `json:"resetCreditsEnabled"`
}

type SettingsRefreshUpdate struct {
	QuotaIntervalSeconds        int64 `json:"quotaIntervalSeconds"`
	ResetCreditsIntervalSeconds int64 `json:"resetCreditsIntervalSeconds"`
	ReconcileIntervalSeconds    int64 `json:"reconcileIntervalSeconds"`
	JSONLDebounceMilliseconds   int64 `json:"jsonlDebounceMilliseconds"`
}

type SettingsUpdatesUpdate struct {
	AutoCheckEnabled     bool  `json:"autoCheckEnabled"`
	CheckIntervalSeconds int64 `json:"checkIntervalSeconds"`
}

type SettingsUIUpdate struct {
	LaunchBehavior string `json:"launchBehavior"`
	OverviewRange  string `json:"overviewRange"`
}

type SettingsUpdateResult string

const (
	SettingsUpdateApplied           SettingsUpdateResult = "applied"
	SettingsUpdateReconcileRequired SettingsUpdateResult = "applied_reconcile_required"
)

type SettingsUpdateReceipt struct {
	Revision string               `json:"revision"`
	Result   SettingsUpdateResult `json:"result"`
}

type HomeSwitchStrategy string

const (
	HomeSwitchIndependentDatabase HomeSwitchStrategy = "independent_database"
	HomeSwitchClearAndRebuild     HomeSwitchStrategy = "clear_and_rebuild"
)

type HomeSwitchPlanRequest struct {
	TargetPath string             `json:"targetPath"`
	Strategy   HomeSwitchStrategy `json:"strategy"`
}

type HomeSwitchPlanReceipt struct {
	Strategy           HomeSwitchStrategy `json:"strategy"`
	TargetGeneration   string             `json:"targetGeneration"`
	PreservesOldFacts  bool               `json:"preservesOldFacts"`
	ClearsDerivedFacts bool               `json:"clearsDerivedFacts"`
}

type HomeSwitchResult string

const (
	HomeSwitchCompletedResult  HomeSwitchResult = "completed"
	HomeSwitchRolledBackResult HomeSwitchResult = "rolled_back"
	HomeSwitchRecoveryRequired HomeSwitchResult = "recovery_required"
)

type HomeSwitchReceipt struct {
	Revision   string           `json:"revision"`
	Generation string           `json:"generation"`
	Result     HomeSwitchResult `json:"result"`
}

type RuntimeAction string

const (
	RuntimeActionPauseBackfill RuntimeAction = "pause_backfill"
	RuntimeActionPauseAll      RuntimeAction = "pause_all"
	RuntimeActionResume        RuntimeAction = "resume"
	RuntimeActionReconcile     RuntimeAction = "reconcile"
)

type RuntimeActionReceipt struct {
	Action      RuntimeAction `json:"action"`
	PauseScope  string        `json:"pauseScope"`
	SourceState string        `json:"sourceState"`
	Transition  string        `json:"transition"`
}

type RepairDryRunReceipt struct {
	AnalyzedAtMS    int64 `json:"analyzedAtMs"`
	ActionCount     int64 `json:"actionCount"`
	ConflictCount   int64 `json:"conflictCount"`
	HistoryCount    int64 `json:"historyCount"`
	DiagnosticCount int64 `json:"diagnosticCount"`
	Noop            bool  `json:"noop"`
}

func (service *Service) UpdateSettings(
	ctx context.Context,
	request SettingsUpdateRequest,
) (SettingsUpdateReceipt, error) {
	command := service.runtimeControlsCommand()
	if command == nil {
		return SettingsUpdateReceipt{}, newServiceFailure(ErrService)
	}
	if !validSettingsUpdateRequest(request) {
		return SettingsUpdateReceipt{}, newServiceFailure(basequery.NewValidationFailure("settings", nil))
	}
	return serviceCall(func() (SettingsUpdateReceipt, error) {
		return command.UpdateSettings(ctx, request)
	})
}

func (service *Service) PlanHomeSwitch(
	ctx context.Context,
	request HomeSwitchPlanRequest,
) (HomeSwitchPlanReceipt, error) {
	command := service.runtimeControlsCommand()
	if command == nil {
		return HomeSwitchPlanReceipt{}, newServiceFailure(ErrService)
	}
	if !filepath.IsAbs(request.TargetPath) || filepath.Clean(request.TargetPath) != request.TargetPath {
		return HomeSwitchPlanReceipt{}, newServiceFailure(basequery.NewValidationFailure("targetPath", nil))
	}
	if !validHomeSwitchStrategy(request.Strategy) {
		return HomeSwitchPlanReceipt{}, newServiceFailure(basequery.NewValidationFailure("strategy", nil))
	}
	return serviceCall(func() (HomeSwitchPlanReceipt, error) {
		return command.PlanHomeSwitch(ctx, request)
	})
}

func (service *Service) ConfirmHomeSwitch(ctx context.Context) (HomeSwitchReceipt, error) {
	command := service.runtimeControlsCommand()
	if command == nil {
		return HomeSwitchReceipt{}, newServiceFailure(ErrService)
	}
	return serviceCall(func() (HomeSwitchReceipt, error) { return command.ConfirmHomeSwitch(ctx) })
}

func (service *Service) RecoverHomeSwitch(ctx context.Context) (HomeSwitchReceipt, error) {
	command := service.runtimeControlsCommand()
	if command == nil {
		return HomeSwitchReceipt{}, newServiceFailure(ErrService)
	}
	return serviceCall(func() (HomeSwitchReceipt, error) { return command.RecoverHomeSwitch(ctx) })
}

func (service *Service) RunRuntimeAction(
	ctx context.Context,
	action RuntimeAction,
) (RuntimeActionReceipt, error) {
	command := service.runtimeControlsCommand()
	if command == nil {
		return RuntimeActionReceipt{}, newServiceFailure(ErrService)
	}
	if !validRuntimeAction(action) {
		return RuntimeActionReceipt{}, newServiceFailure(basequery.NewValidationFailure("action", nil))
	}
	return serviceCall(func() (RuntimeActionReceipt, error) {
		return command.RunRuntimeAction(ctx, action)
	})
}

func (service *Service) AnalyzeSessionIndexRepair(ctx context.Context) (RepairDryRunReceipt, error) {
	command := service.runtimeControlsCommand()
	if command == nil {
		return RepairDryRunReceipt{}, newServiceFailure(ErrService)
	}
	return serviceCall(func() (RepairDryRunReceipt, error) {
		return command.AnalyzeSessionIndexRepair(ctx)
	})
}

func (service *Service) runtimeControlsCommand() runtimeControlCommand {
	if service == nil {
		return nil
	}
	service.runtimeMu.RLock()
	defer service.runtimeMu.RUnlock()
	return service.runtimeControls
}

func validSettingsUpdateRequest(request SettingsUpdateRequest) bool {
	revision, err := strconv.ParseUint(request.ExpectedRevision, 10, 64)
	return err == nil && revision > 0 &&
		request.Refresh.QuotaIntervalSeconds >= 60 && request.Refresh.QuotaIntervalSeconds <= 1800 &&
		request.Refresh.ResetCreditsIntervalSeconds >= 60 && request.Refresh.ResetCreditsIntervalSeconds <= 86400 &&
		request.Refresh.ReconcileIntervalSeconds >= 60 && request.Refresh.ReconcileIntervalSeconds <= 86400 &&
		request.Refresh.JSONLDebounceMilliseconds >= 3000 && request.Refresh.JSONLDebounceMilliseconds <= 5000 &&
		request.Updates.CheckIntervalSeconds >= 3600 && request.Updates.CheckIntervalSeconds <= 86400 &&
		(request.UI.LaunchBehavior == "main_window" || request.UI.LaunchBehavior == "tray") &&
		(request.UI.OverviewRange == "today" || request.UI.OverviewRange == "seven_days" ||
			request.UI.OverviewRange == "thirty_days")
}

func validHomeSwitchStrategy(value HomeSwitchStrategy) bool {
	return value == HomeSwitchIndependentDatabase || value == HomeSwitchClearAndRebuild
}

func validRuntimeAction(value RuntimeAction) bool {
	switch value {
	case RuntimeActionPauseBackfill, RuntimeActionPauseAll, RuntimeActionResume, RuntimeActionReconcile:
		return true
	default:
		return false
	}
}
