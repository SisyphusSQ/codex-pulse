package app

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/SisyphusSQ/codex-pulse/internal/agentprovider"
	logsource "github.com/SisyphusSQ/codex-pulse/internal/codex/logs/source"
	quotaonline "github.com/SisyphusSQ/codex-pulse/internal/codex/quota"
	"github.com/SisyphusSQ/codex-pulse/internal/core"
	"github.com/SisyphusSQ/codex-pulse/internal/cursorprovider"
	"github.com/SisyphusSQ/codex-pulse/internal/grokprovider"
	"github.com/SisyphusSQ/codex-pulse/internal/lightindex"
	"github.com/SisyphusSQ/codex-pulse/internal/preferences"
	"github.com/SisyphusSQ/codex-pulse/internal/providercontrol"
	"github.com/SisyphusSQ/codex-pulse/internal/providerrefresh"
	basequery "github.com/SisyphusSQ/codex-pulse/internal/query"
	"github.com/SisyphusSQ/codex-pulse/internal/store"
	storesqlite "github.com/SisyphusSQ/codex-pulse/internal/store/sqlite"
)

type deferredHomeRuntime struct {
	mu    sync.Mutex
	inner preferences.HomeRuntime
}

func (runtime *deferredHomeRuntime) bind(inner preferences.HomeRuntime) {
	if runtime == nil {
		return
	}
	runtime.mu.Lock()
	runtime.inner = inner
	runtime.mu.Unlock()
}

func (runtime *deferredHomeRuntime) current() preferences.HomeRuntime {
	if runtime == nil {
		return nil
	}
	runtime.mu.Lock()
	defer runtime.mu.Unlock()
	return runtime.inner
}

func (runtime *deferredHomeRuntime) Drain(ctx context.Context, generation uint64) error {
	if inner := runtime.current(); inner != nil {
		return inner.Drain(ctx, generation)
	}
	return nil
}

func (runtime *deferredHomeRuntime) StartBootstrap(ctx context.Context, request preferences.BootstrapRequest) error {
	if inner := runtime.current(); inner != nil {
		return inner.StartBootstrap(ctx, request)
	}
	return nil
}

func (runtime *deferredHomeRuntime) BootstrapStatus(
	ctx context.Context,
	switchID string,
	generation uint64,
) (preferences.BootstrapStatus, error) {
	if inner := runtime.current(); inner != nil {
		return inner.BootstrapStatus(ctx, switchID, generation)
	}
	return preferences.BootstrapStatusNotStarted, nil
}

func (runtime *deferredHomeRuntime) Resume(ctx context.Context, generation uint64) error {
	if inner := runtime.current(); inner != nil {
		return inner.Resume(ctx, generation)
	}
	return nil
}

type ApplicationControlRuntimeConfig struct {
	Database         *storesqlite.Store
	Preferences      *preferences.FileStore
	Controller       *providercontrol.Controller
	Invalidation     queryInvalidationNotifier
	LightMetadata    lightindex.MetadataProvider
	DefaultCodexHome string
	EventTimeout     time.Duration
}

type applicationControlRuntime struct {
	controller      *providercontrol.Controller
	preferences     *preferences.Service
	preferenceStore *preferences.FileStore
	homeRuntime     *deferredHomeRuntime
	invalidation    queryInvalidationNotifier
	database        *storesqlite.Store
	lightMetadata   lightindex.MetadataProvider
	adapter         *LifecycleEventAdapter

	cursorAccountReader cursorAccountReader
	grokAccountReader   grokAccountReader
	grokProfileReader   grokProfileReader
	repository          *store.Repository

	controlCtx  context.Context
	controlStop context.CancelFunc

	mu           sync.Mutex
	worker       *applicationLifecycleRuntime
	orchestrator *providerrefresh.Orchestrator
	homePlanID   string
	closeOnce    sync.Once
	closeErr     error
}

const providerDiscoveryInterval = 5 * time.Minute

func startApplicationControlRuntime(
	ctx context.Context,
	config ApplicationControlRuntimeConfig,
) (*applicationControlRuntime, error) {
	if ctx == nil || config.Preferences == nil || config.Controller == nil {
		return nil, ErrApplicationLifecycleRuntime
	}
	homeRuntime := &deferredHomeRuntime{}
	service, err := preferences.NewService(preferences.ServiceConfig{
		Store: config.Preferences, Probe: logsource.NewHomeProbe(), Runtime: homeRuntime,
	})
	if err != nil {
		return nil, err
	}
	if _, err := service.RecoverSwitch(ctx); err != nil {
		return nil, err
	}
	controlCtx, controlStop := context.WithCancel(ctx)
	runtime := &applicationControlRuntime{
		controller:      config.Controller,
		preferences:     service,
		preferenceStore: config.Preferences,
		homeRuntime:     homeRuntime,
		invalidation:    config.Invalidation,
		database:        config.Database,
		lightMetadata:   config.LightMetadata,
		controlCtx:      controlCtx,
		controlStop:     controlStop,
	}
	config.Controller.SetSettledNotifier(runtime.reconcileSettledProviderState)
	if config.Database != nil {
		runtime.repository = store.NewRepository(config.Database)
	}
	runtime.installAccountReaders()
	adapter, err := NewLifecycleEventAdapter(LifecycleEventAdapterConfig{
		Coordinator:  &controlLifecycleCoordinator{runtime: runtime},
		EventTimeout: config.EventTimeout,
		DidWake: func(wakeCtx context.Context) error {
			err := runtime.refreshDiscoveryAndWorkers(wakeCtx, providerrefresh.TriggerWake)
			runtime.refreshGlobal(wakeCtx, providerrefresh.TriggerWake)
			return err
		},
	})
	if err != nil {
		controlStop()
		return nil, err
	}
	runtime.adapter = adapter
	if _, err := runtime.controller.RefreshDiscovery(ctx, providerrefresh.TriggerStartup); err != nil {
		_ = adapter.Close(context.Background())
		controlStop()
		return nil, err
	}
	return runtime, nil
}

func (runtime *applicationControlRuntime) installAccountReaders() {
	if cursorConfig, err := cursorprovider.DefaultConfig(); err == nil {
		if cursorAuth, authErr := cursorprovider.NewDesktopAuthReader(cursorConfig.StateDatabase, time.Now); authErr == nil {
			runtime.cursorAccountReader = cursorAuth.ReadAccountSnapshot
		}
	}
	if grokConfig, err := grokprovider.DefaultConfig(); err == nil {
		if grokAuth, authErr := grokprovider.NewAuthReader(
			grokConfig.AuthPath,
			time.Now,
			grokprovider.AuthReaderConfig{RefreshEnabled: func() bool {
				if admitProviderControl(runtime.controller, agentprovider.Grok) != nil {
					return false
				}
				current, loadErr := runtime.preferenceStore.LoadPreferences(context.Background())
				return loadErr == nil && current.Online.GrokAutoRefreshEnabled
			}},
		); authErr == nil {
			runtime.grokAccountReader = grokAuth.ReadAccountSnapshot
			if accountClient, accountErr := grokprovider.NewAccountClient(grokprovider.AccountClientConfig{
				BaseURL:     grokConfig.BillingBaseURL,
				HTTPClient:  &http.Client{Timeout: 15 * time.Second},
				TokenSource: grokAuth,
			}); accountErr == nil {
				runtime.grokProfileReader = accountClient.GetAccount
			}
		}
	}
}

func (runtime *applicationControlRuntime) SetGlobalRefresh(orchestrator *providerrefresh.Orchestrator) {
	if runtime == nil || orchestrator == nil {
		return
	}
	runtime.mu.Lock()
	runtime.orchestrator = orchestrator
	worker := runtime.worker
	runtime.mu.Unlock()
	if worker != nil {
		orchestrator.BindCodex(providerrefresh.NewCodexAdapter(worker, worker, nil))
	}
	if runtime.controlCtx != nil {
		go runtime.runProviderRefreshLoop(orchestrator)
	}
}

func (runtime *applicationControlRuntime) AttachWorker(worker *applicationLifecycleRuntime) {
	if runtime == nil || worker == nil {
		return
	}
	runtime.mu.Lock()
	runtime.worker = worker
	orchestrator := runtime.orchestrator
	runtime.mu.Unlock()
	if orchestrator != nil {
		orchestrator.BindCodex(providerrefresh.NewCodexAdapter(worker, worker, nil))
	}
}

func (runtime *applicationControlRuntime) bindHomeRuntime(inner preferences.HomeRuntime) {
	if runtime == nil || runtime.homeRuntime == nil {
		return
	}
	runtime.homeRuntime.bind(inner)
}

func (runtime *applicationControlRuntime) currentWorker() *applicationLifecycleRuntime {
	if runtime == nil {
		return nil
	}
	runtime.mu.Lock()
	defer runtime.mu.Unlock()
	return runtime.worker
}

func (runtime *applicationControlRuntime) refreshGlobal(ctx context.Context, trigger string) {
	if runtime == nil {
		return
	}
	runtime.mu.Lock()
	orchestrator := runtime.orchestrator
	runtime.mu.Unlock()
	if orchestrator == nil {
		return
	}
	_, _ = orchestrator.Refresh(ctx, trigger)
}

func (runtime *applicationControlRuntime) Refresh(ctx context.Context) error {
	return runtime.refreshDiscoveryAndWorkers(ctx, "settings")
}

func (runtime *applicationControlRuntime) refreshDiscoveryAndWorkers(
	ctx context.Context,
	trigger string,
) error {
	if runtime == nil || runtime.controller == nil || ctx == nil {
		return ErrApplicationLifecycleRuntime
	}
	before := runtime.controller.Generation()
	if _, err := runtime.controller.RefreshDiscovery(ctx, trigger); err != nil {
		return err
	}
	if err := runtime.syncCodexWorker(ctx); err != nil {
		return err
	}
	if runtime.controller.Generation() != before {
		runtime.notifyProviderStateChanged(ctx)
	}
	return nil
}

func (runtime *applicationControlRuntime) runProviderRefreshLoop(
	orchestrator *providerrefresh.Orchestrator,
) {
	if runtime == nil || orchestrator == nil || runtime.controlCtx == nil {
		return
	}
	_, _ = orchestrator.Refresh(runtime.controlCtx, providerrefresh.TriggerStartup)
	timer := time.NewTimer(providerDiscoveryInterval)
	defer timer.Stop()
	for {
		select {
		case <-runtime.controlCtx.Done():
			return
		case <-timer.C:
			_ = runtime.refreshDiscoveryAndWorkers(runtime.controlCtx, providerrefresh.TriggerScheduled)
			_, _ = orchestrator.Refresh(runtime.controlCtx, providerrefresh.TriggerScheduled)
			timer.Reset(providerDiscoveryInterval)
		}
	}
}

func (runtime *applicationControlRuntime) notifyProviderStateChanged(ctx context.Context) {
	notifyQueryInvalidation(runtime.invalidation, ctx, core.InvalidationSettings)
	notifyQueryInvalidation(runtime.invalidation, ctx, core.InvalidationIndex)
	notifyQueryInvalidation(runtime.invalidation, ctx, core.InvalidationQuota)
	notifyQueryInvalidation(runtime.invalidation, ctx, core.InvalidationAccount)
}

func (runtime *applicationControlRuntime) reconcileSettledProviderState() {
	if runtime == nil || runtime.controlCtx == nil || runtime.controlCtx.Err() != nil {
		return
	}
	_ = runtime.refreshDiscoveryAndWorkers(runtime.controlCtx, "settled")
	runtime.notifyProviderStateChanged(runtime.controlCtx)
}

func (runtime *applicationControlRuntime) UpdateSettings(
	ctx context.Context,
	request core.SettingsUpdateRequest,
) (core.SettingsUpdateReceipt, error) {
	if runtime == nil || runtime.preferences == nil {
		return core.SettingsUpdateReceipt{}, basequery.NewUnavailableFailure(ErrApplicationLifecycleRuntime)
	}
	expectedRevision, err := strconv.ParseUint(request.ExpectedRevision, 10, 64)
	if err != nil || expectedRevision == 0 {
		return core.SettingsUpdateReceipt{}, basequery.NewValidationFailure("settings", err)
	}
	current, err := runtime.preferenceStore.LoadPreferences(ctx)
	if err != nil {
		return core.SettingsUpdateReceipt{}, publicRuntimeCommandFailure(err)
	}
	update := preferences.SettingsUpdate{
		ExpectedRevision: expectedRevision,
		Providers:        requestProviders(request.Providers, current.Providers),
		Online: preferences.OnlinePreferences{
			QuotaEnabled:           request.Online.QuotaEnabled,
			ResetCreditsEnabled:    request.Online.ResetCreditsEnabled,
			CursorOnlineEnabled:    request.Online.CursorOnlineEnabled,
			GrokQuotaEnabled:       request.Online.GrokQuotaEnabled,
			GrokAutoRefreshEnabled: request.Online.GrokAutoRefreshEnabled,
		},
		CodexAccounts: preferences.CodexAccountPreferences{
			RetainQuotaHistory: request.CodexAccounts.RetainQuotaHistory,
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
	update.UI.Locale = request.UI.Locale

	committed, updateErr := runtime.preferences.UpdateSettings(ctx, update)
	receipt := core.SettingsUpdateReceipt{Revision: strconv.FormatUint(committed.Revision, 10)}
	if updateErr != nil {
		var postCommit *ApplicationPreferencesPostCommitError
		if errors.As(updateErr, &postCommit) && committed.Revision > 0 {
			receipt.Result = core.SettingsUpdateReconcileRequired
			return receipt, nil
		}
		return core.SettingsUpdateReceipt{}, publicRuntimeCommandFailure(updateErr)
	}
	transition, err := runtime.controller.Apply(ctx, committed.Providers)
	notifyQueryInvalidation(runtime.invalidation, ctx, core.InvalidationSettings)
	notifyQueryInvalidation(runtime.invalidation, ctx, core.InvalidationIndex)
	notifyQueryInvalidation(runtime.invalidation, ctx, core.InvalidationQuota)
	notifyQueryInvalidation(runtime.invalidation, ctx, core.InvalidationAccount)
	if err != nil {
		receipt.Result = core.SettingsUpdateReconcileRequired
		return receipt, nil
	}
	if syncErr := runtime.syncCodexWorker(ctx); syncErr != nil {
		receipt.Result = core.SettingsUpdateReconcileRequired
		return receipt, nil
	}
	if worker := runtime.currentWorker(); worker != nil && worker.quota != nil {
		if recErr := worker.quota.ReconcilePreferences(ctx); recErr != nil {
			receipt.Result = core.SettingsUpdateReconcileRequired
			return receipt, nil
		}
	}
	if transition.ReconcileRequired {
		receipt.Result = core.SettingsUpdateReconcileRequired
		return receipt, nil
	}
	receipt.Result = core.SettingsUpdateApplied
	return receipt, nil
}

func (runtime *applicationControlRuntime) PlanHomeSwitch(
	ctx context.Context,
	request core.HomeSwitchPlanRequest,
) (core.HomeSwitchPlanReceipt, error) {
	if worker := runtime.currentWorker(); worker != nil {
		return worker.PlanHomeSwitch(ctx, request)
	}
	if runtime == nil || runtime.preferences == nil {
		return core.HomeSwitchPlanReceipt{}, basequery.NewUnavailableFailure(ErrApplicationLifecycleRuntime)
	}
	strategy := preferences.HomeSwitchStrategy(request.Strategy)
	plan, err := runtime.preferences.PlanSwitch(ctx, request.TargetPath, strategy)
	if err != nil {
		return core.HomeSwitchPlanReceipt{}, publicRuntimeCommandFailure(err)
	}
	runtime.mu.Lock()
	runtime.homePlanID = plan.ID
	runtime.mu.Unlock()
	return core.HomeSwitchPlanReceipt{
		Strategy: request.Strategy, TargetGeneration: strconv.FormatUint(plan.Target.Generation, 10),
		PreservesOldFacts:  plan.Impact.PreservesOldFacts,
		ClearsDerivedFacts: plan.Impact.ClearsDerivedFacts,
	}, nil
}

func (runtime *applicationControlRuntime) ConfirmHomeSwitch(ctx context.Context) (core.HomeSwitchReceipt, error) {
	if worker := runtime.currentWorker(); worker != nil {
		receipt, err := worker.ConfirmHomeSwitch(ctx)
		_ = runtime.syncCodexWorker(ctx)
		return receipt, err
	}
	if runtime == nil || runtime.preferences == nil {
		return core.HomeSwitchReceipt{}, basequery.NewUnavailableFailure(ErrApplicationLifecycleRuntime)
	}
	runtime.mu.Lock()
	planID := runtime.homePlanID
	runtime.mu.Unlock()
	if planID == "" {
		return core.HomeSwitchReceipt{}, basequery.NewUnavailableFailure(preferences.ErrSwitchPlanNotFound)
	}
	snapshot, err := runtime.preferences.ConfirmSwitch(ctx, planID)
	runtime.notifySettings(ctx)
	if err != nil {
		var postCommit *ApplicationPreferencesPostCommitError
		if errors.As(err, &postCommit) && snapshot.Revision > 0 {
			runtime.mu.Lock()
			if runtime.homePlanID == planID {
				runtime.homePlanID = ""
			}
			runtime.mu.Unlock()
			receipt := redactedHomeSwitchReceipt(snapshot)
			receipt.Result = core.HomeSwitchRecoveryRequired
			return receipt, nil
		}
		return core.HomeSwitchReceipt{}, publicRuntimeCommandFailure(err)
	}
	runtime.mu.Lock()
	if runtime.homePlanID == planID {
		runtime.homePlanID = ""
	}
	runtime.mu.Unlock()
	_ = runtime.syncCodexWorker(ctx)
	return redactedHomeSwitchReceipt(snapshot), nil
}

func (runtime *applicationControlRuntime) RecoverHomeSwitch(ctx context.Context) (core.HomeSwitchReceipt, error) {
	if worker := runtime.currentWorker(); worker != nil {
		return worker.RecoverHomeSwitch(ctx)
	}
	if runtime == nil || runtime.preferences == nil {
		return core.HomeSwitchReceipt{}, basequery.NewUnavailableFailure(ErrApplicationLifecycleRuntime)
	}
	runtime.mu.Lock()
	runtime.homePlanID = ""
	runtime.mu.Unlock()
	snapshot, err := runtime.preferences.RecoverSwitch(ctx)
	runtime.notifySettings(ctx)
	if err != nil {
		return core.HomeSwitchReceipt{}, publicRuntimeCommandFailure(err)
	}
	return redactedHomeSwitchReceipt(snapshot), nil
}

func (runtime *applicationControlRuntime) RunRuntimeAction(
	ctx context.Context,
	action core.RuntimeAction,
) (core.RuntimeActionReceipt, error) {
	if runtime == nil {
		return core.RuntimeActionReceipt{}, basequery.NewUnavailableFailure(ErrApplicationLifecycleRuntime)
	}
	operation, err := beginProviderControlOperation(runtime.controller, ctx, agentprovider.Codex)
	if err != nil {
		return core.RuntimeActionReceipt{}, err
	}
	defer operation.Finish()
	worker := runtime.currentWorker()
	if worker == nil {
		return core.RuntimeActionReceipt{}, basequery.NewUnavailableFailure(ErrApplicationLifecycleRuntime)
	}
	return worker.RunRuntimeAction(operation.Context(), action)
}

func (runtime *applicationControlRuntime) AnalyzeSessionIndexRepair(
	ctx context.Context,
) (core.RepairDryRunReceipt, error) {
	if runtime == nil {
		return core.RepairDryRunReceipt{}, basequery.NewUnavailableFailure(ErrApplicationLifecycleRuntime)
	}
	operation, err := beginProviderControlOperation(runtime.controller, ctx, agentprovider.Codex)
	if err != nil {
		return core.RepairDryRunReceipt{}, err
	}
	defer operation.Finish()
	worker := runtime.currentWorker()
	if worker == nil {
		return core.RepairDryRunReceipt{}, basequery.NewUnavailableFailure(ErrApplicationLifecycleRuntime)
	}
	return worker.AnalyzeSessionIndexRepair(operation.Context())
}

func (runtime *applicationControlRuntime) RequestQuotaRefresh(
	ctx context.Context,
	source quotaonline.RefreshSource,
) (store.SourceRefreshSchedule, error) {
	schedule, _, err := runtime.RequestQuotaRefreshResult(ctx, source)
	return schedule, err
}

func (runtime *applicationControlRuntime) RequestQuotaRefreshResult(
	ctx context.Context,
	source quotaonline.RefreshSource,
) (store.SourceRefreshSchedule, bool, error) {
	if runtime == nil {
		return store.SourceRefreshSchedule{}, false, basequery.NewUnavailableFailure(ErrApplicationLifecycleRuntime)
	}
	operation, err := beginProviderControlOperation(runtime.controller, ctx, agentprovider.Codex)
	if err != nil {
		return store.SourceRefreshSchedule{}, false, err
	}
	defer operation.Finish()
	worker := runtime.currentWorker()
	if worker == nil {
		return store.SourceRefreshSchedule{}, false, basequery.NewUnavailableFailure(ErrApplicationLifecycleRuntime)
	}
	return worker.RequestQuotaRefreshResult(operation.Context(), source)
}

func (runtime *applicationControlRuntime) DeepIndexSession(
	ctx context.Context,
	sessionID string,
) (lightindex.DeepIndexResult, error) {
	if runtime == nil {
		return lightindex.DeepIndexResult{}, basequery.NewUnavailableFailure(ErrApplicationLifecycleRuntime)
	}
	operation, err := beginProviderControlOperation(runtime.controller, ctx, agentprovider.Codex)
	if err != nil {
		return lightindex.DeepIndexResult{}, err
	}
	defer operation.Finish()
	worker := runtime.currentWorker()
	if worker == nil {
		return lightindex.DeepIndexResult{}, basequery.NewUnavailableFailure(ErrApplicationLifecycleRuntime)
	}
	return worker.DeepIndexSession(operation.Context(), sessionID)
}

func (runtime *applicationControlRuntime) AccountSnapshot(
	ctx context.Context,
	query core.AccountSnapshotQuery,
) (core.AccountSnapshot, error) {
	if runtime == nil || ctx == nil {
		return core.AccountSnapshot{}, ErrApplicationLifecycleRuntime
	}
	provider, err := agentprovider.Normalize(query.Scope.Provider)
	if err != nil {
		return core.AccountSnapshot{}, err
	}
	operation, err := beginProviderControlOperation(runtime.controller, ctx, provider)
	if err != nil {
		return core.AccountSnapshot{}, err
	}
	defer operation.Finish()
	operationContext := operation.Context()
	switch provider {
	case agentprovider.Cursor:
		return runtime.cursorAccountSnapshot(operationContext)
	case agentprovider.Grok:
		return runtime.grokAccountSnapshot(operationContext)
	case agentprovider.Codex:
		worker := runtime.currentWorker()
		if worker == nil {
			return core.AccountSnapshot{}, basequery.NewUnavailableFailure(nil)
		}
		return worker.AccountSnapshot(operationContext, query)
	default:
		return core.AccountSnapshot{}, ErrApplicationLifecycleRuntime
	}
}

func (runtime *applicationControlRuntime) cursorAccountSnapshot(ctx context.Context) (core.AccountSnapshot, error) {
	if runtime.cursorAccountReader == nil {
		return core.AccountSnapshot{}, nil
	}
	account, err := runtime.cursorAccountReader(ctx)
	if err != nil {
		return core.AccountSnapshot{}, err
	}
	email, plan := strings.TrimSpace(account.Email), strings.TrimSpace(account.MembershipType)
	if email == "" && plan == "" {
		return core.AccountSnapshot{}, nil
	}
	identity := &core.AccountIdentity{Type: agentprovider.Cursor}
	if email != "" {
		identity.Email = &email
	}
	if plan != "" {
		identity.PlanType = &plan
	}
	return core.AccountSnapshot{Account: identity}, nil
}

func (runtime *applicationControlRuntime) grokAccountSnapshot(ctx context.Context) (core.AccountSnapshot, error) {
	var cached grokprovider.AccountSnapshot
	if runtime.grokAccountReader != nil {
		if account, readErr := runtime.grokAccountReader(); readErr == nil {
			cached = account
		}
	}
	var profile grokprovider.AccountSnapshot
	if runtime.grokProfileReader != nil {
		if account, readErr := runtime.grokProfileReader(ctx); readErr == nil {
			profile = account
		}
	}
	email := strings.TrimSpace(profile.Email)
	if email == "" {
		email = strings.TrimSpace(cached.Email)
	}
	plan := strings.TrimSpace(profile.Subscription)
	if plan == "" {
		plan = grokSubscriptionPlanFromRepository(ctx, runtime.repository)
	}
	if email == "" && plan == "" {
		return core.AccountSnapshot{}, nil
	}
	identity := &core.AccountIdentity{Type: agentprovider.Grok}
	if email != "" {
		identity.Email = &email
	}
	if plan != "" {
		identity.PlanType = &plan
	}
	return core.AccountSnapshot{Account: identity}, nil
}

func (runtime *applicationControlRuntime) notifySettings(ctx context.Context) {
	notifyQueryInvalidation(runtime.invalidation, ctx, core.InvalidationSettings)
	notifyQueryInvalidation(runtime.invalidation, ctx, core.InvalidationIndex)
}

func (runtime *applicationControlRuntime) syncCodexWorker(ctx context.Context) error {
	if runtime == nil || runtime.controller == nil || runtime.preferenceStore == nil {
		return nil
	}
	snapshot, err := runtime.preferenceStore.LoadPreferences(ctx)
	if err != nil {
		return err
	}
	state, err := runtime.controller.Snapshot(agentprovider.Codex)
	if err != nil {
		return err
	}
	wantWorker := snapshot.CodexHome != nil && state.Effective == providercontrol.EffectiveEnabled
	worker := runtime.currentWorker()
	if wantWorker && worker == nil {
		return runtime.startCodexWorker(ctx)
	}
	if !wantWorker && worker != nil {
		return runtime.stopCodexWorker(ctx)
	}
	return nil
}

func (runtime *applicationControlRuntime) startCodexWorker(ctx context.Context) error {
	if runtime.database == nil {
		return ErrApplicationLifecycleRuntime
	}
	worker, err := startApplicationLifecycleRuntime(ctx, ApplicationLifecycleRuntimeConfig{
		Database: runtime.database, Preferences: runtime.preferenceStore,
		LightMetadata:      runtime.lightMetadata,
		Invalidation:       runtime.invalidation,
		PreferencesService: runtime.preferences,
		BindHomeRuntime:    runtime.bindHomeRuntime,
	})
	if err != nil {
		return err
	}
	runtime.AttachWorker(worker)
	return nil
}

func (runtime *applicationControlRuntime) stopCodexWorker(ctx context.Context) error {
	runtime.mu.Lock()
	worker := runtime.worker
	runtime.worker = nil
	runtime.mu.Unlock()
	runtime.homeRuntime.bind(nil)
	if worker == nil {
		return nil
	}
	return worker.Close(ctx)
}

func (runtime *applicationControlRuntime) BeginDrain(ctx context.Context) error {
	if runtime == nil {
		return ErrApplicationLifecycleRuntime
	}
	if worker := runtime.currentWorker(); worker != nil {
		return worker.BeginDrain(ctx)
	}
	return nil
}

func (runtime *applicationControlRuntime) Close(ctx context.Context) error {
	if runtime == nil {
		return ErrApplicationLifecycleRuntime
	}
	runtime.closeOnce.Do(func() {
		if runtime.controlStop != nil {
			runtime.controlStop()
		}
		if runtime.controller != nil {
			runtime.controller.SetSettledNotifier(nil)
			_ = runtime.controller.Seal(ctx)
		}
		if worker := runtime.currentWorker(); worker != nil {
			runtime.closeErr = worker.Close(ctx)
		}
		if runtime.adapter != nil {
			runtime.closeErr = errors.Join(runtime.closeErr, runtime.adapter.Close(ctx))
		}
	})
	return runtime.closeErr
}

func admitProviderControl(reader providercontrol.StateReader, provider string) error {
	if reader == nil {
		return nil
	}
	snapshot, err := reader.ProviderState(provider)
	if err != nil {
		if errors.Is(err, providercontrol.ErrInvalidProvider) {
			return basequery.NewValidationFailure("provider", err)
		}
		return basequery.NewUnavailableFailure(err)
	}
	switch {
	case errors.Is(providercontrol.QueryFailure(snapshot), providercontrol.ErrDisabled):
		return basequery.NewProviderDisabledFailure(nil)
	case errors.Is(providercontrol.QueryFailure(snapshot), providercontrol.ErrUnavailable):
		return basequery.NewUnavailableFailure(nil)
	default:
		return nil
	}
}

func beginProviderControlOperation(
	controller *providercontrol.Controller,
	ctx context.Context,
	provider string,
) (providercontrol.Operation, error) {
	if controller == nil || ctx == nil {
		return nil, basequery.NewUnavailableFailure(ErrApplicationLifecycleRuntime)
	}
	operation, err := controller.Begin(ctx, provider)
	if err == nil {
		return operation, nil
	}
	switch {
	case errors.Is(err, providercontrol.ErrDisabled):
		return nil, basequery.NewProviderDisabledFailure(nil)
	case errors.Is(err, providercontrol.ErrInvalidProvider):
		return nil, basequery.NewValidationFailure("provider", err)
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return nil, err
	default:
		return nil, basequery.NewUnavailableFailure(err)
	}
}

func grokSubscriptionPlanFromRepository(ctx context.Context, repository *store.Repository) string {
	if ctx == nil || repository == nil {
		return ""
	}
	snapshot, err := repository.GrokSnapshot(ctx)
	if err != nil || snapshot.Billing == nil || snapshot.BillingStale ||
		snapshot.Billing.SubscriptionTier == nil {
		return ""
	}
	return strings.TrimSpace(*snapshot.Billing.SubscriptionTier)
}

type controlLifecycleCoordinator struct {
	runtime *applicationControlRuntime
}

func (coordinator *controlLifecycleCoordinator) SystemWillSleep(
	ctx context.Context,
	eventID string,
) (store.SchedulerLifecycle, error) {
	if coordinator == nil || coordinator.runtime == nil {
		return store.SchedulerLifecycle{}, nil
	}
	if worker := coordinator.runtime.currentWorker(); worker != nil && worker.coordinator != nil {
		return worker.coordinator.SystemWillSleep(ctx, eventID)
	}
	return store.SchedulerLifecycle{}, nil
}

func (coordinator *controlLifecycleCoordinator) SystemDidWake(
	ctx context.Context,
	eventID string,
) (store.SchedulerLifecycle, error) {
	if coordinator == nil || coordinator.runtime == nil {
		return store.SchedulerLifecycle{}, nil
	}
	if worker := coordinator.runtime.currentWorker(); worker != nil && worker.coordinator != nil {
		return worker.coordinator.SystemDidWake(ctx, eventID)
	}
	return store.SchedulerLifecycle{}, nil
}

func (coordinator *controlLifecycleCoordinator) SourceChanged(
	ctx context.Context,
	eventID string,
	manual bool,
) (store.SchedulerLifecycle, error) {
	if coordinator == nil || coordinator.runtime == nil {
		return store.SchedulerLifecycle{}, nil
	}
	if err := coordinator.runtime.refreshDiscoveryAndWorkers(ctx, providerrefresh.TriggerForeground); err != nil {
		return store.SchedulerLifecycle{}, err
	}
	coordinator.runtime.refreshGlobal(ctx, providerrefresh.TriggerForeground)
	if worker := coordinator.runtime.currentWorker(); worker != nil && worker.coordinator != nil {
		return worker.coordinator.SourceChanged(ctx, eventID, manual)
	}
	return store.SchedulerLifecycle{}, nil
}
