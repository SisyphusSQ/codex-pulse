package app

import (
	"context"
	"errors"
	"math"
	"net/http"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/SisyphusSQ/codex-pulse/internal/bootstrap"
	"github.com/SisyphusSQ/codex-pulse/internal/codex/logs"
	quotaonline "github.com/SisyphusSQ/codex-pulse/internal/codex/quota"
	appLifecycle "github.com/SisyphusSQ/codex-pulse/internal/lifecycle"
	"github.com/SisyphusSQ/codex-pulse/internal/liveindex"
	"github.com/SisyphusSQ/codex-pulse/internal/preferences"
	"github.com/SisyphusSQ/codex-pulse/internal/scheduler"
	"github.com/SisyphusSQ/codex-pulse/internal/store"
	storesqlite "github.com/SisyphusSQ/codex-pulse/internal/store/sqlite"
)

var (
	ErrApplicationLifecycleRuntime      = errors.New("application lifecycle runtime is unavailable")
	ErrApplicationPreferencesPostCommit = errors.New("application preferences committed but reconcile failed")
)

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

type ApplicationLifecycleRuntimeConfig struct {
	Database       *storesqlite.Store
	Registrar      lifecycleEventRegistrar
	Preferences    confirmedPreferencesLoader
	EventTimeout   time.Duration
	QuotaTransport http.RoundTripper
	QuotaClock     func() time.Time
	quotaHooks     quotaRuntimeHooks
	homeRuntime    preferences.HomeRuntime
}

type applicationLifecycleRuntime struct {
	adapter     *LifecycleEventAdapter
	coordinator *appLifecycle.Coordinator
	quota       *applicationQuotaRuntime
	preferences *preferences.Service
	cancel      context.CancelFunc
	workerDone  chan error
	controlCtx  context.Context
	controlStop context.CancelFunc

	controlMu        sync.Mutex
	controlAccepting bool
	controlInflight  int
	controlDone      chan struct{}

	closeOnce sync.Once
	closeDone chan struct{}
	closeErr  error
}

func startApplicationLifecycleRuntime(
	ctx context.Context,
	config ApplicationLifecycleRuntimeConfig,
) (*applicationLifecycleRuntime, error) {
	if ctx == nil || config.Database == nil || config.Registrar == nil || config.EventTimeout < 0 {
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
	bootstrapRuntime, err := bootstrap.NewRuntime(bootstrap.RuntimeConfig{Repository: repository})
	if err != nil {
		return nil, applicationLifecycleDependencyError(ctx, err)
	}
	localHomeRuntime := preferences.HomeRuntime(bootstrapRuntime)
	if config.homeRuntime != nil {
		localHomeRuntime = config.homeRuntime
	}
	preferencesStore, hasPreferencesStore := loader.(preferences.PreferencesStore)
	quotaRuntime, err := startApplicationQuotaRuntime(ctx, ApplicationQuotaRuntimeConfig{
		Repository: repository, Preferences: loader,
		Transport: config.QuotaTransport, Clock: config.QuotaClock,
		suspended: hasPreferencesStore, hooks: config.quotaHooks,
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
			Store: preferencesStore, Probe: logs.NewHomeProbe(),
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
	})
	if err != nil {
		closeQuotaRuntime()
		return nil, applicationLifecycleDependencyError(ctx, err)
	}
	runner, err := appLifecycle.NewQueueReconcileRunner(appLifecycle.QueueReconcileRunnerConfig{
		Repository: repository, Live: liveRuntime, Queue: schedulerService,
	})
	if err != nil {
		closeQuotaRuntime()
		return nil, applicationLifecycleDependencyError(ctx, err)
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
		Registrar: config.Registrar,
		Coordinator: applicationQuotaLifecycleCoordinator{
			local: coordinator, quota: quotaRuntime,
		},
		EventTimeout: config.EventTimeout,
	})
	if err != nil {
		closeQuotaRuntime()
		closeCoordinator()
		return nil, applicationLifecycleDependencyError(ctx, err)
	}
	workerCtx, cancel := context.WithCancel(ctx)
	controlCtx, controlStop := context.WithCancel(ctx)
	runtime := &applicationLifecycleRuntime{
		adapter: adapter, coordinator: coordinator, quota: quotaRuntime,
		preferences: preferencesService, cancel: cancel,
		workerDone: make(chan error, 1), controlCtx: controlCtx, controlStop: controlStop,
		controlAccepting: true, controlDone: closedApplicationLifecycleSignal(),
		closeDone: make(chan struct{}),
	}
	go func() { runtime.workerDone <- schedulerService.Run(workerCtx) }()
	return runtime, nil
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
	if err := runtime.quota.ReconcilePreferences(controlContext); err != nil {
		return committed, &ApplicationPreferencesPostCommitError{Committed: committed, Cause: err}
	}
	return committed, nil
}

func (runtime *applicationLifecycleRuntime) ReconcileQuotaPreferences(ctx context.Context) error {
	if runtime == nil || runtime.quota == nil || ctx == nil {
		return ErrApplicationLifecycleRuntime
	}
	operationContext, finish, err := runtime.beginControlAdmission(ctx)
	if err != nil {
		return err
	}
	defer finish()
	return runtime.quota.ReconcilePreferences(operationContext)
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
	if err := runtime.quota.ResumeGeneration(ctx, committed.CodexHome.Generation); err != nil {
		postCommitErr := &ApplicationPreferencesPostCommitError{
			Committed: committed,
			Cause:     err,
		}
		return committed, errors.Join(original, postCommitErr)
	}
	return committed, original
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

func (runtime *applicationLifecycleRuntime) shutdown() {
	adapterErr := runtime.adapter.Close(context.Background())
	controlDone := runtime.sealControlAdmission()
	<-controlDone
	quotaErr := runtime.quota.Close(context.Background())
	runtime.cancel()
	workerErr := <-runtime.workerDone
	if errors.Is(workerErr, context.Canceled) {
		workerErr = nil
	}
	coordinatorErr := runtime.coordinator.Close(context.Background())
	runtime.closeErr = errors.Join(adapterErr, quotaErr, workerErr, coordinatorErr)
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

func startupEventID(kind string) string {
	return "startup-" + kind + ":" + uuid.NewString()
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
