package logs

import "errors"

const (
	ParserVersion         = "codex-rollout-v3"
	DefaultMaxLineBytes   = 16 << 20
	MaxSupportedLineBytes = 64 << 20
)

var (
	ErrInvalidParserConfig     = errors.New("invalid parser configuration")
	ErrInvalidParserSeed       = errors.New("invalid parser seed")
	ErrNonContiguousChunk      = errors.New("non-contiguous parser chunk")
	ErrUnsupportedParserSource = errors.New("unsupported parser source")
)

// DiagnosticClass groups safe parser failures without retaining source content.
type DiagnosticClass string

const (
	DiagnosticClassFraming       DiagnosticClass = "framing"
	DiagnosticClassSyntax        DiagnosticClass = "syntax"
	DiagnosticClassCompatibility DiagnosticClass = "compatibility"
	DiagnosticClassLifecycle     DiagnosticClass = "lifecycle"
)

// DiagnosticCode identifies a stable, content-free parser diagnostic.
type DiagnosticCode string

const (
	DiagnosticEmptyLine            DiagnosticCode = "empty_line"
	DiagnosticInvalidUTF8          DiagnosticCode = "invalid_utf8"
	DiagnosticLineTooLong          DiagnosticCode = "line_too_long"
	DiagnosticBadJSON              DiagnosticCode = "bad_json"
	DiagnosticDuplicateJSONKey     DiagnosticCode = "duplicate_json_key"
	DiagnosticInvalidTimestamp     DiagnosticCode = "invalid_timestamp"
	DiagnosticInvalidField         DiagnosticCode = "invalid_field"
	DiagnosticUnknownRolloutType   DiagnosticCode = "unknown_rollout_type"
	DiagnosticUnknownEventType     DiagnosticCode = "unknown_event_type"
	DiagnosticMissingSessionMeta   DiagnosticCode = "missing_session_meta"
	DiagnosticMissingTurnStart     DiagnosticCode = "missing_turn_start"
	DiagnosticAmbiguousTurn        DiagnosticCode = "ambiguous_turn"
	DiagnosticInvalidTransition    DiagnosticCode = "invalid_transition"
	DiagnosticOrphanTurnUsage      DiagnosticCode = "orphan_turn_usage"
	DiagnosticStateLimitExceeded   DiagnosticCode = "state_limit_exceeded"
	DiagnosticInvalidQuotaWindow   DiagnosticCode = "invalid_quota_window"
	DiagnosticInvalidQuotaSnapshot DiagnosticCode = "invalid_quota_snapshot"
)

// ParserDiagnostic locates a safely skipped source line. It deliberately omits
// source bytes and decoder error text.
type ParserDiagnostic struct {
	Class       DiagnosticClass
	Code        DiagnosticCode
	StartOffset int64
	EndOffset   int64
	Retryable   bool
}

// TokenCounters preserves the difference between an omitted counter and an
// observed zero. Total tokens are intentionally derived downstream.
type TokenCounters struct {
	InputTokens       *int64
	CachedInputTokens *int64
	OutputTokens      *int64
	ReasoningTokens   *int64
}

type TurnOutcome string

const (
	TurnOutcomeCompleted     TurnOutcome = "completed"
	TurnOutcomeInterrupted   TurnOutcome = "interrupted"
	TurnOutcomeReplaced      TurnOutcome = "replaced"
	TurnOutcomeReviewEnded   TurnOutcome = "review_ended"
	TurnOutcomeBudgetLimited TurnOutcome = "budget_limited"
)

type EventKind string

const (
	EventSessionMeta      EventKind = "session_meta"
	EventTurnStarted      EventKind = "turn_started"
	EventTurnContext      EventKind = "turn_context"
	EventTurnUsage        EventKind = "turn_usage"
	EventSessionUsage     EventKind = "session_usage"
	EventTurnEnded        EventKind = "turn_ended"
	EventQuotaObservation EventKind = "quota_observation"
)

type QuotaSource string

const (
	QuotaAccountScopeDefault             = "default"
	QuotaSourceLocalJSONL    QuotaSource = "local_jsonl"
)

type QuotaWindowKind string

const (
	QuotaWindowPrimary   QuotaWindowKind = "primary"
	QuotaWindowSecondary QuotaWindowKind = "secondary"
)

type QuotaValidity string

const (
	QuotaValidityAccepted   QuotaValidity = "accepted"
	QuotaValiditySuspicious QuotaValidity = "suspicious"
	QuotaValidityRejected   QuotaValidity = "rejected"
)

type QuotaRejectionReason string

const (
	QuotaReasonMissingLimitID       QuotaRejectionReason = "missing_limit_id"
	QuotaReasonMissingPrimaryWindow QuotaRejectionReason = "missing_primary_window"
	QuotaReasonResetNotFuture       QuotaRejectionReason = "reset_not_future"
	QuotaReasonUnknownPlanType      QuotaRejectionReason = "unknown_plan_type"
)

const QuotaPlanUnknown = "unknown"

type SourcePosition struct {
	StartOffset int64
	EndOffset   int64
}

type SessionMetaFact struct {
	SessionID     string
	RootSessionID string
	SourceKind    SourceKind
	CreatedAtMS   int64
	ObservedAtMS  int64
	InitialCWD    string
	Originator    string
	CLIVersion    string
	Source        string
	ModelProvider string
}

type TurnStartFact struct {
	SessionID     string
	TurnID        string
	StartedAtMS   int64
	ContextWindow *int64
}

type TurnContextFact struct {
	SessionID    string
	TurnID       string
	ObservedAtMS int64
	CWD          string
	Model        string
	Effort       *string
}

type TurnUsageFact struct {
	SessionID     string
	TurnID        string
	ObservedAtMS  int64
	Usage         TokenCounters
	ContextWindow *int64
	IsFinal       bool
}

type SessionUsageFact struct {
	SessionID     string
	ObservedAtMS  int64
	Usage         TokenCounters
	ContextWindow *int64
}

type TurnEndFact struct {
	SessionID     string
	TurnID        string
	CompletedAtMS int64
	Outcome       TurnOutcome
	FinalUsage    *TurnUsageFact
}

// QuotaObservationFact contains only the allowlisted local rate-limit fields.
// Source units are percent, minutes, and Unix seconds normalized to epoch
// milliseconds. Remaining percent is deliberately derived downstream.
type QuotaObservationFact struct {
	SessionID       string
	AccountScope    string
	Source          QuotaSource
	LimitID         *string
	LimitName       *string
	WindowKind      QuotaWindowKind
	UsedPercent     float64
	WindowMinutes   int64
	ResetsAtMS      int64
	PlanType        *string
	ObservedAtMS    int64
	Validity        QuotaValidity
	RejectionReason *QuotaRejectionReason
}

// ParsedEvent is a tagged union. Exactly the payload matching Kind is set.
type ParsedEvent struct {
	Kind             EventKind
	Position         SourcePosition
	SessionMeta      *SessionMetaFact
	TurnStart        *TurnStartFact
	TurnContext      *TurnContextFact
	TurnUsage        *TurnUsageFact
	SessionUsage     *SessionUsageFact
	TurnEnd          *TurnEndFact
	QuotaObservation *QuotaObservationFact
}

type ParserConfig struct {
	SourceKind   SourceKind
	StartOffset  int64
	MaxLineBytes int
	Seed         *ParserSeed
}

// ParserSeed is the durable lifecycle checkpoint required when parsing resumes
// after byte offset zero. It contains only the state needed to associate future
// lifecycle records. Feed returns the next seed; callers persist that exact safe
// checkpoint atomically with CommittableOffset and must not reconstruct it from
// mutable current projections.
type ParserSeed struct {
	Session      *SessionMetaFact
	OpenTurns    []OpenTurnSeed
	PendingTurns []PendingTurnSeed
	ClosedTurns  []ClosedTurnSeed
}

type OpenTurnSeed struct {
	TurnID        string
	StartedAtMS   int64
	ContextWindow *int64
	Context       *TurnContextFact
	LatestUsage   *TurnUsageFact
}

type PendingTurnSeed struct {
	TurnID   string
	Context  *PendingTurnContextSeed
	Terminal *PendingTurnTerminalSeed
}

type PendingTurnContextSeed struct {
	Position     SourcePosition
	ObservedAtMS int64
	CWD          string
	Model        string
	Effort       *string
}

type PendingTurnTerminalSeed struct {
	Position      SourcePosition
	CompletedAtMS int64
	Outcome       TurnOutcome
}

type ClosedTurnSeed struct {
	TurnID        string
	StartedAtMS   int64
	ContextWindow *int64
	Terminal      TurnEndFact
}

// ParseStats describes only the complete lines consumed by one Feed call.
type ParseStats struct {
	CompleteLines     uint64
	ParsedLines       uint64
	KnownIgnoredLines uint64
	DiagnosticLines   uint64
	EventsEmitted     uint64
}

func (stats *ParseStats) add(other ParseStats) {
	stats.CompleteLines += other.CompleteLines
	stats.ParsedLines += other.ParsedLines
	stats.KnownIgnoredLines += other.KnownIgnoredLines
	stats.DiagnosticLines += other.DiagnosticLines
	stats.EventsEmitted += other.EventsEmitted
}

type ParseResult struct {
	Events            []ParsedEvent
	Diagnostics       []ParserDiagnostic
	Stats             ParseStats
	ReadOffset        int64
	CommittableOffset int64
	BufferedBytes     int
	NextSeed          *ParserSeed
}
