package app

import "context"

type MigrationRecoveryService struct {
	controller *migrationRecoveryController
}

func newMigrationRecoveryService(controller *migrationRecoveryController) (*MigrationRecoveryService, error) {
	if controller == nil {
		return nil, ErrMigrationRecoveryUnavailable
	}
	return &MigrationRecoveryService{controller: controller}, nil
}

func (service *MigrationRecoveryService) State(context.Context) (MigrationRecoverySnapshot, error) {
	if service == nil || service.controller == nil {
		return MigrationRecoverySnapshot{}, newBindingFailure(ErrMigrationRecoveryUnavailable)
	}
	return service.controller.Snapshot(), nil
}

func (service *MigrationRecoveryService) Retry(ctx context.Context) (MigrationRecoveryReceipt, error) {
	if service == nil || service.controller == nil {
		return MigrationRecoveryReceipt{}, newBindingFailure(ErrMigrationRecoveryUnavailable)
	}
	return bindingCall(func() (MigrationRecoveryReceipt, error) { return service.controller.Retry(ctx) })
}

func (service *MigrationRecoveryService) Prepare(ctx context.Context, backupName string) (MigrationRestoreConfirmation, error) {
	if service == nil || service.controller == nil {
		return MigrationRestoreConfirmation{}, newBindingFailure(ErrMigrationRecoveryUnavailable)
	}
	return bindingCall(func() (MigrationRestoreConfirmation, error) {
		return service.controller.PrepareRestore(ctx, backupName)
	})
}

func (service *MigrationRecoveryService) Confirm(ctx context.Context, token string) (MigrationRecoveryReceipt, error) {
	if service == nil || service.controller == nil {
		return MigrationRecoveryReceipt{}, newBindingFailure(ErrMigrationRecoveryUnavailable)
	}
	return bindingCall(func() (MigrationRecoveryReceipt, error) { return service.controller.ConfirmRestore(ctx, token) })
}

func (service *MigrationRecoveryService) Cancel(context.Context) error {
	if service == nil || service.controller == nil {
		return newBindingFailure(ErrMigrationRecoveryUnavailable)
	}
	_, err := bindingCall(func() (struct{}, error) { return struct{}{}, service.controller.CancelRestore() })
	return err
}

func (service *MigrationRecoveryService) Exit(context.Context) error {
	if service == nil || service.controller == nil {
		return newBindingFailure(ErrMigrationRecoveryUnavailable)
	}
	_, err := bindingCall(func() (struct{}, error) { return struct{}{}, service.controller.Exit() })
	return err
}
