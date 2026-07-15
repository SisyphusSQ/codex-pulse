package store

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"gorm.io/gorm"

	storesqlite "github.com/SisyphusSQ/codex-pulse/internal/store/sqlite"
)

const (
	applicationSchemaV1Version = 1
	applicationSchemaV2Version = 2
	applicationSchemaV3Version = 3
	applicationSchemaV4Version = 4
	applicationSchemaV5Version = 5
	applicationSchemaV6Version = 6
	applicationSchemaV7Version = 7
	applicationSchemaVersion   = applicationSchemaV7Version
)

var (
	// ErrMigrationContract 表示本地 migration history、checksum 或版本状态不自洽。
	ErrMigrationContract = errors.New("migration contract mismatch")
	// ErrMigrationNewer 表示数据库版本高于当前二进制可理解的 catalog。
	ErrMigrationNewer = errors.New("database schema is newer than this application")
)

type schemaMigrationModel struct {
	Version     int    `gorm:"column:version;primaryKey"`
	Name        string `gorm:"column:name"`
	Checksum    string `gorm:"column:checksum"`
	AppliedAtMS int64  `gorm:"column:applied_at_ms"`
}

func (schemaMigrationModel) TableName() string { return "schema_migrations" }

var migrationSchemaObjects = []schemaObject{
	{
		objectType: "table",
		name:       "schema_migrations",
		statement: `CREATE TABLE IF NOT EXISTS schema_migrations (
			version INTEGER PRIMARY KEY CHECK (version > 0),
			name TEXT NOT NULL CHECK (length(name) > 0),
			checksum TEXT NOT NULL CHECK (length(checksum) = 64 AND checksum NOT GLOB '*[^0-9a-f]*'),
			applied_at_ms INTEGER NOT NULL CHECK (applied_at_ms >= 0)
		) STRICT`,
	},
}

type migrationDefinition struct {
	version  int
	name     string
	checksum string
	apply    func(context.Context, *gorm.DB) error
}

var applicationMigrations = []migrationDefinition{
	{
		version:  applicationSchemaV1Version,
		name:     "initial-application-schema",
		checksum: applicationSchemaV1Checksum(),
		apply: func(ctx context.Context, transaction *gorm.DB) error {
			if err := ensureApplicationSchemaV1(ctx, transaction); err != nil {
				return err
			}
			return ensureSchemaObjects(ctx, transaction, runtimeSchemaObjects)
		},
	},
	{
		version:  applicationSchemaV2Version,
		name:     "retention-query-indexes",
		checksum: applicationSchemaV2Checksum(),
		apply: func(ctx context.Context, transaction *gorm.DB) error {
			return ensureSchemaObjects(ctx, transaction, retentionSchemaObjects)
		},
	},
	{
		version:  applicationSchemaV3Version,
		name:     "incremental-ingest-checkpoints",
		checksum: applicationSchemaV3Checksum(),
		apply: func(ctx context.Context, transaction *gorm.DB) error {
			if err := rebuildTurnTablesForV3(ctx, transaction); err != nil {
				return err
			}
			return ensureSchemaObjects(ctx, transaction, ingestSchemaObjects)
		},
	},
	{
		version:  applicationSchemaV4Version,
		name:     "session-project-model-attribution",
		checksum: applicationSchemaV4Checksum(),
		apply: func(ctx context.Context, transaction *gorm.DB) error {
			if err := ensureSchemaObjects(ctx, transaction, attributionSchemaObjects); err != nil {
				return err
			}
			_, err := recomputeAttributionsInTransaction(ctx, transaction, nil)
			return err
		},
	},
	{
		version:  applicationSchemaV5Version,
		name:     "pricing-cost-daily-rollup",
		checksum: applicationSchemaV5Checksum(),
		apply: func(ctx context.Context, transaction *gorm.DB) error {
			return ensureSchemaObjects(ctx, transaction, costSchemaObjects)
		},
	},
	{
		version:  applicationSchemaV6Version,
		name:     "bootstrap-plan-and-job-facts",
		checksum: applicationSchemaV6Checksum(),
		apply: func(ctx context.Context, transaction *gorm.DB) error {
			return ensureSchemaObjects(ctx, transaction, bootstrapSchemaObjects)
		},
	},
	{
		version:  applicationSchemaV7Version,
		name:     "live-backfill-scheduler",
		checksum: applicationSchemaV7Checksum(),
		apply: func(ctx context.Context, transaction *gorm.DB) error {
			return ensureSchemaObjects(ctx, transaction, schedulerSchemaObjects)
		},
	},
}

// MigrationReport 描述本次启动观察到并应用的版本事实。
type MigrationReport struct {
	FromVersion     int
	TargetVersion   int
	AppliedVersions []int
	BackupPath      string
}

type migrationRunner struct {
	repository    *Repository
	catalog       []migrationDefinition
	now           func() time.Time
	verifyCurrent func(context.Context, *gorm.DB) error
	spaceCheck    func(context.Context, string, int64) error
	backup        func(context.Context, int, int, func(storesqlite.BackupProgress)) (string, error)
	observe       func(MigrationProgress)
}

// MigrateApplicationSchema 校验 append-only history，并原子应用全部 pending migration。
// 它是启动期 bootstrap 契约：调用方必须在 Open 后、Store 暴露给任何 runtime
// reader/writer 之前执行。运行期维护不得直接调用；未来若需要在线 migration，必须先由
// 上层建立可证明的排空与独占协议。
func (repository *Repository) MigrateApplicationSchema(ctx context.Context) (MigrationReport, error) {
	now := time.Now
	runner := migrationRunner{
		repository: repository,
		catalog:    applicationMigrations,
		now:        now,
		verifyCurrent: func(ctx context.Context, transaction *gorm.DB) error {
			return verifyApplicationSchema(ctx, transaction)
		},
		spaceCheck: ensureMigrationBackupSpace,
		backup: func(
			ctx context.Context,
			_ int,
			targetVersion int,
			observe func(storesqlite.BackupProgress),
		) (string, error) {
			return defaultMigrationBackup(ctx, repository.database, targetVersion, now(), observe)
		},
	}
	return runner.run(ctx)
}

func (runner migrationRunner) run(ctx context.Context) (MigrationReport, error) {
	if runner.repository == nil || runner.repository.database == nil {
		return MigrationReport{}, ErrInvalidRepository
	}
	targetVersion, err := validateMigrationCatalog(runner.catalog)
	if err != nil {
		return MigrationReport{}, migrationFailure(
			MigrationStageCatalog, MigrationCodeCatalogInvalid, MigrationReport{}, 0, err,
		)
	}
	if runner.now == nil {
		runner.now = time.Now
	}
	report := MigrationReport{TargetVersion: targetVersion}
	runner.emit(MigrationProgress{Stage: MigrationStageInspect, TargetVersion: targetVersion})
	preflight, hasUserSchema, err := runner.preflight(ctx, targetVersion)
	report.FromVersion = preflight.version
	if err != nil {
		return report, migrationFailure(
			MigrationStageInspect, migrationInspectCode(err), report, 0, err,
		)
	}
	if preflight.version < targetVersion && hasUserSchema {
		requiredBytes, err := migrationBackupRequiredBytes(runner.repository.database.Config().Path)
		if err != nil {
			return report, migrationFailure(
				MigrationStageSpace, MigrationCodeSpaceCheckFailed, report, 0, err,
			)
		}
		runner.emit(MigrationProgress{
			Stage: MigrationStageSpace, CurrentVersion: report.FromVersion, TargetVersion: targetVersion,
		})
		if runner.spaceCheck == nil {
			err := fmt.Errorf("%w: required backup has no space checker", ErrMigrationContract)
			return report, migrationFailure(
				MigrationStageSpace, MigrationCodeSpaceCheckFailed, report, 0, err,
			)
		}
		if err := runner.spaceCheck(ctx, runner.repository.database.Config().Path, requiredBytes); err != nil {
			return report, migrationFailure(
				MigrationStageSpace, migrationSpaceCode(err), report, 0, err,
			)
		}
		if runner.backup == nil {
			err := fmt.Errorf("%w: required backup has no hook", ErrMigrationContract)
			return report, migrationFailure(
				MigrationStageBackup, MigrationCodeBackupFailed, report, 0, err,
			)
		}
		runner.emit(MigrationProgress{
			Stage: MigrationStageBackup, CurrentVersion: report.FromVersion, TargetVersion: targetVersion,
		})
		backupPath, err := runner.backup(
			ctx, preflight.version, targetVersion,
			func(progress storesqlite.BackupProgress) {
				runner.emit(MigrationProgress{
					Stage: MigrationStageBackup, CurrentVersion: report.FromVersion,
					TargetVersion: targetVersion, CopiedPages: progress.CopiedPages,
					RemainingPages: progress.RemainingPages, TotalPages: progress.TotalPages,
				})
			},
		)
		if err != nil {
			return report, migrationFailure(
				MigrationStageBackup, MigrationCodeBackupFailed, report, 0, err,
			)
		}
		if backupPath == "" {
			err := fmt.Errorf("%w: backup hook returned an empty path", ErrMigrationContract)
			return report, migrationFailure(
				MigrationStageBackup, MigrationCodeBackupFailed, report, 0, err,
			)
		}
		report.BackupPath = backupPath
	}
	var pendingApplied []int
	failedStage := MigrationStageInspect
	failedVersion := 0
	err = runner.repository.database.Write(ctx, func(ctx context.Context, transaction storesqlite.WriteTx) error {
		state, err := inspectMigrationState(ctx, transaction)
		if err != nil {
			return err
		}
		report.FromVersion = state.version
		if err := validateMigrationState(state, runner.catalog, targetVersion); err != nil {
			return err
		}
		if state.version == targetVersion {
			if runner.verifyCurrent != nil {
				failedStage = MigrationStageVerify
				runner.emit(MigrationProgress{
					Stage: MigrationStageVerify, CurrentVersion: state.version, TargetVersion: targetVersion,
				})
				return runner.verifyCurrent(ctx, transaction)
			}
			return nil
		}

		failedStage = MigrationStageApply
		if err := ensureSchemaObjects(ctx, transaction, migrationSchemaObjects); err != nil {
			return fmt.Errorf("%w: create migration ledger: %v", ErrMigrationContract, err)
		}
		appliedAtMS := runner.now().UnixMilli()
		for _, migration := range runner.catalog[state.version:] {
			failedStage = MigrationStageApply
			failedVersion = migration.version
			runner.emit(MigrationProgress{
				Stage: MigrationStageApply, CurrentVersion: state.version,
				TargetVersion: targetVersion, Version: migration.version,
			})
			if err := migration.apply(ctx, transaction); err != nil {
				return fmt.Errorf("apply migration %d %q: %w", migration.version, migration.name, err)
			}
			history := schemaMigrationModel{
				Version: migration.version, Name: migration.name, Checksum: migration.checksum,
				AppliedAtMS: appliedAtMS,
			}
			if err := transaction.WithContext(ctx).Create(&history).Error; err != nil {
				return fmt.Errorf("%w: append version %d: %v", ErrMigrationContract, migration.version, err)
			}
			pendingApplied = append(pendingApplied, migration.version)
		}
		// user_version 不支持参数绑定；targetVersion 来自已验证的进程内 catalog。
		if err := transaction.WithContext(ctx).
			Exec("PRAGMA user_version = " + strconv.Itoa(targetVersion)).Error; err != nil {
			return fmt.Errorf("%w: advance user_version: %v", ErrMigrationContract, err)
		}
		if runner.verifyCurrent != nil {
			failedStage = MigrationStageVerify
			failedVersion = 0
			runner.emit(MigrationProgress{
				Stage: MigrationStageVerify, CurrentVersion: targetVersion, TargetVersion: targetVersion,
			})
			return runner.verifyCurrent(ctx, transaction)
		}
		return nil
	})
	if err != nil {
		code := MigrationCodeApplyFailed
		if failedStage == MigrationStageInspect {
			code = migrationInspectCode(err)
		} else if failedStage == MigrationStageVerify {
			code = MigrationCodeVerifyFailed
		}
		return report, migrationFailure(failedStage, code, report, failedVersion, err)
	}
	report.AppliedVersions = append(report.AppliedVersions, pendingApplied...)
	runner.emit(MigrationProgress{
		Stage: MigrationStageComplete, CurrentVersion: targetVersion, TargetVersion: targetVersion,
	})
	return report, nil
}

func (runner migrationRunner) emit(progress MigrationProgress) {
	if runner.observe != nil {
		runner.observe(progress)
	}
}

func migrationInspectCode(err error) string {
	if errors.Is(err, ErrMigrationNewer) {
		return MigrationCodeNewerSchema
	}
	if errors.Is(err, ErrMigrationContract) {
		return MigrationCodeHistoryDrift
	}
	return MigrationCodeInspectFailed
}

func migrationSpaceCode(err error) string {
	if errors.Is(err, storesqlite.ErrDiskFull) {
		return MigrationCodeInsufficientSpace
	}
	return MigrationCodeSpaceCheckFailed
}

func (runner migrationRunner) preflight(
	ctx context.Context,
	targetVersion int,
) (migrationState, bool, error) {
	var state migrationState
	var hasUserSchema bool
	err := runner.repository.database.View(ctx, func(ctx context.Context, connection storesqlite.ReadConn) error {
		var err error
		state, err = inspectMigrationState(ctx, connection)
		if err != nil {
			return err
		}
		if err := validateMigrationState(state, runner.catalog, targetVersion); err != nil {
			return err
		}
		tables, err := connection.WithContext(ctx).Migrator().GetTables()
		if err != nil {
			return fmt.Errorf("%w: list existing tables: %v", ErrMigrationContract, err)
		}
		for _, table := range tables {
			if table != (schemaMigrationModel{}).TableName() && !strings.HasPrefix(table, "sqlite_") {
				hasUserSchema = true
				break
			}
		}
		return nil
	})
	return state, hasUserSchema, err
}

type migrationState struct {
	version      int
	ledgerExists bool
	history      []schemaMigrationModel
}

func inspectMigrationState(ctx context.Context, transaction storesqlite.WriteTx) (migrationState, error) {
	var state migrationState
	if err := transaction.WithContext(ctx).Raw(`PRAGMA user_version`).Row().Scan(&state.version); err != nil {
		return migrationState{}, fmt.Errorf("%w: read user_version: %v", ErrMigrationContract, err)
	}
	state.ledgerExists = transaction.Migrator().HasTable(&schemaMigrationModel{})
	if !state.ledgerExists {
		return state, nil
	}
	validLedger, err := verifySchemaObject(ctx, transaction, migrationSchemaObjects[0])
	if err != nil || !validLedger {
		return migrationState{}, fmt.Errorf("%w: invalid migration ledger: %v", ErrMigrationContract, err)
	}
	if err := transaction.WithContext(ctx).Order("version").Find(&state.history).Error; err != nil {
		return migrationState{}, fmt.Errorf("%w: read migration history: %v", ErrMigrationContract, err)
	}
	return state, nil
}

func validateMigrationCatalog(catalog []migrationDefinition) (int, error) {
	if len(catalog) == 0 {
		return 0, fmt.Errorf("%w: migration catalog is empty", ErrMigrationContract)
	}
	for index, migration := range catalog {
		expectedVersion := index + 1
		if migration.version != expectedVersion {
			return 0, fmt.Errorf(
				"%w: catalog version %d, want %d", ErrMigrationContract, migration.version, expectedVersion,
			)
		}
		if migration.name == "" || migration.apply == nil || !validMigrationChecksum(migration.checksum) {
			return 0, fmt.Errorf("%w: invalid descriptor at version %d", ErrMigrationContract, migration.version)
		}
	}
	return catalog[len(catalog)-1].version, nil
}

func validMigrationChecksum(checksum string) bool {
	if len(checksum) != sha256.Size*2 {
		return false
	}
	for _, character := range checksum {
		if !strings.ContainsRune("0123456789abcdef", character) {
			return false
		}
	}
	return true
}

func validateMigrationState(
	state migrationState,
	catalog []migrationDefinition,
	targetVersion int,
) error {
	if state.version > targetVersion {
		return fmt.Errorf("%w: database=%d catalog=%d", ErrMigrationNewer, state.version, targetVersion)
	}
	if !state.ledgerExists {
		if state.version != 0 {
			return fmt.Errorf("%w: user_version=%d without ledger", ErrMigrationContract, state.version)
		}
		return nil
	}
	for _, history := range state.history {
		if history.Version > targetVersion {
			return fmt.Errorf(
				"%w: history version=%d catalog=%d",
				ErrMigrationNewer, history.Version, targetVersion,
			)
		}
	}
	if len(state.history) != state.version {
		return fmt.Errorf(
			"%w: user_version=%d history_count=%d",
			ErrMigrationContract, state.version, len(state.history),
		)
	}
	for index, history := range state.history {
		expectedVersion := index + 1
		if history.Version != expectedVersion || expectedVersion > len(catalog) {
			return fmt.Errorf("%w: non-contiguous history at version %d", ErrMigrationContract, history.Version)
		}
		expected := catalog[index]
		if history.Name != expected.name || history.Checksum != expected.checksum {
			return fmt.Errorf("%w: history drift at version %d", ErrMigrationContract, history.Version)
		}
	}
	return nil
}

func verifyApplicationSchema(ctx context.Context, transaction storesqlite.WriteTx) error {
	for _, objects := range [][]schemaObject{
		migrationSchemaObjects, coreSchemaObjects, runtimeSchemaObjects, retentionSchemaObjects,
		ingestSchemaObjects, attributionSchemaObjects, costSchemaObjects, bootstrapSchemaObjects,
		schedulerSchemaObjects,
	} {
		for _, object := range objects {
			exists, err := verifySchemaObject(ctx, transaction, object)
			if err != nil {
				return err
			}
			if !exists {
				return fmt.Errorf("%w: missing %s %q", ErrSchemaContract, object.objectType, object.name)
			}
		}
	}
	return nil
}

func applicationSchemaV1Checksum() string {
	hasher := sha256.New()
	_, _ = fmt.Fprintln(hasher, applicationSchemaV1Version, "initial-application-schema")
	for _, objects := range [][]schemaObject{migrationSchemaObjects, applicationSchemaV1CoreObjects(), runtimeSchemaObjects} {
		for _, object := range objects {
			_, _ = fmt.Fprintln(
				hasher, object.objectType, object.name,
				strings.TrimSpace(normalizeSchemaSQL(canonicalSchemaSQL(object.statement))),
			)
		}
	}
	return fmt.Sprintf("%x", hasher.Sum(nil))
}

func applicationSchemaV2Checksum() string {
	hasher := sha256.New()
	_, _ = fmt.Fprintln(hasher, applicationSchemaV2Version, "retention-query-indexes")
	for _, object := range retentionSchemaObjects {
		_, _ = fmt.Fprintln(
			hasher, object.objectType, object.name,
			strings.TrimSpace(normalizeSchemaSQL(canonicalSchemaSQL(object.statement))),
		)
	}
	return fmt.Sprintf("%x", hasher.Sum(nil))
}

func applicationSchemaV3Checksum() string {
	hasher := sha256.New()
	_, _ = fmt.Fprintln(hasher, applicationSchemaV3Version, "incremental-ingest-checkpoints")
	for _, object := range append([]schemaObject{currentTurnsSchemaObject()}, ingestSchemaObjects...) {
		_, _ = fmt.Fprintln(
			hasher, object.objectType, object.name,
			strings.TrimSpace(normalizeSchemaSQL(canonicalSchemaSQL(object.statement))),
		)
	}
	return fmt.Sprintf("%x", hasher.Sum(nil))
}

func applicationSchemaV4Checksum() string {
	hasher := sha256.New()
	_, _ = fmt.Fprintln(hasher, applicationSchemaV4Version, "session-project-model-attribution")
	for _, object := range attributionSchemaObjects {
		_, _ = fmt.Fprintln(
			hasher, object.objectType, object.name,
			strings.TrimSpace(normalizeSchemaSQL(canonicalSchemaSQL(object.statement))),
		)
	}
	return fmt.Sprintf("%x", hasher.Sum(nil))
}

func applicationSchemaV5Checksum() string {
	hasher := sha256.New()
	_, _ = fmt.Fprintln(hasher, applicationSchemaV5Version, "pricing-cost-daily-rollup")
	for _, object := range costSchemaObjects {
		_, _ = fmt.Fprintln(
			hasher, object.objectType, object.name,
			strings.TrimSpace(normalizeSchemaSQL(canonicalSchemaSQL(object.statement))),
		)
	}
	return fmt.Sprintf("%x", hasher.Sum(nil))
}

func applicationSchemaV6Checksum() string {
	hasher := sha256.New()
	_, _ = fmt.Fprintln(hasher, applicationSchemaV6Version, "bootstrap-plan-and-job-facts")
	for _, object := range bootstrapSchemaObjects {
		_, _ = fmt.Fprintln(
			hasher, object.objectType, object.name,
			strings.TrimSpace(normalizeSchemaSQL(canonicalSchemaSQL(object.statement))),
		)
	}
	return fmt.Sprintf("%x", hasher.Sum(nil))
}

func applicationSchemaV7Checksum() string {
	hasher := sha256.New()
	_, _ = fmt.Fprintln(hasher, applicationSchemaV7Version, "live-backfill-scheduler")
	for _, object := range schedulerSchemaObjects {
		_, _ = fmt.Fprintln(
			hasher, object.objectType, object.name,
			strings.TrimSpace(normalizeSchemaSQL(canonicalSchemaSQL(object.statement))),
		)
	}
	return fmt.Sprintf("%x", hasher.Sum(nil))
}

func applicationSchemaV1CoreObjects() []schemaObject {
	objects := append([]schemaObject(nil), coreSchemaObjects...)
	for index := range objects {
		if objects[index].objectType == "table" && objects[index].name == "turns" {
			objects[index].statement = turnsSchemaV1Statement
			break
		}
	}
	return objects
}

func ensureApplicationSchemaV1(ctx context.Context, transaction *gorm.DB) error {
	objects := applicationSchemaV1CoreObjects()
	for _, object := range objects {
		if object.objectType != "table" || object.name != "turns" {
			if err := ensureSchemaObjects(ctx, transaction, []schemaObject{object}); err != nil {
				return err
			}
			continue
		}
		if !transaction.Migrator().HasTable("turns") {
			if err := ensureSchemaObjects(ctx, transaction, []schemaObject{object}); err != nil {
				return err
			}
			continue
		}
		validHistorical, historicalErr := verifySchemaObject(ctx, transaction, object)
		if validHistorical {
			continue
		}
		validCurrent, currentErr := verifySchemaObject(ctx, transaction, currentTurnsSchemaObject())
		if validCurrent {
			continue
		}
		if historicalErr != nil {
			return historicalErr
		}
		if currentErr != nil {
			return currentErr
		}
		if !validCurrent {
			return fmt.Errorf("%w: table %q differs from canonical definition", ErrSchemaContract, "turns")
		}
	}
	return nil
}

func currentTurnsSchemaObject() schemaObject {
	return schemaObject{objectType: "table", name: "turns", statement: turnsSchemaCurrentStatement}
}

// rebuildTurnTablesForV3 是 v3 唯一的 table-rebuild raw SQL bridge。SQLite 不支持
// ALTER TABLE DROP CHECK；同时 turns 被 session_current/turn_usage 引用，所以三张表
// 必须在同一 migration transaction 内一起重建，避免 rename 后子表外键指向旧表。
func rebuildTurnTablesForV3(ctx context.Context, transaction *gorm.DB) error {
	for _, index := range []string{
		"idx_turns_source_position", "idx_turns_session_lifecycle", "idx_turns_project_time",
		"idx_turns_model_time", "idx_session_current_activity", "idx_turn_usage_observed_final",
	} {
		if err := transaction.WithContext(ctx).Exec("DROP INDEX IF EXISTS " + index).Error; err != nil {
			return fmt.Errorf("drop v2 index %q: %w", index, err)
		}
	}
	for _, rename := range []string{
		"ALTER TABLE session_current RENAME TO session_current_v2",
		"ALTER TABLE turn_usage RENAME TO turn_usage_v2",
		"ALTER TABLE turns RENAME TO turns_v2",
	} {
		if err := transaction.WithContext(ctx).Exec(rename).Error; err != nil {
			return fmt.Errorf("prepare v3 turn rebuild: %w", err)
		}
	}

	var tables, indexes []schemaObject
	for _, object := range coreSchemaObjects {
		switch {
		case object.objectType == "table" &&
			(object.name == "turns" || object.name == "session_current" || object.name == "turn_usage"):
			tables = append(tables, object)
		case object.objectType == "index" &&
			(object.name == "idx_turns_source_position" || object.name == "idx_turns_session_lifecycle" ||
				object.name == "idx_turns_project_time" || object.name == "idx_turns_model_time" ||
				object.name == "idx_session_current_activity" || object.name == "idx_turn_usage_observed_final"):
			indexes = append(indexes, object)
		}
	}
	if err := ensureSchemaObjects(ctx, transaction, tables); err != nil {
		return err
	}
	for _, copyStatement := range []string{
		`INSERT INTO turns (
			turn_id, session_id, started_at_ms, completed_at_ms, outcome, model,
			reasoning_effort, cwd, project_id, source_generation, start_offset, complete_offset
		) SELECT
			turn_id, session_id, started_at_ms, completed_at_ms, outcome, model,
			reasoning_effort, cwd, project_id, source_generation, start_offset, complete_offset
		FROM turns_v2`,
		`INSERT INTO turn_usage (
			turn_id, observed_at_ms, is_final, input_tokens, cached_input_tokens, output_tokens,
			reasoning_tokens, context_window, source_generation, source_offset, confidence, updated_at_ms
		) SELECT
			turn_id, observed_at_ms, is_final, input_tokens, cached_input_tokens, output_tokens,
			reasoning_tokens, context_window, source_generation, source_offset, confidence, updated_at_ms
		FROM turn_usage_v2`,
		`INSERT INTO session_current (
			session_id, thread_name, thread_name_updated_at_ms, active_turn_id,
			current_model, current_cwd, last_activity_at_ms, updated_at_ms
		) SELECT
			session_id, thread_name, thread_name_updated_at_ms, active_turn_id,
			current_model, current_cwd, last_activity_at_ms, updated_at_ms
		FROM session_current_v2`,
	} {
		if err := transaction.WithContext(ctx).Exec(copyStatement).Error; err != nil {
			return fmt.Errorf("copy v2 turn data: %w", err)
		}
	}
	for _, table := range []string{"session_current_v2", "turn_usage_v2", "turns_v2"} {
		if err := transaction.WithContext(ctx).Exec("DROP TABLE " + table).Error; err != nil {
			return fmt.Errorf("drop rebuilt v2 table %q: %w", table, err)
		}
	}
	return ensureSchemaObjects(ctx, transaction, indexes)
}
