package helper

import (
	"context"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"google.golang.org/grpc/metadata"

	"github.com/SisyphusSQ/codex-pulse/internal/core"
)

type readSizeRecorder struct {
	data       []byte
	offset     int
	maxRequest int
}

func (reader *readSizeRecorder) Read(buffer []byte) (int, error) {
	if len(buffer) > reader.maxRequest {
		reader.maxRequest = len(buffer)
	}
	if reader.offset >= len(reader.data) {
		return 0, io.EOF
	}
	count := copy(buffer, reader.data[reader.offset:])
	reader.offset += count
	return count, nil
}

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

func TestReadAuthPipeConsumesTokenAndSignalsOnlyParentEOF(t *testing.T) {
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = reader.Close(); _ = writer.Close() })
	token := strings.Repeat("a", 43)
	if err := writeAuthTokenForTest(writer, token); err != nil {
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

func TestReadBearerTokenDoesNotBufferFollowingPipeBytes(t *testing.T) {
	token := strings.Repeat("a", 43)
	reader := &readSizeRecorder{data: []byte(token + "\nparent-liveness-bytes")}
	got, err := readBearerToken(reader)
	if err != nil {
		t.Fatalf("readBearerToken() error = %v", err)
	}
	defer clear(got)
	if string(got) != token {
		t.Fatalf("readBearerToken() = %q", got)
	}
	if reader.maxRequest != 1 || reader.offset != len(token)+1 {
		t.Fatalf("token reader requested %d bytes and consumed %d, want bytewise token only", reader.maxRequest, reader.offset)
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

func writeAuthTokenForTest(writer *os.File, token string) error {
	_, err := writer.WriteString(token + "\n")
	return err
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
