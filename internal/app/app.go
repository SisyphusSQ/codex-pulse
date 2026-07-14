package app

import (
	"context"
	"errors"
	"fmt"
	"io/fs"

	factstore "github.com/SisyphusSQ/codex-pulse/internal/store"
	storesqlite "github.com/SisyphusSQ/codex-pulse/internal/store/sqlite"
	"github.com/wailsapp/wails/v3/pkg/application"
)

const appDescription = "Local-first Codex usage and quota desktop companion"

type lifecycleStore interface {
	Close(context.Context) error
}

type storeOpener func(context.Context) (lifecycleStore, error)

func applicationOptions(assets fs.FS) application.Options {
	return application.Options{
		Name:        appName,
		Description: appDescription,
		Services: []application.Service{
			application.NewService(NewService()),
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
		BackgroundColour: application.NewRGB(15, 23, 42),
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
			if err := factstore.NewRepository(database).EnsureApplicationSchema(ctx); err != nil {
				return fmt.Errorf("ensure application schema: %w", err)
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

func runWithStore(ctx context.Context, openStore storeOpener, runApplication func() error) (returnErr error) {
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

	return runApplication()
}

// Run composes and starts the desktop application. The call blocks until the
// application exits and returns Wails startup or shutdown failures to main.
func Run(assets fs.FS) error {
	ctx := context.Background()
	return runWithStore(ctx, openApplicationStore, func() error {
		desktopApp := application.New(applicationOptions(assets))
		desktopApp.Window.NewWithOptions(mainWindowOptions())

		return desktopApp.Run()
	})
}
