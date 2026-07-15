package app

import (
	"context"
	"errors"
	"math"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/SisyphusSQ/codex-pulse/internal/bootstrap"
	appLifecycle "github.com/SisyphusSQ/codex-pulse/internal/lifecycle"
	"github.com/SisyphusSQ/codex-pulse/internal/liveindex"
	"github.com/SisyphusSQ/codex-pulse/internal/preferences"
	"github.com/SisyphusSQ/codex-pulse/internal/scheduler"
	"github.com/SisyphusSQ/codex-pulse/internal/store"
	storesqlite "github.com/SisyphusSQ/codex-pulse/internal/store/sqlite"
)

var ErrApplicationLifecycleRuntime = errors.New("application lifecycle runtime is unavailable")

type confirmedPreferencesLoader interface {
	LoadPreferences(context.Context) (preferences.Snapshot, error)
}

type ApplicationLifecycleRuntimeConfig struct {
	Database     *storesqlite.Store
	Registrar    lifecycleEventRegistrar
	Preferences  confirmedPreferencesLoader
	EventTimeout time.Duration
}

type applicationLifecycleRuntime struct {
	adapter     *LifecycleEventAdapter
	coordinator *appLifecycle.Coordinator
	cancel      context.CancelFunc
	workerDone  chan error

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
	homeGeneration := int64(snapshot.CodexHome.Generation)
	repository := store.NewRepository(config.Database)
	bootstrapRuntime, err := bootstrap.NewRuntime(bootstrap.RuntimeConfig{Repository: repository})
	if err != nil {
		return nil, applicationLifecycleDependencyError(ctx, err)
	}
	liveRuntime, err := liveindex.New(liveindex.Config{Repository: repository})
	if err != nil {
		return nil, applicationLifecycleDependencyError(ctx, err)
	}
	bootstrapExecutor, err := scheduler.NewBootstrapExecutor(bootstrapRuntime)
	if err != nil {
		return nil, applicationLifecycleDependencyError(ctx, err)
	}
	liveExecutor, err := scheduler.NewLiveExecutor(liveRuntime)
	if err != nil {
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
		return nil, applicationLifecycleDependencyError(ctx, err)
	}
	runner, err := appLifecycle.NewQueueReconcileRunner(appLifecycle.QueueReconcileRunnerConfig{
		Repository: repository, Live: liveRuntime, Queue: schedulerService,
	})
	if err != nil {
		return nil, applicationLifecycleDependencyError(ctx, err)
	}
	reconciler, err := appLifecycle.NewConfirmedHomeReconciler(appLifecycle.ConfirmedHomeReconcilerConfig{
		HomeProvider: fileConfirmedHomeProvider{loader: loader}, Runner: runner,
	})
	if err != nil {
		return nil, applicationLifecycleDependencyError(ctx, err)
	}
	coordinator, err := appLifecycle.NewCoordinator(appLifecycle.Config{
		Repository: repository, Scheduler: schedulerService, Reconciler: reconciler,
	})
	if err != nil {
		return nil, applicationLifecycleDependencyError(ctx, err)
	}
	closeCoordinator := func() { _ = coordinator.Close(context.Background()) }
	state, err := repository.SchedulerLifecycle(ctx)
	if errors.Is(err, store.ErrNotFound) {
		state, err = coordinator.Initialize(ctx, homeGeneration, startupEventID("initialize"))
	}
	if err != nil {
		closeCoordinator()
		return nil, applicationLifecycleDependencyError(ctx, err)
	}
	if state.HomeGeneration == homeGeneration {
		// Revalidate the confirmed physical Home before any target recovery can
		// interrupt or rebind durable jobs. A stale path with the same logical
		// generation must fail closed without target-side effects.
		_, wakeErr := coordinator.SystemDidWake(ctx, startupEventID("wake"))
		if !nonFatalSourceStateError(wakeErr) {
			closeCoordinator()
			return nil, applicationLifecycleDependencyError(ctx, wakeErr)
		}
		_, recoverErr := coordinator.Recover(ctx, startupEventID("recover"))
		if !nonFatalSourceStateError(recoverErr) {
			closeCoordinator()
			return nil, applicationLifecycleDependencyError(ctx, recoverErr)
		}
	} else {
		// Preferences 已切到其它generation时绝不恢复旧任务；在Home切换控制面
		// 完成generation handoff前保持fail-closed blocked。
		if _, blockErr := coordinator.SourceChanged(ctx, startupEventID("generation-mismatch"), false); blockErr != nil {
			closeCoordinator()
			return nil, applicationLifecycleDependencyError(ctx, blockErr)
		}
	}
	adapter, err := NewLifecycleEventAdapter(LifecycleEventAdapterConfig{
		Registrar: config.Registrar, Coordinator: coordinator, EventTimeout: config.EventTimeout,
	})
	if err != nil {
		closeCoordinator()
		return nil, applicationLifecycleDependencyError(ctx, err)
	}
	workerCtx, cancel := context.WithCancel(ctx)
	runtime := &applicationLifecycleRuntime{
		adapter: adapter, coordinator: coordinator, cancel: cancel,
		workerDone: make(chan error, 1), closeDone: make(chan struct{}),
	}
	go func() { runtime.workerDone <- schedulerService.Run(workerCtx) }()
	return runtime, nil
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
	runtime.cancel()
	workerErr := <-runtime.workerDone
	if errors.Is(workerErr, context.Canceled) {
		workerErr = nil
	}
	coordinatorErr := runtime.coordinator.Close(context.Background())
	runtime.closeErr = errors.Join(adapterErr, workerErr, coordinatorErr)
	close(runtime.closeDone)
}

type fileConfirmedHomeProvider struct {
	loader confirmedPreferencesLoader
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
