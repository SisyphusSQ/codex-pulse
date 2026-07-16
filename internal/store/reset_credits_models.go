package store

type resetCreditsSnapshotModel struct {
	SnapshotID     string `gorm:"column:snapshot_id;primaryKey"`
	RequestID      string `gorm:"column:request_id"`
	AccountScope   string `gorm:"column:account_scope"`
	AvailableCount int64  `gorm:"column:available_count"`
	ObservedAtMS   int64  `gorm:"column:observed_at_ms"`
}

func (resetCreditsSnapshotModel) TableName() string { return "reset_credit_snapshots" }

type resetCreditModel struct {
	SnapshotID   string `gorm:"column:snapshot_id;primaryKey"`
	CreditIDHash string `gorm:"column:credit_id_hash;primaryKey"`
	Status       string `gorm:"column:status"`
	ResetType    string `gorm:"column:reset_type"`
	GrantedAtMS  int64  `gorm:"column:granted_at_ms"`
	ExpiresAtMS  int64  `gorm:"column:expires_at_ms"`
	RedeemedAtMS *int64 `gorm:"column:redeemed_at_ms"`
}

func (resetCreditModel) TableName() string { return "reset_credits" }
