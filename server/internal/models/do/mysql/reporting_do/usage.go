package reporting_do

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

func (Usage) TableName() string { return "pulse_usage" }
