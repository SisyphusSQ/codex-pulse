package store

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"golang.org/x/sys/unix"

	storesqlite "github.com/SisyphusSQ/codex-pulse/internal/store/sqlite"
)

const migrationBackupMinimumHeadroom int64 = 1 << 20

func migrationBackupRequiredBytes(sourcePath string) (int64, error) {
	databaseInfo, err := os.Stat(sourcePath)
	if err != nil {
		return 0, fmt.Errorf("inspect migration database size: %w", err)
	}
	required := databaseInfo.Size()
	if walInfo, err := os.Stat(sourcePath + "-wal"); err == nil {
		required += walInfo.Size()
	} else if !errors.Is(err, os.ErrNotExist) {
		return 0, fmt.Errorf("inspect migration WAL size: %w", err)
	}
	headroom := required / 10
	if headroom < migrationBackupMinimumHeadroom {
		headroom = migrationBackupMinimumHeadroom
	}
	return required + headroom, nil
}

func ensureMigrationBackupSpace(ctx context.Context, sourcePath string, requiredBytes int64) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	var statistics unix.Statfs_t
	if err := unix.Statfs(filepath.Dir(sourcePath), &statistics); err != nil {
		return fmt.Errorf("inspect migration backup filesystem: %w", err)
	}
	availableBytes := int64(statistics.Bavail) * int64(statistics.Bsize)
	if availableBytes < requiredBytes {
		return fmt.Errorf(
			"%w: migration backup requires %d bytes, only %d available",
			storesqlite.ErrDiskFull, requiredBytes, availableBytes,
		)
	}
	return nil
}

func defaultMigrationBackup(
	ctx context.Context,
	database *storesqlite.Store,
	targetVersion int,
	now time.Time,
	observe func(storesqlite.BackupProgress),
) (string, error) {
	backupPath := filepath.Join(
		filepath.Dir(database.Config().Path),
		"backups",
		fmt.Sprintf(
			"codex-pulse-before-v%d-%s.db",
			targetVersion,
			now.UTC().Format("20060102T150405.000Z"),
		),
	)
	report, err := database.Backup(ctx, storesqlite.BackupOptions{
		Destination: backupPath,
		Observe:     observe,
	})
	if err != nil {
		return "", err
	}
	return report.Path, nil
}
