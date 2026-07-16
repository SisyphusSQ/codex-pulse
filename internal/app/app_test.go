package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"testing/fstest"
	"time"

	"github.com/SisyphusSQ/codex-pulse/internal/codex/logs"
	"github.com/SisyphusSQ/codex-pulse/internal/liveindex"
	"github.com/SisyphusSQ/codex-pulse/internal/preferences"
	"github.com/SisyphusSQ/codex-pulse/internal/pricing"
	factstore "github.com/SisyphusSQ/codex-pulse/internal/store"
	storesqlite "github.com/SisyphusSQ/codex-pulse/internal/store/sqlite"
	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
)

func TestApplicationOptions(t *testing.T) {
	t.Parallel()

	assets := fstest.MapFS{
		"index.html": {Data: []byte("<!doctype html><title>Codex Pulse</title>")},
	}
	service := newBindingTestService(t, &usageCostBindingStub{}, &runtimeInfoBindingStub{})
	got := applicationOptions(assets, service)

	if got.Name != appName {
		t.Fatalf("Name = %q, want %q", got.Name, appName)
	}
	if got.Description == "" {
		t.Fatal("Description must not be empty")
	}
	if len(got.Services) != 1 {
		t.Fatalf("len(Services) = %d, want 1", len(got.Services))
	}
	if got.Services[0].Instance() != service {
		t.Fatalf("registered service = %T, want binding facade", got.Services[0].Instance())
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
	if got.Width != 1120 || got.Height != 720 || got.MinWidth != 900 || got.MinHeight != 600 {
		t.Fatalf(
			"window size = %dx%d min %dx%d, want 1120x720 min 900x600",
			got.Width, got.Height, got.MinWidth, got.MinHeight,
		)
	}
	if got.BackgroundColour != application.NewRGB(242, 245, 249) {
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
		func(got lifecycleStore) error {
			if got != store {
				t.Fatalf("run store = %T, want owned store", got)
			}
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
		func(lifecycleStore) error {
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
		func(lifecycleStore) error { return errRun },
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

func TestApplicationLifecycleRuntimeComposesConfiguredHomeAndReleasesWorker(t *testing.T) {
	ctx := context.Background()
	home := t.TempDir()
	for _, directory := range []string{"sessions", "archived_sessions"} {
		if err := os.MkdirAll(filepath.Join(home, directory), 0o700); err != nil {
			t.Fatalf("create %s: %v", directory, err)
		}
	}
	rolloutPath := filepath.Join(home, "sessions", "runtime-composition.jsonl")
	rollout := []byte(
		`{"timestamp":"2026-07-14T01:00:00Z","type":"session_meta","payload":{"id":"session-app-runtime","timestamp":"2026-07-14T01:00:00Z","cwd":"/tmp/project","originator":"codex_cli_rs","cli_version":"0.142.3","source":"cli","model_provider":"openai"}}` + "\n" +
			`{"timestamp":"2026-07-14T01:00:01Z","type":"event_msg","payload":{"type":"task_started","turn_id":"turn-app-runtime","started_at":1783990801,"model_context_window":258000}}` + "\n" +
			`{"timestamp":"2026-07-14T01:00:02Z","type":"event_msg","payload":{"type":"task_complete","turn_id":"turn-app-runtime","completed_at":1783990802}}` + "\n",
	)
	if err := os.WriteFile(rolloutPath, rollout, 0o600); err != nil {
		t.Fatalf("write rollout: %v", err)
	}
	metadata, err := logs.NewHomeProbe().Probe(ctx, home)
	if err != nil {
		t.Fatalf("Probe(home) error = %v", err)
	}
	preferenceStore, err := preferences.NewFileStore(filepath.Join(t.TempDir(), "private", "preferences.json"))
	if err != nil {
		t.Fatalf("NewFileStore() error = %v", err)
	}
	if err := preferenceStore.Confirm(ctx, preferences.OnboardingSnapshot{
		SchemaVersion: preferences.CurrentSchemaVersion, OnboardingVersion: preferences.CurrentOnboardingVersion,
		OnboardingCompleted: true,
		CodexHome: preferences.ConfirmedSource{
			Path: metadata.Path, DeviceID: metadata.DeviceID, Inode: metadata.Inode,
			ConfirmedAtMS: time.Now().UnixMilli(),
		},
	}); err != nil {
		t.Fatalf("Confirm() error = %v", err)
	}
	databaseDirectory := t.TempDir()
	if err := os.Chmod(databaseDirectory, 0o700); err != nil {
		t.Fatalf("secure database directory: %v", err)
	}
	databasePath := filepath.Join(databaseDirectory, "app.db")
	lifecycleStore, err := openConfiguredStore(ctx, storesqlite.Config{Path: databasePath})
	if err != nil {
		t.Fatalf("openConfiguredStore() error = %v", err)
	}
	database := lifecycleStore.(*storesqlite.Store)
	t.Cleanup(func() { _ = database.Close(context.Background()) })
	registrar := &fakeLifecycleRegistrar{callbacks: make(map[events.ApplicationEventType]func(*application.ApplicationEvent))}
	runtime, err := startApplicationLifecycleRuntime(ctx, ApplicationLifecycleRuntimeConfig{
		Database: database, Registrar: registrar, Preferences: preferenceStore,
		EventTimeout: time.Second,
	})
	if err != nil {
		t.Fatalf("startApplicationLifecycleRuntime() error = %v", err)
	}
	if runtime == nil {
		t.Fatal("configured Home must compose application lifecycle runtime")
	}
	repository := factstore.NewRepository(database)
	state, err := repository.SchedulerLifecycle(ctx)
	if err != nil || state.HomeGeneration != 1 || state.SystemState != factstore.LifecycleSystemAwake ||
		state.SourceState != factstore.LifecycleSourceAvailable ||
		state.Transition != factstore.LifecycleTransitionSteady {
		t.Fatalf("SchedulerLifecycle() = %#v, %v", state, err)
	}
	waitForAppCondition(t, func() bool {
		lease, leaseErr := repository.TryAcquireSchedulerOwner(ctx)
		if leaseErr == nil {
			lease.Release()
		}
		return errors.Is(leaseErr, factstore.ErrSchedulerOwnerBusy)
	}, "scheduler worker did not acquire owner lease")
	waitForAppCondition(t, func() bool {
		tasks, listErr := repository.ListSchedulerTasks(ctx, factstore.SchedulerTaskFilter{Limit: 10})
		return listErr == nil && len(tasks) == 1 && tasks[0].TargetKind == factstore.SchedulerTargetLiveScan &&
			tasks[0].State == factstore.SchedulerTaskSucceeded
	}, "startup reconcile live task did not complete")
	initialRevision := state.Revision
	registrar.trigger(events.Mac.ApplicationDidBecomeActive)
	waitForAppCondition(t, func() bool {
		current, readErr := repository.SchedulerLifecycle(ctx)
		return readErr == nil && current.Revision > initialRevision &&
			current.SourceState == factstore.LifecycleSourceAvailable
	}, "foreground source check did not reconcile")
	if err := runtime.Close(context.Background()); err != nil {
		t.Fatalf("runtime.Close() error = %v", err)
	}
	if registrar.cancelCalls != 3 {
		t.Fatalf("cancel calls = %d, want 3", registrar.cancelCalls)
	}
	lease, err := repository.TryAcquireSchedulerOwner(ctx)
	if err != nil {
		t.Fatalf("TryAcquireSchedulerOwner(after close) error = %v", err)
	}
	lease.Release()
}

func TestApplicationLifecycleRuntimeValidatesPhysicalHomeBeforeRecoveringTargets(t *testing.T) {
	ctx := context.Background()
	home := t.TempDir()
	for _, directory := range []string{"sessions", "archived_sessions"} {
		if err := os.MkdirAll(filepath.Join(home, directory), 0o700); err != nil {
			t.Fatalf("create %s: %v", directory, err)
		}
	}
	rolloutPath := filepath.Join(home, "sessions", "startup-recovery.jsonl")
	rollout := []byte(
		`{"timestamp":"2026-07-14T01:00:00Z","type":"session_meta","payload":{"id":"session-startup-recovery","timestamp":"2026-07-14T01:00:00Z","cwd":"/tmp/project","originator":"codex_cli_rs","cli_version":"0.142.3","source":"cli","model_provider":"openai"}}` + "\n",
	)
	if err := os.WriteFile(rolloutPath, rollout, 0o600); err != nil {
		t.Fatalf("write rollout: %v", err)
	}
	metadata, err := logs.NewHomeProbe().Probe(ctx, home)
	if err != nil {
		t.Fatalf("Probe(home) error = %v", err)
	}
	preferenceStore, err := preferences.NewFileStore(filepath.Join(t.TempDir(), "private", "preferences.json"))
	if err != nil {
		t.Fatalf("NewFileStore() error = %v", err)
	}
	if err := preferenceStore.Confirm(ctx, preferences.OnboardingSnapshot{
		SchemaVersion: preferences.CurrentSchemaVersion, OnboardingVersion: preferences.CurrentOnboardingVersion,
		OnboardingCompleted: true,
		CodexHome: preferences.ConfirmedSource{
			Path: metadata.Path, DeviceID: metadata.DeviceID, Inode: metadata.Inode,
			ConfirmedAtMS: time.Now().UnixMilli(),
		},
	}); err != nil {
		t.Fatalf("Confirm() error = %v", err)
	}
	databaseDirectory := t.TempDir()
	if err := os.Chmod(databaseDirectory, 0o700); err != nil {
		t.Fatalf("secure database directory: %v", err)
	}
	lifecycleStore, err := openConfiguredStore(ctx, storesqlite.Config{
		Path: filepath.Join(databaseDirectory, "startup-recovery.db"),
	})
	if err != nil {
		t.Fatalf("openConfiguredStore() error = %v", err)
	}
	database := lifecycleStore.(*storesqlite.Store)
	t.Cleanup(func() { _ = database.Close(context.Background()) })
	repository := factstore.NewRepository(database)
	discoverer, err := logs.NewConfirmedDiscoverer(metadata.Path, metadata.DeviceID, metadata.Inode)
	if err != nil {
		t.Fatalf("NewConfirmedDiscoverer() error = %v", err)
	}
	discovery, err := discoverer.Discover(ctx)
	if err != nil || len(discovery.Snapshots) != 1 {
		t.Fatalf("Discover() = %#v, %v", discovery, err)
	}
	plan, err := logs.PlanReconcile(metadata.Path, nil, discovery)
	if err != nil || len(plan.Actions) != 1 {
		t.Fatalf("PlanReconcile() = %#v, %v", plan, err)
	}
	liveRuntime, err := liveindex.New(liveindex.Config{Repository: repository})
	if err != nil {
		t.Fatalf("liveindex.New() error = %v", err)
	}
	requestedAtMS := time.Now().UnixMilli()
	job, err := liveRuntime.Start(ctx, liveindex.LiveRequest{
		RequestID: "startup-recovery-before-home-validation", HomeGeneration: 1,
		HomePath: metadata.Path, HomeDeviceID: metadata.DeviceID, HomeInode: metadata.Inode,
		Action: plan.Actions[0], RequestedAtMS: requestedAtMS,
	})
	if err != nil {
		t.Fatalf("liveRuntime.Start() error = %v", err)
	}
	task := factstore.SchedulerTask{
		TaskID: "task-startup-recovery", DedupeKey: "live:startup-recovery",
		TargetKind: factstore.SchedulerTargetLiveScan, TargetID: job.JobID,
		HomeGeneration: 1, Lane: factstore.SchedulerLaneLive,
		ServiceClass: factstore.SchedulerServiceBackground, State: factstore.SchedulerTaskQueued,
		QueueOrderMS: requestedAtMS, EnqueuedAtMS: requestedAtMS, UpdatedAtMS: requestedAtMS,
	}
	if err := repository.EnqueueSchedulerTask(ctx, task, 8); err != nil {
		t.Fatalf("EnqueueSchedulerTask() error = %v", err)
	}
	if _, err := repository.ClaimSchedulerTask(ctx, task.TaskID, requestedAtMS+1); err != nil {
		t.Fatalf("ClaimSchedulerTask() error = %v", err)
	}

	staleHome := home + "-stale"
	if err := os.Rename(home, staleHome); err != nil {
		t.Fatalf("rename confirmed Home: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(staleHome) })
	for _, directory := range []string{"sessions", "archived_sessions"} {
		if err := os.MkdirAll(filepath.Join(home, directory), 0o700); err != nil {
			t.Fatalf("create replacement %s: %v", directory, err)
		}
	}
	registrar := &fakeLifecycleRegistrar{callbacks: make(map[events.ApplicationEventType]func(*application.ApplicationEvent))}
	runtime, err := startApplicationLifecycleRuntime(ctx, ApplicationLifecycleRuntimeConfig{
		Database: database, Registrar: registrar, Preferences: preferenceStore,
		EventTimeout: time.Second,
	})
	if err != nil {
		t.Fatalf("startApplicationLifecycleRuntime() error = %v", err)
	}
	if runtime == nil {
		t.Fatal("configured Home must compose application lifecycle runtime")
	}
	defer func() {
		if err := runtime.Close(context.Background()); err != nil {
			t.Errorf("runtime.Close() error = %v", err)
		}
	}()
	state, err := repository.SchedulerLifecycle(ctx)
	if err != nil || state.SourceState != factstore.LifecycleSourceUnavailable ||
		state.Transition != factstore.LifecycleTransitionBlocked {
		t.Fatalf("SchedulerLifecycle() = %#v, %v", state, err)
	}
	storedTask, err := repository.SchedulerTask(ctx, task.TaskID)
	if err != nil || storedTask.State != factstore.SchedulerTaskRunning || storedTask.TargetID != job.JobID {
		t.Fatalf("SchedulerTask() = %#v, %v; startup must not recover before Home validation", storedTask, err)
	}
	storedJob, _, err := repository.LiveScanRun(ctx, job.JobID)
	if err != nil || storedJob.State != factstore.JobQueued {
		t.Fatalf("LiveScanRun() = %#v, %v; startup must not interrupt target before Home validation", storedJob, err)
	}
}

func waitForAppCondition(t *testing.T, condition func() bool, message string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for !condition() {
		if time.Now().After(deadline) {
			t.Fatal(message)
		}
		time.Sleep(time.Millisecond)
	}
}

// 测试 openConfiguredStore 在新建和重开场景下先完成 Application Schema bootstrap。
func TestOpenConfiguredStoreBootstrapsApplicationSchemaAndReopens(t *testing.T) {
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
			return connection.WithContext(ctx).Raw(`
				SELECT COUNT(*) FROM sqlite_schema
				WHERE type = 'table' AND name IN (
					'projects', 'sessions', 'turns', 'session_current',
					'turn_usage', 'session_usage_current',
					'source_files', 'source_state', 'source_attempts',
					'job_runs', 'health_events', 'pricing_versions', 'model_prices',
					'pricing_catalog_metadata', 'cost_rollup_generations', 'turn_costs',
					'session_usage_rollups', 'usage_daily',
					'project_usage_daily', 'model_usage_daily',
					'schema_migrations'
				)
			`).Row().Scan(&tables)
		})
		if err != nil {
			t.Fatalf("inspect bootstrap schema: %v", err)
		}
		if tables != 21 {
			t.Fatalf("application table count = %d, want 21", tables)
		}
		builtin := pricing.BuiltinOpenAI20260714()
		stored, err := factstore.NewRepository(database).PricingVersion(
			context.Background(), builtin.PricingVersion,
		)
		if err != nil {
			t.Fatalf("PricingVersion(builtin) attempt %d error = %v", attempt+1, err)
		}
		if stored.SourceURL != builtin.SourceURL || stored.VerifiedAtMS != builtin.VerifiedAtMS ||
			len(stored.Models) != len(builtin.Models) {
			t.Fatalf("builtin pricing catalog attempt %d = %#v, want %#v", attempt+1, stored, builtin)
		}
		if err := lifecycle.Close(context.Background()); err != nil {
			t.Fatalf("Close() attempt %d error = %v", attempt+1, err)
		}
	}
}

func TestOpenConfiguredStoreRejectsConflictingBuiltinCatalogAndClosesStore(t *testing.T) {
	directory := t.TempDir()
	if err := os.Chmod(directory, 0o700); err != nil {
		t.Fatalf("secure temp directory: %v", err)
	}
	config := storesqlite.Config{Path: filepath.Join(directory, "catalog-conflict.db")}
	database, err := storesqlite.Open(context.Background(), config)
	if err != nil {
		t.Fatalf("sqlite.Open() error = %v", err)
	}
	repository := factstore.NewRepository(database)
	if err := repository.EnsureApplicationSchema(context.Background()); err != nil {
		t.Fatalf("EnsureApplicationSchema() error = %v", err)
	}
	legacy := pricing.BuiltinOpenAI20260714()
	legacy.SourceURL = ""
	legacy.VerifiedAtMS = 0
	if err := repository.AddPricingVersion(context.Background(), legacy); err != nil {
		t.Fatalf("AddPricingVersion(legacy builtin) error = %v", err)
	}
	if err := database.Close(context.Background()); err != nil {
		t.Fatalf("Close(setup) error = %v", err)
	}

	lifecycle, err := openConfiguredStore(context.Background(), config)
	if lifecycle != nil {
		t.Fatalf("openConfiguredStore() lifecycle = %T, want nil", lifecycle)
	}
	if !errors.Is(err, factstore.ErrInvalidRecord) {
		t.Fatalf("openConfiguredStore() error = %v, want ErrInvalidRecord", err)
	}
	reopened, err := storesqlite.Open(context.Background(), config)
	if err != nil {
		t.Fatalf("sqlite.Open(after rejected bootstrap) error = %v", err)
	}
	if err := reopened.Close(context.Background()); err != nil {
		t.Fatalf("Close(after rejected bootstrap) error = %v", err)
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
		return transaction.WithContext(ctx).
			Exec(`CREATE TABLE sessions (session_id TEXT PRIMARY KEY) STRICT`).Error
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

// 测试 openConfiguredStore 在 core 已存在但 runtime contract 不兼容时拒绝启动。
func TestOpenConfiguredStoreRejectsIncompatibleRuntimeSchema(t *testing.T) {
	directory := t.TempDir()
	if err := os.Chmod(directory, 0o700); err != nil {
		t.Fatalf("secure temp directory: %v", err)
	}
	config := storesqlite.Config{Path: filepath.Join(directory, "incompatible-runtime.db")}
	database, err := storesqlite.Open(context.Background(), config)
	if err != nil {
		t.Fatalf("sqlite.Open() error = %v", err)
	}
	if err := factstore.NewRepository(database).EnsureCoreSchema(context.Background()); err != nil {
		t.Fatalf("EnsureCoreSchema() error = %v", err)
	}
	err = database.Write(context.Background(), func(ctx context.Context, transaction storesqlite.WriteTx) error {
		return transaction.WithContext(ctx).
			Exec(`CREATE TABLE source_files (source_file_id TEXT PRIMARY KEY) STRICT`).Error
	})
	if err != nil {
		t.Fatalf("create incompatible runtime schema: %v", err)
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
