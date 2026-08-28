package providerrefresh

import (
	"context"
	"errors"

	"github.com/SisyphusSQ/codex-pulse/internal/cursorprovider"
	"github.com/SisyphusSQ/codex-pulse/internal/grokprovider"
	"github.com/SisyphusSQ/codex-pulse/internal/store"
)

func ClassifyOnlineError(err error, attempted bool) ComponentResult {
	switch {
	case err == nil && !attempted:
		return ComponentResult{Status: StatusNotDue, ReasonCode: ReasonNotDue, Attempted: false}
	case err == nil:
		return ComponentResult{Status: StatusRefreshed, ReasonCode: StatusRefreshed, Attempted: true}
	case errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded):
		return ComponentResult{Status: StatusFailed, ReasonCode: ReasonCancelled, Attempted: attempted}
	case errors.Is(err, cursorprovider.ErrCollector) || errors.Is(err, grokprovider.ErrCollector):
		if !attempted {
			return ComponentResult{Status: StatusSkippedUnavailable, ReasonCode: ReasonUnavailable, Attempted: false}
		}
		return ComponentResult{Status: StatusFailed, ReasonCode: ReasonProtocol, Attempted: true}
	case errors.Is(err, cursorprovider.ErrDesktopAuthUnavailable) ||
		errors.Is(err, grokprovider.ErrAuthUnavailable):
		return ComponentResult{Status: StatusSkippedNoCredentials, ReasonCode: ReasonNoCredentials, Attempted: false}
	case errors.Is(err, cursorprovider.ErrDesktopAuthExpired) ||
		errors.Is(err, grokprovider.ErrAuthExpired):
		if attempted {
			return ComponentResult{Status: StatusFailed, ReasonCode: ReasonAuthRequired, Attempted: true}
		}
		return ComponentResult{Status: StatusSkippedNoCredentials, ReasonCode: ReasonNoCredentials, Attempted: false}
	case errors.Is(err, cursorprovider.ErrDashboardAuthRejected) ||
		errors.Is(err, grokprovider.ErrBillingAuthRejected):
		return ComponentResult{Status: StatusFailed, ReasonCode: ReasonAuthRequired, Attempted: true}
	case errors.Is(err, cursorprovider.ErrDashboardTransport) ||
		errors.Is(err, grokprovider.ErrBillingProtocol):
		return ComponentResult{Status: StatusFailed, ReasonCode: ReasonNetwork, Attempted: true}
	case errors.Is(err, cursorprovider.ErrDashboardProtocol) ||
		errors.Is(err, grokprovider.ErrCollector):
		return ComponentResult{Status: StatusFailed, ReasonCode: ReasonProtocol, Attempted: attempted}
	default:
		if !attempted {
			return ComponentResult{Status: StatusFailed, ReasonCode: ReasonFailed, Attempted: false}
		}
		return ComponentResult{Status: StatusFailed, ReasonCode: ReasonFailed, Attempted: true}
	}
}

func ClassifyLocalError(err error, attempted bool) ComponentResult {
	switch {
	case err == nil && !attempted:
		return ComponentResult{Status: StatusNotDue, ReasonCode: ReasonNotDue, Attempted: false}
	case err == nil:
		return ComponentResult{Status: StatusRefreshed, ReasonCode: StatusRefreshed, Attempted: true}
	case errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded):
		return ComponentResult{Status: StatusFailed, ReasonCode: ReasonCancelled, Attempted: attempted}
	case errors.Is(err, cursorprovider.ErrCollector) || errors.Is(err, grokprovider.ErrCollector):
		if !attempted {
			return ComponentResult{Status: StatusSkippedUnavailable, ReasonCode: ReasonUnavailable, Attempted: false}
		}
		return ComponentResult{Status: StatusFailed, ReasonCode: ReasonStorage, Attempted: true}
	default:
		return ComponentResult{Status: StatusFailed, ReasonCode: ReasonStorage, Attempted: attempted}
	}
}

func ClassifyCodexSchedule(reason store.SourceRefreshReason, fetched bool, err error) ComponentResult {
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return ComponentResult{Status: StatusFailed, ReasonCode: ReasonCancelled, Attempted: fetched}
		}
		if reason == store.RefreshReasonAuthRequired {
			if fetched {
				return ComponentResult{Status: StatusFailed, ReasonCode: ReasonAuthRequired, Attempted: true}
			}
			return ComponentResult{Status: StatusSkippedNoCredentials, ReasonCode: ReasonNoCredentials, Attempted: false}
		}
		return ComponentResult{Status: StatusFailed, ReasonCode: ReasonFailed, Attempted: fetched}
	}
	switch reason {
	case store.RefreshReasonDisabled:
		return ComponentResult{Status: StatusSkippedDisabled, ReasonCode: ReasonDisabled, Attempted: false}
	case store.RefreshReasonAuthRequired:
		if fetched {
			return ComponentResult{Status: StatusFailed, ReasonCode: ReasonAuthRequired, Attempted: true}
		}
		return ComponentResult{Status: StatusSkippedNoCredentials, ReasonCode: ReasonNoCredentials, Attempted: false}
	case store.RefreshReasonManual, store.RefreshReasonForeground, store.RefreshReasonWakeStale,
		store.RefreshReasonStartup, store.RefreshReasonRecovery:
		if fetched {
			return ComponentResult{Status: StatusRefreshed, ReasonCode: StatusRefreshed, Attempted: true}
		}
		return ComponentResult{Status: StatusNotDue, ReasonCode: ReasonNotDue, Attempted: false}
	default:
		if fetched {
			return ComponentResult{Status: StatusRefreshed, ReasonCode: StatusRefreshed, Attempted: true}
		}
		return ComponentResult{Status: StatusNotDue, ReasonCode: ReasonNotDue, Attempted: false}
	}
}

func WithComponent(component string, result ComponentResult, observedAtMS int64) ComponentResult {
	result.Component = component
	if result.Status == StatusRefreshed && result.CommittedAtMS == nil {
		result.CommittedAtMS = TimestampPointer(observedAtMS)
	}
	if result.ObservedAtMS == nil {
		result.ObservedAtMS = TimestampPointer(observedAtMS)
	}
	return result
}
