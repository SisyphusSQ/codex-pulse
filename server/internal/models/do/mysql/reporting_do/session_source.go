package reporting_do

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
