package reporting_do

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
