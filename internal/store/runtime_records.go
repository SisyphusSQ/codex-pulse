package store

import (
	"crypto/sha256"
	"encoding/hex"

	"github.com/SisyphusSQ/codex-pulse/internal/pricing"
)

// SHA256Digest 是 opaque SHA-256 值；私有状态阻止调用方把任意 string 伪装成摘要。
type SHA256Digest struct {
	bytes [sha256.Size]byte
	valid bool
}

// SHA256DigestOf 在进入持久层前把调用方的稳定结构化 identity 降维为摘要。
func SHA256DigestOf(content []byte) SHA256Digest {
	sum := sha256.Sum256(content)
	return SHA256Digest{bytes: sum, valid: true}
}

// String 只把有效摘要编码为数据库使用的 64 位小写十六进制。
func (digest SHA256Digest) String() string {
	if !digest.valid {
		return ""
	}
	return hex.EncodeToString(digest.bytes[:])
}

func parseSHA256Digest(value string) (SHA256Digest, bool) {
	if len(value) != sha256DigestHexLength {
		return SHA256Digest{}, false
	}
	for index := 0; index < len(value); index++ {
		character := value[index]
		if character < '0' || character > '9' {
			if character < 'a' || character > 'f' {
				return SHA256Digest{}, false
			}
		}
	}
	decoded, err := hex.DecodeString(value)
	if err != nil || len(decoded) != sha256.Size {
		return SHA256Digest{}, false
	}
	var bytes [sha256.Size]byte
	copy(bytes[:], decoded)
	return SHA256Digest{bytes: bytes, valid: true}, true
}

// RuntimeErrorClass 是允许持久化的稳定错误分类，不包含原始错误正文。
type RuntimeErrorClass string

const (
	RuntimeErrorCanceled    RuntimeErrorClass = "canceled"
	RuntimeErrorBusy        RuntimeErrorClass = "busy"
	RuntimeErrorDiskFull    RuntimeErrorClass = "disk_full"
	RuntimeErrorReadOnly    RuntimeErrorClass = "read_only"
	RuntimeErrorPermission  RuntimeErrorClass = "permission"
	RuntimeErrorIO          RuntimeErrorClass = "io"
	RuntimeErrorCorrupt     RuntimeErrorClass = "corrupt"
	RuntimeErrorTimeout     RuntimeErrorClass = "timeout"
	RuntimeErrorUnavailable RuntimeErrorClass = "unavailable"
	RuntimeErrorInvalid     RuntimeErrorClass = "invalid_input"
	RuntimeErrorUnknown     RuntimeErrorClass = "unknown"
)

// SourceFailureCode keeps a source-specific, content-free failure reason next
// to the cross-domain RuntimeErrorClass. It is safe for persistence and UI
// routing because it cannot carry an upstream error or response body.
type SourceFailureCode string

const (
	SourceFailureNetworkUnavailable SourceFailureCode = "network_unavailable"
	SourceFailureTimeout            SourceFailureCode = "timeout"
	SourceFailureAuthRequired       SourceFailureCode = "auth_required"
	SourceFailureHTTP429            SourceFailureCode = "http_429"
	SourceFailureServerError        SourceFailureCode = "server_error"
	SourceFailureSchemaIncompatible SourceFailureCode = "schema_incompatible"
	SourceFailureCancelled          SourceFailureCode = "cancelled"
)

const (
	QuotaSourceInstanceWhamDefault = "quota:wham:default"
	QuotaSourceTypeWham            = "wham_quota"
)

type SourceFileState string

const (
	SourceFileDiscovered  SourceFileState = "discovered"
	SourceFileActive      SourceFileState = "active"
	SourceFileCompleted   SourceFileState = "completed"
	SourceFileUnavailable SourceFileState = "unavailable"
	SourceFileFailed      SourceFileState = "failed"
)

type SourceFreshness string

const (
	SourceFreshnessUnknown     SourceFreshness = "unknown"
	SourceFreshnessCurrent     SourceFreshness = "current"
	SourceFreshnessStale       SourceFreshness = "stale"
	SourceFreshnessUnavailable SourceFreshness = "unavailable"
)

type SourceAttemptOutcome string

const (
	SourceAttemptSucceeded SourceAttemptOutcome = "succeeded"
	SourceAttemptFailed    SourceAttemptOutcome = "failed"
	SourceAttemptCancelled SourceAttemptOutcome = "cancelled"
)

// SourceFile 保存稳定物理身份和当前解析游标，不保存文件正文。
type SourceFile struct {
	SourceFileID     string
	Provider         string
	SessionID        *string
	CurrentPath      string
	DeviceID         string
	Inode            int64
	SizeBytes        int64
	MTimeNS          int64
	ParsedOffset     int64
	ParserVersion    string
	ActiveGeneration int64
	State            SourceFileState
	LastScannedAtMS  *int64
	LastErrorClass   *RuntimeErrorClass
	UpdatedAtMS      int64
}

// SourceState 保存一个逻辑来源的调度与 freshness 状态。
type SourceState struct {
	SourceInstanceID    string
	SourceType          string
	ScopeKey            string
	LastAttemptAtMS     *int64
	LastSuccessAtMS     *int64
	NextDueAtMS         *int64
	ConsecutiveFailures int64
	LastErrorClass      *RuntimeErrorClass
	LastFailureCode     *SourceFailureCode
	FreshnessState      SourceFreshness
	CursorVersion       int64
	UpdatedAtMS         int64
}

// SourceAttempt 是一次已结束的来源请求历史，只保存结构化结果和可选摘要 hash。
type SourceAttempt struct {
	RequestID        string
	SourceInstanceID string
	StartedAtMS      int64
	FinishedAtMS     int64
	Outcome          SourceAttemptOutcome
	HTTPStatus       *int64
	ErrorClass       *RuntimeErrorClass
	FailureCode      *SourceFailureCode
	PayloadSHA256    *SHA256Digest
	AttemptCount     int64
	ResponseBytes    int64
	RetryAtMS        *int64
}

// QuotaFetchRecord is the atomic persistence unit for one online quota fetch.
// Observations and attempt metrics contain no response text or credentials.
type QuotaFetchRecord struct {
	SourceInstanceID string
	SourceType       string
	ScopeKey         string
	Attempt          SourceAttempt
	Observations     []QuotaObservationSample
}

type JobState string

const (
	JobQueued      JobState = "queued"
	JobRunning     JobState = "running"
	JobSucceeded   JobState = "succeeded"
	JobFailed      JobState = "failed"
	JobCancelled   JobState = "cancelled"
	JobInterrupted JobState = "interrupted"
)

type JobPhase string

const (
	JobPhaseDiscover        JobPhase = "discover"
	JobPhaseFastBootstrap   JobPhase = "fast_bootstrap"
	JobPhaseHistoryBackfill JobPhase = "history_backfill"
	JobPhaseReconcile       JobPhase = "reconcile"
	JobPhaseLive            JobPhase = "live"
	JobPhaseMaintenance     JobPhase = "maintenance"
)

// JobCursor 是可恢复扫描位置；使用整数列而非 opaque string，避免原始正文或 token 落盘。
type JobCursor struct {
	Generation int64
	Offset     int64
}

// JobRun 保存一次独立作业尝试；terminal 行不会被恢复为运行态。
type JobRun struct {
	JobID           string
	JobType         string
	RequestedBy     string
	Priority        int64
	State           JobState
	Phase           JobPhase
	SourceFileID    *string
	ResumeOfJobID   *string
	CreatedAtMS     int64
	StartedAtMS     *int64
	FinishedAtMS    *int64
	ProgressCurrent *int64
	ProgressTotal   *int64
	ResumeCursor    *JobCursor
	ErrorClass      *RuntimeErrorClass
	UpdatedAtMS     int64
}

// JobTransition 使用 ExpectedState 防止陈旧调用覆盖已推进状态。
type JobTransition struct {
	JobID           string
	ExpectedState   JobState
	State           JobState
	Phase           JobPhase
	ProgressCurrent *int64
	ProgressTotal   *int64
	ResumeCursor    *JobCursor
	ErrorClass      *RuntimeErrorClass
	AtMS            int64
}

type JobRunFilter struct {
	State        *JobState
	SourceFileID *string
	Limit        int
}

type HealthDomain string

const (
	HealthDomainSource  HealthDomain = "source"
	HealthDomainJob     HealthDomain = "job"
	HealthDomainStore   HealthDomain = "store"
	HealthDomainPricing HealthDomain = "pricing"
	HealthDomainRuntime HealthDomain = "runtime"
)

type HealthCode string

const (
	HealthCodeSourceTimeout     HealthCode = "source.timeout"
	HealthCodeSourceUnavailable HealthCode = "source.unavailable"
	HealthCodeSourcePermission  HealthCode = "source.permission"
	HealthCodeSourceCorrupt     HealthCode = "source.corrupt"
	HealthCodeSourceStale       HealthCode = "source.stale"

	HealthCodeJobInterrupted HealthCode = "job.interrupted"
	HealthCodeJobFailed      HealthCode = "job.failed"
	HealthCodeJobCancelled   HealthCode = "job.cancelled"

	HealthCodeStoreBusy        HealthCode = "store.busy"
	HealthCodeStoreDiskFull    HealthCode = "store.disk_full"
	HealthCodeStoreReadOnly    HealthCode = "store.read_only"
	HealthCodeStorePermission  HealthCode = "store.permission"
	HealthCodeStoreIO          HealthCode = "store.io"
	HealthCodeStoreCorrupt     HealthCode = "store.corrupt"
	HealthCodeStoreUnavailable HealthCode = "store.unavailable"
	HealthCodeStoreUnknown     HealthCode = "store.unknown"

	HealthCodePricingUnavailable HealthCode = "pricing.unavailable"
	HealthCodePricingInvalid     HealthCode = "pricing.invalid"
	HealthCodeRuntimeUnknown     HealthCode = "runtime.unknown"
)

type HealthSeverity string

const (
	HealthInfo     HealthSeverity = "info"
	HealthWarning  HealthSeverity = "warning"
	HealthError    HealthSeverity = "error"
	HealthCritical HealthSeverity = "critical"
)

// HealthObservation 是一次无原始正文的结构化健康观测。
type HealthObservation struct {
	EventID      string
	Fingerprint  SHA256Digest
	Domain       HealthDomain
	Severity     HealthSeverity
	Code         HealthCode
	SourceFileID *string
	JobID        *string
	ErrorClass   *RuntimeErrorClass
	ObservedAtMS int64
}

// HealthEvent 保存同一 fingerprint 的聚合生命周期。
type HealthEvent struct {
	EventID         string
	Fingerprint     SHA256Digest
	Domain          HealthDomain
	Severity        HealthSeverity
	Code            HealthCode
	SourceFileID    *string
	JobID           *string
	ErrorClass      *RuntimeErrorClass
	FirstSeenAtMS   int64
	LastSeenAtMS    int64
	ResolvedAtMS    *int64
	OccurrenceCount int64
	UpdatedAtMS     int64
}

type HealthEventFilter struct {
	Active       *bool
	Severity     *HealthSeverity
	SourceFileID *string
	JobID        *string
	Limit        int
}

type ModelMatchKind = pricing.ModelMatchKind

const (
	ModelMatchExact   = pricing.ModelMatchExact
	ModelMatchPrefix  = pricing.ModelMatchPrefix
	ModelMatchDefault = pricing.ModelMatchDefault
)

type ModelPrice = pricing.ModelPrice

// PricingVersion 是 source/currency 时间线上的不可变 catalog snapshot。
type PricingVersion = pricing.CatalogVersion

// EffectivePricing 返回版本的推导半开区间和唯一匹配规则。
type EffectivePricing struct {
	PricingVersion PricingVersion
	EffectiveToMS  *int64
	Matched        ModelPrice
}
