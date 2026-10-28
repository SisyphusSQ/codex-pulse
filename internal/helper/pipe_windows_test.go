//go:build windows

package helper

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Microsoft/go-winio"

	"github.com/SisyphusSQ/codex-pulse/internal/platformfs"
)

func TestWindowsPipeRejectsUnsafeAndOccupiedEndpoints(t *testing.T) {
	runtime := filepath.Join(t.TempDir(), "private")
	if err := platformfs.Mkdir(runtime, 0o700); err != nil {
		t.Fatal(err)
	}
	name := fmt.Sprintf(`\\.\pipe\codex-pulse-test-%d-%d`, os.Getpid(), time.Now().UnixNano())
	config := RuntimeConfig{SocketPath: name, RuntimeDirectory: runtime}
	listener, cleanup, err := listenEndpoint(config)
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	if _, _, err := listenEndpoint(config); err == nil {
		t.Fatal("second server occupied the endpoint")
	}
	accepted := make(chan error, 1)
	go func() {
		connection, err := listener.Accept()
		if err == nil {
			err = connection.Close()
		}
		accepted <- err
	}()
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	client, err := winio.DialPipeContext(ctx, name)
	if err != nil {
		t.Fatal(err)
	}
	client.Close()
	if err := <-accepted; err != nil {
		t.Fatal(err)
	}
	for _, invalid := range []string{`\\remote\pipe\codex-pulse-test`, `\\.\pipe\other-test`, name + `\nested`, name + ":stream"} {
		if _, _, err := listenEndpoint(RuntimeConfig{SocketPath: invalid, RuntimeDirectory: runtime}); err == nil {
			t.Fatalf("accepted %s", invalid)
		}
	}
	if err := cleanup(); err != nil {
		t.Fatal(err)
	}
	if err := cleanup(); err != nil {
		t.Fatal(err)
	}
	next, closeNext, err := listenEndpoint(config)
	if err != nil {
		t.Fatal(err)
	}
	defer closeNext()
	_ = next
	if _, _, err := listenEndpoint(RuntimeConfig{SocketPath: name + "-unsafe", RuntimeDirectory: t.TempDir()}); err == nil {
		t.Fatal("accepted inherited public runtime ACL")
	}
}
