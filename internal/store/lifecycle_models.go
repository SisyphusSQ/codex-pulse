package store

type schedulerLifecycleModel struct {
	ControlKey     int64  `gorm:"column:control_key;primaryKey"`
	HomeGeneration int64  `gorm:"column:home_generation"`
	UserPauseScope string `gorm:"column:user_pause_scope"`
	SystemState    string `gorm:"column:system_state"`
	Transition     string `gorm:"column:transition"`
	SourceState    string `gorm:"column:source_state"`
	LastEventID    string `gorm:"column:last_event_id"`
	Revision       int64  `gorm:"column:revision"`
	UpdatedAtMS    int64  `gorm:"column:updated_at_ms"`
}

func (schedulerLifecycleModel) TableName() string { return "scheduler_lifecycle" }

type schedulerRetryStateModel struct {
	TaskID         string `gorm:"column:task_id;primaryKey"`
	Disposition    string `gorm:"column:disposition"`
	FailureCount   int64  `gorm:"column:failure_count"`
	LastErrorClass string `gorm:"column:last_error_class"`
	NextRetryAtMS  *int64 `gorm:"column:next_retry_at_ms"`
	RecoveryAction string `gorm:"column:recovery_action"`
	Revision       int64  `gorm:"column:revision"`
	UpdatedAtMS    int64  `gorm:"column:updated_at_ms"`
}

func (schedulerRetryStateModel) TableName() string { return "scheduler_retry_states" }
