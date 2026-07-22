// swift-cancel-probe is a test-only grpc-go server used to prove that cancelling
// a Swift gRPC Task reaches the Go stream context. It is never shipped.
package main

import (
	"bufio"
	"bytes"
	"flag"
	"io"
	"log"
	"os"

	corev1 "github.com/SisyphusSQ/codex-pulse/api/codexpulse/core/v1"
	"github.com/SisyphusSQ/codex-pulse/internal/helper"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

type cancellationProbe struct {
	corev1.UnimplementedCoreServiceServer
	authenticator *helper.Authenticator
	markerPath    string
}

func (probe *cancellationProbe) SubscribeInvalidations(
	_ *corev1.SubscribeInvalidationsRequest,
	stream grpc.ServerStreamingServer[corev1.QueryInvalidationEvent],
) error {
	if err := probe.authenticator.Authorize(stream.Context()); err != nil {
		return err
	}
	if err := stream.SendHeader(metadata.Pairs("codex-pulse-stream-ready", "1")); err != nil {
		return err
	}
	<-stream.Context().Done()
	if err := os.WriteFile(probe.markerPath, []byte("cancelled\n"), 0o600); err != nil {
		return err
	}
	return status.FromContextError(stream.Context().Err()).Err()
}

func main() {
	socketPath := flag.String("socket", "", "Unix Domain Socket path")
	authFD := flag.Int("auth-fd", -1, "inherited authentication descriptor")
	markerPath := flag.String("preferences-path", "", "cancellation marker path")
	_ = flag.String("database-path", "", "unused isolated database path")
	flag.Parse()
	if *socketPath == "" || *authFD < 3 || *markerPath == "" || flag.NArg() != 0 {
		log.Fatal("invalid cancellation probe configuration")
	}

	pipe := os.NewFile(uintptr(*authFD), "auth-pipe")
	if pipe == nil {
		log.Fatal("auth pipe unavailable")
	}
	defer pipe.Close()
	reader := bufio.NewReader(pipe)
	tokenLine, err := reader.ReadBytes('\n')
	if err != nil || len(tokenLine) == 0 || tokenLine[len(tokenLine)-1] != '\n' {
		log.Fatal("auth token unavailable")
	}
	authenticator, err := helper.NewAuthenticator(bytes.TrimSuffix(tokenLine, []byte{'\n'}))
	if err != nil {
		log.Fatal("auth token invalid")
	}

	listener, cleanup, err := helper.ListenUnix(*socketPath)
	if err != nil {
		log.Fatal("listener unavailable")
	}
	defer cleanup()
	server := grpc.NewServer()
	corev1.RegisterCoreServiceServer(server, &cancellationProbe{
		authenticator: authenticator,
		markerPath:    *markerPath,
	})
	done := make(chan error, 1)
	go func() { done <- server.Serve(listener) }()
	parentEOF := make(chan struct{})
	go func() {
		_, _ = io.Copy(io.Discard, reader)
		close(parentEOF)
	}()
	select {
	case <-parentEOF:
		server.Stop()
	case err := <-done:
		if err != nil {
			log.Fatal("probe server failed")
		}
	}
	server.Stop()
	select {
	case <-done:
	default:
	}
}
