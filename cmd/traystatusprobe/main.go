package main

import (
	"encoding/json"
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
	readyFile := flag.String("accessibility-ready-file", "", "optional path receiving the probe PID before status updates")
	continueFile := flag.String("accessibility-continue-file", "", "optional path that releases status updates after AX observer setup")
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
		go runProbe(app, *output, *readyFile, *continueFile)
	})
	if err := app.Run(); err != nil {
		log.Fatal(err)
	}
}

func runProbe(app *application.App, output, readyFile, continueFile string) {
	item, err := platformtray.NewNativeStatusItem()
	if err != nil {
		log.Print(err)
		app.Quit()
		return
	}
	defer item.Close()
	if readyFile != "" || continueFile != "" {
		if readyFile == "" || continueFile == "" {
			log.Print("both accessibility probe signal paths are required")
			app.Quit()
			return
		}
		if err := os.WriteFile(readyFile, []byte(fmt.Sprintf("%d\n", os.Getpid())), 0o600); err != nil {
			log.Print(err)
			app.Quit()
			return
		}
		deadline := time.Now().Add(10 * time.Second)
		for {
			if _, err := os.Stat(continueFile); err == nil {
				break
			} else if !os.IsNotExist(err) || time.Now().After(deadline) {
				log.Print("accessibility observer did not become ready")
				app.Quit()
				return
			}
			time.Sleep(50 * time.Millisecond)
		}
	}
	platformEvents := make(chan platformtray.PlatformChange, 16)
	if err := item.SetPlatformChangeHandler(func(change platformtray.PlatformChange) {
		select {
		case platformEvents <- change:
		default:
		}
	}); err != nil {
		log.Print(err)
		app.Quit()
		return
	}
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
	postPlatformProbeEvents()
	observed := make(map[platformtray.PlatformChange]bool, 4)
	timeout := time.NewTimer(3 * time.Second)
dequeue:
	for len(observed) < 4 {
		select {
		case change := <-platformEvents:
			observed[change] = true
		case <-timeout.C:
			break dequeue
		}
	}
	if !timeout.Stop() {
		select {
		case <-timeout.C:
		default:
		}
	}
	ordered := []platformtray.PlatformChange{
		platformtray.PlatformChangeDisplay,
		platformtray.PlatformChangeSpace,
		platformtray.PlatformChangeWake,
		platformtray.PlatformChangeAppearance,
	}
	for _, change := range ordered {
		if !observed[change] {
			log.Printf("platform event %s was not observed", change)
			app.Quit()
			return
		}
	}
	report, err := json.MarshalIndent(struct {
		Observed []platformtray.PlatformChange `json:"observed"`
	}{Observed: ordered}, "", "  ")
	if err != nil || os.WriteFile(filepath.Join(output, "platform-events.json"), append(report, '\n'), 0o600) != nil {
		log.Print("write platform event evidence failed")
		app.Quit()
		return
	}
	app.Quit()
}

func percentage(value float64) *float64 { return &value }
