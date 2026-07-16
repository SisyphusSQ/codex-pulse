package store

type RuntimeQueryDirection string

const (
	RuntimeQueryAscending  RuntimeQueryDirection = "asc"
	RuntimeQueryDescending RuntimeQueryDirection = "desc"
)

type RuntimeSourceKind string

const (
	RuntimeSourceLocalFile RuntimeSourceKind = "local_file"
	RuntimeSourceOnline    RuntimeSourceKind = "online"
)

type RuntimeSourceCursor struct {
	UpdatedAtMS int64
	SourceKey   string
}

type RuntimeSourceQuery struct {
	Kinds     []RuntimeSourceKind
	States    []string
	After     *RuntimeSourceCursor
	Limit     int
	Direction RuntimeQueryDirection
}

type RuntimeSourceRecord struct {
	SourceKey   string
	Kind        RuntimeSourceKind
	UpdatedAtMS int64
	Local       *SourceFile
	Online      *SourceState
}

type RuntimeSourceSummary struct {
	Total         int64
	LocalFiles    int64
	OnlineSources int64
	Attention     int64
}

type RuntimeSourcePage struct {
	Records          []RuntimeSourceRecord
	MatchedCount     int64
	Summary          RuntimeSourceSummary
	UnavailableKinds []RuntimeSourceKind
	NextCursor       *RuntimeSourceCursor
}

type RuntimeJobCursor struct {
	UpdatedAtMS int64
	JobID       string
}

type RuntimeJobQuery struct {
	States    []JobState
	Phases    []JobPhase
	After     *RuntimeJobCursor
	Limit     int
	Direction RuntimeQueryDirection
}

type RuntimeJobRecord struct {
	Job   JobRun
	Task  *SchedulerTask
	Retry *SchedulerRetryState
}

type RuntimeJobSummary struct {
	Total       int64
	Queued      int64
	Running     int64
	Succeeded   int64
	Failed      int64
	Cancelled   int64
	Interrupted int64
}

type RuntimeJobPage struct {
	Records      []RuntimeJobRecord
	MatchedCount int64
	Summary      RuntimeJobSummary
	NextCursor   *RuntimeJobCursor
}

type RuntimeHealthCursor struct {
	LastSeenAtMS int64
	EventID      string
}

type RuntimeHealthQuery struct {
	Active     *bool
	Severities []HealthSeverity
	Domains    []HealthDomain
	After      *RuntimeHealthCursor
	Limit      int
	Direction  RuntimeQueryDirection
}

type RuntimeHealthSummary struct {
	Total          int64
	Active         int64
	Resolved       int64
	Info           int64
	Warnings       int64
	Errors         int64
	Critical       int64
	ActiveInfo     int64
	ActiveWarnings int64
	ActiveErrors   int64
	ActiveCritical int64
}

type RuntimeHealthPage struct {
	Records      []HealthEvent
	MatchedCount int64
	Summary      RuntimeHealthSummary
	Lifecycle    *SchedulerLifecycle
	NextCursor   *RuntimeHealthCursor
}
