package app

import (
	"testing"
	"testing/fstest"

	"github.com/wailsapp/wails/v3/pkg/application"
)

func TestApplicationOptions(t *testing.T) {
	t.Parallel()

	assets := fstest.MapFS{
		"index.html": {Data: []byte("<!doctype html><title>Codex Pulse</title>")},
	}
	got := applicationOptions(assets)

	if got.Name != appName {
		t.Fatalf("Name = %q, want %q", got.Name, appName)
	}
	if got.Description == "" {
		t.Fatal("Description must not be empty")
	}
	if len(got.Services) != 1 {
		t.Fatalf("len(Services) = %d, want 1", len(got.Services))
	}
	if got.Assets.Handler == nil {
		t.Fatal("Assets.Handler must be configured")
	}
	if !got.Mac.ApplicationShouldTerminateAfterLastWindowClosed {
		t.Fatal("application must terminate after the last window closes")
	}
}

func TestMainWindowOptions(t *testing.T) {
	t.Parallel()

	got := mainWindowOptions()

	if got.Name != "main" {
		t.Fatalf("Name = %q, want main", got.Name)
	}
	if got.Title != appName {
		t.Fatalf("Title = %q, want %q", got.Title, appName)
	}
	if got.URL != "/" {
		t.Fatalf("URL = %q, want /", got.URL)
	}
	if got.Width < 900 || got.Height < 600 {
		t.Fatalf("window size = %dx%d, want at least 900x600", got.Width, got.Height)
	}
	if got.BackgroundColour != application.NewRGB(15, 23, 42) {
		t.Fatalf("BackgroundColour = %#v", got.BackgroundColour)
	}
}
