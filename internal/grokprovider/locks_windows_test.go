//go:build windows

package grokprovider

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestWindowsGrokLockChild(t *testing.T) {
	path := os.Getenv("CODEX_PULSE_TEST_GROK_LOCK")
	if path == "" {
		t.Skip("subprocess-only fixture")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 400*time.Millisecond)
	defer cancel()
	lock, err := acquireAuthFileLock(ctx, path)
	if os.Getenv("CODEX_PULSE_TEST_GROK_LOCK_EXPECT") == "blocked" {
		if !errors.Is(err, ErrAuthRefreshFailed) {
			if lock != nil {
				lock.release()
			}
			t.Fatal("foreign process lock was bypassed")
		}
		return
	}
	if err != nil {
		t.Fatal("released cross-process lock could not be acquired")
	}
	lock.release()
}

func TestWindowsGrokLockIsCrossProcessAndCannotBeReplaced(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "auth.json")
	lock, err := acquireAuthFileLock(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	lockPath := filepath.Join(filepath.Dir(path), "auth.json.lock")
	if err := os.Rename(lockPath, lockPath+".moved"); err == nil {
		lock.release()
		t.Fatal("held refresh lock was replaceable")
	}
	binary, err := os.Executable()
	if err != nil {
		lock.release()
		t.Fatal(err)
	}
	run := func(want string) {
		t.Helper()
		ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
		defer cancel()
		command := exec.CommandContext(ctx, binary, "-test.run=^TestWindowsGrokLockChild$", "-test.count=1")
		command.Env = append(os.Environ(), "CODEX_PULSE_TEST_GROK_LOCK="+path, "CODEX_PULSE_TEST_GROK_LOCK_EXPECT="+want)
		if output, err := command.CombinedOutput(); err != nil {
			t.Fatalf("cross-process lock fixture failed: %v (%s)", err, output)
		}
	}
	run("blocked")
	lock.release()
	run("acquired")
}
