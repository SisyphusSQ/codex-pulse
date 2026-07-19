package main

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/SisyphusSQ/codex-pulse/internal/preferences"
)

func TestConfigureSyntheticHomeCreatesOfflineConfirmedPreferences(t *testing.T) {
	userHome := filepath.Join(t.TempDir(), "home")
	if err := configureSyntheticHome(context.Background(), userHome); err != nil {
		t.Fatalf("configureSyntheticHome() error = %v", err)
	}
	store, err := preferences.NewFileStore(filepath.Join(
		userHome, "Library", "Application Support", "Codex Pulse", "preferences.json",
	))
	if err != nil {
		t.Fatalf("NewFileStore() error = %v", err)
	}
	snapshot, err := store.LoadPreferences(context.Background())
	if err != nil || !snapshot.Onboarding.Completed || snapshot.Updates.AutoCheckEnabled ||
		snapshot.Online.QuotaEnabled || snapshot.Online.ResetCreditsEnabled {
		t.Fatalf("offline idle preferences = %#v, %v", snapshot, err)
	}
}

func TestRemoveIsolatedRootRequiresOwnedMarker(t *testing.T) {
	root, err := os.MkdirTemp("", isolatedRootPrefix)
	if err != nil {
		t.Fatalf("MkdirTemp() error = %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(root) })
	if err := removeIsolatedRoot(root); err == nil {
		t.Fatal("removeIsolatedRoot() error = nil without marker")
	}
	if err := os.WriteFile(filepath.Join(root, ".owned"), []byte(contractVersion), 0o600); err != nil {
		t.Fatalf("WriteFile(marker) error = %v", err)
	}
	if err := removeIsolatedRoot(root); err != nil {
		t.Fatalf("removeIsolatedRoot() error = %v", err)
	}
	if _, err := os.Stat(root); !os.IsNotExist(err) {
		t.Fatalf("isolated root still exists: %v", err)
	}
}

func TestValidateConfigRejectsUnsafeSampling(t *testing.T) {
	executable := filepath.Join(t.TempDir(), "app")
	if err := os.WriteFile(executable, []byte("fixture"), 0o700); err != nil {
		t.Fatalf("WriteFile(app) error = %v", err)
	}
	if err := validateConfig(config{app: executable, warmup: time.Second, interval: time.Second, samples: 3}); err != nil {
		t.Fatalf("validateConfig(valid) error = %v", err)
	}
	if err := validateConfig(config{app: executable, warmup: 0, interval: time.Second, samples: 3}); err == nil {
		t.Fatal("validateConfig(warmup) error = nil")
	}
}

func TestReplaceEnvironmentLeavesOneAuthoritativeHome(t *testing.T) {
	got := replaceEnvironment([]string{"PATH=/bin", "HOME=/real", "HOME=/stale"}, "HOME", "/isolated")
	want := []string{"PATH=/bin", "HOME=/isolated"}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("replaceEnvironment() = %#v, want %#v", got, want)
	}
}

func TestExecuteCancellationStopsChildAndCleansIsolatedRoot(t *testing.T) {
	before, err := filepath.Glob(filepath.Join(os.TempDir(), isolatedRootPrefix+"*"))
	if err != nil {
		t.Fatalf("Glob(before) error = %v", err)
	}
	fixtureDirectory := t.TempDir()
	pidPath := filepath.Join(fixtureDirectory, "pid")
	executable := filepath.Join(fixtureDirectory, "idle-child")
	script := "#!/bin/sh\nprintf '%s' \"$$\" > " + strconv.Quote(pidPath) + "\nexec /bin/sleep 30\n"
	if err := os.WriteFile(executable, []byte(script), 0o700); err != nil {
		t.Fatalf("WriteFile(executable) error = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	childReady := make(chan int, 1)
	go func() {
		deadline := time.Now().Add(5 * time.Second)
		for time.Now().Before(deadline) {
			content, readErr := os.ReadFile(pidPath)
			if readErr == nil {
				pid, parseErr := strconv.Atoi(strings.TrimSpace(string(content)))
				if parseErr == nil {
					childReady <- pid
					cancel()
					return
				}
			}
			time.Sleep(10 * time.Millisecond)
		}
		childReady <- 0
		cancel()
	}()
	_, runErr := execute(ctx, config{
		app: executable, warmup: time.Second, interval: 100 * time.Millisecond, samples: 3,
	})
	pid := <-childReady
	if !errors.Is(runErr, context.Canceled) || pid <= 0 {
		t.Fatalf("execute() error = %v, pid = %d", runErr, pid)
	}
	if err := syscall.Kill(pid, 0); !errors.Is(err, syscall.ESRCH) {
		t.Fatalf("child process %d still exists: %v", pid, err)
	}
	after, err := filepath.Glob(filepath.Join(os.TempDir(), isolatedRootPrefix+"*"))
	if err != nil {
		t.Fatalf("Glob(after) error = %v", err)
	}
	if len(after) != len(before) {
		t.Fatalf("isolated roots after cancellation = %v, before = %v", after, before)
	}
}

func TestStopProcessReturnsImmediatelyAfterExitWasObserved(t *testing.T) {
	command := exec.Command("/bin/sh", "-c", "exit 7")
	if err := command.Start(); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	exited := make(chan error, 1)
	go func() {
		exited <- command.Wait()
		close(exited)
	}()
	<-exited
	started := time.Now()
	stopProcess(command, exited)
	if elapsed := time.Since(started); elapsed > 250*time.Millisecond {
		t.Fatalf("stopProcess() elapsed = %s after observed exit", elapsed)
	}
}
