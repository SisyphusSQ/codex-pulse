package retention

type appRuntimeSampleModel struct{}

func (appRuntimeSampleModel) TableName() string { return "app_runtime_samples" }

type healthEventModel struct{}

func (healthEventModel) TableName() string { return "health_events" }

type jobRunModel struct{}

func (jobRunModel) TableName() string { return "job_runs" }

type sourceAttemptModel struct{}

func (sourceAttemptModel) TableName() string { return "source_attempts" }
