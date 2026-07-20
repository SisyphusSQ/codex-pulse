package app

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	factstore "github.com/SisyphusSQ/codex-pulse/internal/store"
	storesqlite "github.com/SisyphusSQ/codex-pulse/internal/store/sqlite"
)

func TestMigrationRecoveryServiceExposesOnlyRecoverySnapshot(t *testing.T) {
	config := migrationRecoveryTestConfig(t)
	controller := migrationRecoveryTestController(t, config)
	service, err := newMigrationRecoveryService(controller)
	if err != nil {
		t.Fatalf("newMigrationRecoveryService() error = %v", err)
	}

	snapshot, err := service.State(context.Background())
	if err != nil || snapshot.Code != string(factstore.MigrationCodeApplyFailed) {
		t.Fatalf("State() error = %v", err)
	}
	encoded, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatalf("marshal Bootstrap: %v", err)
	}
	if strings.Contains(string(encoded), config.Path) || strings.Contains(string(encoded), "synthetic private cause") {
		t.Fatalf("recovery contract leaked path/cause: %s", encoded)
	}
}

func TestMigrationRecoveryRetryRequiresRestartAndAuditsContentFreeResult(t *testing.T) {
	config := migrationRecoveryTestConfig(t)
	if failure, err := runMigrationStartupGate(context.Background(), config); err != nil {
		t.Fatalf("seed current database failure=%#v error=%v", failure, err)
	}
	controller := migrationRecoveryTestController(t, config)
	controller.now = func() time.Time { return time.UnixMilli(1_784_100_000_000) }

	receipt, err := controller.Retry(context.Background())
	if err != nil || !receipt.RestartRequired || receipt.Phase != MigrationRecoveryRestartRequired {
		t.Fatalf("Retry() = %#v, %v", receipt, err)
	}
	if _, err := controller.Retry(context.Background()); !errors.Is(err, ErrMigrationRecoveryBusy) {
		t.Fatalf("second Retry() error = %v, want busy", err)
	}
	audit, err := os.ReadFile(controller.auditPath)
	if err != nil {
		t.Fatalf("read audit: %v", err)
	}
	if !strings.Contains(string(audit), `"action":"retry"`) || strings.Contains(string(audit), config.Path) || strings.Contains(string(audit), "synthetic private cause") {
		t.Fatalf("audit = %s", audit)
	}
}

func TestMigrationRecoveryRestorePreservesFailedDatabaseAndConsumesConfirmation(t *testing.T) {
	config := migrationRecoveryTestConfig(t)
	if failure, err := runMigrationStartupGate(context.Background(), config); err != nil {
		t.Fatalf("seed database failure=%#v error=%v", failure, err)
	}
	database, err := storesqlite.Open(context.Background(), config)
	if err != nil {
		t.Fatalf("Open(seed) error = %v", err)
	}
	backupPath := filepath.Join(filepath.Dir(config.Path), "backups", "known-good.db")
	if _, err := database.Backup(context.Background(), storesqlite.BackupOptions{Destination: backupPath}); err != nil {
		t.Fatalf("Backup() error = %v", err)
	}
	if err := database.Close(context.Background()); err != nil {
		t.Fatalf("Close(seed) error = %v", err)
	}

	corrupt, err := storesqlite.Open(context.Background(), config)
	if err != nil {
		t.Fatalf("Open(corrupt) error = %v", err)
	}
	if err := corrupt.Write(context.Background(), func(ctx context.Context, tx storesqlite.WriteTx) error {
		return tx.WithContext(ctx).Exec("PRAGMA user_version = 99").Error
	}); err != nil {
		t.Fatalf("set newer version: %v", err)
	}
	if err := corrupt.Close(context.Background()); err != nil {
		t.Fatalf("Close(corrupt) error = %v", err)
	}

	controller := migrationRecoveryTestController(t, config)
	controller.now = func() time.Time { return time.Date(2026, 7, 18, 11, 0, 0, 0, time.UTC) }
	confirmation, err := controller.PrepareRestore(context.Background(), "known-good.db")
	if err != nil || confirmation.Token == "" {
		t.Fatalf("PrepareRestore() = %#v, %v", confirmation, err)
	}
	if _, err := controller.ConfirmRestore(context.Background(), "wrong-token"); !errors.Is(err, ErrMigrationRestoreConfirmation) {
		t.Fatalf("wrong ConfirmRestore() error = %v", err)
	}
	receipt, err := controller.ConfirmRestore(context.Background(), confirmation.Token)
	if err != nil || !receipt.RestartRequired {
		t.Fatalf("ConfirmRestore() = %#v, %v", receipt, err)
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(config.Path), "backups", "codex-pulse-failed-20260718T110000.000Z.db")); err != nil {
		t.Fatalf("preserved failed database: %v", err)
	}
	if failure, err := runMigrationStartupGate(context.Background(), config); err != nil {
		t.Fatalf("restored database failure=%#v error=%v", failure, err)
	}
	if _, err := controller.ConfirmRestore(context.Background(), confirmation.Token); !errors.Is(err, ErrMigrationRestoreConfirmation) {
		t.Fatalf("reused confirmation error = %v", err)
	}
}

func TestMigrationRecoveryRejectsBackupChangedAfterConfirmation(t *testing.T) {
	config := migrationRecoveryTestConfig(t)
	backupDirectory := filepath.Join(filepath.Dir(config.Path), "backups")
	if err := os.Mkdir(backupDirectory, 0o700); err != nil {
		t.Fatalf("mkdir backups: %v", err)
	}
	backupPath := filepath.Join(backupDirectory, "changed.db")
	if err := os.WriteFile(backupPath, []byte("frozen"), 0o600); err != nil {
		t.Fatalf("write backup: %v", err)
	}
	controller := migrationRecoveryTestController(t, config)
	confirmation, err := controller.PrepareRestore(context.Background(), "changed.db")
	if err != nil {
		t.Fatalf("PrepareRestore() error = %v", err)
	}
	originalInfo, err := os.Stat(backupPath)
	if err != nil {
		t.Fatalf("stat backup: %v", err)
	}
	if err := os.WriteFile(backupPath, []byte("mutate"), 0o600); err != nil {
		t.Fatalf("replace backup: %v", err)
	}
	if err := os.Chtimes(backupPath, originalInfo.ModTime(), originalInfo.ModTime()); err != nil {
		t.Fatalf("restore backup timestamp: %v", err)
	}

	if _, err := controller.ConfirmRestore(context.Background(), confirmation.Token); !errors.Is(err, ErrMigrationRestoreConfirmation) {
		t.Fatalf("ConfirmRestore(changed) error = %v", err)
	}
	if controller.Snapshot().Phase != MigrationRecoveryFailed {
		t.Fatalf("phase = %q, want failed", controller.Snapshot().Phase)
	}
}

func TestMigrationRecoveryListsOnlyPrivateBackupDirectoryAndFiles(t *testing.T) {
	config := migrationRecoveryTestConfig(t)
	backupDirectory := filepath.Join(filepath.Dir(config.Path), "backups")
	if err := os.Mkdir(backupDirectory, 0o755); err != nil {
		t.Fatalf("mkdir backups: %v", err)
	}
	if err := os.WriteFile(filepath.Join(backupDirectory, "visible.db"), []byte("private"), 0o600); err != nil {
		t.Fatalf("write backup: %v", err)
	}
	controller := migrationRecoveryTestController(t, config)
	if got := controller.Snapshot().Backups; len(got) != 0 {
		t.Fatalf("backups with public directory = %#v", got)
	}
	if err := os.Chmod(backupDirectory, 0o700); err != nil {
		t.Fatalf("chmod backups: %v", err)
	}
	if err := os.Chmod(filepath.Join(backupDirectory, "visible.db"), 0o644); err != nil {
		t.Fatalf("chmod backup: %v", err)
	}
	if got := controller.Snapshot().Backups; len(got) != 0 {
		t.Fatalf("public backup file = %#v", got)
	}
}

func TestMigrationRecoveryAtomicSwitchRollsBackBeforeCommit(t *testing.T) {
	directory := t.TempDir()
	canonical := filepath.Join(directory, "canonical.db")
	ready := filepath.Join(directory, "ready.db")
	if err := os.WriteFile(canonical, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(ready, []byte("new"), 0o600); err != nil {
		t.Fatal(err)
	}
	controller := migrationRecoveryTestController(t, storesqlite.Config{Path: canonical})
	syncCalls := 0
	controller.syncDir = func(string) error {
		syncCalls++
		if syncCalls == 1 {
			return errors.New("injected sync failure")
		}
		return nil
	}
	committed, err := controller.replaceSQLiteDatabase(canonical, ready)
	if err == nil || committed {
		t.Fatalf("replaceSQLiteDatabase() = committed %v, error %v", committed, err)
	}
	content, readErr := os.ReadFile(canonical)
	if readErr != nil || string(content) != "old" {
		t.Fatalf("canonical after rollback = %q, %v", content, readErr)
	}
}

func TestMigrationRecoveryCancellationPreservesCanonical(t *testing.T) {
	config := migrationRecoveryTestConfig(t)
	if err := os.WriteFile(config.Path, []byte("canonical"), 0o600); err != nil {
		t.Fatal(err)
	}
	backupDirectory := filepath.Join(filepath.Dir(config.Path), "backups")
	if err := os.Mkdir(backupDirectory, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(backupDirectory, "cancel.db"), []byte("not sqlite"), 0o600); err != nil {
		t.Fatal(err)
	}
	controller := migrationRecoveryTestController(t, config)
	confirmation, err := controller.PrepareRestore(context.Background(), "cancel.db")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := controller.ConfirmRestore(ctx, confirmation.Token); err == nil {
		t.Fatal("ConfirmRestore(cancelled) error = nil")
	}
	content, err := os.ReadFile(config.Path)
	if err != nil || string(content) != "canonical" {
		t.Fatalf("canonical after cancellation = %q, %v", content, err)
	}
}

func TestMigrationRecoveryRestoreVerificationFailurePreservesCanonical(t *testing.T) {
	config := migrationRecoveryTestConfig(t)
	if err := os.WriteFile(config.Path, []byte("canonical"), 0o600); err != nil {
		t.Fatal(err)
	}
	backupDirectory := filepath.Join(filepath.Dir(config.Path), "backups")
	if err := os.Mkdir(backupDirectory, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(backupDirectory, "invalid.db"), []byte("not sqlite"), 0o600); err != nil {
		t.Fatal(err)
	}
	controller := migrationRecoveryTestController(t, config)
	confirmation, err := controller.PrepareRestore(context.Background(), "invalid.db")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := controller.ConfirmRestore(context.Background(), confirmation.Token); err == nil {
		t.Fatal("ConfirmRestore(invalid SQLite) error = nil")
	}
	content, err := os.ReadFile(config.Path)
	if err != nil || string(content) != "canonical" {
		t.Fatalf("canonical after verification failure = %q, %v", content, err)
	}
	if controller.Snapshot().Phase != MigrationRecoveryFailed {
		t.Fatalf("phase = %q, want failed", controller.Snapshot().Phase)
	}
}

func TestMigrationRecoveryReadyVerifyFailurePropagatesTypedFailure(t *testing.T) {
	config := migrationRecoveryTestConfig(t)
	if failure, err := runMigrationStartupGate(context.Background(), config); err != nil {
		t.Fatalf("seed database failure=%#v error=%v", failure, err)
	}
	database, err := storesqlite.Open(context.Background(), config)
	if err != nil {
		t.Fatal(err)
	}
	backupPath := filepath.Join(filepath.Dir(config.Path), "backups", "verify.db")
	if _, err := database.Backup(context.Background(), storesqlite.BackupOptions{Destination: backupPath}); err != nil {
		t.Fatal(err)
	}
	if err := database.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(config.Path)
	if err != nil {
		t.Fatal(err)
	}
	controller := migrationRecoveryTestController(t, config)
	injected := &factstore.MigrationFailure{
		Stage: factstore.MigrationStageVerify, Code: factstore.MigrationCodeVerifyFailed,
		CurrentVersion: 14, TargetVersion: 14, FailedVersion: 14,
	}
	controller.verifyReady = func(context.Context, storesqlite.Config) (*factstore.MigrationFailure, error) {
		return injected, errors.New("injected ready verify failure")
	}
	confirmation, err := controller.PrepareRestore(context.Background(), "verify.db")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := controller.ConfirmRestore(context.Background(), confirmation.Token); err == nil {
		t.Fatal("ConfirmRestore(verify failure) error = nil")
	}
	after, err := os.ReadFile(config.Path)
	if err != nil || string(after) != string(before) {
		t.Fatalf("canonical changed before atomic switch: equal=%v error=%v", string(after) == string(before), err)
	}
	snapshot := controller.Snapshot()
	if snapshot.Phase != MigrationRecoveryFailed || snapshot.Stage != factstore.MigrationStageVerify || snapshot.Code != factstore.MigrationCodeVerifyFailed {
		t.Fatalf("snapshot = %#v", snapshot)
	}
}

func TestMigrationRecoveryPrepareCancellationDoesNotBlockState(t *testing.T) {
	config := migrationRecoveryTestConfig(t)
	backupDirectory := filepath.Join(filepath.Dir(config.Path), "backups")
	if err := os.Mkdir(backupDirectory, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(backupDirectory, "large.db"), []byte("fixture"), 0o600); err != nil {
		t.Fatal(err)
	}
	controller := migrationRecoveryTestController(t, config)
	started := make(chan struct{})
	controller.hashBackup = func(ctx context.Context, _ string) ([sha256.Size]byte, MigrationBackupInfo, error) {
		close(started)
		<-ctx.Done()
		return [sha256.Size]byte{}, MigrationBackupInfo{}, ctx.Err()
	}
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() {
		_, err := controller.PrepareRestore(ctx, "large.db")
		result <- err
	}()
	<-started
	if _, err := controller.Retry(context.Background()); !errors.Is(err, ErrMigrationRecoveryBusy) {
		t.Fatalf("Retry during PrepareRestore error = %v, want busy", err)
	}
	snapshotResult := make(chan MigrationRecoverySnapshot, 1)
	go func() { snapshotResult <- controller.Snapshot() }()
	select {
	case snapshot := <-snapshotResult:
		if snapshot.Phase != MigrationRecoveryRunning {
			t.Fatalf("phase during cancellable hash = %q", snapshot.Phase)
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("Snapshot blocked behind backup hashing")
	}
	cancel()
	if err := <-result; !errors.Is(err, context.Canceled) {
		t.Fatalf("PrepareRestore(cancelled) error = %v", err)
	}
	if controller.Snapshot().Phase != MigrationRecoveryFailed {
		t.Fatalf("phase after cancellation = %q", controller.Snapshot().Phase)
	}
	audit, err := os.ReadFile(controller.auditPath)
	if err != nil || !strings.Contains(string(audit), `"action":"restore_prepare","result":"cancelled"`) {
		t.Fatalf("cancellation audit = %q, %v", audit, err)
	}
}

func TestMigrationRecoveryPrepareRandomFailureReturnsToFailedAndAudits(t *testing.T) {
	config := migrationRecoveryTestConfig(t)
	backupDirectory := filepath.Join(filepath.Dir(config.Path), "backups")
	if err := os.Mkdir(backupDirectory, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(backupDirectory, "random.db"), []byte("fixture"), 0o600); err != nil {
		t.Fatal(err)
	}
	controller := migrationRecoveryTestController(t, config)
	controller.randomSource = strings.NewReader("")
	if _, err := controller.PrepareRestore(context.Background(), "random.db"); !errors.Is(err, ErrMigrationRestoreConfirmation) {
		t.Fatalf("PrepareRestore(random failure) error = %v", err)
	}
	if controller.Snapshot().Phase != MigrationRecoveryFailed {
		t.Fatalf("phase = %q, want failed", controller.Snapshot().Phase)
	}
	audit, err := os.ReadFile(controller.auditPath)
	if err != nil || !strings.Contains(string(audit), `"action":"restore_prepare","result":"failed"`) {
		t.Fatalf("audit = %q, %v", audit, err)
	}
}

func TestMigrationRecoveryReportsCompletionAuditWarning(t *testing.T) {
	config := migrationRecoveryTestConfig(t)
	if failure, err := runMigrationStartupGate(context.Background(), config); err != nil {
		t.Fatalf("seed database failure=%#v error=%v", failure, err)
	}
	controller := migrationRecoveryTestController(t, config)
	controller.auditAppender = func(_, result, _ string) error {
		if result == "succeeded" {
			return errors.New("injected completion audit failure")
		}
		return nil
	}
	receipt, err := controller.Retry(context.Background())
	if err != nil || !receipt.RestartRequired || !receipt.AuditWarning {
		t.Fatalf("Retry() = %#v, %v", receipt, err)
	}
}

func TestMigrationRecoverySwitchRollbackFailureReportsCommitted(t *testing.T) {
	directory := t.TempDir()
	canonical := filepath.Join(directory, "canonical.db")
	ready := filepath.Join(directory, "ready.db")
	if err := os.WriteFile(canonical, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(ready, []byte("new"), 0o600); err != nil {
		t.Fatal(err)
	}
	controller := migrationRecoveryTestController(t, storesqlite.Config{Path: canonical})
	swapCalls := 0
	controller.swapFiles = func(left, right string) error {
		swapCalls++
		if swapCalls == 2 {
			return errors.New("injected rollback failure")
		}
		return atomicSwapFiles(left, right)
	}
	controller.syncDir = func(string) error { return errors.New("injected sync failure") }
	committed, err := controller.replaceSQLiteDatabase(canonical, ready)
	if err == nil || !committed {
		t.Fatalf("replaceSQLiteDatabase() = committed %v, error %v", committed, err)
	}
	content, readErr := os.ReadFile(canonical)
	if readErr != nil || string(content) != "new" {
		t.Fatalf("canonical committed content = %q, %v", content, readErr)
	}
}

func TestMigrationRecoveryCompanionCleanupFailureReportsCommitted(t *testing.T) {
	directory := t.TempDir()
	canonical := filepath.Join(directory, "canonical.db")
	ready := filepath.Join(directory, "ready.db")
	if err := os.WriteFile(canonical, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(ready, []byte("new"), 0o600); err != nil {
		t.Fatal(err)
	}
	controller := migrationRecoveryTestController(t, storesqlite.Config{Path: canonical})
	controller.removeCompanions = func(string) error { return errors.New("injected cleanup failure") }
	committed, err := controller.replaceSQLiteDatabase(canonical, ready)
	if err == nil || !committed {
		t.Fatalf("replaceSQLiteDatabase() = committed %v, error %v", committed, err)
	}
}

func TestMigrationRecoveryIgnoresSymlinkBackupAndRejectsSymlinkAudit(t *testing.T) {
	config := migrationRecoveryTestConfig(t)
	backupDirectory := filepath.Join(filepath.Dir(config.Path), "backups")
	if err := os.Mkdir(backupDirectory, 0o700); err != nil {
		t.Fatalf("mkdir backups: %v", err)
	}
	target := filepath.Join(filepath.Dir(config.Path), "outside.db")
	if err := os.WriteFile(target, []byte("outside"), 0o600); err != nil {
		t.Fatalf("write target: %v", err)
	}
	if err := os.Symlink(target, filepath.Join(backupDirectory, "linked.db")); err != nil {
		t.Fatalf("symlink backup: %v", err)
	}
	controller := migrationRecoveryTestController(t, config)
	if backups := controller.Snapshot().Backups; len(backups) != 0 {
		t.Fatalf("symlink backups = %#v", backups)
	}

	outsideLogs := filepath.Join(t.TempDir(), "outside-logs")
	if err := os.Mkdir(outsideLogs, 0o700); err != nil {
		t.Fatalf("mkdir outside logs: %v", err)
	}
	if err := os.Symlink(outsideLogs, filepath.Dir(controller.auditPath)); err != nil {
		t.Fatalf("symlink logs: %v", err)
	}
	if err := controller.appendAuditLocked("retry", "failed", ""); !errors.Is(err, ErrMigrationRecoveryUnavailable) {
		t.Fatalf("appendAuditLocked(symlink) error = %v", err)
	}
	if _, err := controller.Retry(context.Background()); !errors.Is(err, ErrMigrationRecoveryUnavailable) {
		t.Fatalf("Retry(symlink audit) error = %v", err)
	}
	if controller.Snapshot().Phase != MigrationRecoveryFailed {
		t.Fatalf("phase after audit preflight failure = %q", controller.Snapshot().Phase)
	}
}

func migrationRecoveryTestConfig(t *testing.T) storesqlite.Config {
	t.Helper()
	directory := t.TempDir()
	if err := os.Chmod(directory, 0o700); err != nil {
		t.Fatalf("chmod temp directory: %v", err)
	}
	return storesqlite.Config{Path: filepath.Join(directory, "codex-pulse.db")}
}

func migrationRecoveryTestController(t *testing.T, config storesqlite.Config) *migrationRecoveryController {
	t.Helper()
	controller, err := newMigrationRecoveryController(config, &factstore.MigrationFailure{
		Stage: factstore.MigrationStageApply, Code: factstore.MigrationCodeApplyFailed,
		CurrentVersion: 13, TargetVersion: 14, FailedVersion: 14,
		Cause: errors.New("synthetic private cause"),
	})
	if err != nil {
		t.Fatalf("newMigrationRecoveryController() error = %v", err)
	}
	return controller
}
