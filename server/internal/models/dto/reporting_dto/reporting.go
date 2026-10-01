package reporting_dto

import reportingv1 "github.com/SisyphusSQ/codex-pulse/api/codexpulse/reporting/v1"

// SourceSnapshot 是经过服务端来源授权与解码的快照，用于仲裁；不作为 HTTP 响应。
type SourceSnapshot struct {
	ID              string
	ClientID        string
	Snapshot        reportingv1.SessionSnapshot
	CorrectionFence bool
}
type MergeDecision struct {
	Source          SourceSnapshot
	Conflict        bool
	Deleted         bool
	CorrectionFence bool
}
