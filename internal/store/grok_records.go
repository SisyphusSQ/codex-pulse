package store

type GrokSession struct {
	ExternalSessionID  string
	DisplayTitle       string
	TitleSource        string
	ProjectKey         string
	ProjectDisplayName string
	CreatedAtMS        int64
	LastActivityAtMS   int64
	ModelKey           *string
	RequestCount       int64
	ToolCallCount      int64
	LineageConflict    bool
	CoverageState      string
	UpdatedAtMS        int64
}

type GrokSessionLineage struct {
	ExternalSessionID string
	SourceKey         string
	LineageKey        string
	ContentDigest     string
	ObservedAtMS      int64
}

type GrokUsageEvent struct {
	EventID             string
	ExternalSessionID   string
	OccurredAtMS        int64
	ModelKey            *string
	InputTokens         int64
	OutputTokens        int64
	CachedReadTokens    int64
	CacheCreationTokens int64
	ReasoningTokens     int64
	TotalTokens         int64
	ReportedCostMicros  *int64
	UpdatedAtMS         int64
}

type GrokToolEvent struct {
	EventID           string
	ExternalSessionID string
	OccurredAtMS      int64
	ToolName          string
	Outcome           string
	UpdatedAtMS       int64
}

type GrokBillingSnapshot struct {
	Generation        int64
	CollectedAtMS     int64
	PeriodType        string
	PeriodStartMS     int64
	PeriodEndMS       int64
	UsedPercent       float64
	OnDemandUsed      *float64
	OnDemandCap       *float64
	PrepaidBalance    *float64
	SubscriptionTier  *string
	IsUnifiedBilling  bool
	QuotaObservations []GrokBillingQuotaObservation
}

type GrokBillingQuotaObservation struct {
	Generation     int64
	LimitID        string
	UsedPercent    float64
	CycleStartAtMS int64
	CycleEndAtMS   int64
	ObservedAtMS   int64
}

type GrokSnapshot struct {
	Generation         int64
	CollectedAtMS      int64
	Sources            []CursorSourceStatus
	Sessions           []GrokSession
	Lineage            []GrokSessionLineage
	UsageEvents        []GrokUsageEvent
	ToolEvents         []GrokToolEvent
	Billing            *GrokBillingSnapshot
	BillingStale       bool
	BillingFailureCode *string
}
