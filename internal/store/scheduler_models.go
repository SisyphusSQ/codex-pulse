package store

type schedulerTaskModel struct {
	TaskID                string  `gorm:"column:task_id;primaryKey"`
	DedupeKey             string  `gorm:"column:dedupe_key"`
	TargetKind            string  `gorm:"column:target_kind"`
	AdmissionTargetID     string  `gorm:"column:admission_target_id"`
	TargetID              string  `gorm:"column:target_id"`
	HomeGeneration        int64   `gorm:"column:home_generation"`
	Lane                  string  `gorm:"column:lane"`
	AdmissionServiceClass string  `gorm:"column:admission_service_class"`
	ServiceClass          string  `gorm:"column:service_class"`
	State                 string  `gorm:"column:state"`
	QueueOrderMS          int64   `gorm:"column:queue_order_ms"`
	EnqueuedAtMS          int64   `gorm:"column:enqueued_at_ms"`
	FirstStartedAtMS      *int64  `gorm:"column:first_started_at_ms"`
	LastStartedAtMS       *int64  `gorm:"column:last_started_at_ms"`
	FinishedAtMS          *int64  `gorm:"column:finished_at_ms"`
	FilesProcessed        int64   `gorm:"column:files_processed"`
	BytesProcessed        int64   `gorm:"column:bytes_processed"`
	SliceCount            int64   `gorm:"column:slice_count"`
	LastErrorClass        *string `gorm:"column:last_error_class"`
	UpdatedAtMS           int64   `gorm:"column:updated_at_ms"`
}

func (schedulerTaskModel) TableName() string { return "scheduler_tasks" }

type schedulerCycleModel struct {
	CommitOrder          int64  `gorm:"column:commit_order;primaryKey;autoIncrement"`
	CycleID              string `gorm:"column:cycle_id"`
	TaskID               string `gorm:"column:task_id"`
	Lane                 string `gorm:"column:lane"`
	SelectionReason      string `gorm:"column:selection_reason"`
	StopReason           string `gorm:"column:stop_reason"`
	Outcome              string `gorm:"column:outcome"`
	BudgetFiles          int64  `gorm:"column:budget_files"`
	BudgetBytes          int64  `gorm:"column:budget_bytes"`
	BudgetActiveMS       int64  `gorm:"column:budget_active_ms"`
	ConsumedFiles        int64  `gorm:"column:consumed_files"`
	ConsumedBytes        int64  `gorm:"column:consumed_bytes"`
	ActiveMS             int64  `gorm:"column:active_ms"`
	LiveDepth            int64  `gorm:"column:live_depth"`
	BackfillDepth        int64  `gorm:"column:backfill_depth"`
	OldestLiveWaitMS     int64  `gorm:"column:oldest_live_wait_ms"`
	OldestBackfillWaitMS int64  `gorm:"column:oldest_backfill_wait_ms"`
	StartedAtMS          int64  `gorm:"column:started_at_ms"`
	FinishedAtMS         int64  `gorm:"column:finished_at_ms"`
}

func (schedulerCycleModel) TableName() string { return "scheduler_cycles" }

type liveScanJobModel struct {
	JobID            string  `gorm:"column:job_id;primaryKey"`
	RequestID        string  `gorm:"column:request_id"`
	HomeGeneration   int64   `gorm:"column:home_generation"`
	HomePath         string  `gorm:"column:home_path"`
	HomeDeviceID     string  `gorm:"column:home_device_id"`
	HomeInode        int64   `gorm:"column:home_inode"`
	ActionKind       string  `gorm:"column:action_kind"`
	PreviousSourceID *string `gorm:"column:previous_source_file_id"`
	PreviousKind     *string `gorm:"column:previous_source_kind"`
	PreviousPath     *string `gorm:"column:previous_path"`
	PreviousDeviceID *string `gorm:"column:previous_device_id"`
	PreviousInode    *int64  `gorm:"column:previous_inode"`
	PreviousSize     *int64  `gorm:"column:previous_size_bytes"`
	PreviousMTimeNS  *int64  `gorm:"column:previous_mtime_ns"`
	PreviousPrefixN  *int64  `gorm:"column:previous_prefix_bytes"`
	PreviousPrefix   *string `gorm:"column:previous_prefix_sha256"`
	PreviousDigest   *string `gorm:"column:previous_fingerprint_sha256"`
	CurrentSourceID  string  `gorm:"column:current_source_file_id"`
	CurrentKind      string  `gorm:"column:current_source_kind"`
	CurrentPath      string  `gorm:"column:current_path"`
	CurrentDeviceID  string  `gorm:"column:current_device_id"`
	CurrentInode     int64   `gorm:"column:current_inode"`
	CurrentSize      int64   `gorm:"column:current_size_bytes"`
	CurrentMTimeNS   int64   `gorm:"column:current_mtime_ns"`
	CurrentPrefixN   int64   `gorm:"column:current_prefix_bytes"`
	CurrentPrefix    string  `gorm:"column:current_prefix_sha256"`
	CurrentDigest    string  `gorm:"column:current_fingerprint_sha256"`
	UpdatedAtMS      int64   `gorm:"column:updated_at_ms"`
}

func (liveScanJobModel) TableName() string { return "live_scan_jobs" }
