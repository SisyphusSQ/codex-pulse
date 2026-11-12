package store

import (
	"context"

	"gorm.io/gorm"
)

// ReportingAccountRange is bounded by successful quota observations from one confirmed binding.
type ReportingAccountRange struct {
	AccountScope      string
	BindingGeneration int64
	FromMS, ThroughMS int64
}

// ReportingAccountRanges reuses durable quota requests, including requests recorded before an upgrade.
func (r *Repository) ReportingAccountRanges(ctx context.Context, start, now int64) (ranges []ReportingAccountRange, err error) {
	err = r.database.ViewSnapshot(ctx, func(ctx context.Context, db *gorm.DB) error {
		return db.Raw(`
			SELECT q.account_scope, c.binding_generation,
				MIN(q.first_observed_at_ms) AS from_ms, MAX(q.first_observed_at_ms) AS through_ms
			FROM quota_observations q
			JOIN source_refresh_claims c ON c.claim_id = q.request_id
			JOIN source_refresh_schedules s ON s.source_instance_id = c.source_instance_id
			WHERE s.source_type = 'app_server_rate_limits' AND s.scope_key = q.account_scope
				AND c.state = 'completed' AND c.binding_generation > 0 AND q.account_scope <> 'default'
				AND q.source = 'app_server' AND q.validity = 'accepted'
				AND q.limit_id = 'codex' AND q.window_minutes = 10080
				AND q.first_observed_at_ms = q.last_observed_at_ms
				AND q.first_observed_at_ms BETWEEN c.started_at_ms AND c.expires_at_ms
				AND q.first_observed_at_ms BETWEEN ? AND ?
			GROUP BY q.account_scope, c.binding_generation
			ORDER BY from_ms, through_ms, q.account_scope, c.binding_generation
		`, start, now).Scan(&ranges).Error
	})
	return
}
