package reporting_do

type AccountTokenPeriod struct {
	ID              string `gorm:"primaryKey"`
	AccountKey      string
	Provider        string
	ClientID        string
	WindowStartAtMS int64
	ResetsAtMS      int64
	CollectedAtMS   int64
}

func (AccountTokenPeriod) TableName() string { return "pulse_account_token_periods" }
