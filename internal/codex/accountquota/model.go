package accountquota

import (
	"github.com/SisyphusSQ/codex-pulse/internal/codex/subscriptionaccounts"
	"github.com/SisyphusSQ/codex-pulse/internal/store"
)

const ContractVersion = "codex-account-quotas-v1"

type Window struct {
	WindowKind        store.QuotaWindowKind       `json:"windowKind"`
	LimitID           string                      `json:"limitId"`
	UsedPercent       *float64                    `json:"usedPercent,omitempty"`
	RemainingPercent  *float64                    `json:"remainingPercent,omitempty"`
	WindowMinutes     *int64                      `json:"windowMinutes,omitempty"`
	ResetsAtMS        *int64                      `json:"resetsAtMs,omitempty"`
	LastCollectedAtMS *int64                      `json:"lastCollectedAtMs,omitempty"`
	Freshness         store.QuotaCurrentFreshness `json:"freshness"`
	Conflict          store.QuotaConflictState    `json:"conflict"`
}

type Account struct {
	Subscription      subscriptionaccounts.Account `json:"subscription"`
	Windows           []Window                     `json:"windows"`
	LastCollectedAtMS *int64                       `json:"lastCollectedAtMs,omitempty"`
}

type Snapshot struct {
	Version       string    `json:"version"`
	EvaluatedAtMS int64     `json:"evaluatedAtMs"`
	TimeZone      string    `json:"timeZone"`
	Accounts      []Account `json:"accounts"`
}

type ClearReceipt struct {
	AccountCount       int64 `json:"accountCount"`
	WindowCount        int64 `json:"windowCount"`
	ObservationCount   int64 `json:"observationCount"`
	ResetSnapshotCount int64 `json:"resetSnapshotCount"`
}
