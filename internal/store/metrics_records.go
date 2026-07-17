package store

const (
	MetricsSnapshotWindowMS         int64 = 24 * 60 * 60 * 1_000
	MetricsDetailedSampleIntervalMS int64 = 5 * 1_000
	MaxAppRuntimeSampleQueryLimit         = int(MetricsSnapshotWindowMS / MetricsDetailedSampleIntervalMS)
)

// AppRuntimeSample 是 Codex Pulse 进程在一个时间点的 content-free 资源事实。
// CPUPercent 使用单核 100% 口径，多核进程可以超过 100。
type AppRuntimeSample struct {
	CapturedAtMS            int64
	CPUPercent              float64
	CPUUserMS               int64
	CPUSystemMS             int64
	RSSBytes                int64
	PeakRSSBytes            int64
	GoroutineCount          int64
	DBBytes                 int64
	WALBytes                int64
	DiskFreeBytes           int64
	LiveQueueDepth          int64
	BackfillQueueDepth      int64
	OldestLiveWaitMS        int64
	OldestBackfillWaitMS    int64
	QueryCount              int64
	QueryTotalMicros        int64
	QueryMaxMicros          int64
	CollectorDurationMicros int64
	DroppedSamples          int64
}

// AppRuntimeSampleFilter 使用半开时间窗口读取 app runtime samples。
type AppRuntimeSampleFilter struct {
	FromMS  int64
	UntilMS int64
	Limit   int
}

// MetricsSnapshotFilter 固定统一 metrics snapshot 的 24h 半开窗口。
type MetricsSnapshotFilter struct {
	FromMS  int64
	UntilMS int64
}

type SchedulerTaskStateMetric struct {
	State SchedulerTaskState
	Count int64
}

type SchedulerLaneMetric struct {
	Lane  SchedulerLane
	Count int64
}

type SchedulerServiceClassMetric struct {
	ServiceClass SchedulerServiceClass
	Count        int64
}

type SchedulerStopReasonMetric struct {
	Reason SchedulerStopReason
	Count  int64
}

type SchedulerRetryDispositionMetric struct {
	Disposition SchedulerRetryDisposition
	Count       int64
}

type RuntimeErrorClassMetric struct {
	ErrorClass RuntimeErrorClass
	Count      int64
}

type SourceFailureCodeMetric struct {
	FailureCode SourceFailureCode
	Count       int64
}

// SchedulerMetrics 汇总窗口内已提交 scheduler cycle 的有限数值事实。
type SchedulerMetrics struct {
	CycleCount               int64
	CompletedCycles          int64
	YieldedCycles            int64
	FailedCycles             int64
	InterruptedCycles        int64
	FilesScanned             int64
	BytesRead                int64
	ActiveMS                 int64
	MaxCycleActiveMS         int64
	TaskStates               []SchedulerTaskStateMetric
	Lanes                    []SchedulerLaneMetric
	ServiceClasses           []SchedulerServiceClassMetric
	StopReasons              []SchedulerStopReasonMetric
	RetryDispositions        []SchedulerRetryDispositionMetric
	LastProgressAtMS         *int64
	LastBackfillProgressAtMS *int64
}

// JobMetrics 分离当前可运行/可恢复状态与窗口内 terminal 结果。
type JobMetrics struct {
	Queued          int64
	Running         int64
	Interrupted     int64
	Succeeded       int64
	Failed          int64
	Cancelled       int64
	DurationCount   int64
	DurationTotalMS int64
	DurationMaxMS   int64
}

// SourceMetrics 汇总当前 freshness 与窗口内 content-free attempt 结果。
type SourceMetrics struct {
	Total                  int64
	Unknown                int64
	Current                int64
	Stale                  int64
	Unavailable            int64
	ConsecutiveFailures    int64
	MaxConsecutiveFailures int64
	Attempts               int64
	SucceededAttempts      int64
	FailedAttempts         int64
	CancelledAttempts      int64
	ResponseBytes          int64
	LastAttemptAtMS        *int64
	LastSuccessAtMS        *int64
	NextRetryAtMS          *int64
	CurrentErrorClasses    []RuntimeErrorClassMetric
	CurrentFailureCodes    []SourceFailureCodeMetric
	AttemptErrorClasses    []RuntimeErrorClassMetric
	AttemptFailureCodes    []SourceFailureCodeMetric
}

// MetricsSnapshot 是后续 health evaluator 与 Data Health 消费的规范化只读事实。
type MetricsSnapshot struct {
	FromMS         int64
	UntilMS        int64
	RuntimeSamples []AppRuntimeSample
	Scheduler      SchedulerMetrics
	Jobs           JobMetrics
	Sources        SourceMetrics
}

// HealthEvaluationSnapshot 在一个只读事务中固定健康评估所需的全部事实。
type HealthEvaluationSnapshot struct {
	Metrics      MetricsSnapshot
	Lifecycle    *SchedulerLifecycle
	ActiveHealth []HealthEventMetric
}

type HealthEventMetric struct {
	Domain   HealthDomain
	Severity HealthSeverity
	Code     HealthCode
	Count    int64
}
