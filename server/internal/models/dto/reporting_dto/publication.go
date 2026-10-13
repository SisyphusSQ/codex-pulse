package reporting_dto

import (
	reportingv1 "github.com/SisyphusSQ/codex-pulse/api/codexpulse/reporting/v1"
	reporting_do "github.com/SisyphusSQ/codex-pulse/server/internal/models/do/mysql/reporting_do"
)

type ProjectionPlan struct {
	Session   reporting_do.Session
	Snapshot  reportingv1.SessionSnapshot
	Usage     []reporting_do.Usage
	Capsule   reporting_do.SessionCapsule
	Changed   bool
	FlagsOnly bool
}

// PayloadReference 是内部存储清单，不能进入网络快照。
type PayloadReference struct {
	PayloadKey string `json:"payload_ref"`
	Parts      int    `json:"parts"`
	Bytes      int    `json:"bytes"`
	Complete   bool   `json:"complete"`
	Deleted    bool   `json:"deleted"`
}
