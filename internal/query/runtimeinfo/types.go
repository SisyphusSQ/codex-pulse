package runtimeinfo

import (
	quotaquery "github.com/SisyphusSQ/codex-pulse/internal/codex/quota"
	basequery "github.com/SisyphusSQ/codex-pulse/internal/query"
)

const ContractVersion = "runtime-info-v1"

type RecoveryActionKind string

const (
	RecoveryNone            RecoveryActionKind = "none"
	RecoveryRetry           RecoveryActionKind = "retry"
	RecoveryCheckSource     RecoveryActionKind = "check_source"
	RecoveryGrantPermission RecoveryActionKind = "grant_permission"
	RecoveryFreeSpace       RecoveryActionKind = "free_space"
	RecoveryChooseHome      RecoveryActionKind = "choose_home"
	RecoveryRepairStore     RecoveryActionKind = "repair_store"
)

const (
	CommandRetrySource     = "runtime.source.retry"
	CommandCheckSource     = "runtime.source.check"
	CommandGrantPermission = "runtime.permission.open"
	CommandFreeSpace       = "runtime.storage.freeSpace"
	CommandChooseHome      = "runtime.home.choose"
	CommandRepairStore     = "runtime.store.repair"
	CommandRetryJob        = "runtime.job.retry"
	CommandRetryHealth     = "runtime.health.retry"
)

type RecoveryAction struct {
	Kind       RecoveryActionKind `json:"kind"`
	CommandKey *string            `json:"commandKey"`
}

type QuotaCurrentResponse struct {
	Meta    basequery.ResponseMeta     `json:"meta"`
	Current quotaquery.CurrentResponse `json:"current"`
}

type SourceKind string

const (
	SourceLocalFile SourceKind = "local_file"
	SourceOnline    SourceKind = "online"
)

type SourceItem struct {
	SourceKey           string                 `json:"sourceKey"`
	Kind                SourceKind             `json:"kind"`
	Provider            *string                `json:"provider"`
	SourceType          *string                `json:"sourceType"`
	State               string                 `json:"state"`
	SizeBytes           basequery.NumericValue `json:"sizeBytes"`
	ParsedBytes         basequery.NumericValue `json:"parsedBytes"`
	LastScannedAtMS     basequery.NumericValue `json:"lastScannedAtMs"`
	LastAttemptAtMS     basequery.NumericValue `json:"lastAttemptAtMs"`
	LastSuccessAtMS     basequery.NumericValue `json:"lastSuccessAtMs"`
	NextDueAtMS         basequery.NumericValue `json:"nextDueAtMs"`
	ConsecutiveFailures basequery.NumericValue `json:"consecutiveFailures"`
	ErrorClass          *string                `json:"errorClass"`
	FailureCode         *string                `json:"failureCode"`
	UpdatedAtMS         basequery.NumericValue `json:"updatedAtMs"`
	RecoveryAction      RecoveryAction         `json:"recoveryAction"`
}

type SourceSummary struct {
	Total         basequery.NumericValue `json:"total"`
	LocalFiles    basequery.NumericValue `json:"localFiles"`
	OnlineSources basequery.NumericValue `json:"onlineSources"`
	Attention     basequery.NumericValue `json:"attention"`
}

type SourceListResponse struct {
	Meta             basequery.ResponseMeta `json:"meta"`
	Items            []SourceItem           `json:"items"`
	MatchedCount     basequery.NumericValue `json:"matchedCount"`
	Summary          SourceSummary          `json:"summary"`
	UnavailableKinds []SourceKind           `json:"unavailableKinds"`
}

type SourceDetailRequest struct {
	SourceKey string `json:"sourceKey"`
}

type SourceDetailResponse struct {
	Meta basequery.ResponseMeta `json:"meta"`
	Item SourceItem             `json:"item"`
}

type JobProgress struct {
	Current basequery.NumericValue `json:"current"`
	Total   basequery.NumericValue `json:"total"`
}

type JobItem struct {
	JobID           string                 `json:"jobId"`
	JobType         string                 `json:"jobType"`
	RequestedBy     string                 `json:"requestedBy"`
	State           string                 `json:"state"`
	Phase           string                 `json:"phase"`
	SourceKey       *string                `json:"sourceKey"`
	CreatedAtMS     basequery.NumericValue `json:"createdAtMs"`
	StartedAtMS     basequery.NumericValue `json:"startedAtMs"`
	FinishedAtMS    basequery.NumericValue `json:"finishedAtMs"`
	LastSuccessAtMS basequery.NumericValue `json:"lastSuccessAtMs"`
	Progress        JobProgress            `json:"progress"`
	FailureCount    basequery.NumericValue `json:"failureCount"`
	NextRetryAtMS   basequery.NumericValue `json:"nextRetryAtMs"`
	ErrorClass      *string                `json:"errorClass"`
	UpdatedAtMS     basequery.NumericValue `json:"updatedAtMs"`
	RecoveryAction  RecoveryAction         `json:"recoveryAction"`
}

type JobSummary struct {
	Total       basequery.NumericValue `json:"total"`
	Queued      basequery.NumericValue `json:"queued"`
	Running     basequery.NumericValue `json:"running"`
	Succeeded   basequery.NumericValue `json:"succeeded"`
	Failed      basequery.NumericValue `json:"failed"`
	Cancelled   basequery.NumericValue `json:"cancelled"`
	Interrupted basequery.NumericValue `json:"interrupted"`
}

type JobListResponse struct {
	Meta         basequery.ResponseMeta `json:"meta"`
	Items        []JobItem              `json:"items"`
	MatchedCount basequery.NumericValue `json:"matchedCount"`
	Summary      JobSummary             `json:"summary"`
}

type JobDetailRequest struct {
	JobID string `json:"jobId"`
}

type JobDetailResponse struct {
	Meta basequery.ResponseMeta `json:"meta"`
	Item JobItem                `json:"item"`
}

type HealthLevel string

const (
	HealthHealthy  HealthLevel = "healthy"
	HealthBusy     HealthLevel = "busy"
	HealthPaused   HealthLevel = "paused"
	HealthDegraded HealthLevel = "degraded"
	HealthBlocked  HealthLevel = "blocked"
)

type HealthItem struct {
	EventID         string                 `json:"eventId"`
	Domain          string                 `json:"domain"`
	Severity        string                 `json:"severity"`
	Code            string                 `json:"code"`
	Component       string                 `json:"component"`
	Rule            string                 `json:"rule"`
	Impact          string                 `json:"impact"`
	Protection      string                 `json:"protection"`
	SourceKey       *string                `json:"sourceKey"`
	JobID           *string                `json:"jobId"`
	ErrorClass      *string                `json:"errorClass"`
	FirstSeenAtMS   basequery.NumericValue `json:"firstSeenAtMs"`
	LastSeenAtMS    basequery.NumericValue `json:"lastSeenAtMs"`
	ResolvedAtMS    basequery.NumericValue `json:"resolvedAtMs"`
	OccurrenceCount basequery.NumericValue `json:"occurrenceCount"`
	Active          bool                   `json:"active"`
	RecoveryAction  RecoveryAction         `json:"recoveryAction"`
}

type HealthSummary struct {
	Level    HealthLevel            `json:"level"`
	Total    basequery.NumericValue `json:"total"`
	Active   basequery.NumericValue `json:"active"`
	Resolved basequery.NumericValue `json:"resolved"`
	Info     basequery.NumericValue `json:"info"`
	Warnings basequery.NumericValue `json:"warnings"`
	Errors   basequery.NumericValue `json:"errors"`
	Critical basequery.NumericValue `json:"critical"`
}

type HealthListResponse struct {
	Meta         basequery.ResponseMeta `json:"meta"`
	Items        []HealthItem           `json:"items"`
	MatchedCount basequery.NumericValue `json:"matchedCount"`
	Summary      HealthSummary          `json:"summary"`
}

type HealthDetailRequest struct {
	EventID string `json:"eventId"`
}

type HealthDetailResponse struct {
	Meta basequery.ResponseMeta `json:"meta"`
	Item HealthItem             `json:"item"`
}

type DataHealthWindow struct {
	FromMS  basequery.NumericValue `json:"fromMs"`
	UntilMS basequery.NumericValue `json:"untilMs"`
}

type DataHealthRuntimePoint struct {
	CapturedAtMS         basequery.NumericValue `json:"capturedAtMs"`
	CPUPercent           float64                `json:"cpuPercent"`
	RSSBytes             basequery.NumericValue `json:"rssBytes"`
	PeakRSSBytes         basequery.NumericValue `json:"peakRssBytes"`
	DBBytes              basequery.NumericValue `json:"dbBytes"`
	WALBytes             basequery.NumericValue `json:"walBytes"`
	DiskFreeBytes        basequery.NumericValue `json:"diskFreeBytes"`
	LiveQueueDepth       basequery.NumericValue `json:"liveQueueDepth"`
	BackfillQueueDepth   basequery.NumericValue `json:"backfillQueueDepth"`
	OldestLiveWaitMS     basequery.NumericValue `json:"oldestLiveWaitMs"`
	OldestBackfillWaitMS basequery.NumericValue `json:"oldestBackfillWaitMs"`
	DroppedSamples       basequery.NumericValue `json:"droppedSamples"`
}

type DataHealthScheduler struct {
	CycleCount               basequery.NumericValue `json:"cycleCount"`
	CompletedCycles          basequery.NumericValue `json:"completedCycles"`
	YieldedCycles            basequery.NumericValue `json:"yieldedCycles"`
	FailedCycles             basequery.NumericValue `json:"failedCycles"`
	InterruptedCycles        basequery.NumericValue `json:"interruptedCycles"`
	FilesScanned             basequery.NumericValue `json:"filesScanned"`
	BytesRead                basequery.NumericValue `json:"bytesRead"`
	ActiveMS                 basequery.NumericValue `json:"activeMs"`
	MaxCycleActiveMS         basequery.NumericValue `json:"maxCycleActiveMs"`
	LastProgressAtMS         basequery.NumericValue `json:"lastProgressAtMs"`
	LastBackfillProgressAtMS basequery.NumericValue `json:"lastBackfillProgressAtMs"`
}

type DataHealthJobs struct {
	Queued          basequery.NumericValue `json:"queued"`
	Running         basequery.NumericValue `json:"running"`
	Interrupted     basequery.NumericValue `json:"interrupted"`
	Succeeded       basequery.NumericValue `json:"succeeded"`
	Failed          basequery.NumericValue `json:"failed"`
	Cancelled       basequery.NumericValue `json:"cancelled"`
	DurationCount   basequery.NumericValue `json:"durationCount"`
	DurationTotalMS basequery.NumericValue `json:"durationTotalMs"`
	DurationMaxMS   basequery.NumericValue `json:"durationMaxMs"`
}

type DataHealthSources struct {
	Total                  basequery.NumericValue `json:"total"`
	Unknown                basequery.NumericValue `json:"unknown"`
	Current                basequery.NumericValue `json:"current"`
	Stale                  basequery.NumericValue `json:"stale"`
	Unavailable            basequery.NumericValue `json:"unavailable"`
	ConsecutiveFailures    basequery.NumericValue `json:"consecutiveFailures"`
	MaxConsecutiveFailures basequery.NumericValue `json:"maxConsecutiveFailures"`
	Attempts               basequery.NumericValue `json:"attempts"`
	SucceededAttempts      basequery.NumericValue `json:"succeededAttempts"`
	FailedAttempts         basequery.NumericValue `json:"failedAttempts"`
	CancelledAttempts      basequery.NumericValue `json:"cancelledAttempts"`
	ResponseBytes          basequery.NumericValue `json:"responseBytes"`
	LastAttemptAtMS        basequery.NumericValue `json:"lastAttemptAtMs"`
	LastSuccessAtMS        basequery.NumericValue `json:"lastSuccessAtMs"`
	NextRetryAtMS          basequery.NumericValue `json:"nextRetryAtMs"`
}

type DataHealthResponse struct {
	Meta          basequery.ResponseMeta   `json:"meta"`
	EvaluatedAtMS basequery.NumericValue   `json:"evaluatedAtMs"`
	Window        DataHealthWindow         `json:"window"`
	Runtime       []DataHealthRuntimePoint `json:"runtime"`
	Latest        *DataHealthRuntimePoint  `json:"latest"`
	Scheduler     DataHealthScheduler      `json:"scheduler"`
	Jobs          DataHealthJobs           `json:"jobs"`
	Sources       DataHealthSources        `json:"sources"`
	CurrentJobs   []JobItem                `json:"currentJobs"`
	RecentJobs    []JobItem                `json:"recentJobs"`
	OpenEvents    []HealthItem             `json:"openEvents"`
	RecentEvents  []HealthItem             `json:"recentEvents"`
}

type HomeSwitchStatus string

const (
	HomeSwitchStable           HomeSwitchStatus = "stable"
	HomeSwitchPending          HomeSwitchStatus = "pending"
	HomeSwitchRecoveryRequired HomeSwitchStatus = "recovery_required"
)

type SettingsHomeSnapshot struct {
	Configured        bool             `json:"configured"`
	Generation        string           `json:"generation"`
	SwitchStatus      HomeSwitchStatus `json:"switchStatus"`
	LastSwitchOutcome *string          `json:"lastSwitchOutcome"`
}

type SettingsOnlineSnapshot struct {
	QuotaEnabled        bool `json:"quotaEnabled"`
	ResetCreditsEnabled bool `json:"resetCreditsEnabled"`
}

type SettingsRefreshSnapshot struct {
	QuotaIntervalSeconds        int64 `json:"quotaIntervalSeconds"`
	ResetCreditsIntervalSeconds int64 `json:"resetCreditsIntervalSeconds"`
	ReconcileIntervalSeconds    int64 `json:"reconcileIntervalSeconds"`
	JSONLDebounceMilliseconds   int64 `json:"jsonlDebounceMilliseconds"`
}

type SettingsUpdateSnapshot struct {
	AutoCheckEnabled     bool                   `json:"autoCheckEnabled"`
	AutoDownloadEnabled  bool                   `json:"autoDownloadEnabled"`
	Channel              string                 `json:"channel"`
	CheckIntervalSeconds int64                  `json:"checkIntervalSeconds"`
	SkippedVersion       *string                `json:"skippedVersion"`
	SnoozeUntilMS        basequery.NumericValue `json:"snoozeUntilMs"`
	LastCheckAtMS        basequery.NumericValue `json:"lastCheckAtMs"`
}

type SettingsUISnapshot struct {
	Locale         string `json:"locale"`
	LaunchBehavior string `json:"launchBehavior"`
	OverviewRange  string `json:"overviewRange"`
}

type SettingsSnapshot struct {
	SchemaVersion       int                     `json:"schemaVersion"`
	Revision            string                  `json:"revision"`
	OnboardingCompleted bool                    `json:"onboardingCompleted"`
	Home                SettingsHomeSnapshot    `json:"home"`
	Online              SettingsOnlineSnapshot  `json:"online"`
	Refresh             SettingsRefreshSnapshot `json:"refresh"`
	Updates             SettingsUpdateSnapshot  `json:"updates"`
	UI                  SettingsUISnapshot      `json:"ui"`
}

type EditableValueType string

const (
	EditableBoolean EditableValueType = "boolean"
	EditableInteger EditableValueType = "integer"
	EditableEnum    EditableValueType = "enum"
)

type EditableField struct {
	Key      string            `json:"key"`
	Type     EditableValueType `json:"type"`
	Editable bool              `json:"editable"`
	Minimum  *int64            `json:"minimum"`
	Maximum  *int64            `json:"maximum"`
	Options  []string          `json:"options"`
}

type SettingsResponse struct {
	Meta           basequery.ResponseMeta `json:"meta"`
	Snapshot       SettingsSnapshot       `json:"snapshot"`
	EditableFields []EditableField        `json:"editableFields"`
}
