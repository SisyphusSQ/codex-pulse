package store

import "github.com/SisyphusSQ/codex-pulse/internal/attribution"

type AttributionConfidence = attribution.Confidence

const (
	AttributionConfidenceHigh    = attribution.ConfidenceHigh
	AttributionConfidenceMedium  = attribution.ConfidenceMedium
	AttributionConfidenceLow     = attribution.ConfidenceLow
	AttributionConfidenceUnknown = attribution.ConfidenceUnknown
)

type AttributionSource = attribution.Source

const (
	AttributionSourceSessionIDFallback = attribution.SourceSessionIDFallback
	AttributionSourceAppServerName     = attribution.SourceAppServerName
	AttributionSourceRegisteredRoot    = attribution.SourceRegisteredRoot
	AttributionSourceCWDPathDigest     = attribution.SourceCWDPathDigest
	AttributionSourceModelCanonical    = attribution.SourceModelCanonical
	AttributionSourceModelAlias        = attribution.SourceModelAlias
	AttributionSourceConflict          = attribution.SourceConflict
	AttributionSourceMissing           = attribution.SourceMissing
	AttributionSourceInvalidPath       = attribution.SourceInvalidPath
	AttributionSourceInvalidModel      = attribution.SourceInvalidModel
)

type AttributionReason = attribution.Reason

const (
	AttributionReasonStableIdentity = attribution.ReasonStableIdentity
	AttributionReasonRootMatched    = attribution.ReasonRootMatched
	AttributionReasonPathDerived    = attribution.ReasonPathDerived
	AttributionReasonObserved       = attribution.ReasonObserved
	AttributionReasonConflict       = attribution.ReasonConflict
	AttributionReasonMissing        = attribution.ReasonMissing
	AttributionReasonInvalid        = attribution.ReasonInvalid
)

// ProjectAttribution 是不含 root path 的安全项目投影。
type ProjectAttribution struct {
	ProjectID   *string               `json:"projectId"`
	DisplayName *string               `json:"displayName"`
	Confidence  AttributionConfidence `json:"confidence"`
	Source      AttributionSource     `json:"source"`
	Reason      AttributionReason     `json:"reason"`
}

// ModelAttribution 只暴露经过安全 token 规则归一化的 model key/display。
type ModelAttribution struct {
	ModelKey    *string               `json:"modelKey"`
	DisplayName *string               `json:"displayName"`
	Confidence  AttributionConfidence `json:"confidence"`
	Source      AttributionSource     `json:"source"`
	Reason      AttributionReason     `json:"reason"`
}

// SessionAttributionSnapshot 是供后续 query service 消费的安全领域投影。
type SessionAttributionSnapshot struct {
	SessionID       string                `json:"sessionId"`
	DisplayTitle    string                `json:"displayTitle"`
	TitleConfidence AttributionConfidence `json:"titleConfidence"`
	TitleSource     AttributionSource     `json:"titleSource"`
	TitleReason     AttributionReason     `json:"titleReason"`
	Project         ProjectAttribution    `json:"project"`
	Model           ModelAttribution      `json:"model"`
	RuleVersion     int                   `json:"ruleVersion"`
	UpdatedAtMS     int64                 `json:"updatedAtMs"`
}

// TurnAttributionSnapshot 是单 turn 的安全派生归因。
type TurnAttributionSnapshot struct {
	TurnID      string             `json:"turnId"`
	Project     ProjectAttribution `json:"project"`
	Model       ModelAttribution   `json:"model"`
	RuleVersion int                `json:"ruleVersion"`
	UpdatedAtMS int64              `json:"updatedAtMs"`
}

type RecomputeAttributionsRequest struct {
	AtMS int64
}

type RecomputeAttributionsReport struct {
	Sessions    int
	Turns       int
	RuleVersion int
}
