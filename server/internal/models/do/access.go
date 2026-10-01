package do

// Client 统一保存采集设备与管理浏览器的授权摘要，不保存可复用明文。
type Client struct {
	ID               string `gorm:"primaryKey"`
	Purpose          string
	Name             string
	SecretHash       string
	Origin           string
	CSRFHash         string
	CreatedAtMS      int64
	ExpiresAtMS      *int64
	RevokedAtMS      *int64
	LastReceivedAtMS *int64
}

func (Client) TableName() string { return "pulse_clients" }

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
