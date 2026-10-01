package do

// Batch 是已提交的幂等收据，不保存重复传输的正文。
type Batch struct {
	ClientID     string `gorm:"primaryKey"`
	BatchID      string `gorm:"primaryKey"`
	Digest       string
	ReceivedAtMS int64
}

func (Batch) TableName() string { return "pulse_batches" }

type Project struct {
	ID          string `gorm:"primaryKey"`
	ClientID    string
	Provider    string
	LocalID     string
	Name        string
	GroupID     string
	UpdatedAtMS int64
}

func (Project) TableName() string { return "pulse_projects" }

type SessionSource struct {
	ID            string `gorm:"primaryKey"`
	SessionKey    string
	ClientID      string
	HomeID        string
	SourceKind    string
	Revision      int64
	CollectedAtMS int64
	Digest        string
	Payload       string
}

func (SessionSource) TableName() string { return "pulse_session_sources" }

type Session struct {
	ID                string `gorm:"primaryKey"`
	Provider          string
	SessionID         string
	Title             string
	ProjectID         string
	SourceKind        string
	SessionKind       string
	HistoryStartAtMS  int64
	CanonicalSourceID string
	CanonicalRevision int64
	CreatedAtMS       *int64
	LastActiveAtMS    *int64
	CollectedAtMS     int64
	Complete          bool
	CorrectionFence   bool
	Conflict          bool
	Deleted           bool
}

func (Session) TableName() string { return "pulse_sessions" }

type CanonicalSnapshot struct {
	SessionKey string `gorm:"primaryKey"`
	Payload    string
}

func (CanonicalSnapshot) TableName() string { return "pulse_session_canonical" }

type Usage struct {
	SessionKey             string `gorm:"primaryKey"`
	ContributionID         string `gorm:"primaryKey"`
	Position               int64
	ObservedAtMS           *int64
	Model                  *string
	InputTokens            *int64
	CachedTokens           *int64
	CacheWriteTokens       *int64
	OutputTokens           *int64
	ReasoningTokens        *int64
	TotalTokens            *int64
	CostMicroUSD           *int64
	ReportedChargeMicroUSD *int64
	PricingVersion         *string
	PricingMode            string
	InputPrice             *int64
	CachedPrice            *int64
	CacheWritePrice        *int64
	OutputPrice            *int64
	CostStatus             string
}

func (Usage) TableName() string { return "pulse_usage" }

type Invocation struct {
	SessionKey   string `gorm:"primaryKey"`
	InvocationID string `gorm:"primaryKey"`
	ObservedAtMS int64
	Kind         string
	ToolName     string
	Outcome      string
	DurationMS   *int64
}

func (Invocation) TableName() string { return "pulse_invocations" }

type Account struct {
	ID            string `gorm:"primaryKey"`
	Provider      string
	AccountID     string
	Email         *string
	Plan          *string
	CollectedAtMS int64
}

func (Account) TableName() string { return "pulse_accounts" }

type AccountBinding struct {
	ID            string `gorm:"primaryKey"`
	ClientID      string
	Provider      string
	LocalScope    string
	AccountKey    string
	ConfirmedAtMS int64
}

func (AccountBinding) TableName() string { return "pulse_account_bindings" }

type QuotaObservation struct {
	ID            string `gorm:"primaryKey"`
	ClientID      string
	Provider      string
	AccountKey    *string
	LocalScope    string
	ObservationID string
	LimitID       string
	WindowKind    string
	WindowMinutes *int64
	ResetsAtMS    *int64
	ObservedAtMS  int64
	UsedPercent   *float64
	Validity      string
	Source        string
	HistoryOrigin string
	ReceivedAtMS  int64
}

func (QuotaObservation) TableName() string { return "pulse_quota_observations" }

type ResetCredits struct {
	ID            string `gorm:"primaryKey"`
	ClientID      string
	Provider      string
	AccountKey    *string
	LocalScope    string
	ObservedAtMS  int64
	Inventory     *int64
	Status        string
	NextResetAtMS *int64
}

func (ResetCredits) TableName() string { return "pulse_reset_credits" }

type DeviceStatus struct {
	ClientID        string `gorm:"primaryKey"`
	Provider        string `gorm:"primaryKey"`
	Version         string
	CollectedAtMS   *int64
	CoverageStartMS *int64
	CoverageEndMS   *int64
	PendingBatches  int64
	Status          string
	ReceivedAtMS    int64
}

func (DeviceStatus) TableName() string { return "pulse_device_status" }
