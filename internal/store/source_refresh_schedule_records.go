package store

type SourceRefreshReason string

const (
	RefreshReasonStartup            SourceRefreshReason = "startup"
	RefreshReasonNormalInterval     SourceRefreshReason = "normal_interval"
	RefreshReasonLowRemaining       SourceRefreshReason = "low_remaining"
	RefreshReasonNearReset          SourceRefreshReason = "near_reset"
	RefreshReasonResetGrace         SourceRefreshReason = "reset_grace"
	RefreshReasonForeground         SourceRefreshReason = "foreground"
	RefreshReasonWakeStale          SourceRefreshReason = "wake_stale"
	RefreshReasonManual             SourceRefreshReason = "manual"
	RefreshReasonNetworkBackoff     SourceRefreshReason = "network_backoff"
	RefreshReasonRetryAfter         SourceRefreshReason = "retry_after"
	RefreshReasonAuthRequired       SourceRefreshReason = "auth_required"
	RefreshReasonSchemaIncompatible SourceRefreshReason = "schema_incompatible"
	RefreshReasonCancelled          SourceRefreshReason = "cancelled"
	RefreshReasonDisabled           SourceRefreshReason = "disabled"
	RefreshReasonRecovery           SourceRefreshReason = "recovery"
)

type SourceRefreshTrigger string

const (
	RefreshTriggerScheduled  SourceRefreshTrigger = "scheduled"
	RefreshTriggerStartup    SourceRefreshTrigger = "startup"
	RefreshTriggerForeground SourceRefreshTrigger = "foreground"
	RefreshTriggerWake       SourceRefreshTrigger = "wake"
	RefreshTriggerManual     SourceRefreshTrigger = "manual"
	RefreshTriggerRecovery   SourceRefreshTrigger = "recovery"
)

type SourceRefreshSchedule struct {
	SourceInstanceID string
	SourceType       string
	ScopeKey         string
	NextDueAtMS      *int64
	Reason           SourceRefreshReason
	LastManualAtMS   *int64
	ActiveClaimID    *string
	ActiveTrigger    *SourceRefreshTrigger
	ClaimStartedAtMS *int64
	ClaimExpiresAtMS *int64
	Revision         int64
	UpdatedAtMS      int64
}

type SourceRefreshScheduleUpdate struct {
	SourceInstanceID string
	SourceType       string
	ScopeKey         string
	ExpectedRevision int64
	NextDueAtMS      *int64
	Reason           SourceRefreshReason
	AtMS             int64
}

type SourceRefreshCompletion struct {
	SourceInstanceID string
	ClaimID          string
	ExpectedRevision int64
	NextDueAtMS      *int64
	Reason           SourceRefreshReason
	AtMS             int64
}

// SourceRefreshClaimRecovery releases one expired claim whose request attempt
// was never durably recorded. Recorded attempts are completed through the
// normal policy path instead, so success cadence and Retry-After survive a
// crash between recording and schedule completion.
type SourceRefreshClaimRecovery struct {
	SourceInstanceID string
	ClaimID          string
	ExpectedRevision int64
	AtMS             int64
}
