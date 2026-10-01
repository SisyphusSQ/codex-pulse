package repository

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"go.uber.org/fx"

	"github.com/SisyphusSQ/codex-pulse/server/config"
	gormv2 "github.com/SisyphusSQ/codex-pulse/server/internal/lib/gorm"
	"github.com/SisyphusSQ/codex-pulse/server/internal/models/do"
)

func openTestDatabase(t *testing.T, path string) (*Schema, *gormv2.Engine, *fx.App) {
	t.Helper()
	cfg, err := config.Load("../../config/config.yml")
	if err != nil {
		t.Fatal(err)
	}
	cfg.Database.Enabled = true
	cfg.Database.Driver = "sqlite"
	cfg.Database.Path = path
	var engine *gormv2.Engine
	app := fx.New(fx.NopLogger, fx.Supply(cfg), fx.Provide(gormv2.New), fx.Populate(&engine))
	if err := app.Err(); err != nil {
		t.Fatal(err)
	}
	if err := app.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := app.Stop(context.Background()); err != nil {
			t.Error(err)
		}
	})
	return NewSchema(engine), engine, app
}

func TestSchemaExplicitInitReopenAndVersionRejection(t *testing.T) {
	path := filepath.Join(t.TempDir(), "private", "center.sqlite")
	s, engine, app := openTestDatabase(t, path)
	if err := s.Check(t.Context()); !errors.Is(err, ErrSchemaMissing) {
		t.Fatalf("missing schema = %v", err)
	}
	if err := s.Init(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := s.Check(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := s.Init(t.Context()); err != nil {
		t.Fatalf("reentrant init: %v", err)
	}
	var count int64
	if err := engine.DB(t.Context()).Model(&do.SchemaVersion{}).Count(&count).Error; err != nil || count != 1 {
		t.Fatalf("markers=%d err=%v", count, err)
	}
	stat, err := os.Stat(path)
	if err != nil || stat.Mode().Perm() != 0600 {
		t.Fatalf("private file = %v %v", stat, err)
	}
	if err := app.Stop(t.Context()); err != nil {
		t.Fatal(err)
	}
	s, engine, _ = openTestDatabase(t, path)
	if err := s.Check(t.Context()); err != nil {
		t.Fatalf("reopen: %v", err)
	}
	if err := engine.DB(t.Context()).Model(&do.SchemaVersion{}).Where("id = ?", 1).Update("version", 99).Error; err != nil {
		t.Fatal(err)
	}
	if err := s.Check(t.Context()); !errors.Is(err, ErrSchemaIncompatible) {
		t.Fatalf("future schema = %v", err)
	}
	if err := s.Init(t.Context()); !errors.Is(err, ErrSchemaIncompatible) {
		t.Fatalf("init must not downgrade = %v", err)
	}
}

func TestSQLiteTransactionRollbackAndUniqueness(t *testing.T) {
	s, engine, _ := openTestDatabase(t, filepath.Join(t.TempDir(), "private", "center.sqlite"))
	if err := s.Init(t.Context()); err != nil {
		t.Fatal(err)
	}
	failure := errors.New("abort batch")
	err := engine.Transaction(t.Context(), func(ctx context.Context) error {
		if err := engine.DB(ctx).Exec("INSERT INTO pulse_batches(client_id,batch_id,digest,received_at_ms) VALUES(?,?,?,?)", "device", "batch", "digest", 1).Error; err != nil {
			return err
		}
		return failure
	})
	if !errors.Is(err, failure) {
		t.Fatal(err)
	}
	var count int64
	if err := engine.DB(t.Context()).Table("pulse_batches").Count(&count).Error; err != nil || count != 0 {
		t.Fatalf("rollback count=%d err=%v", count, err)
	}
	insert := func() error {
		return engine.DB(t.Context()).Exec("INSERT INTO pulse_batches(client_id,batch_id,digest,received_at_ms) VALUES(?,?,?,?)", "device", "batch", "digest", 1).Error
	}
	if err := insert(); err != nil {
		t.Fatal(err)
	}
	if err := insert(); err == nil {
		t.Fatal("duplicate batch accepted")
	}
	var foreignKeys int
	if err := engine.DB(t.Context()).Raw("PRAGMA foreign_keys").Scan(&foreignKeys).Error; err != nil || foreignKeys != 1 {
		t.Fatalf("foreign keys=%d err=%v", foreignKeys, err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := s.Init(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled init=%v", err)
	}
}

func TestSchemaRejectsMissingColumnsWithoutRepairingThem(t *testing.T) {
	s, engine, _ := openTestDatabase(t, filepath.Join(t.TempDir(), "private", "center.sqlite"))
	if err := s.Init(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := engine.DB(t.Context()).Exec("ALTER TABLE pulse_clients DROP COLUMN origin").Error; err != nil {
		t.Fatal(err)
	}
	if err := s.Check(t.Context()); !errors.Is(err, ErrSchemaIncompatible) {
		t.Fatalf("structural drift=%v", err)
	}
}
