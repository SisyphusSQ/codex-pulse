package store

// 下列类型只描述 SQLite persistence adapter，不得作为业务或 UI contract 暴露。

type projectModel struct {
	ProjectID          string  `gorm:"column:project_id;primaryKey"`
	DisplayName        string  `gorm:"column:display_name"`
	RootPath           string  `gorm:"column:root_path"`
	GitRemoteSanitized *string `gorm:"column:git_remote_sanitized"`
	CreatedAtMS        int64   `gorm:"column:created_at_ms"`
	UpdatedAtMS        int64   `gorm:"column:updated_at_ms"`
}

func (projectModel) TableName() string { return "projects" }

type sessionModel struct {
	SessionID     string  `gorm:"column:session_id;primaryKey"`
	Provider      string  `gorm:"column:provider"`
	Originator    *string `gorm:"column:originator"`
	SourceKind    string  `gorm:"column:source_kind"`
	ModelProvider *string `gorm:"column:model_provider"`
	InitialCWD    *string `gorm:"column:initial_cwd"`
	ProjectID     *string `gorm:"column:project_id"`
	CLIVersion    *string `gorm:"column:cli_version"`
	CreatedAtMS   int64   `gorm:"column:created_at_ms"`
	FirstSeenAtMS int64   `gorm:"column:first_seen_at_ms"`
	LastSeenAtMS  int64   `gorm:"column:last_seen_at_ms"`
}

func (sessionModel) TableName() string { return "sessions" }

type turnModel struct {
	TurnID           string  `gorm:"column:turn_id;primaryKey"`
	SessionID        string  `gorm:"column:session_id"`
	StartedAtMS      int64   `gorm:"column:started_at_ms"`
	CompletedAtMS    *int64  `gorm:"column:completed_at_ms"`
	Outcome          *string `gorm:"column:outcome"`
	Model            *string `gorm:"column:model"`
	ReasoningEffort  *string `gorm:"column:reasoning_effort"`
	CWD              *string `gorm:"column:cwd"`
	ProjectID        *string `gorm:"column:project_id"`
	SourceGeneration int64   `gorm:"column:source_generation"`
	StartOffset      int64   `gorm:"column:start_offset"`
	CompleteOffset   *int64  `gorm:"column:complete_offset"`
}

func (turnModel) TableName() string { return "turns" }

type turnUsageModel struct {
	TurnID            string `gorm:"column:turn_id;primaryKey"`
	ObservedAtMS      int64  `gorm:"column:observed_at_ms"`
	IsFinal           bool   `gorm:"column:is_final"`
	InputTokens       *int64 `gorm:"column:input_tokens"`
	CachedInputTokens *int64 `gorm:"column:cached_input_tokens"`
	OutputTokens      *int64 `gorm:"column:output_tokens"`
	ReasoningTokens   *int64 `gorm:"column:reasoning_tokens"`
	ContextWindow     *int64 `gorm:"column:context_window"`
	SourceGeneration  int64  `gorm:"column:source_generation"`
	SourceOffset      int64  `gorm:"column:source_offset"`
	Confidence        string `gorm:"column:confidence"`
	UpdatedAtMS       int64  `gorm:"column:updated_at_ms"`
}

func (turnUsageModel) TableName() string { return "turn_usage" }

type sessionCurrentModel struct {
	SessionID             string  `gorm:"column:session_id;primaryKey"`
	ThreadName            *string `gorm:"column:thread_name"`
	ThreadNameUpdatedAtMS *int64  `gorm:"column:thread_name_updated_at_ms"`
	ActiveTurnID          *string `gorm:"column:active_turn_id"`
	CurrentModel          *string `gorm:"column:current_model"`
	CurrentCWD            *string `gorm:"column:current_cwd"`
	LastActivityAtMS      *int64  `gorm:"column:last_activity_at_ms"`
	UpdatedAtMS           int64   `gorm:"column:updated_at_ms"`
}

func (sessionCurrentModel) TableName() string { return "session_current" }

type sessionUsageCurrentModel struct {
	SessionID            string `gorm:"column:session_id;primaryKey"`
	CounterEpoch         int64  `gorm:"column:counter_epoch"`
	TotalInputTokens     *int64 `gorm:"column:total_input_tokens"`
	TotalCachedTokens    *int64 `gorm:"column:total_cached_tokens"`
	TotalOutputTokens    *int64 `gorm:"column:total_output_tokens"`
	TotalReasoningTokens *int64 `gorm:"column:total_reasoning_tokens"`
	ObservedAtMS         int64  `gorm:"column:observed_at_ms"`
	SourceGeneration     int64  `gorm:"column:source_generation"`
	SourceOffset         int64  `gorm:"column:source_offset"`
	CounterState         string `gorm:"column:counter_state"`
}

func (sessionUsageCurrentModel) TableName() string { return "session_usage_current" }

type sourceFileModel struct {
	SourceFileID     string  `gorm:"column:source_file_id;primaryKey"`
	Provider         string  `gorm:"column:provider"`
	SessionID        *string `gorm:"column:session_id"`
	CurrentPath      string  `gorm:"column:current_path"`
	DeviceID         string  `gorm:"column:device_id"`
	Inode            int64   `gorm:"column:inode"`
	SizeBytes        int64   `gorm:"column:size_bytes"`
	MTimeNS          int64   `gorm:"column:mtime_ns"`
	ParsedOffset     int64   `gorm:"column:parsed_offset"`
	ParserVersion    string  `gorm:"column:parser_version"`
	ActiveGeneration int64   `gorm:"column:active_generation"`
	State            string  `gorm:"column:state"`
	LastScannedAtMS  *int64  `gorm:"column:last_scanned_at_ms"`
	LastErrorClass   *string `gorm:"column:last_error_class"`
	UpdatedAtMS      int64   `gorm:"column:updated_at_ms"`
}

func (sourceFileModel) TableName() string { return "source_files" }

type sourceStateModel struct {
	SourceInstanceID    string  `gorm:"column:source_instance_id;primaryKey"`
	SourceType          string  `gorm:"column:source_type"`
	ScopeKey            string  `gorm:"column:scope_key"`
	LastAttemptAtMS     *int64  `gorm:"column:last_attempt_at_ms"`
	LastSuccessAtMS     *int64  `gorm:"column:last_success_at_ms"`
	NextDueAtMS         *int64  `gorm:"column:next_due_at_ms"`
	ConsecutiveFailures int64   `gorm:"column:consecutive_failures"`
	LastErrorClass      *string `gorm:"column:last_error_class"`
	FreshnessState      string  `gorm:"column:freshness_state"`
	CursorVersion       int64   `gorm:"column:cursor_version"`
	UpdatedAtMS         int64   `gorm:"column:updated_at_ms"`
}

func (sourceStateModel) TableName() string { return "source_state" }

type sourceAttemptModel struct {
	RequestID        string  `gorm:"column:request_id;primaryKey"`
	SourceInstanceID string  `gorm:"column:source_instance_id"`
	StartedAtMS      int64   `gorm:"column:started_at_ms"`
	FinishedAtMS     int64   `gorm:"column:finished_at_ms"`
	Outcome          string  `gorm:"column:outcome"`
	HTTPStatus       *int64  `gorm:"column:http_status"`
	ErrorClass       *string `gorm:"column:error_class"`
	PayloadSHA256    *string `gorm:"column:payload_sha256"`
}

func (sourceAttemptModel) TableName() string { return "source_attempts" }

type jobRunModel struct {
	JobID            string  `gorm:"column:job_id;primaryKey"`
	JobType          string  `gorm:"column:job_type"`
	RequestedBy      string  `gorm:"column:requested_by"`
	Priority         int64   `gorm:"column:priority"`
	State            string  `gorm:"column:state"`
	Phase            string  `gorm:"column:phase"`
	SourceFileID     *string `gorm:"column:source_file_id"`
	ResumeOfJobID    *string `gorm:"column:resume_of_job_id"`
	CreatedAtMS      int64   `gorm:"column:created_at_ms"`
	StartedAtMS      *int64  `gorm:"column:started_at_ms"`
	FinishedAtMS     *int64  `gorm:"column:finished_at_ms"`
	ProgressCurrent  *int64  `gorm:"column:progress_current"`
	ProgressTotal    *int64  `gorm:"column:progress_total"`
	ResumeGeneration *int64  `gorm:"column:resume_generation"`
	ResumeOffset     *int64  `gorm:"column:resume_offset"`
	ErrorClass       *string `gorm:"column:error_class"`
	UpdatedAtMS      int64   `gorm:"column:updated_at_ms"`
}

func (jobRunModel) TableName() string { return "job_runs" }

type healthEventModel struct {
	EventID         string  `gorm:"column:event_id;primaryKey"`
	Fingerprint     string  `gorm:"column:fingerprint"`
	Domain          string  `gorm:"column:domain"`
	Severity        string  `gorm:"column:severity"`
	Code            string  `gorm:"column:code"`
	SourceFileID    *string `gorm:"column:source_file_id"`
	JobID           *string `gorm:"column:job_id"`
	ErrorClass      *string `gorm:"column:error_class"`
	FirstSeenAtMS   int64   `gorm:"column:first_seen_at_ms"`
	LastSeenAtMS    int64   `gorm:"column:last_seen_at_ms"`
	ResolvedAtMS    *int64  `gorm:"column:resolved_at_ms"`
	OccurrenceCount int64   `gorm:"column:occurrence_count"`
	UpdatedAtMS     int64   `gorm:"column:updated_at_ms"`
}

func (healthEventModel) TableName() string { return "health_events" }

type pricingVersionModel struct {
	PricingVersion  string `gorm:"column:pricing_version;primaryKey"`
	Source          string `gorm:"column:source"`
	Currency        string `gorm:"column:currency"`
	EffectiveFromMS int64  `gorm:"column:effective_from_ms"`
	CreatedAtMS     int64  `gorm:"column:created_at_ms"`
}

func (pricingVersionModel) TableName() string { return "pricing_versions" }

type modelPriceModel struct {
	PricingVersion              string `gorm:"column:pricing_version;primaryKey"`
	MatchKind                   string `gorm:"column:match_kind;primaryKey"`
	ModelPattern                string `gorm:"column:model_pattern;primaryKey"`
	Priority                    int64  `gorm:"column:priority"`
	InputMicrosPerMillion       *int64 `gorm:"column:input_micros_per_million"`
	CachedInputMicrosPerMillion *int64 `gorm:"column:cached_input_micros_per_million"`
	OutputMicrosPerMillion      *int64 `gorm:"column:output_micros_per_million"`
}

func (modelPriceModel) TableName() string { return "model_prices" }
