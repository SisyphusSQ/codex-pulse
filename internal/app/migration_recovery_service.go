package app

import (
	"context"

	"github.com/SisyphusSQ/codex-pulse/internal/core"
	basequery "github.com/SisyphusSQ/codex-pulse/internal/query"
)

type MigrationRecoveryService struct {
	controller *migrationRecoveryController
}

func newMigrationRecoveryService(controller *migrationRecoveryController) (*MigrationRecoveryService, error) {
	if controller == nil {
		return nil, ErrMigrationRecoveryUnavailable
	}
	return &MigrationRecoveryService{controller: controller}, nil
}

func (service *MigrationRecoveryService) State(ctx context.Context) (core.MigrationRecoverySnapshot, error) {
	if service == nil || service.controller == nil {
		return core.MigrationRecoverySnapshot{}, basequery.NewUnavailableFailure(ErrMigrationRecoveryUnavailable)
	}
	if err := ctx.Err(); err != nil {
		return core.MigrationRecoverySnapshot{}, err
	}
	return coreRecoverySnapshot(service.controller.Snapshot()), nil
}

func (service *MigrationRecoveryService) Retry(ctx context.Context) (core.MigrationRecoveryReceipt, error) {
	if service == nil || service.controller == nil {
		return core.MigrationRecoveryReceipt{}, basequery.NewUnavailableFailure(ErrMigrationRecoveryUnavailable)
	}
	receipt, err := service.controller.Retry(ctx)
	return coreRecoveryReceipt(receipt), publicRuntimeCommandFailure(err)
}

func (service *MigrationRecoveryService) Prepare(
	ctx context.Context,
	backupName string,
) (core.MigrationRestoreConfirmation, error) {
	if service == nil || service.controller == nil {
		return core.MigrationRestoreConfirmation{}, basequery.NewUnavailableFailure(ErrMigrationRecoveryUnavailable)
	}
	confirmation, err := service.controller.PrepareRestore(ctx, backupName)
	return coreRestoreConfirmation(confirmation), publicRuntimeCommandFailure(err)
}

func (service *MigrationRecoveryService) Confirm(
	ctx context.Context,
	token string,
) (core.MigrationRecoveryReceipt, error) {
	if service == nil || service.controller == nil {
		return core.MigrationRecoveryReceipt{}, basequery.NewUnavailableFailure(ErrMigrationRecoveryUnavailable)
	}
	receipt, err := service.controller.ConfirmRestore(ctx, token)
	return coreRecoveryReceipt(receipt), publicRuntimeCommandFailure(err)
}

func (service *MigrationRecoveryService) Cancel(ctx context.Context) error {
	if service == nil || service.controller == nil {
		return basequery.NewUnavailableFailure(ErrMigrationRecoveryUnavailable)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return publicRuntimeCommandFailure(service.controller.CancelRestore())
}

func (service *MigrationRecoveryService) Exit(ctx context.Context) error {
	if service == nil || service.controller == nil {
		return basequery.NewUnavailableFailure(ErrMigrationRecoveryUnavailable)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return publicRuntimeCommandFailure(service.controller.Exit())
}

func coreRecoverySnapshot(snapshot MigrationRecoverySnapshot) core.MigrationRecoverySnapshot {
	backups := make([]core.MigrationBackupInfo, 0, len(snapshot.Backups))
	for _, backup := range snapshot.Backups {
		backups = append(backups, core.MigrationBackupInfo{
			Name: backup.Name, SizeBytes: backup.SizeBytes, ModifiedAtMS: backup.ModifiedAtMS,
		})
	}
	return core.MigrationRecoverySnapshot{
		Version: snapshot.Version, Phase: string(snapshot.Phase), Stage: string(snapshot.Stage), Code: snapshot.Code,
		CurrentVersion: snapshot.CurrentVersion, TargetVersion: snapshot.TargetVersion,
		FailedVersion: snapshot.FailedVersion, CanRetry: snapshot.CanRetry, CanExit: snapshot.CanExit,
		Backups: backups, AuditWarning: snapshot.AuditWarning,
	}
}

func coreRecoveryReceipt(receipt MigrationRecoveryReceipt) core.MigrationRecoveryReceipt {
	return core.MigrationRecoveryReceipt{
		Phase: string(receipt.Phase), RestartRequired: receipt.RestartRequired, AuditWarning: receipt.AuditWarning,
	}
}

func coreRestoreConfirmation(confirmation MigrationRestoreConfirmation) core.MigrationRestoreConfirmation {
	return core.MigrationRestoreConfirmation{
		ConfirmationToken: confirmation.Token,
		Backup: core.MigrationBackupInfo{
			Name: confirmation.Backup.Name, SizeBytes: confirmation.Backup.SizeBytes,
			ModifiedAtMS: confirmation.Backup.ModifiedAtMS,
		},
	}
}
