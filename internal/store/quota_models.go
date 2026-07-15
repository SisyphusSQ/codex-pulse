package store

type quotaObservationModel struct {
	ObservationID         string  `gorm:"column:observation_id;primaryKey"`
	AccountScope          string  `gorm:"column:account_scope"`
	Source                string  `gorm:"column:source"`
	LimitID               *string `gorm:"column:limit_id"`
	WindowKind            string  `gorm:"column:window_kind"`
	UsedPercent           float64 `gorm:"column:used_percent"`
	WindowMinutes         int64   `gorm:"column:window_minutes"`
	ResetsAtMS            int64   `gorm:"column:resets_at_ms"`
	PlanType              *string `gorm:"column:plan_type"`
	Validity              string  `gorm:"column:validity"`
	RejectionReason       *string `gorm:"column:rejection_reason"`
	FirstObservedAtMS     int64   `gorm:"column:first_observed_at_ms"`
	LastObservedAtMS      int64   `gorm:"column:last_observed_at_ms"`
	SampleCount           int64   `gorm:"column:sample_count"`
	RequestID             *string `gorm:"column:request_id"`
	SessionID             *string `gorm:"column:session_id"`
	SourceFileID          *string `gorm:"column:source_file_id"`
	FirstSourceGeneration int64   `gorm:"column:first_source_generation"`
	FirstSourceOffset     int64   `gorm:"column:first_source_offset"`
	SourceGeneration      int64   `gorm:"column:source_generation"`
	SourceOffset          int64   `gorm:"column:source_offset"`
}

func (quotaObservationModel) TableName() string { return "quota_observations" }

type quotaObservationReceiptModel struct {
	ObservationID        string `gorm:"column:observation_id;primaryKey"`
	SegmentObservationID string `gorm:"column:segment_observation_id"`
	SampleSHA256         string `gorm:"column:sample_sha256"`
}

func (quotaObservationReceiptModel) TableName() string { return "quota_observation_receipts" }
