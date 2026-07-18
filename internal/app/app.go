package app

import (
	"context"
	"errors"
	"fmt"
	"io/fs"

	"github.com/SisyphusSQ/codex-pulse/internal/metrics"
	platformtray "github.com/SisyphusSQ/codex-pulse/internal/platform/tray"
	"github.com/SisyphusSQ/codex-pulse/internal/pricing"
	"github.com/SisyphusSQ/codex-pulse/internal/singleinstance"
	factstore "github.com/SisyphusSQ/codex-pulse/internal/store"
	storesqlite "github.com/SisyphusSQ/codex-pulse/internal/store/sqlite"
	"github.com/SisyphusSQ/codex-pulse/internal/updater"
	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
)

const appDescription = "Local-first Codex usage and quota desktop companion"

type lifecycleStore interface {
	Close(context.Context) error
}

type storeOpener func(context.Context) (lifecycleStore, error)

func applicationOptions(assets fs.FS, services ...application.Service) application.Options {
	return application.Options{
		Name:        appName,
		Description: appDescription,
		Services:    services,
		Assets: application.AssetOptions{
			Handler: application.AssetFileServerFS(assets),
		},
		Mac: application.MacOptions{
			ApplicationShouldTerminateAfterLastWindowClosed: false,
		},
	}
}

func mainWindowOptions() application.WebviewWindowOptions {
	return application.WebviewWindowOptions{
		Name:             "main",
		Title:            appName,
		Width:            1120,
		Height:           720,
		MinWidth:         900,
		MinHeight:        600,
		URL:              "/",
		CloseButtonState: application.ButtonHidden,
		KeyBindings: map[string]func(application.Window){
			"cmd+w": func(window application.Window) { window.Hide() },
		},
		Mac: application.MacWindow{
			Backdrop:                application.MacBackdropTranslucent,
			InvisibleTitleBarHeight: 52,
			TitleBar:                application.MacTitleBarHiddenInset,
		},
		BackgroundColour: application.NewRGB(242, 245, 249),
	}
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
	if err := repository.AddPricingVersion(ctx, pricing.BuiltinOpenAI20260714()); err != nil {
		closeErr := database.Close(context.WithoutCancel(ctx))
		return nil, nil, errors.Join(err, closeErr)
	}
	return database, nil, nil
}

func openConfiguredStore(ctx context.Context, config storesqlite.Config) (lifecycleStore, error) {
	database, err := openBootstrappedStore(
		ctx,
		func(ctx context.Context) (*storesqlite.Store, error) {
			return storesqlite.Open(ctx, config)
		},
		func(ctx context.Context, database *storesqlite.Store) error {
			repository := factstore.NewRepository(database)
			if err := repository.EnsureApplicationSchema(ctx); err != nil {
				return fmt.Errorf("ensure application schema: %w", err)
			}
			if err := repository.AddPricingVersion(ctx, pricing.BuiltinOpenAI20260714()); err != nil {
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

// Run composes and starts the desktop application. The call blocks until the
// application exits and returns Wails startup or shutdown failures to main.
func Run(assets fs.FS) error {
	ctx := context.Background()
	instanceConfig, err := singleinstance.DefaultConfig()
	if err != nil {
		return err
	}
	instanceLease, owner, err := singleinstance.Acquire(ctx, instanceConfig)
	if err != nil {
		return err
	}
	if !owner {
		return nil
	}
	defer instanceLease.Close()
	database, recovery, err := openApplicationStartup(ctx, storesqlite.Config{})
	if err != nil {
		return fmt.Errorf("open application SQLite store: %w", err)
	}
	if recovery != nil {
		return runMigrationRecoveryApplication(ctx, assets, recovery, instanceLease)
	}
	return runWithStore(ctx, func(context.Context) (lifecycleStore, error) { return database, nil }, func(owned lifecycleStore) (returnErr error) {
		database, ok := owned.(*storesqlite.Store)
		if !ok {
			return ErrApplicationLifecycleRuntime
		}
		metricsRuntime, err := startApplicationMetricsRuntime(ctx, database, metrics.SamplingModeNormal)
		if err != nil {
			return err
		}
		defer func() {
			returnErr = errors.Join(returnErr, metricsRuntime.Close(context.Background()))
		}()
		preferenceStore, err := openApplicationPreferences()
		if err != nil {
			return err
		}
		bindingService, err := composeBindingService(database, preferenceStore, metricsRuntime.Observer())
		if err != nil {
			return err
		}
		nativeQuit := &nativeQuitPreflight{}
		options := applicationOptions(assets, wailsStartupService(newStartupService(nil)), wailsBindingService(bindingService))
		options.ShouldQuit = nativeQuit.ShouldQuit
		desktopApp := application.New(options)
		// This is the earliest shutdown hook and is intentionally non-blocking:
		// platform terminate paths that bypass ShouldQuit must never ACK a second
		// instance while the current owner is already exiting.
		desktopApp.OnShutdown(instanceLease.StopAcceptingWakes)
		updateRuntime, err := startApplicationUpdater(ctx, updater.NewSparkleAdapter(), preferenceStore, func(snapshot updater.Snapshot) {
			if snapshot.Phase == updater.PhaseInstalling {
				nativeQuit.MarkInstallReady()
			} else if snapshot.Phase == updater.PhaseError {
				nativeQuit.AbortInstall()
			}
			desktopApp.Event.Emit(UpdateStateChangedEventName, UpdateStateChangedEvent{Version: UpdateStateChangedContractVersion})
		})
		if err != nil {
			return err
		}
		if err := bindingService.bindUpdateControls(updateRuntime); err != nil {
			return err
		}
		desktopApp.OnShutdown(func() {
			// Sparkle owns main-queue objects and reply blocks. Release them while
			// the AppKit loop is alive; the defer below performs error readback.
			_ = updateRuntime.Close()
		})
		defer func() {
			returnErr = errors.Join(returnErr, updateRuntime.Close())
		}()
		mainWindow := desktopApp.Window.NewWithOptions(mainWindowOptions())
		mainWindow.RegisterHook(events.Common.WindowClosing, func(event *application.WindowEvent) {
			mainWindow.Hide()
			event.Cancel()
		})
		popoverWindow := desktopApp.Window.NewWithOptions(popoverWindowOptions())
		popover, err := newPopoverController(popoverWindow)
		if err != nil {
			return err
		}
		var desktopCommands *desktopCommandCoordinator
		trayHost, err := newTrayRuntimeHost(desktopApp.Event, bindingService, desktopApp.Quit, func(item *platformtray.NativeStatusItem) error {
			if desktopCommands == nil {
				return ErrDesktopCommand
			}
			if err := popover.ConfigureStatusItem(item); err != nil {
				return err
			}
			if err := item.SetPlatformChangeHandler(func(change platformtray.PlatformChange) {
				desktopApp.Event.Emit(PlatformChangedEventName, change)
			}); err != nil {
				return err
			}
			return item.SetMenuHandler(desktopCommands.Handle)
		})
		if err != nil {
			return err
		}
		desktopApp.OnShutdown(func() {
			// Stop refresh and remove NSStatusItem while the AppKit event loop is
			// still alive. The defer below performs idempotent error readback.
			_ = trayHost.Close(context.Background())
		})
		defer func() {
			returnErr = errors.Join(returnErr, trayHost.Close(context.Background()))
		}()
		invalidation, err := newQueryInvalidationPublisher(QueryInvalidationPublisherConfig{
			Emitter:     desktopApp.Event,
			Health:      factstore.NewRepository(database),
			AfterNotify: trayHost.Invalidate,
		})
		if err != nil {
			return err
		}
		runtime, err := startApplicationLifecycleRuntime(ctx, ApplicationLifecycleRuntimeConfig{
			Database: database, Registrar: desktopApp.Event, Preferences: preferenceStore,
			Invalidation: invalidation,
			UpdateWake: func(ctx context.Context) error {
				_, wakeErr := updateRuntime.Wake(ctx)
				return wakeErr
			},
		})
		if err != nil {
			return err
		}
		if runtime != nil {
			if err := bindingService.bindQuotaRefresh(runtime); err != nil {
				return err
			}
			if err := bindingService.bindRuntimeControls(runtime); err != nil {
				return err
			}
			defer func() {
				returnErr = errors.Join(returnErr, runtime.Close(context.Background()))
			}()
		}
		healthRuntime, err := startApplicationHealthRuntime(ctx, database)
		if err != nil {
			return err
		}
		if err := bindingService.bindHealthProjection(healthRuntime); err != nil {
			return err
		}
		defer func() {
			returnErr = errors.Join(returnErr, healthRuntime.Close(context.Background()))
		}()
		retentionRuntime, err := startApplicationRetentionRuntime(ctx, database)
		if err != nil {
			return err
		}
		// Retention starts last and therefore closes first, before health,
		// lifecycle, metrics, and the SQLite owner are torn down.
		defer func() {
			returnErr = errors.Join(returnErr, retentionRuntime.Close(context.Background()))
		}()

		components := []shutdownComponent{
			{Name: "instance-wake-admission", Close: func(context.Context) error { instanceLease.StopAcceptingWakes(); return nil }},
			{Name: "updater-scheduler", Close: func(context.Context) error { return updateRuntime.Suspend() }},
		}
		if runtime != nil {
			components = append(components, shutdownComponent{Name: "scheduler-admission", Close: runtime.BeginDrain})
		}
		components = append(components,
			shutdownComponent{Name: "tray", Close: trayHost.Close},
			shutdownComponent{Name: "retention", Close: retentionRuntime.Close},
			shutdownComponent{Name: "health", Close: healthRuntime.Close},
		)
		if runtime != nil {
			components = append(components, shutdownComponent{Name: "scheduler", Close: runtime.Close})
		}
		components = append(components,
			shutdownComponent{Name: "metrics", Close: metricsRuntime.Close},
			shutdownComponent{Name: "sqlite", Close: database.Close},
			shutdownComponent{Name: "instance-lock", Close: func(context.Context) error { return instanceLease.Close() }},
		)
		shutdown, err := newApplicationShutdownCoordinator(components...)
		if err != nil {
			return err
		}
		if err := nativeQuit.Bind(shutdown, desktopApp.Quit); err != nil {
			return err
		}
		if err := updateRuntime.bindShutdown(shutdown); err != nil {
			return err
		}
		if err := updateRuntime.bindInstallGate(nativeQuit); err != nil {
			return err
		}
		desktopCommands, err = newDesktopCommandCoordinator(desktopCommandCoordinatorConfig{
			Window: mainWindow, Emitter: desktopApp.Event, About: desktopApp.Menu,
			Refresh: bindingService, Invalidation: invalidation, Drain: shutdown,
			Quit: desktopApp.Quit, Termination: nativeQuit,
		})
		if err != nil {
			return err
		}
		go func() {
			for {
				select {
				case <-instanceLease.Done():
					return
				case <-instanceLease.Wake():
					_ = desktopCommands.Execute(platformtray.MenuActionOpenOverview)
				}
			}
		}()

		return desktopApp.Run()
	})
}

func runMigrationRecoveryApplication(
	_ context.Context,
	assets fs.FS,
	recovery *migrationRecoveryController,
	instanceLease *singleinstance.Lease,
) error {
	if recovery == nil || instanceLease == nil {
		return ErrApplicationLifecycleRuntime
	}
	recoveryService, err := newMigrationRecoveryService(recovery)
	if err != nil {
		return err
	}
	desktopApp := application.New(applicationOptions(
		assets, wailsStartupService(newStartupService(recovery)), wailsMigrationRecoveryService(recoveryService),
	))
	mainWindow := desktopApp.Window.NewWithOptions(mainWindowOptions())
	mainWindow.RegisterHook(events.Common.WindowClosing, func(event *application.WindowEvent) {
		mainWindow.Hide()
		event.Cancel()
	})
	if err := recovery.bindExit(desktopApp.Quit); err != nil {
		return err
	}
	desktopApp.OnShutdown(func() {
		instanceLease.StopAcceptingWakes()
	})
	go func() {
		for {
			select {
			case <-instanceLease.Done():
				return
			case <-instanceLease.Wake():
				mainWindow.Show()
				mainWindow.Focus()
			}
		}
	}()
	return desktopApp.Run()
}
