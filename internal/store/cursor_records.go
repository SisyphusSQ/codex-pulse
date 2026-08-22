package store

type CursorSourceStatus struct {
	Provider        string
	SourceKey       string
	SourceType      string
	State           string
	CoverageState   string
	SchemaVersion   *int64
	CheckpointKind  string
	CheckpointValue *string
	RowCount        int64
	LastAttemptAtMS int64
	LastSuccessAtMS *int64
	FailureCode     *string
	UpdatedAtMS     int64
}

type CursorSession struct {
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
	AIEditCount        int64
	AILinesAdded       *int64
	AILinesRemoved     *int64
	LineageConflict    bool
	CoverageState      string
	UpdatedAtMS        int64
}

type CursorSessionLineage struct {
	ExternalSessionID string
	SourceKey         string
	LineageKey        string
	ContentDigest     string
	ObservedAtMS      int64
}

type CursorUsageEvent struct {
	EventID           string
	ExternalSessionID string
	OccurredAtMS      int64
	ModelKey          *string
	InputTokens       int64
	OutputTokens      int64
	UpdatedAtMS       int64
}

type CursorDashboardUsageEvent struct {
	EventFingerprint     string
	OccurrenceCount      int64
	ExternalSessionID    *string
	OccurredAtMS         int64
	ModelKey             *string
	Kind                 int64
	TokenBased           bool
	InputTokens          int64
	OutputTokens         int64
	CacheWriteTokens     int64
	CacheReadTokens      int64
	ReportedChargeMicros int64
	CursorTokenFeeMicros int64
	UpdatedAtMS          int64
}

type CursorDashboardPlanUsage struct {
	TotalSpendMicros    int64
	IncludedSpendMicros int64
	BonusSpendMicros    int64
	RemainingMicros     int64
	LimitMicros         int64
}

type CursorDashboardQuotaWindow struct {
	LimitID        string
	UsedPercent    float64
	CycleStartAtMS int64
	CycleEndAtMS   int64
}

type CursorGrokBotCommit struct {
	Generation     int64
	CollectedAtMS  int64
	Included       bool
	UsedPercent    *float64
	CycleStartAtMS int64
	CycleEndAtMS   int64
}

type CursorDashboardQuotaObservation struct {
	Generation     int64
	LimitID        string
	UsedPercent    float64
	CycleStartAtMS int64
	CycleEndAtMS   int64
	ObservedAtMS   int64
}

type CursorDashboardSnapshot struct {
	Generation        int64
	CollectedAtMS     int64
	WindowStartMS     int64
	WindowEndMS       int64
	BillingCycleEndMS int64
	PlanUsage         *CursorDashboardPlanUsage
	QuotaWindows      []CursorDashboardQuotaWindow
	Events            []CursorDashboardUsageEvent
}

type CursorRequestEvent struct {
	EventID           string
	ExternalSessionID string
	OccurredAtMS      int64
	UpdatedAtMS       int64
}

type CursorToolEvent struct {
	EventID           string
	ExternalSessionID string
	OccurredAtMS      int64
	ToolName          string
	Outcome           string
	Provenance        string
	UpdatedAtMS       int64
}

type CursorAIEditEvent struct {
	EventID           string
	ExternalSessionID *string
	OccurredAtMS      int64
	ModelKey          *string
	EditCount         int64
	UpdatedAtMS       int64
}

type CursorSnapshot struct {
	Generation                 int64
	CollectedAtMS              int64
	Sources                    []CursorSourceStatus
	Sessions                   []CursorSession
	Lineage                    []CursorSessionLineage
	UsageEvents                []CursorUsageEvent
	DashboardGeneration        int64
	DashboardCollectedAtMS     int64
	DashboardWindowStartMS     int64
	DashboardWindowEndMS       int64
	DashboardBillingCycleEndMS int64
	DashboardPlanUsage         *CursorDashboardPlanUsage
	DashboardQuotaObservations []CursorDashboardQuotaObservation
	DashboardUsageEvents       []CursorDashboardUsageEvent
	RequestEvents              []CursorRequestEvent
	ToolEvents                 []CursorToolEvent
	AIEditEvents               []CursorAIEditEvent
}
