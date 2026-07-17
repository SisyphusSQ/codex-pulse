package app

import (
	"context"
	"errors"
	"fmt"
	"io/fs"

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
			ApplicationShouldTerminateAfterLastWindowClosed: true,
		},
	}
}

func mainWindowOptions() application.WebviewWindowOptions {
	return application.WebviewWindowOptions{
		Name:      "main",
		Title:     appName,
		Width:     1120,
		Height:    720,
		MinWidth:  900,
		MinHeight: 600,
		URL:       "/",
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
		preferenceStore, err := openApplicationPreferences()
		if err != nil {
			return err
		}
		bindingService, err := composeBindingService(database, preferenceStore)
		if err != nil {
			return err
		}
		desktopApp := application.New(applicationOptions(assets, bindingService))
		desktopApp.Window.NewWithOptions(mainWindowOptions())
		invalidation, err := newQueryInvalidationPublisher(QueryInvalidationPublisherConfig{
			Emitter: desktopApp.Event,
			Health:  factstore.NewRepository(database),
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

		return desktopApp.Run()
	})
}
