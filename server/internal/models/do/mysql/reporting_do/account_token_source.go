package reporting_do

type AccountTokenSource struct {
	ID       string `gorm:"primaryKey"`
	FactID   string
	ClientID string
}

func (AccountTokenSource) TableName() string { return "pulse_account_token_sources" }
