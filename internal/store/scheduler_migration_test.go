package store

import (
	"context"
	"testing"

	storesqlite "github.com/SisyphusSQ/codex-pulse/internal/store/sqlite"
)

// 测试 application schema v7 checksum 在scheduler contract冻结后保持不变。
func TestApplicationSchemaV7ChecksumIsFrozen(t *testing.T) {
	const want = "73a236152fbbd6c7421df81c1001cb029ebc868ba45f02f960ddadc8e81316ab"
	if got := applicationSchemaV7Checksum(); got != want {
		t.Fatalf("applicationSchemaV7Checksum() = %q, want frozen %q", got, want)
	}
}

// 测试 application schema v7 在全新数据库中创建调度任务、周期观测与 live job 表。
func TestApplicationSchemaV7CreatesSchedulerAndLiveJobTables(t *testing.T) {
	t.Parallel()

	if applicationSchemaVersion != 10 {
		t.Fatalf("applicationSchemaVersion = %d, want 10", applicationSchemaVersion)
	}
	database := openTestDatabase(t)
	repository := NewRepository(database)
	if err := repository.EnsureApplicationSchema(context.Background()); err != nil {
		t.Fatalf("EnsureApplicationSchema() error = %v", err)
	}
	assertMigrationVersionAndHistory(t, database, 10, 10)

	err := database.View(context.Background(), func(_ context.Context, connection storesqlite.ReadConn) error {
		for _, table := range []string{"scheduler_tasks", "scheduler_cycles", "live_scan_jobs"} {
			if !connection.Migrator().HasTable(table) {
				t.Errorf("schema v7 table %q missing", table)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("inspect schema v7: %v", err)
	}
}
