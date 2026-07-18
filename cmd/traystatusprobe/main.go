package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	platformtray "github.com/SisyphusSQ/codex-pulse/internal/platform/tray"
	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
)

func main() {
	output := flag.String("output", "", "directory for bounded synthetic PNG evidence")
	flag.Parse()
	if *output == "" {
		log.Fatal("--output is required")
	}
	if err := os.MkdirAll(*output, 0o755); err != nil {
		log.Fatal(err)
	}
	app := application.New(application.Options{
		Name: "Codex Pulse Tray Status Probe", Assets: application.AlphaAssets,
		Mac: application.MacOptions{ActivationPolicy: application.ActivationPolicyAccessory},
	})
	app.Event.OnApplicationEvent(events.Mac.ApplicationDidFinishLaunching, func(*application.ApplicationEvent) {
		go runProbe(app, *output)
	})
	if err := app.Run(); err != nil {
		log.Fatal(err)
	}
}

func runProbe(app *application.App, output string) {
	item, err := platformtray.NewNativeStatusItem()
	if err != nil {
		log.Print(err)
		app.Quit()
		return
	}
	defer item.Close()
	models := []struct {
		name  string
		model platformtray.StatusViewModel
	}{
		{"secondary-only", platformtray.NewProjector().Project(platformtray.Snapshot{Windows: []platformtray.WindowSnapshot{{
			Kind: platformtray.WindowSecondary, RemainingPercent: percentage(71), Freshness: platformtray.FreshnessFresh,
		}}})},
		{"dual-conflict-blocked", platformtray.NewProjector().Project(platformtray.Snapshot{
			Windows: []platformtray.WindowSnapshot{
				{Kind: platformtray.WindowPrimary, RemainingPercent: percentage(55), Freshness: platformtray.FreshnessFresh, Conflict: true},
				{Kind: platformtray.WindowSecondary, RemainingPercent: percentage(71), Freshness: platformtray.FreshnessFresh},
			}, Health: platformtray.HealthBlocked,
		})},
		{"unavailable-blocked", platformtray.NewProjector().Project(platformtray.Snapshot{
			Health: platformtray.HealthBlocked,
		})},
	}
	for _, sample := range models {
		if err := item.Update(sample.model); err != nil {
			log.Print(err)
			break
		}
		time.Sleep(300 * time.Millisecond)
		path := filepath.Join(output, sample.name+".png")
		if err := item.CapturePNG(path); err != nil {
			log.Print(err)
			break
		}
		fmt.Printf("%s\t%s\n", path, sample.model.AccessibilityLabel)
	}
	app.Quit()
}

func percentage(value float64) *float64 { return &value }
