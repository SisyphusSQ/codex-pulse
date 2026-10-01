// Package reporting owns the optional App-bound center sync; it never reads Agent credentials.
package reporting

import (
	"context"
	"errors"

	reportingv1 "github.com/SisyphusSQ/codex-pulse/api/codexpulse/reporting/v1"
)

var (
	ErrSettings    = errors.New("invalid reporting settings")
	ErrQueueFull   = errors.New("reporting queue is full")
	ErrPending     = errors.New("reporting scope has pending batches")
	ErrUnavailable = errors.New("reporting storage unavailable")
	ErrReconnect   = errors.New("reporting requires pairing")
	ErrProtocol    = errors.New("reporting protocol rejected")
	ErrTransport   = errors.New("reporting transport unavailable")
)

const DefaultIntervalSeconds int64 = 60
const QueueByteBudget int64 = 256 << 20
const QueueBatchBudget = 2048

// Status is the local RPC whitelist; service credentials and pairing codes are excluded.
type Status struct {
	Endpoint         string `json:"endpoint"`
	ClientID         string `json:"clientId"`
	Enabled          bool   `json:"enabled"`
	AllowHTTP        bool   `json:"allowHttp"`
	IntervalSeconds  int64  `json:"intervalSeconds"`
	HistoryStartAtMS int64  `json:"historyStartAtMs"`
	State            string `json:"state"`
	PendingBatches   int64  `json:"pendingBatches"`
	PendingBytes     int64  `json:"pendingBytes"`
	RetainedBatches  int64  `json:"retainedBatches"`
	LastAttemptAtMS  *int64 `json:"lastAttemptAtMs"`
	LastSuccessAtMS  *int64 `json:"lastSuccessAtMs"`
}
type PairRequest struct {
	Endpoint  string `json:"endpoint"`
	AllowHTTP bool   `json:"allowHttp"`
	Code      string `json:"code"`
}
type ConfigureRequest struct {
	Enabled          bool  `json:"enabled"`
	IntervalSeconds  int64 `json:"intervalSeconds"`
	HistoryStartAtMS int64 `json:"historyStartAtMs"`
	ClearPending     bool  `json:"clearPending"`
}
type ExportPage struct {
	Sessions  []reportingv1.SessionSnapshot
	Next      string
	Authority bool
}
type Source interface {
	FactsPartition(context.Context, string) (string, error)
	Facts(context.Context, string, string, int64) (ExportFactsPage, error)
	Page(context.Context, string, string, int64) (ExportPage, error)
	Partition(context.Context, string) (string, error)
	Status(context.Context, string, int64) (reportingv1.DeviceStatus, error)
}

// credentialSettings stays inside Go and the private reporting store.
type credentialSettings struct {
	ID               int    `gorm:"column:id;primaryKey"`
	Endpoint         string `gorm:"column:endpoint"`
	ClientID         string `gorm:"column:client_id"`
	Credential       string `gorm:"column:credential"`
	Enabled          bool   `gorm:"column:enabled"`
	AllowHTTP        bool   `gorm:"column:allow_http"`
	IntervalSeconds  int64  `gorm:"column:interval_seconds"`
	HistoryStartAtMS int64  `gorm:"column:history_start_at_ms"`
	State            string `gorm:"column:state"`
	LastAttemptAtMS  *int64 `gorm:"column:last_attempt_at_ms"`
	LastSuccessAtMS  *int64 `gorm:"column:last_success_at_ms"`
}

func (credentialSettings) TableName() string   { return "reporting_settings" }
func (s credentialSettings) partition() string { return reportingv1.Key(s.Endpoint, s.ClientID) }
