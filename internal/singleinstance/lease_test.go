package singleinstance

import (
	"bufio"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestLeaseReclaimsAfterOwnerProcessCrash(t *testing.T) {
	directory := t.TempDir()
	command := exec.Command(os.Args[0], "-test.run=TestLeaseCrashHelper")
	command.Env = append(os.Environ(), "CODEX_PULSE_LEASE_CRASH_HELPER=1", "CODEX_PULSE_LEASE_DIR="+directory)
	stdout, err := command.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	reader := bufio.NewReader(stdout)
	line, err := reader.ReadString('\n')
	if err != nil || line != "ready\n" {
		_ = command.Process.Kill()
		t.Fatalf("helper readiness=%q err=%v", line, err)
	}
	if err := command.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_ = command.Wait()
	lease, owned, err := Acquire(t.Context(), Config{Directory: directory, Name: "crash-test"})
	if err != nil || !owned || lease == nil {
		t.Fatalf("Acquire after crash=(%v,%v) err=%v", lease, owned, err)
	}
	if err := lease.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestLeaseCrashHelper(t *testing.T) {
	if os.Getenv("CODEX_PULSE_LEASE_CRASH_HELPER") != "1" {
		return
	}
	lease, owned, err := Acquire(context.Background(), Config{Directory: os.Getenv("CODEX_PULSE_LEASE_DIR"), Name: "crash-test"})
	if err != nil || !owned {
		os.Exit(2)
	}
	_ = lease
	_, _ = os.Stdout.WriteString("ready\n")
	select {}
}

func TestAcquireWakesOwnerAndAllowsTakeoverAfterClose(t *testing.T) {
	config := Config{Directory: t.TempDir(), Name: "codex-pulse-test", NotifyTimeout: time.Second}
	owner, owned, err := Acquire(t.Context(), config)
	if err != nil || !owned || owner == nil {
		t.Fatalf("Acquire owner=(%v,%v) err=%v", owner, owned, err)
	}
	t.Cleanup(func() { _ = owner.Close() })

	second, secondOwned, err := Acquire(t.Context(), config)
	if err != nil || secondOwned || second != nil {
		t.Fatalf("Acquire second=(%v,%v) err=%v", second, secondOwned, err)
	}
	select {
	case <-owner.Wake():
	case <-time.After(time.Second):
		t.Fatal("owner did not receive wake")
	}
	if err := owner.Close(); err != nil {
		t.Fatalf("Close owner: %v", err)
	}
	takeover, takeoverOwned, err := Acquire(t.Context(), config)
	if err != nil || !takeoverOwned || takeover == nil {
		t.Fatalf("Acquire takeover=(%v,%v) err=%v", takeover, takeoverOwned, err)
	}
	if err := takeover.Close(); err != nil {
		t.Fatalf("Close takeover: %v", err)
	}
	if _, err := os.Stat(filepath.Join(config.Directory, config.Name+".lock")); err != nil {
		t.Fatalf("lock inode was removed: %v", err)
	}
}

func TestAcquireProtectsRuntimeFilesAndRejectsInvalidConfig(t *testing.T) {
	directory := filepath.Join(t.TempDir(), "nested")
	lease, owned, err := Acquire(t.Context(), Config{Directory: directory, Name: "app"})
	if err != nil || !owned {
		t.Fatalf("Acquire owned=%v err=%v", owned, err)
	}
	for _, path := range []string{directory, filepath.Join(directory, "app.lock"), lease.socketPath} {
		info, statErr := os.Stat(path)
		if statErr != nil {
			t.Fatalf("Stat %s: %v", path, statErr)
		}
		if info.Mode().Perm()&0o077 != 0 {
			t.Fatalf("%s permissions=%#o", path, info.Mode().Perm())
		}
	}
	if err := lease.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if _, _, err := Acquire(context.Background(), Config{Name: "../escape", Directory: directory}); err == nil {
		t.Fatal("invalid config succeeded")
	}
}

func TestContenderTakesOverWhenOwnerStopsAcknowledgingWakes(t *testing.T) {
	config := Config{Directory: t.TempDir(), Name: "shutdown-race", NotifyTimeout: 2 * time.Second}
	owner, owned, err := Acquire(t.Context(), config)
	if err != nil || !owned {
		t.Fatalf("owner=%v err=%v", owned, err)
	}
	owner.StopAcceptingWakes()
	result := make(chan struct {
		lease *Lease
		owned bool
		err   error
	}, 1)
	go func() {
		lease, nextOwned, acquireErr := Acquire(context.Background(), config)
		result <- struct {
			lease *Lease
			owned bool
			err   error
		}{lease: lease, owned: nextOwned, err: acquireErr}
	}()
	// This deliberately exceeds the old 750ms default. A real safe shutdown
	// may spend materially longer draining SQLite and scheduler work.
	time.Sleep(900 * time.Millisecond)
	if err := owner.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case next := <-result:
		if next.err != nil || !next.owned || next.lease == nil {
			t.Fatalf("takeover=(%v,%v) err=%v", next.lease, next.owned, next.err)
		}
		_ = next.lease.Close()
	case <-time.After(2 * time.Second):
		t.Fatal("contender did not take over")
	}
}
