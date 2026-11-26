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

// ReportingAGYAccountRanges builds disjoint confirmed AGY account intervals from
// consecutive same-account quota observations. Any switch gap or same-timestamp
// multi-account ambiguity is left unattributed.
func (r *Repository) ReportingAGYAccountRanges(ctx context.Context, start, now int64) (ranges []ReportingAccountRange, err error) {
	if r == nil || r.database == nil {
		return nil, ErrInvalidRepository
	}
	err = r.database.ViewSnapshot(ctx, func(_ context.Context, db *gorm.DB) error {
		var rows []struct {
			AccountScope string
			ObservedAtMS int64
		}
		if err := db.Raw(`
			SELECT DISTINCT account_scope, observed_at_ms
			FROM agy_quota_observations
			WHERE observed_at_ms BETWEEN ? AND ?
			ORDER BY observed_at_ms, account_scope
		`, start, now).Scan(&rows).Error; err != nil {
			return err
		}
		var current *ReportingAccountRange
		var generation int64
		flush := func() {
			if current != nil {
				ranges = append(ranges, *current)
				current = nil
			}
		}
		for i := 0; i < len(rows); {
			at := rows[i].ObservedAtMS
			j := i + 1
			for j < len(rows) && rows[j].ObservedAtMS == at {
				j++
			}
			// Multiple accounts at the exact same timestamp cannot prove attribution.
			if j-i > 1 || !validHexDigest(rows[i].AccountScope) {
				flush()
				i = j
				continue
			}
			scope := rows[i].AccountScope
			if current != nil && current.AccountScope == scope {
				current.ThroughMS = at
			} else {
				flush()
				generation++
				current = &ReportingAccountRange{
					AccountScope:      scope,
					BindingGeneration: generation,
					FromMS:            at,
					ThroughMS:         at,
				}
			}
			i = j
		}
		flush()
		return nil
	})
	return
}
