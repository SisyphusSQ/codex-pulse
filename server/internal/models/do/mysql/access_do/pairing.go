package access_do

// Pairing 的权限在签发时固定，消费在同库事务内完成。
type Pairing struct {
	CodeHash     string `gorm:"primaryKey"`
	Purpose      string
	Name         string
	CreatedAtMS  int64
	ExpiresAtMS  int64
	ConsumedAtMS *int64
}

func (Pairing) TableName() string { return "pulse_pairings" }
