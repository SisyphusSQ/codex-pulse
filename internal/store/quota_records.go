package store

const maxQuotaWindowMinutes int64 = 525600

type QuotaSource string

const (
	QuotaAccountScopeDefault             = "default"
	QuotaSourceLocalJSONL    QuotaSource = "local_jsonl"
	QuotaSourceWham          QuotaSource = "wham"
)

type QuotaWindowKind string

const (
	QuotaWindowPrimary   QuotaWindowKind = "primary"
	QuotaWindowSecondary QuotaWindowKind = "secondary"
)

type QuotaValidity string

const (
	QuotaValidityAccepted   QuotaValidity = "accepted"
	QuotaValiditySuspicious QuotaValidity = "suspicious"
	QuotaValidityRejected   QuotaValidity = "rejected"
)

type QuotaRejectionReason string

const (
	QuotaReasonMissingLimitID       QuotaRejectionReason = "missing_limit_id"
	QuotaReasonMissingPrimaryWindow QuotaRejectionReason = "missing_primary_window"
	QuotaReasonResetNotFuture       QuotaRejectionReason = "reset_not_future"
	QuotaReasonUnknownPlanType      QuotaRejectionReason = "unknown_plan_type"
	QuotaReasonInvalidUsedPercent   QuotaRejectionReason = "invalid_used_percent"
	QuotaReasonInvalidWindowMinutes QuotaRejectionReason = "invalid_window_minutes"
	QuotaReasonInvalidResetsAt      QuotaRejectionReason = "invalid_resets_at"
	QuotaReasonInvalidStructure     QuotaRejectionReason = "invalid_structure"
	QuotaReasonUsedRegression       QuotaRejectionReason = "used_regression"
	QuotaReasonResetRegression      QuotaRejectionReason = "reset_regression"
	QuotaReasonObservedRegression   QuotaRejectionReason = "observed_time_regression"
	QuotaReasonSourceConflict       QuotaRejectionReason = "source_conflict"
	QuotaReasonDefaultFallback      QuotaRejectionReason = "default_fallback"
)

// QuotaObservationSample is one source observation before continuous samples
// are coalesced. It does not carry source JSON or response content.
type QuotaObservationSample struct {
	ObservationID    string
	AccountScope     string
	Source           QuotaSource
	LimitID          *string
	WindowKind       QuotaWindowKind
	UsedPercent      float64
	WindowMinutes    int64
	ResetsAtMS       int64
	PlanType         *string
	ObservedAtMS     int64
	Validity         QuotaValidity
	RejectionReason  *QuotaRejectionReason
	RequestID        *string
	SessionID        *string
	SourceFileID     *string
	SourceGeneration int64
	SourceOffset     int64
}

// QuotaObservation is the durable, possibly coalesced audit segment.
type QuotaObservation struct {
	ObservationID         string
	AccountScope          string
	Source                QuotaSource
	LimitID               *string
	WindowKind            QuotaWindowKind
	UsedPercent           float64
	WindowMinutes         int64
	ResetsAtMS            int64
	PlanType              *string
	Validity              QuotaValidity
	RejectionReason       *QuotaRejectionReason
	FirstObservedAtMS     int64
	LastObservedAtMS      int64
	SampleCount           int64
	RequestID             *string
	SessionID             *string
	SourceFileID          *string
	FirstSourceGeneration int64
	FirstSourceOffset     int64
	SourceGeneration      int64
	SourceOffset          int64
}

type QuotaObservationFilter struct {
	AccountScope *string
	Source       *QuotaSource
	WindowKind   *QuotaWindowKind
	Validity     *QuotaValidity
	SessionID    *string
	SourceFileID *string
	Limit        int
}
