package store

import reportingv1 "github.com/SisyphusSQ/codex-pulse/api/codexpulse/reporting/v1"

// AGYSession 表示 Antigravity 单会话（主会话或子代理/Swarm/Side-Question）的脱敏事实摘要。
type AGYSession struct {
	ExternalSessionID       string
	DisplayTitle            string
	TitleSource             string
	ParentExternalSessionID *string
	SessionRole             string
	ProjectKey              string
	ProjectDisplayName      string
	ProjectSource           string
	CreatedAtMS             int64
	LastActivityAtMS        int64
	ModelKey                *string
	RequestCount            int64
	LineageConflict         bool
	CoverageState           string
	Throughput              *reportingv1.ThroughputCapsule
	UpdatedAtMS             int64
}

// AGYSessionLineage 记录 AGY 会话在 4 个产品目录中的来源血缘与内容摘要。
type AGYSessionLineage struct {
	ExternalSessionID string
	SourceKey         string
	LineageKey        string
	ContentDigest     string
	ObservedAtMS      int64
}

// AGYUsageEvent 保存 AGY 单步 Token 与计时原样事实，不在入库时预存估算费用。
type AGYUsageEvent struct {
	EventID             string
	ExternalSessionID   string
	StepIndex           int64
	OccurredAtMS        int64
	ModelKey            *string
	UncachedInputTokens int64
	CacheReadTokens     int64
	OutputTokens        int64
	ReasoningTokens     int64
	TotalTokens         int64
	CacheReadKnown      bool
	ReasoningKnown      bool
	TotalKnown          bool
	StartedAtMS         *int64
	EndedAtMS           *int64
	Provenance          string
	UpdatedAtMS         int64
}

// AGYSnapshot 表示 AGY 本地轨迹采集的一次原子落盘批次。
type AGYSnapshot struct {
	Generation    int64
	CollectedAtMS int64
	Sources       []CursorSourceStatus
	Sessions      []AGYSession
	Lineage       []AGYSessionLineage
	UsageEvents   []AGYUsageEvent
}

// AGYQuotaSnapshot 保存某账号摘要下单个配额池与窗口的最近已知快照。
type AGYQuotaSnapshot struct {
	AccountScope  string
	Generation    int64
	PlanType      *string
	LimitID       string
	LimitName     string
	WindowKind    string
	WindowMinutes int64
	UsedPercent   *int64
	ResetsAtMS    *int64
	DataAsOfMS    int64
	UpdatedAtMS   int64
}

// AGYQuotaObservation 保存某账号配额窗口的一条有界历史观测点，用于计算配额节奏。
type AGYQuotaObservation struct {
	AccountScope string
	LimitID      string
	WindowKind   string
	ObservedAtMS int64
	UsedPercent  int64
	ResetsAtMS   *int64
}
