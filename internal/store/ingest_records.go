package store

const (
	ParserCheckpointVersion  = 1
	maxCheckpointOpenTurns   = 64
	maxCheckpointPending     = 1024
	maxCheckpointClosed      = 1024
	maxCheckpointIdentifier  = 512
	maxCheckpointMetadata    = 4096
	maxParserCheckpointBytes = 32 << 20
)

type GenerationState string

const (
	GenerationBuilding   GenerationState = "building"
	GenerationActive     GenerationState = "active"
	GenerationSuperseded GenerationState = "superseded"
)

type GenerationMode string

const (
	GenerationModeAppend  GenerationMode = "append"
	GenerationModeRebuild GenerationMode = "rebuild"
)

type SourceFingerprint struct {
	SourceFileID      string
	Provider          string
	SourceKind        string
	CurrentPath       string
	DeviceID          string
	Inode             int64
	SizeBytes         int64
	MTimeNS           int64
	PrefixBytes       int64
	PrefixSHA256      string
	FingerprintSHA256 string
}

// BuildingGenerationExpectation 是调用方显式提交的 building compare-and-swap token。
type BuildingGenerationExpectation struct {
	SourceFileID      string
	Generation        int64
	FingerprintSHA256 string
	ParserVersion     string
}

type PrepareGenerationRequest struct {
	Mode                 GenerationMode
	Previous             *SourceFingerprint
	Current              SourceFingerprint
	ParserVersion        string
	ReplacesSourceFileID *string
	SupersedeBuilding    *BuildingGenerationExpectation
	AtMS                 int64
}

// GenerationBase 保存 building generation 在 Prepare 时观察到的 authoritative active lineage。
type GenerationBase struct {
	SourceFileID      string
	Generation        int64
	FingerprintSHA256 string
}

type GenerationCursor struct {
	SourceFileID         string
	Generation           int64
	State                GenerationState
	Fingerprint          SourceFingerprint
	ParserVersion        string
	ReplacesSourceFileID *string
	Base                 *GenerationBase
	SupersededBuilding   *BuildingGenerationExpectation
	Checkpoint           ParserCheckpoint
}

type IngestDiagnostic struct {
	Class       string
	Code        string
	StartOffset int64
	EndOffset   int64
	Retryable   bool
}

type IngestBatch struct {
	SourceFileID            string
	Generation              int64
	PreviousCommittedOffset int64
	PreviousFingerprint     *SourceFingerprint
	Fingerprint             SourceFingerprint
	Facts                   []FactBatch
	Diagnostics             []IngestDiagnostic
	Checkpoint              ParserCheckpoint
	EOF                     bool
	// DeferQuotaProjection lets a bounded full-history bootstrap stage raw
	// quota facts without rebuilding the global projection for every source.
	// The caller must rebuild the projection before publishing readiness.
	DeferQuotaProjection bool
	JobTransition        *JobTransition
	AtMS                 int64
}

type CheckpointSourcePosition struct {
	StartOffset int64 `json:"start_offset"`
	EndOffset   int64 `json:"end_offset"`
}

type CheckpointSessionMeta struct {
	SessionID     string `json:"session_id"`
	RootSessionID string `json:"root_session_id"`
	SourceKind    string `json:"source_kind"`
	CreatedAtMS   int64  `json:"created_at_ms"`
	ObservedAtMS  int64  `json:"observed_at_ms"`
	InitialCWD    string `json:"initial_cwd"`
	Originator    string `json:"originator"`
	CLIVersion    string `json:"cli_version"`
	Source        string `json:"source"`
	ModelProvider string `json:"model_provider"`
}

type CheckpointTurnContext struct {
	SessionID    string  `json:"session_id"`
	TurnID       string  `json:"turn_id"`
	ObservedAtMS int64   `json:"observed_at_ms"`
	CWD          string  `json:"cwd"`
	Model        string  `json:"model"`
	Effort       *string `json:"effort"`
}

type CheckpointTurnUsage struct {
	SessionID         string `json:"session_id"`
	TurnID            string `json:"turn_id"`
	ObservedAtMS      int64  `json:"observed_at_ms"`
	InputTokens       *int64 `json:"input_tokens"`
	CachedInputTokens *int64 `json:"cached_input_tokens"`
	OutputTokens      *int64 `json:"output_tokens"`
	ReasoningTokens   *int64 `json:"reasoning_tokens"`
	ContextWindow     *int64 `json:"context_window"`
	IsFinal           bool   `json:"is_final"`
}

type CheckpointTurnEnd struct {
	SessionID     string               `json:"session_id"`
	TurnID        string               `json:"turn_id"`
	CompletedAtMS int64                `json:"completed_at_ms"`
	Outcome       string               `json:"outcome"`
	FinalUsage    *CheckpointTurnUsage `json:"final_usage"`
}

type CheckpointOpenTurn struct {
	TurnID        string                 `json:"turn_id"`
	StartedAtMS   int64                  `json:"started_at_ms"`
	ContextWindow *int64                 `json:"context_window"`
	Context       *CheckpointTurnContext `json:"context"`
	LatestUsage   *CheckpointTurnUsage   `json:"latest_usage"`
}

type CheckpointPendingContext struct {
	Position     CheckpointSourcePosition `json:"position"`
	ObservedAtMS int64                    `json:"observed_at_ms"`
	CWD          string                   `json:"cwd"`
	Model        string                   `json:"model"`
	Effort       *string                  `json:"effort"`
}

type CheckpointPendingTerminal struct {
	Position      CheckpointSourcePosition `json:"position"`
	CompletedAtMS int64                    `json:"completed_at_ms"`
	Outcome       string                   `json:"outcome"`
}

type CheckpointPendingTurn struct {
	TurnID   string                     `json:"turn_id"`
	Context  *CheckpointPendingContext  `json:"context"`
	Terminal *CheckpointPendingTerminal `json:"terminal"`
}

type CheckpointClosedTurn struct {
	TurnID        string            `json:"turn_id"`
	StartedAtMS   int64             `json:"started_at_ms"`
	ContextWindow *int64            `json:"context_window"`
	Terminal      CheckpointTurnEnd `json:"terminal"`
}

type ParserSeedCheckpoint struct {
	Session      *CheckpointSessionMeta  `json:"session"`
	OpenTurns    []CheckpointOpenTurn    `json:"open_turns"`
	PendingTurns []CheckpointPendingTurn `json:"pending_turns"`
	ClosedTurns  []CheckpointClosedTurn  `json:"closed_turns"`
}

// ProjectedOpenTurnCheckpoint 保存事实投影器恢复 open turn 所需的安全字段。
// parser seed 不含 start source offset，因此该状态不能从 seed 或 mutable facts 反推。
type ProjectedOpenTurnCheckpoint struct {
	TurnID           string  `json:"turn_id"`
	SessionID        string  `json:"session_id"`
	StartedAtMS      int64   `json:"started_at_ms"`
	SourceGeneration int64   `json:"source_generation"`
	StartOffset      int64   `json:"start_offset"`
	Model            *string `json:"model"`
	ReasoningEffort  *string `json:"reasoning_effort"`
	CWD              *string `json:"cwd"`
}

// ProjectorCheckpoint 保存 parser seed 不包含、但下一批事实投影需要的安全 previous。
// 它与 parser seed 一起提交，绝不从 mutable current tables 反推 parser checkpoint。
type ProjectorCheckpoint struct {
	SessionSourceKind string                        `json:"session_source_kind"`
	OpenTurns         []ProjectedOpenTurnCheckpoint `json:"open_turns"`
	Current           *SessionCurrent               `json:"current"`
	SessionUsage      *SessionUsageCurrent          `json:"session_usage"`
}

type ParserCheckpoint struct {
	Version         int
	ParserVersion   string
	CommittedOffset int64
	Seed            *ParserSeedCheckpoint
	Projector       ProjectorCheckpoint
}
