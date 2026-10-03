package reporting_do

type AccountBinding struct {
	ID            string `gorm:"primaryKey"`
	ClientID      string
	Provider      string
	LocalScope    string
	AccountKey    string
	ConfirmedAtMS int64
}

func (AccountBinding) TableName() string { return "pulse_account_bindings" }
