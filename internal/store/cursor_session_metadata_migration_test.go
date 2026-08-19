package store

import (
	"context"
	"strings"
	"testing"
	"time"

	"gorm.io/gorm"

	storesqlite "github.com/SisyphusSQ/codex-pulse/internal/store/sqlite"
)

func TestApplicationMigrationAddsCursorSessionMetadataWithoutLosingSessions(t *testing.T) {
	t.Parallel()

	database := openTestDatabase(t)
	v24Runner := migrationRunner{
		repository: NewRepository(database), catalog: applicationMigrations[:applicationSchemaV24Version],
		now:           func() time.Time { return time.UnixMilli(123) },
		verifyCurrent: func(context.Context, *gorm.DB) error { return nil },
	}
	if _, err := v24Runner.run(context.Background()); err != nil {
		t.Fatalf("run(v24) error = %v", err)
	}
	if err := database.Write(context.Background(), func(ctx context.Context, transaction *gorm.DB) error {
		if err := transaction.WithContext(ctx).Create(&cursorSnapshotModel{
			Provider: "cursor", Generation: 1, CollectedAtMS: 2_000,
		}).Error; err != nil {
			return err
		}
		return transaction.WithContext(ctx).Exec(`INSERT INTO cursor_sessions (
			provider, external_session_id, project_key, project_display_name,
			created_at_ms, last_activity_at_ms, request_count, tool_call_count, ai_edit_count,
			lineage_conflict, coverage_state, updated_at_ms
		) VALUES ('cursor', 'session-a', ?, 'Project', 1000, 2000, 0, 0, 0, 0, 'partial', 2000)`,
			strings.Repeat("a", 64)).Error
	}); err != nil {
		t.Fatalf("seed cursor v24 session: %v", err)
	}

	var backupVersions [2]int
	runner := applicationMigrationRunnerForTest(database)
	runner.spaceCheck = func(context.Context, string, int64) error { return nil }
	runner.backup = func(
		_ context.Context, fromVersion int, targetVersion int, _ func(storesqlite.BackupProgress),
	) (string, error) {
		backupVersions = [2]int{fromVersion, targetVersion}
		return "/tmp/application-v24-before-v26.db", nil
	}
	report, err := runner.run(context.Background())
	if err != nil {
		t.Fatalf("run(v26) error = %v", err)
	}
	if report.FromVersion != 24 || report.TargetVersion != 29 ||
		!equalInts(report.AppliedVersions, []int{25, 26, 27, 28, 29}) || backupVersions != [2]int{24, 29} {
		t.Fatalf("run(v27) report = %#v backup=%v", report, backupVersions)
	}

	readback, err := NewRepository(database).CursorSnapshot(context.Background())
	if err != nil {
		t.Fatalf("CursorSnapshot() error = %v", err)
	}
	if len(readback.Sessions) != 1 || readback.Sessions[0].ExternalSessionID != "session-a" ||
		readback.Sessions[0].DisplayTitle != "未命名会话" || readback.Sessions[0].TitleSource != "fallback" {
		t.Fatalf("migrated cursor sessions = %#v", readback.Sessions)
	}
}
