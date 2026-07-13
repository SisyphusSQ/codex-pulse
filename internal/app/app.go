package app

import (
	"io/fs"

	"github.com/wailsapp/wails/v3/pkg/application"
)

const appDescription = "Local-first Codex usage and quota desktop companion"

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

// Run composes and starts the desktop application. The call blocks until the
// application exits and returns Wails startup or shutdown failures to main.
func Run(assets fs.FS) error {
	desktopApp := application.New(applicationOptions(assets))
	desktopApp.Window.NewWithOptions(mainWindowOptions())

	return desktopApp.Run()
}
