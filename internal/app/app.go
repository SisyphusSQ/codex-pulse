package app

import (
	"context"
	"errors"
	"fmt"
	"io/fs"

	"github.com/SisyphusSQ/codex-pulse/internal/metrics"
	"github.com/SisyphusSQ/codex-pulse/internal/pricing"
	factstore "github.com/SisyphusSQ/codex-pulse/internal/store"
	storesqlite "github.com/SisyphusSQ/codex-pulse/internal/store/sqlite"
	"github.com/wailsapp/wails/v3/pkg/application"
)

const appDescription = "Local-first Codex usage and quota desktop companion"

type lifecycleStore interface {
	Close(context.Context) error
}

type storeOpener func(context.Context) (lifecycleStore, error)

func applicationOptions(assets fs.FS, service *Service) application.Options {
	return application.Options{
		Name:        appName,
		Description: appDescription,
		Services: []application.Service{
			wailsBindingService(service),
		},
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
	return runWithStore(ctx, openApplicationStore, func(owned lifecycleStore) (returnErr error) {
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
		desktopApp := application.New(applicationOptions(assets, bindingService))
		desktopApp.Window.NewWithOptions(mainWindowOptions())
		popoverWindow := desktopApp.Window.NewWithOptions(popoverWindowOptions())
		popover, err := newPopoverController(popoverWindow)
		if err != nil {
			return err
		}
		trayHost, err := newTrayRuntimeHost(desktopApp.Event, bindingService, desktopApp.Quit, popover.ConfigureStatusItem)
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

		return desktopApp.Run()
	})
}
