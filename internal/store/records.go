package store

import "errors"

var (
	// ErrInvalidRepository 表示 Repository 未绑定可用的应用 Store。
	ErrInvalidRepository = errors.New("invalid fact repository")
	// ErrInvalidRecord 表示待写入事实不满足字段或引用 contract。
	ErrInvalidRecord = errors.New("invalid fact record")
	// ErrNotFound 表示 typed query 没有找到目标事实。
	ErrNotFound = errors.New("fact record not found")
	// ErrSourceRefreshConflict 表示 refresh claim 的 revision/identity 已由
	// 另一个合法执行推进；调用方应重读 durable schedule，而不是把它当成损坏。
	ErrSourceRefreshConflict = errors.New("source refresh claim conflicts with durable state")
)

// Project 是 Session 和 Turn 引用的最小稳定项目维度。
type Project struct {
	ProjectID          string
	DisplayName        string
	RootPath           string
	GitRemoteSanitized *string
	CreatedAtMS        int64
	UpdatedAtMS        int64
}

// Session 保存身份和稳定 metadata，不保存可变活动状态。
type Session struct {
	SessionID     string
	Provider      string
	Originator    *string
	SourceKind    string
	ModelProvider *string
	InitialCWD    *string
	ProjectID     *string
	CLIVersion    *string
	CreatedAtMS   int64
	FirstSeenAtMS int64
	LastSeenAtMS  int64
}

// Turn 保存一个 Codex turn 的生命周期和来源位置。
type Turn struct {
	TurnID           string
	SessionID        string
	StartedAtMS      int64
	CompletedAtMS    *int64
	Outcome          *string
	Model            *string
	ReasoningEffort  *string
	CWD              *string
	ProjectID        *string
	SourceGeneration int64
	StartOffset      int64
	CompleteOffset   *int64
}

// TurnUsage 保存一个 turn 当前的 token snapshot；nullable token 区分 unknown 与真实零。
type TurnUsage struct {
	TurnID            string
	ObservedAtMS      int64
	IsFinal           bool
	InputTokens       *int64
	CachedInputTokens *int64
	OutputTokens      *int64
	ReasoningTokens   *int64
	ContextWindow     *int64
	SourceGeneration  int64
	SourceOffset      int64
	Confidence        string
	UpdatedAtMS       int64
}

// SessionCurrent 是可由事实重建的 Session 当前投影。
type SessionCurrent struct {
	SessionID             string
	ThreadName            *string
	ThreadNameUpdatedAtMS *int64
	ActiveTurnID          *string
	CurrentModel          *string
	CurrentCWD            *string
	LastActivityAtMS      *int64
	UpdatedAtMS           int64
}

// SessionUsageCurrent 保存 Session 累计计数器的当前观测，不参与日成本求和。
type SessionUsageCurrent struct {
	SessionID            string
	CounterEpoch         int64
	TotalInputTokens     *int64
	TotalCachedTokens    *int64
	TotalOutputTokens    *int64
	TotalReasoningTokens *int64
	ObservedAtMS         int64
	SourceGeneration     int64
	SourceOffset         int64
	CounterState         string
}

// FactBatch 是一个 writer transaction 内原子提交的结构化事实集合。
type FactBatch struct {
	Project             *Project
	Session             *Session
	Turn                *Turn
	Usage               *TurnUsage
	SessionCurrent      *SessionCurrent
	SessionUsageCurrent *SessionUsageCurrent
	QuotaObservation    *QuotaObservationSample
}

// SessionSnapshot 是 Session typed query 的当前返回形状。
type SessionSnapshot struct {
	Session
	Current *SessionCurrent
	Usage   *SessionUsageCurrent
}

// TurnSnapshot 将 turn lifecycle 与可选 usage fact 一起返回。
type TurnSnapshot struct {
	Turn
	Usage *TurnUsage
}

// TurnFilter 限定 typed turn 列表查询；所有 pointer nil 表示不启用该条件。
type TurnFilter struct {
	SessionID            *string
	ProjectID            *string
	Model                *string
	SourceGeneration     *int64
	StartOffsetAtOrAfter *int64
	StartedAtOrAfterMS   *int64
	StartedBeforeMS      *int64
	Limit                int
}
