package schema_repo

import (
	"errors"
	"path/filepath"
	"testing"

	schema_do "github.com/SisyphusSQ/codex-pulse/server/internal/models/do/mysql/schema_do"
)

func TestExplicitV1UpgradePreservesDataAndRejectsDrift(t *testing.T) {
	s, engine, _ := openTestDatabase(t, filepath.Join(t.TempDir(), "private", "center.sqlite"))
	if err := s.Init(t.Context()); err != nil {
		t.Fatal(err)
	}
	db := engine.DB(t.Context())
	if err := db.Exec("INSERT INTO pulse_batches(client_id,batch_id,digest,received_at_ms) VALUES(?,?,?,?)", "device", "retained", "digest", 1).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("DROP TABLE pulse_account_settings").Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&schema_do.SchemaVersion{}).Where("id = ?", 1).Updates(map[string]any{"version": 1, "checksum": "wrong"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := s.Upgrade(t.Context()); !errors.Is(err, ErrSchemaIncompatible) || db.Migrator().HasTable("pulse_account_settings") {
		t.Fatal("drift repaired", err)
	}
	if err := db.Model(&schema_do.SchemaVersion{}).Where("id = ?", 1).Update("checksum", "e5e94cb539d004969cd009c78c12be71bbb3dcbd30e2a69dcb7b310b428e8568").Error; err != nil {
		t.Fatal(err)
	}
	if err := s.Check(t.Context()); !errors.Is(err, ErrSchemaIncompatible) {
		t.Fatal("startup auto upgraded", err)
	}
	if err := s.Upgrade(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := s.Upgrade(t.Context()); err != nil {
		t.Fatal("not reentrant", err)
	}
	var count int64
	if err := db.Table("pulse_batches").Where("batch_id = ?", "retained").Count(&count).Error; err != nil || count != 1 {
		t.Fatal("data lost", err, count)
	}
}

func TestV2UpgradeKeepsExistingStatusAndAddsChunkTables(t *testing.T) {
	s, engine, _ := openTestDatabase(t, filepath.Join(t.TempDir(), "private", "center.sqlite"))
	if err := s.Init(t.Context()); err != nil {
		t.Fatal(err)
	}
	db := engine.DB(t.Context())
	if err := db.Exec("INSERT INTO pulse_device_status(client_id,provider,version,collected_at_ms,coverage_start_ms,coverage_end_ms,pending_batches,status,received_at_ms) VALUES('retained','codex','old',1000,NULL,NULL,7,'partial',2000)").Error; err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"pulse_snapshot_chunks", "pulse_device_sync"} {
		if err := db.Migrator().DropTable(table); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Model(&schema_do.SchemaVersion{}).Where("id = ?", 1).Updates(map[string]any{"version": 2, "checksum": "de4c490365d9ba42a38041a5b6b3f7be4f92e5b2c275dc9b6a51ba0c234862ec"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := s.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	var pending int64
	if err := db.Table("pulse_device_status").Select("pending_batches").Where("client_id = ?", "retained").Scan(&pending).Error; err != nil || pending != 7 {
		t.Fatal("legacy status lost", err)
	}
	if !db.Migrator().HasTable("pulse_snapshot_chunks") || !db.Migrator().HasTable("pulse_device_sync") {
		t.Fatal("v3 tables missing")
	}
	if err := s.Check(t.Context()); err != nil {
		t.Fatal(err)
	}
}
