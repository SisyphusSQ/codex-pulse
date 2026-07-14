package store

type bootstrapJobModel struct {
	JobID                string  `gorm:"column:job_id;primaryKey"`
	SwitchID             string  `gorm:"column:switch_id"`
	HomeGeneration       int64   `gorm:"column:home_generation"`
	HomePath             string  `gorm:"column:home_path"`
	HomeDeviceID         string  `gorm:"column:home_device_id"`
	HomeInode            int64   `gorm:"column:home_inode"`
	DataStoreKey         string  `gorm:"column:data_store_key"`
	Strategy             string  `gorm:"column:strategy"`
	PlanState            string  `gorm:"column:plan_state"`
	PlanSHA256           *string `gorm:"column:plan_sha256"`
	PhaseProgressCurrent int64   `gorm:"column:phase_progress_current"`
	PhaseProgressTotal   int64   `gorm:"column:phase_progress_total"`
	ETAState             string  `gorm:"column:eta_state"`
	ETARemainingMS       *int64  `gorm:"column:eta_remaining_ms"`
	PauseReason          *string `gorm:"column:pause_reason"`
	FirstScreenReadyAtMS *int64  `gorm:"column:first_screen_ready_at_ms"`
	ReconcilePass        int64   `gorm:"column:reconcile_pass"`
	ReconcilePlanAtMS    *int64  `gorm:"column:reconcile_plan_at_ms"`
	FullHistoryReadyAtMS *int64  `gorm:"column:full_history_ready_at_ms"`
	ReconciledAtMS       *int64  `gorm:"column:reconciled_at_ms"`
	ReconcileChangeCount int64   `gorm:"column:reconcile_change_count"`
	ReconcileIssueCount  int64   `gorm:"column:reconcile_issue_count"`
	UpdatedAtMS          int64   `gorm:"column:updated_at_ms"`
}

func (bootstrapJobModel) TableName() string { return "bootstrap_jobs" }

type bootstrapPlanItemModel struct {
	JobID            string  `gorm:"column:job_id;primaryKey"`
	Ordinal          int64   `gorm:"column:ordinal;primaryKey"`
	Pass             int64   `gorm:"column:pass"`
	Lane             string  `gorm:"column:lane"`
	Tier             string  `gorm:"column:tier"`
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
	CurrentSourceID  *string `gorm:"column:current_source_file_id"`
	CurrentKind      *string `gorm:"column:current_source_kind"`
	CurrentPath      *string `gorm:"column:current_path"`
	CurrentDeviceID  *string `gorm:"column:current_device_id"`
	CurrentInode     *int64  `gorm:"column:current_inode"`
	CurrentSize      *int64  `gorm:"column:current_size_bytes"`
	CurrentMTimeNS   *int64  `gorm:"column:current_mtime_ns"`
	CurrentPrefixN   *int64  `gorm:"column:current_prefix_bytes"`
	CurrentPrefix    *string `gorm:"column:current_prefix_sha256"`
	CurrentDigest    *string `gorm:"column:current_fingerprint_sha256"`
	State            string  `gorm:"column:state"`
	SourceGeneration *int64  `gorm:"column:source_generation"`
	ProgressCurrent  int64   `gorm:"column:progress_current"`
	ProgressTotal    int64   `gorm:"column:progress_total"`
	UpdatedAtMS      int64   `gorm:"column:updated_at_ms"`
}

func (bootstrapPlanItemModel) TableName() string { return "bootstrap_plan_items" }
