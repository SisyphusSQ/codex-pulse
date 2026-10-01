package cmd

import (
	"context"
	"github.com/SisyphusSQ/codex-pulse/server/config"
	"github.com/SisyphusSQ/codex-pulse/server/internal/lib/log"
	"go.uber.org/fx"
	"net"
	"testing"
	"time"
)

func runtimeConfig(t *testing.T) config.Config {
	t.Helper()
	cfg, err := config.Load("../../config/config.yml")
	if err != nil {
		t.Fatal(err)
	}
	cfg.Server.Address = "127.0.0.1:0"
	cfg.Database.Enabled = false
	cfg.ContextTimeout = time.Second
	if err := log.New(cfg); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = log.Sync() })
	return cfg
}
func TestDefaultApplicationStartsWithoutExternalServices(t *testing.T) {
	cfg := runtimeConfig(t)
	app := fx.New(fx.NopLogger, inject(cfg))
	if err := app.Err(); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	if err := app.Start(ctx); err != nil {
		t.Fatal(err)
	}
	if err := app.Stop(ctx); err != nil {
		t.Fatal(err)
	}
}
func TestProvidersDoNotConnectWhileBuildingGraph(t *testing.T) {
	cfg := runtimeConfig(t)
	cfg.Database.Enabled = true

	app := fx.New(fx.NopLogger, inject(cfg))
	if err := app.Err(); err != nil {
		t.Fatalf("constructing graph connected to external services: %v", err)
	}
}
func TestOccupiedPortFailsApplicationStartup(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	cfg := runtimeConfig(t)
	cfg.Server.Address = listener.Addr().String()
	app := fx.New(fx.NopLogger, inject(cfg))
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	if err := app.Start(ctx); err == nil {
		_ = app.Stop(ctx)
		t.Fatal("occupied listener was reported as started")
	}
}
