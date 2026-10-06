package store

import reportingv1 "github.com/SisyphusSQ/codex-pulse/api/codexpulse/reporting/v1"

type DSHSession struct {
	Throughput         *reportingv1.ThroughputCapsule
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

type DSHSessionLineage struct {
	ExternalSessionID string
	SourceKey         string
	LineageKey        string
	ContentDigest     string
	ObservedAtMS      int64
}

type DSHUsageEvent struct {
	ModelProvider   string
	CacheReadKnown  bool
	CacheWriteKnown bool
	ReasoningKnown  bool
	TotalKnown      bool
	StartedAtMS     *int64
	EndedAtMS       *int64

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

type DSHToolEvent struct {
	EventID           string
	ExternalSessionID string
	OccurredAtMS      int64
	ToolName          string
	Outcome           string
	UpdatedAtMS       int64
}

type DSHSnapshot struct {
	Generation    int64
	CollectedAtMS int64
	Sources       []CursorSourceStatus
	Sessions      []DSHSession
	Lineage       []DSHSessionLineage
	UsageEvents   []DSHUsageEvent
	ToolEvents    []DSHToolEvent
}
