package app

import (
	"context"
	"errors"
	"strconv"

	"github.com/google/uuid"

	codexindex "github.com/SisyphusSQ/codex-pulse/internal/codex/index"
	logsource "github.com/SisyphusSQ/codex-pulse/internal/codex/logs/source"
	"github.com/SisyphusSQ/codex-pulse/internal/core"
	"github.com/SisyphusSQ/codex-pulse/internal/preferences"
	basequery "github.com/SisyphusSQ/codex-pulse/internal/query"
	"github.com/SisyphusSQ/codex-pulse/internal/store"
)

func (runtime *applicationLifecycleRuntime) UpdateSettings(
	ctx context.Context,
	request core.SettingsUpdateRequest,
) (core.SettingsUpdateReceipt, error) {
	if runtime == nil || runtime.settingsLoader == nil {
		return core.SettingsUpdateReceipt{}, basequery.NewUnavailableFailure(ErrApplicationLifecycleRuntime)
	}
	expectedRevision, err := strconv.ParseUint(request.ExpectedRevision, 10, 64)
	if err != nil || expectedRevision == 0 {
		return core.SettingsUpdateReceipt{}, basequery.NewValidationFailure("settings", err)
	}
	current, err := runtime.settingsLoader.LoadPreferences(ctx)
	if err != nil {
		return core.SettingsUpdateReceipt{}, publicRuntimeCommandFailure(err)
	}
	update := preferences.SettingsUpdate{
		ExpectedRevision: expectedRevision,
		Online: preferences.OnlinePreferences{
			QuotaEnabled:        request.Online.QuotaEnabled,
			ResetCreditsEnabled: request.Online.ResetCreditsEnabled,
		},
		Refresh: preferences.RefreshPreferences{
			QuotaIntervalSeconds:        request.Refresh.QuotaIntervalSeconds,
			ResetCreditsIntervalSeconds: request.Refresh.ResetCreditsIntervalSeconds,
			ReconcileIntervalSeconds:    request.Refresh.ReconcileIntervalSeconds,
			JSONLDebounceMilliseconds:   request.Refresh.JSONLDebounceMilliseconds,
		},
		Updates: current.Updates,
		UI:      current.UI,
	}
	update.Updates.AutoCheckEnabled = request.Updates.AutoCheckEnabled
	update.Updates.CheckIntervalSeconds = request.Updates.CheckIntervalSeconds
	update.Updates.Channel = preferences.UpdateChannel(request.Updates.Channel)
	update.UI.LaunchBehavior = preferences.LaunchBehavior(request.UI.LaunchBehavior)
	update.UI.OverviewRange = preferences.OverviewRange(request.UI.OverviewRange)

	committed, updateErr := runtime.UpdateQuotaSettings(ctx, update)
	receipt := core.SettingsUpdateReceipt{Revision: strconv.FormatUint(committed.Revision, 10)}
	if updateErr == nil {
		receipt.Result = core.SettingsUpdateApplied
		return receipt, nil
	}
	var postCommit *ApplicationPreferencesPostCommitError
	if errors.As(updateErr, &postCommit) && committed.Revision > 0 {
		receipt.Result = core.SettingsUpdateReconcileRequired
		return receipt, nil
	}
	return core.SettingsUpdateReceipt{}, publicRuntimeCommandFailure(updateErr)
}

func (runtime *applicationLifecycleRuntime) PlanHomeSwitch(
	ctx context.Context,
	request core.HomeSwitchPlanRequest,
) (core.HomeSwitchPlanReceipt, error) {
	strategy := preferences.HomeSwitchStrategy(request.Strategy)
	plan, err := runtime.PlanQuotaHomeSwitch(ctx, request.TargetPath, strategy)
	if err != nil {
		return core.HomeSwitchPlanReceipt{}, publicRuntimeCommandFailure(err)
	}
	runtime.homePlanMu.Lock()
	runtime.homePlanID = plan.ID
	runtime.homePlanMu.Unlock()
	return core.HomeSwitchPlanReceipt{
		Strategy: request.Strategy, TargetGeneration: strconv.FormatUint(plan.Target.Generation, 10),
		PreservesOldFacts:  plan.Impact.PreservesOldFacts,
		ClearsDerivedFacts: plan.Impact.ClearsDerivedFacts,
	}, nil
}

func (runtime *applicationLifecycleRuntime) ConfirmHomeSwitch(ctx context.Context) (core.HomeSwitchReceipt, error) {
	if runtime == nil {
		return core.HomeSwitchReceipt{}, basequery.NewUnavailableFailure(ErrApplicationLifecycleRuntime)
	}
	runtime.homePlanMu.Lock()
	planID := runtime.homePlanID
	runtime.homePlanMu.Unlock()
	if planID == "" {
		return core.HomeSwitchReceipt{}, basequery.NewUnavailableFailure(preferences.ErrSwitchPlanNotFound)
	}
	snapshot, err := runtime.ConfirmQuotaHomeSwitch(ctx, planID)
	runtime.notifyHomeSwitchInvalidation(ctx, snapshot)
	if err != nil {
		var postCommit *ApplicationPreferencesPostCommitError
		if errors.As(err, &postCommit) && snapshot.Revision > 0 {
			runtime.clearHomePlanIfCurrent(planID)
			receipt := redactedHomeSwitchReceipt(snapshot)
			receipt.Result = core.HomeSwitchRecoveryRequired
			return receipt, nil
		}
		return core.HomeSwitchReceipt{}, publicRuntimeCommandFailure(err)
	}
	runtime.clearHomePlanIfCurrent(planID)
	return redactedHomeSwitchReceipt(snapshot), nil
}

func (runtime *applicationLifecycleRuntime) RecoverHomeSwitch(ctx context.Context) (core.HomeSwitchReceipt, error) {
	if runtime == nil {
		return core.HomeSwitchReceipt{}, basequery.NewUnavailableFailure(ErrApplicationLifecycleRuntime)
	}
	runtime.homePlanMu.Lock()
	runtime.homePlanID = ""
	runtime.homePlanMu.Unlock()
	snapshot, err := runtime.RecoverQuotaHomeSwitch(ctx)
	runtime.notifyHomeSwitchInvalidation(ctx, snapshot)
	if err != nil {
		var postCommit *ApplicationPreferencesPostCommitError
		if errors.As(err, &postCommit) && snapshot.Revision > 0 {
			receipt := redactedHomeSwitchReceipt(snapshot)
			receipt.Result = core.HomeSwitchRecoveryRequired
			return receipt, nil
		}
		return core.HomeSwitchReceipt{}, publicRuntimeCommandFailure(err)
	}
	return redactedHomeSwitchReceipt(snapshot), nil
}

func (runtime *applicationLifecycleRuntime) clearHomePlanIfCurrent(planID string) {
	if runtime == nil || planID == "" {
		return
	}
	runtime.homePlanMu.Lock()
	defer runtime.homePlanMu.Unlock()
	if runtime.homePlanID == planID {
		runtime.homePlanID = ""
	}
}

func (runtime *applicationLifecycleRuntime) notifyHomeSwitchInvalidation(
	ctx context.Context,
	snapshot preferences.Snapshot,
) {
	if runtime == nil || ctx == nil || snapshot.Revision == 0 {
		return
	}
	notifyQueryInvalidation(runtime.invalidation, ctx, core.InvalidationIndex)
	notifyQueryInvalidation(runtime.invalidation, ctx, core.InvalidationSettings)
}

func (runtime *applicationLifecycleRuntime) RunRuntimeAction(
	ctx context.Context,
	action core.RuntimeAction,
) (core.RuntimeActionReceipt, error) {
	if runtime == nil || runtime.coordinator == nil || ctx == nil {
		return core.RuntimeActionReceipt{}, basequery.NewUnavailableFailure(ErrApplicationLifecycleRuntime)
	}
	operationContext, finish, err := runtime.beginControlAdmission(ctx)
	if err != nil {
		return core.RuntimeActionReceipt{}, publicRuntimeCommandFailure(err)
	}
	defer finish()
	eventID := "user:" + string(action) + ":" + uuid.NewString()
	var state store.SchedulerLifecycle
	switch action {
	case core.RuntimeActionPauseBackfill:
		state, err = runtime.coordinator.Pause(operationContext, eventID, store.LifecyclePauseBackfill)
	case core.RuntimeActionPauseAll:
		state, err = runtime.coordinator.Pause(operationContext, eventID, store.LifecyclePauseAll)
	case core.RuntimeActionResume:
		state, err = runtime.coordinator.Resume(operationContext, eventID)
	case core.RuntimeActionReconcile:
		state, err = runtime.coordinator.SourceChanged(operationContext, eventID, true)
	default:
		return core.RuntimeActionReceipt{}, basequery.NewValidationFailure("action", nil)
	}
	if err != nil && !nonFatalSourceStateError(err) {
		return core.RuntimeActionReceipt{}, publicRuntimeCommandFailure(err)
	}
	notifyQueryInvalidation(runtime.invalidation, operationContext, core.InvalidationIndex)
	return core.RuntimeActionReceipt{
		Action: action, PauseScope: string(state.UserPauseScope),
		SourceState: string(state.SourceState), Transition: string(state.Transition),
	}, nil
}

func (runtime *applicationLifecycleRuntime) AnalyzeSessionIndexRepair(
	ctx context.Context,
) (core.RepairDryRunReceipt, error) {
	if runtime == nil || runtime.database == nil || runtime.settingsLoader == nil || ctx == nil {
		return core.RepairDryRunReceipt{}, basequery.NewUnavailableFailure(ErrApplicationLifecycleRuntime)
	}
	operationContext, finish, err := runtime.beginControlAdmission(ctx)
	if err != nil {
		return core.RepairDryRunReceipt{}, publicRuntimeCommandFailure(err)
	}
	defer finish()
	home, err := fileConfirmedHomeProvider{loader: runtime.settingsLoader}.CurrentHome(operationContext)
	if err != nil {
		return core.RepairDryRunReceipt{}, publicRuntimeCommandFailure(err)
	}
	metadata, err := logsource.NewHomeProbe().Probe(operationContext, home.Path)
	if err != nil || metadata.Path != home.Path || metadata.DeviceID != home.DeviceID || metadata.Inode != home.Inode {
		if err == nil {
			err = ErrApplicationLifecycleRuntime
		}
		return core.RepairDryRunReceipt{}, basequery.NewUnavailableFailure(err)
	}
	service, err := codexindex.NewService(store.NewRepository(runtime.database), runtime.database, home.Path)
	if err != nil {
		return core.RepairDryRunReceipt{}, publicRuntimeCommandFailure(err)
	}
	plan, err := service.Analyze(operationContext)
	if err != nil {
		return core.RepairDryRunReceipt{}, publicRuntimeCommandFailure(err)
	}
	return core.RepairDryRunReceipt{
		AnalyzedAtMS: plan.AnalyzedAtMS, ActionCount: int64(len(plan.Actions)),
		ConflictCount: int64(len(plan.Conflicts)), HistoryCount: int64(len(plan.Histories)),
		DiagnosticCount: int64(len(plan.Diagnostics)),
		Noop:            len(plan.Actions) == 0 && len(plan.Conflicts) == 0,
	}, nil
}

func redactedHomeSwitchReceipt(snapshot preferences.Snapshot) core.HomeSwitchReceipt {
	result := core.HomeSwitchCompletedResult
	if snapshot.PendingSwitch != nil || snapshot.PendingResume != nil {
		result = core.HomeSwitchRecoveryRequired
	} else if snapshot.LastSwitch != nil && snapshot.LastSwitch.Outcome == preferences.HomeSwitchRolledBack {
		result = core.HomeSwitchRolledBackResult
	}
	return core.HomeSwitchReceipt{
		Revision:   strconv.FormatUint(snapshot.Revision, 10),
		Generation: strconv.FormatUint(snapshot.CodexHome.Generation, 10), Result: result,
	}
}

func publicRuntimeCommandFailure(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) ||
		errors.Is(err, basequery.ErrValidation) || errors.Is(err, basequery.ErrUnavailable) {
		return err
	}
	return basequery.NewUnavailableFailure(err)
}
