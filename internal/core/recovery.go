package core

import "context"

type MigrationBackupInfo struct {
	Name         string `json:"name"`
	SizeBytes    int64  `json:"sizeBytes"`
	ModifiedAtMS int64  `json:"modifiedAtMs"`
}

type MigrationRecoverySnapshot struct {
	Version        string                `json:"version"`
	Phase          string                `json:"phase"`
	Stage          string                `json:"stage"`
	Code           string                `json:"code"`
	CurrentVersion int                   `json:"currentVersion"`
	TargetVersion  int                   `json:"targetVersion"`
	FailedVersion  int                   `json:"failedVersion"`
	CanRetry       bool                  `json:"canRetry"`
	CanExit        bool                  `json:"canExit"`
	Backups        []MigrationBackupInfo `json:"backups"`
	AuditWarning   bool                  `json:"auditWarning"`
}

type MigrationRecoveryReceipt struct {
	Phase           string `json:"phase"`
	RestartRequired bool   `json:"restartRequired"`
	AuditWarning    bool   `json:"auditWarning"`
}

type MigrationRestoreConfirmation struct {
	ConfirmationToken string              `json:"confirmationToken"`
	Backup            MigrationBackupInfo `json:"backup"`
}

type MigrationRecoveryService interface {
	State(context.Context) (MigrationRecoverySnapshot, error)
	Retry(context.Context) (MigrationRecoveryReceipt, error)
	Prepare(context.Context, string) (MigrationRestoreConfirmation, error)
	Confirm(context.Context, string) (MigrationRecoveryReceipt, error)
	Cancel(context.Context) error
	Exit(context.Context) error
}
