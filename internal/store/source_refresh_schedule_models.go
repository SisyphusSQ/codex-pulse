package store

type sourceRefreshScheduleModel struct {
	SourceInstanceID string  `gorm:"column:source_instance_id;primaryKey"`
	SourceType       string  `gorm:"column:source_type"`
	ScopeKey         string  `gorm:"column:scope_key"`
	NextDueAtMS      *int64  `gorm:"column:next_due_at_ms"`
	Reason           string  `gorm:"column:reason"`
	LastManualAtMS   *int64  `gorm:"column:last_manual_at_ms"`
	ActiveClaimID    *string `gorm:"column:active_claim_id"`
	ActiveTrigger    *string `gorm:"column:active_trigger"`
	ClaimStartedAtMS *int64  `gorm:"column:claim_started_at_ms"`
	ClaimExpiresAtMS *int64  `gorm:"column:claim_expires_at_ms"`
	Revision         int64   `gorm:"column:revision"`
	UpdatedAtMS      int64   `gorm:"column:updated_at_ms"`
}

func (sourceRefreshScheduleModel) TableName() string { return "source_refresh_schedules" }

type sourceRefreshClaimModel struct {
	ClaimID          string `gorm:"column:claim_id;primaryKey"`
	SourceInstanceID string `gorm:"column:source_instance_id"`
	ScheduleRevision int64  `gorm:"column:schedule_revision"`
	Trigger          string `gorm:"column:trigger"`
	StartedAtMS      int64  `gorm:"column:started_at_ms"`
	ExpiresAtMS      int64  `gorm:"column:expires_at_ms"`
	State            string `gorm:"column:state"`
	FinalizedAtMS    *int64 `gorm:"column:finalized_at_ms"`
}

func (sourceRefreshClaimModel) TableName() string { return "source_refresh_claims" }
