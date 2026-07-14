package store

import "fmt"

type MigrationStage string

const (
	MigrationStageCatalog  MigrationStage = "catalog"
	MigrationStageInspect  MigrationStage = "inspect"
	MigrationStageSpace    MigrationStage = "space"
	MigrationStageBackup   MigrationStage = "backup"
	MigrationStageApply    MigrationStage = "apply"
	MigrationStageVerify   MigrationStage = "verify"
	MigrationStageComplete MigrationStage = "complete"
)

const (
	MigrationCodeCatalogInvalid    = "catalog_invalid"
	MigrationCodeInspectFailed     = "inspect_failed"
	MigrationCodeHistoryDrift      = "history_drift"
	MigrationCodeNewerSchema       = "newer_schema"
	MigrationCodeSpaceCheckFailed  = "space_check_failed"
	MigrationCodeInsufficientSpace = "insufficient_space"
	MigrationCodeBackupFailed      = "backup_failed"
	MigrationCodeApplyFailed       = "apply_failed"
	MigrationCodeVerifyFailed      = "verify_failed"
)

// MigrationProgress 不包含 SQL、数据库内容或 driver 错误文本。
type MigrationProgress struct {
	Stage          MigrationStage
	CurrentVersion int
	TargetVersion  int
	Version        int
	CopiedPages    int
	RemainingPages int
	TotalPages     int
}

// MigrationFailure 保留稳定 stage/code 与可 errors.Is/As 的底层 cause。
type MigrationFailure struct {
	Stage          MigrationStage
	Code           string
	CurrentVersion int
	TargetVersion  int
	FailedVersion  int
	BackupPath     string
	Cause          error
}

func (failure *MigrationFailure) Error() string {
	return fmt.Sprintf(
		"migration %s (%s) current=%d target=%d failed=%d: %v",
		failure.Stage, failure.Code, failure.CurrentVersion, failure.TargetVersion,
		failure.FailedVersion, failure.Cause,
	)
}

func (failure *MigrationFailure) Unwrap() error { return failure.Cause }

func migrationFailure(
	stage MigrationStage,
	code string,
	report MigrationReport,
	failedVersion int,
	cause error,
) error {
	return &MigrationFailure{
		Stage: stage, Code: code, CurrentVersion: report.FromVersion,
		TargetVersion: report.TargetVersion, FailedVersion: failedVersion,
		BackupPath: report.BackupPath, Cause: cause,
	}
}
