package app

import (
	"context"
	"errors"
	"math"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/SisyphusSQ/codex-pulse/internal/agentprovider"
	"github.com/SisyphusSQ/codex-pulse/internal/bootstrap"
	"github.com/SisyphusSQ/codex-pulse/internal/codex/appserver"
	"github.com/SisyphusSQ/codex-pulse/internal/codex/homeidentity"
	logsource "github.com/SisyphusSQ/codex-pulse/internal/codex/logs/source"
	quotaonline "github.com/SisyphusSQ/codex-pulse/internal/codex/quota"
	"github.com/SisyphusSQ/codex-pulse/internal/core"
	"github.com/SisyphusSQ/codex-pulse/internal/cursorprovider"
	"github.com/SisyphusSQ/codex-pulse/internal/grokprovider"
	appLifecycle "github.com/SisyphusSQ/codex-pulse/internal/lifecycle"
	"github.com/SisyphusSQ/codex-pulse/internal/lightindex"
	"github.com/SisyphusSQ/codex-pulse/internal/liveindex"
	"github.com/SisyphusSQ/codex-pulse/internal/preferences"
	"github.com/SisyphusSQ/codex-pulse/internal/scheduler"
	"github.com/SisyphusSQ/codex-pulse/internal/store"
	storelight "github.com/SisyphusSQ/codex-pulse/internal/store/lightindex"
	storesqlite "github.com/SisyphusSQ/codex-pulse/internal/store/sqlite"
)

var (
	ErrApplicationLifecycleRuntime      = errors.New("application lifecycle runtime is unavailable")
	ErrApplicationPreferencesPostCommit = errors.New("application preferences committed but reconcile failed")
)

const defaultApplicationLightRefreshInterval = 30 * time.Second

type ApplicationPreferencesPostCommitError struct {
	Committed preferences.Snapshot
	Cause     error
}

func (postCommitError *ApplicationPreferencesPostCommitError) Error() string {
	return ErrApplicationPreferencesPostCommit.Error()
}

func (postCommitError *ApplicationPreferencesPostCommitError) Unwrap() []error {
	if postCommitError == nil || postCommitError.Cause == nil {
		return []error{ErrApplicationPreferencesPostCommit}
	}
	return []error{ErrApplicationPreferencesPostCommit, postCommitError.Cause}
}

type confirmedPreferencesLoader interface {
	LoadPreferences(context.Context) (preferences.Snapshot, error)
}

type applicationAccountReader func(
	context.Context,
	appserver.ConfirmedHome,
	appserver.ProcessOptions,
) (*appserver.AccountSnapshot, error)

type cursorAccountReader func(context.Context) (cursorprovider.DesktopAccountSnapshot, error)

type grokAccountReader func() (grokprovider.AccountSnapshot, error)

type grokProfileReader func(context.Context) (grokprovider.AccountSnapshot, error)

type ApplicationLifecycleRuntimeConfig struct {
	Database             *storesqlite.Store
	Preferences          confirmedPreferencesLoader
	EventTimeout         time.Duration
	QuotaTransport       http.RoundTripper
	QuotaClock           func() time.Time
	Invalidation         queryInvalidationNotifier
	UpdateWake           func(context.Context) error
	LightMetadata        lightindex.MetadataProvider
	LightRefreshInterval time.Duration
	quotaHooks           quotaRuntimeHooks
	homeRuntime          preferences.HomeRuntime
}

type applicationLifecycleRuntime struct {
	adapter             *LifecycleEventAdapter
	coordinator         *appLifecycle.Coordinator
	scheduler           *scheduler.Service
	repository          *store.Repository
	lightRepository     *storelight.Repository
	quota               *applicationQuotaRuntime
	preferences         *preferences.Service
	settingsLoader      confirmedPreferencesLoader
	accountReader       applicationAccountReader
	cursorAccountReader cursorAccountReader
	grokAccountReader   grokAccountReader
	grokProfileReader   grokProfileReader
	database            *storesqlite.Store
	invalidation        queryInvalidationNotifier
	cancel              context.CancelFunc
	workerDone          chan error
	lightRuntime        *lightindex.Runtime
	lightRun            *lightindex.Run
	lightMu             sync.Mutex
	controlCtx          context.Context
	controlStop         context.CancelFunc

	controlMu        sync.Mutex
	controlAccepting bool
	controlInflight  int
	controlDone      chan struct{}

	homePlanMu sync.Mutex
	homePlanID string
	drainOnce  sync.Once
	drainDone  chan struct{}
	drainErr   error

	closeOnce sync.Once
	closeDone chan struct{}
	closeErr  error
}

func startApplicationLifecycleRuntime(
	ctx context.Context,
	config ApplicationLifecycleRuntimeConfig,
) (*applicationLifecycleRuntime, error) {
	if ctx == nil || config.Database == nil || config.EventTimeout < 0 || config.LightRefreshInterval < 0 {
		return nil, ErrApplicationLifecycleRuntime
	}
	loader := config.Preferences
	if loader == nil {
		path, err := preferences.DefaultPath()
		if err != nil {
			return nil, applicationLifecycleDependencyError(ctx, err)
		}
		loader, err = preferences.NewFileStore(path)
		if err != nil {
			return nil, applicationLifecycleDependencyError(ctx, err)
		}
	}
	snapshot, err := loader.LoadPreferences(ctx)
	if errors.Is(err, preferences.ErrNotConfigured) {
		return nil, nil
	}
	if err != nil || snapshot.CodexHome.Generation > math.MaxInt64 {
		return nil, applicationLifecycleDependencyError(ctx, err)
	}
	repository := store.NewRepository(config.Database)
	lightRepository := storelight.NewRepository(config.Database)
	if config.LightMetadata != nil {
		if err := migrateApplicationLightHomeIdentity(ctx, lightRepository, snapshot.CodexHome.Source); err != nil {
			return nil, applicationLifecycleDependencyError(ctx, err)
		}
	}
	// 额度仲裁规则可独立于 schema 升级；Core 查询开放前必须先重建当前版本的派生投影。
	if err := repository.RebuildQuotaProjection(ctx, store.DefaultQuotaArbitrationRule()); err != nil {
		return nil, applicationLifecycleDependencyError(ctx, err)
	}
	bootstrapRuntime, err := bootstrap.NewRuntime(bootstrap.RuntimeConfig{Repository: repository})
	if err != nil {
		return nil, applicationLifecycleDependencyError(ctx, err)
	}
	useLightIndex := config.LightMetadata != nil
	localHomeRuntime := preferences.HomeRuntime(bootstrapRuntime)
	if useLightIndex && config.homeRuntime == nil {
		localHomeRuntime = applicationLightHomeRuntime{}
	}
	if config.homeRuntime != nil {
		localHomeRuntime = config.homeRuntime
	}
	preferencesStore, hasPreferencesStore := loader.(preferences.PreferencesStore)
	quotaRuntime, err := startApplicationQuotaRuntime(ctx, ApplicationQuotaRuntimeConfig{
		Repository: repository, Preferences: loader,
		Transport: config.QuotaTransport, Clock: config.QuotaClock,
		suspended: hasPreferencesStore, hooks: config.quotaHooks,
		invalidation: config.Invalidation,
	})
	if err != nil {
		return nil, applicationLifecycleDependencyError(ctx, err)
	}
	closeQuotaRuntime := func() { _ = quotaRuntime.Close(context.Background()) }
	var preferencesService *preferences.Service
	var quotaHomeRuntime *applicationQuotaHomeRuntime
	if hasPreferencesStore {
		quotaHomeRuntime = &applicationQuotaHomeRuntime{
			local: localHomeRuntime,
			quota: quotaRuntime,
		}
		preferencesService, err = preferences.NewService(preferences.ServiceConfig{
			Store: preferencesStore, Probe: logsource.NewHomeProbe(),
			Runtime: quotaHomeRuntime,
		})
		if err != nil {
			closeQuotaRuntime()
			return nil, applicationLifecycleDependencyError(ctx, err)
		}
		snapshot, err = preferencesService.RecoverSwitch(ctx)
		if err != nil || snapshot.PendingSwitch != nil || snapshot.PendingResume != nil ||
			snapshot.CodexHome.Generation > math.MaxInt64 {
			closeQuotaRuntime()
			return nil, applicationLifecycleDependencyError(ctx, err)
		}
	}
	var lightRuntime *lightindex.Runtime
	var lightRun *lightindex.Run
	var lightReconcileRunner *applicationLightIndexReconcileRunner
	if useLightIndex {
		refreshInterval := config.LightRefreshInterval
		if refreshInterval == 0 {
			refreshInterval = defaultApplicationLightRefreshInterval
		}
		lightRuntime, err = lightindex.NewRuntime(lightindex.RuntimeConfig{
			Repository: lightRepository, DeepRepository: repository,
			Metadata: config.LightMetadata, RefreshInterval: refreshInterval,
			RefreshCommitted: func() {
				notifyQueryInvalidation(config.Invalidation, context.Background(), core.InvalidationIndex)
			},
		})
		if err == nil {
			lightRun, err = lightRuntime.Start(ctx, storelight.LightHomeIdentity{
				Path: snapshot.CodexHome.Source.Path, DeviceID: snapshot.CodexHome.Source.DeviceID,
				Inode: snapshot.CodexHome.Source.Inode,
			})
		}
		if err != nil {
			closeQuotaRuntime()
			return nil, applicationLifecycleDependencyError(ctx, err)
		}
	} else if err := ensureApplicationBootstrap(ctx, repository, bootstrapRuntime, snapshot.CodexHome); err != nil {
		closeQuotaRuntime()
		return nil, applicationLifecycleDependencyError(ctx, err)
	}
	homeGeneration := int64(snapshot.CodexHome.Generation)
	liveRuntime, err := liveindex.New(liveindex.Config{Repository: repository})
	if err != nil {
		closeQuotaRuntime()
		return nil, applicationLifecycleDependencyError(ctx, err)
	}
	bootstrapExecutor, err := scheduler.NewBootstrapExecutor(bootstrapRuntime)
	if err != nil {
		closeQuotaRuntime()
		return nil, applicationLifecycleDependencyError(ctx, err)
	}
	liveExecutor, err := scheduler.NewLiveExecutor(liveRuntime)
	if err != nil {
		closeQuotaRuntime()
		return nil, applicationLifecycleDependencyError(ctx, err)
	}
	schedulerService, err := scheduler.NewService(scheduler.ServiceConfig{
		Repository: repository,
		Executors: map[store.SchedulerTargetKind]scheduler.Executor{
			store.SchedulerTargetBootstrap: bootstrapExecutor,
			store.SchedulerTargetLiveScan:  liveExecutor,
		},
		BudgetPolicy: scheduler.DefaultBudgetPolicy(), MaxLiveBurst: 8,
		CycleCommitted: func(ctx context.Context, _ store.SchedulerCycle) {
			notifyQueryInvalidation(config.Invalidation, ctx, core.InvalidationIndex)
		},
	})
	if err != nil {
		closeQuotaRuntime()
		return nil, applicationLifecycleDependencyError(ctx, err)
	}
	queueRunner, err := appLifecycle.NewQueueReconcileRunner(appLifecycle.QueueReconcileRunnerConfig{
		Repository: repository, Live: liveRuntime, Queue: schedulerService,
	})
	if err != nil {
		closeQuotaRuntime()
		return nil, applicationLifecycleDependencyError(ctx, err)
	}
	var runner appLifecycle.ReconcileRunner = &applicationBootstrapAwareReconcileRunner{
		repository: repository, delegate: queueRunner,
	}
	if useLightIndex {
		lightReconcileRunner = &applicationLightIndexReconcileRunner{}
		runner = lightReconcileRunner
	}
	reconciler, err := appLifecycle.NewConfirmedHomeReconciler(appLifecycle.ConfirmedHomeReconcilerConfig{
		HomeProvider: fileConfirmedHomeProvider{loader: loader}, Runner: runner,
	})
	if err != nil {
		closeQuotaRuntime()
		return nil, applicationLifecycleDependencyError(ctx, err)
	}
	coordinator, err := appLifecycle.NewCoordinator(appLifecycle.Config{
		Repository: repository, Scheduler: schedulerService, Reconciler: reconciler,
	})
	if err != nil {
		closeQuotaRuntime()
		return nil, applicationLifecycleDependencyError(ctx, err)
	}
	closeCoordinator := func() { _ = coordinator.Close(context.Background()) }
	state, err := repository.SchedulerLifecycle(ctx)
	if errors.Is(err, store.ErrNotFound) {
		state, err = coordinator.Initialize(ctx, homeGeneration, startupEventID("initialize"))
	}
	if err != nil {
		closeCoordinator()
		closeQuotaRuntime()
		return nil, applicationLifecycleDependencyError(ctx, err)
	}
	if state.HomeGeneration == homeGeneration {
		// Revalidate the confirmed physical Home before any target recovery can
		// interrupt or rebind durable jobs. A stale path with the same logical
		// generation must fail closed without target-side effects.
		_, wakeErr := coordinator.SystemDidWake(ctx, startupEventID("wake"))
		if !nonFatalSourceStateError(wakeErr) {
			closeCoordinator()
			closeQuotaRuntime()
			return nil, applicationLifecycleDependencyError(ctx, wakeErr)
		}
		_, recoverErr := coordinator.Recover(ctx, startupEventID("recover"))
		if !nonFatalSourceStateError(recoverErr) {
			closeCoordinator()
			closeQuotaRuntime()
			return nil, applicationLifecycleDependencyError(ctx, recoverErr)
		}
	} else {
		_, handoffErr := coordinator.HomeChanged(
			ctx, startupEventID("home-generation-handoff"), homeGeneration,
		)
		if !nonFatalSourceStateError(handoffErr) {
			closeCoordinator()
			closeQuotaRuntime()
			return nil, applicationLifecycleDependencyError(ctx, handoffErr)
		}
	}
	if !useLightIndex {
		if err := enqueueLatestApplicationBootstrap(ctx, repository, schedulerService, homeGeneration); err != nil {
			closeCoordinator()
			closeQuotaRuntime()
			return nil, applicationLifecycleDependencyError(ctx, err)
		}
	}
	if quotaHomeRuntime != nil {
		quotaHomeRuntime.lifecycle = coordinator
		quotaHomeRuntime.resumeQuota = true
		if err := quotaRuntime.ResumeGeneration(ctx, snapshot.CodexHome.Generation); err != nil {
			closeCoordinator()
			closeQuotaRuntime()
			return nil, applicationLifecycleDependencyError(ctx, err)
		}
	}
	adapter, err := NewLifecycleEventAdapter(LifecycleEventAdapterConfig{
		Coordinator: applicationQuotaLifecycleCoordinator{
			local: coordinator, quota: quotaRuntime,
		},
		EventTimeout: config.EventTimeout,
		DidWake:      config.UpdateWake,
	})
	if err != nil {
		closeQuotaRuntime()
		closeCoordinator()
		return nil, applicationLifecycleDependencyError(ctx, err)
	}
	workerCtx, cancel := context.WithCancel(ctx)
	controlCtx, controlStop := context.WithCancel(ctx)
	runtime := &applicationLifecycleRuntime{
		adapter: adapter, coordinator: coordinator, scheduler: schedulerService,
		repository: repository, lightRepository: lightRepository, quota: quotaRuntime,
		preferences: preferencesService, settingsLoader: loader, database: config.Database,
		accountReader: appserver.ReadLocalAccount,
		invalidation:  config.Invalidation, cancel: cancel,
		workerDone: make(chan error, 1), lightRuntime: lightRuntime, lightRun: lightRun,
		controlCtx: controlCtx, controlStop: controlStop,
		controlAccepting: true, controlDone: closedApplicationLifecycleSignal(),
		drainDone: make(chan struct{}),
		closeDone: make(chan struct{}),
	}
	if cursorConfig, configErr := cursorprovider.DefaultConfig(); configErr == nil {
		if cursorAuth, authErr := cursorprovider.NewDesktopAuthReader(cursorConfig.StateDatabase, time.Now); authErr == nil {
			runtime.cursorAccountReader = cursorAuth.ReadAccountSnapshot
		}
	}
	if grokConfig, configErr := grokprovider.DefaultConfig(); configErr == nil {
		if grokAuth, authErr := grokprovider.NewAuthReader(
			grokConfig.AuthPath,
			time.Now,
			grokprovider.AuthReaderConfig{RefreshEnabled: func() bool {
				current, loadErr := loader.LoadPreferences(context.Background())
				return loadErr != nil || current.Online.GrokAutoRefreshEnabled
			}},
		); authErr == nil {
			runtime.grokAccountReader = grokAuth.ReadAccountSnapshot
			if accountClient, accountErr := grokprovider.NewAccountClient(grokprovider.AccountClientConfig{
				BaseURL: grokConfig.BillingBaseURL,
				HTTPClient: &http.Client{
					Timeout: 15 * time.Second,
				},
				TokenSource: grokAuth,
			}); accountErr == nil {
				runtime.grokProfileReader = accountClient.GetAccount
			}
		}
	}
	if useLightIndex {
		runtime.workerDone <- nil
		lightReconcileRunner.setTrigger(runtime.triggerLightIndex)
	} else {
		go func() { runtime.workerDone <- schedulerService.Run(workerCtx) }()
	}
	return runtime, nil
}

func migrateApplicationLightHomeIdentity(
	ctx context.Context,
	repository *storelight.Repository,
	confirmed preferences.ConfirmedSource,
) error {
	if repository == nil || !homeidentity.IsStableDeviceID(confirmed.DeviceID) {
		return nil
	}
	state, err := repository.LightIndexState(ctx)
	if errors.Is(err, storelight.ErrNotFound) {
		return nil
	}
	if err != nil || state.Home.DeviceID == confirmed.DeviceID {
		return err
	}
	if state.Home.Path != confirmed.Path || state.Home.Inode != confirmed.Inode ||
		!homeidentity.IsLegacyDeviceID(state.Home.DeviceID) {
		return storelight.ErrLightHomeFence
	}
	_, err = repository.MigrateLightHomeIdentity(ctx, state.Home, storelight.LightHomeIdentity{
		Path: confirmed.Path, DeviceID: confirmed.DeviceID, Inode: confirmed.Inode,
	})
	return err
}

const initialApplicationBootstrapSwitchID = "initial-onboarding-bootstrap"

func ensureApplicationBootstrap(
	ctx context.Context,
	repository *store.Repository,
	runtime *bootstrap.Runtime,
	home preferences.CodexHomePreferences,
) error {
	if ctx == nil || repository == nil || runtime == nil || home.Generation == 0 ||
		home.Generation > math.MaxInt64 {
		return ErrApplicationLifecycleRuntime
	}
	_, facts, err := repository.LatestBootstrapRunByGeneration(ctx, int64(home.Generation))
	if err == nil {
		if facts.HomeGeneration != int64(home.Generation) || facts.HomePath != home.Source.Path ||
			facts.HomeDeviceID != home.Source.DeviceID || facts.HomeInode != home.Source.Inode ||
			facts.DataStoreKey != home.DataStoreKey {
			return ErrApplicationLifecycleRuntime
		}
		return nil
	}
	if !errors.Is(err, store.ErrNotFound) {
		return err
	}
	err = runtime.StartBootstrap(ctx, preferences.BootstrapRequest{
		SwitchID:     initialApplicationBootstrapSwitchID,
		Generation:   home.Generation,
		Source:       home.Source,
		DataStoreKey: home.DataStoreKey,
		Strategy:     preferences.HomeSwitchClearAndRebuild,
	})
	if errors.Is(err, logsource.ErrInvalidHome) || errors.Is(err, logsource.ErrUnsafeHome) ||
		errors.Is(err, logsource.ErrHomeChanged) || errors.Is(err, logsource.ErrUnsafeSource) ||
		errors.Is(err, logsource.ErrChangedDuringScan) || errors.Is(err, logsource.ErrUnsupportedFile) ||
		errors.Is(err, bootstrap.ErrDiscoveryIncomplete) || errors.Is(err, bootstrap.ErrInvalidRequest) {
		return nil
	}
	return err
}

type applicationBootstrapAwareReconcileRunner struct {
	repository *store.Repository
	delegate   appLifecycle.ReconcileRunner
}

type applicationLightIndexReconcileRunner struct {
	mu      sync.RWMutex
	trigger func()
}

func (runner *applicationLightIndexReconcileRunner) setTrigger(trigger func()) {
	if runner == nil {
		return
	}
	runner.mu.Lock()
	runner.trigger = trigger
	runner.mu.Unlock()
}

func (runner *applicationLightIndexReconcileRunner) RunReconcile(
	ctx context.Context,
	_ appLifecycle.ConfirmedHome,
	_ appLifecycle.ReconcileReason,
) error {
	if runner == nil || ctx == nil {
		return ErrApplicationLifecycleRuntime
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	runner.mu.RLock()
	trigger := runner.trigger
	runner.mu.RUnlock()
	if trigger != nil {
		trigger()
	}
	return nil
}

func (runtime *applicationLifecycleRuntime) triggerLightIndex() {
	if runtime == nil {
		return
	}
	runtime.lightMu.Lock()
	defer runtime.lightMu.Unlock()
	if runtime.lightRun != nil {
		_ = runtime.lightRun.Trigger()
	}
}

func (runner *applicationBootstrapAwareReconcileRunner) RunReconcile(
	ctx context.Context,
	home appLifecycle.ConfirmedHome,
	reason appLifecycle.ReconcileReason,
) error {
	if runner == nil || runner.repository == nil || runner.delegate == nil || ctx == nil {
		return ErrApplicationLifecycleRuntime
	}
	_, facts, err := runner.repository.LatestBootstrapRunByGeneration(ctx, home.Generation)
	switch {
	case err == nil && facts.FullHistoryReadyAtMS == nil:
		return nil
	case errors.Is(err, store.ErrNotFound):
		return runner.delegate.RunReconcile(ctx, home, reason)
	case err != nil:
		return err
	default:
		return runner.delegate.RunReconcile(ctx, home, reason)
	}
}

func (runtime *applicationLifecycleRuntime) UpdateQuotaSettings(
	ctx context.Context,
	request preferences.SettingsUpdate,
) (preferences.Snapshot, error) {
	if runtime == nil || runtime.preferences == nil || runtime.quota == nil || ctx == nil {
		return preferences.Snapshot{}, ErrApplicationLifecycleRuntime
	}
	controlContext, finishControl, err := runtime.beginControlAdmission(ctx)
	if err != nil {
		return preferences.Snapshot{}, err
	}
	defer finishControl()
	committed, err := runtime.preferences.UpdateSettings(controlContext, request)
	if err != nil {
		return committed, err
	}
	notifyQueryInvalidation(runtime.invalidation, controlContext, core.InvalidationSettings)
	notifyQueryInvalidation(runtime.invalidation, controlContext, core.InvalidationQuota)
	if err := runtime.quota.ReconcilePreferences(controlContext); err != nil {
		return committed, &ApplicationPreferencesPostCommitError{Committed: committed, Cause: err}
	}
	return committed, nil
}

func (runtime *applicationLifecycleRuntime) RequestQuotaRefresh(
	ctx context.Context,
	source quotaonline.RefreshSource,
) (store.SourceRefreshSchedule, error) {
	if runtime == nil || runtime.quota == nil || ctx == nil {
		return store.SourceRefreshSchedule{}, ErrApplicationLifecycleRuntime
	}
	operationContext, finish, err := runtime.beginControlAdmission(ctx)
	if err != nil {
		return store.SourceRefreshSchedule{}, err
	}
	defer finish()
	return runtime.quota.RequestRefresh(operationContext, source, store.RefreshTriggerManual)
}

func (runtime *applicationLifecycleRuntime) PlanQuotaHomeSwitch(
	ctx context.Context,
	targetPath string,
	strategy preferences.HomeSwitchStrategy,
) (preferences.SwitchPlan, error) {
	if runtime == nil || runtime.preferences == nil || ctx == nil {
		return preferences.SwitchPlan{}, ErrApplicationLifecycleRuntime
	}
	operationContext, finish, err := runtime.beginControlAdmission(ctx)
	if err != nil {
		return preferences.SwitchPlan{}, err
	}
	defer finish()
	return runtime.preferences.PlanSwitch(operationContext, targetPath, strategy)
}

func (runtime *applicationLifecycleRuntime) ConfirmQuotaHomeSwitch(
	ctx context.Context,
	planID string,
) (preferences.Snapshot, error) {
	if runtime == nil || runtime.preferences == nil || runtime.quota == nil || ctx == nil {
		return preferences.Snapshot{}, ErrApplicationLifecycleRuntime
	}
	operationContext, finish, err := runtime.beginControlAdmission(ctx)
	if err != nil {
		return preferences.Snapshot{}, err
	}
	defer finish()
	committed, err := runtime.preferences.ConfirmSwitch(operationContext, planID)
	return runtime.resumeCommittedQuotaGeneration(operationContext, committed, err)
}

func (runtime *applicationLifecycleRuntime) RecoverQuotaHomeSwitch(
	ctx context.Context,
) (preferences.Snapshot, error) {
	if runtime == nil || runtime.preferences == nil || runtime.quota == nil || ctx == nil {
		return preferences.Snapshot{}, ErrApplicationLifecycleRuntime
	}
	operationContext, finish, err := runtime.beginControlAdmission(ctx)
	if err != nil {
		return preferences.Snapshot{}, err
	}
	defer finish()
	committed, err := runtime.preferences.RecoverSwitch(operationContext)
	return runtime.resumeCommittedQuotaGeneration(operationContext, committed, err)
}

func (runtime *applicationLifecycleRuntime) resumeCommittedQuotaGeneration(
	ctx context.Context,
	committed preferences.Snapshot,
	original error,
) (preferences.Snapshot, error) {
	if committed.PendingSwitch != nil || committed.PendingResume != nil ||
		committed.CodexHome.Generation == 0 || committed.CodexHome.Generation > math.MaxInt64 {
		return committed, original
	}
	if _, err := runtime.coordinator.HomeChanged(
		ctx,
		startupEventID("home-generation-resume"),
		int64(committed.CodexHome.Generation),
	); err != nil {
		postCommitErr := &ApplicationPreferencesPostCommitError{
			Committed: committed,
			Cause:     err,
		}
		return committed, errors.Join(original, postCommitErr)
	}
	var indexErr error
	if runtime.lightRuntime != nil {
		indexErr = runtime.restartLightIndex(committed)
	} else {
		indexErr = enqueueLatestApplicationBootstrap(
			ctx, runtime.repository, runtime.scheduler, int64(committed.CodexHome.Generation),
		)
	}
	if indexErr != nil {
		postCommitErr := &ApplicationPreferencesPostCommitError{
			Committed: committed,
			Cause:     indexErr,
		}
		return committed, errors.Join(original, postCommitErr)
	}
	if err := runtime.quota.ResumeGeneration(ctx, committed.CodexHome.Generation); err != nil {
		postCommitErr := &ApplicationPreferencesPostCommitError{
			Committed: committed,
			Cause:     err,
		}
		return committed, errors.Join(original, postCommitErr)
	}
	if err := runtime.quota.RearmAfterHomeChange(ctx); err != nil {
		postCommitErr := &ApplicationPreferencesPostCommitError{
			Committed: committed,
			Cause:     err,
		}
		return committed, errors.Join(original, postCommitErr)
	}
	return committed, original
}

func (runtime *applicationLifecycleRuntime) restartLightIndex(committed preferences.Snapshot) error {
	if runtime == nil || runtime.lightRuntime == nil || runtime.lightRepository == nil {
		return ErrApplicationLifecycleRuntime
	}
	runtime.lightMu.Lock()
	defer runtime.lightMu.Unlock()
	state, err := runtime.lightRepository.LightIndexState(runtime.controlCtx)
	if err != nil {
		return err
	}
	if runtime.lightRun != nil {
		runtime.lightRun.Cancel()
		_ = runtime.lightRun.Wait(context.Background())
	}
	next := storelight.LightHomeIdentity{
		Path: committed.CodexHome.Source.Path, DeviceID: committed.CodexHome.Source.DeviceID,
		Inode: committed.CodexHome.Source.Inode,
	}
	run, err := runtime.lightRuntime.StartHomeSwitch(runtime.controlCtx, state.Home, next)
	if err != nil {
		return err
	}
	runtime.lightRun = run
	notifyQueryInvalidation(runtime.invalidation, context.Background(), core.InvalidationIndex)
	return nil
}

// DeepIndexSession admits the on-demand strict parser through the same drain
// fence as every other lifecycle control. Shutdown therefore waits for a
// cooperative checkpoint instead of closing SQLite beneath an active parser.
func (runtime *applicationLifecycleRuntime) DeepIndexSession(
	ctx context.Context,
	sessionID string,
) (lightindex.DeepIndexResult, error) {
	if runtime == nil || runtime.lightRuntime == nil || ctx == nil {
		return lightindex.DeepIndexResult{}, ErrApplicationLifecycleRuntime
	}
	operationContext, finish, err := runtime.beginControlAdmission(ctx)
	if err != nil {
		return lightindex.DeepIndexResult{}, err
	}
	defer finish()
	result, err := runtime.lightRuntime.DeepIndexSession(operationContext, sessionID)
	if err == nil {
		notifyQueryInvalidation(runtime.invalidation, operationContext, core.InvalidationIndex)
	}
	return result, err
}

func (runtime *applicationLifecycleRuntime) Close(ctx context.Context) error {
	if runtime == nil || ctx == nil {
		return ErrApplicationLifecycleRuntime
	}
	runtime.closeOnce.Do(func() { go runtime.shutdown() })
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-runtime.closeDone:
		return runtime.closeErr
	}
}

// BeginDrain is the irreversible admission fence shared by Quit and Install.
// It rejects new RPC controls, closes lifecycle admission, and cancels the
// scheduler worker so an active slice reaches its cooperative checkpoint. Close
// later waits for the worker and tears down quota/coordinator resources.
func (runtime *applicationLifecycleRuntime) BeginDrain(ctx context.Context) error {
	if runtime == nil || ctx == nil {
		return ErrApplicationLifecycleRuntime
	}
	runtime.drainOnce.Do(func() { go runtime.beginDrain() })
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-runtime.drainDone:
		return runtime.drainErr
	}
}

func (runtime *applicationLifecycleRuntime) beginDrain() {
	adapterErr := runtime.adapter.Close(context.Background())
	controlDone := runtime.sealControlAdmission()
	runtime.cancel()
	<-controlDone
	runtime.lightMu.Lock()
	if runtime.lightRun != nil {
		runtime.lightRun.Cancel()
	}
	runtime.lightMu.Unlock()
	runtime.drainErr = adapterErr
	close(runtime.drainDone)
}

func (runtime *applicationLifecycleRuntime) shutdown() {
	runtime.drainOnce.Do(func() { go runtime.beginDrain() })
	<-runtime.drainDone
	quotaErr := runtime.quota.Close(context.Background())
	workerErr := <-runtime.workerDone
	if errors.Is(workerErr, context.Canceled) {
		workerErr = nil
	}
	var lightErr error
	runtime.lightMu.Lock()
	if runtime.lightRun != nil {
		lightErr = runtime.lightRun.Wait(context.Background())
		if errors.Is(lightErr, context.Canceled) {
			lightErr = nil
		}
	}
	runtime.lightMu.Unlock()
	coordinatorErr := runtime.coordinator.Close(context.Background())
	runtime.closeErr = errors.Join(runtime.drainErr, quotaErr, workerErr, lightErr, coordinatorErr)
	close(runtime.closeDone)
}

func (runtime *applicationLifecycleRuntime) beginControlAdmission(
	ctx context.Context,
) (context.Context, func(), error) {
	if runtime == nil || ctx == nil {
		return nil, nil, ErrApplicationLifecycleRuntime
	}
	runtime.controlMu.Lock()
	if !runtime.controlAccepting || runtime.controlCtx == nil || runtime.controlCtx.Err() != nil {
		runtime.controlMu.Unlock()
		return nil, nil, ErrApplicationLifecycleRuntime
	}
	if runtime.controlInflight == 0 {
		runtime.controlDone = make(chan struct{})
	}
	runtime.controlInflight++
	controlCtx := runtime.controlCtx
	runtime.controlMu.Unlock()

	operationContext, cancel := context.WithCancel(ctx)
	stopControlCancel := context.AfterFunc(controlCtx, cancel)
	var finishOnce sync.Once
	finish := func() {
		finishOnce.Do(func() {
			stopControlCancel()
			cancel()
			runtime.controlMu.Lock()
			runtime.controlInflight--
			if runtime.controlInflight == 0 {
				close(runtime.controlDone)
			}
			runtime.controlMu.Unlock()
		})
	}
	return operationContext, finish, nil
}

func (runtime *applicationLifecycleRuntime) sealControlAdmission() <-chan struct{} {
	runtime.controlMu.Lock()
	runtime.controlAccepting = false
	runtime.controlStop()
	done := runtime.controlDone
	runtime.controlMu.Unlock()
	return done
}

func closedApplicationLifecycleSignal() chan struct{} {
	signal := make(chan struct{})
	close(signal)
	return signal
}

type fileConfirmedHomeProvider struct {
	loader confirmedPreferencesLoader
}

type applicationQuotaHomeRuntime struct {
	local       preferences.HomeRuntime
	quota       *applicationQuotaRuntime
	lifecycle   *appLifecycle.Coordinator
	resumeQuota bool
}

// applicationLightHomeRuntime preserves the preferences switch journal without
// scheduling the legacy deep bootstrap. The committed Home is synchronously
// published by restartLightIndex after the preference transaction completes.
type applicationLightHomeRuntime struct{}

func (applicationLightHomeRuntime) Drain(context.Context, uint64) error { return nil }

func (applicationLightHomeRuntime) StartBootstrap(
	context.Context,
	preferences.BootstrapRequest,
) error {
	return nil
}

func (applicationLightHomeRuntime) BootstrapStatus(
	context.Context,
	string,
	uint64,
) (preferences.BootstrapStatus, error) {
	return preferences.BootstrapStatusQueued, nil
}

func (applicationLightHomeRuntime) Resume(context.Context, uint64) error { return nil }

func (runtime applicationQuotaHomeRuntime) Drain(ctx context.Context, generation uint64) error {
	if runtime.local == nil || runtime.quota == nil || ctx == nil {
		return ErrApplicationLifecycleRuntime
	}
	if runtime.lifecycle != nil {
		if _, err := runtime.lifecycle.SourceChanged(
			ctx, startupEventID("home-generation-drain"), false,
		); err != nil {
			return err
		}
	}
	if err := runtime.quota.DrainGeneration(ctx, generation); err != nil {
		return err
	}
	return runtime.local.Drain(ctx, generation)
}

func (runtime applicationQuotaHomeRuntime) StartBootstrap(
	ctx context.Context,
	request preferences.BootstrapRequest,
) error {
	if runtime.local == nil || runtime.quota == nil || ctx == nil {
		return ErrApplicationLifecycleRuntime
	}
	return runtime.local.StartBootstrap(ctx, request)
}

func (runtime applicationQuotaHomeRuntime) BootstrapStatus(
	ctx context.Context,
	switchID string,
	generation uint64,
) (preferences.BootstrapStatus, error) {
	if runtime.local == nil || runtime.quota == nil || ctx == nil {
		return "", ErrApplicationLifecycleRuntime
	}
	return runtime.local.BootstrapStatus(ctx, switchID, generation)
}

func (runtime applicationQuotaHomeRuntime) Resume(ctx context.Context, generation uint64) error {
	if runtime.local == nil || runtime.quota == nil || ctx == nil {
		return ErrApplicationLifecycleRuntime
	}
	if err := runtime.local.Resume(ctx, generation); err != nil {
		return err
	}
	if runtime.lifecycle != nil {
		if generation > math.MaxInt64 {
			return ErrApplicationLifecycleRuntime
		}
		if _, err := runtime.lifecycle.HomeChanged(
			ctx, startupEventID("home-generation-rollback"), int64(generation),
		); err != nil {
			return err
		}
	}
	if !runtime.resumeQuota {
		return nil
	}
	return runtime.quota.ResumeGeneration(ctx, generation)
}

func (provider fileConfirmedHomeProvider) CurrentHome(ctx context.Context) (appLifecycle.ConfirmedHome, error) {
	if provider.loader == nil || ctx == nil {
		return appLifecycle.ConfirmedHome{}, ErrApplicationLifecycleRuntime
	}
	snapshot, err := provider.loader.LoadPreferences(ctx)
	if err != nil || snapshot.CodexHome.Generation > math.MaxInt64 {
		return appLifecycle.ConfirmedHome{}, applicationLifecycleDependencyError(ctx, err)
	}
	return appLifecycle.ConfirmedHome{
		Generation: int64(snapshot.CodexHome.Generation),
		Path:       snapshot.CodexHome.Source.Path,
		DeviceID:   snapshot.CodexHome.Source.DeviceID,
		Inode:      snapshot.CodexHome.Source.Inode,
	}, nil
}

func grokSubscriptionPlan(ctx context.Context, runtime *applicationLifecycleRuntime) string {
	if runtime == nil || runtime.repository == nil {
		return ""
	}
	snapshot, err := runtime.repository.GrokSnapshot(ctx)
	if err != nil || snapshot.Billing == nil || snapshot.BillingStale || snapshot.Billing.SubscriptionTier == nil {
		return ""
	}
	return strings.TrimSpace(*snapshot.Billing.SubscriptionTier)
}

func (runtime *applicationLifecycleRuntime) AccountSnapshot(
	ctx context.Context,
	scope agentprovider.Scope,
) (core.AccountSnapshot, error) {
	if runtime == nil || ctx == nil {
		return core.AccountSnapshot{}, ErrApplicationLifecycleRuntime
	}
	provider, err := agentprovider.Normalize(scope.Provider)
	if err != nil {
		return core.AccountSnapshot{}, err
	}
	switch provider {
	case agentprovider.Cursor:
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
	case agentprovider.Grok:
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
			plan = grokSubscriptionPlan(ctx, runtime)
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
	case agentprovider.Codex:
	default:
		return core.AccountSnapshot{}, ErrApplicationLifecycleRuntime
	}
	if runtime.settingsLoader == nil {
		return core.AccountSnapshot{}, ErrApplicationLifecycleRuntime
	}
	reader := runtime.accountReader
	if reader == nil {
		reader = appserver.ReadLocalAccount
	}
	account, err := readConfirmedApplicationAccount(
		ctx,
		fileConfirmedHomeProvider{loader: runtime.settingsLoader},
		reader,
	)
	if err != nil {
		return core.AccountSnapshot{}, err
	}
	if account == nil {
		return core.AccountSnapshot{}, nil
	}
	return core.AccountSnapshot{Account: &core.AccountIdentity{
		Type:     account.Type,
		Email:    cloneApplicationAccountField(account.Email),
		PlanType: cloneApplicationAccountField(account.PlanType),
	}}, nil
}

func readConfirmedApplicationAccount(
	ctx context.Context,
	provider appLifecycle.ConfirmedHomeProvider,
	reader applicationAccountReader,
) (*appserver.AccountSnapshot, error) {
	if ctx == nil || provider == nil || reader == nil {
		return nil, ErrApplicationLifecycleRuntime
	}
	confirmed, err := provider.CurrentHome(ctx)
	if err != nil {
		return nil, applicationLifecycleDependencyError(ctx, err)
	}
	// 在交给 descriptor guard 前重新读取逻辑快照，避免旧代际进入启动阶段。
	beforeLaunch, err := provider.CurrentHome(ctx)
	if err != nil {
		return nil, applicationLifecycleDependencyError(ctx, err)
	}
	if beforeLaunch != confirmed {
		return nil, ErrApplicationLifecycleRuntime
	}
	account, err := reader(ctx, appserver.ConfirmedHome{
		Generation: confirmed.Generation,
		Path:       confirmed.Path,
		DeviceID:   confirmed.DeviceID,
		Inode:      confirmed.Inode,
	}, appserver.ProcessOptions{BeforeStart: func(startContext context.Context) error {
		current, err := provider.CurrentHome(startContext)
		if err != nil {
			return applicationLifecycleDependencyError(startContext, err)
		}
		if current != confirmed {
			return ErrApplicationLifecycleRuntime
		}
		return nil
	}})
	if err != nil {
		return nil, applicationLifecycleDependencyError(ctx, err)
	}
	afterRead, err := provider.CurrentHome(ctx)
	if err != nil {
		return nil, applicationLifecycleDependencyError(ctx, err)
	}
	if afterRead != confirmed {
		return nil, ErrApplicationLifecycleRuntime
	}
	return account, nil
}

func cloneApplicationAccountField(value *string) *string {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}

func startupEventID(kind string) string {
	return "startup-" + kind + ":" + uuid.NewString()
}

const applicationBootstrapLaneCapacity = 1024

func enqueueLatestApplicationBootstrap(
	ctx context.Context,
	repository *store.Repository,
	queue *scheduler.Service,
	homeGeneration int64,
) error {
	if ctx == nil || repository == nil || queue == nil || homeGeneration < 0 {
		return ErrApplicationLifecycleRuntime
	}
	job, facts, err := repository.LatestBootstrapRunByGeneration(ctx, homeGeneration)
	if errors.Is(err, store.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if facts.PlanState != store.BootstrapPlanReady {
		return ErrApplicationLifecycleRuntime
	}
	if job.State != store.JobQueued && job.State != store.JobRunning {
		return nil
	}
	existing, err := repository.SchedulerTaskByTarget(ctx, job.JobID)
	if err == nil {
		if existing.TargetKind != store.SchedulerTargetBootstrap ||
			existing.HomeGeneration != homeGeneration || existing.Lane != store.SchedulerLaneBackfill ||
			existing.ServiceClass != store.SchedulerServiceInteractive {
			return ErrApplicationLifecycleRuntime
		}
		// target admission 已经持久化即可视为幂等成功。这里不能限定 active
		// state：worker 可能在读 job 与读 task 之间把 task 推进到 terminal。
		return nil
	}
	if !errors.Is(err, store.ErrNotFound) {
		return err
	}
	_, err = queue.Enqueue(ctx, scheduler.EnqueueRequest{
		TaskID: "task-" + job.JobID, DedupeKey: "bootstrap:" + job.JobID,
		TargetKind: store.SchedulerTargetBootstrap, TargetID: job.JobID,
		HomeGeneration: homeGeneration, Lane: store.SchedulerLaneBackfill,
		// 首次索引在无系统压力时使用 interactive budget 连续推进；lane
		// 仍是 backfill，因此 live task、用户 pause、sleep 与 pressure 会优先。
		ServiceClass:  store.SchedulerServiceInteractive,
		RequestedAtMS: job.CreatedAtMS, LaneCapacity: applicationBootstrapLaneCapacity,
	})
	return err
}

func nonFatalSourceStateError(err error) bool {
	return err == nil || errors.Is(err, appLifecycle.ErrSourceUnavailable) ||
		errors.Is(err, appLifecycle.ErrGenerationChanged)
}

func applicationLifecycleDependencyError(ctx context.Context, err error) error {
	if ctx != nil && ctx.Err() != nil {
		return ctx.Err()
	}
	if errors.Is(err, context.Canceled) {
		return context.Canceled
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return context.DeadlineExceeded
	}
	return ErrApplicationLifecycleRuntime
}

var _ appLifecycle.ConfirmedHomeProvider = fileConfirmedHomeProvider{}
var _ preferences.HomeRuntime = (*applicationQuotaHomeRuntime)(nil)
