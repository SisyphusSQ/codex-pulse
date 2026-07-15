package store

type LifecyclePauseScope string

const (
	LifecyclePauseNone     LifecyclePauseScope = "none"
	LifecyclePauseBackfill LifecyclePauseScope = "backfill"
	LifecyclePauseAll      LifecyclePauseScope = "all"
)

type LifecycleSystemState string

const (
	LifecycleSystemAwake    LifecycleSystemState = "awake"
	LifecycleSystemSleeping LifecycleSystemState = "sleeping"
)

type LifecycleTransition string

const (
	LifecycleTransitionSteady      LifecycleTransition = "steady"
	LifecycleTransitionDraining    LifecycleTransition = "draining"
	LifecycleTransitionReconciling LifecycleTransition = "reconciling"
	LifecycleTransitionBlocked     LifecycleTransition = "blocked"
)

type LifecycleSourceState string

const (
	LifecycleSourceUnknown     LifecycleSourceState = "unknown"
	LifecycleSourceAvailable   LifecycleSourceState = "available"
	LifecycleSourceUnavailable LifecycleSourceState = "unavailable"
)

// SchedulerLifecycle 是单一 Codex Home 的持久调度控制事实。用户暂停、系统休眠、
// transition 与来源可用性保持正交，避免 wake 隐式清除用户意图。
type SchedulerLifecycle struct {
	HomeGeneration int64
	UserPauseScope LifecyclePauseScope
	SystemState    LifecycleSystemState
	Transition     LifecycleTransition
	SourceState    LifecycleSourceState
	LastEventID    string
	Revision       int64
	UpdatedAtMS    int64
}

type SchedulerRetryDisposition string

const (
	SchedulerRetryWaiting  SchedulerRetryDisposition = "waiting"
	SchedulerRetryBlocked  SchedulerRetryDisposition = "blocked"
	SchedulerRetryResolved SchedulerRetryDisposition = "resolved"
)

type SchedulerRecoveryAction string

const (
	SchedulerRecoveryNone            SchedulerRecoveryAction = "none"
	SchedulerRecoveryRetry           SchedulerRecoveryAction = "retry"
	SchedulerRecoveryCheckSource     SchedulerRecoveryAction = "check_source"
	SchedulerRecoveryGrantPermission SchedulerRecoveryAction = "grant_permission"
	SchedulerRecoveryFreeSpace       SchedulerRecoveryAction = "free_space"
	SchedulerRecoveryChooseHome      SchedulerRecoveryAction = "choose_home"
	SchedulerRecoveryRepairStore     SchedulerRecoveryAction = "repair_store"
)

// SchedulerRetryState 只保存稳定分类和用户动作，不保存原始错误、路径或正文。
type SchedulerRetryState struct {
	TaskID         string
	Disposition    SchedulerRetryDisposition
	FailureCount   int64
	LastErrorClass RuntimeErrorClass
	NextRetryAtMS  *int64
	RecoveryAction SchedulerRecoveryAction
	Revision       int64
	UpdatedAtMS    int64
}

type SchedulerRetryMutation struct {
	ExpectedRevision int64
	Disposition      SchedulerRetryDisposition
	FailureCount     int64
	LastErrorClass   RuntimeErrorClass
	NextRetryAtMS    *int64
	RecoveryAction   SchedulerRecoveryAction
}

type SchedulerRetryCursor struct {
	NextRetryAtMS int64
	TaskID        string
}

func validLifecyclePauseScope(value LifecyclePauseScope) bool {
	return value == LifecyclePauseNone || value == LifecyclePauseBackfill || value == LifecyclePauseAll
}

func validLifecycleSystemState(value LifecycleSystemState) bool {
	return value == LifecycleSystemAwake || value == LifecycleSystemSleeping
}

func validLifecycleTransition(value LifecycleTransition) bool {
	switch value {
	case LifecycleTransitionSteady, LifecycleTransitionDraining,
		LifecycleTransitionReconciling, LifecycleTransitionBlocked:
		return true
	default:
		return false
	}
}

func validLifecycleSourceState(value LifecycleSourceState) bool {
	return value == LifecycleSourceUnknown || value == LifecycleSourceAvailable ||
		value == LifecycleSourceUnavailable
}

func validSchedulerRetryDisposition(value SchedulerRetryDisposition) bool {
	return value == SchedulerRetryWaiting || value == SchedulerRetryBlocked ||
		value == SchedulerRetryResolved
}

func validSchedulerRecoveryAction(value SchedulerRecoveryAction) bool {
	switch value {
	case SchedulerRecoveryNone, SchedulerRecoveryRetry, SchedulerRecoveryCheckSource,
		SchedulerRecoveryGrantPermission, SchedulerRecoveryFreeSpace,
		SchedulerRecoveryChooseHome, SchedulerRecoveryRepairStore:
		return true
	default:
		return false
	}
}
