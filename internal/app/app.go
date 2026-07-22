package app

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/SisyphusSQ/codex-pulse/internal/core"
	"github.com/SisyphusSQ/codex-pulse/internal/lightindex"
	"github.com/SisyphusSQ/codex-pulse/internal/metrics"
	"github.com/SisyphusSQ/codex-pulse/internal/preferences"
	"github.com/SisyphusSQ/codex-pulse/internal/pricing"
	factstore "github.com/SisyphusSQ/codex-pulse/internal/store"
	storesqlite "github.com/SisyphusSQ/codex-pulse/internal/store/sqlite"
)

var (
	ErrRuntime = errors.New("application runtime is unavailable")
)

type lifecycleStore interface {
	Close(context.Context) error
}

type storeOpener func(context.Context) (lifecycleStore, error)

type Config struct {
	Broker          *core.InvalidationBroker
	Store           storesqlite.Config
	PreferencesPath string
}

// Runtime owns the business graph behind the RPC transport. It contains no
// window, tray, updater, network listener, or platform UI dependency.
type Runtime struct {
	service   *core.Service
	broker    *core.InvalidationBroker
	recovery  core.MigrationRecoveryService
	lifecycle *LifecycleEventAdapter
	shutdown  *applicationShutdownCoordinator

	stopOnce sync.Once
	stop     chan string
}

func Open(ctx context.Context, config Config) (*Runtime, error) {
	if ctx == nil || config.Broker == nil {
		return nil, ErrRuntime
	}
	database, recoveryController, err := openApplicationStartup(ctx, config.Store)
	if err != nil {
		return nil, fmt.Errorf("%w: open SQLite store", ErrRuntime)
	}
	if recoveryController != nil {
		return openRecoveryRuntime(config.Broker, recoveryController)
	}
	return openNormalRuntime(ctx, config, database)
}

func openRecoveryRuntime(
	broker *core.InvalidationBroker,
	controller *migrationRecoveryController,
) (*Runtime, error) {
	recovery, err := newMigrationRecoveryService(controller)
	if err != nil {
		return nil, errors.Join(ErrRuntime, err)
	}
	runtime := &Runtime{broker: broker, recovery: recovery, stop: make(chan string, 1)}
	if err := controller.bindExit(func() { runtime.RequestStop("migration_exit") }); err != nil {
		return nil, errors.Join(ErrRuntime, err)
	}
	runtime.shutdown, err = newApplicationShutdownCoordinator(
		shutdownComponent{Name: "invalidation", Close: func(context.Context) error { broker.Close(); return nil }},
	)
	if err != nil {
		return nil, errors.Join(ErrRuntime, err)
	}
	return runtime, nil
}

func openNormalRuntime(
	ctx context.Context,
	config Config,
	database *storesqlite.Store,
) (*Runtime, error) {
	metricsRuntime, err := startApplicationMetricsRuntime(ctx, database, metrics.SamplingModeNormal)
	if err != nil {
		_ = database.Close(context.Background())
		return nil, err
	}
	preferenceStore, err := openRuntimePreferences(config.PreferencesPath)
	if err != nil {
		_ = metricsRuntime.Close(context.Background())
		_ = database.Close(context.Background())
		return nil, err
	}
	service, err := composeCoreService(database, preferenceStore, metricsRuntime.Observer())
	if err != nil {
		_ = metricsRuntime.Close(context.Background())
		_ = database.Close(context.Background())
		return nil, err
	}
	lifecycleRuntime, err := startApplicationLifecycleRuntime(ctx, ApplicationLifecycleRuntimeConfig{
		Database: database, Preferences: preferenceStore, LightMetadata: lightindex.LocalMetadataProvider{},
		Invalidation: config.Broker,
	})
	if err != nil {
		_ = metricsRuntime.Close(context.Background())
		_ = database.Close(context.Background())
		return nil, err
	}
	if lifecycleRuntime != nil {
		err = core.BindDependencies(service, core.ServiceConfig{
			QuotaRefresh: lifecycleRuntime, RuntimeControls: lifecycleRuntime, SessionDeepIndex: lifecycleRuntime,
		})
		if err != nil {
			_ = lifecycleRuntime.Close(context.Background())
			_ = metricsRuntime.Close(context.Background())
			_ = database.Close(context.Background())
			return nil, err
		}
	}
	healthRuntime, err := startApplicationHealthRuntime(ctx, database)
	if err != nil {
		closeNormalPartial(lifecycleRuntime, metricsRuntime, database)
		return nil, err
	}
	if err := core.BindDependencies(service, core.ServiceConfig{HealthProjection: healthRuntime}); err != nil {
		_ = healthRuntime.Close(context.Background())
		closeNormalPartial(lifecycleRuntime, metricsRuntime, database)
		return nil, err
	}
	retentionRuntime, err := startApplicationRetentionRuntime(ctx, database)
	if err != nil {
		_ = healthRuntime.Close(context.Background())
		closeNormalPartial(lifecycleRuntime, metricsRuntime, database)
		return nil, err
	}

	components := []shutdownComponent{}
	if lifecycleRuntime != nil {
		components = append(components, shutdownComponent{Name: "scheduler-admission", Close: lifecycleRuntime.BeginDrain})
	}
	components = append(components,
		shutdownComponent{Name: "invalidation", Close: func(context.Context) error { config.Broker.Close(); return nil }},
		shutdownComponent{Name: "retention", Close: retentionRuntime.Close},
		shutdownComponent{Name: "health", Close: healthRuntime.Close},
	)
	if lifecycleRuntime != nil {
		components = append(components, shutdownComponent{Name: "lifecycle", Close: lifecycleRuntime.Close})
	}
	components = append(components,
		shutdownComponent{Name: "metrics", Close: metricsRuntime.Close},
		shutdownComponent{Name: "sqlite", Close: database.Close},
	)
	shutdown, err := newApplicationShutdownCoordinator(components...)
	if err != nil {
		_ = retentionRuntime.Close(context.Background())
		_ = healthRuntime.Close(context.Background())
		closeNormalPartial(lifecycleRuntime, metricsRuntime, database)
		return nil, err
	}
	runtime := &Runtime{
		service: service, broker: config.Broker, shutdown: shutdown, stop: make(chan string, 1),
	}
	if lifecycleRuntime != nil {
		runtime.lifecycle = lifecycleRuntime.adapter
	}
	return runtime, nil
}

func closeNormalPartial(
	lifecycle *applicationLifecycleRuntime,
	metricsRuntime *applicationMetricsRuntime,
	database *storesqlite.Store,
) {
	if lifecycle != nil {
		_ = lifecycle.Close(context.Background())
	}
	_ = metricsRuntime.Close(context.Background())
	_ = database.Close(context.Background())
}

func openRuntimePreferences(path string) (*preferences.FileStore, error) {
	if path == "" {
		return openApplicationPreferences()
	}
	return openApplicationPreferencesAt(path)
}

func (runtime *Runtime) Service() *core.Service {
	if runtime == nil {
		return nil
	}
	return runtime.service
}

func (runtime *Runtime) Broker() *core.InvalidationBroker {
	if runtime == nil {
		return nil
	}
	return runtime.broker
}

func (runtime *Runtime) Recovery() core.MigrationRecoveryService {
	if runtime == nil {
		return nil
	}
	return runtime.recovery
}

func (runtime *Runtime) NotifyLifecycle(ctx context.Context, event string) error {
	if runtime == nil || runtime.lifecycle == nil {
		return ErrRuntime
	}
	return runtime.lifecycle.NotifyLifecycle(ctx, event)
}

func (runtime *Runtime) RequestStop(reason string) bool {
	if runtime == nil || runtime.stop == nil || reason == "" {
		return false
	}
	accepted := false
	runtime.stopOnce.Do(func() {
		runtime.stop <- reason
		accepted = true
	})
	return accepted
}

func (runtime *Runtime) RequestShutdown(reason string) bool {
	return runtime.RequestStop(reason)
}

func (runtime *Runtime) StopRequested() <-chan string {
	if runtime == nil {
		return nil
	}
	return runtime.stop
}

func (runtime *Runtime) Close(ctx context.Context) error {
	if runtime == nil || runtime.shutdown == nil || ctx == nil {
		return ErrRuntime
	}
	return runtime.shutdown.Close(ctx)
}

func openApplicationStore(ctx context.Context) (lifecycleStore, error) {
	return openConfiguredStore(ctx, storesqlite.Config{})
}

func openApplicationStartup(
	ctx context.Context,
	config storesqlite.Config,
) (*storesqlite.Store, *migrationRecoveryController, error) {
	database, err := storesqlite.Open(ctx, config)
	if err != nil {
		return nil, nil, err
	}
	repository := factstore.NewRepository(database)
	if _, err := repository.MigrateApplicationSchema(ctx); err != nil {
		normalized := database.Config()
		closeErr := database.Close(context.WithoutCancel(ctx))
		failure := migrationFailureFrom(err)
		if failure == nil {
			return nil, nil, errors.Join(err, closeErr)
		}
		recovery, recoveryErr := newMigrationRecoveryController(normalized, failure)
		if recoveryErr != nil {
			return nil, nil, errors.Join(err, closeErr, recoveryErr)
		}
		return nil, recovery, closeErr
	}
	if err := installBuiltinPricingCatalog(ctx, repository); err != nil {
		closeErr := database.Close(context.WithoutCancel(ctx))
		return nil, nil, errors.Join(err, closeErr)
	}
	return database, nil, nil
}

func openConfiguredStore(ctx context.Context, config storesqlite.Config) (lifecycleStore, error) {
	database, err := openBootstrappedStore(
		ctx,
		func(ctx context.Context) (*storesqlite.Store, error) { return storesqlite.Open(ctx, config) },
		func(ctx context.Context, database *storesqlite.Store) error {
			repository := factstore.NewRepository(database)
			if err := repository.EnsureApplicationSchema(ctx); err != nil {
				return fmt.Errorf("ensure application schema: %w", err)
			}
			if err := installBuiltinPricingCatalog(ctx, repository); err != nil {
				return fmt.Errorf("install builtin pricing catalog: %w", err)
			}
			return nil
		},
	)
	if err != nil {
		return nil, err
	}
	return database, nil
}

func installBuiltinPricingCatalog(ctx context.Context, repository *factstore.Repository) error {
	for _, catalog := range pricing.BuiltinOpenAICatalog() {
		if err := repository.AddPricingVersion(ctx, catalog); err != nil {
			return err
		}
	}
	return nil
}

func openBootstrappedStore[T lifecycleStore](
	ctx context.Context,
	open func(context.Context) (T, error),
	bootstrap func(context.Context, T) error,
) (T, error) {
	var zero T
	store, err := open(ctx)
	if err != nil {
		return zero, err
	}
	if err := bootstrap(ctx, store); err != nil {
		closeErr := store.Close(context.WithoutCancel(ctx))
		if closeErr != nil {
			closeErr = fmt.Errorf("close application SQLite store after schema failure: %w", closeErr)
		}
		return zero, errors.Join(err, closeErr)
	}
	return store, nil
}

func runWithStore(
	ctx context.Context,
	openStore storeOpener,
	runApplication func(lifecycleStore) error,
) (returnErr error) {
	store, err := openStore(ctx)
	if err != nil {
		return fmt.Errorf("open application SQLite store: %w", err)
	}
	defer func() {
		closeErr := store.Close(context.WithoutCancel(ctx))
		if closeErr != nil {
			closeErr = fmt.Errorf("close application SQLite store: %w", closeErr)
		}
		returnErr = errors.Join(returnErr, closeErr)
	}()
	return runApplication(store)
}
