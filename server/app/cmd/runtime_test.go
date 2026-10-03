package cmd

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"go.uber.org/fx"

	"github.com/SisyphusSQ/codex-pulse/server/config"
	apphttp "github.com/SisyphusSQ/codex-pulse/server/internal/http"
	gormv2 "github.com/SisyphusSQ/codex-pulse/server/internal/lib/gorm"
	"github.com/SisyphusSQ/codex-pulse/server/internal/lib/log"
	schema_do "github.com/SisyphusSQ/codex-pulse/server/internal/models/do/mysql/schema_do"
	"github.com/SisyphusSQ/codex-pulse/server/internal/repository/mysql/schema_repo"
)

func runtimeConfig(t *testing.T) config.Config {
	t.Helper()
	cfg, err := config.Load("../../config/config.yml")
	if err != nil {
		t.Fatal(err)
	}
	cfg.Server.Address = "127.0.0.1:0"
	cfg.Database.Enabled = false
	cfg.Database.Driver = "sqlite"
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

func TestApplicationStartupAutomaticallyInitializesSchema(t *testing.T) {
	cfg := runtimeConfig(t)
	cfg.Database.Enabled = true
	cfg.Database.Path = filepath.Join(t.TempDir(), "private", "center.sqlite")
	var engine *gormv2.Engine
	var server *apphttp.Server
	app := fx.New(fx.NopLogger, inject(cfg), fx.Populate(&engine, &server))
	if err := app.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := app.Stop(context.Background()); err != nil {
			t.Error(err)
		}
	})
	if err := schema_repo.NewSchema(engine).Check(t.Context()); err != nil {
		t.Fatal("startup did not migrate", err)
	}
	response := httptest.NewRecorder()
	server.Echo.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "http://127.0.0.1/", nil))
	if response.Code != 200 {
		t.Fatal("embedded Web unavailable after migration", response.Code)
	}
}

func TestIncompatibleSchemaStopsApplicationStartup(t *testing.T) {
	cfg := runtimeConfig(t)
	cfg.Database.Enabled = true
	cfg.Database.Path = filepath.Join(t.TempDir(), "private", "center.sqlite")
	if err := withConfiguredDatabase(t.Context(), cfg, func(ctx context.Context, engine *gormv2.Engine) error {
		if err := schema_repo.NewSchema(engine).Migrate(ctx); err != nil {
			return err
		}
		return engine.DB(ctx).Model(&schema_do.SchemaVersion{}).Where("id = ?", 1).Update("version", 99).Error
	}); err != nil {
		t.Fatal(err)
	}
	app := fx.New(fx.NopLogger, inject(cfg))
	if err := app.Start(t.Context()); err == nil {
		_ = app.Stop(t.Context())
		t.Fatal("incompatible schema accepted")
	}
}
