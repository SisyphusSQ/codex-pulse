package helper

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/SisyphusSQ/codex-pulse/internal/codex/logs"
	"github.com/SisyphusSQ/codex-pulse/internal/core"
	"github.com/SisyphusSQ/codex-pulse/internal/preferences"
	"golang.org/x/sys/unix"
	"google.golang.org/grpc/metadata"
)

func TestApplicationConfigPreservesDefaultAndExplicitPaths(t *testing.T) {
	broker, err := core.NewInvalidationBroker(1)
	if err != nil {
		t.Fatalf("NewInvalidationBroker() error = %v", err)
	}
	t.Cleanup(func() { broker.Close() })

	defaultConfig := applicationConfig(RuntimeConfig{}, broker)
	if defaultConfig.Store.Path != "" || defaultConfig.PreferencesPath != "" ||
		defaultConfig.DefaultCodexHome != "" {
		t.Fatalf("default application config = %#v, want empty path overrides", defaultConfig)
	}
	if defaultConfig.Broker != broker {
		t.Fatal("default application config lost broker")
	}

	explicit := applicationConfig(RuntimeConfig{
		DatabasePath:     "/private/tmp/cp/data/codex-pulse.db",
		PreferencesPath:  "/private/tmp/cp/preferences.json",
		DefaultCodexHome: "/private/tmp/cp/codex-home",
	}, broker)
	if explicit.Store.Path != "/private/tmp/cp/data/codex-pulse.db" ||
		explicit.PreferencesPath != "/private/tmp/cp/preferences.json" ||
		explicit.DefaultCodexHome != "/private/tmp/cp/codex-home" {
		t.Fatalf("explicit application config = %#v", explicit)
	}
}

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
	if _, err := writer.WriteString("abcdefghijklmnopqrstuvwxyzABCDEF0123456789_-token\n"); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
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
	metadata, err := logs.NewHomeProbe().Probe(t.Context(), codexHome)
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

func TestReadAuthPipeConsumesTokenAndSignalsOnlyParentEOF(t *testing.T) {
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = reader.Close(); _ = writer.Close() })
	token := strings.Repeat("a", 43)
	if _, err := writer.WriteString(token + "\n"); err != nil {
		t.Fatal(err)
	}
	authenticator, parentEOF, err := readAuthPipe(reader)
	if err != nil {
		t.Fatalf("readAuthPipe() error = %v", err)
	}
	ctx := metadata.NewIncomingContext(t.Context(), metadata.Pairs("authorization", "Bearer "+token))
	if err := authenticator.Authorize(ctx); err != nil {
		t.Fatalf("Authorize() error = %v", err)
	}
	select {
	case <-parentEOF:
		t.Fatal("parent EOF was signalled while pipe remained open")
	case <-time.After(20 * time.Millisecond):
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-parentEOF:
	case <-time.After(time.Second):
		t.Fatal("parent EOF was not signalled")
	}
}

func TestReadAuthPipeRejectsUnframedAndNonBearerSafeTokens(t *testing.T) {
	for name, payload := range map[string]string{
		"unframed": strings.Repeat("a", 43),
		"space":    strings.Repeat("a", 32) + " private\n",
	} {
		t.Run(name, func(t *testing.T) {
			reader, writer, err := os.Pipe()
			if err != nil {
				t.Fatal(err)
			}
			if _, err := writer.WriteString(payload); err != nil {
				t.Fatal(err)
			}
			_ = writer.Close()
			_, _, err = readAuthPipe(reader)
			_ = reader.Close()
			if err == nil {
				t.Fatal("readAuthPipe() accepted invalid token")
			}
		})
	}
}

func TestGracefulStopHonorsTimeout(t *testing.T) {
	block := make(chan struct{})
	stopCalled := make(chan struct{}, 1)
	server := grpcStopStub{
		graceful: func() { <-block },
		force:    func() { close(block); stopCalled <- struct{}{} },
	}
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Millisecond)
	defer cancel()
	gracefulStop(ctx, server)
	select {
	case <-stopCalled:
	case <-time.After(time.Second):
		t.Fatal("force Stop was not called after graceful timeout")
	}
}

type grpcStopStub struct {
	graceful func()
	force    func()
}

func (stub grpcStopStub) GracefulStop() { stub.graceful() }
func (stub grpcStopStub) Stop()         { stub.force() }
