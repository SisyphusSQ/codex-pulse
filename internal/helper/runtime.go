package helper

import (
	"bufio"
	"context"
	"errors"
	"io"
	"net"
	"os"
	"time"

	"github.com/SisyphusSQ/codex-pulse/internal/app"
	"github.com/SisyphusSQ/codex-pulse/internal/core"
	storesqlite "github.com/SisyphusSQ/codex-pulse/internal/store/sqlite"
	"google.golang.org/grpc"
)

const (
	defaultInvalidationCapacity = 16
	defaultShutdownTimeout      = 15 * time.Second
)

var (
	ErrRuntime       = errors.New("helper runtime is unavailable")
	ErrAuthPipe      = errors.New("helper authentication pipe is invalid")
	ErrServerStopped = errors.New("helper grpc server stopped unexpectedly")
)

type RuntimeConfig struct {
	SocketPath       string
	AuthFD           uintptr
	HelperVersion    string
	ShutdownTimeout  time.Duration
	DatabasePath     string
	PreferencesPath  string
	DefaultCodexHome string
}

// Run starts the authenticated UDS server and blocks until shutdown RPC,
// signal cancellation, or parent-pipe EOF. It never opens a TCP listener.
func Run(ctx context.Context, config RuntimeConfig) error {
	if ctx == nil || config.SocketPath == "" || config.AuthFD < 3 || config.HelperVersion == "" ||
		config.ShutdownTimeout < 0 {
		return ErrRuntime
	}
	if config.ShutdownTimeout == 0 {
		config.ShutdownTimeout = defaultShutdownTimeout
	}
	authPipe := os.NewFile(config.AuthFD, "codex-pulse-auth-pipe")
	if authPipe == nil {
		return ErrAuthPipe
	}
	defer authPipe.Close()
	authenticator, parentEOF, err := readAuthPipe(authPipe)
	if err != nil {
		return err
	}
	broker, err := core.NewInvalidationBroker(defaultInvalidationCapacity)
	if err != nil {
		return errors.Join(ErrRuntime, err)
	}
	application, err := app.Open(ctx, applicationConfig(config, broker))
	if err != nil {
		broker.Close()
		return errors.Join(ErrRuntime, err)
	}
	listener, cleanupSocket, err := ListenUnix(config.SocketPath)
	if err != nil {
		_ = application.Close(context.Background())
		return err
	}
	defer cleanupSocket()
	server, err := NewGRPCServer(ServerConfig{
		Authenticator: authenticator, HelperVersion: config.HelperVersion,
		Service: application.Service(), Broker: application.Broker(), Recovery: application.Recovery(),
		Lifecycle: application, Shutdown: application,
	})
	if err != nil {
		_ = listener.Close()
		_ = application.Close(context.Background())
		return err
	}
	serveDone := make(chan error, 1)
	go func() { serveDone <- server.Serve(listener) }()

	var stopCause error
	select {
	case <-ctx.Done():
		stopCause = ctx.Err()
	case <-parentEOF:
	case _, ok := <-application.StopRequested():
		if !ok {
			stopCause = ErrRuntime
		}
	case serveErr := <-serveDone:
		if !errors.Is(serveErr, grpc.ErrServerStopped) && !errors.Is(serveErr, net.ErrClosed) {
			stopCause = errors.Join(ErrServerStopped, serveErr)
		}
	}

	rpcStopCtx, cancelRPCStop := context.WithTimeout(context.Background(), config.ShutdownTimeout)
	gracefulStop(rpcStopCtx, server)
	cancelRPCStop()
	drainCtx, cancelDrain := context.WithTimeout(context.Background(), config.ShutdownTimeout)
	closeErr := application.Close(drainCtx)
	cancelDrain()
	if errors.Is(stopCause, context.Canceled) {
		stopCause = nil
	}
	return errors.Join(stopCause, closeErr)
}

func applicationConfig(config RuntimeConfig, broker *core.InvalidationBroker) app.Config {
	return app.Config{
		Broker:           broker,
		Store:            storesqlite.Config{Path: config.DatabasePath},
		PreferencesPath:  config.PreferencesPath,
		DefaultCodexHome: config.DefaultCodexHome,
	}
}

func readAuthPipe(pipe *os.File) (*Authenticator, <-chan struct{}, error) {
	if pipe == nil {
		return nil, nil, ErrAuthPipe
	}
	reader := bufio.NewReaderSize(pipe, maximumAuthTokenBytes+2)
	token, err := reader.ReadBytes('\n')
	if err != nil || len(token) < 2 || len(token) > maximumAuthTokenBytes+1 {
		clear(token)
		return nil, nil, ErrAuthPipe
	}
	token = token[:len(token)-1]
	if !bearerSafeToken(token) {
		clear(token)
		return nil, nil, ErrAuthPipe
	}
	authenticator, err := NewAuthenticator(token)
	if err != nil {
		return nil, nil, ErrAuthPipe
	}
	parentEOF := make(chan struct{})
	go func() {
		_, _ = io.Copy(io.Discard, reader)
		close(parentEOF)
	}()
	return authenticator, parentEOF, nil
}

func bearerSafeToken(token []byte) bool {
	if len(token) < minimumAuthTokenBytes || len(token) > maximumAuthTokenBytes {
		return false
	}
	for _, value := range token {
		if (value >= 'a' && value <= 'z') || (value >= 'A' && value <= 'Z') ||
			(value >= '0' && value <= '9') || value == '-' || value == '_' {
			continue
		}
		return false
	}
	return true
}

type grpcStopper interface {
	GracefulStop()
	Stop()
}

func gracefulStop(ctx context.Context, server grpcStopper) {
	if server == nil {
		return
	}
	done := make(chan struct{})
	go func() {
		server.GracefulStop()
		close(done)
	}()
	select {
	case <-done:
	case <-ctx.Done():
		server.Stop()
		<-done
	}
}
