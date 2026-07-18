package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gorm.io/gorm"
)

type backupRecord struct {
	ID      int64  `gorm:"column:id;primaryKey;autoIncrement"`
	Payload string `gorm:"column:payload;not null"`
}

func (backupRecord) TableName() string { return "backup_records" }

func TestBackupCopiesCommittedWALDataWithPrivatePermissionsAndProgress(t *testing.T) {
	store := openTestStore(t, Config{})
	seedBackupRecords(t, store, 256)

	destination := filepath.Join(filepath.Dir(store.Config().Path), "backups", "before-v1.db")
	var progress []BackupProgress
	report, err := store.Backup(context.Background(), BackupOptions{
		Destination:  destination,
		PagesPerStep: 1,
		Observe: func(update BackupProgress) {
			progress = append(progress, update)
		},
	})
	if err != nil {
		t.Fatalf("Backup() error = %v", err)
	}
	if report.Path != destination || report.TotalPages <= 0 || report.CopiedPages != report.TotalPages {
		t.Fatalf("Backup() report = %#v", report)
	}
	if len(progress) == 0 {
		t.Fatal("Backup() emitted no progress")
	}
	for index, update := range progress {
		if update.TotalPages <= 0 || update.CopiedPages < 0 || update.CopiedPages > update.TotalPages || update.RemainingPages < 0 {
			t.Fatalf("progress[%d] = %#v", index, update)
		}
		if index > 0 && update.CopiedPages < progress[index-1].CopiedPages {
			t.Fatalf("progress regressed: %#v then %#v", progress[index-1], update)
		}
	}
	if last := progress[len(progress)-1]; last.RemainingPages != 0 || last.CopiedPages != last.TotalPages {
		t.Fatalf("final progress = %#v", last)
	}

	directoryInfo, err := os.Stat(filepath.Dir(destination))
	if err != nil {
		t.Fatalf("stat backup directory: %v", err)
	}
	if got := directoryInfo.Mode().Perm(); got != 0o700 {
		t.Fatalf("backup directory mode = %#o, want 0700", got)
	}
	fileInfo, err := os.Stat(destination)
	if err != nil {
		t.Fatalf("stat backup: %v", err)
	}
	if got := fileInfo.Mode().Perm(); got != 0o600 {
		t.Fatalf("backup mode = %#o, want 0600", got)
	}

	backupDatabase, err := sql.Open(driverName, destination)
	if err != nil {
		t.Fatalf("open backup: %v", err)
	}
	defer backupDatabase.Close()
	var count int
	if err := backupDatabase.QueryRow(`SELECT count(*) FROM backup_records`).Scan(&count); err != nil {
		t.Fatalf("read backup: %v", err)
	}
	if count != 256 {
		t.Fatalf("backup row count = %d, want 256", count)
	}
}

func TestBackupCancellationRemovesPartialAndDoesNotPublishDestination(t *testing.T) {
	store := openTestStore(t, Config{})
	seedBackupRecords(t, store, 512)

	destination := filepath.Join(filepath.Dir(store.Config().Path), "backups", "canceled.db")
	ctx, cancel := context.WithCancel(context.Background())
	_, err := store.Backup(ctx, BackupOptions{
		Destination:  destination,
		PagesPerStep: 1,
		Observe: func(BackupProgress) {
			cancel()
		},
	})
	if !errors.Is(err, ErrCanceled) {
		t.Fatalf("Backup() error = %v, want ErrCanceled", err)
	}
	if _, err := os.Stat(destination); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("destination stat error = %v, want not exist", err)
	}
	matches, err := filepath.Glob(filepath.Join(filepath.Dir(destination), ".canceled.db.partial-*"))
	if err != nil {
		t.Fatalf("glob partial backups: %v", err)
	}
	if len(matches) != 0 {
		t.Fatalf("partial backups remain: %v", matches)
	}
}

func TestPublishPrivateFileSyncsDirectoryAfterPublishAndCleanup(t *testing.T) {
	directory := t.TempDir()
	temporaryPath := filepath.Join(directory, "partial.db")
	destination := filepath.Join(directory, "backup.db")
	if err := os.WriteFile(temporaryPath, []byte("backup"), 0o600); err != nil {
		t.Fatalf("write temporary file: %v", err)
	}

	var observations []string
	err := publishPrivateFileWithSync(
		temporaryPath,
		destination,
		"publish test backup",
		func(path string) error {
			if path != directory {
				t.Fatalf("sync path = %q, want %q", path, directory)
			}
			_, temporaryErr := os.Stat(temporaryPath)
			_, destinationErr := os.Stat(destination)
			observations = append(
				observations,
				fmt.Sprintf("temporary=%t,destination=%t", temporaryErr == nil, destinationErr == nil),
			)
			return nil
		},
	)
	if err != nil {
		t.Fatalf("publishPrivateFileWithSync() error = %v", err)
	}
	if got, want := strings.Join(observations, ";"),
		"temporary=true,destination=true;temporary=false,destination=true"; got != want {
		t.Fatalf("sync observations = %q, want %q", got, want)
	}
}

func TestPublishPrivateFileFailsClosedWhenDirectorySyncFails(t *testing.T) {
	tests := []struct {
		name       string
		failOnCall int
	}{
		{name: "publish entry", failOnCall: 1},
		{name: "temporary cleanup", failOnCall: 2},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			directory := t.TempDir()
			temporaryPath := filepath.Join(directory, "partial.db")
			destination := filepath.Join(directory, "backup.db")
			if err := os.WriteFile(temporaryPath, []byte("backup"), 0o600); err != nil {
				t.Fatalf("write temporary file: %v", err)
			}

			calls := 0
			errInjected := errors.New("injected directory sync failure")
			err := publishPrivateFileWithSync(
				temporaryPath,
				destination,
				"publish test backup",
				func(string) error {
					calls++
					if calls == test.failOnCall {
						return errInjected
					}
					return nil
				},
			)
			if !errors.Is(err, errInjected) {
				t.Fatalf("publishPrivateFileWithSync() error = %v, want injected failure", err)
			}
			if _, statErr := os.Stat(destination); !errors.Is(statErr, os.ErrNotExist) {
				t.Fatalf("destination stat error = %v, want not exist", statErr)
			}
		})
	}
}

func TestPublishPrivateFileDoesNotClobberConcurrentDestination(t *testing.T) {
	directory := t.TempDir()
	temporaryPath := filepath.Join(directory, "partial.db")
	destination := filepath.Join(directory, "backup.db")
	if err := os.WriteFile(temporaryPath, []byte("new"), 0o600); err != nil {
		t.Fatalf("write temporary file: %v", err)
	}
	if err := os.WriteFile(destination, []byte("existing"), 0o600); err != nil {
		t.Fatalf("write destination: %v", err)
	}

	err := publishPrivateFileWithSync(
		temporaryPath,
		destination,
		"publish test backup",
		func(string) error {
			t.Fatal("directory sync must not run when publication loses the no-clobber race")
			return nil
		},
	)
	if !errors.Is(err, ErrInvalidPath) {
		t.Fatalf("publishPrivateFileWithSync() error = %v, want ErrInvalidPath", err)
	}
	contents, readErr := os.ReadFile(destination)
	if readErr != nil {
		t.Fatalf("read destination: %v", readErr)
	}
	if got := string(contents); got != "existing" {
		t.Fatalf("destination contents = %q, want existing", got)
	}
}

func TestRestoreRebuildsPrivateDatabaseFromBackup(t *testing.T) {
	store := openTestStore(t, Config{})
	seedBackupRecords(t, store, 64)
	directory := filepath.Dir(store.Config().Path)
	backupPath := filepath.Join(directory, "backups", "restore-source.db")
	if _, err := store.Backup(context.Background(), BackupOptions{Destination: backupPath}); err != nil {
		t.Fatalf("Backup() error = %v", err)
	}

	restoredPath := filepath.Join(directory, "restored", "codex-pulse.db")
	report, err := Restore(context.Background(), RestoreOptions{
		Source: backupPath, Destination: restoredPath, PagesPerStep: 1,
	})
	if err != nil {
		t.Fatalf("Restore() error = %v", err)
	}
	if report.Path != restoredPath || report.CopiedPages != report.TotalPages || report.TotalPages <= 0 {
		t.Fatalf("Restore() report = %#v", report)
	}
	info, err := os.Stat(restoredPath)
	if err != nil {
		t.Fatalf("stat restored database: %v", err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("restored database mode = %#o, want 0600", got)
	}
	restoredDatabase, err := sql.Open(driverName, restoredPath)
	if err != nil {
		t.Fatalf("open restored database: %v", err)
	}
	defer restoredDatabase.Close()
	var count int
	if err := restoredDatabase.QueryRow(`SELECT count(*) FROM backup_records`).Scan(&count); err != nil {
		t.Fatalf("read restored database: %v", err)
	}
	if count != 64 {
		t.Fatalf("restored row count = %d, want 64", count)
	}
}

func TestVerifyBackupFileRejectsCorruptBytes(t *testing.T) {
	directory := t.TempDir()
	if err := os.Chmod(directory, 0o700); err != nil {
		t.Fatalf("secure directory: %v", err)
	}
	path := filepath.Join(directory, "corrupt.db")
	if err := os.WriteFile(path, []byte("not a sqlite database"), 0o600); err != nil {
		t.Fatalf("write corrupt backup: %v", err)
	}
	if err := verifyBackupFile(context.Background(), path); !errors.Is(err, ErrCorrupt) {
		t.Fatalf("verifyBackupFile() error = %v, want ErrCorrupt", err)
	}
}

func seedBackupRecords(t *testing.T, store *Store, count int) {
	t.Helper()
	err := store.Write(context.Background(), func(ctx context.Context, transaction *gorm.DB) error {
		if err := transaction.WithContext(ctx).Migrator().CreateTable(&backupRecord{}); err != nil {
			return err
		}
		records := make([]backupRecord, 0, count)
		for index := 0; index < count; index++ {
			records = append(records, backupRecord{Payload: strings.Repeat("x", 512)})
		}
		return transaction.WithContext(ctx).CreateInBatches(records, 64).Error
	})
	if err != nil {
		t.Fatalf("seed backup records: %v", err)
	}
}
