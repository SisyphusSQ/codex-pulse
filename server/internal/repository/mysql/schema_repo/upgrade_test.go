package schema_repo

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	reporting_do "github.com/SisyphusSQ/codex-pulse/server/internal/models/do/mysql/reporting_do"
	schema_do "github.com/SisyphusSQ/codex-pulse/server/internal/models/do/mysql/schema_do"
	"github.com/SisyphusSQ/codex-pulse/server/internal/repository/mysql/reporting_repo"
)

func initUpgradeFixture(t *testing.T, s *Schema) {
	t.Helper()
	if err := s.Init(t.Context()); err != nil {
		t.Fatal(err)
	}
	createLegacyUsageFixture(t, s)
}

func createLegacyUsageFixture(t *testing.T, s *Schema) {
	t.Helper()
	content, err := os.ReadFile("../../../../docs/sqls/schema/center_sqlite.sql")
	if err != nil {
		t.Fatal(err)
	}
	const prefix = "CREATE TABLE IF NOT EXISTS pulse_usage ("
	_, definition, found := strings.Cut(string(content), prefix)
	if !found {
		t.Fatal("old schema fixture lacks pulse_usage")
	}
	ddl, _, _ := strings.Cut(definition, ";")
	if err := s.engine.DB(t.Context()).Exec(prefix + ddl).Error; err != nil {
		t.Fatal(err)
	}
}

func TestExplicitV1UpgradePreservesDataAndRejectsDrift(t *testing.T) {
	s, engine, _ := openTestDatabase(t, filepath.Join(t.TempDir(), "private", "center.sqlite"))
	initUpgradeFixture(t, s)
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
	initUpgradeFixture(t, s)
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

func TestV4AccountTokenUpgradeIsAdditiveAndPreservesRows(t *testing.T) {
	s, engine, _ := openTestDatabase(t, filepath.Join(t.TempDir(), "private", "center.sqlite"))
	initUpgradeFixture(t, s)
	db := engine.DB(t.Context())
	if err := db.Exec("INSERT INTO pulse_batches(client_id,batch_id,digest,received_at_ms) VALUES('device','retained-v4','digest',1)").Error; err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"pulse_account_token_facts", "pulse_account_token_sources", "pulse_account_token_periods"} {
		if err := db.Migrator().DropTable(table); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Model(&schema_do.SchemaVersion{}).Where("id = ?", 1).Updates(map[string]any{"version": 4, "checksum": "ade85380eeb8f7c01b25432092d582d170e6462e3e4f0739f134e6247dae1b9a"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := s.Upgrade(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := s.Upgrade(t.Context()); err != nil {
		t.Fatal("reentrant", err)
	}
	var n int64
	if err := db.Table("pulse_batches").Where("batch_id = ?", "retained-v4").Count(&n).Error; err != nil || n != 1 {
		t.Fatal("data lost", err)
	}
	if !db.Migrator().HasTable("pulse_account_token_facts") {
		t.Fatal("missing v5")
	}
	if err := s.Check(t.Context()); err != nil {
		t.Fatal(err)
	}
}

func TestV5PublicationUpgradePreservesPendingChunksAndReceipts(t *testing.T) {
	s, engine, _ := openTestDatabase(t, filepath.Join(t.TempDir(), "private", "center.sqlite"))
	initUpgradeFixture(t, s)
	db := engine.DB(t.Context())
	for _, sql := range []string{
		"INSERT INTO pulse_batches VALUES('device','retained','digest',1000)",
		"INSERT INTO pulse_snapshot_chunks(client_id,source_key,revision,chunk_index,payload,payload_bytes,received_at_ms) VALUES('device','source',747,20,'{}',2,1000)",
		"DROP TABLE pulse_publication_jobs", "DROP TABLE pulse_usage_versions", "DROP TABLE pulse_session_publications", "DROP TABLE pulse_payload_parts", "DROP TABLE pulse_payload_owners",
	} {
		if err := db.Exec(sql).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Model(&schema_do.SchemaVersion{}).Where("id = ?", 1).Updates(map[string]any{"version": 5, "checksum": "1f476adc4fb3cc1d2f6318073589b48042decd39beb8359710db3f38186f8784"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := s.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := s.Migrate(t.Context()); err != nil {
		t.Fatal("reentrant migration", err)
	}
	for _, table := range []string{"pulse_batches", "pulse_snapshot_chunks"} {
		var n int64
		if err := db.Table(table).Count(&n).Error; err != nil || n != 1 {
			t.Fatal("backlog changed", table, n, err)
		}
	}
	if !db.Migrator().HasTable("pulse_publication_jobs") || !db.Migrator().HasIndex("pulse_usage_versions", "idx_usage_versions_time") {
		t.Fatal("v6 structure incomplete")
	}
}

func TestV6QueryIndexesUpgradeReentryAndDrift(t *testing.T) {
	s, engine, _ := openTestDatabase(t, filepath.Join(t.TempDir(), "private", "center.sqlite"))
	initUpgradeFixture(t, s)
	db := engine.DB(t.Context())
	if err := db.Exec("INSERT INTO pulse_batches VALUES('device','retained-v6','digest',1)").Error; err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"idx_publication_jobs_lease", "idx_publication_jobs_cleanup"} {
		if err := db.Migrator().DropIndex("pulse_publication_jobs", name); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Model(&schema_do.SchemaVersion{}).Where("id = ?", 1).Updates(map[string]any{"version": 6, "checksum": "65d89e7629f95edc6d80967b2f9839b80a792da91b166720fd247567f2c8f096"}).Error; err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := s.Migrate(t.Context()); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.Check(t.Context()); err != nil {
		t.Fatal(err)
	}
	var n int64
	if err := db.Table("pulse_batches").Count(&n).Error; err != nil || n != 1 {
		t.Fatal("receipt changed", n, err)
	}
	if err := db.Migrator().DropIndex("pulse_publication_jobs", "idx_publication_jobs_lease"); err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("CREATE INDEX idx_publication_jobs_lease ON pulse_publication_jobs(state)").Error; err != nil {
		t.Fatal(err)
	}
	if err := s.Migrate(t.Context()); !errors.Is(err, ErrSchemaIncompatible) {
		t.Fatal("index drift accepted", err)
	}
}

func TestV7PaidCreditsUpgradePreservesExistingFacts(t *testing.T) {
	s, engine, _ := openTestDatabase(t, filepath.Join(t.TempDir(), "private", "center.sqlite"))
	initUpgradeFixture(t, s)
	db := engine.DB(t.Context())
	if err := db.Exec("INSERT INTO pulse_batches VALUES('device','before-credits','digest',1000)").Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Migrator().DropTable("pulse_paid_credits"); err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&schema_do.SchemaVersion{}).Where("id = ?", 1).Updates(map[string]any{"version": 7, "checksum": "8a862017f2fc8963bce363e6c4f261d155a66671111affb95e412d2864165f7b"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := s.Upgrade(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := s.Upgrade(t.Context()); err != nil {
		t.Fatal("reentrant", err)
	}
	var n int64
	if err := db.Table("pulse_batches").Where("batch_id = ?", "before-credits").Count(&n).Error; err != nil || n != 1 {
		t.Fatal("existing fact changed", n, err)
	}
	if !db.Migrator().HasTable("pulse_paid_credits") {
		t.Fatal("credits table absent")
	}
	if err := s.Check(t.Context()); err != nil {
		t.Fatal(err)
	}
}

func TestV8UsageCutoverPreservesPublishedMultisetAndRollsBackOnFailure(t *testing.T) {
	s, engine, _ := openTestDatabase(t, filepath.Join(t.TempDir(), "private", "center.sqlite"))
	initUpgradeFixture(t, s)
	db := engine.DB(t.Context())
	if err := db.Model(&schema_do.SchemaVersion{}).Where("id = ?", 1).
		Updates(map[string]any{"version": 8, "checksum": "f2ed58099b1ebca810f87244ece40540c9eb04895fa892863d8ec6f43a5f3bdd"}).Error; err != nil {
		t.Fatal(err)
	}
	migrated := reporting_do.Usage{
		SessionKey: "migrated", ContributionID: "fact", Position: 4, ObservedAtMS: new(int64(100)), Model: new("gpt-6.2-sol"),
		InputTokens: new(int64(9007199254740993)), CachedTokens: new(int64(0)), CacheWriteTokens: new(int64(2)),
		OutputTokens: new(int64(3)), ReasoningTokens: new(int64(5)), TotalTokens: new(int64(9007199254741001)),
		CostMicroUSD: new(int64(7)), ReportedChargeMicroUSD: new(int64(8)), PricingVersion: new("price"),
		PricingMode: "event_cost", InputPrice: new(int64(9)), CachedPrice: new(int64(10)), OutputPrice: new(int64(11)), CostStatus: "known",
	}
	untimed := reporting_do.Usage{SessionKey: "migrated", ContributionID: "untimed", Position: 5, TotalTokens: new(int64(12)), CostStatus: "unpriced"}
	superseded := reporting_do.Usage{SessionKey: "published", ContributionID: "superseded", TotalTokens: new(int64(1000))}
	if err := db.Table("pulse_usage").Create(&[]reporting_do.Usage{migrated, untimed, superseded}).Error; err != nil {
		t.Fatal(err)
	}
	published := reporting_do.Usage{SessionKey: "published", ContributionID: "ready", Model: new("gpt-6.2-astra"), TotalTokens: new(int64(42))}
	for _, row := range []reporting_do.UsageVersion{{Generation: "active", Usage: published}, {Generation: "staged", Usage: superseded}} {
		if err := db.Create(&row).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Create(&reporting_do.SessionPublication{SessionKey: "published", Generation: "active"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`CREATE TRIGGER fail_cutover BEFORE INSERT ON pulse_session_publications
		WHEN NEW.session_key = 'migrated' BEGIN SELECT RAISE(ABORT, 'test cutover failure'); END`).Error; err != nil {
		t.Fatal(err)
	}
	if err := s.Upgrade(t.Context()); err == nil {
		t.Fatal("migration should have rolled back")
	}
	var copied int64
	if err := db.Model(&reporting_do.UsageVersion{}).Where("session_key = ?", "migrated").Count(&copied).Error; err != nil || copied != 0 {
		t.Fatalf("usage copy leaked outside the transaction: %d, %v", copied, err)
	}
	var marker schema_do.SchemaVersion
	if err := db.First(&marker).Error; err != nil || marker.Version != 8 {
		t.Fatalf("schema advanced on failure: %d, %v", marker.Version, err)
	}
	if err := db.Exec("DROP TRIGGER fail_cutover").Error; err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := s.Migrate(t.Context()); err != nil {
			t.Fatal(err)
		}
	}
	var got []reporting_do.Usage
	if err := reporting_repo.PublishedUsage(db).Select("u.*").Order("u.session_key,u.contribution_id").Find(&got).Error; err != nil {
		t.Fatal(err)
	}
	if want := []reporting_do.Usage{migrated, untimed, published}; !reflect.DeepEqual(got, want) {
		t.Fatalf("effective projections changed: %+v", got)
	}
}
