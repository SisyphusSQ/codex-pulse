package preferences

const (
	CurrentPreferencesSchemaVersion = 2
	DefaultDataStoreKey             = "default"
)

type UpdateChannel string

const UpdateChannelStable UpdateChannel = "stable"

type LaunchBehavior string

const (
	LaunchBehaviorMainWindow LaunchBehavior = "main_window"
	LaunchBehaviorTray       LaunchBehavior = "tray"
)

type OverviewRange string

const (
	OverviewRangeQuotaWeek  OverviewRange = "quota_week"
	OverviewRangeToday      OverviewRange = "today"
	OverviewRangeSevenDays  OverviewRange = "seven_days"
	OverviewRangeThirtyDays OverviewRange = "thirty_days"
)

type HomeSwitchStrategy string

const (
	HomeSwitchIndependentDatabase HomeSwitchStrategy = "independent_database"
	HomeSwitchClearAndRebuild     HomeSwitchStrategy = "clear_and_rebuild"
)

type HomeSwitchOutcome string

const (
	HomeSwitchCompleted  HomeSwitchOutcome = "completed"
	HomeSwitchRolledBack HomeSwitchOutcome = "rolled_back"
)

type OnboardingPreferences struct {
	Version   int  `json:"version"`
	Completed bool `json:"completed"`
}

type CodexHomePreferences struct {
	Source       ConfirmedSource `json:"source"`
	Generation   uint64          `json:"generation"`
	DataStoreKey string          `json:"data_store_key"`
}

type OnlinePreferences struct {
	QuotaEnabled        bool `json:"quota_enabled"`
	ResetCreditsEnabled bool `json:"reset_credits_enabled"`
}

type RefreshPreferences struct {
	QuotaIntervalSeconds        int64 `json:"quota_interval_seconds"`
	ResetCreditsIntervalSeconds int64 `json:"reset_credits_interval_seconds"`
	ReconcileIntervalSeconds    int64 `json:"reconcile_interval_seconds"`
	JSONLDebounceMilliseconds   int64 `json:"jsonl_debounce_milliseconds"`
}

type UpdatePreferences struct {
	AutoCheckEnabled     bool          `json:"auto_check_enabled"`
	AutoDownloadEnabled  bool          `json:"auto_download_enabled"`
	Channel              UpdateChannel `json:"channel"`
	CheckIntervalSeconds int64         `json:"check_interval_seconds"`
	SkippedVersion       *string       `json:"skipped_version,omitempty"`
	SnoozeUntilMS        *int64        `json:"snooze_until_ms,omitempty"`
	LastCheckAtMS        *int64        `json:"last_check_at_ms,omitempty"`
}

type UIPreferences struct {
	Locale         string         `json:"locale"`
	LaunchBehavior LaunchBehavior `json:"launch_behavior"`
	OverviewRange  OverviewRange  `json:"overview_range"`
}

// HomeSwitchJournal 是唯一持久中间态。journal 存在时 CodexHome 指向 Target，
// 使旧 generation 的任务无法继续被当作 active source 接纳。
type HomeSwitchJournal struct {
	SwitchID    string               `json:"switch_id"`
	AttemptID   string               `json:"attempt_id"`
	Previous    CodexHomePreferences `json:"previous"`
	Target      CodexHomePreferences `json:"target"`
	Strategy    HomeSwitchStrategy   `json:"strategy"`
	StartedAtMS int64                `json:"started_at_ms"`
}

// HomeResumeJournal 在旧 generation 可能已进入 draining 后持久化恢复责任。
// 只有 Resume 幂等成功并写回 rolled_back audit 后才能清除。
type HomeResumeJournal struct {
	SwitchID         string             `json:"switch_id"`
	AttemptID        string             `json:"attempt_id"`
	Generation       uint64             `json:"generation"`
	TargetGeneration uint64             `json:"target_generation"`
	Strategy         HomeSwitchStrategy `json:"strategy"`
	StartedAtMS      int64              `json:"started_at_ms"`
}

type HomeSwitchAudit struct {
	SwitchID       string             `json:"switch_id"`
	FromGeneration uint64             `json:"from_generation"`
	ToGeneration   uint64             `json:"to_generation"`
	Strategy       HomeSwitchStrategy `json:"strategy"`
	Outcome        HomeSwitchOutcome  `json:"outcome"`
	FinishedAtMS   int64              `json:"finished_at_ms"`
}

// Snapshot 是权威私有 preferences domain contract，与 SQLite model 和 RPC/UI DTO 隔离。
type Snapshot struct {
	SchemaVersion int                    `json:"schema_version"`
	Revision      uint64                 `json:"revision"`
	Onboarding    OnboardingPreferences  `json:"onboarding"`
	CodexHome     CodexHomePreferences   `json:"codex_home"`
	Online        OnlinePreferences      `json:"online"`
	Refresh       RefreshPreferences     `json:"refresh"`
	Updates       UpdatePreferences      `json:"updates"`
	UI            UIPreferences          `json:"ui"`
	DetachedHomes []CodexHomePreferences `json:"detached_homes,omitempty"`
	PendingSwitch *HomeSwitchJournal     `json:"pending_switch,omitempty"`
	PendingResume *HomeResumeJournal     `json:"pending_resume,omitempty"`
	LastSwitch    *HomeSwitchAudit       `json:"last_switch,omitempty"`
}

func DefaultRefreshPreferences() RefreshPreferences {
	return RefreshPreferences{
		QuotaIntervalSeconds: 300, ResetCreditsIntervalSeconds: 1800,
		ReconcileIntervalSeconds: 1800, JSONLDebounceMilliseconds: 4000,
	}
}

func DefaultUpdatePreferences() UpdatePreferences {
	return UpdatePreferences{
		AutoCheckEnabled: true, AutoDownloadEnabled: false,
		Channel: UpdateChannelStable, CheckIntervalSeconds: 3600,
	}
}

func DefaultUIPreferences() UIPreferences {
	return UIPreferences{
		Locale: "zh-CN", LaunchBehavior: LaunchBehaviorTray, OverviewRange: OverviewRangeQuotaWeek,
	}
}
