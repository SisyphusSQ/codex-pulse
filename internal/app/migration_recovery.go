package app

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"golang.org/x/sys/unix"

	factstore "github.com/SisyphusSQ/codex-pulse/internal/store"
	storesqlite "github.com/SisyphusSQ/codex-pulse/internal/store/sqlite"
)

const migrationRecoveryContractVersion = "migration-recovery-v1"

var (
	ErrMigrationRecoveryUnavailable = errors.New("migration recovery is unavailable")
	ErrMigrationRecoveryBusy        = errors.New("migration recovery is busy")
	ErrMigrationRestoreConfirmation = errors.New("migration restore confirmation is invalid")
)

type MigrationRecoveryPhase string

const (
	MigrationRecoveryFailed               MigrationRecoveryPhase = "failed"
	MigrationRecoveryRunning              MigrationRecoveryPhase = "running"
	MigrationRecoveryAwaitingConfirmation MigrationRecoveryPhase = "awaiting_confirmation"
	MigrationRecoveryRestartRequired      MigrationRecoveryPhase = "restart_required"
)

type MigrationBackupInfo struct {
	Name         string `json:"name"`
	SizeBytes    int64  `json:"sizeBytes"`
	ModifiedAtMS int64  `json:"modifiedAtMs"`
}

type MigrationRecoverySnapshot struct {
	Version        string                   `json:"version"`
	Phase          MigrationRecoveryPhase   `json:"phase"`
	Stage          factstore.MigrationStage `json:"stage"`
	Code           string                   `json:"code"`
	CurrentVersion int                      `json:"currentVersion"`
	TargetVersion  int                      `json:"targetVersion"`
	FailedVersion  int                      `json:"failedVersion"`
	CanRetry       bool                     `json:"canRetry"`
	CanExit        bool                     `json:"canExit"`
	Backups        []MigrationBackupInfo    `json:"backups"`
	AuditWarning   bool                     `json:"auditWarning"`
}

type MigrationRestoreConfirmation struct {
	Token  string              `json:"token"`
	Backup MigrationBackupInfo `json:"backup"`
}

type MigrationRecoveryReceipt struct {
	Phase           MigrationRecoveryPhase `json:"phase"`
	RestartRequired bool                   `json:"restartRequired"`
	AuditWarning    bool                   `json:"auditWarning"`
}

type migrationRestoreIntent struct {
	tokenHash  [sha256.Size]byte
	backupPath string
	backup     MigrationBackupInfo
	digest     [sha256.Size]byte
}

type migrationRecoveryController struct {
	mu               sync.Mutex
	config           storesqlite.Config
	phase            MigrationRecoveryPhase
	failure          *factstore.MigrationFailure
	intent           *migrationRestoreIntent
	exit             func()
	now              func() time.Time
	auditPath        string
	auditWarning     bool
	swapFiles        func(string, string) error
	syncDir          func(string) error
	hashBackup       func(context.Context, string) ([sha256.Size]byte, MigrationBackupInfo, error)
	freezeSource     func(context.Context, migrationRestoreIntent) (string, error)
	auditAppender    func(string, string, string) error
	removeCompanions func(string) error
	verifyReady      func(context.Context, storesqlite.Config) (*factstore.MigrationFailure, error)
	randomSource     io.Reader
}

func newMigrationRecoveryController(config storesqlite.Config, failure *factstore.MigrationFailure) (*migrationRecoveryController, error) {
	if config.Path == "" || failure == nil {
		return nil, ErrMigrationRecoveryUnavailable
	}
	directory := filepath.Dir(config.Path)
	controller := &migrationRecoveryController{
		config: config, phase: MigrationRecoveryFailed, failure: cloneMigrationFailure(failure), now: time.Now,
		auditPath:    filepath.Join(directory, "logs", "migration-recovery.jsonl"),
		swapFiles:    atomicSwapFiles,
		syncDir:      syncDirectory,
		hashBackup:   hashPrivateBackup,
		freezeSource: freezeRestoreSource,
	}
	controller.auditAppender = controller.appendAuditFileLocked
	controller.removeCompanions = removeSQLiteCompanions
	controller.verifyReady = runMigrationStartupGate
	controller.randomSource = rand.Reader
	return controller, nil
}

func cloneMigrationFailure(value *factstore.MigrationFailure) *factstore.MigrationFailure {
	if value == nil {
		return nil
	}
	copy := *value
	copy.Cause = nil
	return &copy
}

func (controller *migrationRecoveryController) Snapshot() MigrationRecoverySnapshot {
	controller.mu.Lock()
	defer controller.mu.Unlock()
	return controller.snapshotLocked()
}

func (controller *migrationRecoveryController) snapshotLocked() MigrationRecoverySnapshot {
	snapshot := MigrationRecoverySnapshot{
		Version: migrationRecoveryContractVersion, Phase: controller.phase, CanExit: true,
		AuditWarning: controller.auditWarning,
	}
	if controller.failure != nil {
		snapshot.Stage = controller.failure.Stage
		snapshot.Code = controller.failure.Code
		snapshot.CurrentVersion = controller.failure.CurrentVersion
		snapshot.TargetVersion = controller.failure.TargetVersion
		snapshot.FailedVersion = controller.failure.FailedVersion
	}
	snapshot.CanRetry = controller.phase == MigrationRecoveryFailed
	snapshot.Backups = controller.listBackupsLocked()
	return snapshot
}

func (controller *migrationRecoveryController) listBackupsLocked() []MigrationBackupInfo {
	directory := filepath.Join(filepath.Dir(controller.config.Path), "backups")
	directoryInfo, err := os.Lstat(directory)
	if err != nil || !directoryInfo.IsDir() || directoryInfo.Mode()&os.ModeSymlink != 0 || directoryInfo.Mode().Perm() != 0o700 {
		return []MigrationBackupInfo{}
	}
	entries, err := os.ReadDir(directory)
	if err != nil {
		return []MigrationBackupInfo{}
	}
	values := make([]MigrationBackupInfo, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || entry.Type()&os.ModeSymlink != 0 || filepath.Ext(entry.Name()) != ".db" || strings.Contains(entry.Name(), string(os.PathSeparator)) {
			continue
		}
		info, err := entry.Info()
		if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 {
			continue
		}
		values = append(values, MigrationBackupInfo{Name: entry.Name(), SizeBytes: info.Size(), ModifiedAtMS: info.ModTime().UnixMilli()})
	}
	sort.Slice(values, func(i, j int) bool {
		if values[i].ModifiedAtMS != values[j].ModifiedAtMS {
			return values[i].ModifiedAtMS > values[j].ModifiedAtMS
		}
		return values[i].Name < values[j].Name
	})
	return values
}

func (controller *migrationRecoveryController) bindExit(exit func()) error {
	if controller == nil || exit == nil {
		return ErrMigrationRecoveryUnavailable
	}
	controller.mu.Lock()
	defer controller.mu.Unlock()
	if controller.exit != nil {
		return ErrMigrationRecoveryUnavailable
	}
	controller.exit = exit
	return nil
}

func (controller *migrationRecoveryController) Retry(ctx context.Context) (MigrationRecoveryReceipt, error) {
	if controller == nil {
		return MigrationRecoveryReceipt{}, ErrMigrationRecoveryUnavailable
	}
	controller.mu.Lock()
	if controller.phase != MigrationRecoveryFailed {
		controller.mu.Unlock()
		return MigrationRecoveryReceipt{}, ErrMigrationRecoveryBusy
	}
	if err := controller.appendAuditLocked("retry", "started", ""); err != nil {
		controller.mu.Unlock()
		return MigrationRecoveryReceipt{}, err
	}
	controller.phase = MigrationRecoveryRunning
	controller.intent = nil
	controller.auditWarning = false
	controller.mu.Unlock()

	failure, err := runMigrationStartupGate(ctx, controller.config)
	controller.mu.Lock()
	defer controller.mu.Unlock()
	if err != nil {
		controller.phase = MigrationRecoveryFailed
		if failure != nil {
			controller.failure = failure
		}
		return MigrationRecoveryReceipt{}, errors.Join(err, controller.appendAuditLocked("retry", "failed", ""))
	}
	controller.phase = MigrationRecoveryRestartRequired
	controller.failure = nil
	auditErr := controller.appendAuditLocked("retry", "succeeded", "")
	controller.auditWarning = auditErr != nil
	return MigrationRecoveryReceipt{Phase: controller.phase, RestartRequired: true, AuditWarning: auditErr != nil}, nil
}

func (controller *migrationRecoveryController) PrepareRestore(ctx context.Context, backupName string) (MigrationRestoreConfirmation, error) {
	if controller == nil {
		return MigrationRestoreConfirmation{}, ErrMigrationRecoveryUnavailable
	}
	controller.mu.Lock()
	if controller.phase != MigrationRecoveryFailed {
		controller.mu.Unlock()
		return MigrationRestoreConfirmation{}, ErrMigrationRecoveryBusy
	}
	var selected *MigrationBackupInfo
	for _, backup := range controller.listBackupsLocked() {
		if backup.Name == backupName {
			value := backup
			selected = &value
			break
		}
	}
	if selected == nil {
		controller.mu.Unlock()
		return MigrationRestoreConfirmation{}, ErrMigrationRestoreConfirmation
	}
	controller.phase = MigrationRecoveryRunning
	controller.mu.Unlock()
	backupPath := filepath.Join(filepath.Dir(controller.config.Path), "backups", selected.Name)
	digest, actual, err := controller.hashBackup(ctx, backupPath)
	controller.mu.Lock()
	defer controller.mu.Unlock()
	if err != nil {
		controller.phase = MigrationRecoveryFailed
		result := "failed"
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			result = "cancelled"
		}
		auditErr := controller.appendAuditLocked("restore_prepare", result, selected.Name)
		return MigrationRestoreConfirmation{}, errors.Join(err, auditErr)
	}
	tokenBytes := make([]byte, 32)
	if _, err = io.ReadFull(controller.randomSource, tokenBytes); err != nil {
		controller.phase = MigrationRecoveryFailed
		auditErr := controller.appendAuditLocked("restore_prepare", "failed", selected.Name)
		return MigrationRestoreConfirmation{}, errors.Join(ErrMigrationRestoreConfirmation, auditErr)
	}
	token := hex.EncodeToString(tokenBytes)
	controller.intent = &migrationRestoreIntent{
		tokenHash:  sha256.Sum256([]byte(token)),
		backupPath: backupPath,
		backup:     actual,
		digest:     digest,
	}
	controller.phase = MigrationRecoveryAwaitingConfirmation
	if err := controller.appendAuditLocked("restore_prepare", "awaiting_confirmation", selected.Name); err != nil {
		controller.phase = MigrationRecoveryFailed
		controller.intent = nil
		return MigrationRestoreConfirmation{}, err
	}
	return MigrationRestoreConfirmation{Token: token, Backup: actual}, nil
}

func (controller *migrationRecoveryController) ConfirmRestore(ctx context.Context, token string) (MigrationRecoveryReceipt, error) {
	if controller == nil {
		return MigrationRecoveryReceipt{}, ErrMigrationRecoveryUnavailable
	}
	controller.mu.Lock()
	if controller.phase != MigrationRecoveryAwaitingConfirmation || controller.intent == nil ||
		sha256.Sum256([]byte(token)) != controller.intent.tokenHash {
		controller.mu.Unlock()
		return MigrationRecoveryReceipt{}, ErrMigrationRestoreConfirmation
	}
	intent := *controller.intent
	if err := controller.appendAuditLocked("restore_confirm", "started", intent.backup.Name); err != nil {
		controller.mu.Unlock()
		return MigrationRecoveryReceipt{}, err
	}
	controller.intent = nil
	controller.phase = MigrationRecoveryRunning
	controller.auditWarning = false
	controller.mu.Unlock()

	frozenPath, err := controller.freezeSource(ctx, intent)
	if err != nil {
		controller.mu.Lock()
		controller.intent = nil
		controller.phase = MigrationRecoveryFailed
		auditErr := controller.appendAuditLocked("restore_confirm", "failed", intent.backup.Name)
		controller.mu.Unlock()
		return MigrationRecoveryReceipt{}, errors.Join(err, auditErr)
	}
	intent.backupPath = frozenPath
	defer removeSQLiteFiles(frozenPath)

	failure, committed, err := controller.restore(ctx, intent)
	controller.mu.Lock()
	defer controller.mu.Unlock()
	if err != nil {
		if committed {
			controller.phase = MigrationRecoveryRestartRequired
			controller.failure = nil
			controller.auditWarning = true
			auditErr := controller.appendAuditLocked("restore_confirm", "committed_with_warning", intent.backup.Name)
			return MigrationRecoveryReceipt{Phase: controller.phase, RestartRequired: true, AuditWarning: true}, errors.Join(err, auditErr)
		}
		controller.phase = MigrationRecoveryFailed
		if failure != nil {
			controller.failure = failure
		}
		return MigrationRecoveryReceipt{}, errors.Join(err, controller.appendAuditLocked("restore_confirm", "failed", intent.backup.Name))
	}
	controller.phase = MigrationRecoveryRestartRequired
	controller.failure = nil
	auditErr := controller.appendAuditLocked("restore_confirm", "succeeded", intent.backup.Name)
	controller.auditWarning = auditErr != nil
	return MigrationRecoveryReceipt{Phase: controller.phase, RestartRequired: true, AuditWarning: auditErr != nil}, nil
}

func hashPrivateBackup(ctx context.Context, path string) ([sha256.Size]byte, MigrationBackupInfo, error) {
	var empty [sha256.Size]byte
	descriptor, err := unix.Open(path, unix.O_RDONLY|unix.O_NOFOLLOW, 0)
	if err != nil {
		return empty, MigrationBackupInfo{}, ErrMigrationRestoreConfirmation
	}
	file := os.NewFile(uintptr(descriptor), path)
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 {
		return empty, MigrationBackupInfo{}, ErrMigrationRestoreConfirmation
	}
	hasher := sha256.New()
	if _, err := copyWithContext(ctx, hasher, file); err != nil {
		return empty, MigrationBackupInfo{}, err
	}
	var digest [sha256.Size]byte
	copy(digest[:], hasher.Sum(nil))
	return digest, MigrationBackupInfo{Name: filepath.Base(path), SizeBytes: info.Size(), ModifiedAtMS: info.ModTime().UnixMilli()}, nil
}

func freezeRestoreSource(ctx context.Context, intent migrationRestoreIntent) (string, error) {
	descriptor, err := unix.Open(intent.backupPath, unix.O_RDONLY|unix.O_NOFOLLOW, 0)
	if err != nil {
		return "", ErrMigrationRestoreConfirmation
	}
	source := os.NewFile(uintptr(descriptor), intent.backupPath)
	defer source.Close()
	info, err := source.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 {
		return "", ErrMigrationRestoreConfirmation
	}
	frozenPath := filepath.Join(filepath.Dir(filepath.Dir(intent.backupPath)), ".migration-restore-source-"+hex.EncodeToString(intent.tokenHash[:8])+".db")
	target, err := os.OpenFile(frozenPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return "", err
	}
	hasher := sha256.New()
	_, copyErr := copyWithContext(ctx, io.MultiWriter(target, hasher), source)
	if copyErr != nil {
		closeErr := target.Close()
		_ = os.Remove(frozenPath)
		return "", errors.Join(copyErr, closeErr)
	}
	closeErr := errors.Join(target.Sync(), target.Close())
	if closeErr != nil {
		_ = os.Remove(frozenPath)
		return "", closeErr
	}
	var digest [sha256.Size]byte
	copy(digest[:], hasher.Sum(nil))
	if digest != intent.digest {
		_ = os.Remove(frozenPath)
		return "", ErrMigrationRestoreConfirmation
	}
	return frozenPath, nil
}

func copyWithContext(ctx context.Context, destination io.Writer, source io.Reader) (int64, error) {
	buffer := make([]byte, 1024*1024)
	var total int64
	for {
		if err := ctx.Err(); err != nil {
			return total, err
		}
		count, readErr := source.Read(buffer)
		if count > 0 {
			written, writeErr := destination.Write(buffer[:count])
			total += int64(written)
			if writeErr != nil {
				return total, writeErr
			}
			if written != count {
				return total, io.ErrShortWrite
			}
		}
		if errors.Is(readErr, io.EOF) {
			return total, nil
		}
		if readErr != nil {
			return total, readErr
		}
	}
}

func (controller *migrationRecoveryController) CancelRestore() error {
	if controller == nil {
		return ErrMigrationRecoveryUnavailable
	}
	controller.mu.Lock()
	defer controller.mu.Unlock()
	if controller.phase != MigrationRecoveryAwaitingConfirmation || controller.intent == nil {
		return ErrMigrationRestoreConfirmation
	}
	backupName := controller.intent.backup.Name
	if err := controller.appendAuditLocked("restore_cancel", "cancelled", backupName); err != nil {
		return err
	}
	controller.intent = nil
	controller.phase = MigrationRecoveryFailed
	return nil
}

func (controller *migrationRecoveryController) restore(ctx context.Context, intent migrationRestoreIntent) (*factstore.MigrationFailure, bool, error) {
	stamp := controller.now().UTC().Format("20060102T150405.000Z")
	directory := filepath.Dir(controller.config.Path)
	working := filepath.Join(directory, ".migration-restore-working-"+stamp+".db")
	ready := filepath.Join(directory, ".migration-restore-ready-"+stamp+".db")
	defer removeSQLiteFiles(working)
	defer removeSQLiteFiles(ready)
	if _, err := storesqlite.Restore(ctx, storesqlite.RestoreOptions{Source: intent.backupPath, Destination: working}); err != nil {
		return nil, false, err
	}
	workingConfig := controller.config
	workingConfig.Path = working
	database, err := storesqlite.Open(ctx, workingConfig)
	if err != nil {
		return nil, false, err
	}
	repository := factstore.NewRepository(database)
	if _, err = repository.MigrateApplicationSchema(ctx); err == nil {
		err = installBuiltinPricingCatalog(ctx, repository)
	}
	if err == nil {
		_, err = database.Backup(ctx, storesqlite.BackupOptions{Destination: ready})
	}
	closeErr := database.Close(context.WithoutCancel(ctx))
	if err = errors.Join(err, closeErr); err != nil {
		return nil, false, err
	}
	if failure, verifyErr := controller.verifyReady(ctx, func() storesqlite.Config { value := controller.config; value.Path = ready; return value }()); verifyErr != nil {
		return failure, false, verifyErr
	}
	preserved := filepath.Join(directory, "backups", "codex-pulse-failed-"+stamp+".db")
	canonicalDatabase, err := storesqlite.Open(ctx, controller.config)
	if err != nil {
		return nil, false, err
	}
	_, backupErr := canonicalDatabase.Backup(ctx, storesqlite.BackupOptions{Destination: preserved})
	closeErr = canonicalDatabase.Close(context.WithoutCancel(ctx))
	if err := errors.Join(backupErr, closeErr); err != nil {
		return nil, false, err
	}
	committed, err := controller.replaceSQLiteDatabase(controller.config.Path, ready)
	return nil, committed, err
}

func (controller *migrationRecoveryController) replaceSQLiteDatabase(canonical, ready string) (bool, error) {
	if err := controller.swapFiles(canonical, ready); err != nil {
		return false, err
	}
	directory := filepath.Dir(canonical)
	if err := controller.syncDir(directory); err != nil {
		rollbackErr := controller.swapFiles(canonical, ready)
		if rollbackErr == nil {
			rollbackErr = controller.syncDir(directory)
		}
		if rollbackErr != nil {
			return true, fmt.Errorf("sync atomic migration switch: %w; rollback: %v", err, rollbackErr)
		}
		return false, fmt.Errorf("sync atomic migration switch: %w", err)
	}
	if err := controller.removeCompanions(canonical); err != nil {
		return true, fmt.Errorf("clean committed migration companions: %w", err)
	}
	if err := controller.syncDir(directory); err != nil {
		return true, fmt.Errorf("sync committed migration switch: %w", err)
	}
	return true, nil
}

func removeSQLiteFiles(path string) {
	for _, suffix := range []string{"", "-wal", "-shm"} {
		_ = os.Remove(path + suffix)
	}
}

func removeSQLiteCompanions(path string) error {
	var result error
	for _, suffix := range []string{"-wal", "-shm"} {
		if err := os.Remove(path + suffix); err != nil && !errors.Is(err, os.ErrNotExist) {
			result = errors.Join(result, err)
		}
	}
	return result
}

func syncDirectory(path string) error {
	directory, err := os.Open(path)
	if err != nil {
		return err
	}
	defer directory.Close()
	return directory.Sync()
}

func (controller *migrationRecoveryController) appendAuditLocked(action, result, backupName string) error {
	return controller.auditAppender(action, result, backupName)
}

func (controller *migrationRecoveryController) appendAuditFileLocked(action, result, backupName string) error {
	directory := filepath.Dir(controller.auditPath)
	if err := os.Mkdir(directory, 0o700); err != nil && !errors.Is(err, os.ErrExist) {
		return err
	}
	directoryInfo, err := os.Lstat(directory)
	if err != nil || !directoryInfo.IsDir() || directoryInfo.Mode()&os.ModeSymlink != 0 || directoryInfo.Mode().Perm() != 0o700 {
		return ErrMigrationRecoveryUnavailable
	}
	if info, err := os.Lstat(controller.auditPath); err == nil {
		if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm() != 0o600 {
			return ErrMigrationRecoveryUnavailable
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	record := struct {
		AtMS       int64                    `json:"at_ms"`
		Action     string                   `json:"action"`
		Result     string                   `json:"result"`
		Stage      factstore.MigrationStage `json:"stage,omitempty"`
		Code       string                   `json:"code,omitempty"`
		BackupName string                   `json:"backup_name,omitempty"`
	}{AtMS: controller.now().UnixMilli(), Action: action, Result: result, BackupName: backupName}
	if controller.failure != nil {
		record.Stage = controller.failure.Stage
		record.Code = controller.failure.Code
	}
	encoded, err := json.Marshal(record)
	if err != nil {
		return err
	}
	descriptor, err := unix.Open(controller.auditPath, unix.O_CREAT|unix.O_APPEND|unix.O_WRONLY|unix.O_NOFOLLOW, 0o600)
	if err != nil {
		return err
	}
	file := os.NewFile(uintptr(descriptor), controller.auditPath)
	if _, err := file.Write(append(encoded, '\n')); err != nil {
		_ = file.Close()
		return err
	}
	return errors.Join(file.Sync(), file.Close())
}

func (controller *migrationRecoveryController) Exit() error {
	if controller == nil {
		return ErrMigrationRecoveryUnavailable
	}
	controller.mu.Lock()
	exit := controller.exit
	if exit == nil {
		controller.mu.Unlock()
		return ErrMigrationRecoveryUnavailable
	}
	auditErr := controller.appendAuditLocked("exit", "requested", "")
	controller.mu.Unlock()
	exit()
	return auditErr
}

func runMigrationStartupGate(ctx context.Context, config storesqlite.Config) (*factstore.MigrationFailure, error) {
	database, err := storesqlite.Open(ctx, config)
	if err != nil {
		return nil, err
	}
	repository := factstore.NewRepository(database)
	_, gateErr := repository.MigrateApplicationSchema(ctx)
	if gateErr == nil {
		gateErr = installBuiltinPricingCatalog(ctx, repository)
	}
	closeErr := database.Close(context.WithoutCancel(ctx))
	if gateErr == nil {
		return nil, closeErr
	}
	var failure *factstore.MigrationFailure
	if errors.As(gateErr, &failure) {
		failure = cloneMigrationFailure(failure)
	}
	return failure, errors.Join(gateErr, closeErr)
}

func migrationFailureFrom(err error) *factstore.MigrationFailure {
	var failure *factstore.MigrationFailure
	if errors.As(err, &failure) {
		return cloneMigrationFailure(failure)
	}
	return nil
}
