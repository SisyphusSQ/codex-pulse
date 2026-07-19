package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/SisyphusSQ/codex-pulse/internal/bootstrap"
	"github.com/SisyphusSQ/codex-pulse/internal/codex/logs"
	"github.com/SisyphusSQ/codex-pulse/internal/metrics"
	"github.com/SisyphusSQ/codex-pulse/internal/preferences"
	factstore "github.com/SisyphusSQ/codex-pulse/internal/store"
	storesqlite "github.com/SisyphusSQ/codex-pulse/internal/store/sqlite"
)

const (
	contractVersion    = "m11-tray-idle-v1"
	defaultWarmup      = 10 * time.Second
	defaultInterval    = time.Second
	defaultSamples     = 20
	maximumIdleCPU     = 5.0
	maximumIdleRSS     = int64(512 << 20)
	maximumIdleWAL     = int64(256 << 20)
	isolatedRootPrefix = "codex-pulse-m11-idle-"
)

type config struct {
	app      string
	warmup   time.Duration
	interval time.Duration
	samples  int
}

type summary struct {
	Version           string  `json:"version"`
	Result            string  `json:"result"`
	Samples           int     `json:"samples"`
	AverageCPUPercent float64 `json:"averageCpuPercent"`
	PeakRSSBytes      int64   `json:"peakRssBytes"`
	DatabaseBytes     int64   `json:"databaseBytes"`
	WALBytes          int64   `json:"walBytes"`
	BootstrapComplete bool    `json:"bootstrapComplete"`
	ActiveTasks       int     `json:"activeTasks"`
	IsolatedCleanup   bool    `json:"isolatedCleanup"`
}

type idleStateError struct {
	jobState   factstore.JobState
	activeTask int
}

func (err *idleStateError) Error() string { return "durable idle state not reached" }

var (
	errIdleSetup     = errors.New("tray idle setup failed")
	errIdleLaunch    = errors.New("tray idle launch failed")
	errIdleWarmup    = errors.New("tray idle warmup failed")
	errIdleState     = errors.New("tray idle state verification failed")
	errIdleProbe     = errors.New("tray idle probe failed")
	errIdleThreshold = errors.New("tray idle threshold failed")
)

func main() {
	input := config{}
	flag.StringVar(&input.app, "app", "", "absolute path to packaged app executable")
	flag.DurationVar(&input.warmup, "warmup", defaultWarmup, "settling window before sampling")
	flag.DurationVar(&input.interval, "interval", defaultInterval, "sampling interval")
	flag.IntVar(&input.samples, "samples", defaultSamples, "number of idle samples")
	flag.Parse()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	result, err := execute(ctx, input)
	if err != nil {
		fmt.Fprintln(os.Stderr, stableFailure(err))
		os.Exit(1)
	}
	if err := json.NewEncoder(os.Stdout).Encode(result); err != nil {
		fmt.Fprintln(os.Stderr, "M11-IDLE-002: encode content-free summary")
		os.Exit(1)
	}
}

func execute(ctx context.Context, input config) (result summary, returnErr error) {
	if err := validateConfig(input); err != nil {
		return summary{}, err
	}
	root, err := os.MkdirTemp("", isolatedRootPrefix)
	if err != nil {
		return summary{}, err
	}
	if err := os.Chmod(root, 0o700); err != nil {
		_ = os.RemoveAll(root)
		return summary{}, err
	}
	marker := filepath.Join(root, ".owned")
	if err := os.WriteFile(marker, []byte(contractVersion), 0o600); err != nil {
		_ = os.RemoveAll(root)
		return summary{}, err
	}
	defer func() {
		cleanupErr := removeIsolatedRoot(root)
		result.IsolatedCleanup = cleanupErr == nil
		returnErr = errors.Join(returnErr, cleanupErr)
	}()

	userHome := filepath.Join(root, "home")
	if err := configureSyntheticHome(ctx, userHome); err != nil {
		return summary{}, errors.Join(errIdleSetup, err)
	}
	command := exec.Command(input.app)
	command.Env = replaceEnvironment(os.Environ(), "HOME", userHome)
	command.Stdout = nil
	command.Stderr = nil
	if err := command.Start(); err != nil {
		return summary{}, errors.Join(errIdleLaunch, err)
	}
	processExited := make(chan error, 1)
	go func() {
		processExited <- command.Wait()
		close(processExited)
	}()
	defer stopProcess(command, processExited)

	select {
	case <-ctx.Done():
		return summary{}, ctx.Err()
	case err := <-processExited:
		return summary{}, errors.Join(errIdleWarmup, err)
	case <-time.After(input.warmup):
	}
	databasePath := filepath.Join(userHome, "Library", "Application Support", "Codex Pulse", "codex-pulse.db")
	if err := waitForIdleState(ctx, databasePath, 20*time.Second); err != nil {
		return summary{}, errors.Join(errIdleState, err)
	}
	probe, err := metrics.NewGopsutilProcessProbe(command.Process.Pid)
	if err != nil {
		return summary{}, errors.Join(errIdleProbe, err)
	}
	initial, err := probe.Measure(ctx)
	if err != nil {
		return summary{}, errors.Join(errIdleProbe, err)
	}
	started := time.Now()
	latest := initial
	peakRSS := initial.RSSBytes
	for sample := 0; sample < input.samples; sample++ {
		select {
		case <-ctx.Done():
			return summary{}, ctx.Err()
		case err := <-processExited:
			return summary{}, fmt.Errorf("app exited during sampling: %w", err)
		case <-time.After(input.interval):
		}
		measurement, err := probe.Measure(ctx)
		if err != nil {
			return summary{}, errors.Join(errIdleProbe, err)
		}
		peakRSS = max(peakRSS, measurement.RSSBytes)
		latest = measurement
	}
	elapsedMS := time.Since(started).Milliseconds()
	if elapsedMS <= 0 {
		return summary{}, errors.New("invalid idle sample window")
	}
	cpuMS := latest.CPUUserMS + latest.CPUSystemMS - initial.CPUUserMS - initial.CPUSystemMS
	if cpuMS < 0 {
		return summary{}, errors.New("process CPU counter regressed")
	}
	result = summary{
		Version: contractVersion, Result: "passed", Samples: input.samples,
		AverageCPUPercent: float64(cpuMS) * 100 / float64(elapsedMS), PeakRSSBytes: peakRSS,
		DatabaseBytes: fileSize(databasePath), WALBytes: fileSize(databasePath + "-wal"),
		BootstrapComplete: true, ActiveTasks: 0,
	}
	if result.AverageCPUPercent >= maximumIdleCPU || result.PeakRSSBytes <= 0 ||
		result.PeakRSSBytes > maximumIdleRSS || result.DatabaseBytes <= 0 || result.WALBytes > maximumIdleWAL {
		return result, errIdleThreshold
	}
	return result, nil
}

func replaceEnvironment(environment []string, key, value string) []string {
	prefix := key + "="
	result := make([]string, 0, len(environment)+1)
	for _, item := range environment {
		if !strings.HasPrefix(item, prefix) {
			result = append(result, item)
		}
	}
	return append(result, prefix+value)
}

func verifyIdleState(ctx context.Context, databasePath string) error {
	database, err := storesqlite.Open(ctx, storesqlite.Config{Path: databasePath})
	if err != nil {
		return err
	}
	defer database.Close(context.Background())
	repository := factstore.NewRepository(database)
	job, _, err := repository.LatestBootstrapRunByGeneration(ctx, 1)
	if err != nil {
		if errors.Is(err, factstore.ErrNotFound) {
			return &idleStateError{jobState: factstore.JobState("missing")}
		}
		return err
	}
	if job.State != factstore.JobSucceeded {
		return &idleStateError{jobState: job.State}
	}
	active := true
	tasks, err := repository.ListSchedulerTasks(ctx, factstore.SchedulerTaskFilter{Active: &active, Limit: 10})
	if err != nil {
		return err
	}
	if len(tasks) != 0 {
		return &idleStateError{jobState: job.State, activeTask: len(tasks)}
	}
	return nil
}

func waitForIdleState(ctx context.Context, databasePath string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	var lastErr error
	for {
		if err := verifyIdleState(ctx, databasePath); err == nil {
			return nil
		} else {
			lastErr = err
		}
		if !time.Now().Before(deadline) {
			return lastErr
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(500 * time.Millisecond):
		}
	}
}

func stableFailure(err error) string {
	switch {
	case errors.Is(err, errIdleSetup):
		return "M11-IDLE-011: isolated setup failed"
	case errors.Is(err, errIdleLaunch):
		return "M11-IDLE-012: packaged app launch failed"
	case errors.Is(err, errIdleWarmup):
		return "M11-IDLE-013: packaged app exited during warmup"
	case errors.Is(err, errIdleState):
		var stateErr *idleStateError
		if errors.As(err, &stateErr) {
			return fmt.Sprintf(
				"M11-IDLE-016: packaged app did not reach durable idle state state=%s active=%d",
				stateErr.jobState, stateErr.activeTask,
			)
		}
		return "M11-IDLE-016: packaged app did not reach durable idle state"
	case errors.Is(err, errIdleProbe):
		return "M11-IDLE-014: process resource probe failed"
	case errors.Is(err, errIdleThreshold):
		return "M11-IDLE-015: tray idle threshold failed"
	default:
		return "M11-IDLE-001: tray idle validation failed"
	}
}

func validateConfig(input config) error {
	if !filepath.IsAbs(input.app) || filepath.Clean(input.app) != input.app || input.warmup < time.Second ||
		input.warmup > time.Minute || input.interval < 100*time.Millisecond || input.interval > 5*time.Second ||
		input.samples < 3 || input.samples > 120 {
		return errors.New("invalid tray idle config")
	}
	info, err := os.Stat(input.app)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&0o111 == 0 {
		return errors.New("invalid packaged app executable")
	}
	return nil
}

func configureSyntheticHome(ctx context.Context, userHome string) error {
	codexHome := filepath.Join(userHome, ".codex")
	for _, directory := range []string{filepath.Join(codexHome, "sessions"), filepath.Join(codexHome, "archived_sessions")} {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			return err
		}
	}
	rollout := []byte(`{"timestamp":"2026-07-14T01:00:00Z","type":"session_meta","payload":{"id":"m11-idle-session","timestamp":"2026-07-14T01:00:00Z","cwd":"/tmp/m11-idle","originator":"codex_cli_rs","cli_version":"0.142.3","source":"cli","model_provider":"openai"}}` + "\n")
	if err := os.WriteFile(filepath.Join(codexHome, "sessions", "idle.jsonl"), rollout, 0o600); err != nil {
		return err
	}
	metadata, err := logs.NewHomeProbe().Probe(ctx, codexHome)
	if err != nil {
		return err
	}
	preferencesDirectory := filepath.Join(userHome, "Library", "Application Support", "Codex Pulse")
	if err := os.MkdirAll(preferencesDirectory, 0o700); err != nil {
		return err
	}
	store, err := preferences.NewFileStore(filepath.Join(preferencesDirectory, "preferences.json"))
	if err != nil {
		return err
	}
	if err := store.Confirm(ctx, preferences.OnboardingSnapshot{
		SchemaVersion: preferences.CurrentSchemaVersion, OnboardingVersion: preferences.CurrentOnboardingVersion,
		OnboardingCompleted: true, CodexHome: preferences.ConfirmedSource{
			Path: metadata.Path, DeviceID: metadata.DeviceID, Inode: metadata.Inode,
			ConfirmedAtMS: time.Now().UnixMilli(),
		},
	}); err != nil {
		return err
	}
	snapshot, err := store.LoadPreferences(ctx)
	if err != nil {
		return err
	}
	snapshot.Revision++
	snapshot.Updates.AutoCheckEnabled = false
	snapshot.Online.QuotaEnabled = false
	snapshot.Online.ResetCreditsEnabled = false
	if err := store.CompareAndSwap(ctx, snapshot.Revision-1, snapshot); err != nil {
		return err
	}
	database, err := storesqlite.Open(ctx, storesqlite.Config{
		Path: filepath.Join(preferencesDirectory, "codex-pulse.db"),
	})
	if err != nil {
		return err
	}
	repository := factstore.NewRepository(database)
	if err := repository.EnsureApplicationSchema(ctx); err != nil {
		_ = database.Close(context.Background())
		return err
	}
	runtime, err := bootstrap.NewRuntime(bootstrap.RuntimeConfig{Repository: repository})
	if err == nil {
		err = runtime.StartBootstrap(ctx, preferences.BootstrapRequest{
			SwitchID: "m11-idle-bootstrap", Generation: 1,
			Source: preferences.ConfirmedSource{
				Path: metadata.Path, DeviceID: metadata.DeviceID, Inode: metadata.Inode,
				ConfirmedAtMS: snapshot.CodexHome.Source.ConfirmedAtMS,
			},
			DataStoreKey: preferences.DefaultDataStoreKey,
			Strategy:     preferences.HomeSwitchIndependentDatabase,
		})
	}
	return errors.Join(err, database.Close(context.Background()))
}

func stopProcess(command *exec.Cmd, exited <-chan error) {
	if command == nil || command.Process == nil {
		return
	}
	select {
	case <-exited:
		return
	default:
	}
	_ = command.Process.Signal(syscall.SIGTERM)
	select {
	case <-exited:
		return
	case <-time.After(5 * time.Second):
		_ = command.Process.Kill()
		select {
		case <-exited:
		case <-time.After(5 * time.Second):
		}
	}
}

func removeIsolatedRoot(root string) error {
	if filepath.Base(root) == "." || filepath.Clean(filepath.Dir(root)) != filepath.Clean(os.TempDir()) ||
		len(filepath.Base(root)) <= len(isolatedRootPrefix) ||
		filepath.Base(root)[:len(isolatedRootPrefix)] != isolatedRootPrefix {
		return errors.New("unsafe isolated root")
	}
	content, err := os.ReadFile(filepath.Join(root, ".owned"))
	if err != nil || string(content) != contractVersion {
		return errors.New("missing isolated root marker")
	}
	return os.RemoveAll(root)
}

func fileSize(path string) int64 {
	info, err := os.Stat(path)
	if err != nil {
		return 0
	}
	return info.Size()
}
