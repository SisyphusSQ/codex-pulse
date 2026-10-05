package reporting_do

type AccountTokenFact struct {
	ID           string `gorm:"primaryKey"`
	AccountKey   string
	Provider     string
	ObservedAtMS int64
	TotalTokens  int64
}

func (AccountTokenFact) TableName() string { return "pulse_account_token_facts" }
