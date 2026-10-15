package store

import (
	"context"
	"crypto/sha256"
	"fmt"

	"gorm.io/gorm"

	storeschema "github.com/SisyphusSQ/codex-pulse/internal/store/schema"
)

var quotaRefreshFenceSchemaObject = storeschema.Object{
	ObjectType: "table", Name: "source_refresh_global_fences",
	Statement: `CREATE TABLE IF NOT EXISTS source_refresh_global_fences (
		source_group TEXT PRIMARY KEY CHECK (source_group = 'codex_app_server_rate_limits'),
		not_before_ms INTEGER NOT NULL CHECK (not_before_ms >= 0),
		reason TEXT NOT NULL CHECK (reason IN ('manual_interval', 'network_backoff', 'retry_after')),
		updated_at_ms INTEGER NOT NULL CHECK (updated_at_ms >= 0)
	) STRICT`,
}

func currentAccountBindingSchemaObjects() []storeschema.Object {
	objects := append([]storeschema.Object(nil), accountBindingSchemaObjects...)
	for i := range objects {
		if objects[i].Name == quotaRefreshFenceSchemaObject.Name {
			objects[i] = quotaRefreshFenceSchemaObject
		}
	}
	return objects
}

func applicationSchemaV38Checksum() string {
	return fmt.Sprintf("%x", sha256.Sum256([]byte("38/quota-refresh-server-fence/v1\n"+quotaRefreshFenceSchemaObject.Statement)))
}

func migrateQuotaRefreshFenceForV38(ctx context.Context, transaction *gorm.DB) error {
	database := transaction.WithContext(ctx)
	if err := database.Exec("ALTER TABLE source_refresh_global_fences RENAME TO source_refresh_global_fences_v37").Error; err != nil {
		return err
	}
	if err := storeschema.EnsureObject(ctx, database, quotaRefreshFenceSchemaObject); err != nil {
		return err
	}
	// 旧版本把普通网络退避与服务端限流混存在同一行。
	// 手动间隔原样保留；服务端等待从明确 429 事实重建。
	if err := database.Exec(`INSERT INTO source_refresh_global_fences
		SELECT source_group, not_before_ms, reason, updated_at_ms
		FROM source_refresh_global_fences_v37
		WHERE reason = 'manual_interval'`).Error; err != nil {
		return err
	}
	var notBeforeMS, updatedAtMS int64
	if err := database.Raw(`SELECT COALESCE(MAX(MAX(
		COALESCE(s.next_due_at_ms, 0),
		COALESCE(CASE WHEN q.reason IN ('network_backoff', 'retry_after') THEN q.next_due_at_ms END, 0),
		COALESCE(CASE WHEN g.reason = 'network_backoff' THEN g.not_before_ms END, 0)
	)), 0), COALESCE(MAX(s.updated_at_ms), 0)
		FROM source_state s
		LEFT JOIN source_refresh_schedules q ON q.source_instance_id = s.source_instance_id
		LEFT JOIN source_refresh_global_fences_v37 g ON g.source_group = 'codex_app_server_rate_limits'
		WHERE s.source_type IN ('app_server_rate_limits', 'app_server_reset_credits')
		AND s.last_failure_code = 'http_429'`).Row().Scan(&notBeforeMS, &updatedAtMS); err != nil {
		return err
	}
	if notBeforeMS > 0 {
		if err := raiseSourceRefreshGlobalFence(ctx, database, notBeforeMS, sourceRefreshGlobalFenceRateLimit, updatedAtMS); err != nil {
			return err
		}
	}
	return database.Exec("DROP TABLE source_refresh_global_fences_v37").Error
}
