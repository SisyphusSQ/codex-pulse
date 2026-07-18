package main

import (
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"runtime"
	"time"

	platformtray "github.com/SisyphusSQ/codex-pulse/internal/platform/tray"
	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
)

type config struct {
	iconPath     string
	evidencePath string
	duration     time.Duration
}

func main() {
	configuration := config{}
	flag.StringVar(&configuration.iconPath, "icon", "", "path to a validated template PNG")
	flag.StringVar(&configuration.evidencePath, "evidence", "", "path for private JSON evidence")
	flag.DurationVar(&configuration.duration, "duration", 8*time.Second, "bounded live probe duration")
	flag.Parse()
	if err := run(configuration); err != nil {
		log.Fatal(err)
	}
}

func run(configuration config) error {
	if runtime.GOOS != "darwin" || runtime.GOARCH != "arm64" {
		return fmt.Errorf("tray probe requires darwin/arm64, got %s/%s", runtime.GOOS, runtime.GOARCH)
	}
	if configuration.iconPath == "" || configuration.evidencePath == "" {
		return errors.New("tray probe requires --icon and --evidence")
	}
	if configuration.duration < 3*time.Second || configuration.duration > 2*time.Minute {
		return errors.New("tray probe duration must be between 3s and 2m")
	}
	icon, err := os.ReadFile(configuration.iconPath)
	if err != nil {
		return fmt.Errorf("read template icon: %w", err)
	}
	if len(icon) == 0 {
		return errors.New("template icon is empty")
	}
	recorder, err := platformtray.NewRecorder(runtime.GOOS, runtime.GOARCH, time.Now)
	if err != nil {
		return err
	}
	writeEvidence := func(detail string) {
		recorder.Observe("app_shutdown", detail)
		if err := recorder.Write(configuration.evidencePath); err != nil {
			log.Printf("write shutdown evidence: %v", err)
		}
	}
	app := application.New(application.Options{
		Name:        "Codex Pulse Tray Capability Probe",
		Description: "Bounded Wails tray and attached-window capability probe",
		Assets:      application.AlphaAssets,
		Mac:         application.MacOptions{ActivationPolicy: application.ActivationPolicyAccessory},
		OnShutdown:  func() { writeEvidence("Wails OnShutdown") },
	})
	window := app.Window.NewWithOptions(application.WebviewWindowOptions{
		Name: "tray-capability-probe", Width: 360, Height: 240,
		Frameless: true, AlwaysOnTop: true, Hidden: true, DisableResize: true,
		HideOnEscape: true, HideOnFocusLost: true,
	})
	window.RegisterHook(events.Common.WindowShow, func(*application.WindowEvent) { recorder.Observe("window_show", "native event") })
	window.RegisterHook(events.Common.WindowHide, func(*application.WindowEvent) { recorder.Observe("window_hide", "native event") })
	window.RegisterHook(events.Common.WindowFocus, func(*application.WindowEvent) { recorder.Observe("window_focus", "native event") })
	window.RegisterHook(events.Common.WindowLostFocus, func(*application.WindowEvent) { recorder.Observe("window_lost_focus", "native event") })
	window.RegisterHook(events.Common.WindowClosing, func(event *application.WindowEvent) { window.Hide(); event.Cancel() })

	tray := app.SystemTray.New()
	tray.SetTemplateIcon(icon)
	recorder.Observe("template_icon_configured", fmt.Sprintf("bytes=%d", len(icon)))
	menu := app.NewMenu()
	menu.Add("Probe action").OnClick(func(*application.Context) { recorder.Observe("menu_action", "native callback") })
	menu.Add("Quit probe").OnClick(func(*application.Context) { recorder.Observe("menu_quit", "native callback"); app.Quit() })
	tray.SetMenu(menu)
	tray.AttachWindow(window).WindowOffset(5)
	recorder.Observe("attached_window_configured", "offset=5")
	tray.OnClick(func() { recorder.Observe("left_click", "native callback"); tray.ToggleWindow() })
	tray.OnRightClick(func() { recorder.Observe("right_click", "native callback"); tray.OpenMenu() })

	go func() {
		time.Sleep(750 * time.Millisecond)
		recorder.Observe("app_started", "ActivationPolicyAccessory")
		screens := app.Screen.GetAll()
		primary := app.Screen.GetPrimary()
		recorder.Observe("screen_inventory", fmt.Sprintf("count=%d primary_available=%t", len(screens), primary != nil))
		if err := tray.PositionWindow(window, 5); err != nil {
			recorder.Observe("position_window_call_failed", err.Error())
		} else {
			recorder.Observe("position_window_call_succeeded", "native call returned without error; geometry requires readback")
		}
		tray.ShowWindow()
		time.Sleep(500 * time.Millisecond)
		windowX, windowY := window.Position()
		windowWidth, windowHeight := window.Size()
		windowScreen, screenErr := window.GetScreen()
		if screenErr != nil || windowScreen == nil {
			detail := "screen unavailable"
			if screenErr != nil {
				detail = screenErr.Error()
			}
			recorder.Observe("window_geometry_failed", detail)
		} else {
			recorder.SetWindowGeometry(platformtray.WindowGeometry{
				Window:      platformtray.Rect{X: windowX, Y: windowY, Width: windowWidth, Height: windowHeight},
				Screen:      platformtray.Rect{X: windowScreen.Bounds.X, Y: windowScreen.Bounds.Y, Width: windowScreen.Bounds.Width, Height: windowScreen.Bounds.Height},
				ScreenCount: len(screens), Primary: windowScreen.IsPrimary,
			})
			recorder.Observe("window_geometry_readback", fmt.Sprintf("x=%d y=%d width=%d height=%d screen_primary=%t", windowX, windowY, windowWidth, windowHeight, windowScreen.IsPrimary))
		}
		recorder.Observe("window_visible_readback", fmt.Sprintf("visible=%t focused=%t", window.IsVisible(), window.IsFocused()))
		tray.HideWindow()
		time.Sleep(250 * time.Millisecond)
		recorder.Observe("window_hidden_readback", fmt.Sprintf("visible=%t", window.IsVisible()))
		time.Sleep(configuration.duration - 1500*time.Millisecond)
		app.Quit()
	}()
	if err := app.Run(); err != nil {
		return fmt.Errorf("run Wails tray probe: %w", err)
	}
	writeEvidence("App.Run returned")
	return nil
}
