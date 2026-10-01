//go:build integration

package cmd

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"uuid"

	"go.uber.org/fx"

	"github.com/SisyphusSQ/codex-pulse/server/config"
	apphttp "github.com/SisyphusSQ/codex-pulse/server/internal/http"
	gormv2 "github.com/SisyphusSQ/codex-pulse/server/internal/lib/gorm"
	"github.com/SisyphusSQ/codex-pulse/server/internal/lib/log"
)

type integrationDependencies struct {
	fx.In
	Server *apphttp.Server
	DB     *gormv2.Engine `optional:"true"`
}

func TestIsolatedComponents(t *testing.T) {
	if os.Getenv("APP_TEST_INTEGRATION") != "1" {
		t.Skip("isolated integration environment not enabled")
	}
	file := os.Getenv("APP_TEST_CONFIG")
	if !filepath.IsAbs(file) {
		t.Fatal("APP_TEST_CONFIG must be an absolute isolated config path")
	}
	cfg, err := config.Load(file)
	if err != nil {
		t.Fatal(err)
	}
	cfg.Server.Address = "127.0.0.1:0"
	if err := log.New(cfg); err != nil {
		t.Fatal(err)
	}
	defer log.Sync()
	var deps integrationDependencies
	app := fx.New(fx.NopLogger, inject(cfg), fx.Populate(&deps))
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	if err := app.Start(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		closeCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := app.Stop(closeCtx); err != nil {
			t.Error(err)
		}
	})
	recorder := httptest.NewRecorder()
	deps.Server.Echo.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/ready", nil))
	if recorder.Code != 200 {
		t.Fatalf("ready=%d", recorder.Code)
	}
	name := "starter_probe_" + strings.ReplaceAll(uuid.New().String(), "-", "")
	tested := false

	if deps.DB != nil {
		tested = true
		if err := deps.DB.DB(ctx).Exec("CREATE TABLE " + name + " (id BIGINT PRIMARY KEY)").Error; err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			closeCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := deps.DB.DB(closeCtx).Exec("DROP TABLE " + name).Error; err != nil {
				t.Error(err)
			}
		})
		rollback := errors.New("intentional rollback")
		err := deps.DB.Transaction(ctx, func(txctx context.Context) error {
			if err := deps.DB.DB(txctx).Exec("INSERT INTO "+name+" (id) VALUES (?)", 1).Error; err != nil {
				return err
			}
			return rollback
		})
		if !errors.Is(err, rollback) {
			t.Fatalf("rollback=%v", err)
		}
		var count int64
		if err := deps.DB.DB(ctx).Table(name).Count(&count).Error; err != nil || count != 0 {
			t.Fatalf("rollback left %d rows: %v", count, err)
		}
		if err := deps.DB.DB(ctx).Exec("INSERT INTO "+name+" (id) VALUES (?)", 1).Error; err != nil {
			t.Fatal(err)
		}
		unchanged := deps.DB.DB(ctx).Exec("UPDATE "+name+" SET id = ? WHERE id = ?", 1, 1)
		if unchanged.Error != nil || unchanged.RowsAffected != 1 {
			t.Fatalf("unchanged existing row must match once: rows=%d err=%v", unchanged.RowsAffected, unchanged.Error)
		}

	}

	_ = name
	_ = errors.New
	if !tested {
		t.Fatal("no isolated database or Redis enabled")
	}
}
