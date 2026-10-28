//go:build !windows

package helper

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/sys/unix"

	logsource "github.com/SisyphusSQ/codex-pulse/internal/codex/logs/source"
	"github.com/SisyphusSQ/codex-pulse/internal/preferences"
)

func TestRunStartsWithExplicitIsolatedPaths(t *testing.T) {
	root, err := os.MkdirTemp("/private/tmp", "cp-helper-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(root) })
	if err := os.Chmod(root, 0o700); err != nil {
		t.Fatal(err)
	}
	dataDirectory := filepath.Join(root, "data")
	if err := os.Mkdir(dataDirectory, 0o700); err != nil {
		t.Fatal(err)
	}
	codexHome := filepath.Join(root, "codex-home")
	for _, directory := range []string{
		codexHome,
		filepath.Join(codexHome, "sessions"),
		filepath.Join(codexHome, "archived_sessions"),
	} {
		if err := os.Mkdir(directory, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	preferencesPath := filepath.Join(root, "preferences.json")
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	defer writer.Close()
	authFD, err := unix.Dup(int(reader.Fd()))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() {
		done <- Run(ctx, RuntimeConfig{
			SocketPath:       filepath.Join(root, "core.sock"),
			AuthFD:           uintptr(authFD),
			HelperVersion:    "test",
			DatabasePath:     filepath.Join(dataDirectory, "codex-pulse.db"),
			PreferencesPath:  preferencesPath,
			DefaultCodexHome: codexHome,
		})
	}()
	if err := writeAuthTokenForTest(writer, "abcdefghijklmnopqrstuvwxyzABCDEF0123456789_-token"); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(30 * time.Second)
	for {
		if _, err := os.Stat(filepath.Join(root, "core.sock")); err == nil {
			break
		}
		select {
		case err := <-done:
			t.Fatalf("Run() exited before socket was ready: %v", err)
		default:
		}
		if time.Now().After(deadline) {
			t.Fatal("Run() did not create socket")
		}
		time.Sleep(10 * time.Millisecond)
	}
	preferenceStore, err := preferences.NewFileStore(preferencesPath)
	if err != nil {
		t.Fatalf("NewFileStore() error = %v", err)
	}
	snapshot, err := preferenceStore.LoadPreferences(t.Context())
	if err != nil {
		t.Fatalf("LoadPreferences() error = %v", err)
	}
	metadata, err := logsource.NewHomeProbe().Probe(t.Context(), codexHome)
	if err != nil {
		t.Fatalf("Probe() error = %v", err)
	}
	if snapshot.CodexHome.Source.Path != metadata.Path ||
		snapshot.CodexHome.Source.DeviceID != metadata.DeviceID ||
		snapshot.CodexHome.Source.Inode != metadata.Inode {
		t.Fatalf("automatic Home = %#v, want %#v", snapshot.CodexHome.Source, metadata)
	}
	if !snapshot.Online.QuotaEnabled || !snapshot.Online.ResetCreditsEnabled {
		t.Fatalf("automatic online defaults = %#v, want enabled", snapshot.Online)
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run() error = %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Run() did not stop after cancellation")
	}
}
