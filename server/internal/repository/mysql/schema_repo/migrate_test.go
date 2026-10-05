package schema_repo

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"

	"github.com/SisyphusSQ/codex-pulse/server/docs/sqls/schema"
	schema_do "github.com/SisyphusSQ/codex-pulse/server/internal/models/do/mysql/schema_do"
)

func TestAutomaticMigrationInitializesUpgradesAndPreservesData(t *testing.T) {
	s, engine, _ := openTestDatabase(t, filepath.Join(t.TempDir(), "private", "center.sqlite"))
	if err := s.Migrate(t.Context()); err != nil {
		t.Fatal("empty database", err)
	}
	db := engine.DB(t.Context())
	if err := db.Exec("INSERT INTO pulse_batches(client_id,batch_id,digest,received_at_ms) VALUES(?,?,?,?)", "device", "retained", "digest", 1).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("DROP TABLE pulse_account_settings").Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&schema_do.SchemaVersion{}).Where("id = ?", 1).Updates(map[string]any{"version": 1, "checksum": "e5e94cb539d004969cd009c78c12be71bbb3dcbd30e2a69dcb7b310b428e8568"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := s.Migrate(t.Context()); err != nil {
		t.Fatal("automatic v1 upgrade", err)
	}
	var marker schema_do.SchemaVersion
	if err := db.Take(&marker).Error; err != nil || marker.Version != schema.Version {
		t.Fatal("upgrade marker", err)
	}
	if err := s.Migrate(t.Context()); err != nil {
		t.Fatal("repeated startup", err)
	}
	var repeated schema_do.SchemaVersion
	if err := db.Take(&repeated).Error; err != nil || repeated != marker {
		t.Fatal("unchanged schema marker rewritten", err)
	}
	var count int64
	if err := db.Table("pulse_batches").Where("batch_id = ?", "retained").Count(&count).Error; err != nil || count != 1 {
		t.Fatal("existing data lost", err)
	}
}

func TestAutomaticMigrationRejectsDriftAndFutureVersion(t *testing.T) {
	for _, version := range []int64{1, 99} {
		t.Run(fmt.Sprint(version), func(t *testing.T) {
			s, engine, _ := openTestDatabase(t, filepath.Join(t.TempDir(), "private", "center.sqlite"))
			if err := s.Migrate(t.Context()); err != nil {
				t.Fatal(err)
			}
			db := engine.DB(t.Context())
			if err := db.Exec("DROP TABLE pulse_account_settings").Error; err != nil {
				t.Fatal(err)
			}
			if err := db.Model(&schema_do.SchemaVersion{}).Where("id = ?", 1).Updates(map[string]any{"version": version, "checksum": "unrecognized"}).Error; err != nil {
				t.Fatal(err)
			}
			if err := s.Migrate(t.Context()); !errors.Is(err, ErrSchemaIncompatible) {
				t.Fatal("unrecognized structure accepted", err)
			}
			if db.Migrator().HasTable("pulse_account_settings") {
				t.Fatal("unknown schema modified")
			}
		})
	}
}

func TestAutomaticV3UpgradeAddsProjectionWithoutChangingFacts(t *testing.T) {
	s, engine, _ := openTestDatabase(t, filepath.Join(t.TempDir(), "private", "center.sqlite"))
	if err := s.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	db := engine.DB(t.Context())
	for _, ddl := range []string{"DROP TABLE pulse_session_capsules", "DROP TABLE pulse_retired_observations", "INSERT INTO pulse_batches(client_id,batch_id,digest,received_at_ms) VALUES('synthetic','keep','digest',1)"} {
		if err := db.Exec(ddl).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Model(&schema_do.SchemaVersion{}).Where("id = ?", 1).Updates(map[string]any{"version": 3, "checksum": "a796cb171ecdf6edbf40fccc9543c1ed2b0012d95248dcdf9da472b3eaf51565"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := s.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	if !db.Migrator().HasIndex("pulse_session_capsules", "idx_capsules_session") {
		t.Fatal("capsule lookup index missing")
	}
	var count int64
	if err := db.Table("pulse_batches").Where("batch_id = ?", "keep").Count(&count).Error; err != nil || count != 1 {
		t.Fatal("old facts changed", err)
	}
}

func TestAutomaticUpgradeRollsBackFailedSQLiteDDL(t *testing.T) {
	s, engine, _ := openTestDatabase(t, filepath.Join(t.TempDir(), "private", "center.sqlite"))
	if err := s.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	db := engine.DB(t.Context())
	for _, statement := range []string{"DROP TABLE pulse_account_settings", "ALTER TABLE pulse_clients DROP COLUMN origin"} {
		if err := db.Exec(statement).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Model(&schema_do.SchemaVersion{}).Where("id = ?", 1).Updates(map[string]any{"version": 1, "checksum": "e5e94cb539d004969cd009c78c12be71bbb3dcbd30e2a69dcb7b310b428e8568"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := s.Migrate(t.Context()); !errors.Is(err, ErrSchemaIncompatible) {
		t.Fatal("drift accepted", err)
	}
	if db.Migrator().HasTable("pulse_account_settings") {
		t.Fatal("failed upgrade was not rolled back")
	}
	var marker schema_do.SchemaVersion
	if err := db.Take(&marker).Error; err != nil || marker.Version != 1 {
		t.Fatal("failed upgrade advanced marker", err)
	}
}

func TestMySQLMigrationLockLifecycle(t *testing.T) {
	for _, scenario := range []string{"success", "busy", "migration_failure", "canceled", "release_failure"} {
		t.Run(scenario, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			gdb, err := gorm.Open(mysql.New(mysql.Config{Conn: db, SkipInitializeWithVersion: true}), &gorm.Config{DisableAutomaticPing: true})
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			sum := sha256.Sum256([]byte("synthetic_center"))
			name := fmt.Sprintf("codex-pulse-schema:%x", sum[:16])
			mock.ExpectQuery(regexp.QuoteMeta("SELECT DATABASE()")).WillReturnRows(sqlmock.NewRows([]string{"database"}).AddRow("synthetic_center"))
			acquired := 1
			if scenario == "busy" {
				acquired = 0
			}
			mock.ExpectQuery(regexp.QuoteMeta("SELECT GET_LOCK(?, ?)")).WithArgs(name, 30).WillReturnRows(sqlmock.NewRows([]string{"acquired"}).AddRow(acquired))
			if scenario != "busy" {
				release := mock.ExpectQuery(regexp.QuoteMeta("SELECT RELEASE_LOCK(?)")).WithArgs(name)
				if scenario == "release_failure" {
					release.WillReturnError(errors.New("release failed"))
				} else {
					release.WillReturnRows(sqlmock.NewRows([]string{"released"}).AddRow(1))
				}
			}
			called := false
			failure := errors.New("migration failed")
			err = gdb.WithContext(ctx).Connection(func(conn *gorm.DB) error {
				return mysqlMigrationLock(ctx, conn, func() error {
					called = true
					if scenario == "canceled" {
						cancel()
						return ctx.Err()
					}
					if scenario == "migration_failure" {
						return failure
					}
					return nil
				})
			})
			if scenario == "success" && err != nil {
				t.Fatal(err)
			}
			if scenario != "success" && err == nil {
				t.Fatal("failure was hidden")
			}
			if called != (scenario != "busy") {
				t.Fatal("migration ran without lock")
			}
			if scenario == "migration_failure" && !errors.Is(err, failure) {
				t.Fatal("migration error lost", err)
			}
			if scenario == "canceled" && !errors.Is(err, context.Canceled) {
				t.Fatal("cancellation lost", err)
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		})
	}
}
