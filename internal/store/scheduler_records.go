package store

type SchedulerTargetKind string

const (
	SchedulerTargetBootstrap SchedulerTargetKind = "bootstrap"
	SchedulerTargetLiveScan  SchedulerTargetKind = "live_scan"
)

type SchedulerLane string

const (
	SchedulerLaneLive     SchedulerLane = "live"
	SchedulerLaneBackfill SchedulerLane = "backfill"
)

type SchedulerServiceClass string

const (
	SchedulerServiceBackground  SchedulerServiceClass = "background"
	SchedulerServiceInteractive SchedulerServiceClass = "interactive"
)

type SchedulerTaskState string

const (
	SchedulerTaskQueued      SchedulerTaskState = "queued"
	SchedulerTaskRunning     SchedulerTaskState = "running"
	SchedulerTaskSucceeded   SchedulerTaskState = "succeeded"
	SchedulerTaskFailed      SchedulerTaskState = "failed"
	SchedulerTaskInterrupted SchedulerTaskState = "interrupted"
)

type SchedulerSelectionReason string

const (
	SchedulerSelectionLivePriority     SchedulerSelectionReason = "live_priority"
	SchedulerSelectionLiveOnly         SchedulerSelectionReason = "live_only"
	SchedulerSelectionBackfillOnly     SchedulerSelectionReason = "backfill_only"
	SchedulerSelectionBackfillFairness SchedulerSelectionReason = "backfill_fairness"
)

type SchedulerStopReason string

const (
	SchedulerStopCompleted       SchedulerStopReason = "completed"
	SchedulerStopFileBudget      SchedulerStopReason = "file_budget"
	SchedulerStopByteBudget      SchedulerStopReason = "byte_budget"
	SchedulerStopTimeBudget      SchedulerStopReason = "time_budget"
	SchedulerStopSystemPressure  SchedulerStopReason = "system_pressure"
	SchedulerStopLivePreempted   SchedulerStopReason = "live_preempted"
	SchedulerStopCancelled       SchedulerStopReason = "cancelled"
	SchedulerStopDependencyError SchedulerStopReason = "dependency_error"
	SchedulerStopWorkerPanic     SchedulerStopReason = "worker_panic"
)

type SchedulerCycleOutcome string

const (
	SchedulerCycleCompleted   SchedulerCycleOutcome = "completed"
	SchedulerCycleYielded     SchedulerCycleOutcome = "yielded"
	SchedulerCycleFailed      SchedulerCycleOutcome = "failed"
	SchedulerCycleInterrupted SchedulerCycleOutcome = "interrupted"
)

type LiveScanActionKind string

const (
	LiveScanActionAdded     LiveScanActionKind = "added"
	LiveScanActionUnchanged LiveScanActionKind = "unchanged"
	LiveScanActionGrown     LiveScanActionKind = "grown"
	LiveScanActionTruncated LiveScanActionKind = "truncated"
	LiveScanActionMoved     LiveScanActionKind = "moved"
	LiveScanActionReplaced  LiveScanActionKind = "replaced"
)

// LiveScanJob 保存一个live action attempt的confirmed Home与typed snapshot。
type LiveScanJob struct {
	JobID          string
	RequestID      string
	HomeGeneration int64
	HomePath       string
	HomeDeviceID   string
	HomeInode      int64
	ActionKind     LiveScanActionKind
	Previous       *SourceFingerprint
	Current        SourceFingerprint
	UpdatedAtMS    int64
}

// SchedulerTask 保存一个持久队列任务；target job 持有业务生命周期与真实游标。
type SchedulerTask struct {
	TaskID           string
	DedupeKey        string
	TargetKind       SchedulerTargetKind
	TargetID         string
	HomeGeneration   int64
	Lane             SchedulerLane
	ServiceClass     SchedulerServiceClass
	State            SchedulerTaskState
	QueueOrderMS     int64
	EnqueuedAtMS     int64
	FirstStartedAtMS *int64
	LastStartedAtMS  *int64
	FinishedAtMS     *int64
	FilesProcessed   int64
	BytesProcessed   int64
	SliceCount       int64
	LastErrorClass   *RuntimeErrorClass
	UpdatedAtMS      int64
}

// SchedulerCycle 保存一次选择与预算切片的结构化观测。
type SchedulerCycle struct {
	CycleID              string
	TaskID               string
	Lane                 SchedulerLane
	SelectionReason      SchedulerSelectionReason
	StopReason           SchedulerStopReason
	Outcome              SchedulerCycleOutcome
	BudgetFiles          int64
	BudgetBytes          int64
	BudgetActiveMS       int64
	ConsumedFiles        int64
	ConsumedBytes        int64
	ActiveMS             int64
	LiveDepth            int64
	BackfillDepth        int64
	OldestLiveWaitMS     int64
	OldestBackfillWaitMS int64
	StartedAtMS          int64
	FinishedAtMS         int64
}

type SchedulerTaskFilter struct {
	State  *SchedulerTaskState
	Lane   *SchedulerLane
	Active *bool
	Limit  int
}

// SchedulerQueueSnapshot 是Store对全部queued task的精确lane聚合，不受列表分页上限影响。
type SchedulerQueueSnapshot struct {
	LiveCandidate     *SchedulerTask
	BackfillCandidate *SchedulerTask
	LiveDepth         int64
	BackfillDepth     int64
	MaxQueueOrderMS   int64
}

// SchedulerTaskCursor 是recoverable task按(queue_order_ms, task_id)稳定分页的游标。
type SchedulerTaskCursor struct {
	QueueOrderMS int64
	TaskID       string
}

type SchedulerCycleFilter struct {
	TaskID *string
	Lane   *SchedulerLane
	Limit  int
}

// SchedulerCycleCommit 原子推进task并追加一个cycle事实。
type SchedulerCycleCommit struct {
	TaskID        string
	ExpectedState SchedulerTaskState
	State         SchedulerTaskState
	QueueOrderMS  int64
	FilesDelta    int64
	BytesDelta    int64
	ErrorClass    *RuntimeErrorClass
	AtMS          int64
	Cycle         SchedulerCycle
}

func validSchedulerTargetKind(value SchedulerTargetKind) bool {
	return value == SchedulerTargetBootstrap || value == SchedulerTargetLiveScan
}

func validSchedulerLane(value SchedulerLane) bool {
	return value == SchedulerLaneLive || value == SchedulerLaneBackfill
}

func validSchedulerServiceClass(value SchedulerServiceClass) bool {
	return value == SchedulerServiceBackground || value == SchedulerServiceInteractive
}

func validSchedulerTaskState(value SchedulerTaskState) bool {
	switch value {
	case SchedulerTaskQueued, SchedulerTaskRunning, SchedulerTaskSucceeded,
		SchedulerTaskFailed, SchedulerTaskInterrupted:
		return true
	default:
		return false
	}
}

func validSchedulerSelectionReason(value SchedulerSelectionReason) bool {
	switch value {
	case SchedulerSelectionLivePriority, SchedulerSelectionLiveOnly,
		SchedulerSelectionBackfillOnly, SchedulerSelectionBackfillFairness:
		return true
	default:
		return false
	}
}

func validSchedulerStopReason(value SchedulerStopReason) bool {
	switch value {
	case SchedulerStopCompleted, SchedulerStopFileBudget, SchedulerStopByteBudget,
		SchedulerStopTimeBudget, SchedulerStopSystemPressure, SchedulerStopLivePreempted,
		SchedulerStopCancelled, SchedulerStopDependencyError, SchedulerStopWorkerPanic:
		return true
	default:
		return false
	}
}

func validSchedulerCycleOutcome(value SchedulerCycleOutcome) bool {
	switch value {
	case SchedulerCycleCompleted, SchedulerCycleYielded, SchedulerCycleFailed, SchedulerCycleInterrupted:
		return true
	default:
		return false
	}
}

func validLiveScanActionKind(value LiveScanActionKind) bool {
	switch value {
	case LiveScanActionAdded, LiveScanActionUnchanged, LiveScanActionGrown,
		LiveScanActionTruncated, LiveScanActionMoved, LiveScanActionReplaced:
		return true
	default:
		return false
	}
}
