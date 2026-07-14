package store

type costRollupGenerationModel struct {
	GenerationID      string `gorm:"column:generation_id;primaryKey"`
	ReportingTimezone string `gorm:"column:reporting_timezone"`
	PricingSource     string `gorm:"column:pricing_source"`
	Currency          string `gorm:"column:currency"`
	RollupVersion     int64  `gorm:"column:rollup_version"`
	State             string `gorm:"column:state"`
	CreatedAtMS       int64  `gorm:"column:created_at_ms"`
	CompletedAtMS     *int64 `gorm:"column:completed_at_ms"`
	UpdatedAtMS       int64  `gorm:"column:updated_at_ms"`
}

func (costRollupGenerationModel) TableName() string { return "cost_rollup_generations" }

type turnCostModel struct {
	GenerationID       string  `gorm:"column:generation_id;primaryKey"`
	TurnID             string  `gorm:"column:turn_id;primaryKey"`
	PricingVersion     *string `gorm:"column:pricing_version"`
	EstimatedUSDMicros *int64  `gorm:"column:estimated_usd_micros"`
	PricingStatus      string  `gorm:"column:pricing_status"`
	PricingReason      string  `gorm:"column:pricing_reason"`
	CalculatedAtMS     int64   `gorm:"column:calculated_at_ms"`
}

func (turnCostModel) TableName() string { return "turn_costs" }

type rollupTotalsModel struct {
	TurnCount          int64  `gorm:"column:turn_count"`
	InputTokens        *int64 `gorm:"column:input_tokens"`
	CachedInputTokens  *int64 `gorm:"column:cached_input_tokens"`
	OutputTokens       *int64 `gorm:"column:output_tokens"`
	ReasoningTokens    *int64 `gorm:"column:reasoning_tokens"`
	TotalTokens        *int64 `gorm:"column:total_tokens"`
	EstimatedUSDMicros *int64 `gorm:"column:estimated_usd_micros"`
	PricedTurnCount    int64  `gorm:"column:priced_turn_count"`
	UnpricedTurnCount  int64  `gorm:"column:unpriced_turn_count"`
	FirstActivityAtMS  int64  `gorm:"column:first_activity_at_ms"`
	LastActivityAtMS   int64  `gorm:"column:last_activity_at_ms"`
	UpdatedAtMS        int64  `gorm:"column:updated_at_ms"`
}

type sessionUsageRollupModel struct {
	GenerationID string            `gorm:"column:generation_id;primaryKey"`
	SessionID    string            `gorm:"column:session_id;primaryKey"`
	Totals       rollupTotalsModel `gorm:"embedded"`
}

func (sessionUsageRollupModel) TableName() string { return "session_usage_rollups" }

type usageDailyModel struct {
	GenerationID      string            `gorm:"column:generation_id;primaryKey"`
	BucketStartMS     int64             `gorm:"column:bucket_start_ms;primaryKey"`
	ReportingTimezone string            `gorm:"column:reporting_timezone"`
	Totals            rollupTotalsModel `gorm:"embedded"`
}

func (usageDailyModel) TableName() string { return "usage_daily" }

type projectUsageDailyModel struct {
	GenerationID          string            `gorm:"column:generation_id;primaryKey"`
	BucketStartMS         int64             `gorm:"column:bucket_start_ms;primaryKey"`
	ReportingTimezone     string            `gorm:"column:reporting_timezone"`
	DimensionKey          string            `gorm:"column:dimension_key;primaryKey"`
	ProjectID             *string           `gorm:"column:project_id"`
	ProjectDisplayName    *string           `gorm:"column:project_display_name"`
	AttributionConfidence string            `gorm:"column:attribution_confidence"`
	AttributionSource     string            `gorm:"column:attribution_source"`
	AttributionReason     string            `gorm:"column:attribution_reason"`
	Totals                rollupTotalsModel `gorm:"embedded"`
}

func (projectUsageDailyModel) TableName() string { return "project_usage_daily" }

type modelUsageDailyModel struct {
	GenerationID          string            `gorm:"column:generation_id;primaryKey"`
	BucketStartMS         int64             `gorm:"column:bucket_start_ms;primaryKey"`
	ReportingTimezone     string            `gorm:"column:reporting_timezone"`
	DimensionKey          string            `gorm:"column:dimension_key;primaryKey"`
	ModelKey              *string           `gorm:"column:model_key"`
	ModelDisplayName      *string           `gorm:"column:model_display_name"`
	AttributionConfidence string            `gorm:"column:attribution_confidence"`
	AttributionSource     string            `gorm:"column:attribution_source"`
	AttributionReason     string            `gorm:"column:attribution_reason"`
	Totals                rollupTotalsModel `gorm:"embedded"`
}

func (modelUsageDailyModel) TableName() string { return "model_usage_daily" }
