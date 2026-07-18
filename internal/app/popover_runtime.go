package app

import (
	"errors"
	"sync"

	platformtray "github.com/SisyphusSQ/codex-pulse/internal/platform/tray"
	"github.com/wailsapp/wails/v3/pkg/application"
)

const (
	popoverWidth  = 420
	popoverHeight = 760
	popoverOffset = 6
)

var ErrPopoverRuntime = errors.New("popover runtime is unavailable")

type popoverWindow interface {
	IsVisible() bool
	Hide() application.Window
	Show() application.Window
	Focus()
	SetPosition(int, int)
}

type popoverStatusItem interface {
	SetClickHandler(float64, float64, float64, func(platformtray.PopoverOrigin, bool)) error
}

type popoverController struct {
	mu     sync.Mutex
	window popoverWindow
}

func newPopoverController(window popoverWindow) (*popoverController, error) {
	if window == nil {
		return nil, ErrPopoverRuntime
	}
	return &popoverController{window: window}, nil
}

func (controller *popoverController) ConfigureStatusItem(item *platformtray.NativeStatusItem) error {
	return controller.configureStatusItem(item)
}

func (controller *popoverController) configureStatusItem(item popoverStatusItem) error {
	if controller == nil || item == nil {
		return ErrPopoverRuntime
	}
	return item.SetClickHandler(popoverWidth, popoverHeight, popoverOffset, controller.Toggle)
}

func (controller *popoverController) Toggle(origin platformtray.PopoverOrigin, originValid bool) {
	if controller == nil {
		return
	}
	controller.mu.Lock()
	defer controller.mu.Unlock()
	if controller.window.IsVisible() {
		controller.window.Hide()
		return
	}
	if !originValid {
		return
	}
	controller.window.SetPosition(origin.X, origin.Y)
	controller.window.Show()
	controller.window.Focus()
}

func popoverWindowOptions() application.WebviewWindowOptions {
	return application.WebviewWindowOptions{
		Name: "popover", Title: "Codex Pulse", URL: "/popover",
		Width: popoverWidth, Height: popoverHeight,
		MinWidth: popoverWidth, MaxWidth: popoverWidth,
		MinHeight: popoverHeight, MaxHeight: popoverHeight,
		Hidden: true, Frameless: true, AlwaysOnTop: true, DisableResize: true,
		HideOnEscape: true, HideOnFocusLost: true,
		BackgroundColour: application.NewRGBA(242, 245, 249, 0),
		Mac:              application.MacWindow{Backdrop: application.MacBackdropTranslucent, TitleBar: application.MacTitleBarHidden},
	}
}
