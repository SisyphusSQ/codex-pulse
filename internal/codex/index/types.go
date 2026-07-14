// Package index 提供 Codex session_index.jsonl 的只读分析与显式修复能力。
package index

import "errors"

const (
	maxIndexBytes      = 64 << 20
	maxIndexLineBytes  = 1 << 20
	maxThreadNameBytes = 4096
)

var (
	// ErrIndexTooLarge 表示 session index 超过本地有界解析限制。
	ErrIndexTooLarge = errors.New("session index exceeds size limit")
	// ErrIndexLineTooLarge 表示至少一条可能由 Codex 接受的行超过本地安全解析上限。
	ErrIndexLineTooLarge = errors.New("session index line exceeds safe parse limit")
	// ErrUnsupportedIndexEntry 表示上游 schema 有效的 entry 超过本地安全 repair 能力。
	ErrUnsupportedIndexEntry = errors.New("session index entry is unsupported for safe repair")
	// ErrInvalidExpectation 表示 Store 提供的 expected Session name 不自洽。
	ErrInvalidExpectation = errors.New("invalid session index expectation")
	// ErrInvalidPlan 表示 repair plan 本身无效或 digest 已漂移。
	ErrInvalidPlan = errors.New("invalid session index repair plan")
	// ErrInvalidHome 表示 Codex Home 不是可确认的绝对真实目录。
	ErrInvalidHome = errors.New("invalid Codex home")
	// ErrHomeChanged 表示确认后的 Codex Home 物理身份发生变化。
	ErrHomeChanged = errors.New("confirmed Codex home changed")
	// ErrUnsafeIndex 表示根级 session index 是 symlink 或非普通文件。
	ErrUnsafeIndex = errors.New("unsafe session index")
	// ErrPlanDrift 表示 index 已不再是 dry-run 确认的物理版本。
	ErrPlanDrift = errors.New("session index repair plan drift")
	// ErrExpectationDrift 表示 Store expected Session projection 已不再是确认版本。
	ErrExpectationDrift = errors.New("session index repair expectations drift")
	// ErrInvalidBackup 表示备份目标不满足私有、独占路径约束。
	ErrInvalidBackup = errors.New("invalid session index backup target")
	// ErrConfirmationRequired 表示执行没有精确确认同一 dry-run plan。
	ErrConfirmationRequired = errors.New("session index repair confirmation required")
	// ErrPlanConflict 表示 dry-run 含有不能自动覆盖的 newer/unknown 冲突。
	ErrPlanConflict = errors.New("session index repair plan contains unresolved conflicts")
	// ErrRepairAlreadyRecorded 表示同一 plan 已有非 succeeded terminal 或进行中 job。
	ErrRepairAlreadyRecorded = errors.New("session index repair plan already has an audit job")
	// ErrReconcileFailed 表示 append 后 latest-wins 事实未与 plan 对齐。
	ErrReconcileFailed = errors.New("session index repair reconciliation failed")
)

// Entry 是与 OpenAI Codex 当前 SessionIndexEntry 兼容的稳定字段集合。
type Entry struct {
	ID         string `json:"id"`
	ThreadName string `json:"thread_name"`
	UpdatedAt  string `json:"updated_at"`
}

// ParsedEntry 保留 entry 的安全位置和可比较时间，不保留无效原文。
type ParsedEntry struct {
	Entry
	Line        int
	UpdatedAtMS *int64
}

// DiagnosticCode 是不会携带原始 index 内容的 allowlisted 解析结果。
type DiagnosticCode string

const (
	DiagnosticMalformedJSON     DiagnosticCode = "malformed_json"
	DiagnosticInvalidID         DiagnosticCode = "invalid_id"
	DiagnosticInvalidThreadName DiagnosticCode = "invalid_thread_name"
	DiagnosticInvalidUpdatedAt  DiagnosticCode = "invalid_updated_at"
)

// Diagnostic 只记录行号和稳定分类；Raw 固定为空，便于 contract 测试防泄漏。
type Diagnostic struct {
	Line int
	Code DiagnosticCode
	Raw  string
}

// ParsedIndex 是按 append order 解析后的有效行和诊断。
type ParsedIndex struct {
	Entries     []ParsedEntry
	Diagnostics []Diagnostic
	latest      map[string]ParsedEntry
	history     map[string]int
}

// Latest 返回指定 Session ID 的最后一条有效 entry。
func (index ParsedIndex) Latest(sessionID string) (ParsedEntry, bool) {
	entry, found := index.latest[sessionID]
	return entry, found
}

// HistoryCount 返回指定 Session ID 的有效 append history 行数。
func (index ParsedIndex) HistoryCount(sessionID string) int {
	return index.history[sessionID]
}

// Expectation 是 Codex Pulse Store 中带字段时间的 Session name projection。
type Expectation struct {
	SessionID   string
	ThreadName  string
	UpdatedAtMS int64
}

// FileVersion 锁定 dry-run 观察到的根级 session index 物理版本。
type FileVersion struct {
	Exists    bool
	DeviceID  string
	Inode     int64
	SizeBytes int64
	MTimeNS   int64
	SHA256    string
}

// RepairReason 是允许追加 correction 的原因。
type RepairReason string

const (
	ReasonMissing RepairReason = "missing"
	ReasonStale   RepairReason = "stale"
)

// RepairAction 是一个确定的 append-only correction。
type RepairAction struct {
	SessionID  string
	ThreadName string
	UpdatedAt  string
	Reason     RepairReason
}

// ConflictReason 是 analyzer 拒绝覆盖的稳定原因。
type ConflictReason string

const (
	ConflictIndexNewerOrUnknown ConflictReason = "index_newer_or_unknown"
)

// Conflict 表示 Store 和 index 不一致但不能安全自动选择 Store 的状态。
type Conflict struct {
	SessionID string
	Reason    ConflictReason
}

// History 只说明 append history 数量，不把正常 rename history 当作 repair action。
type History struct {
	SessionID string
	Count     int
}

// RepairPlan 是 dry-run 的完整、可校验输出。
type RepairPlan struct {
	ID                 string
	AnalyzedAtMS       int64
	ExpectationsSHA256 string
	Source             FileVersion
	Actions            []RepairAction
	Conflicts          []Conflict
	Histories          []History
	Diagnostics        []Diagnostic
}

// Confirmation 必须携带用户刚刚查看并确认的完整 plan ID。
type Confirmation struct {
	PlanID string
}

// RepairReport 汇总一个已审计 repair job 的稳定结果和备份位置。
type RepairReport struct {
	PlanID             string
	JobID              string
	Actions            int
	DatabaseBackupPath string
	IndexBackupPath    string
	FinalVersion       FileVersion
	Noop               bool
	Replayed           bool
}
