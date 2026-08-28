package providerrefresh

import (
	"context"
	"errors"

	"github.com/SisyphusSQ/codex-pulse/internal/agentprovider"
	"github.com/SisyphusSQ/codex-pulse/internal/core"
	"github.com/SisyphusSQ/codex-pulse/internal/store"
)

var ErrInvalidTrigger = errors.New("provider refresh trigger is invalid")

const (
	TriggerStartup    = "startup"
	TriggerForeground = "foreground"
	TriggerWake       = "wake"
	TriggerScheduled  = "scheduled"
	TriggerManual     = "manual"

	StatusRefreshed            = "refreshed"
	StatusNotDue               = "not_due"
	StatusSkippedNoCredentials = "skipped_no_credentials"
	StatusSkippedDisabled      = "skipped_disabled"
	StatusSkippedUnavailable   = "skipped_unavailable"
	StatusFailed               = "failed"

	ComponentCodexLocal        = "local_index"
	ComponentCodexQuota        = "online_quota"
	ComponentCodexResetCredits = "reset_credits"
	ComponentCursorLocal       = "local_snapshot"
	ComponentCursorDashboard   = "dashboard"
	ComponentCursorGrokBot     = "grok_bot"
	ComponentGrokLocal         = "local_updates"
	ComponentGrokBilling       = "billing"

	ReasonNotDue        = "not_due"
	ReasonNoCredentials = "no_credentials"
	ReasonDisabled      = "disabled"
	ReasonUnavailable   = "unavailable"
	ReasonAuthRequired  = "auth_required"
	ReasonNetwork       = "network"
	ReasonProtocol      = "protocol"
	ReasonStorage       = "storage"
	ReasonCancelled     = "cancelled"
	ReasonFailed        = "failed"
)

var providerOrder = []string{agentprovider.Codex, agentprovider.Cursor, agentprovider.Grok}

// ComponentResult is one settled local or online refresh outcome. It never
// carries error text, URLs, paths, or credential material.
type ComponentResult struct {
	Component     string `json:"component"`
	Status        string `json:"status"`
	ReasonCode    string `json:"reasonCode"`
	Attempted     bool   `json:"attempted"`
	CommittedAtMS *int64 `json:"committedAtMs,omitempty"`
	ObservedAtMS  *int64 `json:"observedAtMs,omitempty"`
}

// ProviderResult is the stable per-provider aggregate returned by the global
// refresh command. Components stay visible even when the provider status is
// skipped or failed.
type ProviderResult struct {
	Provider   string            `json:"provider"`
	Status     string            `json:"status"`
	Components []ComponentResult `json:"components"`
}

// Receipt is the settled global refresh result. EncodeResponse maps it onto
// the frozen Protobuf receipt.
type Receipt struct {
	Trigger   string           `json:"trigger"`
	Providers []ProviderResult `json:"providers"`
}

type Request struct {
	Trigger string
}

type ProviderRefresher interface {
	RefreshProvider(context.Context, string) ProviderResult
}

type InvalidationNotifier interface {
	Notify(context.Context, core.InvalidationDomain) error
}

func ValidTrigger(value string) bool {
	switch value {
	case TriggerStartup, TriggerForeground, TriggerWake, TriggerScheduled, TriggerManual:
		return true
	default:
		return false
	}
}

func InteractiveTrigger(value string) bool {
	return value == TriggerManual
}

func StoreTrigger(value string) store.SourceRefreshTrigger {
	switch value {
	case TriggerStartup:
		return store.RefreshTriggerStartup
	case TriggerForeground:
		return store.RefreshTriggerForeground
	case TriggerWake:
		return store.RefreshTriggerWake
	case TriggerScheduled:
		return store.RefreshTriggerScheduled
	case TriggerManual:
		return store.RefreshTriggerManual
	default:
		return store.RefreshTriggerScheduled
	}
}

func CloneInt64(value *int64) *int64 {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}

func TimestampPointer(value int64) *int64 {
	if value <= 0 {
		return nil
	}
	cloned := value
	return &cloned
}

func SummarizeProvider(provider string, components []ComponentResult) ProviderResult {
	status := StatusSkippedUnavailable
	if len(components) > 0 {
		status = components[0].Status
	}
	hasFailed := false
	hasRefreshed := false
	hasNotDue := false
	skipCounts := map[string]int{}
	for _, component := range components {
		switch component.Status {
		case StatusFailed:
			hasFailed = true
		case StatusRefreshed:
			hasRefreshed = true
		case StatusNotDue:
			hasNotDue = true
		case StatusSkippedNoCredentials, StatusSkippedDisabled, StatusSkippedUnavailable:
			skipCounts[component.Status]++
		}
	}
	switch {
	case hasFailed:
		status = StatusFailed
	case hasRefreshed:
		status = StatusRefreshed
	case hasNotDue:
		status = StatusNotDue
	case skipCounts[StatusSkippedNoCredentials] == len(components) && len(components) > 0:
		status = StatusSkippedNoCredentials
	case skipCounts[StatusSkippedDisabled] == len(components) && len(components) > 0:
		status = StatusSkippedDisabled
	case skipCounts[StatusSkippedUnavailable] == len(components) && len(components) > 0:
		status = StatusSkippedUnavailable
	case skipCounts[StatusSkippedNoCredentials] > 0:
		status = StatusSkippedNoCredentials
	case skipCounts[StatusSkippedDisabled] > 0:
		status = StatusSkippedDisabled
	}
	copied := append([]ComponentResult(nil), components...)
	return ProviderResult{Provider: provider, Status: status, Components: copied}
}

func UnavailableProvider(provider string, components ...string) ProviderResult {
	results := make([]ComponentResult, 0, len(components))
	for _, component := range components {
		results = append(results, ComponentResult{
			Component:  component,
			Status:     StatusSkippedUnavailable,
			ReasonCode: ReasonUnavailable,
		})
	}
	return SummarizeProvider(provider, results)
}
