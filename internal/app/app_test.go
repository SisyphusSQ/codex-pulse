package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"testing/fstest"

	factstore "github.com/SisyphusSQ/codex-pulse/internal/store"
	storesqlite "github.com/SisyphusSQ/codex-pulse/internal/store/sqlite"
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

// 测试 openConfiguredStore 在新建和重开场景下先完成 Core Schema bootstrap。
func TestOpenConfiguredStoreBootstrapsCoreSchemaAndReopens(t *testing.T) {
	directory := t.TempDir()
	if err := os.Chmod(directory, 0o700); err != nil {
		t.Fatalf("secure temp directory: %v", err)
	}
	config := storesqlite.Config{Path: filepath.Join(directory, "app.db")}

	for attempt := 0; attempt < 2; attempt++ {
		lifecycle, err := openConfiguredStore(context.Background(), config)
		if err != nil {
			t.Fatalf("openConfiguredStore() attempt %d error = %v", attempt+1, err)
		}
		database, ok := lifecycle.(*storesqlite.Store)
		if !ok {
			t.Fatalf("openConfiguredStore() type = %T, want *sqlite.Store", lifecycle)
		}

		var tables int
		err = database.View(context.Background(), func(ctx context.Context, connection storesqlite.ReadConn) error {
			return connection.QueryRowContext(ctx, `
				SELECT COUNT(*) FROM sqlite_schema
				WHERE type = 'table' AND name IN (
					'projects', 'sessions', 'turns', 'session_current',
					'turn_usage', 'session_usage_current'
				)
			`).Scan(&tables)
		})
		if err != nil {
			t.Fatalf("inspect bootstrap schema: %v", err)
		}
		if tables != 6 {
			t.Fatalf("core table count = %d, want 6", tables)
		}
		if err := lifecycle.Close(context.Background()); err != nil {
			t.Fatalf("Close() attempt %d error = %v", attempt+1, err)
		}
	}
}

// 测试 openConfiguredStore 在 Schema Contract 不兼容场景下拒绝启动。
func TestOpenConfiguredStoreRejectsIncompatibleSchema(t *testing.T) {
	directory := t.TempDir()
	if err := os.Chmod(directory, 0o700); err != nil {
		t.Fatalf("secure temp directory: %v", err)
	}
	config := storesqlite.Config{Path: filepath.Join(directory, "incompatible.db")}
	database, err := storesqlite.Open(context.Background(), config)
	if err != nil {
		t.Fatalf("sqlite.Open() error = %v", err)
	}
	err = database.Write(context.Background(), func(ctx context.Context, transaction storesqlite.WriteTx) error {
		_, err := transaction.ExecContext(ctx, `CREATE TABLE sessions (session_id TEXT PRIMARY KEY) STRICT`)
		return err
	})
	if err != nil {
		t.Fatalf("create incompatible schema: %v", err)
	}
	if err := database.Close(context.Background()); err != nil {
		t.Fatalf("close setup database: %v", err)
	}

	lifecycle, err := openConfiguredStore(context.Background(), config)
	if lifecycle != nil {
		t.Fatalf("openConfiguredStore() lifecycle = %T, want nil", lifecycle)
	}
	if !errors.Is(err, factstore.ErrSchemaContract) {
		t.Fatalf("openConfiguredStore() error = %v, want ErrSchemaContract", err)
	}
}

func TestOpenBootstrappedStoreClosesAfterBootstrapFailure(t *testing.T) {
	errBootstrap := errors.New("bootstrap failed")
	errClose := errors.New("close failed")
	var events []string
	store := &fakeLifecycleStore{events: &events, closeErr: errClose}

	got, err := openBootstrappedStore(
		context.Background(),
		func(context.Context) (*fakeLifecycleStore, error) {
			events = append(events, "open")
			return store, nil
		},
		func(context.Context, *fakeLifecycleStore) error {
			events = append(events, "bootstrap")
			return errBootstrap
		},
	)
	if got != nil {
		t.Fatalf("openBootstrappedStore() store = %T, want nil", got)
	}
	if !errors.Is(err, errBootstrap) || !errors.Is(err, errClose) {
		t.Fatalf("openBootstrappedStore() error = %v, want bootstrap and close errors", err)
	}
	if store.closeCalls != 1 {
		t.Fatalf("close calls = %d, want 1", store.closeCalls)
	}
	if want := []string{"open", "bootstrap", "close"}; !reflect.DeepEqual(events, want) {
		t.Fatalf("events = %v, want %v", events, want)
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
