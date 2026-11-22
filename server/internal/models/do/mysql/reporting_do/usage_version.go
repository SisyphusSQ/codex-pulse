package reporting_do

// UsageVersion 在发布指针切换前不可见。
type UsageVersion struct {
	Generation string `gorm:"primaryKey"`
	Usage      `gorm:"embedded"`
}

func (UsageVersion) TableName() string { return "pulse_usage_versions" }

// Usage 是版本表的事实字段，也是统计流读取的投影。
type Usage struct {
	SessionKey             string `gorm:"primaryKey"`
	ContributionID         string `gorm:"primaryKey"`
	Position               int64
	ObservedAtMS           *int64
	Model                  *string
	InputTokens            *int64
	CachedTokens           *int64
	CacheWriteTokens       *int64
	OutputTokens           *int64
	ReasoningTokens        *int64
	TotalTokens            *int64
	CostMicroUSD           *int64
	ReportedChargeMicroUSD *int64
	PricingVersion         *string
	PricingMode            string
	InputPrice             *int64
	CachedPrice            *int64
	CacheWritePrice        *int64
	OutputPrice            *int64
	CostStatus             string
}
