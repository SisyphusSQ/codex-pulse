package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"

	modernsqlite "modernc.org/sqlite"
)

type moderncRestorer interface {
	NewRestore(string) (*modernsqlite.Backup, error)
}

// RestoreOptions 控制从完整 SQLite backup 恢复到一个尚不存在的新文件。
type RestoreOptions struct {
	Source       string
	Destination  string
	PagesPerStep int32
	Observe      func(BackupProgress)
}

// Restore 使用 modernc NewRestore API 构建新数据库；不会覆盖运行中的 Store。
func Restore(ctx context.Context, options RestoreOptions) (BackupReport, error) {
	if err := ctx.Err(); err != nil {
		return BackupReport{}, classifyError("restore", err)
	}
	source, err := validateRestoreSource(options.Source)
	if err != nil {
		return BackupReport{}, err
	}
	destination, err := validateBackupDestination(source, options.Destination)
	if err != nil {
		return BackupReport{}, err
	}
	pagesPerStep := options.PagesPerStep
	if pagesPerStep == 0 {
		pagesPerStep = defaultBackupPagesPerStep
	}
	if pagesPerStep < 0 {
		return BackupReport{}, newClassifiedError(
			"restore", ErrInvalidConfig, fmt.Errorf("pages per step must be positive"),
		)
	}
	if err := prepareBackupDirectory(filepath.Dir(destination)); err != nil {
		return BackupReport{}, err
	}
	temporaryFile, err := os.CreateTemp(
		filepath.Dir(destination), "."+filepath.Base(destination)+".partial-*",
	)
	if err != nil {
		return BackupReport{}, classifyError("create partial restore", err)
	}
	temporaryPath := temporaryFile.Name()
	if closeErr := temporaryFile.Close(); closeErr != nil {
		cleanupBackupArtifacts(temporaryPath)
		return BackupReport{}, classifyError("close partial restore", closeErr)
	}
	published := false
	defer func() {
		if !published {
			cleanupBackupArtifacts(temporaryPath)
		}
	}()

	var progress BackupProgress
	if err := restoreIntoTemporaryFile(
		ctx, source, temporaryPath, pagesPerStep, options.Observe, &progress,
	); err != nil {
		return BackupReport{}, classifyError("restore snapshot", err)
	}
	if err := secureAndSyncBackup(temporaryPath); err != nil {
		return BackupReport{}, err
	}
	if err := publishPrivateFile(temporaryPath, destination, "publish restore"); err != nil {
		return BackupReport{}, err
	}
	published = true
	return BackupReport{
		Path: destination, CopiedPages: progress.CopiedPages, TotalPages: progress.TotalPages,
	}, nil
}

func restoreIntoTemporaryFile(
	ctx context.Context,
	source string,
	destination string,
	pagesPerStep int32,
	observe func(BackupProgress),
	progress *BackupProgress,
) error {
	database, err := sql.Open(driverName, destination)
	if err != nil {
		return err
	}
	database.SetMaxOpenConns(1)
	defer database.Close()
	connection, err := database.Conn(ctx)
	if err != nil {
		return err
	}
	defer connection.Close()
	return connection.Raw(func(driverConnection any) error {
		restorer, ok := driverConnection.(moderncRestorer)
		if !ok {
			return fmt.Errorf("modernc connection does not expose Restore API")
		}
		restore, err := restorer.NewRestore(source)
		if err != nil {
			return err
		}
		return stepBackup(ctx, restore, pagesPerStep, observe, progress)
	})
}

func validateRestoreSource(source string) (string, error) {
	if source == "" {
		return "", newClassifiedError("restore", ErrInvalidPath, fmt.Errorf("source is empty"))
	}
	absolute, err := filepath.Abs(source)
	if err != nil {
		return "", newClassifiedError("restore", ErrInvalidPath, err)
	}
	absolute = filepath.Clean(absolute)
	info, err := os.Lstat(absolute)
	if err != nil {
		return "", classifyError("inspect restore source", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return "", newClassifiedError("restore", ErrInvalidPath, fmt.Errorf("source is not a regular file"))
	}
	return absolute, nil
}
