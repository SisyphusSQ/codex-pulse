package reporting_do

type Account struct {
	ID            string `gorm:"primaryKey"`
	Provider      string
	AccountID     string
	Email         *string
	Plan          *string
	CollectedAtMS int64
}

func (Account) TableName() string { return "pulse_accounts" }
