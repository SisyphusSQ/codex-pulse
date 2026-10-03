package reporting_do

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
