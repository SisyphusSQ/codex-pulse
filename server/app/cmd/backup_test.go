package cmd

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/SisyphusSQ/codex-pulse/server/config"
	gormv2 "github.com/SisyphusSQ/codex-pulse/server/internal/lib/gorm"
	"github.com/SisyphusSQ/codex-pulse/server/internal/models/do/mysql/access_do"
	"github.com/SisyphusSQ/codex-pulse/server/internal/models/do/mysql/reporting_do"
	"github.com/SisyphusSQ/codex-pulse/server/internal/repository/mysql/schema_repo"
)

func TestSQLiteBackupRestoreFactsReceiptsAndAuthorization(t *testing.T) {
	cfg, err := config.Load("../../config/config.yml")
	if err != nil {
		t.Fatal(err)
	}
	cfg.ContextTimeout = 20 * time.Second
	cfg.Database.Path = filepath.Join(t.TempDir(), "source", "center.sqlite")
	var originalDigest string
	if err := withConfiguredDatabase(t.Context(), cfg, func(ctx context.Context, e *gormv2.Engine) error {
		if err := schema_repo.NewSchema(e).Init(ctx); err != nil {
			return err
		}
		for _, value := range []any{&access_do.Client{ID: "synthetic-device", Purpose: "collector", Name: "synthetic", SecretHash: "synthetic-noncredential-hash", CreatedAtMS: 1000}, &access_do.Pairing{CodeHash: "synthetic-unused", Purpose: "admin", Name: "synthetic", CreatedAtMS: 1000, ExpiresAtMS: 9000}, &reporting_do.Session{ID: "synthetic-session", Provider: "codex", SessionID: "original-session", Title: "synthetic title", CollectedAtMS: 2000}, &reporting_do.Batch{ClientID: "synthetic-device", BatchID: "synthetic-batch", Digest: "synthetic-digest", ReceivedAtMS: 2500}, &reporting_do.QuotaObservation{ID: "synthetic-quota", ClientID: "synthetic-device", Provider: "codex", LocalScope: "synthetic-account", ObservationID: "observation", LimitID: "primary", WindowKind: "primary", ObservedAtMS: 2000, ReceivedAtMS: 2500, Validity: "valid", ResetsAtMS: new(int64(5000)), UsedPercent: new(float64(0))}} {
			if err := e.DB(ctx).Create(value).Error; err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	originalDigest, err = fileDigest(t.Context(), cfg.Database.Path)
	if err != nil {
		t.Fatal(err)
	}
	backup := filepath.Join(t.TempDir(), "backup")
	if err := backupSQLite(t.Context(), cfg, backup); err != nil {
		t.Fatal(err)
	}
	if err := backupSQLite(t.Context(), cfg, backup); err == nil {
		t.Fatal("existing backup overwritten")
	}
	restored := cfg
	restored.Database.Path = filepath.Join(t.TempDir(), "restored", "center.sqlite")
	if err := restoreSQLite(t.Context(), restored, backup); err != nil {
		t.Fatal(err)
	}
	if err := restoreSQLite(t.Context(), restored, backup); err == nil {
		t.Fatal("existing database overwritten")
	}
	if err := withConfiguredDatabase(t.Context(), restored, func(ctx context.Context, e *gormv2.Engine) error {
		if err := schema_repo.NewSchema(e).Integrity(ctx); err != nil {
			return err
		}
		var session reporting_do.Session
		if err := e.DB(ctx).Take(&session).Error; err != nil {
			return err
		}
		if session.SessionID != "original-session" || session.Title != "synthetic title" || session.CollectedAtMS != 2000 {
			t.Fatal("session changed")
		}
		var receipt reporting_do.Batch
		if err := e.DB(ctx).Take(&receipt).Error; err != nil {
			return err
		}
		if receipt.Digest != "synthetic-digest" || receipt.ReceivedAtMS != 2500 {
			t.Fatal("receipt changed")
		}
		var quota reporting_do.QuotaObservation
		if err := e.DB(ctx).Take(&quota).Error; err != nil {
			return err
		}
		if quota.UsedPercent == nil || *quota.UsedPercent != 0 || *quota.ResetsAtMS != 5000 || quota.ObservedAtMS != 2000 {
			t.Fatal("quota changed/freshened")
		}
		var client access_do.Client
		if err := e.DB(ctx).Take(&client).Error; err != nil {
			return err
		}
		if client.RevokedAtMS == nil {
			t.Fatal("restored authorization active")
		}
		var pairing access_do.Pairing
		if err := e.DB(ctx).Take(&pairing).Error; err != nil {
			return err
		}
		if pairing.ConsumedAtMS == nil {
			t.Fatal("restored code active")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if digest, err := fileDigest(t.Context(), cfg.Database.Path); err != nil || digest != originalDigest {
		t.Fatal("backup modified original database")
	}
	if err := os.WriteFile(filepath.Join(backup, "center.sqlite"), []byte("corrupted"), 0600); err != nil {
		t.Fatal(err)
	}
	restored.Database.Path = filepath.Join(t.TempDir(), "corrupt", "center.sqlite")
	if err := restoreSQLite(t.Context(), restored, backup); err == nil {
		t.Fatal("corrupted backup accepted")
	}
	if _, err := os.Stat(restored.Database.Path); !os.IsNotExist(err) {
		t.Fatal("failed restore published")
	}
}
