package sqlite

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"gorm.io/gorm"
	modernsqlite "modernc.org/sqlite"
)

const defaultBackupPagesPerStep int32 = 128

type moderncBackuper interface {
	NewBackup(string) (*modernsqlite.Backup, error)
}

// BackupProgress 是不包含 SQL、路径或 driver 原文的稳定页级进度。
type BackupProgress struct {
	CopiedPages    int
	RemainingPages int
	TotalPages     int
}

// BackupOptions 控制一个在线备份；Destination 必须是尚不存在的文件。
type BackupOptions struct {
	Destination  string
	PagesPerStep int32
	Observe      func(BackupProgress)
}

// BackupReport 只在目标文件完整发布后返回。
type BackupReport struct {
	Path        string
	CopiedPages int
	TotalPages  int
}

// Backup 使用 modernc SQLite Backup API 复制包含已提交 WAL 页的完整快照。
func (store *Store) Backup(ctx context.Context, options BackupOptions) (BackupReport, error) {
	if store == nil {
		return BackupReport{}, newClassifiedError("backup", ErrInvalidConfig, fmt.Errorf("store is nil"))
	}
	if err := ctx.Err(); err != nil {
		return BackupReport{}, classifyError("backup", err)
	}
	destination, err := validateBackupDestination(store.config.Path, options.Destination)
	if err != nil {
		return BackupReport{}, err
	}
	pagesPerStep := options.PagesPerStep
	if pagesPerStep == 0 {
		pagesPerStep = defaultBackupPagesPerStep
	}
	if pagesPerStep < 0 {
		return BackupReport{}, newClassifiedError(
			"backup", ErrInvalidConfig, fmt.Errorf("pages per step must be positive"),
		)
	}
	if err := prepareBackupDirectory(filepath.Dir(destination)); err != nil {
		return BackupReport{}, err
	}
	temporaryFile, err := os.CreateTemp(
		filepath.Dir(destination), "."+filepath.Base(destination)+".partial-*",
	)
	if err != nil {
		return BackupReport{}, classifyError("create partial backup", err)
	}
	temporaryPath := temporaryFile.Name()
	if closeErr := temporaryFile.Close(); closeErr != nil {
		cleanupBackupArtifacts(temporaryPath)
		return BackupReport{}, classifyError("close partial backup", closeErr)
	}
	published := false
	defer func() {
		if !published {
			cleanupBackupArtifacts(temporaryPath)
		}
	}()

	var progress BackupProgress
	err = store.View(ctx, func(ctx context.Context, reader *gorm.DB) error {
		database, err := reader.DB()
		if err != nil {
			return err
		}
		connection, err := database.Conn(ctx)
		if err != nil {
			return err
		}
		defer connection.Close()
		return connection.Raw(func(driverConnection any) error {
			backuper, ok := driverConnection.(moderncBackuper)
			if !ok {
				return fmt.Errorf("modernc connection does not expose Backup API")
			}
			backup, err := backuper.NewBackup(temporaryPath)
			if err != nil {
				return err
			}
			return stepBackup(ctx, backup, pagesPerStep, options.Observe, &progress)
		})
	})
	if err != nil {
		return BackupReport{}, classifyError("backup snapshot", err)
	}
	if err := secureAndSyncBackup(temporaryPath); err != nil {
		return BackupReport{}, err
	}
	if err := publishPrivateFile(temporaryPath, destination, "publish backup"); err != nil {
		return BackupReport{}, err
	}
	published = true
	return BackupReport{
		Path: destination, CopiedPages: progress.CopiedPages, TotalPages: progress.TotalPages,
	}, nil
}

func stepBackup(
	ctx context.Context,
	backup *modernsqlite.Backup,
	pagesPerStep int32,
	observe func(BackupProgress),
	progress *BackupProgress,
) (returnErr error) {
	defer func() {
		returnErr = errors.Join(returnErr, backup.Finish())
	}()
	more := true
	maximumTotal := 0
	for more {
		if err := ctx.Err(); err != nil {
			return err
		}
		var err error
		more, err = backup.Step(pagesPerStep)
		if err != nil {
			return err
		}
		total := backup.PageCount()
		remaining := backup.Remaining()
		if total > maximumTotal {
			maximumTotal = total
		}
		if !more {
			total = maximumTotal
			remaining = 0
		}
		*progress = BackupProgress{
			CopiedPages: total - remaining, RemainingPages: remaining, TotalPages: total,
		}
		if observe != nil {
			observe(*progress)
		}
		if err := ctx.Err(); err != nil {
			return err
		}
	}
	return nil
}

func validateBackupDestination(source, destination string) (string, error) {
	if destination == "" {
		return "", newClassifiedError("backup", ErrInvalidPath, fmt.Errorf("destination is empty"))
	}
	absolute, err := filepath.Abs(destination)
	if err != nil {
		return "", newClassifiedError("backup", ErrInvalidPath, err)
	}
	absolute = filepath.Clean(absolute)
	if absolute == filepath.Clean(source) {
		return "", newClassifiedError("backup", ErrInvalidPath, fmt.Errorf("destination equals source"))
	}
	if _, err := os.Lstat(absolute); err == nil {
		return "", newClassifiedError("backup", ErrInvalidPath, fmt.Errorf("destination already exists"))
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", classifyError("inspect backup destination", err)
	}
	return absolute, nil
}

func prepareBackupDirectory(path string) error {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		if err := os.MkdirAll(path, 0o700); err != nil {
			return classifyError("create backup directory", err)
		}
		info, err = os.Lstat(path)
	}
	if err != nil {
		return classifyError("inspect backup directory", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return newClassifiedError("backup", ErrInvalidPath, fmt.Errorf("backup directory is not a real directory"))
	}
	if info.Mode().Perm() != 0o700 {
		return newClassifiedError(
			"backup", ErrPermission,
			fmt.Errorf("backup directory mode is %#o, want 0700", info.Mode().Perm()),
		)
	}
	return nil
}

func secureAndSyncBackup(path string) error {
	file, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		return classifyError("open completed backup", err)
	}
	defer file.Close()
	if err := file.Chmod(0o600); err != nil {
		return classifyError("secure completed backup", err)
	}
	if err := file.Sync(); err != nil {
		return classifyError("sync completed backup", err)
	}
	return nil
}

func publishPrivateFile(temporaryPath, destination, operation string) error {
	return publishPrivateFileWithSync(temporaryPath, destination, operation, syncDirectory)
}

func publishPrivateFileWithSync(
	temporaryPath,
	destination,
	operation string,
	syncParent func(string) error,
) error {
	// 临时文件与目标位于同一私有目录。Link 提供原子 no-clobber 语义，
	// 避免 Lstat 后 Rename 覆盖同用户并发创建的恢复点。目录在新增目标名和
	// 删除临时名后分别落盘，确保成功报告对应一个断电后仍可恢复的文件。
	if err := os.Link(temporaryPath, destination); err != nil {
		if errors.Is(err, os.ErrExist) {
			return newClassifiedError(operation, ErrInvalidPath, err)
		}
		return classifyError(operation, err)
	}
	parent := filepath.Dir(destination)
	if err := syncParent(parent); err != nil {
		cleanupErr := removePublishedPrivateFile(destination, parent, syncParent)
		return classifyError(operation, errors.Join(err, cleanupErr))
	}
	if err := os.Remove(temporaryPath); err != nil {
		cleanupErr := removePublishedPrivateFile(destination, parent, syncParent)
		return classifyError(operation, errors.Join(err, cleanupErr))
	}
	if err := syncParent(parent); err != nil {
		cleanupErr := removePublishedPrivateFile(destination, parent, syncParent)
		return classifyError(operation, errors.Join(err, cleanupErr))
	}
	return nil
}

func removePublishedPrivateFile(destination, parent string, syncParent func(string) error) error {
	removeErr := os.Remove(destination)
	if errors.Is(removeErr, os.ErrNotExist) {
		removeErr = nil
	}
	return errors.Join(removeErr, syncParent(parent))
}

func syncDirectory(path string) error {
	directory, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open directory for sync: %w", err)
	}
	syncErr := directory.Sync()
	closeErr := directory.Close()
	return errors.Join(syncErr, closeErr)
}

func cleanupBackupArtifacts(path string) {
	for _, candidate := range []string{path, path + "-journal", path + "-wal", path + "-shm"} {
		_ = os.Remove(candidate)
	}
}
