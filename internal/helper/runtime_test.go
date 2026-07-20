package helper

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"google.golang.org/grpc/metadata"
)

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
