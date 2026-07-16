package app

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"time"

	quotaonline "github.com/SisyphusSQ/codex-pulse/internal/codex/quota"
	"github.com/SisyphusSQ/codex-pulse/internal/scheduler"
	"github.com/SisyphusSQ/codex-pulse/internal/store"
)

const quotaRuntimeRecordTimeout = 5 * time.Second

var ErrApplicationQuotaRuntime = errors.New("application quota runtime is unavailable")

type ApplicationQuotaRuntimeConfig struct {
	Repository   *store.Repository
	Preferences  confirmedPreferencesLoader
	Transport    http.RoundTripper
	Clock        func() time.Time
	suspended    bool
	hooks        quotaRuntimeHooks
	invalidation queryInvalidationNotifier
}

type quotaRuntimeHooks struct {
	afterAdmission        func()
	afterAdmissionContext func(context.Context)
	afterAdmissionSealed  func()
	beforeResumeReadback  func()
	runRunner             func(context.Context) error
}

type applicationQuotaRuntime struct {
	coordinator          *scheduler.QuotaRefreshCoordinator
	runner               *scheduler.QuotaRefreshRunner
	preferences          confirmedPreferencesLoader
	reconcilePreferences func(context.Context) error
	rootContext          context.Context
	rootCancel           context.CancelFunc
	hooks                quotaRuntimeHooks

	mu                sync.Mutex
	generation        uint64
	accepting         bool
	closed            bool
	generationContext context.Context
	generationCancel  context.CancelFunc
	runnerDone        chan struct{}
	runnerErr         error
	runtimeErr        error
	inflight          int
	inflightDone      chan struct{}
	transition        chan struct{}

	closeOnce sync.Once
	closeDone chan struct{}
	closeErr  error
}

type applicationQuotaLifecycleCoordinator struct {
	local systemLifecycleCoordinator
	quota *applicationQuotaRuntime
}

func startApplicationQuotaRuntime(
	ctx context.Context,
	config ApplicationQuotaRuntimeConfig,
) (*applicationQuotaRuntime, error) {
	if ctx == nil || config.Repository == nil || config.Preferences == nil {
		return nil, ErrApplicationQuotaRuntime
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	snapshot, err := config.Preferences.LoadPreferences(ctx)
	if err != nil || snapshot.CodexHome.Generation == 0 {
		return nil, applicationQuotaDependencyError(ctx, err)
	}
	credentials, err := newAuthFileCredentialProvider(config.Preferences)
	if err != nil {
		return nil, applicationQuotaDependencyError(ctx, err)
	}
	clientConfig := quotaonline.ClientConfig{
		Transport:   config.Transport,
		Credentials: credentials,
		Now:         config.Clock,
	}
	quotaClient, err := quotaonline.NewClient(clientConfig)
	if err != nil {
		return nil, applicationQuotaDependencyError(ctx, err)
	}
	resetCreditsClient, err := quotaonline.NewResetCreditsClient(clientConfig)
	if err != nil {
		return nil, applicationQuotaDependencyError(ctx, err)
	}
	quotaService, err := quotaonline.NewService(
		quotaClient,
		config.Repository,
		quotaRuntimeRecordTimeout,
	)
	if err != nil {
		return nil, applicationQuotaDependencyError(ctx, err)
	}
	resetCreditsService, err := quotaonline.NewResetCreditsService(
		resetCreditsClient,
		config.Repository,
		quotaRuntimeRecordTimeout,
	)
	if err != nil {
		return nil, applicationQuotaDependencyError(ctx, err)
	}
	coordinator, err := scheduler.NewQuotaRefreshCoordinator(scheduler.QuotaRefreshCoordinatorConfig{
		Repository:          config.Repository,
		Preferences:         config.Preferences,
		QuotaFetcher:        scheduler.AdaptQuotaFetchService(quotaService),
		ResetCreditsFetcher: scheduler.AdaptResetCreditsFetchService(resetCreditsService),
		Clock:               config.Clock,
		RefreshCommitted: func(ctx context.Context, _ quotaonline.RefreshSource) {
			notifyQueryInvalidation(config.invalidation, ctx, QueryInvalidationQuota)
		},
	})
	if err != nil {
		return nil, applicationQuotaDependencyError(ctx, err)
	}
	runner, err := scheduler.NewQuotaRefreshRunner(coordinator)
	if err != nil {
		return nil, applicationQuotaDependencyError(ctx, err)
	}
	rootContext, rootCancel := context.WithCancel(ctx)
	runtime := &applicationQuotaRuntime{
		coordinator: coordinator, runner: runner, preferences: config.Preferences,
		reconcilePreferences: coordinator.ReconcilePreferences,
		rootContext:          rootContext, rootCancel: rootCancel, hooks: config.hooks,
		inflightDone: closedQuotaRuntimeSignal(), closeDone: make(chan struct{}),
		transition: make(chan struct{}, 1),
	}
	runtime.transition <- struct{}{}
	if config.suspended {
		generationContext, generationCancel := context.WithCancel(rootContext)
		generationCancel()
		runtime.generation = snapshot.CodexHome.Generation
		runtime.generationContext = generationContext
		runtime.generationCancel = generationCancel
		runtime.runnerDone = closedQuotaRuntimeSignal()
	} else {
		runtime.startRunnerLocked(snapshot.CodexHome.Generation)
	}
	return runtime, nil
}

func (runtime *applicationQuotaRuntime) Close(ctx context.Context) error {
	if runtime == nil || ctx == nil {
		return ErrApplicationQuotaRuntime
	}
	runtime.closeOnce.Do(func() {
		runnerDone, inflightDone, sealedHook := runtime.sealForClose()
		if sealedHook != nil {
			sealedHook()
		}
		go runtime.shutdown(runnerDone, inflightDone)
	})
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-runtime.closeDone:
		return runtime.closeErr
	}
}

func (runtime *applicationQuotaRuntime) ReconcilePreferences(ctx context.Context) error {
	operationContext, finish, err := runtime.beginAdmission(ctx)
	if err != nil {
		return err
	}
	defer finish()
	return runtime.reconcileAdmitted(operationContext)
}

func (runtime *applicationQuotaRuntime) RequestRefresh(
	ctx context.Context,
	source quotaonline.RefreshSource,
	trigger store.SourceRefreshTrigger,
) (store.SourceRefreshSchedule, error) {
	operationContext, finish, err := runtime.beginAdmission(ctx)
	if err != nil {
		return store.SourceRefreshSchedule{}, err
	}
	defer finish()
	return runtime.coordinator.RequestRefresh(operationContext, source, trigger)
}

func (runtime *applicationQuotaRuntime) requestLifecycleRefresh(
	ctx context.Context,
	trigger store.SourceRefreshTrigger,
) error {
	if runtime == nil || ctx == nil {
		return ErrApplicationQuotaRuntime
	}
	var refreshErr error
	for _, source := range []quotaonline.RefreshSource{
		quotaonline.RefreshSourceQuota,
		quotaonline.RefreshSourceResetCredits,
	} {
		_, err := runtime.RequestRefresh(ctx, source, trigger)
		refreshErr = errors.Join(refreshErr, err)
	}
	return refreshErr
}

func (runtime *applicationQuotaRuntime) beginAdmission(
	ctx context.Context,
) (context.Context, func(), error) {
	if runtime == nil || runtime.coordinator == nil || ctx == nil {
		return nil, nil, ErrApplicationQuotaRuntime
	}
	runtime.mu.Lock()
	if runtime.closed || !runtime.accepting || runtime.generationContext == nil ||
		runtime.generationContext.Err() != nil {
		runtime.mu.Unlock()
		return nil, nil, ErrApplicationQuotaRuntime
	}
	if runtime.inflight == 0 {
		runtime.inflightDone = make(chan struct{})
	}
	runtime.inflight++
	generationContext := runtime.generationContext
	afterAdmission := runtime.hooks.afterAdmission
	afterAdmissionContext := runtime.hooks.afterAdmissionContext
	runtime.mu.Unlock()

	operationContext, cancel := context.WithCancel(ctx)
	stopGenerationCancel := context.AfterFunc(generationContext, cancel)
	var finishOnce sync.Once
	finish := func() {
		finishOnce.Do(func() {
			stopGenerationCancel()
			cancel()
			runtime.mu.Lock()
			runtime.inflight--
			if runtime.inflight == 0 {
				close(runtime.inflightDone)
			}
			runtime.mu.Unlock()
		})
	}
	if afterAdmission != nil {
		afterAdmission()
	}
	if afterAdmissionContext != nil {
		afterAdmissionContext(operationContext)
	}
	return operationContext, finish, nil
}

func (runtime *applicationQuotaRuntime) reconcileAdmitted(ctx context.Context) error {
	if runtime == nil || runtime.reconcilePreferences == nil || ctx == nil {
		return ErrApplicationQuotaRuntime
	}
	return runtime.reconcilePreferences(ctx)
}

func (runtime *applicationQuotaRuntime) DrainGeneration(ctx context.Context, generation uint64) error {
	if runtime == nil || ctx == nil || generation == 0 {
		return ErrApplicationQuotaRuntime
	}
	finishTransition, err := runtime.beginGenerationTransition(ctx)
	if err != nil {
		return err
	}
	defer finishTransition()
	runtime.mu.Lock()
	if runtime.closed || runtime.generation != generation {
		runtime.mu.Unlock()
		return ErrApplicationQuotaRuntime
	}
	wasAccepting := runtime.accepting
	runtime.accepting = false
	if runtime.generationCancel != nil {
		runtime.generationCancel()
	}
	runnerDone := runtime.runnerDone
	inflightDone := runtime.inflightDone
	var sealedHook func()
	if wasAccepting {
		sealedHook = runtime.hooks.afterAdmissionSealed
	}
	runtime.mu.Unlock()
	if sealedHook != nil {
		sealedHook()
	}
	if err := waitForQuotaRuntimeSignal(ctx, runnerDone); err != nil {
		return err
	}
	if err := waitForQuotaRuntimeSignal(ctx, inflightDone); err != nil {
		return err
	}
	runtime.mu.Lock()
	defer runtime.mu.Unlock()
	return runtime.runnerErr
}

func (runtime *applicationQuotaRuntime) ResumeGeneration(ctx context.Context, generation uint64) error {
	if runtime == nil || ctx == nil || generation == 0 {
		return ErrApplicationQuotaRuntime
	}
	finishTransition, err := runtime.beginGenerationTransition(ctx)
	if err != nil {
		return err
	}
	defer finishTransition()
	runtime.mu.Lock()
	if runtime.closed {
		runtime.mu.Unlock()
		return ErrApplicationQuotaRuntime
	}
	if runtime.accepting {
		if runtime.generation == generation {
			runtime.mu.Unlock()
			return nil
		}
		runtime.mu.Unlock()
		return ErrApplicationQuotaRuntime
	}
	runnerDone := runtime.runnerDone
	inflightDone := runtime.inflightDone
	beforeResumeReadback := runtime.hooks.beforeResumeReadback
	runtime.mu.Unlock()
	if beforeResumeReadback != nil {
		beforeResumeReadback()
	}
	if err := waitForQuotaRuntimeSignal(ctx, runnerDone); err != nil {
		return err
	}
	if err := waitForQuotaRuntimeSignal(ctx, inflightDone); err != nil {
		return err
	}
	snapshot, err := runtime.preferences.LoadPreferences(ctx)
	if err != nil || snapshot.CodexHome.Generation != generation {
		return applicationQuotaDependencyError(ctx, err)
	}
	runtime.mu.Lock()
	defer runtime.mu.Unlock()
	if runtime.closed {
		return ErrApplicationQuotaRuntime
	}
	if runtime.accepting {
		if runtime.generation == generation {
			return nil
		}
		return ErrApplicationQuotaRuntime
	}
	runtime.startRunnerLocked(generation)
	return nil
}

func (runtime *applicationQuotaRuntime) startRunnerLocked(generation uint64) {
	generationContext, generationCancel := context.WithCancel(runtime.rootContext)
	runnerDone := make(chan struct{})
	runtime.generation = generation
	runtime.accepting = true
	runtime.generationContext = generationContext
	runtime.generationCancel = generationCancel
	runtime.runnerDone = runnerDone
	runtime.runnerErr = nil
	go runtime.runRunner(generationContext, runnerDone)
}

func (runtime *applicationQuotaRuntime) runRunner(ctx context.Context, done chan struct{}) {
	var err error
	if runtime.hooks.runRunner != nil {
		err = runtime.hooks.runRunner(ctx)
	} else {
		err = runtime.runner.Run(ctx)
	}
	if errors.Is(err, context.Canceled) {
		err = nil
	}
	runtime.mu.Lock()
	if runtime.runnerDone == done {
		runtime.runnerErr = err
		if err != nil {
			runtime.accepting = false
			runtime.generationCancel()
			runtime.runtimeErr = errors.Join(runtime.runtimeErr, err)
		} else if ctx.Err() != nil {
			runtime.accepting = false
		}
	}
	close(done)
	runtime.mu.Unlock()
}

func (runtime *applicationQuotaRuntime) sealForClose() (chan struct{}, chan struct{}, func()) {
	runtime.mu.Lock()
	wasAccepting := runtime.accepting
	runtime.closed = true
	runtime.accepting = false
	runtime.rootCancel()
	runtime.generationCancel()
	runnerDone := runtime.runnerDone
	inflightDone := runtime.inflightDone
	var sealedHook func()
	if wasAccepting {
		sealedHook = runtime.hooks.afterAdmissionSealed
	}
	runtime.mu.Unlock()
	return runnerDone, inflightDone, sealedHook
}

func (runtime *applicationQuotaRuntime) beginGenerationTransition(
	ctx context.Context,
) (func(), error) {
	if runtime == nil || ctx == nil || runtime.transition == nil {
		return nil, ErrApplicationQuotaRuntime
	}
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-runtime.transition:
		var once sync.Once
		return func() {
			once.Do(func() { runtime.transition <- struct{}{} })
		}, nil
	}
}

func (runtime *applicationQuotaRuntime) shutdown(runnerDone, inflightDone chan struct{}) {
	<-runnerDone
	<-inflightDone
	runtime.mu.Lock()
	runtime.closeErr = runtime.runtimeErr
	runtime.mu.Unlock()
	close(runtime.closeDone)
}

func waitForQuotaRuntimeSignal(ctx context.Context, signal <-chan struct{}) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-signal:
		return nil
	}
}

func closedQuotaRuntimeSignal() chan struct{} {
	signal := make(chan struct{})
	close(signal)
	return signal
}

func applicationQuotaDependencyError(ctx context.Context, err error) error {
	if ctx != nil && ctx.Err() != nil {
		return ctx.Err()
	}
	if errors.Is(err, context.Canceled) {
		return context.Canceled
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return context.DeadlineExceeded
	}
	return ErrApplicationQuotaRuntime
}

func (coordinator applicationQuotaLifecycleCoordinator) SystemWillSleep(
	ctx context.Context,
	eventID string,
) (store.SchedulerLifecycle, error) {
	if coordinator.local == nil || coordinator.quota == nil {
		return store.SchedulerLifecycle{}, ErrApplicationQuotaRuntime
	}
	return coordinator.local.SystemWillSleep(ctx, eventID)
}

func (coordinator applicationQuotaLifecycleCoordinator) SystemDidWake(
	ctx context.Context,
	eventID string,
) (store.SchedulerLifecycle, error) {
	if coordinator.local == nil || coordinator.quota == nil {
		return store.SchedulerLifecycle{}, ErrApplicationQuotaRuntime
	}
	state, localErr := coordinator.local.SystemDidWake(ctx, eventID)
	quotaErr := coordinator.quota.requestLifecycleRefresh(ctx, store.RefreshTriggerWake)
	return state, errors.Join(localErr, quotaErr)
}

func (coordinator applicationQuotaLifecycleCoordinator) SourceChanged(
	ctx context.Context,
	eventID string,
	available bool,
) (store.SchedulerLifecycle, error) {
	if coordinator.local == nil || coordinator.quota == nil {
		return store.SchedulerLifecycle{}, ErrApplicationQuotaRuntime
	}
	state, localErr := coordinator.local.SourceChanged(ctx, eventID, available)
	if !available {
		return state, localErr
	}
	quotaErr := coordinator.quota.requestLifecycleRefresh(ctx, store.RefreshTriggerForeground)
	return state, errors.Join(localErr, quotaErr)
}

var _ systemLifecycleCoordinator = applicationQuotaLifecycleCoordinator{}
