package app

import (
	"context"
	"errors"
	"reflect"
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

func TestRunWithStoreOwnsOpenRunCloseLifecycle(t *testing.T) {
	var events []string
	store := &fakeLifecycleStore{events: &events}

	err := runWithStore(
		context.Background(),
		func(context.Context) (lifecycleStore, error) {
			events = append(events, "open")
			return store, nil
		},
		func() error {
			events = append(events, "run")
			return nil
		},
	)
	if err != nil {
		t.Fatalf("runWithStore() error = %v", err)
	}
	if want := []string{"open", "run", "close"}; !reflect.DeepEqual(events, want) {
		t.Fatalf("events = %v, want %v", events, want)
	}
}

func TestRunWithStoreDoesNotRunApplicationWhenOpenFails(t *testing.T) {
	errOpen := errors.New("open failed")
	runCalled := false

	err := runWithStore(
		context.Background(),
		func(context.Context) (lifecycleStore, error) { return nil, errOpen },
		func() error {
			runCalled = true
			return nil
		},
	)
	if !errors.Is(err, errOpen) {
		t.Fatalf("runWithStore() error = %v, want open error", err)
	}
	if runCalled {
		t.Fatal("application runner was called after store open failure")
	}
}

func TestRunWithStorePreservesRunAndCloseFailures(t *testing.T) {
	errRun := errors.New("run failed")
	errClose := errors.New("close failed")
	store := &fakeLifecycleStore{closeErr: errClose}

	err := runWithStore(
		context.Background(),
		func(context.Context) (lifecycleStore, error) { return store, nil },
		func() error { return errRun },
	)
	if !errors.Is(err, errRun) {
		t.Fatalf("runWithStore() error = %v, want run error", err)
	}
	if !errors.Is(err, errClose) {
		t.Fatalf("runWithStore() error = %v, want close error", err)
	}
	if store.closeCalls != 1 {
		t.Fatalf("close calls = %d, want 1", store.closeCalls)
	}
}

type fakeLifecycleStore struct {
	events     *[]string
	closeErr   error
	closeCalls int
}

func (store *fakeLifecycleStore) Close(context.Context) error {
	store.closeCalls++
	if store.events != nil {
		*store.events = append(*store.events, "close")
	}
	return store.closeErr
}
