package store

import (
	"context"
	"fmt"

	"gorm.io/gorm"

	storeretention "github.com/SisyphusSQ/codex-pulse/internal/store/retention"
	storeschema "github.com/SisyphusSQ/codex-pulse/internal/store/schema"
)

func migrateCodexAccountBindingForV32(ctx context.Context, transaction *gorm.DB) error {
	if transaction == nil {
		return fmt.Errorf("%w: invalid account binding migration database", ErrMigrationContract)
	}
	if err := storeschema.EnsureObjects(ctx, transaction, accountBindingSchemaObjects); err != nil {
		return err
	}
	if err := rebuildQuotaAccountScopeTablesForV32(ctx, transaction); err != nil {
		return err
	}
	if err := rebuildSourceRefreshAndResetCreditsForV32(ctx, transaction); err != nil {
		return err
	}
	if err := sealLegacyCodexSchedulesForV32(ctx, transaction); err != nil {
		return err
	}
	return verifyNoForeignKeyViolations(ctx, transaction)
}

func rebuildQuotaAccountScopeTablesForV32(ctx context.Context, transaction *gorm.DB) error {
	renames := []string{
		"quota_arbitration_evidence",
		"quota_current",
		"quota_observation_receipts",
		"quota_observations",
	}
	if err := renameTablesForV32(ctx, transaction, renames); err != nil {
		return err
	}
	if err := ensureNamedObjects(ctx, transaction, quotaSchemaObjectsV32, "table", "quota_observations"); err != nil {
		return err
	}
	if err := copyTableColumns(ctx, transaction, "quota_observations_v31", "quota_observations", []string{
		"observation_id", "account_scope", "source", "limit_id", "limit_name", "window_kind",
		"used_percent", "window_minutes", "resets_at_ms", "plan_type", "validity", "rejection_reason",
		"first_observed_at_ms", "last_observed_at_ms", "sample_count", "request_id", "session_id",
		"source_file_id", "first_source_generation", "first_source_offset", "source_generation", "source_offset",
	}); err != nil {
		return err
	}
	if err := ensureNamedObjects(ctx, transaction, quotaSchemaObjectsV32, "table", "quota_observation_receipts"); err != nil {
		return err
	}
	if err := copyTableColumns(ctx, transaction, "quota_observation_receipts_v31", "quota_observation_receipts", []string{
		"observation_id", "segment_observation_id", "sample_sha256",
	}); err != nil {
		return err
	}
	if err := ensureNamedObjects(ctx, transaction, quotaProjectionSchemaObjectsV32, "table", "quota_current"); err != nil {
		return err
	}
	if err := copyTableColumns(ctx, transaction, "quota_current_v31", "quota_current", []string{
		"account_scope", "window_kind", "limit_id", "observation_id", "effective_used_percent",
		"window_minutes", "resets_at_ms", "window_generation", "selected_source", "freshness_state",
		"conflict_state", "fresh_until_ms", "last_success_at_ms", "last_attempt_at_ms",
		"rule_version", "explanation_code", "evaluated_at_ms",
	}); err != nil {
		return err
	}
	if err := ensureNamedObjects(ctx, transaction, quotaProjectionSchemaObjectsV32, "table", "quota_arbitration_evidence"); err != nil {
		return err
	}
	if err := copyTableColumns(ctx, transaction, "quota_arbitration_evidence_v31", "quota_arbitration_evidence", []string{
		"account_scope", "window_kind", "limit_id", "observation_id", "window_generation",
		"disposition", "reason", "explanation_code",
	}); err != nil {
		return err
	}
	if err := dropTablesForV32(ctx, transaction, []string{
		"quota_arbitration_evidence_v31",
		"quota_current_v31",
		"quota_observation_receipts_v31",
		"quota_observations_v31",
	}); err != nil {
		return err
	}
	if err := ensureObjectTypes(ctx, transaction, quotaSchemaObjectsV32, "index"); err != nil {
		return err
	}
	if err := ensureObjectTypes(ctx, transaction, quotaProjectionSchemaObjectsV32, "index"); err != nil {
		return err
	}
	return storeschema.EnsureObjects(ctx, transaction, quotaPerformanceSchemaObjects)
}

func rebuildSourceRefreshAndResetCreditsForV32(ctx context.Context, transaction *gorm.DB) error {
	renames := []string{
		"reset_credits",
		"reset_credit_snapshots",
		"source_refresh_claims",
		"source_refresh_schedules",
		"source_attempts",
	}
	if err := renameTablesForV32(ctx, transaction, renames); err != nil {
		return err
	}
	attempts := sourceAttemptsSchemaObjectV32()
	if attempts.Name == "" {
		return fmt.Errorf("%w: source_attempts v32 statement is missing", ErrMigrationContract)
	}
	if err := storeschema.EnsureObjects(ctx, transaction, []storeschema.Object{attempts}); err != nil {
		return err
	}
	if err := transaction.WithContext(ctx).Exec(`
		INSERT INTO source_attempts (
			request_id, source_instance_id, started_at_ms, finished_at_ms, outcome,
			http_status, error_class, payload_sha256, failure_code, attempt_count,
			response_bytes, retry_at_ms, binding_generation
		)
		SELECT
			request_id, source_instance_id, started_at_ms, finished_at_ms, outcome,
			http_status, error_class, payload_sha256, failure_code, attempt_count,
			response_bytes, retry_at_ms, 0
		FROM source_attempts_v31
	`).Error; err != nil {
		return fmt.Errorf("%w: copy source_attempts: %v", ErrMigrationContract, err)
	}
	if err := ensureNamedObjects(ctx, transaction, quotaScheduleSchemaObjectsV32, "table", "source_refresh_schedules"); err != nil {
		return err
	}
	if err := transaction.WithContext(ctx).Exec(`
		INSERT INTO source_refresh_schedules (
			source_instance_id, source_type, scope_key, binding_generation, next_due_at_ms, reason,
			last_manual_at_ms, active_claim_id, active_trigger, claim_started_at_ms,
			claim_expires_at_ms, revision, updated_at_ms
		)
		SELECT
			source_instance_id, source_type, scope_key, 0, next_due_at_ms, reason,
			last_manual_at_ms, active_claim_id, active_trigger, claim_started_at_ms,
			claim_expires_at_ms, revision, updated_at_ms
		FROM source_refresh_schedules_v31
	`).Error; err != nil {
		return fmt.Errorf("%w: copy source_refresh_schedules: %v", ErrMigrationContract, err)
	}
	if err := ensureNamedObjects(ctx, transaction, quotaScheduleSchemaObjectsV32, "table", "source_refresh_claims"); err != nil {
		return err
	}
	if err := transaction.WithContext(ctx).Exec(`
		INSERT INTO source_refresh_claims (
			claim_id, source_instance_id, schedule_revision, binding_generation, trigger,
			started_at_ms, expires_at_ms, state, finalized_at_ms
		)
		SELECT
			claim_id, source_instance_id, schedule_revision, 0, trigger,
			started_at_ms, expires_at_ms, state, finalized_at_ms
		FROM source_refresh_claims_v31
	`).Error; err != nil {
		return fmt.Errorf("%w: copy source_refresh_claims: %v", ErrMigrationContract, err)
	}
	if err := ensureNamedObjects(ctx, transaction, quotaScheduleSchemaObjectsV32, "table", "reset_credit_snapshots"); err != nil {
		return err
	}
	if err := copyTableColumns(ctx, transaction, "reset_credit_snapshots_v31", "reset_credit_snapshots", []string{
		"snapshot_id", "request_id", "account_scope", "available_count", "observed_at_ms",
	}); err != nil {
		return err
	}
	if err := ensureNamedObjects(ctx, transaction, quotaScheduleSchemaObjectsV32, "table", "reset_credits"); err != nil {
		return err
	}
	if err := copyTableColumns(ctx, transaction, "reset_credits_v31", "reset_credits", []string{
		"snapshot_id", "credit_id_hash", "status", "reset_type", "granted_at_ms",
		"expires_at_ms", "redeemed_at_ms",
	}); err != nil {
		return err
	}
	if err := dropTablesForV32(ctx, transaction, []string{
		"reset_credits_v31",
		"reset_credit_snapshots_v31",
		"source_refresh_claims_v31",
		"source_refresh_schedules_v31",
		"source_attempts_v31",
	}); err != nil {
		return err
	}
	if err := ensureObjectTypes(ctx, transaction, quotaScheduleSchemaObjectsV32, "index"); err != nil {
		return err
	}
	if err := storeschema.EnsureObjects(ctx, transaction, []storeschema.Object{attempts}); err != nil {
		return err
	}
	for _, object := range currentRuntimeSchemaObjects() {
		if object.ObjectType == "index" && object.Name == "idx_source_attempts_history" {
			if err := storeschema.EnsureObjects(ctx, transaction, []storeschema.Object{object}); err != nil {
				return err
			}
		}
	}
	return storeschema.EnsureObjects(ctx, transaction, storeretention.SchemaObjects())
}

func sealLegacyCodexSchedulesForV32(ctx context.Context, transaction *gorm.DB) error {
	database := transaction.WithContext(ctx)
	if err := database.Exec(`
		UPDATE source_refresh_claims
		SET state = 'abandoned', finalized_at_ms = CASE
			WHEN finalized_at_ms IS NULL THEN started_at_ms
			ELSE finalized_at_ms
		END
		WHERE state = 'active'
	`).Error; err != nil {
		return fmt.Errorf("%w: abandon active refresh claims: %v", ErrMigrationContract, err)
	}
	if err := database.Exec(`
		UPDATE source_refresh_schedules
		SET
			active_claim_id = NULL,
			active_trigger = NULL,
			claim_started_at_ms = NULL,
			claim_expires_at_ms = NULL,
			reason = 'disabled',
			next_due_at_ms = NULL
		WHERE scope_key = 'default'
	`).Error; err != nil {
		return fmt.Errorf("%w: disable default refresh schedules: %v", ErrMigrationContract, err)
	}
	return nil
}

func renameTablesForV32(ctx context.Context, transaction *gorm.DB, tables []string) error {
	for _, table := range tables {
		if err := transaction.WithContext(ctx).Exec(
			"ALTER TABLE " + table + " RENAME TO " + table + "_v31",
		).Error; err != nil {
			return fmt.Errorf("%w: rename %s: %v", ErrMigrationContract, table, err)
		}
	}
	return nil
}

func dropTablesForV32(ctx context.Context, transaction *gorm.DB, tables []string) error {
	for _, table := range tables {
		if err := transaction.WithContext(ctx).Exec("DROP TABLE " + table).Error; err != nil {
			return fmt.Errorf("%w: drop %s: %v", ErrMigrationContract, table, err)
		}
	}
	return nil
}

func copyTableColumns(ctx context.Context, transaction *gorm.DB, from, to string, columns []string) error {
	list := joinSQLIdentifiers(columns)
	if err := transaction.WithContext(ctx).Exec(
		"INSERT INTO " + to + " (" + list + ") SELECT " + list + " FROM " + from,
	).Error; err != nil {
		return fmt.Errorf("%w: copy %s: %v", ErrMigrationContract, to, err)
	}
	return nil
}

func joinSQLIdentifiers(columns []string) string {
	result := columns[0]
	for _, column := range columns[1:] {
		result += ", " + column
	}
	return result
}

func ensureNamedObjects(ctx context.Context, transaction *gorm.DB, objects []storeschema.Object, objectType, name string) error {
	for _, object := range objects {
		if object.ObjectType == objectType && object.Name == name {
			return storeschema.EnsureObjects(ctx, transaction, []storeschema.Object{object})
		}
	}
	return fmt.Errorf("%w: missing %s %q", ErrMigrationContract, objectType, name)
}

func ensureObjectTypes(ctx context.Context, transaction *gorm.DB, objects []storeschema.Object, objectType string) error {
	var selected []storeschema.Object
	for _, object := range objects {
		if object.ObjectType == objectType {
			selected = append(selected, object)
		}
	}
	if len(selected) == 0 {
		return nil
	}
	return storeschema.EnsureObjects(ctx, transaction, selected)
}

func verifyNoForeignKeyViolations(ctx context.Context, transaction *gorm.DB) error {
	var violations []struct {
		Table  string
		Rowid  int64
		Parent string
		Fkid   int64
	}
	if err := transaction.WithContext(ctx).Raw("PRAGMA foreign_key_check").Scan(&violations).Error; err != nil {
		return fmt.Errorf("%w: foreign key check: %v", ErrMigrationContract, err)
	}
	if len(violations) > 0 {
		return fmt.Errorf("%w: foreign key check failed on %s", ErrMigrationContract, violations[0].Table)
	}
	return nil
}
