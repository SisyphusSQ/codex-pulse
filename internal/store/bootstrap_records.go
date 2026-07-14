package store

type BootstrapPlanState string

const (
	BootstrapPlanPending BootstrapPlanState = "pending"
	BootstrapPlanReady   BootstrapPlanState = "ready"
)

type BootstrapETAState string

const (
	BootstrapETAUnknown  BootstrapETAState = "unknown"
	BootstrapETAKnown    BootstrapETAState = "known"
	BootstrapETAComplete BootstrapETAState = "complete"
)

type BootstrapPauseReason string

const (
	BootstrapPauseSourceUnavailable   BootstrapPauseReason = "source_unavailable"
	BootstrapPauseStorageBackpressure BootstrapPauseReason = "storage_backpressure"
	BootstrapPauseApplicationDraining BootstrapPauseReason = "application_draining"
	BootstrapPauseUser                BootstrapPauseReason = "user_paused"
)

type BootstrapLane string

const (
	BootstrapLaneFast      BootstrapLane = "fast"
	BootstrapLaneBackfill  BootstrapLane = "backfill"
	BootstrapLaneReconcile BootstrapLane = "reconcile"
)

type BootstrapTier string

const (
	BootstrapTierActiveAppend BootstrapTier = "active_append"
	BootstrapTierToday        BootstrapTier = "today"
	BootstrapTierRecent7Days  BootstrapTier = "recent_7d"
	BootstrapTierRecent30Days BootstrapTier = "recent_30d"
	BootstrapTierOlder        BootstrapTier = "older"
	BootstrapTierReconcile    BootstrapTier = "reconcile"
)

type BootstrapActionKind string

const (
	BootstrapActionAdded      BootstrapActionKind = "added"
	BootstrapActionUnchanged  BootstrapActionKind = "unchanged"
	BootstrapActionGrown      BootstrapActionKind = "grown"
	BootstrapActionTruncated  BootstrapActionKind = "truncated"
	BootstrapActionMoved      BootstrapActionKind = "moved"
	BootstrapActionReplaced   BootstrapActionKind = "replaced"
	BootstrapActionDeleted    BootstrapActionKind = "deleted"
	BootstrapActionUnreadable BootstrapActionKind = "unreadable"
)

type BootstrapItemState string

const (
	BootstrapItemQueued    BootstrapItemState = "queued"
	BootstrapItemRunning   BootstrapItemState = "running"
	BootstrapItemSucceeded BootstrapItemState = "succeeded"
	BootstrapItemDrifted   BootstrapItemState = "drifted"
	BootstrapItemFailed    BootstrapItemState = "failed"
)

// BootstrapJobFacts 保存首次索引专有状态；公共 lifecycle 仍由 JobRun 表达。
type BootstrapJobFacts struct {
	JobID                string
	SwitchID             string
	HomeGeneration       int64
	HomePath             string
	HomeDeviceID         string
	HomeInode            int64
	DataStoreKey         string
	Strategy             string
	PlanState            BootstrapPlanState
	PlanSHA256           SHA256Digest
	PhaseProgressCurrent int64
	PhaseProgressTotal   int64
	ETAState             BootstrapETAState
	ETARemainingMS       *int64
	PauseReason          *BootstrapPauseReason
	FirstScreenReadyAtMS *int64
	ReconcilePass        int64
	ReconcilePlanAtMS    *int64
	FullHistoryReadyAtMS *int64
	ReconciledAtMS       *int64
	ReconcileChangeCount int64
	ReconcileIssueCount  int64
	UpdatedAtMS          int64
}

// BootstrapPlanItem 持久冻结一个 source action；snapshot 使用 typed 列而非 JSON payload。
type BootstrapPlanItem struct {
	JobID            string
	Ordinal          int64
	Pass             int64
	Lane             BootstrapLane
	Tier             BootstrapTier
	ActionKind       BootstrapActionKind
	Previous         *SourceFingerprint
	Current          *SourceFingerprint
	State            BootstrapItemState
	SourceGeneration *int64
	ProgressCurrent  int64
	ProgressTotal    int64
	UpdatedAtMS      int64
}

type BootstrapPlanItemFilter struct {
	JobID string
	Lane  *BootstrapLane
	State *BootstrapItemState
}

// BootstrapAdvance atomically advances the public job, specialized facts, and
// optionally one fixed plan item.
type BootstrapAdvance struct {
	Job   JobTransition
	Facts BootstrapJobFacts
	Item  *BootstrapPlanItem
}

func validBootstrapPlanState(value BootstrapPlanState) bool {
	return value == BootstrapPlanPending || value == BootstrapPlanReady
}

func validBootstrapETAState(value BootstrapETAState) bool {
	return value == BootstrapETAUnknown || value == BootstrapETAKnown || value == BootstrapETAComplete
}

func validBootstrapPauseReason(value BootstrapPauseReason) bool {
	switch value {
	case BootstrapPauseSourceUnavailable, BootstrapPauseStorageBackpressure,
		BootstrapPauseApplicationDraining, BootstrapPauseUser:
		return true
	default:
		return false
	}
}

func validBootstrapLane(value BootstrapLane) bool {
	return value == BootstrapLaneFast || value == BootstrapLaneBackfill || value == BootstrapLaneReconcile
}

func validBootstrapTier(value BootstrapTier) bool {
	switch value {
	case BootstrapTierActiveAppend, BootstrapTierToday, BootstrapTierRecent7Days,
		BootstrapTierRecent30Days, BootstrapTierOlder, BootstrapTierReconcile:
		return true
	default:
		return false
	}
}

func validBootstrapActionKind(value BootstrapActionKind) bool {
	switch value {
	case BootstrapActionAdded, BootstrapActionUnchanged, BootstrapActionGrown,
		BootstrapActionTruncated, BootstrapActionMoved, BootstrapActionReplaced,
		BootstrapActionDeleted, BootstrapActionUnreadable:
		return true
	default:
		return false
	}
}

func validBootstrapItemState(value BootstrapItemState) bool {
	switch value {
	case BootstrapItemQueued, BootstrapItemRunning, BootstrapItemSucceeded,
		BootstrapItemDrifted, BootstrapItemFailed:
		return true
	default:
		return false
	}
}
