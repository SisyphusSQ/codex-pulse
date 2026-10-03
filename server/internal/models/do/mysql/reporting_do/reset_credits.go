package reporting_do

type ResetCredits struct {
	DetailsStatus   string
	ExpirySchedule  string
	NextExpiresAtMS *int64
	ID              string `gorm:"primaryKey"`
	ClientID        string
	Provider        string
	AccountKey      *string
	LocalScope      string
	ObservedAtMS    int64
	Inventory       *int64
	Status          string
	NextResetAtMS   *int64
}

func (ResetCredits) TableName() string { return "pulse_reset_credits" }
