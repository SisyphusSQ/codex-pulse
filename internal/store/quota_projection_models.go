package store

type quotaCurrentModel struct {
	AccountScope         string   `gorm:"column:account_scope;primaryKey"`
	WindowKind           string   `gorm:"column:window_kind;primaryKey"`
	LimitID              string   `gorm:"column:limit_id;primaryKey"`
	ObservationID        *string  `gorm:"column:observation_id"`
	EffectiveUsedPercent *float64 `gorm:"column:effective_used_percent"`
	WindowMinutes        *int64   `gorm:"column:window_minutes"`
	ResetsAtMS           *int64   `gorm:"column:resets_at_ms"`
	WindowGeneration     *int64   `gorm:"column:window_generation"`
	SelectedSource       *string  `gorm:"column:selected_source"`
	FreshnessState       string   `gorm:"column:freshness_state"`
	ConflictState        string   `gorm:"column:conflict_state"`
	FreshUntilMS         *int64   `gorm:"column:fresh_until_ms"`
	LastSuccessAtMS      *int64   `gorm:"column:last_success_at_ms"`
	LastAttemptAtMS      *int64   `gorm:"column:last_attempt_at_ms"`
	RuleVersion          string   `gorm:"column:rule_version"`
	ExplanationCode      string   `gorm:"column:explanation_code"`
	EvaluatedAtMS        int64    `gorm:"column:evaluated_at_ms"`
}

func (quotaCurrentModel) TableName() string { return "quota_current" }

type quotaArbitrationEvidenceModel struct {
	AccountScope     string  `gorm:"column:account_scope;primaryKey"`
	WindowKind       string  `gorm:"column:window_kind;primaryKey"`
	LimitID          string  `gorm:"column:limit_id;primaryKey"`
	ObservationID    string  `gorm:"column:observation_id;primaryKey"`
	WindowGeneration int64   `gorm:"column:window_generation"`
	Disposition      string  `gorm:"column:disposition"`
	Reason           *string `gorm:"column:reason"`
	ExplanationCode  string  `gorm:"column:explanation_code"`
}

func (quotaArbitrationEvidenceModel) TableName() string { return "quota_arbitration_evidence" }

type quotaProjectionKeyModel struct {
	AccountScope string `gorm:"column:account_scope"`
	WindowKind   string `gorm:"column:window_kind"`
	LimitID      string `gorm:"column:limit_id"`
}
