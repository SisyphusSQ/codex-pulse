package reporting_do

type PaidCredit struct {
	ID           string `gorm:"primaryKey"`
	ClientID     string
	Provider     string
	AccountKey   string
	LocalScope   string
	LimitID      string
	State        string
	Balance      *string
	HasCredits   *bool
	Unlimited    *bool
	ObservedAtMS int64
	ReceivedAtMS int64
}

func (PaidCredit) TableName() string { return "pulse_paid_credits" }
