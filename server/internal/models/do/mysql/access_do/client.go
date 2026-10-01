package access_do

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
